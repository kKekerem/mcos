package fbdev

import (
	"image"
	"image/color"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// HIZLI YOL, YAVAŞ YOLLA BİREBİR AYNI PİKSELİ ÜRETMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu test hayati ────────────────────────────────────────────────────
//
// Hızlı yol kanal sırasını KELİME İÇİNDE değiştiriyor. Bir kaydırma yanlış
// olursa program çalışır, hata vermez ve hiçbir test kırılmaz — ekran yalnızca
// MAVİYE ÇALAR. Böyle bir hata ancak gerçek donanımda gözle görülür ve o da
// "sanırım renkler tuhaf" diye geçiştirilir.
//
// Bu yüzden ölçüt ŞUDUR: hızlı yolun yazdığı bellek, eski (yavaş) dönüşümün
// yazdığı bellekle BAYT BAYT aynı olmalı.

// yaziYavas, device_linux.go'daki özgün dönüşümün birebir kopyasıdır.
// Referans olarak duruyor: hızlı yol buna karşı kanıtlanıyor.
func yaziYavas(mem []byte, row int, img *image.RGBA, w, h int,
	rShift, gShift, bShift uint) {
	b := img.Bounds()
	for y := 0; y < h; y++ {
		src := img.PixOffset(b.Min.X, b.Min.Y+y)
		dst := y * row
		for x := 0; x < w; x++ {
			s := src + x*4
			v := uint32(img.Pix[s])<<rShift |
				uint32(img.Pix[s+1])<<gShift |
				uint32(img.Pix[s+2])<<bShift
			o := dst + x*4
			mem[o] = byte(v)
			mem[o+1] = byte(v >> 8)
			mem[o+2] = byte(v >> 16)
			mem[o+3] = byte(v >> 24)
		}
	}
}

// renkliKare, her kanalı ayrı ayrı zorlayan bir desen üretir.
//
// Düz gri bir kare kanal takasını GİZLERDİ: R=G=B iken yanlış sıra fark
// edilmez. Desen bilerek kanal başına farklı değer veriyor.
func renkliKare(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: byte(x * 7),
				G: byte(y * 13),
				B: byte(x*3 + y*5),
				A: 255,
			})
		}
	}
	return img
}

func yeniAygit(w, h int, rS, gS, bS uint) *Device {
	d := &Device{}
	d.bpp = 32
	d.row = w * 4
	d.rShift, d.gShift, d.bShift = rS, gS, bS
	d.mem = make([]byte, d.row*h)
	d.format = detectFormat(d.bpp, rS, gS, bS)
	return d
}

func TestHizliYolXRGBYavasYolaEsit(t *testing.T) {
	const w, h = 64, 32
	img := renkliKare(w, h)

	// XRGB8888: en yaygın düzen (VMware, QEMU stdvga, çoğu UEFI GOP).
	d := yeniAygit(w, h, 16, 8, 0)
	if d.format != fmtXRGB {
		t.Fatalf("düzen fmtXRGB olarak tanınmadı: %v", d.format)
	}
	if !d.blitRows(img, 0, h, w) {
		t.Fatal("hızlı yol uygulanamadı")
	}

	beklenen := make([]byte, len(d.mem))
	yaziYavas(beklenen, d.row, img, w, h, 16, 8, 0)

	for i := range beklenen {
		if d.mem[i] != beklenen[i] {
			t.Fatalf("bayt %d farklı: hızlı=%#02x yavaş=%#02x "+
				"(piksel %d, kanal %d) — KANAL SIRASI BOZUK, ekran renkleri yanlış çıkar",
				i, d.mem[i], beklenen[i], i/4, i%4)
		}
	}
}

func TestHizliYolXBGRYavasYolaEsit(t *testing.T) {
	const w, h = 64, 32
	img := renkliKare(w, h)

	d := yeniAygit(w, h, 0, 8, 16)
	if d.format != fmtXBGR {
		t.Fatalf("düzen fmtXBGR olarak tanınmadı: %v", d.format)
	}
	if !d.blitRows(img, 0, h, w) {
		t.Fatal("hızlı yol uygulanamadı")
	}

	beklenen := make([]byte, len(d.mem))
	yaziYavas(beklenen, d.row, img, w, h, 0, 8, 16)

	for i := range beklenen {
		if d.mem[i] != beklenen[i] {
			t.Fatalf("bayt %d farklı: hızlı=%#02x yavaş=%#02x", i, d.mem[i], beklenen[i])
		}
	}
}

