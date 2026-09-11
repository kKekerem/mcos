package panel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mcos/internal/model"
	"mcos/panel/theme"
)

func (a *App) renderSoftware(w, h int) string {
	th := a.th
	var b strings.Builder

	b.WriteString(th.CardTitle.Render("◈ Java Yönetim ve İndirme Merkezi") + "\n")
	b.WriteString(th.Muted.Render("Yön tuşları (↑/↓) ile Java sürümünü seçip Enter'a basarak indirebilirsiniz.") + "\n\n")

	installedMap := make(map[int]model.JavaRuntime)
	for _, rt := range a.java {
		installedMap[rt.Major] = rt
	}

	targets := []struct {
		major int
		badge string
		name  string
		desc  string
	}{
		{17, "LTS ÖNERİLEN", "Java 17 (Temurin JDK)", "Paper/Spigot 1.17 - 1.20.4 için standart"},
		{21, "LTS EN YENİ", "Java 21 (Temurin JDK)", "Minecraft 1.20.5+ ve 1.21+ sunucuları için"},
		{11, "LTS ESKİ", "Java 11 (Temurin JDK)", "Minecraft 1.12 - 1.16 arası için"},
		{8, "LEGACY", "Java 8 (Temurin JDK)", "Legacy Minecraft 1.8 - 1.12 için"},
	}

	for idx, t := range targets {
		sel := (a.focus == focusContent && a.rowCursor == idx)
		prefix := "  "
		if sel {
			prefix = theme.IconCursor + " "
		}

		titleStyle := th.Val.Render(t.name)
		if sel {
			titleStyle = th.Accent.Bold(true).Render(t.name)
		}

		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			th.Accent.Render(prefix),
			titleStyle,
			th.Muted.Render(t.badge)))

		prog, downloading := a.javaProgress[t.major]

		if downloading && !prog.Done {
			pct := prog.Percent
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			barW := 24
			filled := (pct * barW) / 100
			empty := barW - filled
			fillStr := strings.Repeat("█", filled)
			emptyStr := strings.Repeat("─", empty)
			pStr := fmt.Sprintf("      %s [ %s%s ] %3d%%  %s", theme.IconWait, th.Accent.Bold(true).Render(fillStr), th.Muted.Render(emptyStr), pct, prog.Status)
			b.WriteString(pStr + "\n")
		} else if rt, isInst := installedMap[t.major]; isInst {
			verShort := truncate(rt.Version, w-35)
			statusStr := fmt.Sprintf("      ✓ KURULU (%s) — Path: %s", verShort, rt.JavaBin)
			b.WriteString(lipgloss.NewStyle().Foreground(th.P.Green).Render(statusStr) + "\n")
		} else {
			statusStr := fmt.Sprintf("      ▶ %s — [Enter] İndir", t.desc)
			if sel {
				b.WriteString(th.Val.Bold(true).Render(statusStr) + "\n")
			} else {
				b.WriteString(th.Muted.Render(statusStr) + "\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString(th.CardTitle.Render("▲ Desteklenen Sunucu Türleri") + "\n")
	var types []string
	for _, s := range model.AllSoftware {
		types = append(types, string(s))
	}
	b.WriteString(th.Muted.Render("  "+strings.Join(types, " · ")) + "\n")

	return a.contentFrame(w, h, "Yazılım (Java)", b.String())
}

func (a *App) renderPerformance(w, h int) string {
	th := a.th
	if a.status == nil {
		return a.contentFrame(w, h, "Performans", th.Muted.Render("veri alınıyor…"))
	}
	st := a.status
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("▲ Sistem Performansı") + "\n")
	b.WriteString(kv(th, "CPU Usage", bar(th, st.CPU.UsagePct, 24)) + "\n")
	b.WriteString(kv(th, "Bellek", bar(th, st.Memory.UsagePct, 24)) + "\n")
	for _, d := range st.Disks {
		b.WriteString(kv(th, "Disk "+d.Mount, bar(th, d.UsagePct, 24)) + "\n")
	}
	b.WriteString("\n" + th.CardTitle.Render("▶ Sunucu Kaynak Limitleri") + "\n")
	if len(a.servers) == 0 {
		b.WriteString(th.Muted.Render("sunucu yok"))
	}
	for _, s := range a.servers {
		cpu := "sınırsız"
		if s.CPUQuota > 0 {
			cpu = fmt.Sprintf("%d%%", s.CPUQuota)
		}
		b.WriteString(fmt.Sprintf("  %s  %s  RAM %dMB · CPU %s · %s\n",
			stateBadge(th, s.State), th.Val.Render(s.Name), s.RAMMB, cpu, th.Muted.Render(string(s.Priority))))
	}
	if st.Tier == model.TierLow {
		b.WriteString("\n" + th.Muted.Render("Tier LOW: ağır analizler kapalı, polling seyrek."))
	}
	return a.contentFrame(w, h, "Performans", b.String())
}

func (a *App) renderNetwork(w, h int) string {
	th := a.th
	if a.status == nil {
		return a.contentFrame(w, h, "Ağ", th.Muted.Render("veri alınıyor…"))
	}
	st := a.status
	var b strings.Builder
	b.WriteString(kv(th, "Yerel IP", orDash(st.Net.LocalIP)) + "\n")
	b.WriteString(kv(th, "İnternet", onlineBadge(th, st.Net.Internet)) + "\n")
	b.WriteString(kv(th, "Hostname", orDash(st.Net.Hostname)) + "\n\n")
	b.WriteString(th.CardTitle.Render("◈ Ağ Arayüzleri") + "\n")
	for _, n := range st.Net.NICs {
		up := boolBadge(th, n.Up, "UP", "DOWN")
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", up, th.Val.Render(n.Name), th.Muted.Render(orDash(n.IPv4))))
	}
	b.WriteString("\n" + th.CardTitle.Render("▶ Sunucu Portları") + "\n")
	if len(a.servers) == 0 {
		b.WriteString(th.Muted.Render("sunucu yok"))
	}
	for _, s := range a.servers {
		b.WriteString(fmt.Sprintf("  %s  port %d\n", th.Val.Render(s.Name), s.Port))
	}
	return a.contentFrame(w, h, "Ağ", b.String())
}

// renderTunnel shows the Serveo tunnels (the cloudflared replacement). Tunnels
// are opened automatically when a server enables WAN access; each one is an
// `ssh -R … serveo.net` process whose public address is parsed from its output.
func (a *App) renderTunnel(w, h int) string {
	th := a.th
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("◇ İnternete Açık Tüneller (Serveo)") + "\n")
	if len(a.tunnels) == 0 {
		b.WriteString(th.Muted.Render("henüz tünel yok — bir sunucuyu 'WAN' ile açtığınızda otomatik kurulur") + "\n")
	} else {
		for _, t := range a.tunnels {
			state := th.Badge("KAPALI", th.P.Red)
			if t.Running {
				state = th.Badge("AÇIK", th.P.Green)
			}
			addr := th.Muted.Render("adres bekleniyor…")
			if t.Hostname != "" {
				addr = th.Accent.Render(t.Hostname)
			}
			b.WriteString(fmt.Sprintf("  %s  %s  pid=%d\n", state, addr, t.PID))
			if len(t.Log) > 0 {
				last := t.Log[len(t.Log)-1]
				b.WriteString(th.Muted.Render("    » "+truncate(last, w-10)) + "\n")
			}
		}
	}
	b.WriteString("\n" + th.Muted.Render("Serveo ssh üzerinden çalışır — ek kurulum/indirme yok."))
	b.WriteString("\n" + th.Muted.Render("Sunucu detayından 'İnternete Aç' sekmesi ile yönetebilirsiniz."))
	return a.contentFrame(w, h, "Tünel (Serveo)", b.String())
}

