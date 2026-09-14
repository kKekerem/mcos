package fbui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"mcos/internal/fbdraw"
	"mcos/internal/fbfont"
)

// newBenchUI, ölçüm için açılış ekranıyla AYNI tuvali kurar.
//
// Ölçüm gerçekçi olmalı: fbdraw.Painter.path() her şekilde TÜM tuvali
// sıfırlayıp üstüne kompozit ettiği için maliyet doğrudan piksel sayısına
// bağlıdır. 1280x800'de ölçüp "hızlı" demek, 1920x1080 hedef donanımda
// yanıltıcı olurdu.
func newBenchUI(b *testing.B, w, h int, px float64) (*UI, *fbfont.Face) {
	b.Helper()
	f, err := fbfont.Load(px)
	if err != nil {
		b.Fatalf("font: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	u := NewUI(img, f, DefaultPalette)
	u.Clear()
	return u, f
}

// benchRingGeometry, cmd/mcos-splash/main.go:274'teki gerçek çağrıyı
// birebir tekrarlar: 1920x1080 tuval, size = min(W,H)*0.13, r = size*1.35.
func benchRingGeometry(w, h int) (cx, cy, r float64) {
	fw, fh := float64(w), float64(h)
	cx = fw / 2
	cy = fh * 0.42
	size := fh * 0.13 // min(1920,1080) = 1080
	if fw < fh {
		size = fw * 0.13
	}
	return cx, cy, size * 1.35
}

// BenchmarkProgressRing, açılış ekranındaki ilerleme halkasının TEK bir
// karesinin maliyetini ölçer.
//
// Yakalanan gerçek hata: halka eskiden yayı ~82 ayrı FillCircle çağrısıyla
// kuruyordu ve her çağrı 1920x1080 = 2.07 megapiksellik tam tuval geçişi
// demekti. Açılış ekranı 33 ms'de bir kare istiyor (30 kare/sn), ama tek
// kare saniyeler sürüyordu; kullanıcı donmuş bir ekran görüyordu. Bu ölçüm,
// düzeltmenin geri gelmemesi için bekçilik eder.
func BenchmarkProgressRing(b *testing.B) {
	const W, H = 1920, 1080
	u, f := newBenchUI(b, W, H, 18)
	defer f.Close()
	cx, cy, r := benchRingGeometry(W, H)

	// pct=92: açılış eğrisinin (int(92*(1-exp(-el/4)))) ulaştığı tavan,
	// yani pratikte en pahalı kare.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.ProgressRing(cx, cy, r, 92, u.Pal.Accent)
	}
}

// BenchmarkProgressRingFull, pct=100 karesini ölçer: eski kodda en pahalı
// olan (82 nokta) ve BOOT_READY'den sonra ekranı kilitleyen kare.
func BenchmarkProgressRingFull(b *testing.B) {
	const W, H = 1920, 1080
	u, f := newBenchUI(b, W, H, 18)
	defer f.Close()
	cx, cy, r := benchRingGeometry(W, H)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.ProgressRing(cx, cy, r, 100, u.Pal.Accent)
	}
}

// ── ÖNCE/SONRA ölçümünün "ÖNCE" tarafı ──────────────────────────────────────

