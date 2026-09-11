package fbpanel

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
	"mcos/internal/model"
)

// Draw paints the whole screen.
//
// ── Düzen ───────────────────────────────────────────────────────────────────
//
//	┌──────────┬────────────────────────────────┐
//	│ kenar    │ içerik                         │
//	│ çubuğu   │                                │
//	├──────────┴────────────────────────────────┤
//	│ canlı olay              kısayollar        │  <- alt çubuk
//	└───────────────────────────────────────────┘
//
// Eski panelle AYNI yerleşim. Alt çubuk yeni: kullanıcı "altta biraz alan
// olacak oradan girdiğimiz yerin kısayolu ve şu an olan şey olacak" dedi.
func (a *App) Draw() {
	u := a.ui
	b := u.Bounds()

	if a.Asleep() {
		// Uykuda hiçbir şey çizilmez: ekran tamamen siyah.
		// Donanım gerçekten karartılamıyorsa (efifb/simpledrm) görsel etki
		// budur; cmd katmanı ayrıca FBIOBLANK dener.
		u.P.Fill(b, color.RGBA{A: 255})
		return
	}

	u.Clear()

	barH := u.StatusBarH()
	content := image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Max.Y-barH)

	pad := u.M.PadX
	sideW := a.sidebarWidth()

	a.drawSidebar(image.Rect(content.Min.X+pad, content.Min.Y+pad,
		content.Min.X+pad+sideW, content.Max.Y-pad))

	main := image.Rect(content.Min.X+pad*2+sideW, content.Min.Y+pad,
		content.Max.X-pad, content.Max.Y-pad)
	a.drawContent(main)

	// Açılır pencere en üstte, arkası bulanık.
	if m := a.ActiveModal(); m != nil {
		if !a.scrim.Restore(u) {
			a.scrim.Capture(u)
			a.scrim.Restore(u)
		}
		cols, rows := m.Size()
		in := u.Modal(cols*u.F.CellW, rows*u.F.CellH, m.Title())
		m.Draw(a, in)
	}

	u.StatusBar(a.LastEvent(), a.shortcuts(), a.spinFrame())
}

// sidebarWidth scales with the font so the labels always fit.
//
// Sabit piksel genişliği 4K'da minik, 800x600'de devasa olurdu; en uzun
// bölüm adına göre hesaplamak her çözünürlükte doğru sonucu verir.
func (a *App) sidebarWidth() int {
	longest := 0
	for _, n := range sectionNames {
		if len(n) > longest {
			longest = len([]rune(n))
		}
	}
	// ad + ok işareti + iki yandan dolgu
	return (longest+3)*a.ui.F.CellW + a.ui.M.PadX*2
}

func (a *App) spinFrame() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.spin
}

// drawSidebar paints the navigation column.
func (a *App) drawSidebar(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "", false)

	st, _, _ := a.Snapshot()

	y := in.Min.Y
	x := u.Text(in.Min.X, y, "MCOS", u.Pal.Accent)
	ver := "v0.1.0"
	if st != nil && st.Version != "" {
		ver = "v" + st.Version
	}
	u.Text(x+u.M.Gap, y, ver, u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	cur := a.Section()
	for _, s := range a.visibleSections() {
		row := image.Rect(in.Min.X-u.M.PadX/2, y, in.Max.X+u.M.PadX/2, y+u.M.RowH)
		cx := u.Row(row, s == cur)
		ty := y + (u.M.RowH-u.F.CellH)/2

		col := u.Pal.TextDim
		if s == cur {
			col = u.Pal.Accent
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		}
		u.Text(cx, ty, s.Name(), col)

		// Sağda küçük bir sayaç/gösterge: kullanıcı bölüme girmeden durumu
		// görebilmeli.
		if badge, bc := a.sidebarBadge(s); badge != "" {
			u.TextRight(in.Max.X, ty, badge, bc)
		}
		y += u.M.RowH
	}
}

