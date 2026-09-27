//go:build linux

package vnc

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ════════════════════════════════════════════════════════════════════════════
// GİRDİ ENJEKSİYONU (uinput)
// ════════════════════════════════════════════════════════════════════════════
//
// VNC'den gelen tuş ve fare olayları SANAL BİR GİRDİ AYGITI üzerinden
// çekirdeğe yazılıyor. Panel onları kendi evdev okuyucusuyla, yerel klavye ve
// fareyle AYNI yoldan alıyor.
//
// ── Neden paneli doğrudan beslemiyoruz ──────────────────────────────────────
//
// Panele özel bir soket açmak daha kolay olurdu ama iki ayrı girdi yolu
// demekti: biri yerel evdev, biri uzak. İki yol zamanla ayrışır ve "VNC'de
// çalışıyor, klavyede çalışmıyor" (ya da tersi) türü hatalar üretir. Üstelik
// açılış animasyonu ve kurtarma kabuğu panelin soketini bilmez; uinput
// hepsinde çalışır.
//
// ── MUTLAK imleç ────────────────────────────────────────────────────────────
//
// VNC koordinatları MUTLAKTIR (istemci "imleç 640,360'ta" der). Bağıl bir
// fare taklidi, ivmelenme ve sınır etkileri yüzünden imleci kaydırırdı. Bu
// yüzden sanal aygıt mutlak eksenler (ABS_X/ABS_Y) + fare düğmeleri bildiriyor
// ama BTN_TOUCH bildirmiyor: bir parmak değil, konumu her an bilinen bir
// faredir.
//
// Burada eskiden "panelin fbinput katmanı mutlak aygıtları zaten tanıyor"
// yazıyordu ve YANLIŞTI: fbinput bu bit bileşimini touchpad sanıyor, touchpad
// çözücüsü de hareketi yalnızca temas sürerken işlediği için VNC'de imleç hiç
// kıpırdamıyordu (kullanıcı: "VNC ile fare imlecini kontrol edemiyoruz").
// fbinput artık bu aygıtı "hover" mutlak işaretçi sayıyor (bkz.
// internal/fbinput absPointerKind); o sınıflandırma ile bu aygıtın bitleri
// BİRLİKTE değişmeli.

const (
	uinputPath = "/dev/uinput"

	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetAbsBit  = 0x40045567
	uiSetRelBit  = 0x40045566

	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	synReport = 0

	absX = 0x00
	absY = 0x01

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
)

// uinputUserDev mirrors struct uinput_user_dev.
type uinputUserDev struct {
	Name      [80]byte
	ID        inputID
	FFEffects uint32
	AbsMax    [64]int32
	AbsMin    [64]int32
	AbsFuzz   [64]int32
	AbsFlat   [64]int32
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

// uinputInjector is an Injector backed by a virtual input device.
type uinputInjector struct {
	mu   sync.Mutex
	f    *os.File
	w, h int
	// buttons is the last pointer button mask, so we only send changes.
	buttons uint8
}

// NewInjector creates the virtual keyboard+pointer device.
//
// Ekran boyutu MUTLAK eksenlerin ölçeğini belirler: VNC koordinatları
// doğrudan piksel olduğu için aygıt 0..w-1 / 0..h-1 aralığını bildiriyor ve
// hiçbir ölçekleme yapılmıyor.
func NewInjector(w, h int) (Injector, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("uinput: geçersiz ekran boyutu %dx%d", w, h)
	}
	f, err := os.OpenFile(uinputPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		// Çekirdekte CONFIG_INPUT_UINPUT yoksa ya da aygıt yoksa VNC yine
		// çalışmalı — yalnızca İZLEME kipinde. Ekranı görebilmek, hiç
		// bağlanamamaktan iyidir.
		return nil, fmt.Errorf("uinput açılamadı: %w", err)
	}

	inj := &uinputInjector{f: f, w: w, h: h}
	if err := inj.setup(); err != nil {
		f.Close()
		return nil, err
	}
	return inj, nil
}

