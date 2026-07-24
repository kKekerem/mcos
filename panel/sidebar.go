package panel

import (
	"strings"

	"mcos/internal/model"
)

// Global sidebar sections.
const (
	secDashboard = iota
	secServers
	secSoftware
	secPerformance
	secDevices
	secNetwork
	secWAN
	secPeers
	secSettings
	secCount
)

var sectionNames = []string{
	"Sistem Durumu",
	"Sunucular",
	"Yazılım",
	"Performans",
	"Donanım",
	"Ağ",
	"Tünel (Serveo)",
	"MCOS Paylaşım",
	"Ayarlar",
}

// Precise 1-column section icons for 100% border alignment
var sectionIcons = []string{
	"◆", "▶", "◈", "▲", "●", "◈", "◇", "★", "⚙",
}

// sidebarDisabled reports whether a section is adaptively disabled given the
// current system status (no internet -> wan).
func (a *App) sidebarDisabled(section int) bool {
	if a.status == nil {
		return false
	}
	switch section {
	case secWAN:
		return !a.status.WANOn
	default:
		return false
	}
}

// renderSidebar draws the left navigation column as raw content; the focus
// border is added by pane() in View(). The active section remains visible in
// both panes, with a complete rounded pill when this navigation has focus.
func (a *App) renderSidebar(w, h int) string {
	th := a.th
	var b strings.Builder

	b.WriteString(th.Title.Render("◆ MCOS") + th.Muted.Render(" v"+appVersion(a.status)) + "\n")
	b.WriteString(th.Muted.Render("Minecraft Server OS") + "\n")
	b.WriteString(th.Muted.Render("Klavye Kontrollü TUI") + "\n\n")

	for i, name := range sectionNames {
		disabled := a.sidebarDisabled(i)
		icon := sectionIcons[i]
		b.WriteString(RenderNavItem(th, icon, name, i == a.section, a.focus == focusSidebar, disabled) + "\n")
	}

	// Footer: quick system badges.
	b.WriteString("\n")
	if a.status != nil {
		b.WriteString(th.Muted.Render("Tier  ") + a.tierBadge() + "\n")
		b.WriteString(th.Muted.Render("Sunucu  ") +
			th.Val.Render(itoa(a.status.ServersUp)+"/"+itoa(a.status.ServersTotal)) + "\n")
	}
	return b.String()
}

func (a *App) tierBadge() string {
	if a.status == nil {
		return a.th.Badge("?", a.th.P.Muted)
	}
	switch a.status.Tier {
	case model.TierHigh:
		return a.th.Badge("HIGH", a.th.P.Green)
	case model.TierMedium:
		return a.th.Badge("MED", a.th.P.Yellow)
	default:
		return a.th.Badge("LOW", a.th.P.Muted)
	}
}

func appVersion(st *model.SystemStatus) string {
	if st != nil && st.Version != "" {
		return st.Version
	}
	return "0.1.0"
}
