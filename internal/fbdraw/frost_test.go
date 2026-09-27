package fbdraw

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// DÜŞÜK ÇÖZÜNÜRLÜKLÜ BULANIKLIK: NE KADAR FARKLI, NE KADAR HIZLI?
// ════════════════════════════════════════════════════════════════════════════
//
// İddia: bulanıklık yüksek frekansları zaten sildiği için 1/2 ya da 1/4
// çözünürlükte hesaplayıp büyütmek gözle AYNI görüntüyü verir. Bu dosya o
// iddiayı SAYIYLA ölçer (en büyük piksel farkı ve PSNR) ve eşikleri ölçülen
// değerlere göre kilitler.

// panelBenzeri draws a worst-case-ish UI frame: flat surfaces, 1 px lines,
// text-like fine detail and a checkerboard (highest possible frequency).
func panelBenzeri(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	p := New(img)
	p.Fill(img.Bounds(), color.RGBA{15, 18, 22, 255})
	p.FillRoundRect(R(24, 24, float64(w)/5, float64(h)-72), 12.65, color.RGBA{22, 27, 33, 255})
	p.FillRoundRect(R(float64(w)/5+48, 24, float64(w)*0.72, float64(h)-72), 12.65, color.RGBA{22, 27, 33, 255})
	p.StrokeRoundRect(R(float64(w)/5+48, 24, float64(w)*0.72, float64(h)-72), 12.65, 2.5, color.RGBA{35, 169, 156, 255})
	rng := rand.New(rand.NewSource(7))
	// "Metin": ince, yüksek kontrastlı kısa çizgiler.
	for i := 0; i < 900; i++ {
		x := float64(rng.Intn(w - 40))
		y := float64(rng.Intn(h - 20))
		p.Fill(image.Rect(int(x), int(y), int(x)+2+rng.Intn(9), int(y)+2+rng.Intn(14)),
			color.RGBA{220, 227, 234, 255})
	}
	// Dama: en kötü durum (Nyquist'te enerji).
	for y := h / 2; y < h/2+120 && y < h; y++ {
		for x := w / 2; x < w/2+200 && x < w; x++ {
			if (x+y)%2 == 0 {
				o := img.PixOffset(x, y)
				img.Pix[o], img.Pix[o+1], img.Pix[o+2] = 255, 255, 255
			}
		}
	}
	return img
}

func farkOlc(a, b *image.RGBA) (enBuyuk int, psnr float64) {
	var se float64
	n := 0
	for i := range a.Pix {
		if i%4 == 3 {
			continue
		}
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > enBuyuk {
			enBuyuk = d
		}
		se += float64(d * d)
		n++
	}
	mse := se / float64(n)
	if mse == 0 {
		return enBuyuk, math.Inf(1)
	}
	return enBuyuk, 10 * math.Log10(255*255/mse)
}

// TestDusukCozunurlukBulaniklikTamCozunurlukleAyni — measured, not claimed.
//
// Perdede kullanılan yarıçap 1080p'de ~12 px (bkz. fbui ScrimRadius).
// ÖLÇÜLEN (1920x1080, yarıçap 12, panelBenzeri):
//
//	1/2: en büyük fark  9, PSNR 47,6 dB
//	1/4: en büyük fark 18, PSNR 41,4 dB
//	1/8: en büyük fark 37, PSNR 34,5 dB
//
// 40 dB üstü görüntü işlemede "ayırt edilemez" kabul edilir; en büyük fark
// da yalnızca dama deseninin (tasarımda hiç olmayan en kötü durum) kenarında
// çıkıyor. 1/8 gözle fark edilir hale geldiği için yalnızca kare bütçesi
// aşıldığında kullanılır (uyarlamalı kalite).
func TestDusukCozunurlukBulaniklikTamCozunurlukleAyni(t *testing.T) {
	const w, h, radius = 1920, 1080, 12
	src := panelBenzeri(w, h)
	ref := image.NewRGBA(src.Bounds())
	copy(ref.Pix, src.Pix)
	BlurScaled(ref, ref.Bounds(), radius, 1)

	for _, tc := range []struct {
		scale   int
		minPSNR float64
	}{{2, 45}, {4, 39}, {8, 32}} {
		got := image.NewRGBA(src.Bounds())
		copy(got.Pix, src.Pix)
		BlurScaled(got, got.Bounds(), radius, tc.scale)
		mx, p := farkOlc(ref, got)
		t.Logf("1/%d: en büyük fark %d, PSNR %.1f dB", tc.scale, mx, p)
		if p < tc.minPSNR {
			t.Errorf("1/%d: PSNR %.1f dB < %.0f — düşük çözünürlüklü bulanıklık "+
				"tam çözünürlüklüden gözle görülür biçimde ayrılıyor", tc.scale, p, tc.minPSNR)
		}
	}
}

func BenchmarkBlurScaled1080p(b *testing.B) {
	src := panelBenzeri(1920, 1080)
	img := image.NewRGBA(src.Bounds())
	for _, s := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("1-%d", s), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				copy(img.Pix, src.Pix)
				BlurScaled(img, img.Bounds(), 12, s)
			}
		})
	}
}

