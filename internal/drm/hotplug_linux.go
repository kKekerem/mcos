package drm

import (
	"bytes"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// ── Monitör takma/çıkarma (hotplug) ─────────────────────────────────────────
//
// NEDEN: Ekran yalnızca açılışta kuruluyordu. Gerçek bilgisayarlarda üç
// yaygın durumda panel yanlış kalıyordu:
//
//   - Monitör açılıştan SONRA açıldı / takıldı (sunucu makinesi monitörsüz
//     açılır, sonra bakmak için ekran bağlanır).
//   - Monitör değiştirildi: 1080p ekranın yerine 4K takıldı; panel eski
//     modda kalıyor, yeni ekranın doğal çözünürlüğü ve tazelemesi hiç
//     seçilmiyordu. Yeni monitör eski modu desteklemiyorsa kara ekran.
//   - Dizüstünde harici ekran çıkarıldı; birincil çıkış o idiyse panel
//     görünmeyen bir çıkışa çizmeye devam ediyordu.
//
// Biz DRM "master" olduğumuz sürece çekirdek bu durumlarda ekranı KENDİSİ
// yeniden kurmaz (fbcon'un hotplug işleyicisi master varken çalışmaz); yeni
// düzeni kurmak bizim işimiz. Çekirdek her bağlantı değişiminde
// NETLINK_KOBJECT_UEVENT üzerinden "SUBSYSTEM=drm HOTPLUG=1" yayınlar
// (kesmesiz VGA gibi çıkışlar için de: çekirdek onları 10 sn'de bir yoklar
// ve aynı olayı üretir). udev gerekmez; imajda udev yok.

// isDRMHotplug reports whether a raw uevent message is a DRM hotplug event.
//
// Mesaj biçimi: "change@/devices/.../drm/card0\0ACTION=change\0...
// SUBSYSTEM=drm\0HOTPLUG=1\0...". Alanlar NUL ile ayrılır. udev'in yeniden
// yayınladığı "libudev" başlıklı mesajlar (imajda yok ama olası) ikili
// başlık taşır; onlar da aynı KEY=VALUE alanlarını içerdiğinden alan taraması
// ikisinde de çalışır.
func isDRMHotplug(msg []byte) bool {
	drm, hot := false, false
	for _, f := range bytes.Split(msg, []byte{0}) {
		switch string(f) {
		case "SUBSYSTEM=drm":
			drm = true
		case "HOTPLUG=1":
			hot = true
		}
	}
	return drm && hot
}

// hotplugWatch listens for DRM hotplug uevents in the background.
type hotplugWatch struct {
	fd   int
	hit  atomic.Bool
	stop atomic.Bool
}

// watchHotplug opens the uevent socket. Hata olursa nil döner: hotplug
// algılanmaz ama ekran açılıştaki düzenle çalışmaya devam eder.
func watchHotplug() *hotplugWatch {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil
	}
	// Grup 1 = çekirdeğin kendi yayınladığı olaylar.
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1}); err != nil {
		unix.Close(fd)
		return nil
	}
	// Okuma zaman aşımı: Close() başka bir goroutine'de bekleyen read'i
	// Linux'ta güvenilir biçimde uyandırmaz; döngü saniyede bir stop
	// bayrağına bakar.
	tv := unix.NsecToTimeval(int64(time.Second))
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	w := &hotplugWatch{fd: fd}
	go w.loop()
	return w
}

func (w *hotplugWatch) loop() {
	// fd'yi YALNIZCA bu döngü kapatır (bkz. close).
	defer unix.Close(w.fd)
	buf := make([]byte, 8192)
	for !w.stop.Load() {
		n, _, err := unix.Recvfrom(w.fd, buf, 0)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EINTR || err == unix.ENOBUFS {
				// ENOBUFS: olay taşması; bir hotplug kaçmış olabilir,
				// yeniden taramak ucuz ve güvenli.
				if err == unix.ENOBUFS {
					w.hit.Store(true)
				}
				continue
			}
			return
		}
		if isDRMHotplug(buf[:n]) {
			w.hit.Store(true)
		}
	}
}

