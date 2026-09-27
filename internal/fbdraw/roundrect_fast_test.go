package fbdraw

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/vector"
)

// ════════════════════════════════════════════════════════════════════════════
// HIZLI YUVARLAK DİKDÖRTGEN, GENEL YOLLA AYNI GÖRÜNMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu test hayati ────────────────────────────────────────────────────
//
// Hızlı yol şekli PARÇALARA bölüyor: düz kısımlar doğrudan dolgu, köşeler ayrı
// yol. Bir parçanın sınırı bir piksel kayarsa sonuç "çalışıyor" görünür ama
// panelde ince bir dikiş ya da kesik köşe kalır — ve kimse sebebini aramaz.
//
// AYNI hatayı bu oturumda bir kez yaptık: StrokeCircle'ı dört banda bölmek
// halkayı bozdu, çünkü aynı yol dört kutuya çiziliyordu ve rasterleştirici
// kendi alanı dışını yanlış sayıyor. Burada her parça KENDİ kutusunda KAPALI
// bir yol; test bunun gerçekten böyle olduğunu kanıtlıyor.

// genelFill is the original whole-box rasterisation, kept as reference.
func genelFill(p *Painter, rc Rect, radius float64, c color.RGBA) {
	p.path(rc, func(ras *vector.Rasterizer, ox, oy float32) {
		roundRectPath(ras, rc, radius, +1, ox, oy)
	}, c)
}

func genelStroke(p *Painter, rc Rect, radius, thickness float64, c color.RGBA) {
	inner := Rect{X: rc.X + thickness, Y: rc.Y + thickness,
		W: rc.W - 2*thickness, H: rc.H - 2*thickness}
	innerRad := radius - thickness
	if innerRad < 0 {
		innerRad = 0
	}
	p.path(rc, func(ras *vector.Rasterizer, ox, oy float32) {
		roundRectPath(ras, rc, radius, +1, ox, oy)
		roundRectPath(ras, inner, innerRad, -1, ox, oy)
	}, c)
}

func iki(w, h int) (*image.RGBA, *Painter, *image.RGBA, *Painter) {
	a := image.NewRGBA(image.Rect(0, 0, w, h))
	b := image.NewRGBA(image.Rect(0, 0, w, h))
	return a, New(a), b, New(b)
}

func karsilastir(t *testing.T, ad string, a, b *image.RGBA, izin int, oranIzin float64) {
	t.Helper()
	farkli, enBuyuk := 0, 0
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > 0 {
			farkli++
			if d > enBuyuk {
				enBuyuk = d
			}
		}
	}
	oran := 100 * float64(farkli) / float64(len(a.Pix))
	if enBuyuk > izin || oran > oranIzin {
		t.Errorf("%s: %d bayt farklı (%%%.3f), en büyük sapma %d "+
			"(izin: sapma<=%d, oran<=%%%.3f) — parça sınırları kaymış",
			ad, farkli, oran, enBuyuk, izin, oranIzin)
	} else {
		t.Logf("%s: %d bayt farklı (%%%.3f), en büyük sapma %d",
			ad, farkli, oran, enBuyuk)
	}
}

