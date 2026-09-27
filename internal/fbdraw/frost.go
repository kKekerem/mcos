package fbdraw

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// vectorRas kısaltmadır: bu dosyadaki yol kurucuların imzaları okunur kalsın.
type vectorRas = vector.Rasterizer

// ════════════════════════════════════════════════════════════════════════════
// DÜŞÜK ÇÖZÜNÜRLÜKLÜ KATMANLAR (BUZLU CAM)
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "blurlu olsun hani netflix vardır ya çözünürlük tamdır ama
// düşük gibidir öyle olsun".
//
// Netflix'in arayüzünde çıktı TAM çözünürlüktedir; metin ve ön plan keskin
// kalır. "Düşük gibi" görünen şey arka plan katmanlarıdır: bulanık, yumuşak.
// Bulanıklık yüksek frekansları zaten sildiği için o katmanı tam
// çözünürlükte hesaplamanın hiçbir görsel getirisi yoktur — 1/4 çözünürlükte
// hesaplayıp büyütmek AYNI görüntüyü 16 kat daha az pikselle verir.
//
// Bu dosyadaki her şey o fikrin parçaları:
//
//   - Small: küçük bir RGBA katman (ör. 1080p için 480x270).
//   - Downscale: tam çözünürlükten ortalama alarak küçültür.
//   - Blur: küçük katmanda kutu bulanıklığı (yarıçap küçük piksel cinsinden).
//   - Compose: TEK tam çözünürlük geçişinde büyütür, tonlar, keskin kareyle
//     karıştırır ve gren ekler. Ayrı ayrı yapılsaydı her biri 8 MB'lık bir
//     okuma+yazma daha demekti.
//
// Gren NEDEN var: yumuşak bir gradyanı 8 bite yuvarlamak bantlaşma yapar
// (koyu zeminde gözle görülen basamaklar). Büyütme, ara değerleri 8 kesirli
// bitle taşır; sabit desenli çok hafif bir titreşim (dither) eklenip ancak
// sonra yuvarlanır. Desen piksel konumuna bağlıdır, zamana değil: kareden
// kareye kıpırdamaz.

// Small is a low-resolution RGBA layer.
type Small struct {
	Pix  []uint8
	W, H int

	tmp []uint8 // bulanıklık ara tamponu, her karede yeniden ayrılmasın
}

func (s *Small) ensure(w, h int) {
	s.W, s.H = w, h
	n := w * h * 4
	if cap(s.Pix) < n {
		s.Pix = make([]uint8, n)
	}
	s.Pix = s.Pix[:n]
}

// CopyFrom makes s an exact copy of o (storage reused).
func (s *Small) CopyFrom(o *Small) {
	s.ensure(o.W, o.H)
	copy(s.Pix, o.Pix)
}

// Downscale box-averages region r of src by factor f into s.
//
// Ortalama alınır, örnek seçilmez: tek örnek (nearest) takma ad üretir ve
// büyütme sonrası titrek kenarlar olarak görünür.
func (s *Small) Downscale(src *image.RGBA, r image.Rectangle, f int) {
	r = r.Intersect(src.Bounds())
	if f < 1 {
		f = 1
	}
	sw, sh := r.Dx()/f, r.Dy()/f
	if sw < 1 {
		sw = 1
	}
	if sh < 1 {
		sh = 1
	}
	s.ensure(sw, sh)
	if r.Empty() {
		return
	}
	// Bölme yerine çarpma (bkz. magicDiv). Sihirli çarpan pencere genişliği
	// 81'e kadar doğrulandı, yani f en çok 8 (64 piksel) olabilir.
	if f > 8 {
		f = 8
		s.ensure(maxInt(1, r.Dx()/f), maxInt(1, r.Dy()/f))
		sw, sh = s.W, s.H
	}
	m, sft := magicDiv(f * f)
	forEachBand(sh, 16, func(ya, yz int) {
		for y := ya; y < yz; y++ {
			o := y * sw * 4
			for x := 0; x < sw; x++ {
				var a, b, c, d uint32
				for by := 0; by < f; by++ {
					sy := r.Min.Y + y*f + by
					if sy >= r.Max.Y {
						sy = r.Max.Y - 1
					}
					p := src.PixOffset(r.Min.X+x*f, sy)
					row := src.Pix[p : p+f*4 : p+f*4]
					for i := 0; i < len(row); i += 4 {
						a += uint32(row[i])
						b += uint32(row[i+1])
						c += uint32(row[i+2])
						d += uint32(row[i+3])
					}
				}
				s.Pix[o] = uint8((a * m) >> sft)
				s.Pix[o+1] = uint8((b * m) >> sft)
				s.Pix[o+2] = uint8((c * m) >> sft)
				s.Pix[o+3] = uint8((d * m) >> sft)
				o += 4
			}
		}
	})
}