// sidebarBadge returns a short indicator for a section.
func (a *App) sidebarBadge(s Section) (string, color.RGBA) {
	u := a.ui
	st, servers, _ := a.Snapshot()
	switch s {
	case SecServers:
		if st == nil {
			return "", u.Pal.TextFaint
		}
		up := 0
		for _, sv := range servers {
			if sv.State == model.StateRunning {
				up++
			}
		}
		if len(servers) == 0 {
			return "", u.Pal.TextFaint
		}
		c := u.Pal.TextFaint
		if up > 0 {
			c = u.Pal.OK
		}
		return fmt.Sprintf("%d/%d", up, len(servers)), c
	case SecNetwork:
		if st == nil {
			return "", u.Pal.TextFaint
		}
		if !st.Net.Internet {
			return "çevrimdışı", u.Pal.Warn
		}
	}
	return "", u.Pal.TextFaint
}

// drawContent dispatches to the per-section renderer.
func (a *App) drawContent(r image.Rectangle) {
	switch a.Section() {
	case SecDashboard:
		a.drawDashboard(r)
	case SecServers:
		a.drawServers(r)
	case SecPower:
		a.drawPower(r)
	case SecSettings:
		a.drawSettings(r)
	case SecNetwork:
		a.drawNetwork(r)
	case SecPerformance:
		a.drawPerformance(r)
	case SecDevices:
		a.drawDevices(r)
	default:
		a.drawPlaceholder(r)
	}
}

// ── Sistem Durumu ───────────────────────────────────────────────────────────

func (a *App) drawDashboard(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Sistem Durumu", true)
	st, servers, cfg := a.Snapshot()

	if st == nil {
		u.Text(in.Min.X, in.Min.Y, "Daemon'a bağlanılıyor…", u.Pal.TextDim)
		return
	}

	y := in.Min.Y

	// Üst satır: dört ölçüm kartı yan yana.
	cardW := (in.Dx() - u.M.Gap*3) / 4
	cardH := u.F.CellH * 4
	metrics := []struct {
		label, value string
		pct          int
		col          color.RGBA
	}{
		{"İŞLEMCI", fmt.Sprintf("%.0f%%", st.CPU.UsagePct), int(st.CPU.UsagePct), u.Pal.Accent},
		{"BELLEK", fmt.Sprintf("%.0f%%", st.Memory.UsagePct), int(st.Memory.UsagePct), u.Pal.Accent},
		{"SUNUCU", fmt.Sprintf("%d / %d", st.ServersUp, st.ServersTotal), pctOf(st.ServersUp, st.ServersTotal), u.Pal.OK},
		{"ÇALIŞMA", uptimeShort(st.Uptime), 0, u.Pal.TextDim},
	}
	for i, m := range metrics {
		cx := in.Min.X + i*(cardW+u.M.Gap)
		card := image.Rect(cx, y, cx+cardW, y+cardH)
		u.P.FillRoundRect(rect(card), u.M.Radius, u.Pal.Raised)

		u.Text(card.Min.X+u.M.PadX, card.Min.Y+u.M.PadY, m.label, u.Pal.TextFaint)
		u.Text(card.Min.X+u.M.PadX, card.Min.Y+u.M.PadY+u.F.CellH+u.M.PadY/2, m.value, m.col)
		if m.pct > 0 {
			u.Progress(card.Min.X+u.M.PadX, card.Max.Y-u.M.PadY-u.F.CellH/2,
				cardW-u.M.PadX*2, m.pct)
		}
	}
	y += cardH + u.M.PadY*2

	// Bilgi satırları.
	kv := func(k, v string, c color.RGBA) {
		u.Text(in.Min.X, y, k, u.Pal.TextDim)
		u.Text(in.Min.X+u.F.CellW*18, y, v, c)
		y += u.F.CellH + u.M.PadY/2
	}
	kv("İşlemci", st.CPU.Model, u.Pal.Text)
	kv("Çekirdek", fmt.Sprintf("%d çekirdek / %d iş parçacığı", st.CPU.Cores, st.CPU.Threads), u.Pal.Text)
	kv("Bellek", fmt.Sprintf("%s / %s", bytesShort(st.Memory.UsedBytes), bytesShort(st.Memory.TotalBytes)), u.Pal.Text)
	if st.Net.LocalIP != "" {
		kv("Yerel adres", st.Net.LocalIP, u.Pal.Text)
	}
	netCol, netTxt := u.Pal.Warn, "yok (çevrimdışı kip)"
	if st.Net.Internet {
		netCol, netTxt = u.Pal.OK, "var"
	}
	kv("İnternet", netTxt, netCol)
	if cfg != nil && cfg.Turbo {
		kv("Turbo", "açık", u.Pal.Warn)
	}
	_ = servers

	// Canlı olay geçmişi: alt çubuk yalnızca sonuncuyu gösterir, burada
	// hepsi durur. Bir sunucu gece çöktüyse sabah burada görünür.
	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2
	u.Text(in.Min.X, y, "SON OLAYLAR", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY/2

	ev := a.Events()
	for i := len(ev) - 1; i >= 0 && y+u.F.CellH < in.Max.Y; i-- {
		e := ev[i]
		_, dot := u.EventColors(e.Kind)
		u.StatusDot(in.Min.X, y, dot)
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, e.At.Format("15:04:05"), u.Pal.TextFaint)
		u.Text(in.Min.X+u.F.CellW*11, y, e.Text, u.Pal.TextDim)
		y += u.F.CellH + u.M.PadY/3
	}
}

