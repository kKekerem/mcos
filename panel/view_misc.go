package panel

import (
	"fmt"
	"strings"

	"mcos/internal/model"
)

func (a *App) renderSoftware(w, h int) string {
	th := a.th
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("Kurulu Java Çalışma Zamanları") + "\n")
	if len(a.java) == 0 {
		b.WriteString(th.Muted.Render("kurulu Java yok — sunucu kurarken otomatik indirilir") + "\n")
	} else {
		for _, rt := range a.java {
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				th.Accent.Render(fmt.Sprintf("Java %-2d", rt.Major)),
				th.Val.Render(rt.Vendor),
				th.Muted.Render(truncate(rt.Version, w-30))))
		}
	}
	b.WriteString("\n" + th.CardTitle.Render("Desteklenen Sunucu Türleri") + "\n")
	var types []string
	for _, s := range model.AllSoftware {
		types = append(types, string(s))
	}
	b.WriteString(th.Val.Render("  " + strings.Join(types, " · ")))
	b.WriteString("\n\n" + th.Muted.Render("Java sürümü, sunucunun Minecraft sürümüne göre otomatik seçilir."))
	return a.contentFrame(w, h, "Yazılım", b.String())
}

func (a *App) renderPerformance(w, h int) string {
	th := a.th
	if a.status == nil {
		return a.contentFrame(w, h, "Performans", th.Muted.Render("veri alınıyor…"))
	}
	st := a.status
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("Sistem") + "\n")
	b.WriteString(kv(th, "CPU", bar(th, st.CPU.UsagePct, 24)) + "\n")
	b.WriteString(kv(th, "Bellek", bar(th, st.Memory.UsagePct, 24)) + "\n")
	for _, d := range st.Disks {
		b.WriteString(kv(th, "Disk "+d.Mount, bar(th, d.UsagePct, 24)) + "\n")
	}
	b.WriteString("\n" + th.CardTitle.Render("Sunucu Kaynak Limitleri") + "\n")
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
	b.WriteString(th.CardTitle.Render("Ağ Arayüzleri") + "\n")
	for _, n := range st.Net.NICs {
		up := boolBadge(th, n.Up, "UP", "DOWN")
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", up, th.Val.Render(n.Name), th.Muted.Render(orDash(n.IPv4))))
	}
	b.WriteString("\n" + th.CardTitle.Render("Sunucu Portları") + "\n")
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
	b.WriteString(th.CardTitle.Render("İnternete Açık Tüneller (Serveo)") + "\n")
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
	b.WriteString("\n" + th.Muted.Render("Sunucu detayı → 'İnternete Aç' sekmesinden yönetin."))
	return a.contentFrame(w, h, "Tünel (Serveo)", b.String())
}

func (a *App) renderPeers(w, h int) string {
	th := a.th
	if a.status != nil && !a.status.ClusterOn {
		return a.contentFrame(w, h, "PC Eşleştirme",
			th.Muted.Render("Tek PC modu — cluster devre dışı. Ayarlar'dan açılabilir."))
	}
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("Keşfedilen PC'ler") + "\n")
	if len(a.peers) == 0 {
		b.WriteString(th.Muted.Render("henüz eşleşmiş PC yok — aynı ağdaki diğer MCOS düğümleri otomatik keşfedilir") + "\n")
	} else {
		for _, p := range a.peers {
			state := string(p.State)
			if p.Paired {
				state += " (eşleşmiş)"
			}
			b.WriteString(fmt.Sprintf("  %s  %s  %d çekirdek  %dMB  %s\n",
				th.Accent.Render(truncate(p.Name, 16)), th.Val.Render(state), p.Cores, p.RAMMB, th.Muted.Render(p.IP)))
		}
	}
	b.WriteString("\n" + th.CardTitle.Render("Görevler") + "\n")
	if len(a.tasks) == 0 {
		b.WriteString(th.Muted.Render("aktif görev yok") + "\n")
	} else {
		for _, t := range a.tasks {
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n", t.ID, t.Kind, t.State))
		}
	}
	return a.contentFrame(w, h, "PC Eşleştirme", b.String())
}

func (a *App) renderSettings(w, h int) string {
	th := a.th
	cfg, err := a.cl.Config()
	if err != nil || cfg == nil {
		return a.contentFrame(w, h, "Ayarlar", th.Muted.Render("config alınamadı"))
	}
	turbo := th.Muted.Render("kapalı")
	if cfg.Turbo {
		turbo = th.Badge("AÇIK ⚡", th.P.Accent)
	}
	var b strings.Builder
	b.WriteString(kv(th, "Tema", cfg.Theme) + "\n")
	b.WriteString(kv(th, "Tier modu", cfg.Tier.Mode) + "\n")
	b.WriteString(kv(th, "Turbo modu", turbo) + "\n")
	b.WriteString(kv(th, "Oto-başlat", yesno(cfg.AutostartServers)) + "\n\n")
	b.WriteString(th.CardTitle.Render("Cluster") + "\n")
	b.WriteString(kv(th, "Etkin", yesno(cfg.Cluster.Enabled)) + "\n")
	b.WriteString(kv(th, "Düğüm adı", cfg.Cluster.NodeName) + "\n\n")
	b.WriteString(th.CardTitle.Render("Özellikler (auto/on/off)") + "\n")
	b.WriteString(kv(th, "GPU izleme", string(cfg.Features.GPUMonitor)) + "\n")
	b.WriteString(kv(th, "Gelişmiş analiz", string(cfg.Features.AdvancedAnalytics)) + "\n")
	b.WriteString(kv(th, "Tam performans", string(cfg.Features.FullPerformance)) + "\n\n")
	b.WriteString(th.CardTitle.Render("Kısayollar") + "\n")
	b.WriteString(th.Key.Render("  t") + th.Val.Render("  Turbo modunu aç/kapat (tüm sunucuları boost et)") + "\n")
	b.WriteString(th.Key.Render("  g") + th.Val.Render("  Güç menüsü — Kapat / Yeniden Başlat") + "\n")
	b.WriteString(th.Key.Render("  p") + th.Val.Render("  USB'yi kalıcı yap (/data reboot'ta korunur)") + "\n")
	return a.contentFrame(w, h, "Ayarlar", b.String())
}

func yesno(b bool) string {
	if b {
		return "evet"
	}
	return "hayır"
}