// Blur applies the three-pass box blur (≈ Gauss) in place.
//
// radius KÜÇÜK piksel cinsindendir: 1/4 katmanda 3, tam çözünürlükte 12'ye
// denk gelir.
func (s *Small) Blur(radius int) {
	if radius < 1 || s.W < 1 || s.H < 1 {
		return
	}
	if radius > s.W {
		radius = s.W
	}
	if radius > s.H {
		radius = s.H
	}
	if cap(s.tmp) < len(s.Pix) {
		s.tmp = make([]uint8, len(s.Pix))
	}
	tmp := s.tmp[:len(s.Pix)]
	for i := 0; i < 3; i++ {
		boxBlurH(s.Pix, tmp, s.W, s.H, radius)
		boxBlurV(tmp, s.Pix, s.W, s.H, radius)
	}
}

// ComposeOpts controls the single full-resolution pass of Compose.
type ComposeOpts struct {
	// Sharp, karıştırılacak keskin karedir (dst ile aynı boyutta). nil ise
	// yalnızca büyütülmüş katman yazılır.
	Sharp *image.RGBA
	// Mix: 0 = tamamen keskin kare, 1 = tamamen bulanık katman. Sharp nil
	// ise yok sayılır.
	Mix float64
	// Tint / TintAmt: bulanık katman bu renge doğru çekilir (karartma ya da
	// cam tonu). 0 = ton yok.
	Tint    color.RGBA
	TintAmt float64
	// Grain: titreşim genliği (8 bitlik birim). 0 kapalı; 1-2 bantlaşmayı
	// siler, 3-4 hafif bir film greni verir.
	Grain int
}

// Compose upsamples s bilinearly over r in dst, tinting, mixing with a sharp
// frame and dithering in one pass.
//
// Küçük pikseller MERKEZLERİNDEN hizalanır: (i+0,5)*f-0,5. Köşeden
// hizalamak katmanı f/2 piksel sola-yukarı kaydırırdı; buzlu cam, altındaki
// içeriğe göre kaymış görünürdü.
func (s *Small) Compose(dst *image.RGBA, r image.Rectangle, o ComposeOpts) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() || s.W < 1 || s.H < 1 {
		return
	}
	if o.Sharp != nil && o.Sharp.Bounds() != dst.Bounds() {
		o.Sharp = nil
	}
	mix := 256
	if o.Sharp != nil {
		mix = int(clamp01(o.Mix)*256 + 0.5)
		if mix == 0 {
			if o.Sharp != dst {
				copyRect(dst, o.Sharp, r)
			}
			return
		}
	}
	ta := int(clamp01(o.TintAmt)*256 + 0.5)
	tint := [4]int{int(o.Tint.R), int(o.Tint.G), int(o.Tint.B), int(o.Tint.A)}
	grain := o.Grain
	if grain < 0 {
		grain = 0
	}

	w, h := r.Dx(), r.Dy()
	// Katman r'yi kapsar: küçük piksel başına kaç tam piksel?
	xi, xw := bilinearTaps(w, s.W)
	yi, yw := bilinearTaps(h, s.H)

	forEachBand(h, 24, func(ya, yz int) {
		n := w * 4
		rowA := make([]uint16, n)
		rowB := make([]uint16, n)
		// -2: "hiçbir satır hesaplanmadı". -1 ile başlanınca, kaynak satırı
		// 0 olan ilk satır "y0 == cached+1" dalına düşüyordu; o dal bir
		// ÖNCEKİ satırın rowB'de hazır olduğunu varsayıp yalnızca yenisini
		// hesaplar. Hazır satır yoktu: rowA sıfır kaldı ve ekranın en üstteki
		// satırları SİYAHA karıştı (ölçüldü: 1080p'de ilk 4 satır 0,0,0).
		cached := -2
		for y := ya; y < yz; y++ {
			y0 := int(yi[y])
			switch {
			case y0 == cached:
			case y0 == cached+1:
				rowA, rowB = rowB, rowA
				lerpRow16(rowB, s, minInt(y0+1, s.H-1), xi, xw)
				cached = y0
			default:
				lerpRow16(rowA, s, y0, xi, xw)
				lerpRow16(rowB, s, minInt(y0+1, s.H-1), xi, xw)
				cached = y0
			}
			wy := int(yw[y])
			dy := r.Min.Y + y
			p := dst.PixOffset(r.Min.X, dy)
			dp := dst.Pix[p : p+n : p+n]
			var sp []uint8
			if o.Sharp != nil {
				q := o.Sharp.PixOffset(r.Min.X, dy)
				sp = o.Sharp.Pix[q : q+n : q+n]
			}
			ra := rowA[:n:n]
			rb := rowB[:n:n]
			if sp == nil {
				composeRowFast(dp, ra, rb, w, wy, ta, tint, grain, r.Min.X, dy)
				continue
			}
			for x := 0; x < w; x++ {
				i := x * 4
				// Titreşim: konuma bağlı, zamana değil. 8 kesirli bitte
				// eklenir ki yuvarlamadan ÖNCE etkisini göstersin.
				var g int
				if grain > 0 {
					g = grainAt(r.Min.X+x, dy, grain)
				}
				for c := 0; c < 4; c++ {
					a := int(ra[i+c])
					v := a + ((int(rb[i+c])-a)*wy)>>8 // 8 kesirli bit
					if ta > 0 {
						v = (v*(256-ta) + tint[c]*256*ta) >> 8
					}
					if c < 3 {
						v += g
					}
					if sp != nil && mix < 256 {
						v = (int(sp[i+c])*256*(256-mix) + v*mix) >> 8
					}
					v = (v + 128) >> 8
					if v < 0 {
						v = 0
					} else if v > 255 {
						v = 255
					}
					dp[i+c] = uint8(v)
				}
			}
		}
	})
}

