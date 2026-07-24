package panel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"mcos/internal/model"
	"mcos/panel/theme"
)

// UI Component Library for MCOS TUI
// The panel runs under fbterm which renders TrueType fonts on the framebuffer.
// All Unicode characters (rounded borders, emojis, box-drawing) display perfectly.

// RenderCard wraps content inside a sleek container with rounded borders.
func RenderCard(th *theme.Theme, title string, body string, width int, focused bool) string {
	if width < 30 {
		width = 30
	}
	frameW := width - 2 // Border is outside the style width; padding is inside.
	if frameW < 28 {
		frameW = 28
	}

	borderColor := th.P.Border
	if focused {
		borderColor = th.P.Accent
	}

	boxStyle := lipgloss.NewStyle().
		Background(th.P.Bg).
		Foreground(th.P.Text).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderBackground(th.P.Bg).
		Padding(1, 2).
		Width(frameW)

	header := th.Title.Bold(true).Render(title)
	cardContent := header + "\n\n" + body
	return boxStyle.Render(cardContent)
}

// RenderHeader renders the top step bar for OOBE and setup wizards.
func RenderHeader(th *theme.Theme, currentStep int, totalSteps int, title string) string {
	pct := (currentStep * 100) / totalSteps
	if pct > 100 {
		pct = 100
	}

	barWidth := 25
	filled := (pct * barWidth) / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	barStr := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	stepBadge := th.Badge(fmt.Sprintf(" Adım %d / %d ", currentStep, totalSteps), th.P.Accent)
	titleStr := th.Title.Bold(true).Render(title)
	progressStr := th.Muted.Render(fmt.Sprintf("[%s] %d%%", barStr, pct))

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Center, stepBadge, "  ", titleStr),
		progressStr,
	)
}

// RenderButton renders a keyboard action with a clear focus marker.
func RenderButton(th *theme.Theme, label string, shortcut string, focused bool) string {
	color := th.P.Text
	prefix := "  "
	if focused {
		color = th.P.Accent
		prefix = "➜ "
	}
	btnText := fmt.Sprintf("%s%s  %s", prefix, label, shortcut)
	return lipgloss.NewStyle().Foreground(color).Bold(focused).Render(btnText)
}

// RenderOptionRow renders an interactive option selection item.
func RenderOptionRow(th *theme.Theme, label string, note string, selected bool) string {
	if selected {
		row := renderPill(th.P.Accent, th.P.Bg, "➜ "+label, true)
		if note != "" {
			row += "  " + th.Muted.Render(note)
		}
		return row
	}
	rowText := th.MenuItem.Render("  ◯  " + label)
	if note != "" {
		rowText += "  " + th.Muted.Render(note)
	}
	return rowText
}

// RenderNavItem is the sidebar counterpart of RenderOptionRow. The active
// entry is a complete rounded pill, so focus never depends on color alone.
func RenderNavItem(th *theme.Theme, icon string, label string, selected bool, focused bool, disabled bool) string {
	text := icon + " " + label
	if disabled {
		return th.MenuItem.Faint(true).Render("  "+text) + th.Muted.Render("  pasif")
	}
	if selected {
		if focused {
			return renderPill(th.P.Accent, th.P.Bg, "➜ "+text, true)
		}
		return th.Accent.Bold(true).Render("  ➜ " + text)
	}
	return th.MenuItem.Render("    " + text)
}

// RenderInputField renders a form input row with focused status.
func RenderInputField(th *theme.Theme, label string, inputView string, help string, focused bool) string {
	lblStyle := th.Key
	if focused {
		lblStyle = th.Accent.Bold(true)
	}

	lbl := lblStyle.Render(fmt.Sprintf("%-18s", label))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.P.Border).
		Padding(0, 1).
		Render(inputView)

	if focused {
		box = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(th.P.Accent).
			Padding(0, 1).
			Render(inputView)
	}

	res := fmt.Sprintf("%s : %s", lbl, box)
	if help != "" && focused {
		res += "\n" + th.Muted.Render("                     "+help)
	}
	return res
}

