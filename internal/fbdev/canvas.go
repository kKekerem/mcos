// Package fbdev owns the pixel surface: a portable in-memory canvas plus, on
// Linux, the real /dev/fb0 framebuffer it is flipped onto.
//
// Tasarım: çizim yapan hiçbir kod framebuffer'ı bilmez. Her şey bir
// *image.RGBA üzerine çizer (saf Go, donanım gerekmez); Device yalnızca o
// tamponu ekranın piksel biçimine çevirip yazar. Bu sayede tüm çizim katmanı
// (fbfont, fbrender) donanım olmadan test edilebilir ve PNG olarak
// doğrulanabilir.
package fbdev

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// NewCanvas returns an opaque w×h RGBA surface filled with bg.
//
// *image.RGBA bilerek seçildi: stdlib'in draw/png paketleriyle doğrudan
// çalışır, yani glif bindirme ve PNG kaydı için ek kod gerekmez.
func NewCanvas(w, h int, bg color.RGBA) *image.RGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	Fill(img, img.Bounds(), bg)
	return img
}

// Fill paints a solid rectangle. draw.Src kullanılır (Over değil): arka plan
// tamamen değişir, altındaki eski kare sızmaz.
func Fill(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(dst, r.Intersect(dst.Bounds()), &image.Uniform{C: c}, image.Point{}, draw.Src)
}

// SavePNG writes img to path, creating parent directories as needed.
//
// Faz 1'in doğrulama yolu bu: donanım olmadan gerçek çıktıyı görebilmek.
func SavePNG(img image.Image, path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("fbdev: dizin oluşturulamadı %s: %w", dir, err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("fbdev: PNG oluşturulamadı %s: %w", path, err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return fmt.Errorf("fbdev: PNG kodlanamadı %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("fbdev: PNG kapatılamadı %s: %w", path, err)
	}
	return nil
}