func (a *App) renderPeers(w, h int) string {
	th := a.th
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("★ MCOS Ağ Paylaşımı ve PC Eşleştirme") + "\n")
	b.WriteString(th.Muted.Render("Yerel ağdaki diğer MCOS bilgisayarları otomatik taranır. Yön tuşları (↑/↓) ile seçip Enter'a basabilirsiniz.") + "\n\n")

	if len(a.peers) == 0 {
		b.WriteString(th.Muted.Render("  [SEARCH] Aynı ağda aktif diğer MCOS bilgisayarı taranıyor...") + "\n")
	} else {
		for idx, p := range a.peers {
			sel := (a.focus == focusContent && a.rowCursor == idx)
			prefix := "  "
			if sel {
				prefix = theme.IconCursor + " "
			}
			state := string(p.State)
			if p.Paired {
				state += " (Eşleşmiş)"
			}
			b.WriteString(fmt.Sprintf("%s  %s  %s  %d Çekirdek  %dMB RAM  %s\n",
				th.Accent.Render(prefix),
				th.Val.Bold(sel).Render(truncate(p.Name, 16)),
				th.Val.Render(state), p.Cores, p.RAMMB, th.Muted.Render(p.IP)))
		}
	}

	b.WriteString("\n" + th.CardTitle.Render("▶ Yerel Sunucular") + "\n")
	if len(a.servers) == 0 {
		b.WriteString(th.Muted.Render("  henüz sunucu yok") + "\n")
	} else {
		for _, s := range a.servers {
			b.WriteString(fmt.Sprintf("  %s  %s  Port: %d  RAM: %dMB\n",
				stateBadge(th, s.State), th.Val.Render(s.Name), s.Port, s.RAMMB))
		}
	}
	return a.contentFrame(w, h, "MCOS Paylaşım", b.String())
}

