// Package fbfont rasterises text for the framebuffer UI.
//
// Tasarım kararları:
//
//   - Font İKİLİYE GÖMÜLÜ (go:embed). Çalışma zamanında fontconfig, dosya yolu
//     veya fbterm yapılandırması aranmaz. Geçen oturumda "unicode görünmüyor"
//     hatasının kökü tam olarak bu zincirdi: .fbtermrc'deki birincil font
//     sistemde kurulu değildi ve fbterm harfleri glifi olmayan bir emoji
//     fontuyla çizmeye çalışıyordu. Gömülü font o hata sınıfını tamamen yok
//     eder.
//
//   - MONOSPACE HÜCRE IZGARASI. Panel düzeni (lipgloss) karakter hücresi
//     varsayar; piksel çizici de aynı ızgarayı kullanır, böylece mevcut
//     görünüm birebir korunur. Hücre genişliği fontun ilerlemesinden
//     hesaplanır ve TAM SAYIYA yuvarlanır — kesirli ilerleme satır sonunda
//     birikip sütunları kaydırırdı.
//
//   - GLİF ÖNBELLEĞİ. Her rune bir kez rasterize edilir. Panel saniyede birkaç
//     kez tam kare çizdiği için bu şart: önbelleksiz her karede binlerce glif
//     yeniden rasterize edilirdi.
package fbfont

import (
	_ "embed"
	"fmt"
	"image"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// FiraCode Nerd Font: monospace, box-drawing + Türkçe glifler + ikonlar.
// Aynı dosya rootfs'e de kurulur (os/buildroot/external/package/nerd-font),
// ama framebuffer paneli DİSKTEKİ kopyaya bakmaz — buradaki gömülü kopyayı
// kullanır.
//
//go:embed fonts/FiraCodeNerdFont-Regular.ttf
var firaCodeTTF []byte

// Face is a rasterised monospace font at one pixel size.
//
// Eşzamanlı kullanım güvenlidir: Glyph() önbelleği kendi kilidiyle korur.
type Face struct {
	face font.Face

	// CellW / CellH, karakter hücresinin piksel ölçüsü. Tüm düzen bu ızgaraya
	// oturur.
	CellW, CellH int
	// Baseline, hücrenin üstünden taban çizgisine olan piksel uzaklığı.
	Baseline int
	// SizePx, istenen piksel boyutu (tanı amaçlı).
	SizePx float64

	mu    sync.RWMutex
	cache map[rune]*Glyph
}

// Glyph is one rasterised character: an 8-bit coverage mask plus where to put it.
type Glyph struct {
	// Mask, alfa kapsama maskesi (gri tonlamalı kenar yumuşatma).
	// Boş glifler (boşluk gibi) için nil olabilir.
	Mask *image.Alpha
	// OffX / OffY, maskenin hücrenin sol-üst köşesine göre konumu.
	OffX, OffY int
	// Missing, fontta bu rune için glif olmadığını bildirir. Çizici bunu
	// görünür bir yer tutucuyla gösterebilir; sessizce boş bırakmak, eksik
	// fontu teşhis etmeyi imkânsız kılardı.
	Missing bool
}

// Load rasterises the embedded font at sizePx pixels.
//
// sizePx, em kutusunun piksel yüksekliğidir; hücre ölçüsü buradan türetilir.
func Load(sizePx float64) (*Face, error) {
	if sizePx < 6 {
		return nil, fmt.Errorf("fbfont: punto çok küçük: %.1f (en az 6)", sizePx)
	}
	if sizePx > 96 {
		return nil, fmt.Errorf("fbfont: punto çok büyük: %.1f (en fazla 96)", sizePx)
	}

	ttf, err := opentype.Parse(firaCodeTTF)
	if err != nil {
		return nil, fmt.Errorf("fbfont: gömülü font ayrıştırılamadı: %w", err)
	}

	// DPI 72 seçildi: bu durumda "punto" doğrudan piksele eşittir, yani
	// sizePx gerçekten piksel olur ve ölçek hesabı iki yerde tekrarlanmaz.
	ff, err := opentype.NewFace(ttf, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     72,
		Hinting: font.HintingFull, // küçük puntolarda kenarları ızgaraya oturtur
	})
	if err != nil {
		return nil, fmt.Errorf("fbfont: yüz oluşturulamadı: %w", err)
	}

	m := ff.Metrics()

	// Hücre genişliği: monospace fontta her glifin ilerlemesi aynıdır; 'M'
	// üzerinden ölçüp yuvarlıyoruz. Kesirli ilerlemeyi yuvarlamamak, 80
	// kolonluk bir satırda birkaç piksellik kayma biriktirirdi.
	adv, ok := ff.GlyphAdvance('M')
	if !ok {
		ff.Close()
		return nil, fmt.Errorf("fbfont: gömülü font 'M' glifi içermiyor (bozuk font?)")
	}
	cellW := roundFixed(adv)
	if cellW < 1 {
		ff.Close()
		return nil, fmt.Errorf("fbfont: hesaplanan hücre genişliği geçersiz: %d", cellW)
	}

	// Hücre yüksekliği: ascent + descent. Satır aralığı EKLENMEZ — panel
	// düzeni bitişik satırlar varsayar ve box-drawing karakterlerinin dikey
	// olarak birleşmesi buna bağlıdır.
	cellH := roundFixed(m.Ascent) + roundFixed(m.Descent)
	if cellH < 1 {
		ff.Close()
		return nil, fmt.Errorf("fbfont: hesaplanan hücre yüksekliği geçersiz: %d", cellH)
	}

	return &Face{
		face:     ff,
		CellW:    cellW,
		CellH:    cellH,
		Baseline: roundFixed(m.Ascent),
		SizePx:   sizePx,
		cache:    make(map[rune]*Glyph, 512),
	}, nil
}

