//go:build linux

package fbdev

import (
	"fmt"
	"image"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Framebuffer ioctl numbers and enums, taken verbatim from <linux/fb.h>
// (doğrulandı: /usr/include/linux/fb.h satır 14, 16, 37, 63).
const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602

	fbTypePackedPixels = 0
	fbVisualTrueColor  = 2
)

// varScreenInfo mirrors struct fb_var_screeninfo (<linux/fb.h>).
//
// Tüm alanlar __u32 ve fb_bitfield (3 × __u32) olduğu için C ve Go yerleşimi
// birebir aynıdır; elle dolgu gerekmez. Boyut 160 bayt — device_test.go bunu
// derleme zamanına yakın bir testle sabitler.
type varScreenInfo struct {
	XRes, YRes               uint32
	XResVirtual, YResVirtual uint32
	XOffset, YOffset         uint32

	BitsPerPixel uint32
	Grayscale    uint32

	Red, Green, Blue, Transp bitfield

	NonStd      uint32
	Activate    uint32
	Height      uint32
	Width       uint32
	AccelFlags  uint32
	PixClock    uint32
	LeftMargin  uint32
	RightMargin uint32
	UpperMargin uint32
	LowerMargin uint32
	HSyncLen    uint32
	VSyncLen    uint32
	Sync        uint32
	VMode       uint32
	Rotate      uint32
	Colorspace  uint32
	Reserved    [4]uint32
}

// bitfield mirrors struct fb_bitfield: bir renk bileşeninin piksel içindeki
// bit konumu. 32bpp'de kanal sırası buradan okunur — XRGB ve BGRX kartları
// karıştırmamak için sabit varsayım YAPILMAZ.
type bitfield struct {
	Offset, Length, MSBRight uint32
}

// fixScreenInfo mirrors struct fb_fix_screeninfo (<linux/fb.h>).
//
// Bu yapıda `unsigned long` alanlar var (amd64'te 8 bayt), bu yüzden C
// derleyicisi hizalama dolgusu ekler. Go'nun doğal hizalama kuralları AYNI
// dolguyu üretir (uint64 → 8, uint32 → 4), dolayısıyla elle padding alanı
// gerekmez. Toplam 80 bayt — device_test.go doğrular.
type fixScreenInfo struct {
	ID         [16]byte
	SmemStart  uint64
	SmemLen    uint32
	Type       uint32
	TypeAux    uint32
	Visual     uint32
	XPanStep   uint16
	YPanStep   uint16
	YWrapStep  uint16
	LineLength uint32 // Go burada 2 bayt dolgu ekler — C ile aynı
	MmioStart  uint64 // Go burada 4 bayt dolgu ekler — C ile aynı
	MmioLen    uint32
	Accel      uint32
	Caps       uint16
	Reserved   [2]uint16
}

// Device is an open, memory-mapped Linux framebuffer.
type Device struct {
	f    *os.File
	mem  []byte
	vinf varScreenInfo
	finf fixScreenInfo

	// row, tek satırın bayt uzunluğu. xres*bpp/8 DEĞİL: sürücüler satır
	// sonuna dolgu ekleyebilir (stride). Yanlış kullanılırsa görüntü kayar.
	row int
	bpp int

	// Kanal kaydırmaları, vinf bitfield'lerinden çözülür.
	rShift, gShift, bShift uint
}

// Open maps a framebuffer device. Boş yol "/dev/fb0" anlamına gelir.
func Open(path string) (*Device, error) {
	if path == "" {
		path = "/dev/fb0"
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("fbdev: %s açılamadı: %w", path, err)
	}

	d := &Device{f: f}
	if err := ioctlPtr(f.Fd(), fbioGetVScreenInfo, unsafe.Pointer(&d.vinf)); err != nil {
		f.Close()
		return nil, fmt.Errorf("fbdev: FBIOGET_VSCREENINFO: %w", err)
	}
	if err := ioctlPtr(f.Fd(), fbioGetFScreenInfo, unsafe.Pointer(&d.finf)); err != nil {
		f.Close()
		return nil, fmt.Errorf("fbdev: FBIOGET_FSCREENINFO: %w", err)
	}

	d.bpp = int(d.vinf.BitsPerPixel)
	d.row = int(d.finf.LineLength)
	if d.row == 0 {
		// Sürücü stride bildirmediyse en makul tahmin.
		d.row = int(d.vinf.XResVirtual) * d.bpp / 8
	}

	if err := d.check(); err != nil {
		f.Close()
		return nil, err
	}

	// Kanal kaydırmalarını sürücünün bildirdiği bitfield'lerden al.
	d.rShift = uint(d.vinf.Red.Offset)
	d.gShift = uint(d.vinf.Green.Offset)
	d.bShift = uint(d.vinf.Blue.Offset)

	size := d.row * int(d.vinf.YResVirtual)
	if n := int(d.finf.SmemLen); n > 0 && n < size {
		size = n
	}
	mem, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("fbdev: mmap (%d bayt): %w", size, err)
	}
	d.mem = mem
	return d, nil
}

