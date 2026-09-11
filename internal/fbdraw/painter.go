// Package fbdraw draws the MCOS interface as REAL VECTOR GRAPHICS.
//
// ══════════════════════════════════════════════════════════════════════════
// NEDEN BU PAKET VAR
// ══════════════════════════════════════════════════════════════════════════
//
// Panel eskiden tüm çerçeveleri, çizgileri ve butonları box-drawing
// KARAKTERLERİYLE çiziyordu (╭ ─ ╮ │ ╰ ╯). Bunun iki kırılmaz sınırı var:
//
//  1. Köşeler asla tek parça olmaz. Her köşe ayrı bir karakter hücresidir;
//     hücre ızgarası ile glif ilerlemesi tam oturmadığında köşelerde görünür
//     kopukluk oluşur.
//
//  2. Glif kapsaması fonta bağlıdır. Ölçüldü (internal/fbfont testleri):
//     gömülü FiraCode Nerd Font şu ikonları İÇERMİYOR
//     ▸ ◂ ▴ ▾ ◈ ◍ ★ ⚑ ⚙ ⚠ ✗
//     ve terminal bunlar için başka bir fonta düşüyor; o fontun ilerlemesi
//     farklı olunca sütunlar kayıyor.
//
// Bu paket ikisini de kökünden çözer: çerçeve, yuvarlak köşe, ayırıcı çizgi,
// buton, radyo/onay kutusu, ok ve ikon artık FONTTAN GELMEZ — gerçek,
// kenar yumuşatmalı vektör şekiller olarak çizilir. Köşeler gerçek çeyrek
// daire yaylarıdır ve tek parçadır.
//
// Font YALNIZCA metin çizer (Latin + Türkçe + noktalama — hepsi doğrulandı).
//
// ══════════════════════════════════════════════════════════════════════════
// UYGULAMA
// ══════════════════════════════════════════════════════════════════════════
//
// Rasterleştirme golang.org/x/image/vector ile yapılır: analitik kapsama
// hesabı, yani gerçek kenar yumuşatma (gri tonlamalı). Saf Go — CGO_ENABLED=0
// çapraz derlemesi korunur.
//
// Koordinatlar float64 PİKSELDİR, hücre değil. Çağıran taraf (fbui) hücre
// ızgarasını piksele çevirir; burada ızgara kavramı yoktur. Bu ayrım sayesinde
// çerçeveler hücre sınırlarına hapsolmaz — bir kenarlık 1.5 piksel kalınlıkta
// ve iki hücrenin ortasından geçebilir.
package fbdraw

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// kappa, bir çeyrek daireyi kübik Bézier ile yaklaşık çizmek için kullanılan
// sabittir. Kontrol noktası uzaklığı = kappa * r.
//
// 4/3 * (sqrt(2) - 1) — maksimum sapma yarıçapın ~0.02%'si, yani gözle
// ayırt edilemez. Yuvarlak köşelerin "gerçek yay" olmasının sebebi bu.
const kappa = 0.5522847498307933

// Painter draws vector shapes onto an RGBA canvas.
//
// Eşzamanlı kullanılamaz: tek bir kare tek bir goroutine tarafından çizilir.
type Painter struct {
	dst *image.RGBA
	// ras yeniden kullanılır: her şekilde yeni rasterleştirici ayırmak, saniyede
	// birkaç tam kare çizen bir panelde ciddi çöp üretirdi.
	ras *vector.Rasterizer
}

// New returns a Painter that draws onto dst.
func New(dst *image.RGBA) *Painter {
	return &Painter{dst: dst, ras: &vector.Rasterizer{}}
}

// Bounds returns the canvas bounds.
func (p *Painter) Bounds() image.Rectangle { return p.dst.Bounds() }

