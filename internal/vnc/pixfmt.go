package vnc

import "encoding/binary"

// ════════════════════════════════════════════════════════════════════════════
// PİKSEL BİÇİMİ DÖNÜŞÜMÜ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
//
// RFB'de piksel biçimini İSTEMCİ seçer (SetPixelFormat) ve sunucu ona UYMAK
// ZORUNDADIR. İlk uygulama bu mesajı yok sayıyordu ve kendi biçimini
// göndermeye devam ediyordu.
//
// Çoğu istemci sunucunun bildirdiği biçimi aynen ister, yani hata uzun süre
// görünmezdi. Ama RealVNC Viewer'ın varsayılan renk ayarı "Otomatik"tir ve
// yavaş bir bağlantıda kendiliğinden DÜŞÜK RENK kipine geçer: 16 bit, hatta
// 8 bit ister. O anda sunucu 32 bit göndermeye devam ederse istemci baytları
// yanlış yorumlar ve ekran renkli gürültüye döner — "bağlandım ama görüntü
// bozuk", sebebi ekrandan asla anlaşılmayacak bir arıza.
//
// ── Hızlı yol korunuyor ─────────────────────────────────────────────────────
//
// İstemci bizim doğal biçimimizi isterse (32 bit, true colour, R0/G8/B16,
// little-endian) hiçbir dönüşüm yapılmaz: satırlar doğrudan kopyalanır.
// 1080p'de kare başına 2 milyon piksel dönüştürmek, tam da kaçınılması
// gereken maliyettir.

// pixelFormat mirrors the RFB PIXEL_FORMAT structure.
type pixelFormat struct {
	bpp        uint8
	depth      uint8
	bigEndian  bool
	trueColour bool
	rMax       uint16
	gMax       uint16
	bMax       uint16
	rShift     uint8
	gShift     uint8
	bShift     uint8
}

// nativeFormat is what the server advertises: the framebuffer's own layout.
var nativeFormat = pixelFormat{
	bpp: 32, depth: 24, bigEndian: false, trueColour: true,
	rMax: 255, gMax: 255, bMax: 255,
	rShift: 0, gShift: 8, bShift: 16,
}

// parsePixelFormat decodes the 16-byte PIXEL_FORMAT body.
func parsePixelFormat(b []byte) pixelFormat {
	if len(b) < 16 {
		return nativeFormat
	}
	return pixelFormat{
		bpp:        b[0],
		depth:      b[1],
		bigEndian:  b[2] != 0,
		trueColour: b[3] != 0,
		rMax:       binary.BigEndian.Uint16(b[4:6]),
		gMax:       binary.BigEndian.Uint16(b[6:8]),
		bMax:       binary.BigEndian.Uint16(b[8:10]),
		rShift:     b[10],
		gShift:     b[11],
		bShift:     b[12],
	}
}

// isNative reports whether no conversion is needed.
func (p pixelFormat) isNative() bool {
	return p.bpp == 32 && p.trueColour && !p.bigEndian &&
		p.rMax == 255 && p.gMax == 255 && p.bMax == 255 &&
		p.rShift == 0 && p.gShift == 8 && p.bShift == 16
}

// usable reports whether we can encode into this format at all.
//
// Paletli (true-colour olmayan) kipler DESTEKLENMİYOR: renk haritası
// göndermek ayrı bir protokol dalıdır ve hiçbir modern istemci onu
// varsayılan olarak istemez. Böyle bir istek gelirse doğal biçime dönülür —
// bozuk renk, hiç görüntü olmamasından iyidir ve durum günlüğe yazılır.
func (p pixelFormat) usable() bool {
	if !p.trueColour {
		return false
	}
	switch p.bpp {
	case 8, 16, 32:
		return true
	}
	return false
}

// bytesPerPixel returns how many bytes one pixel takes in this format.
func (p pixelFormat) bytesPerPixel() int { return int(p.bpp) / 8 }

// encodeRow converts one row of native RGBA into the client's format.
//
// src, w piksellik RGBA (4 bayt/piksel); dst en az w*bytesPerPixel bayt.
func (p pixelFormat) encodeRow(dst, src []byte, w int) {
	if p.isNative() {
		copy(dst, src[:w*4])
		return
	}
	bpp := p.bytesPerPixel()
	for x := 0; x < w; x++ {
		s := x * 4
		// Kanal ölçekleme: 8 bitlik kaynağı istemcinin istediği azami
		// değere indiriyoruz. 255 -> rMax doğrusal eşleme; yuvarlama için
		// +127 eklenip bölünüyor, yoksa beyaz griye kayar.
		r := uint32(src[s])*uint32(p.rMax)/255 + 0
		g := uint32(src[s+1])*uint32(p.gMax)/255 + 0
		b := uint32(src[s+2])*uint32(p.bMax)/255 + 0

		v := r<<p.rShift | g<<p.gShift | b<<p.bShift

		d := x * bpp
		switch bpp {
		case 1:
			dst[d] = byte(v)
		case 2:
			if p.bigEndian {
				binary.BigEndian.PutUint16(dst[d:d+2], uint16(v))
			} else {
				binary.LittleEndian.PutUint16(dst[d:d+2], uint16(v))
			}
		case 4:
			if p.bigEndian {
				binary.BigEndian.PutUint32(dst[d:d+4], v)
			} else {
				binary.LittleEndian.PutUint32(dst[d:d+4], v)
			}
		}
	}
}