// Bilinmeyen düzende hızlı yol DEVREYE GİRMEMELİ: yanlış renk basmaktansa
// yavaş ama doğru yol çalışsın.
func TestTuhafDuzendeHizliYolReddedilir(t *testing.T) {
	for _, tc := range []struct {
		ad         string
		bpp        int
		rS, gS, bS uint
	}{
		{"16bpp RGB565", 16, 11, 5, 0},
		{"24bpp", 24, 16, 8, 0},
		{"tuhaf kaydırma", 32, 8, 16, 24},
	} {
		if f := detectFormat(tc.bpp, tc.rS, tc.gS, tc.bS); f != fmtGeneric {
			t.Errorf("%s: fmtGeneric bekleniyordu, %v geldi — yanlış renk basılır",
				tc.ad, f)
		}
	}
}

// ── Hasar takibi ────────────────────────────────────────────────────────────

func TestIlkKareTumEkraniDondurur(t *testing.T) {
	const w, h = 32, 16
	var dm damage
	r := dm.rowsChanged(renkliKare(w, h), w, h)
	if len(r) != 1 || r[0] != [2]int{0, h} {
		t.Fatalf("ilk karede %v döndü; tüm ekran {0,%d} olmalı — "+
			"yoksa ekranda çöp kalır", r, h)
	}
}

func TestDegismeyenKareHicSatirDondurmez(t *testing.T) {
	const w, h = 32, 16
	img := renkliKare(w, h)
	var dm damage
	dm.rowsChanged(img, w, h) // ilk kare

	if r := dm.rowsChanged(img, w, h); len(r) != 0 {
		t.Errorf("aynı kare için %v döndü; hiçbir satır yazılmamalı — "+
			"boştaki panel her tikte tüm ekranı yazardı", r)
	}
}

func TestYalnizDegisenSatirlarDondurulur(t *testing.T) {
	const w, h = 32, 40
	img := renkliKare(w, h)
	var dm damage
	dm.rowsChanged(img, w, h)

	// 10..14 arası satırları değiştir.
	for y := 10; y < 15; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		}
	}
	r := dm.rowsChanged(img, w, h)
	if len(r) != 1 || r[0] != [2]int{10, 15} {
		t.Fatalf("%v döndü; {10,15} bekleniyordu", r)
	}
}

// Bitişik olmayan iki değişiklik AYRI aralık olmalı: aradaki değişmemiş
// satırları yazmak kazancı yer.
func TestAyrikDegisiklikerAyriAralik(t *testing.T) {
	const w, h = 32, 40
	img := renkliKare(w, h)
	var dm damage
	dm.rowsChanged(img, w, h)

	for _, y := range []int{5, 30} {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 9, G: 9, B: 9, A: 255})
		}
	}
	r := dm.rowsChanged(img, w, h)
	if len(r) != 2 || r[0] != [2]int{5, 6} || r[1] != [2]int{30, 31} {
		t.Fatalf("%v döndü; [{5,6} {30,31}] bekleniyordu", r)
	}
}

// Boyut değişince hasar durumu SIFIRLANMALI, yoksa eski boyutun kalıntısı
// yanlış satırları "değişmedi" sayar.
func TestBoyutDegisinceTamEkran(t *testing.T) {
	var dm damage
	dm.rowsChanged(renkliKare(32, 16), 32, 16)
	r := dm.rowsChanged(renkliKare(64, 32), 64, 32)
	if len(r) != 1 || r[0] != [2]int{0, 32} {
		t.Fatalf("boyut değişiminde %v döndü; tüm ekran beklenir", r)
	}
}

// ── Kıyaslama: kazanç gerçek mi ─────────────────────────────────────────────

func benchKare() (*Device, *image.RGBA) {
	const w, h = 1920, 1080
	return yeniAygit(w, h, 16, 8, 0), renkliKare(w, h)
}

func BenchmarkFlipYavasYol(b *testing.B) {
	d, img := benchKare()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		yaziYavas(d.mem, d.row, img, 1920, 1080, 16, 8, 0)
	}
}

func BenchmarkFlipHizliYolTamKare(b *testing.B) {
	d, img := benchKare()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.blitRows(img, 0, 1080, 1920)
	}
}

func BenchmarkFlipHizliYol40Satir(b *testing.B) {
	d, img := benchKare()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.blitRows(img, 500, 540, 1920)
	}
}

func BenchmarkHasarTaramasi(b *testing.B) {
	const w, h = 1920, 1080
	img := renkliKare(w, h)
	var dm damage
	dm.rowsChanged(img, w, h)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dm.rowsChanged(img, w, h)
	}
}