// Close releases the underlying font face.
func (f *Face) Close() error {
	if f.face == nil {
		return nil
	}
	err := f.face.Close()
	f.face = nil
	return err
}

// Glyph returns the rasterised mask for r, caching the result.
//
// Asla nil dönmez: bilinmeyen rune için Missing=true olan bir glif döner,
// böylece çağıran taraf nil denetimi yapmak zorunda kalmaz.
func (f *Face) Glyph(r rune) *Glyph {
	f.mu.RLock()
	g, ok := f.cache[r]
	f.mu.RUnlock()
	if ok {
		return g
	}

	g = f.rasterise(r)

	f.mu.Lock()
	f.cache[r] = g
	f.mu.Unlock()
	return g
}

// rasterise draws one rune into a fresh alpha mask.
func (f *Face) rasterise(r rune) *Glyph {
	// GlyphBounds, glifin taban çizgisine göre sınırlarını verir.
	bounds, _, ok := f.face.GlyphBounds(r)
	if !ok {
		// Fontta yok. Görünür bir yer tutucu çizmek çağıranın işi; burada
		// yalnızca durumu bildiriyoruz.
		return &Glyph{Missing: true}
	}

	x0 := floorFixed(bounds.Min.X)
	y0 := floorFixed(bounds.Min.Y)
	x1 := ceilFixed(bounds.Max.X)
	y1 := ceilFixed(bounds.Max.Y)

	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 {
		// Boşluk gibi çizilecek pikseli olmayan glifler. Eksik DEĞİL.
		return &Glyph{}
	}

	// Makul olmayan boyutlara karşı koruma: bozuk bir font devasa bir maske
	// ayırtmamıza yol açmasın.
	if w > 4*f.CellW+64 || h > 4*f.CellH+64 {
		return &Glyph{Missing: true}
	}

	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(image.White.C),
		Face: f.face,
		// Glifi maskenin içine oturt: taban çizgisi -y0'da, sol kenar -x0'da.
		Dot: fixed.Point26_6{
			X: fixed.I(-x0),
			Y: fixed.I(-y0),
		},
	}
	d.DrawString(string(r))

	// OffY: maskenin üst kenarının HÜCRE üstüne göre konumu.
	// y0 taban çizgisine göre negatiftir (yukarı), bu yüzden Baseline eklenir.
	return &Glyph{
		Mask: dst,
		OffX: x0,
		OffY: f.Baseline + y0,
	}
}

// Grid returns how many whole cells fit in a pixel area.
func (f *Face) Grid(pxW, pxH int) (cols, rows int) {
	if f.CellW > 0 {
		cols = pxW / f.CellW
	}
	if f.CellH > 0 {
		rows = pxH / f.CellH
	}
	return cols, rows
}

// Info returns a one-line description for the boot log.
func (f *Face) Info() string {
	return fmt.Sprintf("%.0fpx hücre=%dx%d taban=%d", f.SizePx, f.CellW, f.CellH, f.Baseline)
}

// ── fixed.Int26_6 yardımcıları ──────────────────────────────────────────────
//
// fixed.Int26_6, 1/64 piksel çözünürlüklü sabit noktalı sayıdır.

func roundFixed(v fixed.Int26_6) int { return int((v + 32) >> 6) }
func floorFixed(v fixed.Int26_6) int { return int(v >> 6) }
func ceilFixed(v fixed.Int26_6) int  { return int((v + 63) >> 6) }
