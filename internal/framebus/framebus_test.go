//go:build unix

package framebus

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

func kareUret(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// Okuyucu yokken yazıcı HİÇBİR piksel kopyalamamalı (4K'da saniyede GB'lar).
func TestOkuyucuYokkenKopyaYok(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kare")
	wr, err := Create(yol, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer wr.Close()
	wr.Publish(kareUret(8, 4, color.RGBA{R: 200, A: 255}))
	for _, b := range wr.mem[headerSize:] {
		if b != 0 {
			t.Fatal("okuyucu yokken piksel kopyalandı")
		}
	}
}

// Okuyucu bağlanınca panel boşta olsa bile (yeni kare yok) son kare gelmeli.
func TestOkuyucuBaglanincaSonKareGelir(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kare")
	wr, err := Create(yol, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer wr.Close()
	wr.Publish(kareUret(8, 4, color.RGBA{G: 123, A: 255})) // okuyucu yok: kopya yok

	r, err := Open(yol)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	wr.Heartbeat() // panelin saniyelik yoklaması
	dst := make([]byte, 8*4*4)
	if err := r.Snapshot(dst); err != nil {
		t.Fatal(err)
	}
	if dst[1] != 123 {
		t.Fatalf("son kare gelmedi: % x", dst[:8])
	}
}

func TestBoyutDegisinceOkuyucuHaberdar(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kare")
	wr, err := Create(yol, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer wr.Close()
	r, err := Open(yol)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := wr.Resize(16, 8); err != nil {
		t.Fatal(err)
	}
	if err := r.Snapshot(make([]byte, 8*4*4)); err != ErrResized {
		t.Fatalf("ErrResized bekleniyordu, %v", err)
	}
	if w, h := r.CurrentSize(); w != 16 || h != 8 {
		t.Fatalf("yeni boyut okunamadı: %dx%d", w, h)
	}
}

// Kapalı paneli okuyucu "canlı" sanmamalı: VNC /dev/fb0'a düşebilsin.
func TestKapaliPanelCanliSayilmaz(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "kare")
	wr, err := Create(yol, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	wr.Close()
	if _, err := Open(yol); err == nil {
		t.Fatal("kapalı panelin dosyası açıldı")
	}
}
