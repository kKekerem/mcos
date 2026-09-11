//go:build linux

package fbdev

import (
	"testing"
	"unsafe"
)

// TestStructLayout, ioctl yapılarının C yerleşimiyle birebir aynı olduğunu
// sabitler. Bu test OLMADAN bir alan kayması sessizce yanlış çözünürlük veya
// yanlış stride okunmasına, yani eğri/kayık bir ekrana yol açar — ve sebebi
// çok zor bulunur.
//
// Beklenen değerler <linux/fb.h> üzerinden elle hesaplandı:
//
//	fb_var_screeninfo : 40 × __u32 (4 fb_bitfield = 12 × __u32 dahil) = 160
//	fb_fix_screeninfo : hizalama dolgusuyla 80
func TestStructLayout(t *testing.T) {
	if got := unsafe.Sizeof(varScreenInfo{}); got != 160 {
		t.Errorf("sizeof(fb_var_screeninfo) = %d, 160 olmalı", got)
	}
	if got := unsafe.Sizeof(fixScreenInfo{}); got != 80 {
		t.Errorf("sizeof(fb_fix_screeninfo) = %d, 80 olmalı", got)
	}
	if got := unsafe.Sizeof(bitfield{}); got != 12 {
		t.Errorf("sizeof(fb_bitfield) = %d, 12 olmalı", got)
	}

	// Kritik alan konumları: yanlış olursa çözünürlük/stride yanlış okunur.
	var v varScreenInfo
	base := uintptr(unsafe.Pointer(&v))
	checks := []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"XRes", uintptr(unsafe.Pointer(&v.XRes)) - base, 0},
		{"YRes", uintptr(unsafe.Pointer(&v.YRes)) - base, 4},
		{"XResVirtual", uintptr(unsafe.Pointer(&v.XResVirtual)) - base, 8},
		{"YResVirtual", uintptr(unsafe.Pointer(&v.YResVirtual)) - base, 12},
		{"BitsPerPixel", uintptr(unsafe.Pointer(&v.BitsPerPixel)) - base, 24},
		{"Red", uintptr(unsafe.Pointer(&v.Red)) - base, 32},
		{"Green", uintptr(unsafe.Pointer(&v.Green)) - base, 44},
		{"Blue", uintptr(unsafe.Pointer(&v.Blue)) - base, 56},
		{"Transp", uintptr(unsafe.Pointer(&v.Transp)) - base, 68},
	}
	for _, c := range checks {
		if c.off != c.want {
			t.Errorf("fb_var_screeninfo.%s ofseti = %d, %d olmalı", c.name, c.off, c.want)
		}
	}

	var f fixScreenInfo
	fbase := uintptr(unsafe.Pointer(&f))
	fchecks := []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"SmemStart", uintptr(unsafe.Pointer(&f.SmemStart)) - fbase, 16},
		{"SmemLen", uintptr(unsafe.Pointer(&f.SmemLen)) - fbase, 24},
		{"Type", uintptr(unsafe.Pointer(&f.Type)) - fbase, 28},
		{"Visual", uintptr(unsafe.Pointer(&f.Visual)) - fbase, 36},
		// LineLength 46'da değil 48'de: derleyici 2 bayt dolgu ekler.
		{"LineLength", uintptr(unsafe.Pointer(&f.LineLength)) - fbase, 48},
		// MmioStart 52'de değil 56'da: uint64 8'e hizalanır.
		{"MmioStart", uintptr(unsafe.Pointer(&f.MmioStart)) - fbase, 56},
	}
	for _, c := range fchecks {
		if c.off != c.want {
			t.Errorf("fb_fix_screeninfo.%s ofseti = %d, %d olmalı", c.name, c.off, c.want)
		}
	}
}

// TestOpenMissingDeviceFails: var olmayan aygıt sessizce başarılı olmamalı.
func TestOpenMissingDeviceFails(t *testing.T) {
	if d, err := Open("/dev/definitely-not-a-framebuffer"); err == nil {
		d.Close()
		t.Error("var olmayan aygıt için hata beklenmişti")
	}
}
