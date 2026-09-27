//go:build linux

// Package drm talks to the kernel's mode-setting interface, in pure Go.
//
// ════════════════════════════════════════════════════════════════════════════
// ── Neden bu paket var ──────────────────────────────────────────────────────
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının üç isteği aynı yere çıkıyor:
//
//	"yeniden baslatmadan cozunurluk degistirme"
//	"ekran tazeleme hizi maks kac destekliyosa o kadar olmali"
//	"ekran kartına cizdirsin bunları"
//
// Panel eskiden /dev/fb0'a yazıyordu. O yol fbdev emülasyonundan geçer ve
// çekirdekte SABİT bir gecikmeyle taranır:
//
//	drivers/gpu/drm/drm_fbdev_generic.c:120   fbdefio.delay = HZ / 20;
//
// Yani panel ne kadar hızlı çizerse çizsin ekran saniyede EN FAZLA 20 kez
// tazelenir; çözünürlük de GRUB'ın verdiği modda kilitli kalır.
//
// DRM/KMS üçünü de çözüyor: modları donanımdan okuyor, çalışırken mod
// değiştiriyor ve sayfa çevirme ile monitörün gerçek tazeleme hızına çıkıyor.
//
// ── Neden libdrm değil ──────────────────────────────────────────────────────
//
// CGO_ENABLED=0 zorunlu (initramfs'te libc bağımlılığı istemiyoruz). Bütün
// arayüz ioctl'lerden ibaret; yapılar çekirdek başlıklarından BİREBİR
// kopyalandı ve boyutları testte sabitlendi. Bir alan kayması çekirdek
// belleğini bozar.
//
// Bu dosya DÜŞÜK SEVİYE: aygıt açma, kaynak/bağlayıcı/encoder sorguları,
// master. Ekranı süren taraf screen_linux.go, tamponlar buffer_linux.go,
// platformdan bağımsız mod mantığı mode.go.
package drm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ── ioctl numaraları ────────────────────────────────────────────────────────
//
// _IOC(dir,type,nr,size) = (dir<<30) | (size<<16) | (type<<8) | nr
// DRM'in harfi 'd'. Boyut YAPIDAN geliyor: yanlış boyut = yanlış ioctl
// numarası = çekirdek isteği tanımaz.
func iowr(nr, size uintptr) uintptr {
	const okuYaz = 3 // _IOC_READ|_IOC_WRITE
	return okuYaz<<30 | size<<16 | 'd'<<8 | nr
}

// io builds a DRM_IO (no argument) ioctl number.
func io(nr uintptr) uintptr { return 'd'<<8 | nr }

const (
	nrVersion      = 0x00
	nrGetCap       = 0x0c
	nrSetMaster    = 0x1e
	nrDropMaster   = 0x1f
	nrGetResources = 0xA0
	nrGetCrtc      = 0xA1
	nrSetCrtc      = 0xA2
	nrGetEncoder   = 0xA6
	nrGetConnector = 0xA7
	nrAddFB        = 0xAE
	nrRmFB         = 0xAF
	nrPageFlip     = 0xB0
	nrDirtyFB      = 0xB1
	nrCreateDumb   = 0xB2
	nrMapDumb      = 0xB3
	nrDestroyDumb  = 0xB4
	nrAddFB2       = 0xB8
)

// ── Çekirdek yapıları (include/uapi/drm/drm.h, drm_mode.h) ──────────────────

type cardRes struct {
	FbIDPtr, CrtcIDPtr, ConnIDPtr, EncIDPtr     uint64
	CountFbs, CountCrtcs, CountConns, CountEncs uint32
	MinW, MaxW, MinH, MaxH                      uint32
}

type getConn struct {
	EncodersPtr, ModesPtr, PropsPtr, PropValuesPtr uint64
	CountModes, CountProps, CountEncoders          uint32
	EncoderID, ConnectorID, ConnectorType          uint32
	ConnectorTypeID, Connection                    uint32
	MmWidth, MmHeight, Subpixel                    uint32
	Pad                                            uint32
}

type getEncoder struct {
	EncoderID, EncoderType, CrtcID uint32
	PossibleCrtcs, PossibleClones  uint32
}

// modeCrtc is struct drm_mode_crtc (GETCRTC / SETCRTC).
type modeCrtc struct {
	SetConnectorsPtr uint64
	CountConnectors  uint32
	CrtcID           uint32
	FbID             uint32
	X, Y             uint32
	GammaSize        uint32
	ModeValid        uint32
	Mode             ModeInfo
}

