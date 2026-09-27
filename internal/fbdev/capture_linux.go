//go:build linux

package fbdev

import "fmt"

// ════════════════════════════════════════════════════════════════════════════
// EKRAN YAKALAMA (VNC için)
// ════════════════════════════════════════════════════════════════════════════
//
// Flip'in TERSİ: ekranda ne varsa RGBA olarak okur. VNC sunucusu bunu kullanıp
// gerçek ekranı yayınlıyor — yani uzaktan bağlanan kullanıcı monitörde olanın
// AYNISINI görüyor: açılış animasyonu, sihirbaz, kilit ekranı, her şey.
//
// ── Neden ayrı bir okuma yolu ───────────────────────────────────────────────
//
// Panelin kendi tuvalini (image.RGBA) paylaşmak daha ucuz olurdu ama o tuval
// yalnızca PANELİN çizdiğini içerir. Açılış animasyonunu ayrı bir süreç
// (mcos-splash) çiziyor, kurtarma kabuğu hiç çizmiyor. Çerçeve arabelleğinin
// kendisini okumak, ekranda GERÇEKTEN ne varsa onu verir.
//
// ── Performans ──────────────────────────────────────────────────────────────
//
// mmap edilmiş bellekten okuma, piksel başına birkaç kaydırma ve bir yazma.
// 1080p'de ~2 milyon piksel; ölçülen ~6 ms (i7). VNC en fazla 25 kare/sn
// istediği için bu bütçeye rahat sığıyor. Okuma YALNIZCA bir istemci
// bağlıyken yapılır.

// Snapshot copies the current screen into dst as RGBA (4 bytes per pixel).
//
// dst, w*h*4 bayt olmalıdır (Size ile öğrenilir). Kısa bir dilim sessizce
// kırpılmaz: yarım bir kare, istemcide bozuk görüntü demektir ve sebebi
// aranırken saatler harcanır.
func (d *Device) Snapshot(dst []byte) error {
	if d.mem == nil {
		return fmt.Errorf("fbdev: aygıt kapalı")
	}
	w, h := d.Size()
	need := w * h * 4
	if len(dst) < need {
		return fmt.Errorf("fbdev: hedef tampon küçük (%d < %d)", len(dst), need)
	}

	rMask := uint32(1)<<d.vinf.Red.Length - 1
	gMask := uint32(1)<<d.vinf.Green.Length - 1
	bMask := uint32(1)<<d.vinf.Blue.Length - 1

	for y := 0; y < h; y++ {
		src := y * d.row
		out := y * w * 4
		switch d.bpp {
		case 32:
			for x := 0; x < w; x++ {
				o := src + x*4
				v := uint32(d.mem[o]) | uint32(d.mem[o+1])<<8 |
					uint32(d.mem[o+2])<<16 | uint32(d.mem[o+3])<<24
				p := out + x*4
				dst[p] = byte((v >> d.rShift) & rMask)
				dst[p+1] = byte((v >> d.gShift) & gMask)
				dst[p+2] = byte((v >> d.bShift) & bMask)
				dst[p+3] = 255
			}
		case 24:
			for x := 0; x < w; x++ {
				o := src + x*3
				v := uint32(d.mem[o]) | uint32(d.mem[o+1])<<8 |
					uint32(d.mem[o+2])<<16
				p := out + x*4
				dst[p] = byte((v >> d.rShift) & rMask)
				dst[p+1] = byte((v >> d.gShift) & gMask)
				dst[p+2] = byte((v >> d.bShift) & bMask)
				dst[p+3] = 255
			}
		case 16:
			// 16 bitte kanallar 5-6-5 bittir; 8 bite GERİ ÖLÇEKLENİR.
			// Kaydırmadan sonra sola kaydırmak (v<<3) en parlak değeri
			// 248'de bırakır ve ekran soluk görünür; bu yüzden üst bitler
			// alta kopyalanıyor (expand).
			for x := 0; x < w; x++ {
				o := src + x*2
				v := uint32(d.mem[o]) | uint32(d.mem[o+1])<<8
				p := out + x*4
				dst[p] = expand(byte((v>>d.rShift)&rMask), d.vinf.Red.Length)
				dst[p+1] = expand(byte((v>>d.gShift)&gMask), d.vinf.Green.Length)
				dst[p+2] = expand(byte((v>>d.bShift)&bMask), d.vinf.Blue.Length)
				dst[p+3] = 255
			}
		default:
			return fmt.Errorf("fbdev: desteklenmeyen derinlik: %d bit", d.bpp)
		}
	}
	return nil
}

// expand widens a `bits`-bit channel value to 8 bits without losing range.
//
// Örnek (5 bit): 31 -> 255 (0b11111 -> 0b11111111), 0 -> 0. Yalnızca sola
// kaydırmak 31'i 248'de bırakır ve beyaz griye kayar.
func expand(v byte, bits uint32) byte {
	switch {
	case bits == 0 || bits >= 8:
		return v
	default:
		out := uint32(v) << (8 - bits)
		out |= out >> bits
		return byte(out)
	}
}