// ── Sunucular ───────────────────────────────────────────────────────────────

func (a *App) drawServers(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Sunucular", true)
	_, servers, _ := a.Snapshot()

	if len(servers) == 0 {
		u.Text(in.Min.X, in.Min.Y, "Henüz sunucu yok.", u.Pal.TextDim)
		u.Text(in.Min.X, in.Min.Y+u.F.CellH*2,
			"Yeni bir sunucu oluşturmak için N tuşuna basın.", u.Pal.TextFaint)
		return
	}

	cur := a.Cursor()
	y := in.Min.Y
	cardH := u.F.CellH*3 + u.M.PadY*2

	for i, s := range servers {
		if y+cardH > in.Max.Y {
			u.Text(in.Min.X, y, fmt.Sprintf("… ve %d sunucu daha", len(servers)-i), u.Pal.TextFaint)
			break
		}
		card := image.Rect(in.Min.X, y, in.Max.X, y+cardH)
		u.P.FillRoundRect(rect(card), u.M.Radius, u.Pal.Raised)
		if i == cur {
			u.P.StrokeRoundRect(rect(card), u.M.Radius, u.M.StrokeFocus, u.Pal.Accent)
		}

		cx := card.Min.X + u.M.PadX
		cy := card.Min.Y + u.M.PadY

		kind, label, col := serverBadge(u, s)
		if kind == fbui.EventBusy {
			u.Spinner(cx, cy, a.spinFrame(), col)
		} else {
			u.StatusDot(cx, cy, col)
		}

		nx := u.Text(cx+u.F.CellW+u.M.Gap, cy, s.Name, u.Pal.Text)
		u.Text(nx+u.M.Gap*2, cy, string(s.Software)+" "+s.MCVersion, u.Pal.TextDim)

		bw := u.TextWidth(label) + u.F.CellW*2
		u.Badge(card.Max.X-u.M.PadX-bw, cy-u.F.CellH/6, label, col)

		cy += u.F.CellH + u.M.PadY
		info := fmt.Sprintf("Port %d", s.Port)
		if s.RAMMB > 0 {
			info += fmt.Sprintf("   Bellek %d MB", s.RAMMB)
		}
		u.Text(cx+u.F.CellW+u.M.Gap, cy, info, u.Pal.TextDim)

		y = card.Max.Y + u.M.PadY
	}
}

