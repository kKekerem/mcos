package fbdraw

import (
	"image"
	"testing"
)

// Açılış geçişinin GERÇEK kare maliyeti.
//
// Kullanıcının isteği "blurlar geçiş efektleri dönen tekerlek felan çok iyi
// olsun" idi. "Çok iyi" ölçülebilir bir şeydir: 1080p'de kare başına 16 ms'nin
// altı 60 fps, 33 ms'nin altı 30 fps demektir. Bunun üstü gözle görülür
// takılmadır.
func BenchmarkZoomBlurFade1080p(b *testing.B) {
	bounds := image.Rect(0, 0, 1920, 1080)
	dst := image.NewRGBA(bounds)
	from := image.NewRGBA(bounds)
	scratch := image.NewRGBA(bounds)
	for i := range from.Pix {
		from.Pix[i] = uint8(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Geçişin ortası: en pahalı an (bulanıklık büyümüş, ölçek artmış).
		ZoomBlurFade(dst, from, scratch, bounds, 1.18, 9, 0.5)
	}
}

func BenchmarkZoom1080p(b *testing.B) {
	bounds := image.Rect(0, 0, 1920, 1080)
	dst := image.NewRGBA(bounds)
	src := image.NewRGBA(bounds)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Zoom(dst, src, bounds, 1.18)
	}
}

func BenchmarkCrossFade1080p(b *testing.B) {
	bounds := image.Rect(0, 0, 1920, 1080)
	dst := image.NewRGBA(bounds)
	from := image.NewRGBA(bounds)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CrossFade(dst, from, bounds, 0.5)
	}
}
