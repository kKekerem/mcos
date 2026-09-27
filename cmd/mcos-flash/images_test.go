package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEnYeniImajOnceGelir, "bayat imaj yazılıyordu" hatasını sabitliyor:
// boyuta göre sıralayınca 4 GB'lık ESKİ imaj listenin başına çıkıyordu.
func TestEnYeniImajOnceGelir(t *testing.T) {
	dir := t.TempDir()
	simdi := time.Now()
	yaz := func(ad string, boyut int64, yas time.Duration) {
		p := filepath.Join(dir, ad)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(boyut); err != nil {
			t.Fatal(err)
		}
		f.Close()
		m := simdi.Add(-yas)
		if err := os.Chtimes(p, m, m); err != nil {
			t.Fatal(err)
		}
	}
	// dist/ içinde ölçülen gerçek düzen: en büyük olan en eski.
	yaz("mcos-uefi.img", 64<<20, 7*24*time.Hour)
	yaz("mcos-x86_64.iso", 32<<20, 0)
	yaz("mcos-x86_64.img", 48<<20, 100*24*time.Hour)

	im := findImages([]string{dir})
	if len(im) != 3 {
		t.Fatalf("3 imaj bekleniyordu, %d bulundu", len(im))
	}
	if filepath.Base(im[0].path) != "mcos-x86_64.iso" {
		t.Fatalf("ilk sırada en yeni imaj olmalı, %s geldi", im[0].path)
	}
	if bayatMi(im[0], im[0]) {
		t.Fatal("en yeni imaj kendine göre bayat sayıldı")
	}
	if !bayatMi(im[1], im[0]) || !bayatMi(im[2], im[0]) {
		t.Fatal("eski imajlar bayat olarak işaretlenmedi")
	}
}
