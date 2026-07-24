package panel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderServers draws the server cards list with the selected card highlighted.
func (a *App) renderServers(w, h int) string {
	th := a.th
	header := th.Title.Render("🎮 Sunucular")
	newBtn := RenderButton(th, "Yeni Sunucu", "n", true)

	if len(a.servers) == 0 {
		return header + "\n\n" + newBtn + "\n\n" + a.emptyServers(w)
	}

	// Window the cards around the cursor so the selection is always visible and
	// the list never overflows the pane.
	const cardH = 6
	avail := h - 4
	if avail < cardH {
		avail = cardH
	}
	per := avail / cardH
	if per < 1 {
		per = 1
	}
	start := a.serverCursor - per/2
	if start > len(a.servers)-per {
		start = len(a.servers) - per
	}
	if start < 0 {
		start = 0
	}
	end := start + per
	if end > len(a.servers) {
		end = len(a.servers)
	}
	cards := make([]string, 0, per)
	for i := start; i < end; i++ {
		cards = append(cards, a.serverCard(a.servers[i], i == a.serverCursor, a.focus == focusContent, w))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, cards...)
	if len(a.servers) > per {
		body += "\n" + th.Muted.Render(fmt.Sprintf("  %d-%d / %d sunucu (yukarı/aşağı)", start+1, end, len(a.servers)))
	}
	return header + "\n\n" + newBtn + "\n\n" + body
}

func (a *App) emptyServers(w int) string {
	th := a.th
	boxW := 56
	if boxW > w {
		boxW = w
	}
	box := RenderCard(th, "🎮 Henüz sunucu yok", strings.Join([]string{
		th.Val.Render("İlk Minecraft sunucunuzu oluşturmak için"),
		th.Val.Render("yeni sunucu sihirbazını başlatın."),
		"",
		RenderKeyHints(th, []KeyHint{{"n", "yeni sunucu"}, {"Esc", "menü"}}, boxW-6),
	}, "\n"), boxW, true)
	return box
}

// serverCard renders one server as a card. The selected row is unmistakable:
// a "▶" prefix on the name, a "● SEÇİLİ" badge, and an accent border (brightest
// when the content pane is focused); every other card is dimmed so the active
// one pops even on a flat console.
func (a *App) serverCard(s *serverInfo, selected, focused bool, w int) string {
	th := a.th
	frameW := w - 2
	contentW := w - 4
	if frameW < 22 {
		frameW = 22
	}
	if contentW < 18 {
		contentW = 18
	}

	name := s.Name
	if selected {
		name = "➜ " + s.Name
	}
	title := th.Val.Bold(true).Render(name)
	line1 := title + "  " + stateBadge(th, s.State)
	if selected {
		line1 += "  " + th.Badge(" SEÇİLİ ", th.P.Accent)
	}
	if s.WAN.Enabled {
		line1 += "  " + th.Badge(" WAN ", th.P.Blue)
	}

	line2 := th.Key.Render(fmt.Sprintf("%s · MC %s · Java %d", s.Software, s.MCVersion, s.JavaMajor))
	line3 := th.Key.Render(fmt.Sprintf("RAM %d MB · Port %d · Oyuncu %d/%d · %s",
		s.RAMMB, s.Port, s.Players, orZero(s.MaxPlayers, 20), fmtUptime(s.UptimeSec)))

	lastLog := s.LastLog
	if lastLog == "" {
		lastLog = "Log henüz yok"
	}
	line4 := th.Muted.Render("  📋 " + truncate(lastLog, contentW-4))

	bodyText := strings.Join([]string{line1, line2, line3, line4}, "\n")

	style := th.Card.Width(frameW)
	switch {
	case selected && focused:
		style = style.BorderForeground(th.P.Accent).Bold(true)
	case selected:
		style = style.BorderForeground(th.P.Accent)
	default:
		style = style.BorderForeground(th.P.Border).Faint(true)
	}
	return style.Render(bodyText)
}

func orZero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