// progressRingLegacy, düzeltmeden ÖNCEKİ ProgressRing'in birebir kopyasıdır:
// yay, merkezleri yay üzerinde duran ~82 ayrı daireden kuruluyordu.
//
// Yakalanan gerçek hata: her FillCircle çağrısı fbdraw.Painter.path()'e gider,
// o da şeklin büyüklüğünden BAĞIMSIZ olarak tuvalin tamamını sıfırlar
// (ras.Reset(w,h)) ve tuvalin tamamına kompozit eder (ras.Draw(dst,
// dst.Bounds(), ...)). Yani 16 pikselik bir nokta bile 1920x1080 = 2.07
// megapiksellik tam ekran geçişiydi. Üstelik thick = max(1.5, r*0.14) yani
// r ile orantılı olduğu için steps = 2*pi*r/(thick*0.55) yarıçaptan bağımsız
// ~81'de sabitlenir: halka küçülünce bile ucuzlamıyordu.
//
// Bu kopya YALNIZCA ölçüm içindir; üretim kodu artık tek geçiş kullanıyor.
// Kopyayı burada tutmak, "eski hâli ne kadar sürüyordu?" sorusunun cevabının
// depoda yaşamasını ve düzeltme geri alınırsa farkın anında görünmesini sağlar.
func progressRingLegacy(u *UI, cx, cy, r float64, pct int, c color.RGBA) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	thick := math.Max(1.5, r*0.14)
	u.P.StrokeCircle(cx, cy, r, thick, fbdraw.Alpha(c, 0.18))

	steps := int(2 * math.Pi * r / (thick * 0.55))
	if steps < 24 {
		steps = 24
	}
	n := steps * pct / 100
	for i := 0; i <= n; i++ {
		a := -math.Pi/2 + 2*math.Pi*float64(i)/float64(steps)
		u.P.FillCircle(cx+r*math.Cos(a), cy+r*math.Sin(a), thick/2, c)
	}
}

// BenchmarkProgressRingLegacy, BenchmarkProgressRing ile AYNI tuval, AYNI
// geometri ve AYNI pct üzerinde eski yöntemi ölçer. İkisi yan yana
// koşturulduğunda düzeltmenin kazancı doğrudan okunur.
func BenchmarkProgressRingLegacy(b *testing.B) {
	const W, H = 1920, 1080
	u, f := newBenchUI(b, W, H, 18)
	defer f.Close()
	cx, cy, r := benchRingGeometry(W, H)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		progressRingLegacy(u, cx, cy, r, 92, u.Pal.Accent)
	}
}

// BenchmarkProgressRingLegacyFull, eski yöntemin en pahalı karesini (pct=100,
// 82 nokta) ölçer.
func BenchmarkProgressRingLegacyFull(b *testing.B) {
	const W, H = 1920, 1080
	u, f := newBenchUI(b, W, H, 18)
	defer f.Close()
	cx, cy, r := benchRingGeometry(W, H)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		progressRingLegacy(u, cx, cy, r, 100, u.Pal.Accent)
	}
}

// ── Geometri denkliği ───────────────────────────────────────────────────────

// ringMask, tuvalde YALNIZCA ilerleme şeridini işaretler.
//
// Şerit tam opak vurgu rengiyle (A=255) çizilir, dolayısıyla iç pikselleri
// tuvalde birebir o renktir. Soluk yörünge halkası ise 0.18 alfayla arka
// planın üstüne karıştığı için bu testten kendiliğinden dışlanır. Böylece iki
// sürümü kıyaslarken sadece şeridi kıyaslıyoruz.
func ringMask(img *image.RGBA, c color.RGBA) []bool {
	b := img.Bounds()
	m := make([]bool, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := img.PixOffset(x, y)
			if img.Pix[i] == c.R && img.Pix[i+1] == c.G &&
				img.Pix[i+2] == c.B && img.Pix[i+3] == c.A {
				m[(y-b.Min.Y)*b.Dx()+(x-b.Min.X)] = true
			}
		}
	}
	return m
}

