package fbdraw

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/vector"
)

// ════════════════════════════════════════════════════════════════════════════
// HALKA: DÖRT BANT, TEK KUTUYLA AYNI PİKSELİ ÜRETMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu test hayati ────────────────────────────────────────────────────
//
// StrokeCircle artık halkayı dört ayrı bölgede rasterleştiriyor (boş ortayı
// taramamak için). Bantların sınırları yanlış hesaplanırsa halkada GÖRÜNMEZ
// bir kesik ya da köşelerde ÇİFT ÇİZİM oluşur — ikisi de kenar yumuşatmayla
// birleşince "hafif tuhaf" görünür ve kimse sebebini aramaz.
//
// Ölçüt: yeni çizim, eski tek-kutulu çizimle BAYT BAYT aynı olmalı.

// eskiStrokeCircle, düzeltmeden önceki tek-kutulu uygulamadır.
func eskiStrokeCircle(p *Painter, cx, cy, r, thickness float64, c color.RGBA) {
	if r <= 0 || thickness <= 0 {
		return
	}
	if thickness >= r {
		p.FillCircle(cx, cy, r, c)
		return
	}
	p.path(Rect{X: cx - r, Y: cy - r, W: 2 * r, H: 2 * r},
		func(ras *vector.Rasterizer, ox, oy float32) {
			circlePath(ras, cx, cy, r, +1, ox, oy)
			circlePath(ras, cx, cy, r-thickness, -1, ox, oy)
		}, c)
}

func tuval(w, h int) (*image.RGBA, *Painter) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	return img, New(img)
}

// Bu test bir DÜZELTMEYİ değil, BİR DENEMENİN NEDEN GERİ ALINDIĞINI korur:
// halkayı dört banda bölme fikri buradan geçemedi ve geri alındı. Biri aynı
// fikri yeniden denerse test onu yine yakalar.
func TestHalkaDortBantEskisiyleAyni(t *testing.T) {
	for _, tc := range []struct {
		ad             string
		cx, cy, r, kal float64
	}{
		{"büyük halka", 200, 200, 150, 8},
		{"ince halka", 200, 200, 180, 2},
		{"kalın halka", 200, 200, 100, 30},
		{"küçük halka", 200, 200, 12, 3},
		{"kenara taşan", 60, 60, 120, 6},
		{"ondalık yarıçap", 200, 200, 77.5, 4.25},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			renk := color.RGBA{R: 45, G: 212, B: 191, A: 255}

			eski, pe := tuval(400, 400)
			eskiStrokeCircle(pe, tc.cx, tc.cy, tc.r, tc.kal, renk)

			yeni, py := tuval(400, 400)
			py.StrokeCircle(tc.cx, tc.cy, tc.r, tc.kal, renk)

			farkli := 0
			enBuyuk := 0
			for i := range eski.Pix {
				d := int(eski.Pix[i]) - int(yeni.Pix[i])
				if d < 0 {
					d = -d
				}
				if d != 0 {
					farkli++
					if d > enBuyuk {
						enBuyuk = d
					}
				}
			}
			if farkli != 0 {
				t.Errorf("%d bayt farklı (en büyük sapma %d) — "+
					"bant sınırları yanlış: halkada kesik ya da köşede çift çizim var",
					farkli, enBuyuk)
			}
		})
	}
}

// Halka GERÇEKTEN çiziliyor mu? Yukarıdaki test iki boş tuvali de "aynı"
// sayardı; bu test ikisinin de dolu olduğunu kanıtlar.
func TestHalkaGercektenCiziliyor(t *testing.T) {
	img, p := tuval(400, 400)
	p.StrokeCircle(200, 200, 150, 8, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	dolu := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			dolu++
		}
	}
	// Halka çevresi ~2*pi*150 ve kalınlık 8: kabaca 7500 piksel beklenir.
	if dolu < 4000 {
		t.Fatalf("yalnızca %d piksel boyandı — halka çizilmiyor", dolu)
	}
	// Merkez BOŞ kalmalı: dolu daire değil halka çiziyoruz.
	if _, _, _, a := img.At(200, 200).RGBA(); a != 0 {
		t.Error("halkanın ortası dolu — FillCircle gibi davranıyor")
	}
}

func BenchmarkStrokeCircleEski(b *testing.B) {
	img, p := tuval(1000, 1000)
	_ = img
	renk := color.RGBA{R: 45, G: 212, B: 191, A: 255}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eskiStrokeCircle(p, 500, 500, 400, 6, renk)
	}
}

func BenchmarkStrokeCircleYeni(b *testing.B) {
	img, p := tuval(1000, 1000)
	_ = img
	renk := color.RGBA{R: 45, G: 212, B: 191, A: 255}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.StrokeCircle(500, 500, 400, 6, renk)
	}
}
