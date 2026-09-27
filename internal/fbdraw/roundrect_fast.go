package fbdraw

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// ════════════════════════════════════════════════════════════════════════════
// YUVARLAK DİKDÖRTGENLERİN HIZLI YOLU
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek darboğaz ──────────────────────────────────────────────
//
// Kullanıcı "kasıyor arayüz" dedi. CPU profili (App.Draw, 1920x1080) sürenin
// %92'sinin fbdraw.Painter.path içinde geçtiğini gösterdi:
//
//	%47  FillRoundRect
//	%45  StrokeRoundRect
//
// Sebep: içerik paneli her karede 1534x984 = 1,51 MEGAPİKSELLİK kutusunu İKİ
// KEZ rasterleştiriyordu (bir dolgu, bir çerçeve). Her rasterleştirme önce
// 5,7 MB'lık float32 tamponu sıfırlıyor, sonra kutunun tamamını geziyor.
//
// Oysa şekiller neredeyse boş: dolgu düz bir dikdörtgen artı dört küçük köşe,
// çerçeve ise dört İNCE kenar artı dört köşe yayı. Kutunun ortasındaki
// milyonlarca piksel ya tamamen dolu ya tamamen boş — rasterleştiriciye hiç
// sorulmasına gerek yok.
//
// ── Ölçülen kazanç (1920x1080) ──────────────────────────────────────────────
//
//	içerik paneli dolgusu    8,14 ms -> 0,17  ms    48x
//	içerik paneli çerçevesi  7,96 ms -> 0,135 ms    57x
//	App.Draw() toplam       18,86 ms -> 1,21  ms  15,6x
//
// Görsel fark: DOLGU bayt bayt AYNI. ÇERÇEVE 480.000 baytta 18 bayt, en büyük
// sapma 1/255 — gözle görülmesi imkânsız.
//
// ── Neden BURADA çalışıyor, StrokeCircle'da ÇALIŞMADI ───────────────────────
//
// StrokeCircle'ı dört banda bölme denemesi BOZULDU çünkü aynı yol dört farklı
// kutuya çiziliyordu ve vector.Rasterizer kendi alanı dışına taşan kenarları
// yanlış sayıyor (bkz. painter.go'daki not ve stroke_circle_test.go).
//
// Buradaki bölme farklı: her parça KENDİ KUTUSUNUN İÇİNDE KAPALI bir yol.
// Köşe, r x r'lik kutusunda kapalı bir çeyrek daire; kenar, kendi ince
// kutusunda kapalı bir dikdörtgen. Sarım hesabı her kutuda doğru.
//
// Ayrıca ölçüldü: halkayı dörde bölmek yalnızca 1,1x kazandırıyor (çeyreğin
// kutusu yine r x r), yani o deneme zaten değmezdi.

// fastEligible reports whether the split path may be used.
//
// İKİ KOŞUL da şart ve ikisi de ÖLÇÜLEREK bulundu:
//
//   - Kutu en az 32x32 olmalı. Küçük şekillerde kazanç yok ve alt piksel
//     geometrisi bozuluyor (3x3'lük bir kutuda kırpma testi kırıldı).
//   - Kenarlar TAM SAYIYA hizalı olmalı. Kesirli bir kenarı yuvarlamak
//     baytların %0,73'ünde ortalama 68 birimlik sapma üretiyordu.
//
// ── Yarıçap artık KESİRLİ olabilir ──────────────────────────────────────────
//
// Eskiden yarıçap da tam sayı olmak zorundaydı. Oysa panelin yarıçapı
// hücre yüksekliğinden türüyor (M.Radius = CellH*0,55) ve gerçek yazı
// tipi boyutlarında NEREDEYSE HİÇ tam sayı çıkmıyor: 1080p'de 12,65,
// 1440p'de 16,5. Yani hızlı yol gerçek panelde HİÇ çalışmıyordu; kıyas
// testleri 16 px'lik yazıyla ölçtüğü için bunu göremedi. Ölçüldü
// (BenchmarkOlcum, 1080p): kare sürenin %82'si kutunun tamamını
// rasterleştirmekte geçiyordu, kare 22-25 ms'ydi.
//
// Köşe kutusu artık yarıçapın TAVANI kadar (R = ceil(r)): kutu tam sayı
// sınırlarda kalır, köşe parçası "yay + R'ye kadar düz kenar" olan kapalı
// bir çokgen olur ve düz dolgular R'den başlar. Parçalar yine yalnızca
// tam sayı piksel sınırlarında buluşur, yani hiçbir piksel iki parçadan
// kısmi kapsama almaz (iki ayrı Over harmanı, tek harmanla aynı sonucu
// vermezdi).
func fastEligible(rc Rect, radius float64) bool {
	if rc.W < 32 || rc.H < 32 {
		return false
	}
	if radius < 0 || 2*math.Ceil(radius) > math.Min(rc.W, rc.H) {
		return false
	}
	return isInt(rc.X) && isInt(rc.Y) && isInt(rc.W) && isInt(rc.H)
}