// composeRowFast is Compose's inner loop when no sharp frame is mixed in.
//
// Perde ve arka plan bu yoldan geçiyor. Genel döngü her piksel VE her kanal
// için dört koşul sınıyordu (ton var mı, gren var mı, keskin kare var mı,
// karışım tam mı); profilde Compose tek başına kare süresinin %48'iydi
// (1080p pencere ilk karesi). Burada koşullar döngü dışında çözülüyor ve ton
// sabitleri önceden çarpılıyor.
func composeRowFast(dp []uint8, ra, rb []uint16, w, wy, ta int, tint [4]int, grain, x0, dy int) {
	inv := 256 - ta
	var tt [4]int
	for c := range tt {
		tt[c] = tint[c] * 256 * ta
	}
	for x := 0; x < w; x++ {
		i := x * 4
		g := 0
		if grain > 0 {
			g = grainAt(x0+x, dy, grain)
		}
		for c := 0; c < 4; c++ {
			a := int(ra[i+c])
			v := a + ((int(rb[i+c])-a)*wy)>>8
			if ta > 0 {
				v = (v*inv + tt[c]) >> 8
			}
			if c < 3 {
				v += g
			}
			v = (v + 128) >> 8
			if v > 255 {
				v = 255
			} else if v < 0 {
				v = 0
			}
			dp[i+c] = uint8(v)
		}
	}
}

// bilinearTaps returns, for each destination index, the lower source index and
// the 8-bit weight of the upper one (centre-aligned sampling).
func bilinearTaps(dstN, srcN int) ([]int32, []int32) {
	idx := make([]int32, dstN)
	wt := make([]int32, dstN)
	if srcN <= 1 {
		return idx, wt
	}
	// 16.16 sabit nokta: (x+0,5)*srcN/dstN - 0,5
	step := (srcN << 16) / dstN
	pos := step/2 - (1 << 15)
	for x := 0; x < dstN; x++ {
		p := pos
		if p < 0 {
			p = 0
		}
		i0 := p >> 16
		f := (p & 0xFFFF) >> 8
		if i0 >= srcN-1 {
			i0 = srcN - 1
			f = 0
		}
		idx[x] = int32(i0)
		wt[x] = int32(f)
		pos += step
	}
	return idx, wt
}

