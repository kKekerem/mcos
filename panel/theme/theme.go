// Package theme defines the MCOS panel palette, design tokens and the reusable
// Lip Gloss styles built from them.
//
// Panel fbterm (framebuffer terminali) altında TrueType fontla çalışır, bu
// yüzden tüm Unicode karakterler (yuvarlak kenarlar, box-drawing, Türkçe
// glifler) doğru çizilir — ŞARTIYLA ki birincil font gerçekten monospace bir
// metin fontu olsun. Font yığını board/mcos/post-build.sh içinde tanımlıdır.
//
// Renk yeteneğinin neden 0-7 + bold ile sınırlı olduğu palette.go'da
// ölçümlerle açıklanıyor.
package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// Palette maps semantic roles to fbterm palette slots. Değerler ham sayı
// DEĞİL, palette.go'daki Slot* sabitleridir.
type Palette struct {
	Bg     lipgloss.Color // sayfa arka planı
	Border lipgloss.Color // kenarlık / ayırıcı
	Accent lipgloss.Color // seçim, odak, başlık
	Text   lipgloss.Color // ana metin
	Muted  lipgloss.Color // ikincil metin, ipucu
	OK     lipgloss.Color // çalışıyor / başarılı
	Warn   lipgloss.Color // başlıyor / uyarı
	Error  lipgloss.Color // hata / çöktü

	// Eski adlar. Görünüm dosyaları hâlâ bunları kullanıyor; OK/Warn/Error ile
	// aynı yuvaya bakarlar. Yeni kod semantik adları kullanmalı.
	Green  lipgloss.Color // = OK
	Yellow lipgloss.Color // = Warn
	Red    lipgloss.Color // = Error
	Blue   lipgloss.Color // = Accent (fbterm'de ayrı bir mavi yuva ayrılmadı)
}

// semanticPalette, TÜM temaların paylaştığı rol→yuva eşlemesi.
//
// Temalar arasında değişen tek şey 6. yuvanın RGB değeridir (palette.go
// içindeki accents tablosu). Rol eşlemesi sabit kalır, böylece hiçbir temada
// kenarlık ile ikincil metin karışmaz — eski kodda ikisi de "5" yuvasındaydı.
var semanticPalette = Palette{
	Bg:     SlotBg,
	Border: SlotBorder,
	Accent: SlotAccent,
	Text:   SlotText,
	Muted:  SlotMuted,
	OK:     SlotOK,
	Warn:   SlotWarn,
	Error:  SlotError,

	Green:  SlotOK,
	Yellow: SlotWarn,
	Red:    SlotError,
	Blue:   SlotAccent,
}

const defaultTheme = "graphite-teal"

// themeOrder is the selectable theme order shown in the first-boot wizard.
var themeOrder = []string{
	"graphite-teal", "noir-purple", "anthracite-orange",
	"anthracite-green", "crimson-night", "amber-graphite",
}

// themeLabels are human-friendly Turkish names for the picker.
var themeLabels = map[string]string{
	"graphite-teal":     "Grafit Teal",
	"noir-purple":       "Noir Mor",
	"anthracite-orange": "Antrasit Turuncu",
	"anthracite-green":  "Antrasit Yeşil",
	"crimson-night":     "Gece Kızılı",
	"amber-graphite":    "Kehribar Grafit",
}

// Names returns the selectable theme names in display order.
func Names() []string { return themeOrder }

// Label returns a human-friendly name for a theme.
func Label(name string) string {
	if l, ok := themeLabels[name]; ok {
		return l
	}
	return name
}

// Valid reports whether name is a known theme.
func Valid(name string) bool {
	_, ok := accents[name]
	return ok
}

// AccentHex returns a theme's accent colour as an RRGGBB string. Tema
// seçicisinde önizleme için kullanılır.
func AccentHex(name string) string {
	if a, ok := accents[name]; ok {
		return a.base
	}
	return accents[defaultTheme].base
}

// Theme bundles the semantic palette with precomputed Lip Gloss styles.
//
// Görünümler stil ÜRETMEZ; buradaki hazır stilleri kullanır. Böylece renk ve
// kenarlık kararları tek yerde kalır.
type Theme struct {
	Name string
	P    Palette

	// Metin stilleri
	Title    lipgloss.Style // ekran başlığı (bold text → parlak)
	Heading  lipgloss.Style // kart başlığı (bold accent)
	Body     lipgloss.Style // normal metin
	Muted    lipgloss.Style // ikincil metin / ipucu
	Label    lipgloss.Style // "etiket :" kolonu, sabit genişlik
	Value    lipgloss.Style // değer kolonu
	Selected lipgloss.Style // seçili satır
	Disabled lipgloss.Style // pasif satır

	// Durum stilleri
	OK    lipgloss.Style
	Warn  lipgloss.Style
	Error lipgloss.Style

	// Çerçeveler
	Card        lipgloss.Style // yuvarlak kenarlı kart
	CardFocused lipgloss.Style // odaklı kart (bold kenarlık)
	Divider     lipgloss.Style // yatay ayırıcı

	// Etkileşim
	Button        lipgloss.Style // normal buton
	ButtonFocused lipgloss.Style // odaklı buton
	KeyCap        lipgloss.Style // tuş kapağı
	Input         lipgloss.Style // metin girişi çerçevesi
	InputFocused  lipgloss.Style

	// ── Eski adlar (geçiş katmanı) ──────────────────────────────────────────
	// Görünüm dosyaları (view_setup, view_wizard, view_detail…) bu adları
	// yoğun kullanıyor: Val 49, Accent 49, CardTitle 18, Key 13 yerde.
	// Yeni semantik stillere BAĞLANMIŞLARDIR — yani eski çağrılar da yeni
	// paleti, yuvarlak kenarları ve doğru renkleri kullanır.
	//
	// YENİ KOD BUNLARI KULLANMAMALI; karşılıkları yorumda.
	App        lipgloss.Style // → Body
	CardTitle  lipgloss.Style // → Heading
	Key        lipgloss.Style // → Label
	Val        lipgloss.Style // → Value
	Accent     lipgloss.Style // → Selected
	Help       lipgloss.Style // → Muted
	MenuItem   lipgloss.Style // → Body (dolgulu)
	MenuActive lipgloss.Style // → Selected
}

