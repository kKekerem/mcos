package fbdraw

import (
	"image"
	"image/color"
	"math"
	"testing"
)

var (
	bg  = color.RGBA{R: 15, G: 18, B: 22, A: 255}
	ink = color.RGBA{R: 240, G: 244, B: 248, A: 255}
)

func canvas(w, h int) (*image.RGBA, *Painter) {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	p := New(dst)
	p.Fill(dst.Bounds(), bg)
	return dst, p
}

// isInk reports whether a pixel differs meaningfully from the background.
func isInk(img *image.RGBA, x, y int) bool {
	if !(image.Point{x, y}).In(img.Bounds()) {
		return false
	}
	i := img.PixOffset(x, y)
	// Kanal başına 24'lük eşik: kenar yumuşatmanın en soluk pikselini de
	// "mürekkep" sayar ama arka plan gürültüsünü saymaz.
	return int(img.Pix[i]) > int(bg.R)+24 ||
		int(img.Pix[i+1]) > int(bg.G)+24 ||
		int(img.Pix[i+2]) > int(bg.B)+24
}

// TestStrokedRoundRectIsContinuous — KULLANICININ ASIL İSTEĞİNİN TESTİ.
//
// Çerçevenin kesintisiz TEK PARÇA olduğunu kanıtlar: konturu takip eden her
// tarama çizgisinde mürekkep bulunmalı. Karakterle çizilen çerçevelerde
// köşelerde kopukluk olur; burada olmamalı.
func TestStrokedRoundRectIsContinuous(t *testing.T) {
	const W, H = 200, 120
	img, p := canvas(W, H)

	rc := R(20, 20, 160, 80)
	const rad, th = 16, 2
	p.StrokeRoundRect(rc, rad, th, ink)

	// 1) Kutunun kapladığı HER satırda en az iki mürekkep sütunu olmalı
	//    (sol kenar + sağ kenar). Bir satırda hiç yoksa çerçevede yatay
	//    kopukluk var demektir.
	for y := int(rc.Y); y < int(rc.Y+rc.H); y++ {
		found := 0
		for x := 0; x < W; x++ {
			if isInk(img, x, y) {
				found++
			}
		}
		if found == 0 {
			t.Errorf("y=%d satırında hiç mürekkep yok — çerçevede kopukluk", y)
		}
	}

	// 2) Aynısı sütunlar için (üst + alt kenar).
	for x := int(rc.X); x < int(rc.X+rc.W); x++ {
		found := 0
		for y := 0; y < H; y++ {
			if isInk(img, x, y) {
				found++
			}
		}
		if found == 0 {
			t.Errorf("x=%d sütununda hiç mürekkep yok — çerçevede kopukluk", x)
		}
	}
}

// TestCornersAreActuallyRounded — köşe gerçekten yay mı, yoksa dik mi?
//
// Dik köşede kutunun tam köşe pikseli doludur. Yuvarlak köşede boştur, ama
// yay üzerindeki nokta doludur.
func TestCornersAreActuallyRounded(t *testing.T) {
	img, p := canvas(200, 120)
	rc := R(20, 20, 160, 80)
	const rad, th = 16, 2
	p.StrokeRoundRect(rc, rad, th, ink)

	corners := []struct {
		name   string
		cx, cy int // kutunun tam köşesi — BOŞ olmalı
	}{
		{"sol-üst", int(rc.X), int(rc.Y)},
		{"sağ-üst", int(rc.X + rc.W - 1), int(rc.Y)},
		{"sol-alt", int(rc.X), int(rc.Y + rc.H - 1)},
		{"sağ-alt", int(rc.X + rc.W - 1), int(rc.Y + rc.H - 1)},
	}
	for _, c := range corners {
		if isInk(img, c.cx, c.cy) {
			t.Errorf("%s köşe pikseli (%d,%d) dolu — köşe yuvarlak değil, dik",
				c.name, c.cx, c.cy)
		}
	}

	// Yay üzerinde bir nokta: sol-üst köşe merkezinden 45 derecede.
	// Merkez = (X+rad, Y+rad); yay noktası = merkez - rad/sqrt(2) her eksende.
	ccx, ccy := rc.X+rad, rc.Y+rad
	off := float64(rad) / math.Sqrt2
	ax, ay := int(math.Round(ccx-off)), int(math.Round(ccy-off))
	// Kenar yumuşatma nedeniyle tam piksel kayabilir: 2 piksel komşulukta ara.
	hit := false
	for dy := -2; dy <= 2 && !hit; dy++ {
		for dx := -2; dx <= 2 && !hit; dx++ {
			if isInk(img, ax+dx, ay+dy) {
				hit = true
			}
		}
	}
	if !hit {
		t.Errorf("sol-üst yay üzerinde (%d,%d) civarında mürekkep yok — köşe yayı çizilmemiş", ax, ay)
	}
}

