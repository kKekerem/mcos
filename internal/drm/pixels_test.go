package drm

import (
	"image"
	"image/color"
	"testing"
)

// TestSwizzleRGBAdanXRGBye: bellekte R,G,B,A -> B,G,R,x.
func TestSwizzleRGBAdanXRGBye(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF})
	src.Set(1, 0, color.RGBA{R: 0xAA, G: 0xBB, B: 0xCC, A: 0x80})
	dst := make([]byte, 8)
	copyRows(dst, 8, 2, 1, src, 0, 1, false)
	want := []byte{0x33, 0x22, 0x11, 0x00, 0xCC, 0xBB, 0xAA, 0x00}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("bayt %d = %#x, %#x bekleniyordu (tam: % x)", i, dst[i], want[i], dst)
		}
	}
}

// XBGR8888 tamponda satır AYNEN kopyalanır (dönüşüm yok).
func TestDirectKopyaDonusumsuz(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	dst := make([]byte, 4)
	copyRows(dst, 4, 1, 1, src, 0, 1, true)
	if dst[0] != 1 || dst[1] != 2 || dst[2] != 3 {
		t.Fatalf("doğrudan kopya bozuk: % x", dst)
	}
}

// Pitch genişlik*4 OLMAYABİLİR (sürücüler satırı 64/256 bayta hizalar).
// Satır sonundaki dolgu baytlarına yazılmamalı.
func TestPitchDolgusunaYazilmaz(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i := range src.Pix {
		src.Pix[i] = 0x7F
	}
	const pitch = 16 // 3 piksel = 12 bayt + 4 bayt dolgu
	dst := make([]byte, pitch*2)
	for i := range dst {
		dst[i] = 0xEE
	}
	copyRows(dst, pitch, 3, 2, src, 0, 2, true)
	for y := 0; y < 2; y++ {
		for x := 12; x < pitch; x++ {
			if dst[y*pitch+x] != 0xEE {
				t.Fatalf("satır %d dolgu baytı %d ezildi", y, x)
			}
		}
	}
}

// Mod değişiminin ortasında tuval tampondan büyük olabilir: taşma yok.
func TestBoyutUyusmazligindaTasmaYok(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	dst := make([]byte, 10*4*10)
	copyRows(dst, 40, 10, 10, src, 0, 100, false) // panik olmamalı
}

// ── Çift tampon hasar takibi ────────────────────────────────────────────────
//
// Arka tampon İKİ kare önceki görüntüyü taşır. Kare N-1'de değişen satır,
// kare N'de değişmese bile arka tampona YAZILMALI; yoksa o satır iki karede
// bir eski hâline döner (titreme). Bu test tam o durumu kuruyor.
func TestCiftTamponHasari(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	d := newDamage(4, 4)
	d.observe(img)
	d.take(0)
	d.take(1) // ikisi de güncel

	// Kare 1: satır 1 değişti, tampon 1'e yazılıyor.
	img.Pix[1*img.Stride] = 9
	d.observe(img)
	if got := d.take(1); len(got) != 1 || got[0] != [2]int{1, 2} {
		t.Fatalf("tampon 1: %v", got)
	}
	// Kare 2: satır 3 değişti, tampon 0'a yazılıyor. Tampon 0 satır 1'i
	// HENÜZ görmedi — o da gelmeli.
	img.Pix[3*img.Stride] = 9
	d.observe(img)
	got := d.take(0)
	if len(got) != 2 || got[0] != [2]int{1, 2} || got[1] != [2]int{3, 4} {
		t.Fatalf("tampon 0 hem satır 1'i hem 3'ü almalı: %v", got)
	}
	// Tampon 1 yalnızca satır 3'ü almalı (1'i zaten yazdı).
	if got := d.take(1); len(got) != 1 || got[0] != [2]int{3, 4} {
		t.Fatalf("tampon 1 yalnızca satır 3'ü almalı: %v", got)
	}
}

func TestDegismeyenKareHasarsiz(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	d := newDamage(8, 8)
	d.observe(img)
	d.take(0)
	d.take(1)
	if n := d.observe(img); n != 0 {
		t.Fatalf("aynı kare %d satır değişti dedi", n)
	}
	if len(d.take(0)) != 0 {
		t.Fatal("değişmeyen karede hasar var")
	}
}

// ── Hz metni ────────────────────────────────────────────────────────────────

func TestHzMetni(t *testing.T) {
	for _, tc := range []struct {
		mhz  int
		want string
	}{
		{60000, "60"}, {59940, "59,94"}, {143912, "143,91"}, {144000, "144"},
		{74973, "74,97"}, {59999, "60"},
	} {
		if got := (Mode{MilliHz: tc.mhz}).HzText(); got != tc.want {
			t.Errorf("%d mHz -> %q, %q bekleniyordu", tc.mhz, got, tc.want)
		}
	}
}

// ── Başlangıç modu politikası ───────────────────────────────────────────────

// Gerçek monitörde tercih edilen mod doğal çözünürlüktür ama tazelemesi
// genelde 60'tır; 144 Hz'lik bir monitörde 144 seçilmeli.
func TestGercekMonitordeDogalCozunurlukEnYuksekHz(t *testing.T) {
	ms := []Mode{
		{Width: 3840, Height: 2160, MilliHz: 30000, Refresh: 30},
		{Width: 2560, Height: 1440, MilliHz: 143912, Refresh: 143},
		{Width: 2560, Height: 1440, MilliHz: 59951, Refresh: 59, Preferred: true},
		{Width: 1920, Height: 1080, MilliHz: 60000, Refresh: 60},
	}
	sirala(ms)
	m, _ := Choose(ms, 11 /* HDMI-A */, "")
	if m.Width != 2560 || m.MilliHz != 143912 {
		t.Fatalf("2560x1440@143,91 bekleniyordu, %s seçildi", m)
	}
}

func TestKaydedilenTercihKazanir(t *testing.T) {
	ms := []Mode{
		{Width: 2560, Height: 1440, MilliHz: 143912, Preferred: true},
		{Width: 1920, Height: 1080, MilliHz: 60000},
	}
	m, why := Choose(ms, 11, "1920x1080@60000")
	if m.Width != 1920 {
		t.Fatalf("kayıtlı mod seçilmedi: %s (%s)", m, why)
	}
}

// Kayıt 60,00 Hz, yeni monitör yalnız 59,94 sunuyor: aynı çözünürlükte en
// yakın tazeleme bulunmalı, politika sessizce başka çözünürlüğe atlamamalı.
func TestKayitliModEnYakinTazelemeye(t *testing.T) {
	ms := []Mode{
		{Width: 2560, Height: 1440, MilliHz: 59951, Preferred: true},
		{Width: 1920, Height: 1080, MilliHz: 59940},
	}
	m, ok := Find(ms, "1920x1080@60000")
	if !ok || m.MilliHz != 59940 {
		t.Fatalf("en yakın tazeleme bulunmadı: %v %s", ok, m)
	}
}