// New builds a Theme for a named theme, falling back to the default.
//
// NOT: lipgloss.SetColorProfile burada ÇAĞRILMAZ. Global durum tema
// kurucusunun içinde olmamalı; program açılışında bir kez SetupTerminal()
// çağrılır.
func New(name string) *Theme {
	if !Valid(name) {
		name = defaultTheme
	}
	p := semanticPalette
	t := &Theme{Name: name, P: p}

	// ── Metin ───────────────────────────────────────────────────────────────
	// Bold, fbterm'de rengi aynı tonun parlak karşılığına çevirir (fcolor ^= 8),
	// bu yüzden "bold" burada gerçek bir vurgu aracıdır.
	t.Title = lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	t.Heading = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	t.Body = lipgloss.NewStyle().Foreground(p.Text)
	t.Muted = lipgloss.NewStyle().Foreground(p.Muted)
	t.Label = lipgloss.NewStyle().Foreground(p.Muted).Width(LabelWidth)
	t.Value = lipgloss.NewStyle().Foreground(p.Text)
	t.Selected = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	// Faint KULLANILMAZ: fbterm faint'i her zaman 8. yuvaya sabitler
	// (fbshell.cpp:701), yani rengi kontrol edemezdik. Pasif öğeler için
	// açıkça Muted rengi kullanılıyor.
	t.Disabled = lipgloss.NewStyle().Foreground(p.Muted)

	// ── Durum ───────────────────────────────────────────────────────────────
	t.OK = lipgloss.NewStyle().Foreground(p.OK).Bold(true)
	t.Warn = lipgloss.NewStyle().Foreground(p.Warn).Bold(true)
	t.Error = lipgloss.NewStyle().Foreground(p.Error).Bold(true)

	// ── Çerçeveler ──────────────────────────────────────────────────────────
	// RoundedBorder: fbterm TrueType işlediği için ╭╮╰╯ doğru çizilir.
	t.Card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Padding(CardPadY, CardPadX)
	t.CardFocused = t.Card.
		BorderForeground(p.Accent)
	t.Divider = lipgloss.NewStyle().Foreground(p.Border)

	// ── Etkileşim ───────────────────────────────────────────────────────────
	// Butonlar GERÇEK çerçeveli: eski renderPill yalnızca renkli metin
	// üretiyordu (bg parametresini hiç kullanmıyordu), bu yüzden butonlar
	// buton gibi görünmüyordu.
	t.Button = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Foreground(p.Text).
		Padding(0, ButtonPadX)
	t.ButtonFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Accent).
		Foreground(p.Accent).
		Bold(true).
		Padding(0, ButtonPadX)

	t.KeyCap = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Foreground(p.Accent).
		Bold(true).
		Padding(0, KeyCapPadX)

	t.Input = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Foreground(p.Text).
		Padding(0, 1)
	t.InputFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Accent).
		Foreground(p.Text).
		Padding(0, 1)

	// ── Eski adları yeni stillere bağla ─────────────────────────────────────
	t.App = t.Body
	t.CardTitle = t.Heading
	t.Key = t.Label
	t.Val = t.Value
	t.Accent = t.Selected
	t.Help = t.Muted
	t.MenuItem = lipgloss.NewStyle().Foreground(p.Text).Padding(0, 1)
	t.MenuActive = t.Selected

	return t
}

// Badge renders a compact, text-only status marker in the given colour.
// Arka plan bloğu KULLANILMAZ: renk durumu tek başına anlatır ve terminal sakin
// kalır.
func (t *Theme) Badge(label string, fg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(label)
}

// Swatch returns a solid colour preview block for the theme picker.
func Swatch(c lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(c).Render("██████")
}

// AccentColor returns the accent slot for a named theme.
//
// fbterm'de vurgu her temada AYNI yuvadadır (6); temalar arası fark o yuvanın
// RGB değerinde (bkz. palette.go accents). Bu yüzden önizleme için gerçek RGB
// gerekiyorsa AccentHex() kullanılmalıdır.
func AccentColor(string) lipgloss.Color { return SlotAccent }
