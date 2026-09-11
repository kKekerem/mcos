//go:build !linux

package fbdev

import (
	"fmt"
	"image"
)

// Framebuffer erişimi Linux'a özgüdür (/dev/fb0, ioctl, mmap). Geliştirici
// makinelerinde (Windows/macOS) açık bir hata döner — sessiz no-op, panelin
// neden boş kaldığını gizlerdi.
//
// Kanvas katmanı (canvas.go) her platformda çalışır, bu yüzden PNG üreten
// doğrulama yolu geliştirici makinesinde de kullanılabilir.

// Device is unavailable off Linux.
type Device struct{}

// Open is unsupported off Linux.
func Open(string) (*Device, error) {
	return nil, fmt.Errorf("fbdev: framebuffer yalnızca Linux'ta desteklenir")
}

func (*Device) Size() (int, int)  { return 0, 0 }
func (*Device) BitsPerPixel() int { return 0 }
func (*Device) Info() string      { return "framebuffer yok" }
func (*Device) Flip(*image.RGBA) error {
	return fmt.Errorf("fbdev: framebuffer yalnızca Linux'ta desteklenir")
}
func (*Device) Close() error { return nil }
