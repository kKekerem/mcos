// Package fbui composes fbdraw (vector shapes) and fbfont (text) into the
// MCOS interface widgets.
//
// Bu katman, panelin görünümünün TEK tanımıdır: renkler, ölçüler, yarıçaplar
// ve her widget'ın nasıl çizildiği burada. Hiçbir ekran kodu ham renk veya ham
// sayı yazmaz.
//
// Terminal katmanından farkı: burada hiçbir şey karakter hücresine hapsolmaz.
// Bir kenarlık 2 piksel kalınlıkta, bir köşe 10 piksel yarıçaplı gerçek yay
// olabilir. Ölçüler PİKSELDİR.
package fbui

import "image/color"

// Palette holds the interface colours as true 24-bit RGB.
//
// Terminalde 16 palet yuvasıyla sınırlıydık ve parlak tonlara yalnızca "bold"
// ile erişilebiliyordu (fbterm 256 renk SGR'sini desteklemiyor). Kendi
// pikselimizi çizdiğimiz için o sınır tamamen kalktı: renkler artık tam olarak
// tasarlandıkları değerde çiziliyor.
type Palette struct {
	// Zeminler
	Bg      color.RGBA // sayfa arka planı
	Surface color.RGBA // kart / panel yüzeyi (arka plandan hafif açık)
	Raised  color.RGBA // öne çıkan yüzey (seçili satır, buton dolgusu)

	// Çizgiler
	Border      color.RGBA // normal kenarlık
	BorderFocus color.RGBA // odaklı kenarlık
	Divider     color.RGBA // ayırıcı çizgi (kenarlıktan soluk)

	// Metin
	Text      color.RGBA // ana metin
	TextDim   color.RGBA // ikincil metin
	TextFaint color.RGBA // en soluk (yardım, pasif)
	TextOn    color.RGBA // vurgu dolgusu ÜZERİNDEKİ metin

	// Anlam
	Accent color.RGBA // birincil eylem, seçim, odak
	OK     color.RGBA // çalışıyor / başarılı
	Warn   color.RGBA // uyarı / başlıyor
	Error  color.RGBA // hata
}

// DefaultPalette is the graphite/teal look.
//
// Arka plan ile kenarlık arasındaki kontrast BİLEREK yüksek tutuldu: önceki
// terminal paletinde kenarlık (#2f3945) arka plandan (#0f1216) ancak ~40
// parlaklık farklıydı ve düşük kontrastlı ekranlarda çerçeveler kayboluyordu.
var DefaultPalette = Palette{
	Bg:      rgb(0x0F1216),
	Surface: rgb(0x161B21),
	Raised:  rgb(0x1E252D),

	Border:      rgb(0x39485A),
	BorderFocus: rgb(0x23A99C),
	Divider:     rgb(0x26303B),

	Text:      rgb(0xDCE3EA),
	TextDim:   rgb(0x8D9AA8),
	TextFaint: rgb(0x606D7B),
	TextOn:    rgb(0x07120F),

	Accent: rgb(0x23A99C),
	OK:     rgb(0x46A758),
	Warn:   rgb(0xD9A21B),
	Error:  rgb(0xE5484D),
}

// Accents maps a theme name to its accent colour. Diğer renkler ortak kalır:
// tema değiştirmek okunabilirliği değiştirmemeli, yalnızca kimliği.
var Accents = map[string]color.RGBA{
	"graphite-teal":     rgb(0x23A99C),
	"noir-purple":       rgb(0x8B5CF6),
	"anthracite-orange": rgb(0xE07A3F),
	"anthracite-green":  rgb(0x46A758),
	"crimson-night":     rgb(0xD64550),
	"amber-graphite":    rgb(0xD9A21B),
}

// WithAccent returns the palette re-tinted for a named theme.
func (p Palette) WithAccent(theme string) Palette {
	if a, ok := Accents[theme]; ok {
		p.Accent = a
		p.BorderFocus = a
	}
	return p
}

// Metrics holds every pixel dimension the interface uses.
//
// Ölçüler font boyutundan TÜRETİLİR, sabit değil: kullanıcı çözünürlüğü veya
// yazı tipi boyutunu değiştirdiğinde arayüz orantılı büyür. Sabit piksel
// değerleri 4K ekranda minik, 800x600'de devasa görünürdü.
type Metrics struct {
	CellW, CellH int // font hücresi (fbfont'tan)

	PadX, PadY    int     // kart içi dolgu
	Gap           int     // bileşenler arası boşluk
	Radius        float64 // kart / pencere köşe yarıçapı
	RadiusSmall   float64 // buton / küçük öğe yarıçapı
	Stroke        float64 // normal kenarlık kalınlığı
	StrokeFocus   float64 // odaklı kenarlık kalınlığı
	DividerStroke float64 // ayırıcı çizgi kalınlığı
	ButtonH       int     // buton yüksekliği
	ButtonMinW    int     // buton en az genişliği
	RowH          int     // liste satırı yüksekliği
	MarkerR       float64 // radyo/onay işaret yarıçapı
}

// MetricsFor derives all dimensions from the font cell size.
func MetricsFor(cellW, cellH int) Metrics {
	return Metrics{
		CellW: cellW, CellH: cellH,

		PadX: cellW * 2,
		PadY: cellH / 2,
		Gap:  cellW,

		// Yarıçap hücre yüksekliğine oranlı: küçük fontta küçük, büyükte büyük.
		Radius:      float64(cellH) * 0.55,
		RadiusSmall: float64(cellH) * 0.40,

		Stroke:        maxF(1, float64(cellH)/14),
		StrokeFocus:   maxF(2, float64(cellH)/9),
		DividerStroke: 1,

		ButtonH:    cellH*2 - cellH/3,
		ButtonMinW: cellW * 14,
		RowH:       cellH + cellH/3,
		MarkerR:    float64(cellH) * 0.28,
	}
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 255,
	}
}