func (a *App) renderSettings(w, h int) string {
	th := a.th
	cfg := a.config
	if cfg == nil {
		return a.contentFrame(w, h, "Ayarlar", th.Muted.Render("ayarlar yükleniyor…"))
	}
	turbo := th.Muted.Render("kapalı")
	if cfg.Turbo {
		turbo = th.Badge(theme.IconTurbo+" AÇIK", th.P.Accent)
	}

	options := []struct {
		title string
		val   string
		desc  string
	}{
		{"Tema Değiştir", cfg.Theme, "[Enter] Temayı Değiştir"},
		{"Turbo Modu", turbo, "[Enter] Aç / Kapat"},
		{"USB Kalıcı Yap (Persist)", "Çalıştır", "[Enter] USB /data Dizinini Kalıcı Yap"},
		{"Diske / USB'ye MCOS Kur", "Sihirbaz", "[Enter] MCOS Kurulum Sihirbazını Başlat"},
		{"Oto-Başlatma", yesno(cfg.AutostartServers), "[Enter] Sunucuları Açılışta Otomatik Başlat"},
	}

	var b strings.Builder
	b.WriteString(th.CardTitle.Render("⚙ Sistem Ayarları") + "\n")
	b.WriteString(th.Muted.Render("Yön tuşları (↑/↓) ile ayarı seçip Enter'a basarak değiştirebilirsiniz.") + "\n\n")

	for idx, opt := range options {
		sel := (a.focus == focusContent && a.rowCursor == idx)
		prefix := "  "
		if sel {
			prefix = theme.IconCursor + " "
		}

		labelStyle := th.Key.Render(fmt.Sprintf("%-28s", opt.title))
		if sel {
			labelStyle = th.Accent.Bold(true).Render(fmt.Sprintf("%-28s", opt.title))
		}

		b.WriteString(fmt.Sprintf("%s%s  %s  %s\n",
			th.Accent.Render(prefix),
			labelStyle,
			th.Val.Render(opt.val),
			th.Muted.Render("— "+opt.desc)))
	}

	b.WriteString("\n" + th.CardTitle.Render("★ Cluster & Düğüm") + "\n")
	b.WriteString(kv(th, "Etkin", yesno(cfg.Cluster.Enabled)) + "\n")
	b.WriteString(kv(th, "Düğüm adı", cfg.Cluster.NodeName) + "\n")

	return a.contentFrame(w, h, "Ayarlar", b.String())
}

func yesno(b bool) string {
	if b {
		return "evet"
	}
	return "hayır"
}
