package fbdraw

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// TestBlurSpreadsInk — bulanıklık mürekkebi komşu piksellere YAYMALI.
//
// isInk() eşiği (+24) burada KULLANILMAZ: 16 pikselden ibaret bir kare geniş
// bir alana yayılınca her pikselin katkısı o eşiğin çok altında kalır. Doğru
// ölçüt "komşu piksel arka plandan farklı mı" ve "tepe değer düştü mü".
func TestBlurSpreadsInk(t *testing.T) {
	img, p := canvas(64, 64)
	p.Fill(image.Rect(30, 30, 34, 34), ink) // tek keskin kare

	peakBefore := img.Pix[img.PixOffset(32, 32)]
	nearBefore := img.Pix[img.PixOffset(24, 32)]
	if nearBefore != bg.R {
		t.Fatalf("test kurulumu hatalı: (24,32) zaten %d", nearBefore)
	}

	Blur(img, img.Bounds(), 6)

	nearAfter := img.Pix[img.PixOffset(24, 32)]
	if nearAfter <= nearBefore {
		t.Errorf("(24,32) değişmedi (%d -> %d) — mürekkep yayılmamış", nearBefore, nearAfter)
	}

	peakAfter := img.Pix[img.PixOffset(32, 32)]
	if peakAfter >= peakBefore {
		t.Errorf("tepe değer düşmedi (%d -> %d) — bulanıklık uygulanmamış", peakBefore, peakAfter)
	}
}

// TestBlurLargeRegionIsFast — büyük bölgede küçültme yolu devreye girmeli.
//
// ── Neden mutlak süre ÖLÇMÜYORUZ ────────────────────────────────────────────
// Bu test önce "1080p bulanıklık 60 ms'yi aşmasın" diyordu. İki sorunu vardı:
//
//  1. Makine hızına bağlıydı. Aynı kod bir dizüstünde 55 ms, yük altında
//     79 ms sürüyordu — yani test kodu değil, o anda başka ne çalıştığını
//     ölçüyordu.
//  2. "go test -race" altında KESİN başarısız oluyordu: yarış dedektörü her
//     şeyi 10-20 kat yavaşlatır. Yani sıradan bir "-race ./..." koşusu
//     kırmızı veriyordu ve insan onu görmezden gelmeyi öğrenirdi — asıl
//     tehlike bu.
//
// Asıl doğrulanmak istenen şey bir süre değil, bir DEĞİŞMEZ: büyük bölgede
// küçültme yolunun gerçekten devreye girdiği. Onu oranla ölçüyoruz: aynı
// görüntüyü tam çözünürlükte bulanıklaştırmak, Blur'un kendisinden belirgin
// biçimde YAVAŞ olmalı. İki ölçüm de aynı makinede, aynı yük altında
// yapıldığı için oran makineden bağımsızdır.
func TestBlurLargeRegionIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("kısa kipte atlandı")
	}
	if raceEnabled {
		// Gerekçe race_on_test.go içinde: yarış dedektörü iki yolun bellek
		// erişim profillerini farklı oranda yavaşlattığı için ORAN anlamını
		// yitiriyor ve test hiçbir şey bozulmamışken kırmızı yanıyor.
		t.Skip("yarış dedektörü altında süre oranı ölçülemez")
	}
	const w, h = 1920, 1080
	const radius = 12

	// ── Neden ÜÇ turun en iyisi? ────────────────────────────────────────
	// Tek ölçüm, o an makinede koşan başka bir işe takılırsa şişer ve test
	// rastgele kırmızı yanar. En iyi süre, "bu kod yolu en az ne kadar iş
	// yapıyor" sorusunun en kararlı cevabıdır; gürültü yalnızca YUKARI
	// yönde olur, aşağı değil.
	best := func(f func()) time.Duration {
		var d time.Duration
		for i := 0; i < 3; i++ {
			start := time.Now()
			f()
			if el := time.Since(start); d == 0 || el < d {
				d = el
			}
		}
		return d
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fast := best(func() { Blur(img, img.Bounds(), radius) })

	// Karşılaştırma: küçültmeden, tam çözünürlükte aynı bulanıklık.
	buf := make([]uint8, w*h*4)
	full := best(func() { blurBuf(buf, w, h, radius) })

	if fast >= full {
		t.Errorf("Blur (%v) tam çözünürlüklü bulanıklıktan (%v) hızlı değil — "+
			"küçültme yolu çalışmıyor", fast, full)
	}
	// Küçültme 4 kat ise alan 16 kat azalır; 3 kat hızlanma çok ihtiyatlı bir
	// alt sınır ve yalnızca yol tamamen kapandığında ihlal edilir.
	if ratio := float64(full) / float64(fast); ratio < 3 {
		t.Errorf("yalnızca %.1f kat hızlanma (%v -> %v) — küçültme yolu "+
			"beklenenden az iş kazandırıyor", ratio, full, fast)
	}
	t.Logf("1920x1080: küçülterek %v, tam çözünürlükte %v", fast, full)
}