// check rejects framebuffer formats we cannot render, with a message that says
// what was found. Sessizce bozuk çizmekten iyidir — mcos-launch bu hatayı
// görüp fbterm yoluna düşer.
func (d *Device) check() error {
	if d.finf.Type != fbTypePackedPixels {
		return fmt.Errorf("fbdev: desteklenmeyen framebuffer türü %d (yalnızca packed pixels)", d.finf.Type)
	}
	if d.finf.Visual != fbVisualTrueColor {
		return fmt.Errorf("fbdev: desteklenmeyen görsel mod %d (yalnızca truecolor)", d.finf.Visual)
	}
	switch d.bpp {
	case 16, 24, 32:
	default:
		return fmt.Errorf("fbdev: desteklenmeyen renk derinliği %d bpp (16/24/32 bekleniyor)", d.bpp)
	}
	if d.vinf.XRes == 0 || d.vinf.YRes == 0 {
		return fmt.Errorf("fbdev: geçersiz çözünürlük %dx%d", d.vinf.XRes, d.vinf.YRes)
	}
	return nil
}

// Size returns the visible resolution in pixels.
func (d *Device) Size() (w, h int) { return int(d.vinf.XRes), int(d.vinf.YRes) }

// BitsPerPixel returns the framebuffer colour depth.
func (d *Device) BitsPerPixel() int { return d.bpp }

// Info returns a one-line human-readable description, for the boot log.
func (d *Device) Info() string {
	return fmt.Sprintf("%dx%d %dbpp stride=%d r<<%d g<<%d b<<%d",
		d.vinf.XRes, d.vinf.YRes, d.bpp, d.row, d.rShift, d.gShift, d.bShift)
}

// Flip converts the RGBA canvas to the framebuffer's pixel format and writes it
// to video memory. Kanvas ekrandan büyükse kırpılır, küçükse kalan alana
// dokunulmaz.
func (d *Device) Flip(img *image.RGBA) error {
	if d.mem == nil {
		return fmt.Errorf("fbdev: aygıt kapalı")
	}
	sw, sh := d.Size()
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > sw {
		w = sw
	}
	if h > sh {
		h = sh
	}

	for y := 0; y < h; y++ {
		src := img.PixOffset(b.Min.X, b.Min.Y+y)
		dst := y * d.row
		switch d.bpp {
		case 32:
			for x := 0; x < w; x++ {
				s := src + x*4
				v := uint32(img.Pix[s])<<d.rShift |
					uint32(img.Pix[s+1])<<d.gShift |
					uint32(img.Pix[s+2])<<d.bShift
				o := dst + x*4
				d.mem[o] = byte(v)
				d.mem[o+1] = byte(v >> 8)
				d.mem[o+2] = byte(v >> 16)
				d.mem[o+3] = byte(v >> 24)
			}
		case 24:
			for x := 0; x < w; x++ {
				s := src + x*4
				v := uint32(img.Pix[s])<<d.rShift |
					uint32(img.Pix[s+1])<<d.gShift |
					uint32(img.Pix[s+2])<<d.bShift
				o := dst + x*3
				d.mem[o] = byte(v)
				d.mem[o+1] = byte(v >> 8)
				d.mem[o+2] = byte(v >> 16)
			}
		case 16:
			// Genellikle RGB565; kaydırmalar sürücüden geldiği için 555 gibi
			// varyantlar da doğru çalışır.
			rl, gl, bl := d.vinf.Red.Length, d.vinf.Green.Length, d.vinf.Blue.Length
			for x := 0; x < w; x++ {
				s := src + x*4
				v := uint16(scale8(img.Pix[s], rl))<<d.rShift |
					uint16(scale8(img.Pix[s+1], gl))<<d.gShift |
					uint16(scale8(img.Pix[s+2], bl))<<d.bShift
				o := dst + x*2
				d.mem[o] = byte(v)
				d.mem[o+1] = byte(v >> 8)
			}
		}
	}
	return nil
}

// scale8 reduces an 8-bit channel to `bits` bits.
func scale8(v byte, bits uint32) uint32 {
	if bits == 0 || bits >= 8 {
		return uint32(v)
	}
	return uint32(v) >> (8 - bits)
}

// Close unmaps video memory and closes the device.
func (d *Device) Close() error {
	var err error
	if d.mem != nil {
		err = unix.Munmap(d.mem)
		d.mem = nil
	}
	if d.f != nil {
		if cerr := d.f.Close(); err == nil {
			err = cerr
		}
		d.f = nil
	}
	return err
}

// ioctlPtr issues an ioctl whose argument is a pointer to a struct.
//
// golang.org/x/sys/unix yalnızca sabit tipler için sarmalayıcı sunuyor
// (IoctlGetInt vb.); framebuffer yapıları için doğrudan syscall gerekiyor.
// CGO kullanılmaz — çapraz derleme CGO_ENABLED=0 ile yapılıyor.
func ioctlPtr(fd uintptr, req uint, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
