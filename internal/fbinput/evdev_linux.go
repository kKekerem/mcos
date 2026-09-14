//go:build linux

package fbinput

import (
	"os"
	"syscall"
	"unsafe"
)

// Bu dosya evdev aygıtlarının YETENEKLERİNİ sorar.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// /dev/input/event* altında her şey vardır: klavye, fare, touchpad, güç
// düğmesi, kapak sensörü, hatta PC hoparlörü. Hepsini "fare" sayıp okumak,
// klavyedeki her tuşu imleç hareketi sanmak demektir.
//
// Doğru yol, çekirdeğe SORMAKTIR: EVIOCGBIT ioctl'i aygıtın hangi olay
// türlerini ve hangi kodları üretebildiğini bit maskesi olarak verir. Bir
// aygıt REL_X + REL_Y üretiyorsa faredir; ABS_X + ABS_Y + BTN_TOUCH
// üretiyorsa touchpad/dokunmatik ekrandır.
//
// Alternatif (aygıt adına bakmak) güvenilmezdir: "SynPS/2 Synaptics
// TouchPad", "ELAN1200:00 04F3:3090 Touchpad", "MSFT0001:00 06CB:CE2D
// Touchpad" gibi onlarca biçim var ve üretici istediğini yazabilir.

// ioctl yön bitleri (asm-generic/ioctl.h).
const (
	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits

	iocRead = 2
)

// ioc builds an ioctl request number the same way the kernel macros do.
func ioc(dir, typ, nr, size uint32) uint32 {
	return dir<<iocDirShift | size<<iocSizeShift | typ<<iocTypeShift | nr<<iocNRShift
}

// evdev ioctl'leri (linux/input.h).
func eviocgbit(ev, length uint32) uint32 { return ioc(iocRead, 'E', 0x20+ev, length) }
func eviocgabs(abs uint32) uint32        { return ioc(iocRead, 'E', 0x40+abs, absInfoSize) }
func eviocgname(length uint32) uint32    { return ioc(iocRead, 'E', 0x06, length) }

// Olay türleri ve kodları (linux/input-event-codes.h).
const (
	evRel = 0x02
	evAbs = 0x03

	relX     = 0x00
	relY     = 0x01
	relWheel = 0x08
	relHWhl  = 0x06

	absX      = 0x00
	absY      = 0x01
	absMTPosX = 0x35
	absMTPosY = 0x36
	absMTSlot = 0x2F

	btnLeft      = 0x110
	btnRight     = 0x111
	btnMiddle    = 0x112
	btnTouch     = 0x14a
	btnToolFing  = 0x145
	btnToolDoubl = 0x14d
	btnToolTripl = 0x14e
)

// absInfoSize is sizeof(struct input_absinfo): 6 × __s32.
//
//	struct input_absinfo { __s32 value, minimum, maximum, fuzz, flat, resolution; };
const absInfoSize = 24

// absInfo is the decoded axis range.
type absInfo struct {
	Value, Min, Max, Fuzz, Flat, Resolution int32
}

// Kind classifies an input device.
type Kind int

const (
	// KindOther: klavye, güç düğmesi, sensör — imleç için ilgisiz.
	KindOther Kind = iota
	// KindMouse: bağıl hareket üreten aygıt (fare, trackpoint, trackball).
	KindMouse
	// KindTouchpad: mutlak konum + parmak algılama (dizüstü touchpad'i).
	KindTouchpad
	// KindTouchscreen: mutlak konum, parmak aracı YOK (dokunmatik ekran).
	//
	// Touchpad'den ayrı tutulur çünkü davranışı farklıdır: dokunmatik
	// ekranda parmağın DEĞDİĞİ yer imlecin gittiği yerdir; touchpad'de
	// parmağın HAREKETİ imleci göreli olarak iter.
	KindTouchscreen
)

// String returns a Turkish label for logs and the settings screen.
func (k Kind) String() string {
	switch k {
	case KindMouse:
		return "fare"
	case KindTouchpad:
		return "touchpad"
	case KindTouchscreen:
		return "dokunmatik ekran"
	}
	return "diğer"
}