// take reports (and clears) whether a hotplug happened since the last call.
func (w *hotplugWatch) take() bool {
	if w == nil {
		return false
	}
	return w.hit.Swap(false)
}

// again re-arms the flag (olay şimdi işlenemedi, sonra tekrar bakılacak).
func (w *hotplugWatch) again() {
	if w != nil {
		w.hit.Store(true)
	}
}

func (w *hotplugWatch) close() {
	if w == nil {
		return
	}
	// fd burada KAPATILMAZ. Kapatılsaydı döngü o sırada Recvfrom'da
	// bekliyor olabilirdi; çekirdek aynı fd numarasını hemen yeni açılan
	// bir dosyaya (ör. panelin IPC soketi) verirse döngü bir sonraki
	// turda ONUN baytlarını okuyup yutardı. Döngü SO_RCVTIMEO sayesinde en
	// geç 1 sn içinde bayrağı görür ve fd'yi kendisi kapatır.
	w.stop.Store(true)
}

// ── Bağlantı imzası ─────────────────────────────────────────────────────────

// outputSig summarizes which monitors are connected and what they offer.
//
// Hotplug olayı her zaman bir DEĞİŞİKLİK demek değildir: bazı monitörler
// bekleme kipine girip çıkarken de HPD sinyalini oynatır. Her olayda ekranı
// yeniden kurmak (mod ayarı = bir an kararma) bu monitörlerde sonsuz bir
// titreme döngüsü olurdu. Bu yüzden yalnızca imza değiştiyse yeniden
// kurulur: bağlı çıkışların kimliği + tercih ettiği mod + mod sayısı.
func outputSig(bagli []connector) string {
	var b bytes.Buffer
	for _, c := range bagli {
		b.WriteString(c.Name())
		b.WriteByte(':')
		if m, ok := preferredOf(c.Modes); ok {
			b.WriteString(m.Key())
		}
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(c.Modes)))
		b.WriteByte(';')
	}
	return b.String()
}

func preferredOf(modes []Mode) (Mode, bool) {
	for _, m := range modes {
		if m.Preferred {
			return m, true
		}
	}
	if len(modes) > 0 {
		return modes[0], true
	}
	return Mode{}, false
}

// connectedList returns the connected outputs in the same order open() sees
// them (imza karşılaştırması sıraya duyarlı).
//
// GETCONNECTOR'ın ilk aşaması (count_modes=0) çekirdeğe ZORLA yoklama
// yaptırır (DDC/EDID okuması, onlarca ms). Bu yüzden yalnızca hotplug
// olayında çağrılır, her saniye değil.
func (d *Device) connectedList() ([]connector, error) {
	_, conns, err := d.resources()
	if err != nil {
		return nil, err
	}
	var bagli []connector
	for _, id := range conns {
		c, err := d.connectorInfo(id)
		if errors.Is(err, ErrLost) {
			return nil, err
		}
		if err == nil && c.Connected && len(c.Modes) > 0 {
			bagli = append(bagli, c)
		}
	}
	return bagli, nil
}

// HotplugWatch, ekran kartı açık DEĞİLKEN monitör takılmasını bekleyenler
// içindir (panel monitörsüz açıldığında; bkz. cmd/mcos-panel-fb). Açık bir
// Screen kendi dinleyicisini zaten tutar.
type HotplugWatch struct{ w *hotplugWatch }

// WatchHotplug starts listening; soket açılamazsa nil döner (nil güvenli).
func WatchHotplug() *HotplugWatch {
	w := watchHotplug()
	if w == nil {
		return nil
	}
	return &HotplugWatch{w: w}
}

// Take reports (and clears) whether a DRM hotplug event arrived.
func (h *HotplugWatch) Take() bool {
	if h == nil {
		return false
	}
	return h.w.take()
}

// Close stops listening.
func (h *HotplugWatch) Close() {
	if h != nil {
		h.w.close()
	}
}
