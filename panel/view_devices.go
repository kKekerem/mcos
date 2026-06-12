package panel

import (
	"fmt"
	"strings"
)

// renderDevices is the dedicated "Donanım" tab: everything MCOS detected about
// this machine (CPU/RAM/disk/GPU + network adapters with driver/link) plus the
// non-blocking connect actions. Network adapters are the main "detected-later"
// hardware, so this is also where a wired link is brought up or Wi-Fi scanned.
func (a *App) renderDevices(w, h int) string {
	th := a.th
	if a.status == nil {
		return a.contentFrame(w, h, "Donanım", th.Muted.Render("donanım taranıyor…"))
	}
	st := a.status
	var b strings.Builder

	b.WriteString(th.CardTitle.Render("Sistem") + "\n")
	b.WriteString(kv(th, "İşlemci", fmt.Sprintf("%s (%d çekirdek / %d iş parçacığı)",
		truncate(st.CPU.Model, max(10, w-44)), st.CPU.Cores, st.CPU.Threads)) + "\n")
	b.WriteString(kv(th, "Bellek", fmt.Sprintf("%.1f GiB", float64(st.Memory.TotalBytes)/(1<<30))) + "\n")
	for _, d := range st.Disks {
		b.WriteString(kv(th, "Disk "+d.Mount, fmt.Sprintf("%.0f GiB", float64(d.TotalBytes)/(1<<30))) + "\n")
	}
	gpu := "algılanmadı"
	if len(st.GPUs) > 0 {
		gpu = truncate(strings.TrimSpace(st.GPUs[0].Vendor+" "+st.GPUs[0].Model), max(10, w-30))
	}
	b.WriteString(kv(th, "GPU", gpu) + "\n")
	b.WriteString(kv(th, "Ekran/Klavye", "bağlı (konsol etkin)") + "\n")

	b.WriteString("\n" + th.CardTitle.Render("Ağ Adaptörleri") + "\n")
	if len(st.Net.NICs) == 0 {
		b.WriteString(th.Muted.Render("  ağ adaptörü algılanmadı (sürücü gelmemiş olabilir)") + "\n")
	}
	for _, n := range st.Net.NICs {
		link := "—"
		if n.Link {
			link = "bağlı"
		}
		kind := n.Kind
		if kind == "" {
			kind = "?"
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s  sürücü=%s  link=%s  %s\n",
			boolBadge(th, n.Up, "UP", "DOWN"),
			th.Val.Render(fmt.Sprintf("%-8s", n.Name)),
			th.Muted.Render(fmt.Sprintf("%-9s", kind)),
			th.Muted.Render(orDash(n.Driver)),
			th.Muted.Render(link),
			th.Accent.Render(orDash(n.IPv4))))
	}
	b.WriteString(kv(th, "İnternet", onlineBadge(th, st.Net.Internet)) + "\n")

	b.WriteString("\n" + th.CardTitle.Render("WiFi Ağları") + " " + th.Muted.Render(a.wifiNote) + "\n")
	if len(a.wifiNets) == 0 {
		b.WriteString(th.Muted.Render("  (taramak için w)") + "\n")
	} else {
		rows := make([]string, len(a.wifiNets))
		for i, n := range a.wifiNets {
			lock := ""
			if n.Secured {
				lock = " [kilitli]"
			}
			line := fmt.Sprintf("%-24s %3d%%%s", truncate(n.SSID, 24), n.Signal, lock)
			if a.section == secDevices && a.focus == focusContent && i == a.wifiCursor && !a.wifiConnect {
				rows[i] = th.Accent.Render("» " + line)
			} else {
				rows[i] = "  " + th.Val.Render(line)
			}
		}
		b.WriteString(scrollList(th, rows, a.wifiCursor, 8) + "\n")
	}

	if a.wifiConnect {
		b.WriteString("\n" + th.CardTitle.Render("Bağlan » "+a.wifiSelectedSSID()) + "\n")
		b.WriteString("  " + a.wifiInput.View() + "\n")
		b.WriteString(th.Muted.Render("  Enter: bağlan · Esc: vazgeç"))
	} else {
		b.WriteString("\n" + th.Muted.Render(
			"w: tara · yukarı/aşağı: seç · Enter: bağlan · e: kabloyu bağla · r: yenile"))
	}
	return a.contentFrame(w, h, "Donanım", b.String())
}