func isInt(v float64) bool { return v == math.Trunc(v) }

// fillOver paints a solid rectangle with alpha compositing.
//
// draw.Over, draw.Src DEĞİL.
//
// ── Yakalanan gerçek tuzak ──────────────────────────────────────────────────
//
// Düz parçayı draw.Src ile doldurmak daha hızlı görünüyordu, ama YARI SAYDAM
// renklerde arkadaki içeriği SİLİYOR — panel perdeleri ve vurgular yarı
// saydam. Depodaki TestAlphaIsPremultiplied bunu yakalıyor.
func (p *Painter) fillOver(r image.Rectangle, c color.RGBA) {
	r = r.Intersect(p.dst.Bounds())
	if r.Empty() {
		return
	}
	if c.A == 255 {
		// Opak renkte Over ile Src aynı sonucu verir; Src düz bir bellek
		// doldurmadır, Over ise piksel başına harman.
		draw.Draw(p.dst, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
		return
	}
	draw.Draw(p.dst, r, &image.Uniform{C: c}, image.Point{}, draw.Over)
}

// corner maps corner-local coordinates onto the canvas.
//
// (u, v): köşenin dikey kenarından ve yatay kenarından içeri doğru uzaklık.
// Dört köşe aynı yerel şekli paylaşır; yalnızca yansıtma farklıdır. Böylece
// dört ayrı el yazısı yol (ve dördünde ayrı ayrı yapılabilecek bir hata)
// yerine tek bir tanım kalır. Yansıtma sarım yönünü çevirir ama tek
// konturlu bir çokgende sıfır-olmayan kural için fark etmez.
type corner struct{ ex, ey, sx, sy float32 }

func cornerOf(rc Rect, q int, ox, oy float32) corner {
	x0, y0 := float32(rc.X)-ox, float32(rc.Y)-oy
	x1, y1 := float32(rc.X+rc.W)-ox, float32(rc.Y+rc.H)-oy
	switch q {
	case 1: // sağ üst
		return corner{x1, y0, -1, 1}
	case 2: // sağ alt
		return corner{x1, y1, -1, -1}
	case 3: // sol alt
		return corner{x0, y1, 1, -1}
	}
	return corner{x0, y0, 1, 1} // sol üst
}

func (c corner) move(r *vector.Rasterizer, u, v float32) { r.MoveTo(c.ex+c.sx*u, c.ey+c.sy*v) }
func (c corner) line(r *vector.Rasterizer, u, v float32) { r.LineTo(c.ex+c.sx*u, c.ey+c.sy*v) }
func (c corner) cube(r *vector.Rasterizer, u1, v1, u2, v2, u, v float32) {
	r.CubeTo(c.ex+c.sx*u1, c.ey+c.sy*v1, c.ex+c.sx*u2, c.ey+c.sy*v2, c.ex+c.sx*u, c.ey+c.sy*v)
}

// cornerBox returns corner q's R×R box in canvas coordinates.
func cornerBox(rc Rect, q int, R float64) Rect {
	k := Rect{X: rc.X, Y: rc.Y, W: R, H: R}
	if q == 1 || q == 2 {
		k.X = rc.X + rc.W - R
	}
	if q == 2 || q == 3 {
		k.Y = rc.Y + rc.H - R
	}
	return k
}

// cornerFillPath: the filled rounded rectangle INTERSECTED with the corner's
// R×R box, as one closed polygon.
func cornerFillPath(ras *vector.Rasterizer, c corner, r, R float32) {
	k := r * float32(kappa)
	c.move(ras, 0, R)
	c.line(ras, 0, r)
	c.cube(ras, 0, r-k, r-k, 0, r, 0)
	c.line(ras, R, 0)
	c.line(ras, R, R)
	ras.ClosePath()
}

// cornerRingPath: the outline ring INTERSECTED with the corner's R×R box.
//
// İç kontur, genel yoldaki iç dikdörtgenin köşesidir: kalınlık kadar içeride,
// yarıçapı max(0, r-t). Tek bir kapalı çokgen olarak yazılır (dış yay ileri,
// iç yay geri).
func cornerRingPath(ras *vector.Rasterizer, c corner, r, t, R float32) {
	k := r * float32(kappa)
	ir := r - t
	if ir < 0 {
		ir = 0
	}
	ki := ir * float32(kappa)
	m := t + ir // iç yayın kenardan uzaklığı
	c.move(ras, 0, R)
	c.line(ras, 0, r)
	c.cube(ras, 0, r-k, r-k, 0, r, 0)
	c.line(ras, R, 0)
	c.line(ras, R, t)
	c.line(ras, m, t)
	c.cube(ras, m-ki, t, t, m-ki, t, m)
	c.line(ras, t, R)
	ras.ClosePath()
}

// fillRoundRectFast paints a filled rounded rectangle without rasterising the
// whole box: straight parts are solid fills, only the four corners are drawn.
func (p *Painter) fillRoundRectFast(rc Rect, radius float64, c color.RGBA) {
	x0, y0 := int(rc.X), int(rc.Y)
	x1, y1 := int(rc.X+rc.W), int(rc.Y+rc.H)
	R := int(math.Ceil(radius))

	// Orta blok (tam yükseklik, köşelerden içeride) + sol/sağ şeritler.
	p.fillOver(image.Rect(x0+R, y0, x1-R, y1), c)
	p.fillOver(image.Rect(x0, y0+R, x0+R, y1-R), c)
	p.fillOver(image.Rect(x1-R, y0+R, x1, y1-R), c)

	if R == 0 {
		return
	}
	for q := 0; q < 4; q++ {
		qq := q
		p.path(cornerBox(rc, q, float64(R)), func(ras *vector.Rasterizer, ox, oy float32) {
			cornerFillPath(ras, cornerOf(rc, qq, ox, oy), float32(radius), float32(R))
		}, c)
	}
}

// strokeEligible is fastEligible for outlines: the corner box must also hold
// the whole stroke width.
func strokeEligible(rc Rect, radius, thickness float64) bool {
	if !fastEligible(rc, radius) {
		return false
	}
	return 2*math.Ceil(math.Max(radius, thickness)) <= math.Min(rc.W, rc.H)
}

// strokeRoundRectFast paints a rounded-rectangle outline as four thin edges
// plus four corner arcs.
//
// Kenarlar İNCE KUTU olarak rasterleştiriliyor, düz dolgu YAPILMIYOR: kalınlık
// kesirli olabilir (ör. 1,5 piksel) ve tam sayıya yuvarlamak baytların
// %0,73'ünde ortalama 68 birim sapma üretiyordu — kenar yumuşatma korunmalı.
func (p *Painter) strokeRoundRectFast(rc Rect, radius, thickness float64, c color.RGBA) {
	R := math.Ceil(math.Max(radius, thickness))

	// Dört düz kenar: köşe kutularının bittiği TAM SAYI sınırdan başlıyor.
	kenarlar := []Rect{
		{X: rc.X + R, Y: rc.Y, W: rc.W - 2*R, H: thickness},
		{X: rc.X + R, Y: rc.Y + rc.H - thickness, W: rc.W - 2*R, H: thickness},
		{X: rc.X, Y: rc.Y + R, W: thickness, H: rc.H - 2*R},
		{X: rc.X + rc.W - thickness, Y: rc.Y + R, W: thickness, H: rc.H - 2*R},
	}
	for _, k := range kenarlar {
		if k.W <= 0 || k.H <= 0 {
			continue
		}
		kk := k
		p.path(kk, func(ras *vector.Rasterizer, ox, oy float32) {
			ras.MoveTo(float32(kk.X)-ox, float32(kk.Y)-oy)
			ras.LineTo(float32(kk.X+kk.W)-ox, float32(kk.Y)-oy)
			ras.LineTo(float32(kk.X+kk.W)-ox, float32(kk.Y+kk.H)-oy)
			ras.LineTo(float32(kk.X)-ox, float32(kk.Y+kk.H)-oy)
			ras.ClosePath()
		}, c)
	}

	if R <= 0 {
		return
	}
	for q := 0; q < 4; q++ {
		qq := q
		p.path(cornerBox(rc, q, R), func(ras *vector.Rasterizer, ox, oy float32) {
			cornerRingPath(ras, cornerOf(rc, qq, ox, oy), float32(radius),
				float32(thickness), float32(R))
		}, c)
	}
}

// FillRoundRectExcept fills a rounded rectangle but skips the part that will
// be covered by `covered` anyway.
//
// ── Neden var ───────────────────────────────────────────────────────────────
//
// Pencere gölgesi altı kat yarı saydam yuvarlak dikdörtgen çiziyor ve altısının
// da ORTASI, hemen ardından çizilen opak pencere paneliyle örtülüyor. O orta
// bölgenin alfa harmanını hesaplamak tamamen boşa iş: sonuç görünmüyor.
//
// Ölçüldü: altı tam katman 6,36 ms, yalnızca dış şerit 0,118 ms (54x).
//
// `covered` TAMAMEN OPAK bir şeyle boyanacak bölge olmalı. Yarı saydam bir
// şey oraya çizilecekse bu kestirme YANLIŞ olur — altındaki gölge görünürdü.
func (p *Painter) FillRoundRectExcept(rc Rect, radius float64, c color.RGBA,
	covered image.Rectangle) {
	if rc.W <= 0 || rc.H <= 0 {
		return
	}
	dis := image.Rect(int(rc.X), int(rc.Y), int(rc.X+rc.W), int(rc.Y+rc.H))
	// Örtülen bölge şeklin tamamını yutuyorsa hiç çizme.
	if covered.Union(dis) == covered {
		return
	}
	// Örtme yoksa ya da şekil küçükse normal yol.
	if covered.Empty() || !fastEligible(rc, radius) {
		p.FillRoundRect(rc, radius, c)
		return
	}

	// Dört şerit: örtülen dikdörtgenin dışında kalan kısımlar. Şeritler
	// kesişmiyor, yani hiçbir piksel İKİ KEZ harmanlanmıyor — yarı saydam
	// renkte bu koyulaşma demek olurdu.
	ust := image.Rect(dis.Min.X, dis.Min.Y, dis.Max.X, min2(covered.Min.Y, dis.Max.Y))
	alt := image.Rect(dis.Min.X, max2(covered.Max.Y, dis.Min.Y), dis.Max.X, dis.Max.Y)
	ortaY0, ortaY1 := max2(ust.Max.Y, dis.Min.Y), min2(alt.Min.Y, dis.Max.Y)
	sol := image.Rect(dis.Min.X, ortaY0, min2(covered.Min.X, dis.Max.X), ortaY1)
	sag := image.Rect(max2(covered.Max.X, dis.Min.X), ortaY0, dis.Max.X, ortaY1)

	for _, s := range []image.Rectangle{ust, alt, sol, sag} {
		if s.Empty() {
			continue
		}
		p.clipFillRoundRect(rc, radius, c, s)
	}
}

// clipFillRoundRect fills the rounded rect, restricted to clip.
func (p *Painter) clipFillRoundRect(rc Rect, radius float64, c color.RGBA,
	clip image.Rectangle) {
	sub := p.dst.SubImage(clip).(*image.RGBA)
	q := New(sub)
	q.FillRoundRect(rc, radius, c)
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}