// TestStrokeIsHollow — kontur halka olmalı, dolu değil.
func TestStrokeIsHollow(t *testing.T) {
	img, p := canvas(200, 120)
	rc := R(20, 20, 160, 80)
	p.StrokeRoundRect(rc, 16, 2, ink)

	cx, cy := int(rc.X+rc.W/2), int(rc.Y+rc.H/2)
	if isInk(img, cx, cy) {
		t.Error("kutunun merkezi dolu — kontur değil, dolu şekil çizilmiş")
	}
}

// TestFillRoundRectIsSolid — dolu şekil gerçekten dolu olmalı.
func TestFillRoundRectIsSolid(t *testing.T) {
	img, p := canvas(200, 120)
	rc := R(20, 20, 160, 80)
	p.FillRoundRect(rc, 16, ink)

	cx, cy := int(rc.X+rc.W/2), int(rc.Y+rc.H/2)
	if !isInk(img, cx, cy) {
		t.Error("dolu yuvarlak dikdörtgenin merkezi boş")
	}
	// Köşe yine boş olmalı (yuvarlaklık dolu şekilde de geçerli).
	if isInk(img, int(rc.X), int(rc.Y)) {
		t.Error("dolu şeklin köşesi dik — yuvarlanmamış")
	}
}

// TestAntiAliasing — kenarlarda ara tonlar olmalı.
//
// Kenar yumuşatma yoksa her piksel ya tam arka plan ya tam mürekkeptir;
// o durumda köşeler basamaklı görünür. Ara ton varlığı yumuşatmanın
// çalıştığını kanıtlar.
func TestAntiAliasing(t *testing.T) {
	img, p := canvas(200, 120)
	p.FillCircle(100, 60, 40, ink)

	var partial int
	for y := 0; y < 120; y++ {
		for x := 0; x < 200; x++ {
			i := img.PixOffset(x, y)
			r := img.Pix[i]
			// Ne arka plan ne de tam mürekkep -> ara ton.
			if r > bg.R+24 && r < ink.R-24 {
				partial++
			}
		}
	}
	if partial < 50 {
		t.Errorf("yalnızca %d ara tonlu piksel — kenar yumuşatma çalışmıyor gibi", partial)
	}
}

// TestRadiusClamped — yarıçap kenarın yarısını aşarsa şekil bozulmamalı.
func TestRadiusClamped(t *testing.T) {
	img, p := canvas(100, 100)
	rc := R(10, 10, 40, 40)
	// Yarıçap 1000: kenarın yarısına (20) kırpılmalı ve daire olmalı.
	p.FillRoundRect(rc, 1000, ink)

	cx, cy := int(rc.X+rc.W/2), int(rc.Y+rc.H/2)
	if !isInk(img, cx, cy) {
		t.Error("aşırı yarıçapta merkez boş kaldı — şekil bozuldu")
	}
	if isInk(img, int(rc.X), int(rc.Y)) {
		t.Error("aşırı yarıçapta köşe hâlâ dik")
	}
}

// TestStrokeCircleIsRing — daire konturu halka olmalı.
func TestStrokeCircleIsRing(t *testing.T) {
	img, p := canvas(120, 120)
	p.StrokeCircle(60, 60, 40, 3, ink)

	if isInk(img, 60, 60) {
		t.Error("daire konturunun merkezi dolu — halka değil")
	}
	// Çember üzerinde 8 yönde mürekkep olmalı: kesintisizlik kanıtı.
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		x := int(math.Round(60 + 40*math.Cos(a)))
		y := int(math.Round(60 + 40*math.Sin(a)))
		hit := false
		for dy := -3; dy <= 3 && !hit; dy++ {
			for dx := -3; dx <= 3 && !hit; dx++ {
				if isInk(img, x+dx, y+dy) {
					hit = true
				}
			}
		}
		if !hit {
			t.Errorf("çember üzerinde %d. yönde (%d,%d) mürekkep yok — halka kesintili", i, x, y)
		}
	}
}

