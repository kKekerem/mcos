package drm

import (
	"bytes"
	"image"
	"unsafe"
)

// Bu dosya PLATFORMDAN BAĞIMSIZ: satır kopyalama ve hasar takibi. Donanım
// gerektirmediği için her makinede sınanıyor.

// copyRows copies rows [y0, y1) of src into a scan-out buffer.
//
// direct=true: tampon XBGR8888 (bellekte R,G,B,x) — RGBA ile AYNI düzen,
// satır doğrudan kopyalanır. direct=false: XRGB8888 (bellekte B,G,R,x) — R ile
// B yer değiştirir. Dönüşüm 32 bitlik kelimelerle yapılıyor: bayt bayt
// yapmak 1080p'de kare başına 8 milyon ayrı yazma demekti (fbdev yolunda
// ölçülmüştü: 11,6 ms -> kelime işlemiyle 0,8 ms).
//
// Genişlik farklıysa (mod değişimi sırasında bir kare) ORTAK kısım kopyalanır;
// taşma yok.
func copyRows(dst []byte, pitch int, dstW, dstH int, src *image.RGBA, y0, y1 int, direct bool) {
	sb := src.Bounds()
	w := min(sb.Dx(), dstW)
	if y0 < 0 {
		y0 = 0
	}
	y1 = min(y1, sb.Dy(), dstH)
	if w <= 0 || y0 >= y1 {
		return
	}
	n := w * 4
	for y := y0; y < y1; y++ {
		so := y * src.Stride
		do := y * pitch
		if do+n > len(dst) || so+n > len(src.Pix) {
			return
		}
		s := src.Pix[so : so+n]
		d := dst[do : do+n]
		if direct {
			copy(d, s)
			continue
		}
		swizzleRow(d, s)
	}
}

// swizzleRow converts one RGBA row to XRGB8888 (bellekte B,G,R,x).
func swizzleRow(d, s []byte) {
	px := len(s) / 4
	if px == 0 {
		return
	}
	sw := unsafe.Slice((*uint32)(unsafe.Pointer(&s[0])), px)
	dw := unsafe.Slice((*uint32)(unsafe.Pointer(&d[0])), px)
	for i, p := range sw {
		// p (küçük-sonlu) = 0xAABBGGRR; istenen 0x00RRGGBB.
		dw[i] = (p>>16)&0xFF | p&0xFF00 | (p&0xFF)<<16
	}
}

// damage tracks which rows changed, separately for each of two buffers.
//
// ── Neden iki ayrı küme ─────────────────────────────────────────────────────
//
// Çift tamponda arka tampon İKİ kare önceki görüntüyü taşır. Onu güncel hâle
// getirmek için yalnızca SON karede değişen satırlar yetmez; bir önceki
// karede değişenler de gerekir (onlar diğer tampona yazılmıştı). fbdev
// yolundaki hasar takibi tek hedef tampon varsayıyordu; burada her tamponun
// kendi "henüz yazılmadı" kümesi var: bir satır değişince İKİ kümeye de
// işaretlenir, bir tampona yazılınca yalnızca onun kümesinden silinir.
type damage struct {
	prev   []byte
	stride int
	w, h   int
	dirty  [2][]bool
}

func newDamage(w, h int) *damage {
	d := &damage{stride: w * 4, w: w, h: h, prev: make([]byte, w*4*h)}
	d.dirty[0] = make([]bool, h)
	d.dirty[1] = make([]bool, h)
	d.markAll()
	return d
}

// markAll forces a full rewrite of both buffers (ilk kare, mod değişimi).
func (d *damage) markAll() {
	for i := range d.dirty {
		for y := range d.dirty[i] {
			d.dirty[i][y] = true
		}
	}
}

// observe compares img with the last seen frame and marks changed rows.
// Değişen satır sayısını döner.
func (d *damage) observe(img *image.RGBA) int {
	b := img.Bounds()
	if b.Dx() != d.w || b.Dy() != d.h {
		// Tuval boyutu tampondan farklı (mod değişiminin ortası): önceki
		// kopya anlamsız, her şeyi yeniden yaz.
		*d = *newDamage(b.Dx(), b.Dy())
		return d.h
	}
	n := 0
	for y := 0; y < d.h; y++ {
		s := img.Pix[y*img.Stride : y*img.Stride+d.stride]
		p := d.prev[y*d.stride : (y+1)*d.stride]
		if bytes.Equal(s, p) {
			continue
		}
		copy(p, s)
		d.dirty[0][y] = true
		d.dirty[1][y] = true
		n++
	}
	return n
}

// take returns the contiguous dirty row ranges of buffer i and clears them.
func (d *damage) take(i int) [][2]int {
	var out [][2]int
	ds := d.dirty[i]
	for y := 0; y < len(ds); {
		if !ds[y] {
			y++
			continue
		}
		y0 := y
		for y < len(ds) && ds[y] {
			ds[y] = false
			y++
		}
		out = append(out, [2]int{y0, y})
	}
	return out
}

// clearAll forgets pending work for both buffers (ikisi de güncel).
func (d *damage) clearAll() {
	for i := range d.dirty {
		clear(d.dirty[i])
	}
}
