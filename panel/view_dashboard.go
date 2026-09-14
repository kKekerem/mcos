package panel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mcos/panel/theme"
)

// renderDashboard draws the main system control center.
func (a *App) renderDashboard(w, h int) string {
	th := a.th
	if a.status == nil {
		return a.contentFrame(w, h, "Sistem Durumu", th.Muted.Render("durum alınıyor…"))
	}
	st := a.status
	// Two cards per row with a 2-space gap. card() renders at exactly its target
	// width (inner+border+padding), so 2*colW+gap must stay <= w or the row wraps
	// and the whole view "shifts" — the bug we are fixing.
	colW := (w - 2) / 2
	if colW < 20 {
		colW = 20
	}

	// System card.
	sysBody := strings.Join([]string{
		kv(th, "Ad", st.SystemName),
		kv(th, "Sürüm", st.Version),
		kv(th, "Çalışma", fmtUptime(st.Uptime)),
		kv(th, "Tier", string(st.Tier)),
	}, "\n")

	// CPU card.
	cpuBody := strings.Join([]string{
		kv(th, "Model", truncate(st.CPU.Model, colW-18)),
		kv(th, "Çekirdek", fmt.Sprintf("%d çekirdek / %d iş parçacığı", st.CPU.Cores, st.CPU.Threads)),
		kv(th, "Kullanım", bar(th, st.CPU.UsagePct, 16)),
	}, "\n")

	// Memory card.
	memBody := strings.Join([]string{
		kv(th, "Toplam", fmtBytes(st.Memory.TotalBytes)),
		kv(th, "Kullanılabilir", fmtBytes(st.Memory.AvailableBytes)),
		kv(th, "Kullanım", bar(th, st.Memory.UsagePct, 16)),
	}, "\n")

	// Disk card.
	diskLines := []string{}
	if len(st.Disks) == 0 {
		diskLines = append(diskLines, th.Muted.Render("disk bilgisi yok"))
	}
	for _, d := range st.Disks {
		diskLines = append(diskLines, kv(th, d.Mount, bar(th, d.UsagePct, 14)))
		diskLines = append(diskLines, th.Muted.Render(fmt.Sprintf("   %s / %s boş",
			fmtBytes(d.FreeBytes), fmtBytes(d.TotalBytes))))
	}
	diskBody := strings.Join(diskLines, "\n")

	// Network card.
	netBody := strings.Join([]string{
		kv(th, "Yerel IP", orDash(st.Net.LocalIP)),
		kv(th, "İnternet", onlineBadge(th, st.Net.Internet)),
		kv(th, "Arayüz", fmt.Sprintf("%d NIC", len(st.Net.NICs))),
	}, "\n")

	// GPU card (adaptive).
	var gpuBody string
	if a.status.GPUMonitorOn && len(st.GPUs) > 0 {
		g := st.GPUs[0]
		gpuBody = strings.Join([]string{
			kv(th, "Üretici", orDash(g.Vendor)),
			kv(th, "Model", orDash(g.Model)),
			kv(th, "Kullanım", bar(th, g.UsagePct, 14)),
		}, "\n")
	} else {
		gpuBody = th.Muted.Render("GPU yok veya izleme pasif")
	}

	// Services card.
	turbo := "kapalı"
	if st.TurboOn {
		turbo = theme.IconTurbo + " AÇIK"
	}
	svcBody := strings.Join([]string{
		kv(th, "Sunucular", fmt.Sprintf("%d çalışıyor / %d toplam", st.ServersUp, st.ServersTotal)),
		kv(th, "Java", javaList(st.JavaVersions)),
		kv(th, "Tünel (playit)", st.WAN),
		kv(th, "Turbo", turbo),
		kv(th, "Eşleşmiş PC", itoa(st.PeersOnline)),
		kv(th, "Aktif görev", itoa(st.ActiveTasks)),
	}, "\n")

	ready := th.Badge("SİSTEM HAZIR", th.P.Green)
	if !st.Net.Internet {
		ready = th.Badge("ÇEVRİMDIŞI", th.P.Yellow)
	}
	overview := RenderCard(th, "◆ "+st.SystemName+"  "+ready,
		th.Muted.Render("Minecraft sunucularınızın kontrol merkezi"), w, true)

	rows := []string{overview}
	if w < 68 {
		rows = append(rows,
			card(th, "◆ Sistem", sysBody, w),
			card(th, "▶ İşlemci", cpuBody, w),
			card(th, "◈ Bellek", memBody, w),
			card(th, "▲ Disk", diskBody, w),
			card(th, "● Ağ", netBody, w),
			card(th, "◇ GPU", gpuBody, w),
			card(th, "★ Servisler & Görevler", svcBody, w),
		)
	} else {
		rows = append(rows,
			joinH(2, card(th, "◆ Sistem", sysBody, colW), card(th, "▶ İşlemci", cpuBody, colW)),
			joinH(2, card(th, "◈ Bellek", memBody, colW), card(th, "▲ Disk", diskBody, colW)),
			joinH(2, card(th, "● Ağ", netBody, colW), card(th, "◇ GPU", gpuBody, colW)),
			card(th, "★ Servisler & Görevler", svcBody, w),
		)
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, rows...)
	// Route through contentFrame so the grid scrolls (↑↓) and is clamped to the
	// pane — no self-imposed Width/Height/Padding that double-pads inside pane()
	// and pushes the layout sideways.
	return a.contentFrame(w, h, "Sistem Durumu", inner)
}

func onlineBadge(t *theme.Theme, on bool) string {
	if on {
		return t.Badge("✓ BAĞLI", t.P.Green)
	}
	return t.Badge("⚠ YOK", t.P.Red)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func javaList(majors []int) string {
	if len(majors) == 0 {
		return "kurulu değil"
	}
	parts := make([]string, len(majors))
	for i, m := range majors {
		parts[i] = itoa(m)
	}
	return strings.Join(parts, ", ")
}