// RenderProgressBar renders a visual progress bar with sleek block characters.
func RenderProgressBar(th *theme.Theme, pct int, width int) string {
	if width < 10 {
		width = 20
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct * width) / 100
	empty := width - filled

	fillStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("─", empty)

	return fmt.Sprintf("▐%s%s▌ %3d%%", th.Accent.Bold(true).Render(fillStr), th.Muted.Render(emptyStr), pct)
}

// KeyCap renders a single keyboard key as a compact visual affordance.
func KeyCap(th *theme.Theme, key string) string {
	return renderPill(th.P.Accent, th.P.Bg, key, true)
}

// KeyHint pairs a key with a short action label for the bottom help bar.
type KeyHint struct {
	Key   string
	Label string
}

// RenderKeyHints renders keyboard-only controls in a way that looks actionable
// without implying mouse support.
func RenderKeyHints(th *theme.Theme, hints []KeyHint, maxWidth int) string {
	parts := make([]string, 0, len(hints))
	sep := th.Muted.Render("  ")
	for _, h := range hints {
		if h.Key == "" || h.Label == "" {
			continue
		}
		part := KeyCap(th, h.Key) + " " + th.Help.Render(h.Label)
		candidate := part
		if len(parts) > 0 {
			candidate = strings.Join(append(append([]string{}, parts...), part), sep)
		}
		if maxWidth > 0 && lipgloss.Width(candidate) > maxWidth {
			break
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, sep)
}

// renderPill preserves the familiar compact controls without filling their
// background. The selected state uses a bright marker and foreground colour.
func renderPill(bg, fg lipgloss.Color, label string, bold bool) string {
	color := fg
	if fg == "0" {
		color = bg
	}
	return lipgloss.NewStyle().Foreground(color).Bold(bold).Render(label)
}

// Dashboard helper primitives
func kv(t *theme.Theme, key, val string) string {
	return t.Key.Render(fmt.Sprintf("%-14s", key)) + " " + t.Val.Render(val)
}

func bar(t *theme.Theme, pct float64, width int) string {
	if width < 6 {
		width = 10
	}
	pInt := int(pct)
	if pInt < 0 {
		pInt = 0
	}
	if pInt > 100 {
		pInt = 100
	}
	filled := (pInt * width) / 100
	empty := width - filled

	fillStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("─", empty)

	return fmt.Sprintf("▐%s%s▌ %3.0f%%", t.Accent.Bold(true).Render(fillStr), t.Muted.Render(emptyStr), pct)
}

func card(t *theme.Theme, title, body string, w int) string {
	frameW := w - 2 // border is outside the style width; padding is inside.
	if frameW < 10 {
		frameW = 10
	}
	return t.Card.Width(frameW).Render(t.CardTitle.Render(title) + "\n\n" + body)
}

func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func fmtUptime(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	s := sec % 60

	if d > 0 {
		return fmt.Sprintf("%dg %dh", d, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func joinH(gap int, left, right string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

func stateBadge(t *theme.Theme, st model.ServerState) string {
	switch st {
	case model.StateRunning:
		return t.Badge("● ÇALIŞIYOR", t.P.Green)
	case model.StateStarting:
		return t.Badge("● BAŞLIYOR", t.P.Yellow)
	case model.StateStopping:
		return t.Badge("● DURUYOR", t.P.Yellow)
	case model.StateError:
		return t.Badge("● HATA", t.P.Red)
	default:
		return t.Badge("○ KAPALI", t.P.Muted)
	}
}

func boolBadge(t *theme.Theme, val bool, labels ...string) string {
	onLabel := "EVET"
	offLabel := "HAYIR"
	if len(labels) > 0 && labels[0] != "" {
		onLabel = labels[0]
	}
	if len(labels) > 1 && labels[1] != "" {
		offLabel = labels[1]
	}
	if val {
		return t.Badge("✓ "+onLabel, t.P.Green)
	}
	return t.Badge("– "+offLabel, t.P.Muted)
}