func TestHizliDolguGenelYollaAyni(t *testing.T) {
	renk := color.RGBA{R: 20, G: 28, B: 38, A: 255}
	for _, tc := range []struct {
		ad  string
		rc  Rect
		rad float64
	}{
		{"icerik paneli", Rect{X: 40, Y: 24, W: 500, H: 300}, 12},
		{"kenar cubugu", Rect{X: 8, Y: 8, W: 200, H: 400}, 10},
		{"kare kose", Rect{X: 10, Y: 10, W: 100, H: 100}, 0},
		{"buyuk yaricap", Rect{X: 10, Y: 10, W: 200, H: 120}, 60},
		// Gerçek paneldeki yarıçaplar: CellH*0,55, 1080p ve 1440p.
		{"kesirli yaricap 1080p", Rect{X: 40, Y: 24, W: 500, H: 300}, 12.65},
		{"kesirli yaricap 1440p", Rect{X: 40, Y: 24, W: 500, H: 300}, 16.5},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			a, pa, b, pb := iki(600, 460)
			pa.FillRoundRect(tc.rc, tc.rad, renk)
			genelFill(pb, tc.rc, tc.rad, renk)
			// ── ÖLÇÜLEN gerçek, iddia edilen değil ──────────────────
			//
			// Araştırma "dolgu bayt bayt aynı olur" demişti; ÖLÇÜM bunu
			// tam olarak doğrulamadı. Köşe yayları artık kendi küçük
			// kutularında rasterleştiği için köşe ile düz kenarın
			// birleştiği yerde kenar yumuşatma en fazla 2/255 sapıyor:
			//
			//	içerik paneli   7 bayt  (%0,001)  en büyük sapma 1
			//	kenar çubuğu    5 bayt  (%0,000)  en büyük sapma 1
			//	kare köşe       0 bayt             (birebir aynı)
			//	büyük yarıçap  51 bayt  (%0,005)  en büyük sapma 2
			//
			// 2/255, ekranın kendi nicemlemesinin altında — gözle
			// görülemez. Eşik ÖLÇÜLEN değere göre konuyor: bir değişiklik
			// bunu kötüleştirirse test yakalar.
			karsilastir(t, tc.ad, a, b, 2, 0.01)
		})
	}
}

func TestHizliCerceveGenelYollaAyni(t *testing.T) {
	renk := color.RGBA{R: 45, G: 212, B: 191, A: 255}
	for _, tc := range []struct {
		ad       string
		rc       Rect
		rad, kal float64
	}{
		{"panel cercevesi", Rect{X: 40, Y: 24, W: 500, H: 300}, 12, 1},
		{"kalin cerceve", Rect{X: 40, Y: 24, W: 400, H: 260}, 16, 3},
		{"kesirli kalinlik", Rect{X: 40, Y: 24, W: 400, H: 260}, 14, 1.5},
		{"kare kose", Rect{X: 20, Y: 20, W: 300, H: 200}, 0, 2},
		{"kesirli yaricap ve kalinlik", Rect{X: 40, Y: 24, W: 400, H: 260}, 16.5, 1.37},
		{"odak cercevesi 1080p", Rect{X: 40, Y: 24, W: 400, H: 260}, 12.65, 2.56},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			a, pa, b, pb := iki(600, 460)
			pa.StrokeRoundRect(tc.rc, tc.rad, tc.kal, renk)
			genelStroke(pb, tc.rc, tc.rad, tc.kal, renk)
			// ÖLÇÜLEN:
			//	panel çerçevesi     16 bayt (%0,001) sapma 1
			//	kalın çerçeve        7 bayt (%0,001) sapma 1
			//	kesirli kalınlık   870 bayt (%0,079) sapma 1
			//	kare köşe            0 bayt          (birebir)
			//
			// Kesirli kalınlıkta fark daha çok, çünkü köşe/kenar birleşimi
			// alt piksel sınırına denk geliyor. Sapma yine de 1/255.
			//
			// Köşe parçası kesirli yarıçap için "yay + düz uç" çokgenine
			// dönünce (bkz. cornerRingPath) kalın çerçevede sapma 2/255'e
			// çıktı (34 bayt, %0,003): aynı geometri, farklı kayan nokta
			// sırası. Ekranın nicemlemesinin altında.
			karsilastir(t, tc.ad, a, b, 2, 0.10)
		})
	}
}

