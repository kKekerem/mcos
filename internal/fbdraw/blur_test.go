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
// Tam çözünürlükte 1080p bulanıklık 176 ms sürüyordu; bu kabul edilemez.
// Bu test, bütçenin aşılmadığını CI'da da doğrular.
func TestBlurLargeRegionIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("kısa kipte atlandı")
	}
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	start := time.Now()
	Blur(img, img.Bounds(), 12)
	el := time.Since(start)
	if el > 60*time.Millisecond {
		t.Errorf("1080p bulanıklık %v sürdü — 60 ms bütçesi aşıldı, küçültme yolu çalışmıyor", el)
	}
	t.Logf("1920x1080 bulanıklık: %v", el)
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