// Fill paints a solid axis-aligned rectangle (no anti-aliasing needed).
func (p *Painter) Fill(r image.Rectangle, c color.RGBA) {
	r = r.Intersect(p.dst.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(p.dst, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

// Rect is a float-precision rectangle in pixels.
type Rect struct{ X, Y, W, H float64 }

// R builds a Rect.
func R(x, y, w, h float64) Rect { return Rect{X: x, Y: y, W: w, H: h} }

// path accumulates a shape then rasterises it as a mask over a solid colour.
//
// Kapsama maskesi olarak çizilir (draw.Over): kenar pikselleri kısmi alfa ile
// harmanlanır, yani basamak yerine yumuşak kenar oluşur.
func (p *Painter) path(build func(r *vector.Rasterizer), c color.RGBA) {
	b := p.dst.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}

	p.ras.Reset(w, h)
	p.ras.DrawOp = draw.Over
	build(p.ras)
	p.ras.Draw(p.dst, b, &image.Uniform{C: c}, image.Point{})
}

// ── Yuvarlak dikdörtgen ─────────────────────────────────────────────────────

// roundRectPath appends a rounded rectangle to the rasteriser.
//
// dir=+1 saat yönü, dir=-1 saat yönünün tersi. Ters yön, halka (çerçeve)
// çizerken iç konturu oymak için kullanılır: vector.Rasterizer sıfır-olmayan
// sarım kuralı uygular, bu yüzden ters yönlü iç kontur deliği açar.
func roundRectPath(ras *vector.Rasterizer, rc Rect, rad float64, dir int) {
	// Yarıçap kenarın yarısını aşamaz, yoksa köşeler birbirine girer.
	maxR := math.Min(rc.W, rc.H) / 2
	if rad > maxR {
		rad = maxR
	}
	if rad < 0 {
		rad = 0
	}

	x0, y0 := float32(rc.X), float32(rc.Y)
	x1, y1 := float32(rc.X+rc.W), float32(rc.Y+rc.H)
	r := float32(rad)
	k := float32(rad * kappa)

	if dir >= 0 {
		// Saat yönü: sol-üstten başla, sağa git.
		ras.MoveTo(x0+r, y0)
		ras.LineTo(x1-r, y0)
		ras.CubeTo(x1-r+k, y0, x1, y0+r-k, x1, y0+r) // sağ-üst yay
		ras.LineTo(x1, y1-r)
		ras.CubeTo(x1, y1-r+k, x1-r+k, y1, x1-r, y1) // sağ-alt yay
		ras.LineTo(x0+r, y1)
		ras.CubeTo(x0+r-k, y1, x0, y1-r+k, x0, y1-r) // sol-alt yay
		ras.LineTo(x0, y0+r)
		ras.CubeTo(x0, y0+r-k, x0+r-k, y0, x0+r, y0) // sol-üst yay
	} else {
		// Saat yönünün tersi.
		ras.MoveTo(x0+r, y0)
		ras.CubeTo(x0+r-k, y0, x0, y0+r-k, x0, y0+r)
		ras.LineTo(x0, y1-r)
		ras.CubeTo(x0, y1-r+k, x0+r-k, y1, x0+r, y1)
		ras.LineTo(x1-r, y1)
		ras.CubeTo(x1-r+k, y1, x1, y1-r+k, x1, y1-r)
		ras.LineTo(x1, y0+r)
		ras.CubeTo(x1, y0+r-k, x1-r+k, y0, x1-r, y0)
	}
	ras.ClosePath()
}

// FillRoundRect paints a filled rounded rectangle.
func (p *Painter) FillRoundRect(rc Rect, radius float64, c color.RGBA) {
	if rc.W <= 0 || rc.H <= 0 {
		return
	}
	p.path(func(ras *vector.Rasterizer) {
		roundRectPath(ras, rc, radius, +1)
	}, c)
}

// StrokeRoundRect paints a rounded rectangle OUTLINE of the given thickness.
//
// Tek parça çizilir: dış kontur saat yönünde, iç kontur ters yönde eklenir ve
// aradaki halka tek seferde rasterleştirilir. Dört kenarı ayrı ayrı çizmek
// köşelerde bindirme/boşluk üretirdi — tam olarak karakterle çizmenin sorunu.
func (p *Painter) StrokeRoundRect(rc Rect, radius, thickness float64, c color.RGBA) {
	if rc.W <= 0 || rc.H <= 0 || thickness <= 0 {
		return
	}
	// Kalınlık kutuyu yutuyorsa dolu şekle dön.
	if thickness*2 >= rc.W || thickness*2 >= rc.H {
		p.FillRoundRect(rc, radius, c)
		return
	}

	inner := Rect{
		X: rc.X + thickness,
		Y: rc.Y + thickness,
		W: rc.W - 2*thickness,
		H: rc.H - 2*thickness,
	}
	// İç yarıçap: dış yarıçaptan kalınlık kadar küçük, ama negatif olamaz.
	innerRad := math.Max(0, radius-thickness)

	p.path(func(ras *vector.Rasterizer) {
		roundRectPath(ras, rc, radius, +1)
		roundRectPath(ras, inner, innerRad, -1)
	}, c)
}

// ── Çizgiler ────────────────────────────────────────────────────────────────

// HLine paints a horizontal line (crisp, pixel-aligned).
//
// Ayırıcı çizgiler için: kenar yumuşatma İSTENMEZ, yoksa 1 piksellik bir çizgi
// iki yarım-parlak satıra yayılıp bulanık görünür.
func (p *Painter) HLine(x0, x1, y float64, thickness float64, c color.RGBA) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	t := int(math.Round(thickness))
	if t < 1 {
		t = 1
	}
	yi := int(math.Round(y))
	p.Fill(image.Rect(int(math.Round(x0)), yi, int(math.Round(x1)), yi+t), c)
}

// VLine paints a vertical line (crisp, pixel-aligned).
func (p *Painter) VLine(y0, y1, x float64, thickness float64, c color.RGBA) {
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	t := int(math.Round(thickness))
	if t < 1 {
		t = 1
	}
	xi := int(math.Round(x))
	p.Fill(image.Rect(xi, int(math.Round(y0)), xi+t, int(math.Round(y1))), c)
}

// Line paints an arbitrary anti-aliased line segment of the given width.
func (p *Painter) Line(x0, y0, x1, y1, width float64, c color.RGBA) {
	dx, dy := x1-x0, y1-y0
	length := math.Hypot(dx, dy)
	if length == 0 || width <= 0 {
		return
	}
	// Çizgiyi, doğrultusuna dik yarım genişlikte ötelenmiş bir dikdörtgen
	// olarak inşa et.
	nx, ny := -dy/length*width/2, dx/length*width/2

	p.path(func(ras *vector.Rasterizer) {
		ras.MoveTo(float32(x0+nx), float32(y0+ny))
		ras.LineTo(float32(x1+nx), float32(y1+ny))
		ras.LineTo(float32(x1-nx), float32(y1-ny))
		ras.LineTo(float32(x0-nx), float32(y0-ny))
		ras.ClosePath()
	}, c)
}

// ── Daireler ────────────────────────────────────────────────────────────────

func circlePath(ras *vector.Rasterizer, cx, cy, r float64, dir int) {
	k := float32(r * kappa)
	fx, fy, fr := float32(cx), float32(cy), float32(r)
	if dir >= 0 {
		ras.MoveTo(fx, fy-fr)
		ras.CubeTo(fx+k, fy-fr, fx+fr, fy-k, fx+fr, fy)
		ras.CubeTo(fx+fr, fy+k, fx+k, fy+fr, fx, fy+fr)
		ras.CubeTo(fx-k, fy+fr, fx-fr, fy+k, fx-fr, fy)
		ras.CubeTo(fx-fr, fy-k, fx-k, fy-fr, fx, fy-fr)
	} else {
		ras.MoveTo(fx, fy-fr)
		ras.CubeTo(fx-k, fy-fr, fx-fr, fy-k, fx-fr, fy)
		ras.CubeTo(fx-fr, fy+k, fx-k, fy+fr, fx, fy+fr)
		ras.CubeTo(fx+k, fy+fr, fx+fr, fy+k, fx+fr, fy)
		ras.CubeTo(fx+fr, fy-k, fx+k, fy-fr, fx, fy-fr)
	}
	ras.ClosePath()
}

// FillCircle paints a filled circle.
func (p *Painter) FillCircle(cx, cy, r float64, c color.RGBA) {
	if r <= 0 {
		return
	}
	p.path(func(ras *vector.Rasterizer) { circlePath(ras, cx, cy, r, +1) }, c)
}

// StrokeCircle paints a circle outline — one continuous ring, not segments.
func (p *Painter) StrokeCircle(cx, cy, r, thickness float64, c color.RGBA) {
	if r <= 0 || thickness <= 0 {
		return
	}
	if thickness >= r {
		p.FillCircle(cx, cy, r, c)
		return
	}
	p.path(func(ras *vector.Rasterizer) {
		circlePath(ras, cx, cy, r, +1)
		circlePath(ras, cx, cy, r-thickness, -1)
	}, c)
}

// ── Çokgen ──────────────────────────────────────────────────────────────────

// Pt is a float pixel point.
type Pt struct{ X, Y float64 }

// FillPolygon paints a filled polygon (used for arrows, triangles, chevrons).
func (p *Painter) FillPolygon(pts []Pt, c color.RGBA) {
	if len(pts) < 3 {
		return
	}
	p.path(func(ras *vector.Rasterizer) {
		ras.MoveTo(float32(pts[0].X), float32(pts[0].Y))
		for _, q := range pts[1:] {
			ras.LineTo(float32(q.X), float32(q.Y))
		}
		ras.ClosePath()
	}, c)
}

// ── Renk yardımcıları ───────────────────────────────────────────────────────

// Blend mixes two colours: t=0 gives a, t=1 gives b.
//
// Vurgu tonları ve pasif durumlar için: paletin her varyantını elle tanımlamak
// yerine temel renkten türetiyoruz.
func Blend(a, b color.RGBA, t float64) color.RGBA {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x)*(1-t) + float64(y)*t))
	}
	return color.RGBA{
		R: mix(a.R, b.R),
		G: mix(a.G, b.G),
		B: mix(a.B, b.B),
		A: mix(a.A, b.A),
	}
}

// Alpha returns c with its alpha scaled by f (0..1).
//
// DİKKAT: Go'da color.RGBA ALFA ÖN ÇARPIMLIDIR (premultiplied). Yalnızca A
// alanını kısmak geçersiz bir renk üretir (R > A); draw.Over bunu aşırı parlak
// çizer ve saydam olması gereken bir rozet dolu görünür. Bu yüzden DÖRT kanal
// da ölçeklenir.
func Alpha(c color.RGBA, f float64) color.RGBA {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	sc := func(v uint8) uint8 { return uint8(math.Round(float64(v) * f)) }
	return color.RGBA{R: sc(c.R), G: sc(c.G), B: sc(c.B), A: sc(c.A)}
}
