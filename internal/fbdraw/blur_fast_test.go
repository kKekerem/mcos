package fbdraw

import (
	"math/rand"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// HIZLANDIRILMIŞ BULANIKLIK, ÇIKTIYI DEĞİŞTİRMEMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu test hayati ────────────────────────────────────────────────────
//
// İki değişiklik yapıldı ve ikisi de "aynı sonucu daha hızlı" iddiasında:
//
//   1. Piksel başına dört tamsayı BÖLMESİ yerine sihirli çarpan.
//   2. Dikey geçiş sütun sütun değil SATIR satır.
//
// Sihirli çarpan bir YAKLAŞIM değil, bölmenin tam karşılığı olmalı. Bir tek
// pencere genişliğinde bile 1 birim sapsa, perde her karede hafifçe farklı
// çıkar ve bu gözle "titreme" olarak görünür — ama hiçbir test kırılmaz.
// Bu yüzden eşitlik BURADA, sayıyla kanıtlanıyor.

// bolerekBulanik is the original implementation, kept as the reference.
func bolerekBulanikH(src, out []uint8, w, h, radius int) {
	win := 2*radius + 1
	for y := 0; y < h; y++ {
		row := y * w * 4
		var sum [4]int
		for i := -radius; i <= radius; i++ {
			xi := clampInt(i, 0, w-1)
			p := row + xi*4
			for c := 0; c < 4; c++ {
				sum[c] += int(src[p+c])
			}
		}
		for x := 0; x < w; x++ {
			o := row + x*4
			for c := 0; c < 4; c++ {
				out[o+c] = uint8(sum[c] / win)
			}
			lo := row + clampInt(x-radius, 0, w-1)*4
			hi := row + clampInt(x+radius+1, 0, w-1)*4
			for c := 0; c < 4; c++ {
				sum[c] += int(src[hi+c]) - int(src[lo+c])
			}
		}
	}
}

func bolerekBulanikV(src, out []uint8, w, h, radius int) {
	win := 2*radius + 1
	for x := 0; x < w; x++ {
		col := x * 4
		var sum [4]int
		for i := -radius; i <= radius; i++ {
			p := clampInt(i, 0, h-1)*w*4 + col
			for c := 0; c < 4; c++ {
				sum[c] += int(src[p+c])
			}
		}
		for y := 0; y < h; y++ {
			o := y*w*4 + col
			for c := 0; c < 4; c++ {
				out[o+c] = uint8(sum[c] / win)
			}
			lo := clampInt(y-radius, 0, h-1)*w*4 + col
			hi := clampInt(y+radius+1, 0, h-1)*w*4 + col
			for c := 0; c < 4; c++ {
				sum[c] += int(src[hi+c]) - int(src[lo+c])
			}
		}
	}
}

func rastgeleTampon(w, h int, tohum int64) []uint8 {
	r := rand.New(rand.NewSource(tohum))
	b := make([]uint8, w*h*4)
	for i := range b {
		b[i] = uint8(r.Intn(256))
	}
	return b
}

func TestHizliBulaniklikBolmeyleAyni(t *testing.T) {
	for _, boyut := range [][2]int{{64, 48}, {320, 200}, {201, 97}} {
		for _, r := range []int{1, 2, 3, 5, 8, 15} {
			w, h := boyut[0], boyut[1]
			src := rastgeleTampon(w, h, int64(w*h+r))

			yeniH := make([]uint8, len(src))
			eskiH := make([]uint8, len(src))
			boxBlurH(src, yeniH, w, h, r)
			bolerekBulanikH(src, eskiH, w, h, r)
			for i := range eskiH {
				if yeniH[i] != eskiH[i] {
					t.Fatalf("YATAY %dx%d r=%d: bayt %d farklı (%d vs %d) — "+
						"sihirli çarpan bölmeye eşit değil",
						w, h, r, i, yeniH[i], eskiH[i])
				}
			}

			yeniV := make([]uint8, len(src))
			eskiV := make([]uint8, len(src))
			boxBlurV(src, yeniV, w, h, r)
			bolerekBulanikV(src, eskiV, w, h, r)
			for i := range eskiV {
				if yeniV[i] != eskiV[i] {
					t.Fatalf("DİKEY %dx%d r=%d: bayt %d farklı (%d vs %d) — "+
						"satır bazlı geçiş sonucu değiştirdi",
						w, h, r, i, yeniV[i], eskiV[i])
				}
			}
		}
	}
}

// Sihirli çarpan, ULAŞILABİLECEK TÜM toplamlar için bölmeye eşit olmalı.
// Tek bir aralıkta sapsa bile perde her karede hafifçe farklı çıkardı.
func TestSihirliCarpanTumToplamlardaDogru(t *testing.T) {
	for radius := 0; radius <= 40; radius++ {
		win := 2*radius + 1
		m, s := magicDiv(win)
		enBuyuk := uint32(win) * 255 // pencerede olabilecek en büyük toplam
		for v := uint32(0); v <= enBuyuk; v++ {
			if (v*m)>>s != v/uint32(win) {
				t.Fatalf("yarıçap %d (pencere %d), toplam %d: "+
					"sihirli=%d bölme=%d — YAKLAŞIM, tam karşılık değil",
					radius, win, v, (v*m)>>s, v/uint32(win))
			}
		}
	}
}

func BenchmarkBulaniklikEskiBolmeli(b *testing.B) {
	const w, h, r = 960, 540, 3
	src := rastgeleTampon(w, h, 1)
	out := make([]uint8, len(src))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bolerekBulanikH(src, out, w, h, r)
		bolerekBulanikV(out, src, w, h, r)
	}
}

func BenchmarkBulaniklikYeni(b *testing.B) {
	const w, h, r = 960, 540, 3
	src := rastgeleTampon(w, h, 1)
	out := make([]uint8, len(src))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		boxBlurH(src, out, w, h, r)
		boxBlurV(out, src, w, h, r)
	}
}