// TestCrispLinesAreNotBlurred — ayırıcı çizgiler keskin olmalı.
func TestCrispLinesAreNotBlurred(t *testing.T) {
	img, p := canvas(100, 40)
	p.HLine(10, 90, 20, 1, ink)

	// y=20 satırı tam mürekkep, komşu satırlar tam arka plan olmalı.
	i := img.PixOffset(50, 20)
	if img.Pix[i] != ink.R {
		t.Errorf("çizgi satırı tam mürekkep değil: %d (beklenen %d)", img.Pix[i], ink.R)
	}
	for _, y := range []int{19, 21} {
		j := img.PixOffset(50, y)
		if img.Pix[j] != bg.R {
			t.Errorf("y=%d komşu satırı kirlenmiş: %d — çizgi bulanık", y, img.Pix[j])
		}
	}
}

// TestBlend — renk karışımı uç noktalarda doğru olmalı.
func TestBlend(t *testing.T) {
	a := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	b := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if got := Blend(a, b, 0); got != a {
		t.Errorf("Blend(t=0) = %v, a olmalı", got)
	}
	if got := Blend(a, b, 1); got != b {
		t.Errorf("Blend(t=1) = %v, b olmalı", got)
	}
	mid := Blend(a, b, 0.5)
	if mid.R < 126 || mid.R > 129 {
		t.Errorf("Blend(t=0.5).R = %d, ~128 olmalı", mid.R)
	}
}

// TestNothingDrawsOutsideCanvas — taşma olmamalı (panik veya bozulma).
func TestNothingDrawsOutsideCanvas(t *testing.T) {
	_, p := canvas(50, 50)
	// Tamamen dışarıda, kısmen dışarıda ve negatif koordinatlı şekiller.
	p.FillRoundRect(R(-100, -100, 40, 40), 8, ink)
	p.StrokeRoundRect(R(40, 40, 100, 100), 8, 2, ink)
	p.FillCircle(-20, -20, 30, ink)
	p.Line(-50, -50, 200, 200, 3, ink)
	p.FillPolygon([]Pt{{-10, -10}, {100, 5}, {5, 100}}, ink)
	// Panik olmadan buraya geldiyse geçti.
}

// TestAlphaIsPremultiplied — saydam dolgu GERCEKTEN saydam cizilmeli.
//
// Go color.RGBA alfa on carpimlidir. Yalnizca A alanini kismak (R,G,B ayni
// kalarak) gecersiz bir renk uretir ve draw.Over onu neredeyse tam opak
// cizer: %20 alfa ile cizilen bir rozet dolu sari gorunurdu.
func TestAlphaIsPremultiplied(t *testing.T) {
	yellow := color.RGBA{R: 217, G: 162, B: 27, A: 255}
	a := Alpha(yellow, 0.20)

	if a.R > a.A || a.G > a.A || a.B > a.A {
		t.Fatalf("Alpha(%v,0.2) = %v — on carpimli degil (kanal > alfa)", yellow, a)
	}

	// Koyu zemine %20 alfa ile dolu dikdortgen ciz: sonuc zemine YAKIN olmali,
	// kaynak renge degil.
	img, p := canvas(40, 40)
	p.FillRoundRect(R(5, 5, 30, 30), 4, a)

	i := img.PixOffset(20, 20)
	gotR := int(img.Pix[i])
	// Beklenen: 0.2*217 + 0.8*15 = 55.4
	if gotR < 45 || gotR > 66 {
		t.Errorf("merkez R = %d, ~55 olmali (%%20 alfa harmani). "+
			"Cok yuksekse Alpha on carpim yapmiyor demektir", gotR)
	}
	// Tam opak cizilseydi 217 civari olurdu.
	if gotR > 150 {
		t.Errorf("merkez R = %d — saydam dolgu opak cizilmis", gotR)
	}
}

// TestAlphaZeroDrawsNothing — f=0 hicbir sey cizmemeli.
func TestAlphaZeroDrawsNothing(t *testing.T) {
	img, p := canvas(20, 20)
	p.FillRoundRect(R(2, 2, 16, 16), 2, Alpha(ink, 0))
	if isInk(img, 10, 10) {
		t.Error("Alpha(c,0) ile cizim gorunuyor")
	}
}
