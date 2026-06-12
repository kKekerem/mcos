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
	"PC Eşleştirme",
	"Ayarlar",
}

// Decorative per-section icons were removed: the framebuffer console font can't
// render arbitrary symbol glyphs, so they showed as garbage. Names are enough.

// sidebarDisabled reports whether a section is adaptively disabled given the
// current system status (no internet -> wan; cluster off -> peers).
func (a *App) sidebarDisabled(section int) bool {
	if a.status == nil {
		return false
	}
	switch section {
	case secWAN:
		return !a.status.WANOn
	case secPeers:
		return !a.status.ClusterOn || a.status.PeersOnline == 0 && !a.status.ClusterOn
	default:
		return false
	}
}

// renderSidebar draws the left navigation column as raw content; the focus
// border is added by pane() in View(). The active section is a filled accent
// bar when the sidebar has focus, or an accent "» name" when focus is in the
// content pane — so it's always obvious where you are.
func (a *App) renderSidebar(w, h int) string {
	th := a.th
	var b strings.Builder

	b.WriteString(th.Title.Render("MCOS") + th.Muted.Render(" v"+appVersion(a.status)) + "\n")
	b.WriteString(th.Muted.Render("Minecraft Server OS") + "\n\n")

	for i, name := range sectionNames {
		disabled := a.sidebarDisabled(i)
		switch {
		case i == a.section && a.focus == focusSidebar:
			// Active + focused: accent side-bar + filled label — unmissable.
			b.WriteString(th.Accent.Render("▌") + th.MenuActive.Render(" "+name+" ") + "\n")
		case i == a.section:
			// Active but focus is in the content pane: accent ▶ marker.
			b.WriteString(th.Accent.Render("▶ "+name) + "\n")
		case disabled:
			b.WriteString(th.MenuItem.Faint(true).Render("  "+name) + th.Muted.Render(" ·") + "\n")
		default:
			b.WriteString(th.MenuItem.Render("  "+name) + "\n")
		}
	}

	// Footer: quick system badges.
	b.WriteString("\n")
	if a.status != nil {
		b.WriteString(th.Muted.Render("Tier: ") + a.tierBadge() + "\n")
		b.WriteString(th.Muted.Render("Sunucu: ") +
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
