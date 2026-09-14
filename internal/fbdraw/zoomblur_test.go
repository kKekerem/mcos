package fbdraw

import (
	"image"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// AÇILIŞ GEÇİŞİNİN HIZLI YOLU
// ════════════════════════════════════════════════════════════════════════════
//
// ZoomBlurFade büyük bölgelerde yakınlaştırma ve bulanıklığı 1/f çözünürlükte
// yapar. Bu bir HIZ eniyilemesidir, ama görüntüyü değiştirmemelidir: aksi
// hâlde geçiş "ucuz" görünür.
//
// Aşağıdaki testler iki şeyi ayırır:
//   - hızlı yol tam çözünürlüklü yola YAKIN bir sonuç veriyor mu,
//   - hızlı yol sınır dışına taşıyor mu.

// gradient builds a smooth test image. Düz renk kullanmak anlamsız olurdu:
// her yol düz rengi doğru üretir; asıl soru ara değerlemenin doğruluğu.
func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := img.PixOffset(x, y)
			img.Pix[o] = uint8(x * 255 / w)
			img.Pix[o+1] = uint8(y * 255 / h)
			img.Pix[o+2] = uint8((x + y) * 255 / (w + h))
			img.Pix[o+3] = 255
		}
	}
	return img
}

// TestReducedPathMatchesFullPath — hızlı yol, yavaş yolla aynı görüntüyü
// vermeli.
//
// Tolerans: bulanık bir görüntüde kanal başına ortalama 6/255'lik sapma gözle
// seçilemez. Asıl yakalamak istediğimiz, yanlış eşleme (kayma, ters çevirme,
// yanlış merkez) — o tür bir hata ortalamayı onlarca birim kaydırır.
func TestReducedPathMatchesFullPath(t *testing.T) {
	const w, h = 640, 480
	b := image.Rect(0, 0, w, h)
	from := gradient(w, h)

	for _, tc := range []struct {
		scale  float64
		radius int
		t      float64
	}{
		{1.00, 8, 0.30},
		{1.18, 12, 0.50},
		{1.35, 16, 0.85},
		{0.80, 10, 0.40},
	} {
		fast := image.NewRGBA(b)
		slow := image.NewRGBA(b)
		scratch := image.NewRGBA(b)
		for i := range fast.Pix {
			fast.Pix[i] = 40
			slow.Pix[i] = 40
		}

		// Hızlı yol (ZoomBlurFade kendisi seçer).
		ZoomBlurFade(fast, from, scratch, b, tc.scale, tc.radius, tc.t)

		// Tam çözünürlüklü karşılık.
		Zoom(scratch, from, b, tc.scale)
		Blur(scratch, b, tc.radius)
		CrossFade(slow, scratch, b, tc.t)

		var sum, worst int
		for i := range fast.Pix {
			d := int(fast.Pix[i]) - int(slow.Pix[i])
			if d < 0 {
				d = -d
			}
			sum += d
			if d > worst {
				worst = d
			}
		}
		avg := float64(sum) / float64(len(fast.Pix))
		if avg > 6 {
			t.Errorf("ölçek=%.2f yarıçap=%d t=%.2f: ortalama sapma %.2f "+
				"(en kötü %d) — hızlı yol farklı bir görüntü üretiyor",
				tc.scale, tc.radius, tc.t, avg, worst)
		}
		t.Logf("ölçek=%.2f yarıçap=%d t=%.2f  ortalama sapma %.2f, en kötü %d",
			tc.scale, tc.radius, tc.t, avg, worst)
	}
}

// Hızlı yol yalnızca büyük bölgelerde ve yeterli bulanıklıkta devreye girmeli;
// küçük bir bölgede küçültmek blok blok görünürdü.
func TestReducedPathDeclinesSmallRegions(t *testing.T) {
	b := image.Rect(0, 0, 12, 12)
	dst := image.NewRGBA(b)
	from := gradient(12, 12)
	if zoomBlurFadeReduced(dst, from, b, 1.2, 16, 4, 0.5) {
		t.Error("12x12 bölgede küçültme kabul edildi — blok blok görünür")
	}
}

// Uç değerlerde taşma olmamalı. Panik, açılışta siyah ekran demektir.
func TestZoomBlurFadeExtremesDoNotPanic(t *testing.T) {
	sizes := []image.Rectangle{
		image.Rect(0, 0, 1, 1),
		image.Rect(0, 0, 3, 200),
		image.Rect(0, 0, 200, 3),
		image.Rect(0, 0, 65, 65),
		image.Rect(0, 0, 300, 300),
	}
	scales := []float64{-1, 0, 0.01, 0.5, 1, 1.35, 40}
	radii := []int{0, 1, 2, 4, 9, 64, 4096}
	ts := []float64{-0.5, 0, 0.5, 1, 2}

	for _, b := range sizes {
		dst := image.NewRGBA(b)
		from := image.NewRGBA(b)
		scratch := image.NewRGBA(b)
		for _, sc := range scales {
			for _, rad := range radii {
				for _, tt := range ts {
					ZoomBlurFade(dst, from, scratch, b, sc, rad, tt)
				}
			}
		}
	}
}

// Dikdörtgen 0,0'da başlamadığında da doğru yere yazmalı: ekranın bir
// bölümünü geçirirken kayma olursa görüntü yerinden oynar.
func TestZoomBlurFadeOffsetRect(t *testing.T) {
	b := image.Rect(0, 0, 400, 300)
	r := image.Rect(50, 40, 350, 260)
	dst := image.NewRGBA(b)
	from := gradient(400, 300)
	scratch := image.NewRGBA(b)

	// Dikdörtgen dışını işaretle; dokunulmamalı.
	const mark = 7
	for i := range dst.Pix {
		dst.Pix[i] = mark
	}
	ZoomBlurFade(dst, from, scratch, r, 1.2, 12, 0.5)

	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			if image.Pt(x, y).In(r) {
				continue
			}
			o := dst.PixOffset(x, y)
			if dst.Pix[o] != mark {
				t.Fatalf("dikdörtgen dışına yazıldı: (%d,%d)=%d", x, y, dst.Pix[o])
			}
		}
	}
}
