package providers

import (
	"sort"
	"testing"

	"mcos/internal/mcver"
)

// TestSurumSiralamasi, metin sıralamasının ürettiği gerçek hatayı sabitliyor:
// düz sort.Strings ile "1.21.11" listenin SONUNA düşüyordu. Karşılaştırma
// artık ortak mcver paketinde (paper.go onu kullanıyor); 26.x de eklendi.
func TestSurumSiralamasi(t *testing.T) {
	in := []string{"1.21.4", "26.1", "1.21.11", "1.21.8", "26.1.2", "1.20.6", "1.21", "26.3", "1.21.5"}
	istenen := []string{"26.3", "26.1.2", "26.1", "1.21.11", "1.21.8", "1.21.5", "1.21.4", "1.21", "1.20.6"}

	out := append([]string(nil), in...)
	mcver.SortNewestFirst(out)
	for i := range istenen {
		if out[i] != istenen[i] {
			t.Fatalf("sıra yanlış:\n aldım:  %v\n istedim: %v", out, istenen)
		}
	}

	// Karşı sınama: metin sıralaması bu testi GEÇMEMELİ, yoksa test bir şey
	// kanıtlamıyor demektir.
	metin := append([]string(nil), in...)
	sort.Sort(sort.Reverse(sort.StringSlice(metin)))
	for i := range istenen {
		if metin[i] != istenen[i] {
			return
		}
	}
	t.Fatal("metin sıralaması da doğru sonuç verdi: test bir şey kanıtlamıyor")
}
