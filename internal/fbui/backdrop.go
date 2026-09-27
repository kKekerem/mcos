package fbui

import (
	"image"
	"image/color"

	"mcos/internal/fbdraw"
)

// ── Işıklı arka plan (buzlu cam zemini) ─────────────────────────────────────
//
// Kullanıcı: "blurlu olsun hani netflix vardır ya çözünürlük tamdır ama
// düşük gibidir öyle olsun".
//
// Netflix hissi iki parçadan geliyor: arkada YUMUŞAK, renkli bir zemin ve
// onun üstünde yarı saydam, "buzlu" kartlar. Zemin her karede
// bulanıklaştırılmıyor: SoftLights ışıkları 1/8 çözünürlükte hesaplayıp tek
// geçişte, grenle büyütüyor ve sonuç önbelleğe alınıyor. Yeniden hesap
// YALNIZCA boyut ya da tema değişince yapılıyor; her karenin bedeli bir
// bellek kopyası (düz dolguyla aynı mertebe). Zemin durağan olduğu için hasar
// takibi de bozulmuyor: panel boştayken yine hiç kare üretilmiyor.

// Backdrop paints the ambient background into the canvas.
func (u *UI) Backdrop() {
	u.ensureBackdrop()
	copy(u.dst.Pix, u.bd.Pix)
}

// ensureBackdrop builds the backdrop and its glass layer if stale.
//
// ── Cam katmanı NEDEN önceden hesaplanıyor ──────────────────────────────────
//
// İlk denemede kartlar yarı saydam bir renkle dolduruluyordu. Ölçüldü
// (BenchmarkOlcum, 1080p bölüm çizimi): 2,1 ms -> 13,1 ms; profilde
// image/draw.drawFillOver sürenin %75'i — büyük dikdörtgenlerde piksel
// başına karıştırma. Zemin DURAĞAN olduğu için "camdan görünen hâli" de
// durağandır: bir kez hesaplanır, kart çizmek o katmandan satır KOPYALAMAK
// olur (düz dolguyla aynı maliyet).
func (u *UI) ensureBackdrop() {
	b := u.dst.Bounds()
	key := backdropKey{w: b.Dx(), h: b.Dy(), bg: u.Pal.Bg, accent: u.Pal.Accent, surface: u.Pal.Surface}
	if u.bd != nil && u.bdKey == key {
		return
	}
	u.bd = image.NewRGBA(b)
	fbdraw.SoftLights(u.bd, b, u.Pal.Bg, ambientLights(u.Pal), 1)
	u.gl = image.NewRGBA(b)
	s := u.Pal.Surface
	ga := glassAlpha
	a := int(ga*256 + 0.5)
	yuz := [3]int{int(s.R) * a, int(s.G) * a, int(s.B) * a}
	for i := 0; i < len(u.bd.Pix); i += 4 {
		for c := 0; c < 3; c++ {
			u.gl.Pix[i+c] = uint8((int(u.bd.Pix[i+c])*(256-a) + yuz[c] + 128) >> 8)
		}
		u.gl.Pix[i+3] = 255
	}
	u.bdKey = key
}

// GlassLayer returns the frosted surface layer (kart ve çubuk dolgusu).
func (u *UI) GlassLayer() *image.RGBA {
	u.ensureBackdrop()
	return u.gl
}

type backdropKey struct {
	w, h                int
	bg, accent, surface color.RGBA
}

// ambientLights derives the glows from the theme.
//
// Vurgu rengi sol üstte, onun soğuk bir eşi sağ altta. Güçleri BİLEREK düşük:
// metin kontrastı düz zemindekinden fark edilir biçimde düşmemeli; ışık
// "arkada bir şey var" hissi vermeli, dikkat çekmemeli.
func ambientLights(p Palette) []fbdraw.Light {
	soguk := fbdraw.Blend(p.Accent, color.RGBA{R: 0x3B, G: 0x5B, B: 0xDB, A: 0xFF}, 0.55)
	return []fbdraw.Light{
		{X: 0.12, Y: 0.08, R: 0.95, C: p.Accent, A: 0.24},
		{X: 0.92, Y: 0.96, R: 0.85, C: soguk, A: 0.17},
		{X: 0.58, Y: 0.40, R: 0.55, C: p.Accent, A: 0.06},
	}
}

// glassAlpha, kartların yüzey örtücülüğü: arkadaki ışığın ne kadarının
// camdan sızacağı. %80: ışık seçilebiliyor, metin zemini yine sakin.
const glassAlpha = 0.80

// Glass returns the translucent surface colour for cards over the backdrop.
func (u *UI) Glass() color.RGBA { return fbdraw.Alpha(u.Pal.Surface, glassAlpha) }

// BackdropExcept paints the backdrop everywhere except inside the holes.
//
// Ölçüldü (4K, BenchmarkOlcum): bölüm karesinin yarısı bellek kopyasıydı —
// önce TÜM ekrana zemin kopyalanıyor, sonra kenar çubuğu ve içerik kartı aynı
// alanın ~%90'ını camla YENİDEN yazıyordu. Kartın içi zaten tamamen
// örtüleceği için oraya zemin yazmak boşa 30 MB demek.
//
// Delikler çağıranın kart dikdörtgenleridir; köşe yarıçapı kadar İÇERİDEN
// kesilir: yuvarlak köşenin kenar yumuşatması altındaki zemine karışıyor,
// o bant zemin taşımalı.
func (u *UI) BackdropExcept(holes ...image.Rectangle) {
	u.ensureBackdrop()
	b := u.dst.Bounds()
	in := int(u.M.Radius+0.999) + 1
	var hs []image.Rectangle
	for _, h := range holes {
		h = image.Rect(h.Min.X+in, h.Min.Y+in, h.Max.X-in, h.Max.Y-in).Intersect(b)
		if !h.Empty() {
			hs = append(hs, h)
		}
	}
	stride := u.dst.Stride
	for y := b.Min.Y; y < b.Max.Y; y++ {
		x := b.Min.X
		for x < b.Max.X {
			// Bu satırda x'ten sonraki ilk deliği bul.
			next, skipTo := b.Max.X, b.Max.X
			for _, h := range hs {
				if y < h.Min.Y || y >= h.Max.Y || h.Max.X <= x {
					continue
				}
				if h.Min.X <= x {
					next, skipTo = x, h.Max.X
					break
				}
				if h.Min.X < next {
					next, skipTo = h.Min.X, h.Max.X
				}
			}
			if next > x {
				o := (y-b.Min.Y)*stride + (x-b.Min.X)*4
				n := (next - x) * 4
				copy(u.dst.Pix[o:o+n], u.bd.Pix[o:o+n])
			}
			x = skipTo
		}
	}
}
