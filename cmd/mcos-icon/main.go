// Command mcos-icon writes the MCOS mark as a square PNG.
//
// Windows .exe simgeleri (mcos-flash, masaüstü uygulaması) bundan üretiliyor.
// Simge ELLE çizilmiş bir dosya değil, panelin açılış ekranında çizdiği
// küpün TA KENDİSİ (fbui.Logo): marka tek bir yerde tanımlı kalsın, simge ile
// açılış ekranı bir gün birbirinden ayrışmasın.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
)

func main() {
	out := flag.String("o", "mcos-icon.png", "çıktı PNG yolu")
	size := flag.Int("size", 256, "kenar uzunluğu (piksel)")
	flag.Parse()

	img := image.NewRGBA(image.Rect(0, 0, *size, *size))
	p := fbdraw.New(img)
	// Arka plan SAYDAM kalıyor: Windows simgeyi görev çubuğunun ve
	// masaüstünün kendi rengi üstüne koyar.
	u := &fbui.UI{P: p, Pal: fbui.DefaultPalette}
	s := float64(*size)
	// Küp, kenarlardan %8 pay bırakacak büyüklükte. Logo'nun yüksekliği
	// genişliğinin ~1,08 katı (üst yüz + yan yüz), bu yüzden ölçü yüksekliğe
	// göre alınıyor ki alt köşe kırpılmasın.
	u.Logo(s/2, s/2, s*0.84/1.08, u.Pal.Accent)

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcos-icon:", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, "mcos-icon:", err)
		os.Exit(1)
	}
}