// serverBadge maps a server state to its indicator.
func serverBadge(u *fbui.UI, s *model.Server) (fbui.EventKind, string, color.RGBA) {
	switch s.State {
	case model.StateRunning:
		return fbui.EventOK, "ÇALIŞIYOR", u.Pal.OK
	case model.StateStarting:
		return fbui.EventBusy, "BAŞLIYOR", u.Pal.Warn
	case model.StateStopping:
		return fbui.EventBusy, "KAPANIYOR", u.Pal.Warn
	case model.StateError:
		return fbui.EventError, "HATA", u.Pal.Error
	default:
		return fbui.EventInfo, "KAPALI", u.Pal.TextFaint
	}
}

// ── Güç ─────────────────────────────────────────────────────────────────────

// powerItems are the entries of the power menu, in order.
//
// "Uyku" kullanıcının isteğiyle eklendi: ekran kapanır, SUNUCULAR ÇALIŞMAYA
// DEVAM EDER. Bu ayrım menüde açıkça yazılı, çünkü "uyku" kelimesi çoğu
// masaüstü sisteminde her şeyin durması anlamına gelir.
var powerItems = []struct {
	label, desc string
	danger      bool
}{
	{"Uyku", "Ekranı kapatır. Sunucular çalışmaya devam eder; tuş veya fareyle uyanır.", false},
	{"Yeniden Başlat", "Sistemi yeniden başlatır. Çalışan sunucular düzgünce durdurulur.", true},
	{"Kapat", "Sistemi kapatır. Çalışan sunucular düzgünce durdurulur.", true},
}

func (a *App) drawPower(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Güç", true)
	cur := a.Cursor()

	y := in.Min.Y
	for i, it := range powerItems {
		rowH := u.F.CellH*2 + u.M.PadY*2
		row := image.Rect(in.Min.X, y, in.Max.X, y+rowH)
		cx := u.Row(row, i == cur)
		ty := row.Min.Y + u.M.PadY

		col := u.Pal.Text
		if it.danger {
			col = u.Pal.Error
		}
		if i == cur {
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		}
		u.Text(cx, ty, it.label, col)
		u.Text(cx, ty+u.F.CellH+u.M.PadY/2, it.desc, u.Pal.TextFaint)
		y = row.Max.Y + u.M.PadY/2
	}

	a.mu.Lock()
	note := a.sleepMsg
	a.mu.Unlock()
	if note != "" {
		y += u.M.PadY
		u.Divider(in.Min.X, in.Max.X, y)
		y += u.M.PadY * 2
		u.Text(in.Min.X, y, note, u.Pal.TextFaint)
	}
}

// ── Ayarlar ─────────────────────────────────────────────────────────────────