// lerpRow16 interpolates small row y horizontally into out, keeping 8
// fractional bits (value*256) so the vertical pass and the dither see the
// in-between levels instead of an already-rounded byte.
func lerpRow16(out []uint16, s *Small, y int, xi, xw []int32) {
	row := s.Pix[y*s.W*4 : (y+1)*s.W*4]
	last := (s.W - 1) * 4
	n := len(out) / 4
	for x := 0; x < n; x++ {
		i0 := int(xi[x]) * 4
		i1 := i0 + 4
		if i1 > last {
			i1 = last
		}
		wx := int(xw[x])
		o := out[x*4 : x*4+4 : x*4+4]
		for c := 0; c < 4; c++ {
			a := int(row[i0+c])
			o[c] = uint16(a*256 + (int(row[i1+c])-a)*wx)
		}
	}
}

// grainAt returns a fixed per-pixel dither in [-amp, amp] 8-bit units, scaled
// by 256 (so it lands in the 8 fractional bits of Compose).
//
// Üçgen dağılım (iki eşit dağılımın farkı): düz gürültüden daha az "karlı"
// görünür ve aynı genlikte bantlaşmayı daha iyi siler.
func grainAt(x, y, amp int) int {
	h := uint32(x)*0x9E3779B1 ^ uint32(y)*0x85EBCA77
	h ^= h >> 15
	h *= 0x2C1B3C6D
	h ^= h >> 12
	a := int(h & 0xFF)
	b := int((h >> 8) & 0xFF)
	return (a - b) * amp // (-255..255)*amp: 1/256 birimde ±amp
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BlurScaled blurs region r at 1/scale resolution and writes it back.
//
// Blur ile aynı iş, ama küçültme oranını ÇAĞIRAN seçer: uyarlamalı kalite
// (kare bütçesi aşılınca 1/2 -> 1/4 -> 1/8) buna dayanır. scale 1 ise tam
// çözünürlükte bulanıklaştırır (referans yolu, ölçüm için).
func BlurScaled(dst *image.RGBA, r image.Rectangle, radius, scale int) {
	r = r.Intersect(dst.Bounds())
	if radius <= 0 || r.Empty() {
		return
	}
	if scale <= 1 {
		w, h := r.Dx(), r.Dy()
		if radius > w {
			radius = w
		}
		if radius > h {
			radius = h
		}
		buf := extractRegion(dst, r)
		blurBuf(buf, r.Dx(), r.Dy(), radius)
		writeRegion(dst, r, buf)
		return
	}
	var s Small
	s.Downscale(dst, r, scale)
	rs := (radius + scale/2) / scale
	if rs < 1 {
		rs = 1
	}
	s.Blur(rs)
	s.Compose(dst, r, ComposeOpts{})
}

// ── Yumuşak ışık lekeleri ───────────────────────────────────────────────────

// Light is one soft coloured glow of an ambient backdrop.
//
// X, Y bölgenin kesri olarak merkezdir (0..1); R, bölgenin KISA kenarının
// kesri olarak yarıçaptır. A, merkezdeki en yüksek katkıdır (0..1).
type Light struct {
	X, Y, R float64
	C       color.RGBA
	A       float64
}

// SoftLights paints bg plus a few very soft glows over r in dst.
//
// Netflix'teki bulanık arka plan hissi: düz siyah değil, arkada yavaşça
// dolaşan renkli ışıklar. Hesap 1/8 çözünürlükte yapılır (1080p'de 240x135:
// 32 bin piksel) ve Compose ile tek geçişte, grenle büyütülür. Yani bedeli
// neredeyse yalnızca tam çözünürlükte bir kez YAZMAKTIR; üstelik çağıran bunu
// önbelleğe alır ve yalnızca değiştiğinde yeniden hesaplar.
func SoftLights(dst *image.RGBA, r image.Rectangle, bg color.RGBA, lights []Light, grain int) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	const f = 8
	var s Small
	sw, sh := (r.Dx()+f-1)/f, (r.Dy()+f-1)/f
	s.ensure(sw, sh)
	short := math.Min(float64(r.Dx()), float64(r.Dy())) / f

	type lt struct {
		cx, cy, inv2s2 float64
		c              [3]float64
	}
	ls := make([]lt, 0, len(lights))
	for _, l := range lights {
		sig := l.R * short / 2
		if sig <= 0 {
			continue
		}
		ls = append(ls, lt{
			cx: l.X * float64(sw), cy: l.Y * float64(sh),
			inv2s2: 1 / (2 * sig * sig),
			c:      [3]float64{float64(l.C.R) * l.A, float64(l.C.G) * l.A, float64(l.C.B) * l.A},
		})
	}
	base := [3]float64{float64(bg.R), float64(bg.G), float64(bg.B)}
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			v := base
			for _, l := range ls {
				dx, dy := float64(x)+0.5-l.cx, float64(y)+0.5-l.cy
				k := math.Exp(-(dx*dx + dy*dy) * l.inv2s2)
				// "Ekran" karışımı: ışıklar üst üste binince doygunluğa
				// yaklaşır ama 255'i aşmaz ve beyaza patlamaz.
				for c := 0; c < 3; c++ {
					add := l.c[c] * k
					v[c] = v[c] + add - v[c]*add/255
				}
			}
			o := (y*sw + x) * 4
			for c := 0; c < 3; c++ {
				s.Pix[o+c] = uint8(math.Round(math.Min(255, math.Max(0, v[c]))))
			}
			s.Pix[o+3] = 255
		}
	}
	s.Compose(dst, r, ComposeOpts{Grain: grain})
}

