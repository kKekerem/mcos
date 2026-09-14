package fbdraw

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"golang.org/x/image/vector"
)

// ════════════════════════════════════════════════════════════════════════════
// KIRPMA ÇİZİMİ DEĞİŞTİRMEMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// Painter artık her şekli kendi sınır kutusu kadar rasterleştiriyor; eskiden
// tuvalin tamamını rasterleştiriyordu. Bu bir HIZ eniyilemesidir ve görüntüyü
// zerre kadar değiştirmemelidir.
//
// ── Neden bu test şart ──────────────────────────────────────────────────────
// Kırpmada yapılacak hata SESSİZDİR: şekil bir piksel kayar, kenarı tıraşlanır
// ya da hiç çizilmez. Panelde bunu gözle fark etmek neredeyse imkânsız —
// bir düğmenin kenarının yarım piksel eksik olduğunu kim görür?
//
// Bu yüzden testler, tam tuval rasterleştiricisiyle üretilen REFERANS
// görüntüyle piksel piksel karşılaştırır.

// reference draws a shape the OLD way: rasteriser sized to the whole canvas,
// path coordinates in canvas space, composited over the full bounds.
//
// Painter'ın eski path() gövdesinin birebir aynısı. Üretim kodunda ölü kod
// bırakmamak için burada duruyor.
func reference(w, h int, c color.RGBA, build func(*vector.Rasterizer)) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	ras := &vector.Rasterizer{}
	ras.Reset(w, h)
	ras.DrawOp = draw.Over
	build(ras)
	ras.Draw(dst, dst.Bounds(), &image.Uniform{C: c}, image.Point{})
	return dst
}

// diff reports the worst and total per-channel difference between two images.
func diff(a, b *image.RGBA) (worst int, total int64, at image.Point) {
	for i := 0; i < len(a.Pix); i++ {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		total += int64(d)
		if d > worst {
			worst = d
			px := i / 4
			at = image.Pt(px%a.Rect.Dx(), px/a.Rect.Dx())
		}
	}
	return worst, total, at
}

const (
	canvasW = 400
	canvasH = 300
)

var testColor = color.RGBA{R: 0x23, G: 0xA9, B: 0x9C, A: 0xFF}

// maxEdgeDelta, kabul edilen en büyük kanal farkı.
//
// ── Neden sıfır değil ───────────────────────────────────────────────────────
// Ölçüldü: kırpmadan sonra fark YALNIZCA eğri kenarlarda çıkıyor ve en fazla
// 2/255. Düz kenarlı şekiller (çokgen, çizgi) birebir aynı kalıyor.
//
// Nedeni bir hata değil: vector.Rasterizer kapsama değerlerini float32 ile
// biriktirir ve ızgaranın BOYUTU değişince toplama sırası değişir. Eğri
// (Bézier) kenarlarda bu son bitte görünür. Gözle ayırt edilemez: 255'te 2,
// yumuşatılmış bir kenar pikselinde.
//
// Tolerans gerçek hataları YAKALAMAYA devam eder: bir piksellik kayma ya da
// tıraşlanmış bir kenar, dolu bir kenar pikselinde 255'e yakın fark üretir.
const maxEdgeDelta = 4

// maxDifferingFraction, farklı olabilecek piksel oranı.
//
// Kayan nokta farkı yalnızca kenar piksellerinde olur; şeklin içi ve dışı tam
// olarak aynıdır. Sistematik bir hata (yanlış öteleme) ise çok daha geniş bir
// alanı etkilerdi.
const maxDifferingFraction = 0.02

func check(t *testing.T, name string, got, want *image.RGBA) {
	t.Helper()
	worst, total, at := diff(got, want)

	if worst > maxEdgeDelta {
		t.Errorf("%s: kırpma çizimi DEĞİŞTİRDİ — en kötü fark %d (sınır %d), "+
			"(%d,%d) noktasında; toplam %d",
			name, worst, maxEdgeDelta, at.X, at.Y, total)
		return
	}

	// Kaç piksel farklı? Az sayıda kenar pikseli beklenir; geniş bir alan
	// sistematik bir hatadır.
	differing := 0
	for i := 0; i < len(got.Pix); i += 4 {
		if got.Pix[i] != want.Pix[i] || got.Pix[i+1] != want.Pix[i+1] ||
			got.Pix[i+2] != want.Pix[i+2] || got.Pix[i+3] != want.Pix[i+3] {
			differing++
		}
	}
	pixels := len(got.Pix) / 4
	if frac := float64(differing) / float64(pixels); frac > maxDifferingFraction {
		t.Errorf("%s: %d piksel farklı (%.1f%%, sınır %.1f%%) — "+
			"kayan nokta farkı bu kadar geniş olamaz, öteleme hatası olabilir",
			name, differing, frac*100, maxDifferingFraction*100)
	}
}

func TestClippedCircleMatchesFullCanvas(t *testing.T) {
	cases := []struct{ cx, cy, r float64 }{
		{200, 150, 60},  // ortada
		{20, 20, 30},    // sol üst köşeye taşıyor
		{390, 290, 40},  // sağ alt köşeye taşıyor
		{200, 150, 0.6}, // bir pikselden küçük
		{-10, 150, 40},  // tuvalin dışında başlıyor
	}
	for _, c := range cases {
		dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
		New(dst).FillCircle(c.cx, c.cy, c.r, testColor)

		want := reference(canvasW, canvasH, testColor, func(ras *vector.Rasterizer) {
			circlePath(ras, c.cx, c.cy, c.r, +1, 0, 0)
		})
		check(t, "FillCircle", dst, want)
	}
}

