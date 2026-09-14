package fbui

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"mcos/internal/fbdraw"
	"mcos/internal/fbfont"
)

// UI draws MCOS widgets onto a canvas.
//
// Her widget TEK PARÇA vektör şekiller kullanır; hiçbir çerçeve, köşe, çizgi
// veya işaret karakterden gelmez. Font yalnızca metin çizer.
type UI struct {
	P   *fbdraw.Painter
	F   *fbfont.Face
	Pal Palette
	M   Metrics
	dst *image.RGBA
}

// NewUI binds a canvas, a font and a palette together.
func NewUI(dst *image.RGBA, f *fbfont.Face, pal Palette) *UI {
	return &UI{
		P:   fbdraw.New(dst),
		F:   f,
		Pal: pal,
		M:   MetricsFor(f.CellW, f.CellH),
		dst: dst,
	}
}

// Clear paints the page background.
func (u *UI) Clear() { u.P.Fill(u.dst.Bounds(), u.Pal.Bg) }

// Bounds returns the canvas rectangle.
//
// Ekran kodu tuvalin boyutunu bilmek zorunda (kenar cubugu genisligi, alt
// cubugun yeri); dst alanini disari acmadan yalnizca sinirlari veriyoruz.
func (u *UI) Bounds() image.Rectangle { return u.dst.Bounds() }

// Canvas returns the image being drawn into.
//
// Geçiş efektleri (fbdraw.CrossFade, SlideBlend, Zoom) tuvalin KENDİSİNİ
// ister: pikselleri karıştırmak için widget katmanından geçmenin anlamı yok.
func (u *UI) Canvas() *image.RGBA { return u.dst }

// Pix returns the raw pixel buffer (RGBA, 4 bytes per pixel).
//
// Kare kopyalamak için: copy(dst, u.Pix()) tek bir memmove'dur; piksel piksel
// dolaşmak 2 milyon çağrı demektir.
func (u *UI) Pix() []uint8 { return u.dst.Pix }

// ── Metin ───────────────────────────────────────────────────────────────────

// Text draws s with its LEFT edge at x and its CELL TOP at y.
// Returns the x position just past the drawn text.
func (u *UI) Text(x, y int, s string, c color.RGBA) int {
	for _, r := range s {
		u.glyph(x, y, r, c)
		x += u.F.CellW
	}
	return x
}

// TextRight draws s so that it ENDS at x (right-aligned).
func (u *UI) TextRight(x, y int, s string, c color.RGBA) {
	u.Text(x-u.TextWidth(s), y, s, c)
}

// TextCenter draws s centred in the horizontal span [x0,x1).
func (u *UI) TextCenter(x0, x1, y int, s string, c color.RGBA) {
	w := u.TextWidth(s)
	u.Text(x0+(x1-x0-w)/2, y, s, c)
}