// drmVersion is struct drm_version. __kernel_size_t ve işaretçiler amd64'te
// 8 bayt; üç int'ten sonra 4 baytlık hizalama boşluğu var.
type drmVersion struct {
	Major, Minor, Patch int32
	_                   int32
	NameLen             uint64
	Name                uint64
	DateLen             uint64
	Date                uint64
	DescLen             uint64
	Desc                uint64
}

type getCap struct{ Capability, Value uint64 }

const capDumbBuffer = 0x1

// connectionConnected, bir ekranın gerçekten takılı olduğu durum.
const connectionConnected = 1

// ── Aygıt ───────────────────────────────────────────────────────────────────

// ErrLost, aygıtın çekirdek tarafından söküldüğünü bildirir.
//
// Gerçek PC'de EN SIK karşılaşılan durum budur: açılışta simpledrm
// (firmware framebuffer'ı) card0 olarak gelir; ardından i915/amdgpu probe
// edip onu DEVRALIR (drm_aperture_remove_conflicting_pci_framebuffers ->
// simpledrm'de drm_dev_unplug). O andan sonra eski aygıttaki her ioctl
// -ENODEV döner. Panel bunu görüp YENİ kartı açmalı; eskiden /dev/fb0'ın
// eski eşlemesine yazmaya devam ediyor ve ekran ilk karede DONUYORDU.
var ErrLost = errors.New("drm: ekran aygıtı kayboldu")

// ErrNoMonitor: ekran kartı(ları) açıldı ama HİÇBİRİNE monitör bağlı değil.
//
// Diğer hatalardan ayrı tutulur çünkü GEÇİCİDİR: sunucu makinesi monitörsüz
// açılır, sonra ekran takılır. Panel bu durumda framebuffer'la başlar ve
// monitör gelince ekran kartı yoluna geçer (cmd/mcos-panel-fb upgradeDisplay).
// Kalıcı hatalarda (CPU tamponu yok vb.) yeniden denemek boşunadır.
var ErrNoMonitor = errors.New("drm: bağlı monitör yok")

// Device is an open DRM card.
type Device struct {
	f *os.File
	// Path, açılan düğüm: /dev/dri/card0.
	Path string
	// Driver, çekirdek sürücüsünün adı: i915, amdgpu, simpledrm, bochs-drm...
	Driver string
	master bool
}

// Cards lists the DRM primary nodes, lowest number first.
func Cards() []string {
	ms, _ := filepath.Glob("/dev/dri/card*")
	sort.Slice(ms, func(i, j int) bool {
		// card10'un card2'den SONRA gelmesi için sayısal sıra.
		return cardNum(ms[i]) < cardNum(ms[j])
	})
	return ms
}

