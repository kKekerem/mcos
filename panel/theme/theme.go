// Package theme defines the MCOS panel color palette and reusable Lip Gloss
// styles. The panel runs exclusively under fbterm (framebuffer terminal) which
// renders TrueType fonts — so ALL Unicode characters (rounded borders, emojis,
// box-drawing, Turkish glyphs) display perfectly, like a GUI application.
package theme

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Palette holds the raw colors for a theme.
type Palette struct {
	Bg       lipgloss.Color // page background
	Surface  lipgloss.Color // card / panel surface
	SurfaceA lipgloss.Color // alternate surface (selected row, headers)
	Border   lipgloss.Color // default border
	Accent   lipgloss.Color // primary accent (selection, highlights)
	Dim      lipgloss.Color // dim accent / secondary border
	Text     lipgloss.Color // primary text
	Muted    lipgloss.Color // secondary text
	Green    lipgloss.Color // running / ok
	Yellow   lipgloss.Color // starting / warn
	Red      lipgloss.Color // error / stopped-bad
	Blue     lipgloss.Color // info (used sparingly)
}

// Named palettes.
const defaultPalette = "graphite-teal"

var palettes = map[string]Palette{
	"graphite-teal": {
		Bg: "0", Surface: "0", SurfaceA: "0",
		Border: "5", Accent: "6", Dim: "5",
		Text: "7", Muted: "5",
		Green: "2", Yellow: "3", Red: "1", Blue: "6",
	},
	"noir-purple": {
		Bg: "0", Surface: "0", SurfaceA: "0",
		Border: "5", Accent: "5", Dim: "5",
		Text: "7", Muted: "5",
		Green: "2", Yellow: "3", Red: "1", Blue: "6",
	},
	"anthracite-orange": {
		Bg: "0", Surface: "0", SurfaceA: "0",
		Border: "5", Accent: "3", Dim: "5",
		Text: "7", Muted: "5",
		Green: "2", Yellow: "3", Red: "1", Blue: "6",
	},
	"anthracite-green": {
		Bg: "0", Surface: "0", SurfaceA: "0",
		Border: "5", Accent: "2", Dim: "5",
		Text: "7", Muted: "5",
		Green: "2", Yellow: "3", Red: "1", Blue: "6",
	},
	"crimson-night": {
		Bg: "0", Surface: "0", SurfaceA: "0",
		Border: "5", Accent: "1", Dim: "5",
		Text: "7", Muted: "5",
		Green: "2", Yellow: "3", Red: "1", Blue: "6",
	},
	"amber-graphite": {
		Bg: "0", Surface: "0", SurfaceA: "0",
		Border: "5", Accent: "3", Dim: "5",
		Text: "7", Muted: "5",
		Green: "2", Yellow: "3", Red: "1", Blue: "6",
	},
}

// themeOrder is the selectable palette order shown in the first-boot wizard's
// theme step. The default is balanced graphite/teal; the rest are alternate
// accent moods for users who want a warmer or sharper console.
var themeOrder = []string{
	"graphite-teal", "noir-purple", "anthracite-orange", "anthracite-green", "crimson-night", "amber-graphite",
}

// themeLabels are human-friendly names for the picker.
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

// Label returns a human-friendly name for a theme (falls back to the raw name).
func Label(name string) string {
	if l, ok := themeLabels[name]; ok {
		return l
	}
	return name
}

// Swatch returns a color preview block using full Unicode block characters.
// fbterm renders these perfectly with TrueType fonts.
func Swatch(c lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(c).Render("██████")
}

// AccentColor returns the accent color of a named palette (for preview swatches),
// falling back to the default theme's accent.
func AccentColor(name string) lipgloss.Color {
	if p, ok := palettes[name]; ok {
		return p.Accent
	}
	return palettes[defaultPalette].Accent
}

// Theme bundles a palette with precomputed Lip Gloss styles.
type Theme struct {
	P Palette

	App        lipgloss.Style
	Sidebar    lipgloss.Style
	MenuItem   lipgloss.Style
	MenuActive lipgloss.Style
	Title      lipgloss.Style
	Card       lipgloss.Style
	CardTitle  lipgloss.Style
	Key        lipgloss.Style
	Val        lipgloss.Style
	Muted      lipgloss.Style
	Help       lipgloss.Style
	Accent     lipgloss.Style
}

// New builds a Theme for a named palette, falling back to the default palette.
// Uses RoundedBorder (Unicode) because the panel runs under fbterm which
// renders TrueType fonts — all Unicode characters display perfectly.
func New(name string) *Theme {
	p, ok := palettes[name]
	if !ok {
		p = palettes[defaultPalette]
	}

	// fbterm's extended colour protocol differs from xterm's. Restricting the
	// panel to its portable ANSI colours prevents a black screen while the
	// framebuffer palette supplies the intended soft graphite and teal shades.
	lipgloss.SetColorProfile(termenv.ANSI)

	t := &Theme{P: p}
	t.App = lipgloss.NewStyle().Background(p.Bg).Foreground(p.Text)
	t.Sidebar = lipgloss.NewStyle().Background(p.Surface).Foreground(p.Text).
		Padding(1, 2).Border(lipgloss.RoundedBorder(), false, true, false, false).
		BorderForeground(p.Border).BorderBackground(p.Bg)
	t.MenuItem = lipgloss.NewStyle().Foreground(p.Text).Padding(0, 1)
	t.MenuActive = lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Padding(0, 1)
	t.Title = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)

	// Clean rounded borders — connects 100% seamlessly on all framebuffers
	t.Card = lipgloss.NewStyle().Background(p.Surface).Foreground(p.Text).
		Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).
		BorderBackground(p.Bg).Padding(0, 1)
	t.CardTitle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	t.Key = lipgloss.NewStyle().Foreground(p.Muted)
	t.Val = lipgloss.NewStyle().Foreground(p.Text)
	t.Muted = lipgloss.NewStyle().Foreground(p.Muted)
	t.Help = lipgloss.NewStyle().Foreground(p.Muted).Background(p.Bg)
	t.Accent = lipgloss.NewStyle().Foreground(p.Accent)
	return t
}

// Badge renders a compact text-only status marker. The terminal remains calm:
// colour communicates state without putting a block behind the text.
func (t *Theme) Badge(label string, fg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(label)
}