// ── Paralel karışımlar, tek çekirdeklilerle AYNI olmalı ─────────────────────

func crossFadeSeri(dst, from *image.RGBA, r image.Rectangle, t float64) {
	wNew := int(t*256 + 0.5)
	wOld := 256 - wNew
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := dst.PixOffset(r.Min.X, y)
		end := o + r.Dx()*4
		for i := o; i < end; i++ {
			dst.Pix[i] = uint8((int(dst.Pix[i])*wNew + int(from.Pix[i])*wOld) >> 8)
		}
	}
}

func rastgeleKare(w, h int, tohum int64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(tohum))
	rng.Read(img.Pix)
	return img
}

func TestCrossFadeParalelAyni(t *testing.T) {
	a := rastgeleKare(1280, 720, 1)
	b := image.NewRGBA(a.Bounds())
	copy(b.Pix, a.Pix)
	from := rastgeleKare(1280, 720, 2)
	r := image.Rect(100, 50, 1200, 700)
	CrossFade(a, from, r, 0.37)
	crossFadeSeri(b, from, r, 0.37)
	if mx, _ := farkOlc(a, b); mx != 0 {
		t.Fatalf("paralel CrossFade tek çekirdekliden %d farklı", mx)
	}
}

func slideSeri(dst, old *image.RGBA, r image.Rectangle, dx int, t float64) {
	w := r.Dx()
	row := make([]uint8, w*4)
	wNew := int(t*256 + 0.5)
	wOld := 256 - wNew
	oldDX := -dx / 3
	for y := r.Min.Y; y < r.Max.Y; y++ {
		base := dst.PixOffset(r.Min.X, y)
		for x := 0; x < w; x++ {
			for c := 0; c < 4; c++ {
				var nv, ov int
				if sn := x - dx; sn >= 0 && sn < w {
					nv = int(dst.Pix[base+sn*4+c])
				}
				if so := x - oldDX; so >= 0 && so < w {
					ov = int(old.Pix[base+so*4+c])
				}
				row[x*4+c] = uint8((nv*wNew + ov*wOld) >> 8)
			}
		}
		copy(dst.Pix[base:base+w*4], row)
	}
}

func TestSlideBlendParalelAyni(t *testing.T) {
	a := rastgeleKare(1280, 720, 3)
	b := image.NewRGBA(a.Bounds())
	copy(b.Pix, a.Pix)
	old := rastgeleKare(1280, 720, 4)
	r := image.Rect(300, 20, 1260, 700)
	SlideBlend(a, old, r, 57, 0.41)
	slideSeri(b, old, r, 57, 0.41)
	if mx, _ := farkOlc(a, b); mx != 0 {
		t.Fatalf("paralel SlideBlend tek çekirdekliden %d farklı", mx)
	}
}

// ── Cam dolgu ───────────────────────────────────────────────────────────────

// Düz renkli bir kaynaktan görüntü dolgusu, aynı renkte düz dolguyla AYNI
// olmalı: cam panel, opak panelin yerine geçerken şekli değişmemeli.
func TestGoruntuDolgusuDuzDolguylaAyni(t *testing.T) {
	renk := color.RGBA{22, 27, 33, 255}
	for _, tc := range []struct {
		ad  string
		rc  Rect
		rad float64
	}{
		{"hizli yol kesirli yaricap", Rect{X: 40, Y: 24, W: 500, H: 300}, 12.65},
		{"genel yol kesirli kutu", Rect{X: 40.5, Y: 24.25, W: 300, H: 200}, 9.3},
		{"kucuk", Rect{X: 10, Y: 10, W: 20, H: 18}, 6},
	} {
		a, pa, b, pb := iki(600, 400)
		arka := color.RGBA{200, 100, 50, 255}
		pa.Fill(a.Bounds(), arka)
		pb.Fill(b.Bounds(), arka)
		src := image.NewRGBA(a.Bounds())
		New(src).Fill(src.Bounds(), renk)

		pa.FillRoundRectImage(tc.rc, tc.rad, src)
		pb.FillRoundRect(tc.rc, tc.rad, renk)
		karsilastir(t, tc.ad, a, b, 1, 0.01)
	}
}

// ── Gren bantlaşmayı siliyor mu? ────────────────────────────────────────────

// enUzunDuzluk returns the longest run of identical R values along row y.
func enUzunDuzluk(img *image.RGBA, y int) int {
	best, run := 0, 1
	prev := -1
	for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
		v := int(img.Pix[img.PixOffset(x, y)])
		if v == prev {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
		prev = v
	}
	return best
}