// WrapLines breaks s into lines that fit maxWidth pixels.
//
// ── Neden gerekli? ──────────────────────────────────────────────────────────
// Sabit genişlikte bir alana uzun bir açıklama yazmak, metnin panelin
// kenarından TAŞMASINA ve komşu sütunun üstüne binmesine yol açar. Kesmek
// (…) bilgiyi kaybettirir; kaydırmak kaybettirmez.
//
// Kelime sınırında kırar; tek bir kelime satıra sığmıyorsa (uzun bir URL
// gibi) onu zorla böler — aksi halde yine taşardı.
func (u *UI) WrapLines(s string, maxWidth int) []string {
	cols := maxWidth / u.F.CellW
	if cols < 4 {
		cols = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			cand := w
			if line != "" {
				cand = line + " " + w
			}
			if len([]rune(cand)) <= cols {
				line = cand
				continue
			}
			if line != "" {
				out = append(out, line)
				line = ""
			}
			// Tek kelime sığmıyorsa zorla böl.
			r := []rune(w)
			for len(r) > cols {
				out = append(out, string(r[:cols]))
				r = r[cols:]
			}
			line = string(r)
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// TextWrap draws word-wrapped text and returns the y just past the last line.
func (u *UI) TextWrap(x, y, maxWidth int, s string, c color.RGBA) int {
	for _, line := range u.WrapLines(s, maxWidth) {
		u.Text(x, y, line, c)
		y += u.F.CellH
	}
	return y
}

// TextWidth returns the pixel width of s on the cell grid.
func (u *UI) TextWidth(s string) int {
	n := 0
	for range s {
		n++
	}
	return n * u.F.CellW
}

// glyph composites one rasterised character.
func (u *UI) glyph(x, y int, r rune, c color.RGBA) {
	g := u.F.Glyph(r)
	if g.Missing {
		// Eksik glifi SESSİZCE yutma: görünür bir kutu çiz, böylece font
		// kapsaması sorunu ekranda hemen fark edilir.
		u.P.StrokeRoundRect(
			fbdraw.R(float64(x)+1, float64(y)+2, float64(u.F.CellW)-2, float64(u.F.CellH)-4),
			1, 1, u.Pal.Error)
		return
	}
	if g.Mask == nil {
		return // boşluk
	}
	dr := image.Rect(x+g.OffX, y+g.OffY, x+g.OffX+g.Mask.Bounds().Dx(), y+g.OffY+g.Mask.Bounds().Dy())
	draw.DrawMask(u.dst, dr.Intersect(u.dst.Bounds()),
		&image.Uniform{C: c}, image.Point{},
		g.Mask, g.Mask.Bounds().Min.Add(image.Pt(
			maxI(0, u.dst.Bounds().Min.X-(x+g.OffX)),
			maxI(0, u.dst.Bounds().Min.Y-(y+g.OffY)))),
		draw.Over)
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Pencere / kart ──────────────────────────────────────────────────────────

// Panel draws a rounded surface with a single-piece border.
//
// title boşsa başlık çubuğu çizilmez. focused ise kenarlık vurgu renginde ve
// daha kalındır — odak renkten BAĞIMSIZ olarak kalınlıktan da anlaşılır.
func (u *UI) Panel(r image.Rectangle, title string, focused bool) image.Rectangle {
	rc := fbdraw.R(float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy()))

	// Yüzey dolgusu, sonra tek parça çerçeve.
	u.P.FillRoundRect(rc, u.M.Radius, u.Pal.Surface)

	stroke, col := u.M.Stroke, u.Pal.Border
	if focused {
		stroke, col = u.M.StrokeFocus, u.Pal.BorderFocus
	}
	u.P.StrokeRoundRect(rc, u.M.Radius, stroke, col)

	inner := image.Rect(
		r.Min.X+u.M.PadX, r.Min.Y+u.M.PadY,
		r.Max.X-u.M.PadX, r.Max.Y-u.M.PadY,
	)

	if title != "" {
		u.Text(inner.Min.X, inner.Min.Y, title, u.Pal.Text)
		ty := inner.Min.Y + u.F.CellH + u.M.PadY/2
		u.P.HLine(float64(inner.Min.X), float64(inner.Max.X), float64(ty),
			u.M.DividerStroke, u.Pal.Divider)
		inner.Min.Y = ty + u.M.PadY
	}
	return inner
}

// Divider draws a horizontal separator across the given span.
func (u *UI) Divider(x0, x1, y int) {
	u.P.HLine(float64(x0), float64(x1), float64(y), u.M.DividerStroke, u.Pal.Divider)
}

// ── Buton ───────────────────────────────────────────────────────────────────

// ButtonStyle selects a button's visual weight.
type ButtonStyle int

const (
	// ButtonPrimary: dolu vurgu zemini. Ekranın ana eylemi.
	ButtonPrimary ButtonStyle = iota
	// ButtonSecondary: yalnızca çerçeve.
	ButtonSecondary
	// ButtonDanger: yıkıcı eylem (disk silme vb.).
	ButtonDanger
)

// Button draws a real button and returns the rectangle it occupied.
//
// Metin ile buton arasındaki fark artık tartışmasız: buton dolu/çerçeveli bir
// yüzeydir, metin değildir. Odaklı buton ayrıca dış hâle alır.
func (u *UI) Button(x, y int, label, key string, style ButtonStyle, focused bool) image.Rectangle {
	text := label
	if key != "" {
		text += "  " + key
	}
	w := u.TextWidth(text) + u.M.PadX*2
	if w < u.M.ButtonMinW {
		w = u.M.ButtonMinW
	}
	h := u.M.ButtonH
	rc := fbdraw.R(float64(x), float64(y), float64(w), float64(h))

	var fill, border, fg color.RGBA
	switch style {
	case ButtonPrimary:
		fill, border, fg = u.Pal.Accent, u.Pal.Accent, u.Pal.TextOn
	case ButtonDanger:
		fill, border, fg = u.Pal.Error, u.Pal.Error, u.Pal.TextOn
	default:
		fill, border, fg = u.Pal.Raised, u.Pal.Border, u.Pal.Text
	}

	// Odak hâlesi: butonun dışına soluk bir çerçeve. Renk körlüğünde de
	// görünür olsun diye kalınlık da artar.
	if focused {
		halo := fbdraw.R(rc.X-u.M.StrokeFocus, rc.Y-u.M.StrokeFocus,
			rc.W+2*u.M.StrokeFocus, rc.H+2*u.M.StrokeFocus)
		u.P.StrokeRoundRect(halo, u.M.RadiusSmall+u.M.StrokeFocus,
			u.M.StrokeFocus, fbdraw.Alpha(u.Pal.Accent, 0.55))
	}

	u.P.FillRoundRect(rc, u.M.RadiusSmall, fill)
	if style == ButtonSecondary {
		u.P.StrokeRoundRect(rc, u.M.RadiusSmall, u.M.Stroke, border)
	}

	ty := y + (h-u.F.CellH)/2
	u.TextCenter(x, x+w, ty, text, fg)

	return image.Rect(x, y, x+w, y+h)
}

// ButtonRow lays buttons left to right and returns each button's rectangle.
//
// DİKDÖRTGENLERİ DÖNDÜRÜR çünkü fare desteği bunu ister: tıklanabilir alan,
// çizilen alanla AYNI olmak zorundadır. İkisini ayrı hesaplamak, düzen
// değiştiğinde sessizce kayan tıklama alanları demektir.
func (u *UI) ButtonRow(x, y int, btns []Btn, focusIdx int) []image.Rectangle {
	out := make([]image.Rectangle, 0, len(btns))
	cur := x
	for i, b := range btns {
		r := u.Button(cur, y, b.Label, b.Key, b.Style, i == focusIdx)
		out = append(out, r)
		cur = r.Max.X + u.M.Gap*2
	}
	return out
}

// Btn describes one button in a row.
type Btn struct {
	Label string
	Key   string
	Style ButtonStyle
}

// ── Seçim işaretleri ────────────────────────────────────────────────────────

// Radio draws a radio marker: a ring, filled with a dot when selected.
// Karakterle (● / ○) değil, gerçek daire olarak çizilir.
func (u *UI) Radio(x, y int, selected bool) {
	cx := float64(x) + float64(u.F.CellW)/2
	cy := float64(y) + float64(u.F.CellH)/2
	r := u.M.MarkerR

	if selected {
		u.P.StrokeCircle(cx, cy, r, u.M.Stroke*1.4, u.Pal.Accent)
		u.P.FillCircle(cx, cy, r*0.5, u.Pal.Accent)
	} else {
		u.P.StrokeCircle(cx, cy, r, u.M.Stroke*1.2, u.Pal.Border)
	}
}

// Check draws a checkbox: a rounded square with a drawn tick when checked.
func (u *UI) Check(x, y int, checked bool) {
	s := u.M.MarkerR * 2
	rx := float64(x) + (float64(u.F.CellW)-s)/2
	ry := float64(y) + (float64(u.F.CellH)-s)/2
	rc := fbdraw.R(rx, ry, s, s)

	if checked {
		u.P.FillRoundRect(rc, s*0.25, u.Pal.Accent)
		// Onay işareti: iki çizgi segmenti — font glifi değil.
		u.P.Line(rx+s*0.24, ry+s*0.52, rx+s*0.44, ry+s*0.72, u.M.Stroke*1.5, u.Pal.TextOn)
		u.P.Line(rx+s*0.44, ry+s*0.72, rx+s*0.78, ry+s*0.28, u.M.Stroke*1.5, u.Pal.TextOn)
	} else {
		u.P.StrokeRoundRect(rc, s*0.25, u.M.Stroke*1.2, u.Pal.Border)
	}
}

// Chevron draws a directional arrow (used for cursors and "more" hints).
// dir: 0=right 1=left 2=up 3=down
func (u *UI) Chevron(x, y int, dir int, c color.RGBA) {
	w := float64(u.F.CellW)
	h := float64(u.F.CellH)
	cx, cy := float64(x)+w/2, float64(y)+h/2
	s := math.Min(w, h) * 0.30

	var pts []fbdraw.Pt
	switch dir {
	case 1: // sol
		pts = []fbdraw.Pt{{X: cx + s*0.7, Y: cy - s}, {X: cx - s*0.7, Y: cy}, {X: cx + s*0.7, Y: cy + s}}
	case 2: // yukarı
		pts = []fbdraw.Pt{{X: cx - s, Y: cy + s*0.7}, {X: cx, Y: cy - s*0.7}, {X: cx + s, Y: cy + s*0.7}}
	case 3: // aşağı
		pts = []fbdraw.Pt{{X: cx - s, Y: cy - s*0.7}, {X: cx, Y: cy + s*0.7}, {X: cx + s, Y: cy - s*0.7}}
	default: // sağ
		pts = []fbdraw.Pt{{X: cx - s*0.7, Y: cy - s}, {X: cx + s*0.7, Y: cy}, {X: cx - s*0.7, Y: cy + s}}
	}
	u.P.FillPolygon(pts, c)
}

// ── Liste satırı ────────────────────────────────────────────────────────────

// Row draws a selectable list row with a rounded highlight when selected.
// Returns the x where content may start.
func (u *UI) Row(r image.Rectangle, selected bool) int {
	if selected {
		rc := fbdraw.R(float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy()))
		u.P.FillRoundRect(rc, u.M.RadiusSmall, u.Pal.Raised)
		// Sol kenarda vurgu şeridi: seçim renkten bağımsız olarak da okunur.
		u.P.FillRoundRect(
			fbdraw.R(float64(r.Min.X), float64(r.Min.Y)+2, u.M.StrokeFocus*1.5, float64(r.Dy())-4),
			u.M.StrokeFocus*0.75, u.Pal.Accent)
	}
	return r.Min.X + u.M.PadX
}

// ── İlerleme ────────────────────────────────────────────────────────────────

// Progress draws a rounded progress bar. pct is 0..100.
func (u *UI) Progress(x, y, w int, pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	h := float64(u.F.CellH) * 0.45
	rad := h / 2 // tam yuvarlak uçlar

	track := fbdraw.R(float64(x), float64(y), float64(w), h)
	u.P.FillRoundRect(track, rad, u.Pal.Raised)

	fw := float64(w) * float64(pct) / 100
	if fw > 0 {
		// Çok küçük dolguda yarıçapı kıs, yoksa şekil bozulur.
		if fw < h {
			fw = h
		}
		u.P.FillRoundRect(fbdraw.R(float64(x), float64(y), fw, h), rad, u.Pal.Accent)
	}
}

// StatusDot draws a filled status indicator with a soft halo.
func (u *UI) StatusDot(x, y int, c color.RGBA) {
	cx := float64(x) + float64(u.F.CellW)/2
	cy := float64(y) + float64(u.F.CellH)/2
	u.P.FillCircle(cx, cy, u.M.MarkerR*1.5, fbdraw.Alpha(c, 0.25))
	u.P.FillCircle(cx, cy, u.M.MarkerR*0.75, c)
}

// WarnTriangle draws a filled warning triangle in one cell.
//
// Font glifi DEĞİL: gömülü yazı tipinde uyarı işareti yok (bkz. font_test.go).
// Çizilmiş üçgen her boyutta aynı görünür ve satır hizasını kaydırmaz.
func (u *UI) WarnTriangle(x, y int, c color.RGBA) {
	cx := float64(x) + float64(u.F.CellW)/2
	cy := float64(y) + float64(u.F.CellH)/2
	r := float64(u.F.CellH) * 0.40
	u.P.FillPolygon([]fbdraw.Pt{
		{X: cx, Y: cy - r},
		{X: cx + r*0.92, Y: cy + r*0.72},
		{X: cx - r*0.92, Y: cy + r*0.72},
	}, c)
}

// RowDimmed draws a list row highlight that means "this is the current
// section, but the keyboard is somewhere else".
//
// NEDEN: kullanıcı soldaki menüden sağa geçince hangisinin AKTİF olduğu belli
// olmuyordu — iki taraf da aynı parlaklıkta vurgulanıyordu. Artık odak
// neredeyse orası parlak, diğeri soluk kalıyor.
func (u *UI) RowDimmed(r image.Rectangle) int {
	u.P.FillRoundRect(
		fbdraw.R(float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy())),
		u.M.RadiusSmall, fbdraw.Blend(u.Pal.Bg, u.Pal.Raised, 0.55))
	u.P.FillRoundRect(
		fbdraw.R(float64(r.Min.X), float64(r.Min.Y)+2, u.M.StrokeFocus*1.5, float64(r.Dy())-4),
		u.M.StrokeFocus*0.75, fbdraw.Alpha(u.Pal.Accent, 0.45))
	return r.Min.X + u.M.PadX
}

// Badge draws a small rounded pill with text.
func (u *UI) Badge(x, y int, label string, c color.RGBA) image.Rectangle {
	w := u.TextWidth(label) + u.F.CellW*2
	h := u.F.CellH + u.F.CellH/3
	rc := fbdraw.R(float64(x), float64(y), float64(w), float64(h))
	u.P.FillRoundRect(rc, float64(h)/2, fbdraw.Alpha(c, 0.20))
	u.P.StrokeRoundRect(rc, float64(h)/2, u.M.Stroke, c)
	u.TextCenter(x, x+w, y+(h-u.F.CellH)/2, label, c)
	return image.Rect(x, y, x+w, y+h)
}
