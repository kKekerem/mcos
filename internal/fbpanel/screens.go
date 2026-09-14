package fbpanel

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/model"
)

// Bu dosya HER bölümün içeriğini çizer. Eski panelde bunlar on ayrı dosyaya
// dağılmıştı; burada tek yerde toplandı çünkü hepsi aynı birkaç yapı taşını
// (kart, satır, anahtar/değer, ilerleme çubuğu) paylaşıyor.

// ── Sistem Durumu ───────────────────────────────────────────────────────────

func (a *App) drawDashboard(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Sistem Durumu")
	st, _, cfg := a.Snapshot()
	if st == nil {
		a.waiting(in)
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
		{"İŞLEMCİ", fmt.Sprintf("%.0f%%", st.CPU.UsagePct), int(st.CPU.UsagePct), u.Pal.Accent},
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

	netCol, netTxt := u.Pal.Warn, "yok (çevrimdışı kip)"
	if st.Net.Internet {
		netCol, netTxt = u.Pal.OK, "var"
	}
	turbo, turboCol := "kapalı", u.Pal.TextDim
	if cfg != nil && cfg.Turbo {
		turbo, turboCol = "AÇIK", u.Pal.Warn
	}
	y = a.kvList(in, y, 18, [][3]any{
		{"İşlemci", st.CPU.Model, u.Pal.Text},
		{"Çekirdek", fmt.Sprintf("%d çekirdek / %d iş parçacığı", st.CPU.Cores, st.CPU.Threads), u.Pal.Text},
		{"Bellek", fmt.Sprintf("%s / %s", bytesShort(st.Memory.UsedBytes), bytesShort(st.Memory.TotalBytes)), u.Pal.Text},
		{"Ekran", a.displayLine(), u.Pal.Text},
		{"Yerel adres", st.Net.LocalIP, u.Pal.Text},
		{"İnternet", netTxt, netCol},
		{"Turbo", turbo, turboCol},
	})

	// Canlı olay geçmişi.
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
	in := a.contentPanel(r, "Sunucular")
	_, servers, _ := a.Snapshot()

	if len(servers) == 0 {
		u.Text(in.Min.X, in.Min.Y, "Henüz sunucu yok.", u.Pal.TextDim)
		a.hint(in, in.Min.Y+u.F.CellH*2,
			"Yeni bir sunucu oluşturmak için n tuşuna basın.")
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
			col := u.Pal.Accent
			if !a.contentFocused() {
				col = u.Pal.Border
			}
			u.P.StrokeRoundRect(rect(card), u.M.Radius, u.M.StrokeFocus, col)
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

// ── USB Bellek ──────────────────────────────────────────────────────────────

func (a *App) drawUSB(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "USB Bellek")
	st, _, _ := a.Snapshot()

	y := in.Min.Y
	if st == nil || !st.USB.Present {
		u.Text(in.Min.X, y, "Takılı USB bellek yok.", u.Pal.TextDim)
		a.hint(in, y+u.F.CellH*2,
			"Bir USB bellek takın; içindeki .jar dosyaları burada listelenir",
			"ve sunuculara tek adımda kurulabilir.")
		return
	}

	u.StatusDot(in.Min.X, y, u.Pal.OK)
	u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
		fmt.Sprintf("USB bellek takılı — %d bölüm", st.USB.Partitions), u.Pal.Text)
	y += u.F.CellH + u.M.PadY*2

	a.mu.Lock()
	jars := a.usbJars
	scanned := a.usbScanned
	a.mu.Unlock()

	// Ağ ve eş taramasıyla AYNI gösterge: bekleme her yerde aynı görünmeli.
	if a.scanning() {
		a.mu.Lock()
		note := a.scanNote
		a.mu.Unlock()
		if note == "" {
			note = "USB bellek taranıyor…"
		}
		u.ScanBanner(in, y, a.Spin(), note, "")
		return
	}
	if !scanned {
		u.Text(in.Min.X, y, "Taramak için r tuşuna basın.", u.Pal.TextFaint)
		return
	}
	if len(jars) == 0 {
		u.Text(in.Min.X, y, "USB bellekte .jar dosyası bulunamadı.", u.Pal.TextDim)
		return
	}

	u.Text(in.Min.X, y, "BULUNAN DOSYALAR", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	for i, j := range jars {
		if y+u.M.RowH > in.Max.Y {
			u.Text(in.Min.X, y, fmt.Sprintf("… ve %d dosya daha", len(jars)-i), u.Pal.TextFaint)
			break
		}
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx, col := a.contentRow(row, i)
		ty := y + (u.M.RowH-u.F.CellH)/2
		u.Text(cx, ty, j.Name, col)
		u.TextRight(in.Max.X-u.M.PadX, ty,
			fmt.Sprintf("%s  ·  %s", j.Origin(), bytesShort(uint64(j.SizeBytes))),
			u.Pal.TextFaint)
		y += u.M.RowH
	}
}

// ── Yazılım (Java) ──────────────────────────────────────────────────────────

// javaOffer lists the Java majors MCOS can install.
//
// Sıra eski panelle aynı (1=17, 2=21, 3=11, 4=8) ama artık sayı tuşu yerine
// listeden seçiliyor — hangi sürümün neye yaradığı da yazılı.
var javaOffer = []struct {
	major int
	note  string
}{
	{17, "Minecraft 1.17 – 1.20.4"},
	{21, "Minecraft 1.20.5 ve sonrası"},
	{11, "Eski sürümler / bazı modlar"},
	{8, "Minecraft 1.16 ve öncesi"},
}

func (a *App) drawSoftware(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Yazılım")
	st, _, _ := a.Snapshot()
	if st == nil {
		a.waiting(in)
		return
	}

	y := in.Min.Y
	u.Text(in.Min.X, y, "KURULU JAVA SÜRÜMLERİ", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	a.mu.Lock()
	runtimes := a.javaRuntimes
	a.mu.Unlock()

	if len(runtimes) == 0 && len(st.JavaVersions) == 0 {
		u.Text(in.Min.X, y, "Kurulu Java yok.", u.Pal.Warn)
		y += u.F.CellH + u.M.PadY
	} else if len(runtimes) > 0 {
		for _, rt := range runtimes {
			u.StatusDot(in.Min.X, y, u.Pal.OK)
			x := u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
				fmt.Sprintf("Java %d", rt.Major), u.Pal.Text)
			u.Text(x+u.M.Gap*2, y, rt.Version, u.Pal.TextDim)
			u.TextRight(in.Max.X-u.M.PadX, y, rt.Vendor, u.Pal.TextFaint)
			y += u.F.CellH + u.M.PadY/2
		}
	} else {
		for _, v := range st.JavaVersions {
			u.StatusDot(in.Min.X, y, u.Pal.OK)
			u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, fmt.Sprintf("Java %d", v), u.Pal.Text)
			y += u.F.CellH + u.M.PadY/2
		}
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2
	u.Text(in.Min.X, y, "KURULABİLİR", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	installed := map[int]bool{}
	for _, v := range st.JavaVersions {
		installed[v] = true
	}
	for _, rt := range runtimes {
		installed[rt.Major] = true
	}

	for i, o := range javaOffer {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx, col := a.contentRow(row, i)
		ty := y + (u.M.RowH-u.F.CellH)/2

		u.Radio(cx, ty, installed[o.major])
		cx += u.F.CellW + u.M.Gap
		x := u.Text(cx, ty, fmt.Sprintf("Java %d", o.major), col)
		u.Text(x+u.M.Gap*2, ty, o.note, u.Pal.TextFaint)

		if installed[o.major] {
			u.TextRight(in.Max.X-u.M.PadX, ty, "kurulu", u.Pal.OK)
		}
		y += u.M.RowH
	}

	if !st.Net.Internet {
		y += u.M.PadY
		u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
			"İnternet yok — indirme yapılamaz.", u.Pal.Warn)
	}
}

// ── Performans ──────────────────────────────────────────────────────────────

func (a *App) drawPerformance(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Performans")
	st, servers, cfg := a.Snapshot()
	if st == nil {
		a.waiting(in)
		return
	}

	y := in.Min.Y
	bar := func(label string, pct float64, detail string) {
		u.Text(in.Min.X, y, label, u.Pal.TextDim)
		u.TextRight(in.Max.X-u.M.PadX, y, detail, u.Pal.Text)
		y += u.F.CellH + u.M.PadY/2
		u.Progress(in.Min.X, y, in.Dx()-u.M.PadX, int(pct))
		y += u.F.CellH + u.M.PadY
	}
	bar("İşlemci", st.CPU.UsagePct, fmt.Sprintf("%.0f%%", st.CPU.UsagePct))
	bar("Bellek", st.Memory.UsagePct,
		fmt.Sprintf("%s / %s", bytesShort(st.Memory.UsedBytes), bytesShort(st.Memory.TotalBytes)))
	for _, d := range st.Disks {
		if y+u.F.CellH*4 > in.Max.Y-u.F.CellH*6 {
			break
		}
		bar("Disk "+d.Mount, d.UsagePct,
			fmt.Sprintf("%s / %s", bytesShort(d.UsedBytes), bytesShort(d.TotalBytes)))
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	on := cfg != nil && cfg.Turbo
	row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
	cx, _ := a.contentRow(row, 0)
	ty := y + (u.M.RowH-u.F.CellH)/2
	u.Check(cx, ty, on)
	txt, col := "Turbo kapalı", u.Pal.TextDim
	if on {
		txt, col = "Turbo AÇIK", u.Pal.Warn
	}
	u.Text(cx+u.F.CellW+u.M.Gap, ty, txt, col)
	y += u.M.RowH + u.M.PadY/2

	a.hint(in, y,
		"Turbo açıkken kaynak sınırları yok sayılır ve sunucular yüksek",
		"öncelikle çalıştırılır. t tuşu veya Enter ile değiştirin.")

	// Kaynak bütçesi — ne uygulandığı görünür olsun.
	if cfg != nil {
		y += u.F.CellH*2 + u.M.PadY
		ram := "sınırsız"
		if cfg.Budget.MaxServerRAMMB > 0 {
			ram = fmt.Sprintf("%d MB", cfg.Budget.MaxServerRAMMB)
		}
		cpu := "sınırsız"
		if cfg.Budget.MaxServerCPUPercent > 0 {
			cpu = fmt.Sprintf("%%%d", cfg.Budget.MaxServerCPUPercent)
		}
		a.kvList(in, y, 22, [][3]any{
			{"Sunucu başına bellek", ram, u.Pal.Text},
			{"Sunucu başına işlemci", cpu, u.Pal.Text},
			{"Çalışan sunucu", fmt.Sprintf("%d", countRunning(servers)), u.Pal.Text},
		})
	}
}

func countRunning(list []*model.Server) int {
	n := 0
	for _, s := range list {
		if s.State == model.StateRunning {
			n++
		}
	}
	return n
}

// ── Donanım ─────────────────────────────────────────────────────────────────

func (a *App) drawDevices(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Donanım")
	st, _, _ := a.Snapshot()
	if st == nil {
		a.waiting(in)
		return
	}
	rows := [][3]any{
		{"İşlemci", st.CPU.Model, u.Pal.Text},
		{"Çekirdek", fmt.Sprintf("%d / %d", st.CPU.Cores, st.CPU.Threads), u.Pal.Text},
	}
	if st.CPU.MHz > 0 {
		rows = append(rows, [3]any{"Frekans", fmt.Sprintf("%d MHz", st.CPU.MHz), u.Pal.Text})
	}
	if st.CPU.TempC > 0 {
		c := u.Pal.Text
		if st.CPU.TempC > 80 {
			c = u.Pal.Warn
		}
		rows = append(rows, [3]any{"Sıcaklık", fmt.Sprintf("%.0f °C", st.CPU.TempC), c})
	}
	rows = append(rows, [3]any{"Bellek", bytesShort(st.Memory.TotalBytes), u.Pal.Text})
	for _, g := range st.GPUs {
		rows = append(rows, [3]any{"Ekran kartı", strings.TrimSpace(g.Vendor + " " + g.Model), u.Pal.Text})
	}
	rows = append(rows, [3]any{"Ekran", a.displayLine(), u.Pal.Text})
	for _, d := range st.Disks {
		rows = append(rows, [3]any{"Disk " + d.Mount,
			fmt.Sprintf("%s (%s)", bytesShort(d.TotalBytes), d.Filesystem), u.Pal.Text})
	}
	if len(st.JavaVersions) > 0 {
		var vs []string
		for _, v := range st.JavaVersions {
			vs = append(vs, fmt.Sprint(v))
		}
		rows = append(rows, [3]any{"Java", strings.Join(vs, ", "), u.Pal.Text})
	}
	a.kvList(in, in.Min.Y, 16, rows)
}

// ── Ağ ──────────────────────────────────────────────────────────────────────

func (a *App) drawNetwork(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Ağ")
	st, _, _ := a.Snapshot()
	if st == nil {
		a.waiting(in)
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

	// Tararken AYNI radar animasyonu: kullanıcının isteği "listelenirken
	// dönen animasyon, aynıları wifi veya bir şey listelenirken de olacak".
	// Tek bir fonksiyon (ScanBanner) kullanmak, iki ekranın birbirinden
	// farklı görünmesini imkânsız kılar.
	if a.scanning() {
		a.mu.Lock()
		note := a.scanNote
		a.mu.Unlock()
		if note == "" {
			note = "Kablosuz ağlar aranıyor…"
		}
		y = u.ScanBanner(in, y, a.Spin(), note, "Bu birkaç saniye sürebilir.")
		y += u.M.PadY
	}

	u.Text(in.Min.X, y, "ARAYÜZLER", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	for i, n := range st.Net.NICs {
		if y+u.M.RowH > in.Max.Y-u.F.CellH*4 {
			break
		}
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx, tc := a.contentRow(row, i)
		ty := y + (u.M.RowH-u.F.CellH)/2

		dot := u.Pal.TextFaint
		switch {
		case n.Up && n.Link:
			dot = u.Pal.OK
		case n.Up:
			dot = u.Pal.Warn
		}
		u.StatusDot(cx, ty, dot)
		x := u.Text(cx+u.F.CellW+u.M.Gap, ty, n.Name, tc)
		kind := n.Kind
		if kind == "" {
			kind = "diğer"
		} else if kind == "wireless" {
			kind = "kablosuz"
		} else if kind == "wired" {
			kind = "kablolu"
		}
		u.Text(x+u.M.Gap*2, ty, kind, u.Pal.TextFaint)
		if n.IPv4 != "" {
			u.TextRight(in.Max.X-u.M.PadX, ty, n.IPv4, u.Pal.TextDim)
		}
		y += u.M.RowH
	}

	a.mu.Lock()
	note := a.wifiNote
	a.mu.Unlock()
	if note != "" {
		y += u.M.PadY
		u.Text(in.Min.X, y, note, u.Pal.TextDim)
		y += u.F.CellH
	}

	y += u.M.PadY
	a.hint(in, y,
		"w  kablosuz ağ seç        e  kablolu bağlan")
}

// ── Ekran ───────────────────────────────────────────────────────────────────

// displayModes are the resolutions offered to the user.
//
// Liste scripts/lib/display.sh içindeki MCOS_MODES ile AYNI olmalı: orada
// olmayan bir modu seçmek, açılışta GRUB'un onu bulamaması demektir.
var displayModes = []string{
	"1920x1080", "1680x1050", "1600x900", "1440x900",
	"1366x768", "1280x1024", "1280x800", "1280x720", "1024x768",
}

func (a *App) drawDisplay(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Ekran")

	y := in.Min.Y

	// ŞU ANKİ durum — kullanıcı önce neye baktığını görmeli.
	a.mu.Lock()
	curW, curH := a.screenW, a.screenH
	pref := a.displayPref
	a.mu.Unlock()

	u.Text(in.Min.X, y, "ŞU AN", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	// Gerçek çözünürlük düşükse bunu VURGULA. Kullanıcı "ekran hâlâ düşük
	// çözünürlüklü" dediğinde bakacağı yer burasıdır: rakamı görmeli ve
	// neden öyle olduğunu okuyabilmeli.
	resCol := u.Pal.Accent
	if curH > 0 && curH < 900 {
		resCol = u.Pal.Warn
	}
	y = a.kvList(in, y, 20, [][3]any{
		{"Çözünürlük", fmt.Sprintf("%d x %d", curW, curH), resCol},
		{"Yazı hücresi", fmt.Sprintf("%d x %d piksel", u.F.CellW, u.F.CellH), u.Pal.Text},
		{"Izgara", fmt.Sprintf("%d sütun x %d satır", curW/u.F.CellW, curH/u.F.CellH), u.Pal.Text},
	})

	// Seçilen ile gerçekleşen uyuşmuyorsa sebebini söyle — sessizce farklı
	// bir moda düşmek, kullanıcının "ayar çalışmıyor" sanmasına yol açar.
	if pref != "" && pref != fmt.Sprintf("%dx%d", curW, curH) {
		y += u.M.PadY
		u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
			"Seçilen mod ("+pref+") uygulanmadı.", u.Pal.Warn)
		y += u.F.CellH + u.M.PadY/2
		y = a.hint(in, y,
			"Ya henüz yeniden başlatılmadı, ya da bu ekran/firmware o modu",
			"sunmuyor. Firmware sunmuyorsa GRUB listedeki bir sonraki modu seçer.")
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	u.Text(in.Min.X, y, "AÇILIŞ ÇÖZÜNÜRLÜĞÜ", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	for i, m := range displayModes {
		if y+u.M.RowH > in.Max.Y-u.F.CellH*5 {
			break
		}
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx, col := a.contentRow(row, i)
		ty := y + (u.M.RowH-u.F.CellH)/2

		u.Radio(cx, ty, m == pref)
		u.Text(cx+u.F.CellW+u.M.Gap, ty, strings.Replace(m, "x", " x ", 1), col)

		if fmt.Sprintf("%dx%d", curW, curH) == m {
			u.TextRight(in.Max.X-u.M.PadX, ty, "şu an etkin", u.Pal.OK)
		} else if m == pref {
			u.TextRight(in.Max.X-u.M.PadX, ty, "seçili (yeniden başlatınca)", u.Pal.Warn)
		}
		y += u.M.RowH
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	// DÜRÜSTLÜK: çözünürlük çalışırken değiştirilemez ve sebebi yazılı.
	u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
	u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
		"Çözünürlük değişikliği yeniden başlatma gerektirir.", u.Pal.Warn)
	y += u.F.CellH + u.M.PadY
	a.hint(in, y,
		"Bu sistemde ekran kartı sürücüsü yok; framebuffer'ı firmware kurar.",
		"Bu yüzden mod ancak önyükleyicide (GRUB) değiştirilebilir. Seçiminiz",
		"kaydedilir ve bir sonraki açılışta uygulanır.")
}

// displayLine renders the current mode as one line.
func (a *App) displayLine() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.screenW == 0 {
		return ""
	}
	return fmt.Sprintf("%d x %d", a.screenW, a.screenH)
}

// ── Güç ─────────────────────────────────────────────────────────────────────

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
	in := a.contentPanel(r, "Güç")
	cur := a.Cursor()

	y := in.Min.Y
	for i, it := range powerItems {
		rowH := u.F.CellH*2 + u.M.PadY*2
		row := image.Rect(in.Min.X, y, in.Max.X, y+rowH)
		cx, _ := a.contentRow(row, i)
		ty := row.Min.Y + u.M.PadY

		col := u.Pal.Text
		if it.danger {
			col = u.Pal.Error
		}
		if i == cur && a.contentFocused() {
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
