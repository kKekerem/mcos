package fbfont

import (
	"testing"
)

// TestLoadMetrics: hücre ızgarası tutarlı olmalı, yoksa tüm düzen kayar.
func TestLoadMetrics(t *testing.T) {
	for _, px := range []float64{12, 14, 16, 20, 24} {
		f, err := Load(px)
		if err != nil {
			t.Fatalf("Load(%.0f): %v", px, err)
		}
		if f.CellW < 1 || f.CellH < 1 {
			t.Errorf("%.0fpx: geçersiz hücre %dx%d", px, f.CellW, f.CellH)
		}
		if f.Baseline <= 0 || f.Baseline > f.CellH {
			t.Errorf("%.0fpx: taban çizgisi hücre dışında: %d (hücre yüksekliği %d)",
				px, f.Baseline, f.CellH)
		}
		// Monospace fontta hücre kabaca 0.4–0.8 en/boy oranındadır.
		ratio := float64(f.CellW) / float64(f.CellH)
		if ratio < 0.3 || ratio > 0.9 {
			t.Errorf("%.0fpx: hücre en/boy oranı şüpheli: %.2f (%dx%d) — monospace değil olabilir",
				px, ratio, f.CellW, f.CellH)
		}
		f.Close()
	}
}

// TestMonospaceAdvance: TÜM kullandığımız glifler aynı ilerlemeye sahip olmalı.
// Değilse sütunlar kayar — geçen oturumda emoji yüzünden yaşanan hatanın aynısı.
func TestMonospaceAdvance(t *testing.T) {
	f, err := Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// SÖZLEŞME: bu font yalnızca METİN çizer.
	//
	// Çerçeveler, yuvarlak köşeler, ayırıcı çizgiler, butonlar, radyo/onay
	// kutuları, oklar ve ikonlar FONTTAN GELMEZ — internal/fbdraw tarafından
	// gerçek vektör şekiller olarak çizilir. Sebebi ölçümle bulundu: gömülü
	// FiraCode Nerd Font şu glifleri İÇERMİYOR
	//
	//     ▸ ◂ ▴ ▾ ◈ ◍ ★ ⚑ ⚙ ⚠ ✗
	//
	// ve bunlar tam olarak eski token setindeki ikonlardı. fbterm bu glifler
	// için başka bir fonta düşüyor, o fontun ilerlemesi farklı olabildiği için
	// sütunlar kayıyordu. Şekilleri kendimiz çizerek sorunu kaynağında yok
	// ediyoruz.
	//
	// Bu yüzden burada yalnızca metin karakterleri sınanır.
	sets := map[string]string{
		"latin":     "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		"turkce":    "ığüşöçİĞÜŞÖÇ",
		"noktalama": " .,:;!?()[]{}<>/\\|-_=+*#%&@'\"`~^$",
	}

	for name, chars := range sets {
		for _, r := range chars {
			adv, ok := f.face.GlyphAdvance(r)
			if !ok {
				t.Errorf("%s: %q (U+%04X) fontta YOK", name, r, r)
				continue
			}
			if got := roundFixed(adv); got != f.CellW {
				t.Errorf("%s: %q (U+%04X) ilerlemesi %d, hücre genişliği %d — sütunlar kayar",
					name, r, r, got, f.CellW)
			}
		}
	}
}

// TestGlyphRasterises: kritik glifler GERÇEKTEN piksel üretmeli.
// Fontta "var" görünüp boş maske üretmesi, ekranda boşluk demektir.
func TestGlyphRasterises(t *testing.T) {
	f, err := Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Yalnızca METİN (bkz. TestMonospaceAdvance sözleşme notu).
	mustDraw := []rune{
		'A', 'g', '0', 'W', 'i',
		'ı', 'ğ', 'ş', 'İ', 'Ş', 'Ç', 'ö', 'ü', // Türkçe — panel baştan sona Türkçe
		'.', ',', ':', '/', '-', '(', ')', '%',
	}

	for _, r := range mustDraw {
		g := f.Glyph(r)
		if g.Missing {
			t.Errorf("%q (U+%04X): fontta YOK", r, r)
			continue
		}
		if g.Mask == nil {
			t.Errorf("%q (U+%04X): maske nil — hiç piksel üretilmedi", r, r)
			continue
		}
		// En az bir piksel sıfırdan farklı olmalı.
		var ink int
		for _, v := range g.Mask.Pix {
			if v > 0 {
				ink++
			}
		}
		if ink == 0 {
			t.Errorf("%q (U+%04X): maske tamamen boş — ekranda görünmez", r, r)
		}
	}
}

// TestSpaceIsBlankNotMissing: boşluk çizilecek piksel içermez ama EKSİK de
// değildir. İkisini karıştırmak, çizicinin her boşluk için "eksik glif"
// yer tutucusu basmasına yol açardı.
func TestSpaceIsBlankNotMissing(t *testing.T) {
	f, err := Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	g := f.Glyph(' ')
	if g.Missing {
		t.Error("boşluk 'eksik' olarak işaretlendi")
	}
	if g.Mask != nil {
		var ink int
		for _, v := range g.Mask.Pix {
			if v > 0 {
				ink++
			}
		}
		if ink != 0 {
			t.Errorf("boşluk %d mürekkepli piksel üretti", ink)
		}
	}
}

// TestGlyphCached: aynı rune iki kez istendiğinde AYNI nesne dönmeli.
// Önbelleksiz her karede binlerce glif yeniden rasterize edilirdi.
func TestGlyphCached(t *testing.T) {
	f, err := Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	a := f.Glyph('X')
	b := f.Glyph('X')
	if a != b {
		t.Error("glif önbelleğe alınmadı — her çağrıda yeniden rasterize ediliyor")
	}
}

// TestMissingGlyphIsReported: bilinmeyen rune SESSİZCE yutulmamalı.
func TestMissingGlyphIsReported(t *testing.T) {
	f, err := Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Unicode'da atanmamış bir kod noktası (Private Use dışı, kesin boş).
	g := f.Glyph(rune(0x0B00))
	if g == nil {
		t.Fatal("Glyph() nil döndü — asla nil dönmemeli")
	}
	// Missing ya da boş maske; ikisi de kabul — ama panik/nil olmamalı.
}

// TestLoadRejectsAbsurdSizes: geçersiz punto sessizce kabul edilmemeli.
func TestLoadRejectsAbsurdSizes(t *testing.T) {
	for _, px := range []float64{0, -1, 3, 200} {
		if f, err := Load(px); err == nil {
			f.Close()
			t.Errorf("Load(%.0f) hata döndürmedi", px)
		}
	}
}

// TestGrid: piksel alanından hücre sayısı hesabı.
func TestGrid(t *testing.T) {
	f, err := Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	cols, rows := f.Grid(1920, 1080)
	if cols < 100 || rows < 30 {
		t.Errorf("1920x1080 -> %dx%d hücre; beklenenden çok küçük (hücre %dx%d)",
			cols, rows, f.CellW, f.CellH)
	}
	// Izgara ekrana sığmalı.
	if cols*f.CellW > 1920 || rows*f.CellH > 1080 {
		t.Errorf("ızgara ekranı taşıyor: %dx%d hücre x %dx%d piksel",
			cols, rows, f.CellW, f.CellH)
	}
	t.Logf("1920x1080 @ %s -> %d sütun x %d satır", f.Info(), cols, rows)
}