// TestBlurPreservesAverageBrightness — bulanıklık toplam parlaklığı korumalı.
//
// Kenar kenetleme yanlış yapılırsa kenarlar kararır ve ortalama düşer; bu test
// o hatayı yakalar.
func TestBlurPreservesAverageBrightness(t *testing.T) {
	img, p := canvas(80, 80)
	p.Fill(image.Rect(0, 0, 40, 80), ink) // sol yarı dolu

	before := meanR(img)
	Blur(img, img.Bounds(), 8)
	after := meanR(img)

	if diff := absF(before - after); diff > 2.0 {
		t.Errorf("ortalama parlaklık %.1f -> %.1f (fark %.1f) — kenar kenetleme bozuk",
			before, after, diff)
	}
}

// TestBlurUniformStaysUniform — düz renk bulanıklıktan sonra DEĞİŞMEMELİ.
func TestBlurUniformStaysUniform(t *testing.T) {
	img, _ := canvas(50, 50)
	Blur(img, img.Bounds(), 7)
	for _, pt := range [][2]int{{0, 0}, {25, 25}, {49, 49}, {0, 49}, {49, 0}} {
		i := img.PixOffset(pt[0], pt[1])
		if img.Pix[i] != bg.R {
			t.Errorf("(%d,%d) düz zeminde değişti: %d (beklenen %d) — kenarlarda sızıntı var",
				pt[0], pt[1], img.Pix[i], bg.R)
		}
	}
}

// TestBlurRegionOnly — yalnızca verilen bölge etkilenmeli.
func TestBlurRegionOnly(t *testing.T) {
	img, p := canvas(60, 60)
	p.Fill(image.Rect(0, 0, 60, 60), ink)
	p.Fill(image.Rect(5, 5, 25, 25), bg)

	region := image.Rect(0, 0, 30, 30)
	Blur(img, region, 5)

	// Bölge dışındaki bir piksel dokunulmamış olmalı.
	i := img.PixOffset(50, 50)
	if img.Pix[i] != ink.R {
		t.Errorf("bölge dışı piksel değişti: %d (beklenen %d)", img.Pix[i], ink.R)
	}
}

// TestDimDarkens — karartma pikselleri hedefe doğru çekmeli.
func TestDimDarkens(t *testing.T) {
	img, p := canvas(20, 20)
	p.Fill(img.Bounds(), color.RGBA{R: 200, G: 200, B: 200, A: 255})

	Dim(img, img.Bounds(), color.RGBA{A: 255}, 0.5)

	i := img.PixOffset(10, 10)
	// 200 * 0.5 + 0 * 0.5 = 100
	if got := int(img.Pix[i]); got < 96 || got > 104 {
		t.Errorf("karartma sonrası R = %d, ~100 olmalı", got)
	}
}

// TestDimZeroIsNoOp — amount=0 hiçbir şeyi değiştirmemeli.
func TestDimZeroIsNoOp(t *testing.T) {
	img, p := canvas(20, 20)
	p.Fill(img.Bounds(), ink)
	Dim(img, img.Bounds(), color.RGBA{A: 255}, 0)
	i := img.PixOffset(10, 10)
	if img.Pix[i] != ink.R {
		t.Errorf("amount=0 iken piksel değişti: %d", img.Pix[i])
	}
}

// TestBlurDoesNotPanicOutOfBounds — taşan bölge panik yapmamalı.
func TestBlurDoesNotPanicOutOfBounds(t *testing.T) {
	img, _ := canvas(30, 30)
	Blur(img, image.Rect(-50, -50, 200, 200), 4)
	Blur(img, image.Rect(100, 100, 200, 200), 4) // tamamen dışarıda
	Blur(img, img.Bounds(), 0)                   // yarıçap yok
	Blur(img, img.Bounds(), 9999)                // aşırı yarıçap
	Dim(img, image.Rect(-10, -10, 100, 100), color.RGBA{A: 255}, 2)
}

func meanR(img *image.RGBA) float64 {
	b := img.Bounds()
	var sum float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			sum += float64(img.Pix[img.PixOffset(x, y)])
		}
	}
	return sum / float64(b.Dx()*b.Dy())
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// BenchmarkBlur1080p — modal açılırken tam ekran bulanıklık ne kadar sürüyor?
//
// Bu bir performans BÜTÇESİ testidir: modal açılışı tek seferlik olduğu için
// birkaç on milisaniye kabul edilebilir, ama kare başına yapılamaz.
func BenchmarkBlur1080p(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Blur(img, img.Bounds(), 12)
	}
}