// ── Görüntüden dolgu (cam paneller) ─────────────────────────────────────────

// FillRoundRectImage fills a rounded rectangle with the pixels of src at the
// same coordinates (src must share dst's coordinate space).
//
// Buzlu cam paneli böyle çizilir: arka plan katmanının "camdan görünen"
// hâli önceden hesaplanmış bir görüntüdür ve panel onun o bölgesini
// KOPYALAR. Düz dolgu gibi ucuzdur (orta kısım satır satır bellek
// kopyası), köşeler yine kenar yumuşatmalıdır.
func (p *Painter) FillRoundRectImage(rc Rect, radius float64, src *image.RGBA) {
	if src == nil || rc.W <= 0 || rc.H <= 0 {
		return
	}
	if fastEligible(rc, radius) {
		x0, y0 := int(rc.X), int(rc.Y)
		x1, y1 := int(rc.X+rc.W), int(rc.Y+rc.H)
		R := int(math.Ceil(radius))
		p.copyFrom(src, image.Rect(x0+R, y0, x1-R, y1))
		p.copyFrom(src, image.Rect(x0, y0+R, x0+R, y1-R))
		p.copyFrom(src, image.Rect(x1-R, y0+R, x1, y1-R))
		if R == 0 {
			return
		}
		for q := 0; q < 4; q++ {
			qq := q
			p.pathImage(cornerBox(rc, q, float64(R)), func(ras *vectorRas, ox, oy float32) {
				cornerFillPath(ras, cornerOf(rc, qq, ox, oy), float32(radius), float32(R))
			}, src)
		}
		return
	}
	p.pathImage(rc, func(ras *vectorRas, ox, oy float32) {
		roundRectPath(ras, rc, radius, +1, ox, oy)
	}, src)
}

// pathImage is path() with an image source instead of a solid colour.
func (p *Painter) pathImage(box Rect, build func(r *vector.Rasterizer, ox, oy float32),
	src *image.RGBA) {
	clip, ok := p.clipFor(box)
	if !ok {
		return
	}
	p.ras.Reset(clip.Dx(), clip.Dy())
	p.ras.DrawOp = draw.Over
	build(p.ras, float32(clip.Min.X), float32(clip.Min.Y))
	// src tuvalle aynı koordinat uzayında: kaynak noktası kırpmanın köşesi.
	p.ras.Draw(p.dst, clip, src, clip.Min)
}

// copyFrom copies r from src (opaque) into the canvas.
func (p *Painter) copyFrom(src *image.RGBA, r image.Rectangle) {
	r = r.Intersect(p.dst.Bounds()).Intersect(src.Bounds())
	if r.Empty() {
		return
	}
	w := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := p.dst.PixOffset(r.Min.X, y)
		s := src.PixOffset(r.Min.X, y)
		copy(p.dst.Pix[o:o+w], src.Pix[s:s+w])
	}
}

// ── Paralel karışım ─────────────────────────────────────────────────────────

// blendRows is CrossFade's inner loop over rows [ya, yz) of r.
func blendRows(dst, from *image.RGBA, r image.Rectangle, wNew, wOld, ya, yz int) {
	n := r.Dx() * 4
	for y := r.Min.Y + ya; y < r.Min.Y+yz; y++ {
		o := dst.PixOffset(r.Min.X, y)
		dp := dst.Pix[o : o+n : o+n]
		fp := from.Pix[o : o+n : o+n]
		for i := range dp {
			dp[i] = uint8((int(dp[i])*wNew + int(fp[i])*wOld) >> 8)
		}
	}
}