func cardNum(p string) int {
	n := 0
	for _, c := range strings.TrimPrefix(filepath.Base(p), "card") {
		if c < '0' || c > '9' {
			return 1 << 30
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// OpenCard opens one card and reads its driver name.
func OpenCard(path string) (*Device, error) {
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	d := &Device{f: f, Path: path}
	d.Driver, _ = d.version()
	return d, nil
}

// Close releases the device.
//
// Dosya kapanınca çekirdek master'ı da bırakır ve fbcon kendi modunu geri
// yükler (drm_fbdev_generic_client_restore). Panel çökse bile konsol
// kurtulur, çünkü süreç ölünce fd'ler çekirdek tarafından kapatılır.
func (d *Device) Close() error {
	if d == nil || d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	return err
}

// Fd returns the raw descriptor (sayfa çevirme olaylarını okumak için).
func (d *Device) Fd() int { return int(d.f.Fd()) }

func (d *Device) ioctl(nr, size uintptr, arg unsafe.Pointer) error {
	return d.rawIoctl(iowr(nr, size), arg)
}

func (d *Device) rawIoctl(req uintptr, arg unsafe.Pointer) error {
	if d.f == nil {
		return ErrLost
	}
	for {
		_, _, e := unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), req, uintptr(arg))
		switch e {
		case 0:
			return nil
		case unix.EINTR, unix.EAGAIN:
			// drmIoctl'ün de yaptığı gibi: sinyal araya girdiyse tekrar.
			continue
		case unix.ENODEV:
			return ErrLost
		}
		return e
	}
}

// version reads the driver name (DRM_IOCTL_VERSION).
func (d *Device) version() (string, error) {
	var v drmVersion
	if err := d.ioctl(nrVersion, unsafe.Sizeof(v), unsafe.Pointer(&v)); err != nil {
		return "", err
	}
	if v.NameLen == 0 || v.NameLen > 256 {
		return "", nil
	}
	ad := make([]byte, v.NameLen)
	v.Name = uint64(uintptr(unsafe.Pointer(&ad[0])))
	v.DateLen, v.DescLen = 0, 0
	if err := d.ioctl(nrVersion, unsafe.Sizeof(v), unsafe.Pointer(&v)); err != nil {
		return "", err
	}
	runtime.KeepAlive(ad)
	return strings.TrimRight(string(ad), "\x00"), nil
}

// hasDumb reports DRM_CAP_DUMB_BUFFER: CPU ile yazılabilen tampon desteği.
func (d *Device) hasDumb() bool {
	c := getCap{Capability: capDumbBuffer}
	if err := d.ioctl(nrGetCap, unsafe.Sizeof(c), unsafe.Pointer(&c)); err != nil {
		return false
	}
	return c.Value != 0
}

// AcquireMaster makes this descriptor the display owner.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Eski SetMode master'ı alıp "defer" ile HEMEN bırakıyordu. Oysa SETCRTC,
// PAGE_FLIP ve DIRTYFB çekirdekte DRM_MASTER bayraklı (drm_ioctl.c:660,678,
// 679) ve master olmayan çağrı -EACCES alır (drm_ioctl.c:541-543). Sonuç:
// mod bir kez ayarlanıyor, sonra ne kare çevrilebiliyor ne de "yanlış modu
// geri al" (Restore) çalışıyordu — 15 saniyelik güvenlik ağı gerçek donanımda
// yoktu. Master artık panel yaşadıkça TUTULUYOR.
//
// Birincil düğümü ilk açan istemci zaten otomatik master olur
// (drm_master_open); bu çağrı başka bir istemci master'ı bıraktıysa geri
// almak için.
func (d *Device) AcquireMaster() error {
	if d.master {
		return nil
	}
	if err := d.rawIoctl(io(nrSetMaster), nil); err != nil {
		return fmt.Errorf("drm: ekran denetimi alınamadı (%v) — başka bir "+
			"program ekranı tutuyor olabilir", err)
	}
	d.master = true
	return nil
}

// ReleaseMaster gives the display back (VT değişimi, çıkış).
func (d *Device) ReleaseMaster() {
	if !d.master {
		return
	}
	_ = d.rawIoctl(io(nrDropMaster), nil)
	d.master = false
}

// ── Kaynak sorguları ────────────────────────────────────────────────────────

// resources returns the CRTC and connector ids.
//
// ── İKİ IOCTL, İKİ FARKLI DAVRANIŞ (karıştırmak sessiz hata üretir) ─────────
//
//	GETRESOURCES: çekirdek VERDİĞİN SAYIYA KADAR kopyalar ve gerçek sayıyı
//	              geri yazar. Az yer verirsen liste KIRPILIR, HATA DÖNMEZ.
//	GETCONNECTOR: HEPSİ-YA-HİÇBİRİ. Verdiğin sayı küçükse diziye HİÇ yazmaz.
//
// Güvenli kalıp ikisi için de aynı: önce her işaretçi VE her sayı sıfır, sonra
// ayır, sonra tekrar çağır — dönen sayı büyüdüyse (sıcak takma) yeniden dene.
// İlgilenmediğin dizinin hem İŞARETÇİSİ hem SAYISI sıfırlanmalı; yalnızca
// işaretçiyi sıfırlarsan çekirdek NULL'a copy_to_user dener.
func (d *Device) resources() (crtcs, conns []uint32, err error) {
	for deneme := 0; deneme < 4; deneme++ {
		var res cardRes
		boy := unsafe.Sizeof(res)
		if err := d.ioctl(nrGetResources, boy, unsafe.Pointer(&res)); err != nil {
			if err == ErrLost {
				return nil, nil, err
			}
			return nil, nil, fmt.Errorf("drm: kaynaklar okunamadı (%s sürücüsü "+
				"mod ayarlamayı desteklemiyor olabilir): %w", d.Driver, err)
		}
		if res.CountConns == 0 || res.CountCrtcs == 0 {
			return nil, nil, fmt.Errorf("drm: %s: bağlayıcı ya da crtc yok", d.Driver)
		}
		nc, nk := res.CountCrtcs, res.CountConns
		crtcs = make([]uint32, nc)
		conns = make([]uint32, nk)
		res.CrtcIDPtr = uint64(uintptr(unsafe.Pointer(&crtcs[0])))
		res.ConnIDPtr = uint64(uintptr(unsafe.Pointer(&conns[0])))
		res.FbIDPtr, res.EncIDPtr = 0, 0
		res.CountFbs, res.CountEncs = 0, 0
		if err := d.ioctl(nrGetResources, boy, unsafe.Pointer(&res)); err != nil {
			return nil, nil, fmt.Errorf("drm: kaynak listesi alınamadı: %w", err)
		}
		// Dilimler ioctl boyunca CANLI kalmalı: işaretçiyi uintptr'a çevirip
		// yapıya yazdığımız için çöp toplayıcı onları GÖRMÜYOR.
		runtime.KeepAlive(crtcs)
		runtime.KeepAlive(conns)
		if res.CountCrtcs > nc || res.CountConns > nk {
			continue // liste büyüdü: baştan
		}
		return crtcs[:res.CountCrtcs], conns[:res.CountConns], nil
	}
	return nil, nil, fmt.Errorf("drm: kaynak listesi durulmadı")
}

// connector is one output as the panel sees it.
type connector struct {
	ID, Type, TypeID uint32
	Connected        bool
	// EncoderID, şu an bağlı encoder (0 = sürülmüyor).
	EncoderID uint32
	Encoders  []uint32
	Modes     []Mode
}

// Name returns the kernel-style name ("HDMI-A-1").
func (c connector) Name() string { return ConnectorName(c.Type, c.TypeID) }

// connectorInfo reads one connector with its modes and encoders.
//
// DİKKAT: count_modes=0 ile yapılan ilk çağrı çekirdekte bir ALGILAMA
// (EDID okuma) tetikler ve birkaç on milisaniye sürebilir. Bu yüzden bu
// işlev kare döngüsünden değil, yalnızca açılışta ve mod değişiminde
// çağrılıyor.
func (d *Device) connectorInfo(id uint32) (connector, error) {
	for deneme := 0; deneme < 4; deneme++ {
		var c getConn
		boy := unsafe.Sizeof(c)
		c.ConnectorID = id
		if err := d.ioctl(nrGetConnector, boy, unsafe.Pointer(&c)); err != nil {
			return connector{}, err
		}
		out := connector{
			ID: id, Type: c.ConnectorType, TypeID: c.ConnectorTypeID,
			Connected: c.Connection == connectionConnected,
			EncoderID: c.EncoderID,
		}
		nm, ne := c.CountModes, c.CountEncoders
		if nm == 0 && ne == 0 {
			return out, nil
		}
		var ham []ModeInfo
		var encs []uint32
		c.ModesPtr, c.EncodersPtr, c.PropsPtr, c.PropValuesPtr = 0, 0, 0, 0
		c.CountProps = 0
		if nm > 0 {
			ham = make([]ModeInfo, nm)
			c.ModesPtr = uint64(uintptr(unsafe.Pointer(&ham[0])))
		}
		if ne > 0 {
			encs = make([]uint32, ne)
			c.EncodersPtr = uint64(uintptr(unsafe.Pointer(&encs[0])))
		}
		c.CountModes, c.CountEncoders = nm, ne
		if err := d.ioctl(nrGetConnector, boy, unsafe.Pointer(&c)); err != nil {
			return connector{}, err
		}
		runtime.KeepAlive(ham)
		runtime.KeepAlive(encs)
		if c.CountModes > nm || c.CountEncoders > ne {
			continue // hepsi-ya-hiçbiri: diziye yazılmadı, baştan
		}
		out.Encoders = encs[:c.CountEncoders]
		out.Modes = modeList(ham[:c.CountModes])
		out.EncoderID = c.EncoderID
		return out, nil
	}
	return connector{}, fmt.Errorf("drm: bağlayıcı %d durulmadı", id)
}

func (d *Device) encoderInfo(id uint32) (getEncoder, error) {
	e := getEncoder{EncoderID: id}
	err := d.ioctl(nrGetEncoder, unsafe.Sizeof(e), unsafe.Pointer(&e))
	return e, err
}

func (d *Device) getCrtc(id uint32) (modeCrtc, error) {
	c := modeCrtc{CrtcID: id}
	err := d.ioctl(nrGetCrtc, unsafe.Sizeof(c), unsafe.Pointer(&c))
	return c, err
}

// setCrtc programs a CRTC. fb=0 ve m=nil CRTC'yi KAPATIR (ekran uykusu).
func (d *Device) setCrtc(crtc, fb uint32, conns []uint32, m *ModeInfo) error {
	c := modeCrtc{CrtcID: crtc, FbID: fb}
	if m != nil && len(conns) > 0 {
		c.SetConnectorsPtr = uint64(uintptr(unsafe.Pointer(&conns[0])))
		c.CountConnectors = uint32(len(conns))
		c.ModeValid = 1
		c.Mode = *m
	}
	err := d.ioctl(nrSetCrtc, unsafe.Sizeof(c), unsafe.Pointer(&c))
	runtime.KeepAlive(conns)
	return err
}
