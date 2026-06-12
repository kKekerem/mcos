// Package theme defines the MCOS panel color palette and reusable Lip Gloss
// styles. The default is a near-black background (#0F0B0A) with a light-purple
// accent; status colors are green/yellow/red. Themes are addressable by name so
// the global config's "theme" field can switch palettes.
package theme

import (
	"os"
	"strings"

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
var palettes = map[string]Palette{
	"noir-purple": {
		Bg: "#0F0B0A", Surface: "#1A1417", SurfaceA: "#241B29",
		Border: "#3A2E42", Accent: "#C8A6FF", Dim: "#8B6FB8",
		Text: "#ECE6EC", Muted: "#8A7F88",
		Green: "#5DD39E", Yellow: "#FFC857", Red: "#FF6B6B", Blue: "#7AA2F7",
	},
	"anthracite-orange": {
		Bg: "#121212", Surface: "#1C1C1C", SurfaceA: "#2A2320",
		Border: "#3A3330", Accent: "#FF8C42", Dim: "#A85F2E",
		Text: "#ECE6E0", Muted: "#8A8078",
		Green: "#5DD39E", Yellow: "#FFC857", Red: "#FF6B6B", Blue: "#7AA2F7",
	},
	"anthracite-green": {
		Bg: "#0E1110", Surface: "#171C1A", SurfaceA: "#1F2A24",
		Border: "#2E3A33", Accent: "#5DD39E", Dim: "#3E8C6A",
		Text: "#E6ECE8", Muted: "#7F8A84",
		Green: "#5DD39E", Yellow: "#FFC857", Red: "#FF6B6B", Blue: "#7AA2F7",
	},
	"crimson-night": {
		Bg: "#100B0C", Surface: "#1A1416", SurfaceA: "#2A1B1F",
		Border: "#3F2A30", Accent: "#FF5C72", Dim: "#A8404E",
		Text: "#ECE0E2", Muted: "#8A7B7E",
		Green: "#5DD39E", Yellow: "#FFC857", Red: "#FF6B6B", Blue: "#7AA2F7",
	},
	"amber-graphite": {
		Bg: "#0F0F0E", Surface: "#19191A", SurfaceA: "#262420",
		Border: "#39352E", Accent: "#FFCB47", Dim: "#B8922E",
		Text: "#ECEAE2", Muted: "#8A877E",
		Green: "#5DD39E", Yellow: "#FFC857", Red: "#FF6B6B", Blue: "#7AA2F7",
	},
}

// themeOrder is the selectable palette order shown in the first-boot wizard's
// theme step. All are dark / anthracite with warm (purple/orange/green/crimson/
// amber) accents — deliberately not blue-heavy.
var themeOrder = []string{
	"noir-purple", "anthracite-orange", "anthracite-green", "crimson-night", "amber-graphite",
}

// themeLabels are human-friendly names for the picker.
var themeLabels = map[string]string{
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

// Swatch returns a robust color preview block.
func Swatch(c lipgloss.Color) string {
	// Restore the smooth solid block symbol.
	return lipgloss.NewStyle().Foreground(c).Render("██████")
}

// AccentColor returns the accent color of a named palette (for preview swatches),
// falling back to the default theme's accent.
func AccentColor(name string) lipgloss.Color {
	if p, ok := palettes[name]; ok {
		return p.Accent
	}
	return palettes["noir-purple"].Accent
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

// New builds a Theme for a named palette, falling back to "noir-purple".
func New(name string) *Theme {
	p, ok := palettes[name]
	if !ok {
		p = palettes["noir-purple"]
	}

	// Force TrueColor for the full experience. fbterm supports this on TTY.
	lipgloss.SetColorProfile(termenv.TrueColor)

	t := &Theme{P: p}
	t.App = lipgloss.NewStyle().Background(p.Bg).Foreground(p.Text)
	t.Sidebar = lipgloss.NewStyle().Background(p.Surface).Foreground(p.Text).
		Padding(1, 2).Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(p.Border).BorderBackground(p.Bg)
	// Unselected menu items use full-strength text (not muted) so every option
	// is readable even on a flat 16-colour console — the selected one then pops
	// via the accent bar/marker rather than relying on a muted/bright contrast.
	t.MenuItem = lipgloss.NewStyle().Foreground(p.Text).Padding(0, 1)
	t.MenuActive = lipgloss.NewStyle().Foreground(p.Bg).Background(p.Accent).
		Bold(true).Padding(0, 1)
	t.Title = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)

	// Use rounded borders only when we have a modern terminal emulator (fbterm/ssh).
	// On raw Linux console, stick to ASCIIBorder which uses + - | that never fails.
	border := lipgloss.NormalBorder()
	term := os.Getenv("TERM")
	if term == "fbterm" || strings.Contains(term, "xterm") || term == "screen" {
		border = lipgloss.RoundedBorder()
	} else if term == "linux" {
		border = lipgloss.ASCIIBorder()
	}

	t.Card = lipgloss.NewStyle().Background(p.Surface).Foreground(p.Text).
		Border(border).BorderForeground(p.Border).
		BorderBackground(p.Bg).Padding(0, 1)
	t.CardTitle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	t.Key = lipgloss.NewStyle().Foreground(p.Muted)
	t.Val = lipgloss.NewStyle().Foreground(p.Text)
	t.Muted = lipgloss.NewStyle().Foreground(p.Muted)
	t.Help = lipgloss.NewStyle().Foreground(p.Muted).Background(p.Bg)
	t.Accent = lipgloss.NewStyle().Foreground(p.Accent)
	return t
}

// Badge renders a small status pill in the given color.
func (t *Theme) Badge(label string, fg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(t.P.Bg).Background(fg).Bold(true).
		Padding(0, 1).Render(label)
}
