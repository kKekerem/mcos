package drm

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Çekirdeğin drm_sysfs_hotplug_event() ile yayınladığı mesajın birebir
// biçimi (Linux 6.6, QEMU virtio-gpu'da monitörün boyutu değişince
// yakalanan dizilim: başlık + NUL ile ayrılmış alanlar).
func uevent(fields ...string) []byte {
	return []byte(strings.Join(fields, "\x00") + "\x00")
}

func TestHotplugOlayiTaninir(t *testing.T) {
	msg := uevent("change@/devices/pci0000:00/0000:00:02.0/drm/card0",
		"ACTION=change", "DEVPATH=/devices/pci0000:00/0000:00:02.0/drm/card0",
		"SUBSYSTEM=drm", "HOTPLUG=1", "DEVNAME=dri/card0", "DEVTYPE=drm_minor",
		"SEQNUM=2141", "MAJOR=226", "MINOR=0")
	if !isDRMHotplug(msg) {
		t.Fatal("gerçek DRM hotplug olayı tanınmadı")
	}
	// Bağlantıya özgü olay (drm_sysfs_connector_hotplug_event) da HOTPLUG=1 taşır.
	msg = uevent("change@/devices/virtual/drm/card1", "ACTION=change",
		"SUBSYSTEM=drm", "HOTPLUG=1", "CONNECTOR=36", "PROPERTY=5")
	if !isDRMHotplug(msg) {
		t.Fatal("bağlantıya özgü hotplug olayı tanınmadı")
	}
}

func TestHotplugOlmayanOlaylarYokSayilir(t *testing.T) {
	cases := map[string][]byte{
		// USB bellek takıldı: ekranı yeniden kurmak gereksiz kararma olurdu.
		"usb": uevent("add@/devices/pci0000:00/usb1/1-1", "ACTION=add",
			"SUBSYSTEM=usb", "DEVTYPE=usb_device"),
		// DRM ama hotplug değil (ör. kartın eklenmesi; o yol ErrLost ile işlenir).
		"drm-add": uevent("add@/devices/virtual/drm/card1", "ACTION=add",
			"SUBSYSTEM=drm", "DEVNAME=dri/card1"),
		// HOTPLUG=1 başka alt sistemden (ör. sound jack) gelirse de değil.
		"baska": uevent("change@/devices/x", "SUBSYSTEM=sound", "HOTPLUG=1"),
		// Alan önekiyle eşleşme olmamalı ("HOTPLUG=10" gibi).
		"onek": uevent("change@/devices/x", "SUBSYSTEM=drm", "HOTPLUG=10"),
		"bos":  nil,
	}
	for ad, msg := range cases {
		if isDRMHotplug(msg) {
			t.Errorf("%s: hotplug sanıldı", ad)
		}
	}
}

func conn(typ, id uint32, modes ...Mode) connector {
	return connector{ID: 30 + id, Type: typ, TypeID: id, Connected: true, Modes: modes}
}

func TestImzaMonitorDegisinceDegisir(t *testing.T) {
	m1080 := Mode{Width: 1920, Height: 1080, MilliHz: 60000, Preferred: true}
	m4k := Mode{Width: 3840, Height: 2160, MilliHz: 60000, Preferred: true}
	m720 := Mode{Width: 1280, Height: 720, MilliHz: 60000}
	const hdmi = 11

	once := outputSig([]connector{conn(hdmi, 1, m1080, m720)})
	// Aynı monitör HPD'yi oynattı: imza AYNI kalmalı (yoksa titreme döngüsü).
	if s := outputSig([]connector{conn(hdmi, 1, m1080, m720)}); s != once {
		t.Fatalf("aynı monitör için imza değişti: %q != %q", s, once)
	}
	// Aynı porta 4K monitör takıldı: imza DEĞİŞMELİ. Mod SAYISI bilerek
	// aynı (2): yalnızca tercih edilen mod farklı. (İlk yazılışta 4K'ya bir
	// mod fazla verilmişti; imza tercih edilen modu hiç içermese de test
	// mod sayısı yüzünden geçiyordu — karşı sınamada yakalandı.)
	if s := outputSig([]connector{conn(hdmi, 1, m4k, m720)}); s == once {
		t.Fatal("4K monitör takıldığı hâlde imza aynı kaldı")
	}
	// İkinci monitör eklendi.
	if s := outputSig([]connector{conn(hdmi, 1, m1080, m720), conn(hdmi, 2, m1080)}); s == once {
		t.Fatal("ikinci monitör eklendiği hâlde imza aynı kaldı")
	}
	// Tercih aynı ama mod listesi değişti (farklı model, aynı doğal çözünürlük).
	if s := outputSig([]connector{conn(hdmi, 1, m1080)}); s == once {
		t.Fatal("mod listesi değiştiği hâlde imza aynı kaldı")
	}
	// Hepsi çıkarıldı.
	if s := outputSig(nil); s == once || s != "" {
		t.Fatalf("boş bağlantı imzası %q", s)
	}
}

// Canlı soket: WSL/CI çekirdeğinde de NETLINK_KOBJECT_UEVENT açılabilmeli ve
// close() döngüyü en geç ~1 sn'de bitirmeli (fd'yi döngü kapatır).
func TestHotplugDinleyiciAcilirKapanir(t *testing.T) {
	w := watchHotplug()
	if w == nil {
		t.Skip("uevent soketi bu ortamda açılamadı")
	}
	fd := w.fd
	// Döngü Recvfrom'da BEKLERKEN kapat: asıl sınanan zaman aşımı yolu.
	// (Hemen kapatılsa döngü daha başlamadan çıkabilirdi.)
	time.Sleep(300 * time.Millisecond)
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		t.Fatal("dinleyici kapatılmadan önce fd kapanmış — döngü hemen çıkıyor")
	}
	w.close()
	son := time.Now().Add(3 * time.Second)
	for time.Now().Before(son) {
		// Döngü fd'yi kapatınca fcntl EBADF döner (numara yeniden
		// kullanılmadıysa). Kullanıldıysa bile soket türü değişir.
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
			return
		}
		if typ, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL); err == nil && typ != unix.NETLINK_KOBJECT_UEVENT {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("close() sonrası dinleyici fd'si 3 sn'de kapanmadı")
}