// TestProgressRingGeometryMatchesLegacy, tek geçişli yeni çizimin ESKİ nokta
// yığınıyla AYNI şeridi kapladığını kanıtlar.
//
// Yakalanan gerçek hata performanstı, ama hızlandırma görüntüyü bozmamalıydı;
// bu test o sözü bağlar. Üç şeye bakar: (1) eski noktaların kapladığı her
// piksel yeni şeritte de doludur — yani yay ne kısaldı ne uzadı, ne içeri ne
// dışarı kaydı; (2) yeni şerit r±thick/2 bandının ve [başlangıç, bitiş] açı
// aralığının DIŞINA taşmaz; (3) fazladan gelen pikseller yalnızca noktalar
// arası tırtıkların dolmasından ibarettir, yani küçük bir paydır.
func TestProgressRingGeometryMatchesLegacy(t *testing.T) {
	const W, H = 420, 420
	const r = 140.0
	cx, cy := float64(W)/2, float64(H)/2
	thick := math.Max(1.5, r*0.14)
	d := thick / 2

	steps := int(2 * math.Pi * r / (thick * 0.55))
	if steps < 24 {
		steps = 24
	}

	for _, pct := range []int{0, 1, 37, 62, 92, 99, 100} {
		imgOld, uOld, fOld := newScreen(t, W, H, 14)
		progressRingLegacy(uOld, cx, cy, r, pct, uOld.Pal.Accent)
		accent := uOld.Pal.Accent
		fOld.Close()

		imgNew, uNew, fNew := newScreen(t, W, H, 14)
		uNew.ProgressRing(cx, cy, r, pct, accent)
		fNew.Close()

		mOld := ringMask(imgOld, accent)
		mNew := ringMask(imgNew, accent)

		var oldN, both, extra int
		for i := range mOld {
			switch {
			case mOld[i] && mNew[i]:
				oldN++
				both++
			case mOld[i]:
				oldN++
			case mNew[i]:
				extra++
			}
		}
		if oldN == 0 {
			t.Fatalf("pct=%d: eski surum hic piksel cizmemis, olcum anlamsiz", pct)
		}

		keep := float64(both) / float64(oldN)
		ratio := float64(extra) / float64(oldN)
		t.Logf("pct=%3d eski=%5d ortak=%5d fazla=%5d korunan=%.2f%% fazla=%.2f%%",
			pct, oldN, both, extra, keep*100, ratio*100)

		// (1) Eski seridin her pikseli yeni seritte de dolu olmali. Kenar
		//     yumusatma iki tarafta son basamagi farkli yuvarlayabildigi icin
		//     %1 pay birakiyoruz.
		if keep < 0.99 {
			t.Errorf("pct=%d: eski seridin yalnizca %.2f%%'i korunmus (eski=%d, ortak=%d)",
				pct, keep*100, oldN, both)
		}
		// (3) Fazlalik yalnizca tirtiklarin dolmasidir: eski alanin kucuk bir
		//     yuzdesi. Serit yanlislikla kalinlassa ya da uzasa bu oran patlardi.
		if ratio > 0.20 {
			t.Errorf("pct=%d: yeni serit eski alanin %.2f%%'i kadar fazla piksel boyuyor",
				pct, ratio*100)
		}

		// (2) Yeni serit halka bandinin ve aci araliginin disina tasmamali.
		//     Uclardaki yuvarlak kapaklar merkezden asin(d/r) kadar aci tasar;
		//     bu tasma ESKI surumdeki ilk/son noktanin tasmasinin aynisidir.
		n := steps * pct / 100
		if n > steps {
			n = steps
		}
		sweep := 2 * math.Pi * float64(n) / float64(steps)
		capSlop := math.Asin(math.Min(1, (d+1.5)/(r-1.5)))
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				if !mNew[y*W+x] {
					continue
				}
				dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
				rad := math.Hypot(dx, dy)
				if rad < r-d-1.5 || rad > r+d+1.5 {
					t.Fatalf("pct=%d: (%d,%d) piksel r=%.2f ile bandin disinda (%.2f..%.2f)",
						pct, x, y, rad, r-d, r+d)
				}
				if pct >= 100 {
					continue // tam halka: aci siniri yok
				}
				// Aciyi baslangic acisina gore [0, 2pi) araligina tasi.
				off := math.Mod(math.Atan2(dy, dx)-(-math.Pi/2)+2*math.Pi, 2*math.Pi)
				if off > sweep+capSlop && off < 2*math.Pi-capSlop {
					t.Fatalf("pct=%d: (%d,%d) piksel yayin disinda (sapma %.3f rad, supurme %.3f)",
						pct, x, y, off, sweep)
				}
			}
		}
	}
}