func (u *uinputInjector) setup() error {
	fd := u.f.Fd()

	for _, ev := range []uintptr{evKey, evRel, evAbs, evSyn} {
		if err := ioctl(fd, uiSetEvBit, ev); err != nil {
			return fmt.Errorf("uinput: olay türü açılamadı: %w", err)
		}
	}
	// Klavye: TÜM tuş kodları açılıyor (0..255). Tek tek seçmek, eşleme
	// tablosuna yeni bir tuş eklendiğinde burayı güncellemeyi unutmak
	// demekti — ve unutulan tuş sessizce çalışmazdı.
	for code := uintptr(1); code < 256; code++ {
		_ = ioctl(fd, uiSetKeyBit, code)
	}
	for _, b := range []uintptr{btnLeft, btnRight, btnMiddle} {
		if err := ioctl(fd, uiSetKeyBit, b); err != nil {
			return fmt.Errorf("uinput: düğme açılamadı: %w", err)
		}
	}
	// Tekerlek GERÇEK tekerlek olarak (REL_WHEEL). Burada eskiden
	// BTN_GEAR_DOWN/UP (0x150/0x151) "tuş taklidi" vardı ve yorum "panel
	// tekerleği düğme olarak da kabul ediyor" diyordu — doğru değildi:
	// fbinput o kodları hiç tanımıyor, VNC'de tekerlek sessizce ölüydü.
	// Yalnızca tekerlek eksenleri açılıyor, REL_X/REL_Y DEĞİL: onları da
	// açmak aygıtı "bağıl fare" gibi gösterir ve konumu mutlak olan VNC
	// imleci için yanlış sınıflandırmaya davetiye olurdu.
	for _, r := range []uintptr{relWheel, relHWheel} {
		if err := ioctl(fd, uiSetRelBit, r); err != nil {
			return fmt.Errorf("uinput: tekerlek ekseni açılamadı: %w", err)
		}
	}
	for _, a := range []uintptr{absX, absY} {
		if err := ioctl(fd, uiSetAbsBit, a); err != nil {
			return fmt.Errorf("uinput: eksen açılamadı: %w", err)
		}
	}

	var dev uinputUserDev
	copy(dev.Name[:], "MCOS VNC")
	dev.ID = inputID{Bustype: 0x06 /* BUS_VIRTUAL */, Vendor: 0x4d43, Product: 0x0001, Version: 1}
	dev.AbsMin[absX], dev.AbsMax[absX] = 0, int32(u.w-1)
	dev.AbsMin[absY], dev.AbsMax[absY] = 0, int32(u.h-1)

	buf := (*[unsafe.Sizeof(dev)]byte)(unsafe.Pointer(&dev))[:]
	if _, err := u.f.Write(buf); err != nil {
		return fmt.Errorf("uinput: aygıt tanımı yazılamadı: %w", err)
	}
	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		return fmt.Errorf("uinput: aygıt oluşturulamadı: %w", err)
	}
	// Çekirdeğin aygıtı yayınlaması ve panelin onu görmesi için kısa bir an.
	// Bu bekleme olmadan ilk tuş, aygıt henüz açılmamışken gidiyor ve
	// KAYBOLUYOR — kullanıcı "ilk tuşum yutuluyor" der.
	time.Sleep(120 * time.Millisecond)
	return nil
}

// Key implements Injector.
func (u *uinputInjector) Key(keysym uint32, down bool) error {
	code, ok := keysymToCode(keysym)
	if !ok {
		return nil // eşlenmemiş tuş sessizce yok sayılır
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	val := int32(0)
	if down {
		val = 1
	}
	if err := u.emit(evKey, uint16(code), val); err != nil {
		return err
	}
	return u.sync()
}

// Pointer implements Injector.
func (u *uinputInjector) Pointer(x, y int, buttons uint8) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= u.w {
		x = u.w - 1
	}
	if y >= u.h {
		y = u.h - 1
	}
	if err := u.emit(evAbs, absX, int32(x)); err != nil {
		return err
	}
	if err := u.emit(evAbs, absY, int32(y)); err != nil {
		return err
	}

	// Düğmeler: yalnızca DEĞİŞEN bitler gönderiliyor. Her olayda basılı
	// düğmeyi yeniden bildirmek, panelin tıklama sayacını şişirir.
	changed := u.buttons ^ buttons
	for i, code := range []uint16{btnLeft, btnMiddle, btnRight} {
		bit := uint8(1 << i)
		if changed&bit == 0 {
			continue
		}
		val := int32(0)
		if buttons&bit != 0 {
			val = 1
		}
		if err := u.emit(evKey, code, val); err != nil {
			return err
		}
	}

	// Tekerlek: RFB'de bir tekerlek adımı, düğme 4 (yukarı) / 5 (aşağı) /
	// 6 (sola) / 7 (sağa) için bir BASMA + BIRAKMA çiftidir. Adım BASILDIĞI
	// AN sayılır; bırakma yok sayılır (yoksa her adım iki kez sayılırdı).
	// REL_WHEEL işareti evdev geleneğidir: +1 = yukarı, -1 = aşağı.
	for _, w := range wheelSteps(u.buttons, buttons) {
		if err := u.emit(evRel, w.axis, w.val); err != nil {
			return err
		}
	}

	u.buttons = buttons
	return u.sync()
}

// Close implements Injector.
func (u *uinputInjector) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.f == nil {
		return nil
	}
	_ = ioctl(u.f.Fd(), uiDevDestroy, 0)
	err := u.f.Close()
	u.f = nil
	return err
}

func (u *uinputInjector) emit(typ, code uint16, val int32) error {
	ev := inputEvent{Type: typ, Code: code, Value: val}
	buf := (*[unsafe.Sizeof(ev)]byte)(unsafe.Pointer(&ev))[:]
	_, err := u.f.Write(buf)
	return err
}

func (u *uinputInjector) sync() error {
	return u.emit(evSyn, synReport, 0)
}

func ioctl(fd uintptr, req uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return errors.New(errno.Error())
	}
	return nil
}