// caps describes what a device can emit.
type caps struct {
	Kind    Kind
	Name    string
	RangeX  absInfo
	RangeY  absInfo
	HasMT   bool // ABS_MT_POSITION_* kullanır
	HasWhl  bool
	HasDbl  bool // BTN_TOOL_DOUBLETAP bildirir (iki parmak kaydırma mümkün)
	HasBtnL bool
}

// probe asks the kernel what a device can do.
func probe(f *os.File) caps {
	c := caps{Name: deviceName(f)}

	keyBits := bitmask(f, evKey, 0x300)
	relBits := bitmask(f, evRel, 0x10)
	absBits := bitmask(f, evAbs, 0x40)

	c.HasBtnL = testBit(keyBits, btnLeft)
	c.HasWhl = testBit(relBits, relWheel)
	c.HasDbl = testBit(keyBits, btnToolDoubl)

	hasRelXY := testBit(relBits, relX) && testBit(relBits, relY)
	hasAbsXY := testBit(absBits, absX) && testBit(absBits, absY)
	hasMTXY := testBit(absBits, absMTPosX) && testBit(absBits, absMTPosY)

	switch {
	case hasAbsXY || hasMTXY:
		// Parmak aracı bildiren mutlak aygıt = touchpad. Bildirmeyen =
		// dokunmatik ekran. (Grafik tabletleri BTN_TOOL_PEN bildirir ve
		// ikisine de girmez; imleç için touchpad gibi davranmaları yeterli.)
		if testBit(keyBits, btnToolFing) {
			c.Kind = KindTouchpad
		} else if testBit(keyBits, btnTouch) {
			c.Kind = KindTouchscreen
		} else {
			c.Kind = KindTouchpad
		}
		c.HasMT = hasMTXY
		if hasMTXY {
			c.RangeX = absRange(f, absMTPosX)
			c.RangeY = absRange(f, absMTPosY)
		}
		// MT aralığı boşsa (bazı sürücüler yalnızca ABS_X/Y doldurur)
		// klasik eksenlere düş.
		if c.RangeX.Max <= c.RangeX.Min {
			c.RangeX = absRange(f, absX)
			c.RangeY = absRange(f, absY)
			c.HasMT = false
		}
	case hasRelXY:
		c.Kind = KindMouse
	default:
		c.Kind = KindOther
	}
	return c
}

// bitmask reads one EVIOCGBIT capability bitmap.
//
// nbits kadar bit isteriz; çekirdek kaç bayt yazdığını döndürür. Hata
// durumunda boş dilim döner ve her test false olur — yani aygıt sessizce
// "hiçbir şey yapamaz" sayılır. Bu doğru geri düşüştür: yeteneklerini
// söyleyemeyen bir aygıtı fare sanmak, imleci rastgele oynatırdı.
func bitmask(f *os.File, ev, nbits uint32) []byte {
	buf := make([]byte, (nbits+7)/8)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(eviocgbit(ev, uint32(len(buf)))),
		uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil
	}
	return buf
}

// testBit reports whether bit n is set in a kernel bitmap.
func testBit(b []byte, n int) bool {
	i := n / 8
	if i < 0 || i >= len(b) {
		return false
	}
	return b[i]&(1<<uint(n%8)) != 0
}

// absRange reads one axis's min/max.
func absRange(f *os.File, axis uint32) absInfo {
	var raw [absInfoSize]byte
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(eviocgabs(axis)), uintptr(unsafe.Pointer(&raw[0])))
	if errno != 0 {
		return absInfo{}
	}
	return absInfo{
		Value:      int32(hostU32(raw[0:])),
		Min:        int32(hostU32(raw[4:])),
		Max:        int32(hostU32(raw[8:])),
		Fuzz:       int32(hostU32(raw[12:])),
		Flat:       int32(hostU32(raw[16:])),
		Resolution: int32(hostU32(raw[20:])),
	}
}

// deviceName reads the human-readable device name (for the settings screen).
func deviceName(f *os.File) string {
	buf := make([]byte, 128)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(eviocgname(uint32(len(buf)))),
		uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// hostU32 decodes a little-endian uint32 (amd64/arm64 are both LE).
func hostU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