// TestGrenBantlasmayiSiler — a slow dark gradient must not show steps.
//
// Koyu zeminde 1920 piksele yayılan 12 seviyelik bir gradyan, grensiz
// yuvarlanınca ~160 piksel genişliğinde düz BANTLAR olur; gözle basamak
// olarak görünür. Grenle aynı gradyan hiçbir yerde uzun düzlük bırakmamalı.
func TestGrenBantlasmayiSiler(t *testing.T) {
	var s Small
	s.ensure(2, 2)
	for i, v := range []uint8{14, 26, 14, 26} {
		o := i * 4
		s.Pix[o], s.Pix[o+1], s.Pix[o+2], s.Pix[o+3] = v, v, v, 255
	}
	olc := func(grain int) int {
		img := image.NewRGBA(image.Rect(0, 0, 1920, 64))
		s.Compose(img, img.Bounds(), ComposeOpts{Grain: grain})
		return enUzunDuzluk(img, 32)
	}
	duz, grenli := olc(0), olc(2)
	t.Logf("en uzun düz bant: grensiz %d px, grenli %d px", duz, grenli)
	if duz < 60 {
		t.Fatalf("test kurulumu hatalı: grensiz gradyanda bant yok (%d px)", duz)
	}
	if grenli > 12 {
		t.Errorf("grenli gradyanda %d piksellik düz bant — bantlaşma sürüyor", grenli)
	}
}

// TestGrenSabitDesen — the dither must not change between frames.
//
// Zamanla değişen gren, boştaki ekranda "kaynayan" bir görüntü demek olurdu
// ve her karede tam ekran hasar üretirdi.
func TestGrenSabitDesen(t *testing.T) {
	var s Small
	s.ensure(4, 4)
	for i := range s.Pix {
		s.Pix[i] = uint8(40 + i)
	}
	a := image.NewRGBA(image.Rect(0, 0, 400, 300))
	b := image.NewRGBA(a.Bounds())
	s.Compose(a, a.Bounds(), ComposeOpts{Grain: 3})
	s.Compose(b, b.Bounds(), ComposeOpts{Grain: 3})
	if mx, _ := farkOlc(a, b); mx != 0 {
		t.Fatalf("aynı katman iki kez büyütülünce %d farklı çıktı — gren zamana bağlı", mx)
	}
}

// TestComposeMerkezHizali — a single bright small pixel must land on its own
// block's centre, not shifted by half a block.
func TestComposeMerkezHizali(t *testing.T) {
	var s Small
	s.ensure(9, 9)
	for i := 3; i < len(s.Pix); i += 4 {
		s.Pix[i] = 255
	}
	o := (4*9 + 4) * 4
	s.Pix[o], s.Pix[o+1], s.Pix[o+2] = 255, 255, 255
	img := image.NewRGBA(image.Rect(0, 0, 72, 72)) // f = 8
	s.Compose(img, img.Bounds(), ComposeOpts{})
	// Blok 4'ün merkezi: 4*8+4 = 36 civarı (35,5).
	best, bx := -1, -1
	for x := 0; x < 72; x++ {
		if v := int(img.Pix[img.PixOffset(x, 36)]); v > best {
			best, bx = v, x
		}
	}
	if bx < 35 || bx > 36 {
		t.Errorf("tepe x=%d; 35-36 bekleniyordu — katman kaymış", bx)
	}
}

func BenchmarkComposeMix1080p(b *testing.B) {
	sharp := panelBenzeri(1920, 1080)
	dst := image.NewRGBA(sharp.Bounds())
	var s Small
	s.Downscale(sharp, sharp.Bounds(), 4)
	s.Blur(3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Compose(dst, dst.Bounds(), ComposeOpts{Sharp: sharp, Mix: 0.6,
			Tint: color.RGBA{15, 18, 22, 255}, TintAmt: 0.25, Grain: 1})
	}
}

func BenchmarkSoftLights1080p(b *testing.B) {
	dst := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	ls := []Light{{X: 0.2, Y: 0.3, R: 0.9, C: color.RGBA{35, 169, 156, 255}, A: 0.2},
		{X: 0.8, Y: 0.7, R: 0.8, C: color.RGBA{90, 60, 200, 255}, A: 0.12}}
	for i := 0; i < b.N; i++ {
		SoftLights(dst, dst.Bounds(), color.RGBA{15, 18, 22, 255}, ls, 2)
	}
}

// TestComposeUstSatirlarSiyahDegil: düz renkli bir katman HER satırda aynı
// renkte büyümeli. Önbellek -1 ile başlarken ilk satırlar siyaha karışıyordu
// (1080p arka planda ilk 4 satır 0,0,0 ölçüldü).
func TestComposeUstSatirlarSiyahDegil(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 160, 90))
	var s Small
	s.ensure(20, 12)
	for i := 0; i < len(s.Pix); i += 4 {
		s.Pix[i], s.Pix[i+1], s.Pix[i+2], s.Pix[i+3] = 30, 60, 90, 255
	}
	s.Compose(dst, dst.Bounds(), ComposeOpts{})
	for y := 0; y < 90; y++ {
		for _, x := range []int{0, 80, 159} {
			o := dst.PixOffset(x, y)
			if r, g, b := dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2]; r != 30 || g != 60 || b != 90 {
				t.Fatalf("(%d,%d) = %d,%d,%d; düz katman 30,60,90 olmalı", x, y, r, g, b)
			}
		}
	}
}
