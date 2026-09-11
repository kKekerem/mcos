package panel

import (
	"strings"

	"mcos/internal/model"
	"mcos/panel/theme"
)

// Global sidebar sections.
const (
	secDashboard = iota
	secServers
	secUSB // yalnızca USB takılıyken görünür
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
	"USB Bellek",
	"Yazılım",
	"Performans",
	"Donanım",
	"Ağ",
	"Tünel (Serveo)",
	"MCOS Paylaşım",
	"Ayarlar",
}

// sectionIcons — hepsi TEK kolon genişliğinde (bkz. panel/theme/tokens.go).
// sectionNames ile birebir aynı sırada olmalı.
var sectionIcons = []string{
	theme.IconInfo,     // Sistem Durumu
	theme.IconServer,   // Sunucular
	theme.IconUSB,      // USB Bellek
	theme.IconSoftware, // Yazılım
	theme.IconPerf,     // Performans
	theme.IconDisk,     // Donanım
	theme.IconNetwork,  // Ağ
	theme.IconTunnel,   // Tünel
	theme.IconTurbo,    // MCOS Paylaşım
	theme.IconSettings, // Ayarlar
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

// sidebarHidden reports whether a section is not applicable right now and must
// be left out of the menu entirely (not just greyed out).
//
// USB bölümü yalnızca gerçekten bir USB bellek takılıyken listelenir: boş bir
// "USB" satırı kullanıcıyı yanıltır. Tespit ucuzdur ve hiçbir şeyi bağlamaz
// (bkz. internal/files/usbdetect_linux.go).
func (a *App) sidebarHidden(section int) bool {
	if section != secUSB {
		return false
	}
	return a.status == nil || !a.status.USB.Present
}

// visibleSections returns the section indices currently shown, in order.
// Gezinme bu listede yapılır, böylece gizli bir bölüme imleç hiç gitmez.
func (a *App) visibleSections() []int {
	out := make([]int, 0, secCount)
	for i := 0; i < secCount; i++ {
		if !a.sidebarHidden(i) {
			out = append(out, i)
		}
	}
	return out
}

// moveSection advances the sidebar cursor by delta over VISIBLE sections only.
func (a *App) moveSection(delta int) {
	vis := a.visibleSections()
	if len(vis) == 0 {
		return
	}
	cur := 0
	for i, s := range vis {
		if s == a.section {
			cur = i
			break
		}
	}
	cur = (cur + delta + len(vis)) % len(vis)
	a.section = vis[cur]
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

	for _, i := range a.visibleSections() {
		b.WriteString(RenderNavItem(th, sectionIcons[i], sectionNames[i], w,
			i == a.section, a.sidebarDisabled(i)) + "\n")
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