func TestClippedRingMatchesFullCanvas(t *testing.T) {
	const cx, cy, r, th = 200, 150, 70, 12
	dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	New(dst).StrokeCircle(cx, cy, r, th, testColor)

	want := reference(canvasW, canvasH, testColor, func(ras *vector.Rasterizer) {
		circlePath(ras, cx, cy, r, +1, 0, 0)
		circlePath(ras, cx, cy, r-th, -1, 0, 0)
	})
	check(t, "StrokeCircle", dst, want)
}

func TestClippedRoundRectMatchesFullCanvas(t *testing.T) {
	cases := []Rect{
		{X: 50, Y: 40, W: 200, H: 120},
		{X: -20, Y: -10, W: 100, H: 80},  // sol üstten taşıyor
		{X: 340, Y: 250, W: 120, H: 100}, // sağ alttan taşıyor
		{X: 10, Y: 10, W: 3, H: 3},       // minik
	}
	for _, rc := range cases {
		dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
		New(dst).FillRoundRect(rc, 12, testColor)

		want := reference(canvasW, canvasH, testColor, func(ras *vector.Rasterizer) {
			roundRectPath(ras, rc, 12, +1, 0, 0)
		})
		check(t, "FillRoundRect", dst, want)
	}
}

func TestClippedStrokeRoundRectMatchesFullCanvas(t *testing.T) {
	rc := Rect{X: 60, Y: 50, W: 240, H: 150}
	const rad, th = 14.0, 3.0

	dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	New(dst).StrokeRoundRect(rc, rad, th, testColor)

	inner := Rect{X: rc.X + th, Y: rc.Y + th, W: rc.W - 2*th, H: rc.H - 2*th}
	want := reference(canvasW, canvasH, testColor, func(ras *vector.Rasterizer) {
		roundRectPath(ras, rc, rad, +1, 0, 0)
		roundRectPath(ras, inner, math.Max(0, rad-th), -1, 0, 0)
	})
	check(t, "StrokeRoundRect", dst, want)
}

func TestClippedPolygonMatchesFullCanvas(t *testing.T) {
	pts := []Pt{{X: 100, Y: 60}, {X: 260, Y: 110}, {X: 140, Y: 220}}

	dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	New(dst).FillPolygon(pts, testColor)

	want := reference(canvasW, canvasH, testColor, func(ras *vector.Rasterizer) {
		ras.MoveTo(float32(pts[0].X), float32(pts[0].Y))
		for _, q := range pts[1:] {
			ras.LineTo(float32(q.X), float32(q.Y))
		}
		ras.ClosePath()
	})
	check(t, "FillPolygon", dst, want)
}

func TestClippedLineMatchesFullCanvas(t *testing.T) {
	const x0, y0, x1, y1, width = 40.0, 40.0, 350.0, 260.0, 7.0

	dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	New(dst).Line(x0, y0, x1, y1, width, testColor)

	dx, dy := x1-x0, y1-y0
	length := math.Hypot(dx, dy)
	nx, ny := -dy/length*width/2, dx/length*width/2
	want := reference(canvasW, canvasH, testColor, func(ras *vector.Rasterizer) {
		ras.MoveTo(float32(x0+nx), float32(y0+ny))
		ras.LineTo(float32(x1+nx), float32(y1+ny))
		ras.LineTo(float32(x1-nx), float32(y1-ny))
		ras.LineTo(float32(x0-nx), float32(y0-ny))
		ras.ClosePath()
	})
	check(t, "Line", dst, want)
}

// Şekil tuvalin TAMAMEN dışındaysa hiçbir şey çizilmemeli ve çökmemeli.
func TestFullyOffCanvasDrawsNothing(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	p := New(dst)

	p.FillCircle(-500, -500, 20, testColor)
	p.FillRoundRect(Rect{X: 900, Y: 900, W: 50, H: 50}, 4, testColor)
	p.FillPolygon([]Pt{{X: -100, Y: -100}, {X: -50, Y: -100}, {X: -75, Y: -50}}, testColor)
	p.Line(-200, -200, -100, -100, 4, testColor)

	for i := range dst.Pix {
		if dst.Pix[i] != 0 {
			t.Fatalf("tuval dışındaki şekil piksel yazdı (dizin %d = %d)",
				i, dst.Pix[i])
		}
	}
}

// ── Hız ─────────────────────────────────────────────────────────────────────

// BenchmarkSmallShapeOnLargeCanvas, asıl kazancı ölçer: 1080p bir tuvale
// çizilen KÜÇÜK bir şekil.
//
// Eskiden bu, şeklin boyutundan bağımsız olarak iki milyon piksellik bir
// geçişti; panelde bir karede onlarca böyle şekil var.
func BenchmarkSmallShapeOnLargeCanvas(b *testing.B) {
	dst := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	p := New(dst)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Tipik bir chevron/ok işareti boyutu.
		p.FillPolygon([]Pt{{X: 100, Y: 100}, {X: 112, Y: 108}, {X: 100, Y: 116}},
			testColor)
	}
}

func BenchmarkSmallShapeOnLargeCanvasFullCanvasRaster(b *testing.B) {
	dst := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	ras := &vector.Rasterizer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ras.Reset(1920, 1080)
		ras.DrawOp = draw.Over
		ras.MoveTo(100, 100)
		ras.LineTo(112, 108)
		ras.LineTo(100, 116)
		ras.ClosePath()
		ras.Draw(dst, dst.Bounds(), &image.Uniform{C: testColor}, image.Point{})
	}
}