func (a *App) drawSettings(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Ayarlar", true)
	_, _, cfg := a.Snapshot()
	cur := a.Cursor()

	y := in.Min.Y
	u.Text(in.Min.X, y, "TEMA", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	active := ""
	if cfg != nil {
		active = cfg.Theme
	}
	for i, name := range fbui.ThemeOrder {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx := u.Row(row, i == cur)
		ty := y + (u.M.RowH-u.F.CellH)/2

		u.Radio(cx, ty, name == active)
		col := u.Pal.Text
		if i == cur {
			col = u.Pal.Accent
		}
		nx := u.Text(cx+u.F.CellW+u.M.Gap, ty, fbui.ThemeLabel(name), col)

		// Vurgu rengini yerinde göster: kullanıcı seçmeden önce görsün.
		sw := fbui.DefaultPalette.WithAccent(name).Accent
		u.P.FillRoundRect(
			fbdraw.R(float64(nx+u.M.Gap*2), float64(ty)+float64(u.F.CellH)*0.2,
				float64(u.F.CellW)*3, float64(u.F.CellH)*0.6),
			float64(u.F.CellH)*0.3, sw)
		y += u.M.RowH
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2
	u.Text(in.Min.X, y, "Tema seçmek için ↑↓ ve Enter.", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY
	u.Text(in.Min.X, y, "Ekran çözünürlüğü \"Ekran\" bölümünden değiştirilir "+
		"(yeniden başlatma gerekir).", u.Pal.TextFaint)
}

// ── Ağ ──────────────────────────────────────────────────────────────────────

func (a *App) drawNetwork(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Ağ", true)
	st, _, _ := a.Snapshot()
	if st == nil {
		u.Text(in.Min.X, in.Min.Y, "Daemon'a bağlanılıyor…", u.Pal.TextDim)
		return
	}

	y := in.Min.Y
	col, txt := u.Pal.Warn, "İnternet yok — çevrimdışı kip"
	if st.Net.Internet {
		col, txt = u.Pal.OK, "İnternet bağlantısı var"
	}
	u.StatusDot(in.Min.X, y, col)
	u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, txt, col)
	y += u.F.CellH + u.M.PadY*2

	u.Text(in.Min.X, y, "ARAYÜZLER", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	for _, n := range st.Net.NICs {
		if y+u.M.RowH > in.Max.Y {
			break
		}
		dot := u.Pal.TextFaint
		switch {
		case n.Up && n.Link:
			dot = u.Pal.OK
		case n.Up:
			dot = u.Pal.Warn
		}
		u.StatusDot(in.Min.X, y, dot)
		x := u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, n.Name, u.Pal.Text)
		kind := n.Kind
		if kind == "" {
			kind = "other"
		}
		u.Text(x+u.M.Gap*2, y, kind, u.Pal.TextFaint)
		if n.IPv4 != "" {
			u.TextRight(in.Max.X, y, n.IPv4, u.Pal.TextDim)
		}
		y += u.M.RowH
	}
}

// ── Performans ──────────────────────────────────────────────────────────────

func (a *App) drawPerformance(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Performans", true)
	st, _, cfg := a.Snapshot()
	if st == nil {
		u.Text(in.Min.X, in.Min.Y, "Daemon'a bağlanılıyor…", u.Pal.TextDim)
		return
	}

	y := in.Min.Y
	bar := func(label string, pct float64, detail string) {
		u.Text(in.Min.X, y, label, u.Pal.TextDim)
		u.TextRight(in.Max.X, y, detail, u.Pal.Text)
		y += u.F.CellH + u.M.PadY/2
		u.Progress(in.Min.X, y, in.Dx(), int(pct))
		y += u.F.CellH + u.M.PadY
	}
	bar("İşlemci", st.CPU.UsagePct, fmt.Sprintf("%.0f%%", st.CPU.UsagePct))
	bar("Bellek", st.Memory.UsagePct,
		fmt.Sprintf("%s / %s", bytesShort(st.Memory.UsedBytes), bytesShort(st.Memory.TotalBytes)))
	for _, d := range st.Disks {
		bar("Disk "+d.Mount, d.UsagePct,
			fmt.Sprintf("%s / %s", bytesShort(d.UsedBytes), bytesShort(d.TotalBytes)))
		if y+u.F.CellH*3 > in.Max.Y {
			break
		}
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	on := cfg != nil && cfg.Turbo
	u.Check(in.Min.X, y, on)
	txt := "Turbo kapalı"
	col := u.Pal.TextDim
	if on {
		txt, col = "Turbo AÇIK", u.Pal.Warn
	}
	u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, txt, col)
	y += u.M.RowH
	u.Text(in.Min.X, y, "Turbo açıkken kaynak sınırları yok sayılır ve sunucular", u.Pal.TextFaint)
	y += u.F.CellH
	u.Text(in.Min.X, y, "yüksek öncelikle çalıştırılır. T tuşuyla değiştirin.", u.Pal.TextFaint)
}

// ── Donanım ─────────────────────────────────────────────────────────────────

func (a *App) drawDevices(r image.Rectangle) {
	u := a.ui
	in := u.Panel(r, "Donanım", true)
	st, _, _ := a.Snapshot()
	if st == nil {
		u.Text(in.Min.X, in.Min.Y, "Daemon'a bağlanılıyor…", u.Pal.TextDim)
		return
	}
	y := in.Min.Y
	kv := func(k, v string) {
		if v == "" {
			return
		}
		u.Text(in.Min.X, y, k, u.Pal.TextDim)
		u.Text(in.Min.X+u.F.CellW*16, y, v, u.Pal.Text)
		y += u.F.CellH + u.M.PadY/2
	}
	kv("İşlemci", st.CPU.Model)
	kv("Çekirdek", fmt.Sprintf("%d / %d", st.CPU.Cores, st.CPU.Threads))
	if st.CPU.MHz > 0 {
		kv("Frekans", fmt.Sprintf("%d MHz", st.CPU.MHz))
	}
	if st.CPU.TempC > 0 {
		kv("Sıcaklık", fmt.Sprintf("%.0f °C", st.CPU.TempC))
	}
	kv("Bellek", bytesShort(st.Memory.TotalBytes))
	for _, g := range st.GPUs {
		kv("Ekran kartı", strings.TrimSpace(g.Vendor+" "+g.Model))
	}
	for _, d := range st.Disks {
		kv("Disk "+d.Mount, fmt.Sprintf("%s (%s)", bytesShort(d.TotalBytes), d.Filesystem))
	}
	if len(st.JavaVersions) > 0 {
		var vs []string
		for _, v := range st.JavaVersions {
			vs = append(vs, fmt.Sprint(v))
		}
		kv("Java", strings.Join(vs, ", "))
	}
}

// drawPlaceholder is used for sections not yet ported to this renderer.
//
// DÜRÜSTLÜK: "yakında" yazan sahte bir ekran göstermiyoruz. Bu bölüm henüz
// yeni çizim motoruna taşınmadı ve kullanıcı bunu bilmeli — eski panele nasıl
// dönüleceği de yazılı.
func (a *App) drawPlaceholder(r image.Rectangle) {
	u := a.ui
	sec := a.Section()
	in := u.Panel(r, sec.Name(), true)
	y := in.Min.Y
	u.Text(in.Min.X, y, "Bu bölüm henüz yeni arayüze taşınmadı.", u.Pal.Text)
	y += u.F.CellH + u.M.PadY*2
	u.Text(in.Min.X, y, "Eski panele dönmek için F12 tuşuna basın;", u.Pal.TextDim)
	y += u.F.CellH
	u.Text(in.Min.X, y, "orada bu bölüm tam olarak çalışıyor.", u.Pal.TextDim)
}

// ── Kısayollar ──────────────────────────────────────────────────────────────

// shortcuts returns the key hints for the current screen.
func (a *App) shortcuts() []fbui.Shortcut {
	if a.ActiveModal() != nil {
		return []fbui.Shortcut{
			{Key: "Esc", Label: "Kapat"},
			{Key: "Enter", Label: "Tamam"},
		}
	}
	base := []fbui.Shortcut{{Key: "F12", Label: "Eski panel"}}
	switch a.Section() {
	case SecServers:
		return append([]fbui.Shortcut{
			{Key: "Enter", Label: "Başlat/Durdur"},
			{Key: "K", Label: "Konsol"},
			{Key: "N", Label: "Yeni"},
		}, base...)
	case SecPower:
		return append([]fbui.Shortcut{{Key: "Enter", Label: "Seç"}}, base...)
	case SecSettings:
		return append([]fbui.Shortcut{{Key: "Enter", Label: "Temayı uygula"}}, base...)
	case SecPerformance:
		return append([]fbui.Shortcut{{Key: "T", Label: "Turbo"}}, base...)
	}
	return append([]fbui.Shortcut{{Key: "↑↓", Label: "Gezin"}}, base...)
}

// ── Yardımcılar ─────────────────────────────────────────────────────────────

func rect(r image.Rectangle) fbdraw.Rect {
	return fbdraw.R(float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy()))
}

func pctOf(a, b int) int {
	if b <= 0 {
		return 0
	}
	return a * 100 / b
}

func bytesShort(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTP"[exp])
}

func uptimeShort(sec int64) string {
	d := time.Duration(sec) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dg %dsa", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dsa %ddk", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%ddk", int(d.Minutes()))
	}
}