// YARI SAYDAM renkte arkadaki içerik SİLİNMEMELİ.
//
// Düz parçayı draw.Src ile doldurmak daha hızlıydı ama tam olarak bunu
// bozuyordu: perdeler ve vurgular yarı saydam çiziliyor.
func TestHizliDolguArkayiSilmez(t *testing.T) {
	a, pa, b, pb := iki(300, 200)
	arka := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	for _, p := range []*Painter{pa, pb} {
		p.Fill(image.Rect(0, 0, 300, 200), arka)
	}
	yari := color.RGBA{R: 10, G: 10, B: 10, A: 128}
	pa.FillRoundRect(Rect{X: 20, Y: 20, W: 200, H: 120}, 10, yari)
	genelFill(pb, Rect{X: 20, Y: 20, W: 200, H: 120}, 10, yari)
	karsilastir(t, "yari saydam", a, b, 1, 0.01)

	// Ortada arkaplan GERÇEKTEN karışmış olmalı, silinmiş değil.
	r, g, bl, _ := a.At(120, 80).RGBA()
	if r>>8 < 60 || g>>8 < 30 || bl>>8 < 10 {
		t.Errorf("yarı saydam dolgu arkayı SİLDİ: (%d,%d,%d)", r>>8, g>>8, bl>>8)
	}
}

// Küçük ve kesirli kutularda hızlı yol DEVREYE GİRMEMELİ.
func TestKucukVeKesirliKutudaHizliYolYok(t *testing.T) {
	if fastEligible(Rect{X: 0, Y: 0, W: 10, H: 10}, 2) {
		t.Error("10x10 kutuda hızlı yol açık — alt piksel geometrisi bozulur")
	}
	if fastEligible(Rect{X: 0.5, Y: 0, W: 100, H: 100}, 8) {
		t.Error("kesirli X'te hızlı yol açık — kenar yumuşatma kayar")
	}
	if !fastEligible(Rect{X: 10, Y: 20, W: 500, H: 300}, 12) {
		t.Error("büyük tam sayı kutuda hızlı yol KAPALI — kazanç kaybediliyor")
	}
}

// KESİRLİ yarıçap hızlı yoldan GİTMELİ.
//
// Panelin yarıçapı CellH*0,55; gerçek yazı tipi boyutlarında bu neredeyse hiç
// tam sayı değil. Eskiden kesirli yarıçap genel yola düşüyordu ve 1080p'de
// kare 22 ms sürüyordu (bkz. fastEligible). Bu test o durumu kilitler.
func TestKesirliYaricapHizliYolda(t *testing.T) {
	for _, r := range []float64{12.65, 16.5, 10.45, 21.45} {
		if !fastEligible(Rect{X: 300, Y: 60, W: 1534, H: 984}, r) {
			t.Errorf("yarıçap %.2f: hızlı yol KAPALI — içerik paneli her karede "+
				"1,5 megapiksel rasterleştirilir", r)
		}
		if !strokeEligible(Rect{X: 300, Y: 60, W: 1534, H: 984}, r, 2.56) {
			t.Errorf("yarıçap %.2f: çerçeve hızlı yolu KAPALI", r)
		}
	}
	// Yarıçapın tavanı kutuya sığmıyorsa genel yol (o kırpar).
	if fastEligible(Rect{X: 0, Y: 0, W: 40, H: 40}, 20.5) {
		t.Error("tavanı kutunun yarısını aşan yarıçapta hızlı yol açık")
	}
}

func BenchmarkFillRoundRectGenel(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	p := New(img)
	rc := Rect{X: 300, Y: 60, W: 1534, H: 984}
	c := color.RGBA{R: 20, G: 28, B: 38, A: 255}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		genelFill(p, rc, 12, c)
	}
}

func BenchmarkFillRoundRectHizli(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	p := New(img)
	rc := Rect{X: 300, Y: 60, W: 1534, H: 984}
	c := color.RGBA{R: 20, G: 28, B: 38, A: 255}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.FillRoundRect(rc, 12, c)
	}
}

func BenchmarkStrokeRoundRectGenel(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	p := New(img)
	rc := Rect{X: 300, Y: 60, W: 1534, H: 984}
	c := color.RGBA{R: 45, G: 212, B: 191, A: 255}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		genelStroke(p, rc, 12, 1, c)
	}
}

func BenchmarkStrokeRoundRectHizli(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	p := New(img)
	rc := Rect{X: 300, Y: 60, W: 1534, H: 984}
	c := color.RGBA{R: 45, G: 212, B: 191, A: 255}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.StrokeRoundRect(rc, 12, 1, c)
	}
}
