package model

import (
	"fmt"
	"testing"
)

// Bu testler ORTAK DÜNYANIN matematiğini doğrular.
//
// Bir hata burada, oyuncunun hiçbir sunucuya ait olmadığı bir "boşluk"
// bırakır ya da iki sunucunun aynı bölgeyi sahiplendiği bir çakışma yaratır.
// İkisi de ancak oyun sırasında, sınırın tam üstünde fark edilir — ve
// nedeni asla anlaşılmaz. Bu yüzden matematiği burada, kesin olarak
// sınıyoruz.

func TestTerritoriesTwoNodesSplitAtOrigin(t *testing.T) {
	ts := Territories([]string{"a", "b"}, DefaultSlabChunks)
	if len(ts) != 2 {
		t.Fatalf("2 dilim bekleniyordu, %d geldi", len(ts))
	}

	// "dünyanın yarısı diğer pc de" ifadesi HARFİYEN doğru olmalı:
	// sınır tam olarak x=0.
	if !ts[0].UnboundedMin || ts[0].MaxChunkX != 0 {
		t.Errorf("ilk dilim yanlış: %+v", ts[0])
	}
	if ts[1].MinChunkX != 0 || !ts[1].UnboundedMax {
		t.Errorf("ikinci dilim yanlış: %+v", ts[1])
	}

	if got := OwnerOf(ts, -1); got != 0 {
		t.Errorf("chunkX=-1 sahibi %d, 0 olmalıydı", got)
	}
	if got := OwnerOf(ts, 0); got != 1 {
		t.Errorf("chunkX=0 sahibi %d, 1 olmalıydı", got)
	}
}

// Dilimler dünyayı BOŞLUKSUZ ve ÇAKIŞMASIZ kaplamalı.
//
// Bu, ortak dünyanın tek gerçek değişmezidir: her chunk'ın tam olarak BİR
// sahibi vardır. Boşluk = kaybolan oyuncu; çakışma = iki makine arasında
// sonsuz aktarım.
func TestTerritoriesCoverEveryChunkExactlyOnce(t *testing.T) {
	for n := 1; n <= 8; n++ {
		names := make([]string, n)
		for i := range names {
			names[i] = fmt.Sprintf("node%d", i)
		}
		for _, slab := range []int{1, 4, 32, 100} {
			ts := Territories(names, slab)
			if len(ts) != n {
				t.Fatalf("n=%d slab=%d: %d dilim", n, slab, len(ts))
			}
			// Sınırların çok ötesine kadar tara.
			span := slab*n + 64
			for cx := -span; cx <= span; cx++ {
				owners := 0
				for _, ter := range ts {
					if ter.Contains(cx) {
						owners++
					}
				}
				if owners != 1 {
					t.Fatalf("n=%d slab=%d chunkX=%d: %d sahip (1 olmalı)\n%+v",
						n, slab, cx, owners, ts)
				}
			}
		}
	}
}

// Sınırlar spawn etrafında SİMETRİK olmalı: tek sayıda düğümde ortadaki
// düğüm doğuş noktasını içerir ve iki yanı eşit genişlikte olur.
func TestTerritoriesAreSymmetricAroundSpawn(t *testing.T) {
	ts := Territories([]string{"a", "b", "c"}, 32)
	mid := ts[1]
	if mid.UnboundedMin || mid.UnboundedMax {
		t.Fatalf("orta dilim sonsuz olmamalı: %+v", mid)
	}
	if mid.MinChunkX != -16 || mid.MaxChunkX != 16 {
		t.Errorf("orta dilim [-16,16) olmalıydı, %+v geldi", mid)
	}
	if !mid.Contains(0) {
		t.Error("orta dilim doğuş noktasını içermiyor")
	}
}

func TestTerritoriesSingleNodeOwnsEverything(t *testing.T) {
	ts := Territories([]string{"tek"}, 32)
	if len(ts) != 1 || !ts[0].UnboundedMin || !ts[0].UnboundedMax {
		t.Fatalf("tek düğüm tüm dünyayı almalı: %+v", ts)
	}
	for _, cx := range []int{-1 << 20, -1, 0, 1, 1 << 20} {
		if !ts[0].Contains(cx) {
			t.Errorf("chunkX=%d dışarıda kaldı", cx)
		}
	}
}

func TestTerritoriesEmptyNodeList(t *testing.T) {
	if ts := Territories(nil, 32); ts != nil {
		t.Errorf("boş liste için nil bekleniyordu, %+v geldi", ts)
	}
}

// Geçersiz bir dilim genişliği varsayılana düşmeli: sıfır genişlik, tüm
// sınırları üst üste bindirir ve dünyayı tek düğüme verirdi.
func TestTerritoriesZeroSlabUsesDefault(t *testing.T) {
	a := Territories([]string{"a", "b", "c"}, 0)
	b := Territories([]string{"a", "b", "c"}, DefaultSlabChunks)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("slab=0 varsayılana düşmedi: %+v vs %+v", a[i], b[i])
		}
	}
}

func TestLinkSpecNormalizeFillsDefaults(t *testing.T) {
	got := LinkSpec{}.Normalize()
	if got.Mode != LinkOff {
		t.Errorf("mod varsayılanı: %q", got.Mode)
	}
	if got.Difficulty != DifficultyNormal {
		t.Errorf("zorluk varsayılanı: %q", got.Difficulty)
	}
	if got.SlabChunks != DefaultSlabChunks {
		t.Errorf("dilim varsayılanı: %d", got.SlabChunks)
	}
	if got.LinkPort != DefaultLinkPort {
		t.Errorf("link portu varsayılanı: %d", got.LinkPort)
	}
	// Sıfır bir port sunucuyu açılışta çökertirdi.
	if got.Port == 0 {
		t.Error("Minecraft portu sıfır kaldı")
	}
	if got.RAMMB == 0 {
		t.Error("RAM sıfır kaldı — sunucu başlayamaz")
	}
	if got.ServerName == "" {
		t.Error("sunucu adı boş kaldı")
	}
}

func TestDifficultyLabels(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range AllDifficulties {
		l := DifficultyLabel(d)
		if l == "" || l == string(d) {
			t.Errorf("%q için Türkçe etiket yok: %q", d, l)
		}
		if seen[l] {
			t.Errorf("etiket tekrar ediyor: %q", l)
		}
		seen[l] = true
	}
}
