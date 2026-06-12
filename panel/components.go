package panel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mcos/internal/model"
	"mcos/panel/theme"
)

// kv renders a "label: value" line with themed colors.
func kv(th *theme.Theme, label, value string) string {
	return th.Key.Render(fmt.Sprintf("%-14s", label)) + " " + th.Val.Render(value)
}

// card renders a titled, bordered card with body content at a target width.
func card(th *theme.Theme, title, body string, width int) string {
	inner := width - 4
	if inner < 8 {
		inner = 8
	}
	head := th.CardTitle.Render(title)
	content := head + "\n" + body
	return th.Card.Width(inner).Render(content)
}

// stateBadge returns a colored pill for a server state.
func stateBadge(th *theme.Theme, s model.ServerState) string {
	switch s {
	case model.StateRunning:
		return th.Badge("ÇALIŞIYOR", th.P.Green)
	case model.StateStarting:
		return th.Badge("BAŞLIYOR", th.P.Yellow)
	case model.StateStopping:
		return th.Badge("DURUYOR", th.P.Yellow)
	case model.StateError:
		return th.Badge("HATA", th.P.Red)
	default:
		return th.Badge("DURDU", th.P.Muted)
	}
}

// boolBadge renders an on/off pill.
func boolBadge(th *theme.Theme, on bool, onText, offText string) string {
	if on {
		return th.Badge(onText, th.P.Green)
	}
	return th.Badge(offText, th.P.Muted)
}

// fmtBytes renders a byte count as a human-readable string.
func fmtBytes(b uint64) string {
	const u = 1024
	if b < u {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// fmtUptime renders seconds as e.g. "1g 2sa 3dk" (gün/saat/dakika).
func fmtUptime(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dg %dsa %ddk", d, h, m)
	case h > 0:
		return fmt.Sprintf("%dsa %ddk", h, m)
	default:
		return fmt.Sprintf("%ddk", m)
	}
}

// bar renders a compact usage bar like [████░░░░] 42%.
func bar(th *theme.Theme, pct float64, width int) string {
	if width < 4 {
		width = 4
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	color := th.P.Green
	switch {
	case pct >= 90:
		color = th.P.Red
	case pct >= 70:
		color = th.P.Yellow
	}
	fill := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled))
	empty := th.Muted.Render(strings.Repeat("░", width-filled))
	return fmt.Sprintf("%s%s %3.0f%%", fill, empty, pct)
}

// joinH lays panels side by side with a gap.
func joinH(gap int, panels ...string) string {
	sep := strings.Repeat(" ", gap)
	withSep := make([]string, 0, len(panels)*2)
	for i, p := range panels {
		if i > 0 {
			withSep = append(withSep, sep)
		}
		withSep = append(withSep, p)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, withSep...)
}
