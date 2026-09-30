package fbpanel

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"unicode"

	"mcos/internal/fbdraw"
	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/model"
)

// ── Görünüm modeli ──────────────────────────────────────────────────────────
//
// Çizim ile tuşlar AYNI satır listesinden beslenir (detailViewFor): imleç
// yalnızca çizilen satırlarda gezebilir. Eski panelde Ayarlar'ın imleci sabit
// "7" ile sarıyordu; bir satır eklenince imleç görünmeyen bir satıra
// inebiliyordu. Tek kaynak bunu imkânsız kılar.

// detailItem is one selectable row.
type detailItem struct {
	label     string
	detail    string
	badge     string
	badgeKind fbui.EventKind
	// act, Enter'ın ne yapacağıdır (bkz. detailActivate). Boşsa satır
	// yalnızca bilgi verir ama yine de seçilebilir (dosya adı gibi).
	act   string
	value any
	// accent, bir EYLEM satırıdır ("Yeni yedek al"): vurgu renginde çizilir.
	accent   bool
	disabled bool
}

// detailView is everything a tab shows, top to bottom.
type detailView struct {
	info      [][2]string
	note      []string
	header    string
	items     []detailItem
	empty     string
	footTitle string
	foot      [][2]string
	loading   bool
	err       string
	// buttons: Genel sekmesindeki Başlat/Durdur/Yeniden başlat düğmeleri.
	buttons bool
}

func softwareLabel(sw model.Software) string {
	switch sw {
	case model.SoftwarePaper:
		return "Paper"
	case model.SoftwarePurpur:
		return "Purpur"
	case model.SoftwareSpigot:
		return "Spigot"
	case model.SoftwareCraftBukkit:
		return "CraftBukkit"
	case model.SoftwareFabric:
		return "Fabric"
	case model.SoftwareForge:
		return "Forge"
	case model.SoftwareNeoForge:
		return "NeoForge"
	case model.SoftwareFolia:
		return "Folia"
	case model.SoftwareQuilt:
		return "Quilt"
	case model.SoftwareVanilla:
		return "Vanilla"
	}
	if sw == "" {
		return "—"
	}
	return string(sw)
}

func stateLabel(st model.ServerState) string {
	switch st {
	case model.StateRunning:
		return "Çalışıyor"
	case model.StateStarting:
		return "Açılıyor"
	case model.StateStopping:
		return "Kapanıyor"
	case model.StateError:
		return "Hata"
	}
	return "Kapalı"
}

func evetHayir(b bool) string {
	if b {
		return "Evet"
	}
	return "Hayır"
}

func acikKapali(b bool) string {
	if b {
		return "Açık"
	}
	return "Kapalı"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func running(s *model.Server) bool {
	return s.State == model.StateRunning || s.State == model.StateStarting
}

func cpuQuotaLabel(q int) string {
	if q <= 0 {
		return "sınırsız"
	}
	return fmt.Sprintf("%%%d", q)
}

func gamemodeLabel(g string) string {
	switch g {
	case "survival", "":
		return "Hayatta kalma"
	case "creative":
		return "Yaratıcı"
	case "adventure":
		return "Macera"
	case "spectator":
		return "İzleyici"
	}
	return g
}

func difficultyLabel(d string) string {
	switch d {
	case "peaceful":
		return "Barışçıl"
	case "easy":
		return "Kolay"
	case "normal", "":
		return "Normal"
	case "hard":
		return "Zor"
	}
	return d
}

func downloadsShort(n int) string {
	switch {
	// "B" bayt ile karışırdı; Türkçe kısaltma yerine sözcük.
	case n >= 1_000_000:
		return strings.Replace(fmt.Sprintf("%.1f milyon", float64(n)/1e6), ".", ",", 1)
	case n >= 1_000:
		return strings.Replace(fmt.Sprintf("%.1f bin", float64(n)/1e3), ".", ",", 1)
	}
	return fmt.Sprintf("%d", n)
}

// generalInfo is the Genel tab (old panel: Durum…Oto-başlat + son günlük).
func generalInfo(s *model.Server, st *model.SystemStatus) [][2]string {
	uptime := "—"
	if s.State == model.StateRunning && s.UptimeSec > 0 {
		uptime = uptimeShort(s.UptimeSec)
	}
	java := "otomatik"
	if s.JavaMajor > 0 {
		java = fmt.Sprintf("Java %d", s.JavaMajor)
	}
	// Sunucu başına CPU ölçümü daemon'da YOK; uydurmak yerine kotayı ve
	// sistemin toplam kullanımını yazıyoruz.
	cpu := "kota " + cpuQuotaLabel(s.CPUQuota)
	if st != nil && st.CPU.Cores > 0 {
		cpu += fmt.Sprintf(" · sistem %%%.0f (%d çekirdek)", st.CPU.UsagePct, st.CPU.Cores)
	}
	pid := "—"
	if s.PID > 0 {
		pid = itoa(s.PID)
	}
	rows := [][2]string{
		{"Durum", stateLabel(s.State)},
		{"Yazılım", softwareLabel(s.Software)},
		{"Sürüm", "Minecraft " + orDash(s.MCVersion)},
		{"Java", java},
		{"Port", itoa(s.Port)},
		{"RAM", fmt.Sprintf("%d MB", s.RAMMB)},
		{"Oyuncular", fmt.Sprintf("%d / %d", s.Players, orInt(s.MaxPlayers, 20))},
		{"Çalışma süresi", uptime},
		{"CPU", cpu},
		{"PID", pid},
		{"Oto-başlat", evetHayir(s.Autostart)},
		{"Son günlük", orDash(s.LastLog)},
	}
	// Bölünmüş dünya: menüde tek sunucu, ama kaç süreç çalıştığı görünsün.
	if s.RunningInstances > 1 {
		rows = append(rows[:2], append([][2]string{
			{"Sunucu sayısı", fmt.Sprintf("×%d (dünya bölünmüş)", s.RunningInstances)},
		}, rows[2:]...)...)
	}
	return rows
}

// detailViewFor builds the active tab's rows.
func (a *App) detailViewFor(d *ServerDetail, s *model.Server) detailView {
	st, _, _ := a.Snapshot()
	d.mu.Lock()
	defer d.mu.Unlock()
	v := detailView{loading: d.loading[d.tab], err: d.errs[d.tab]}

	switch d.tab {
	case dtGeneral:
		v.info = generalInfo(s, st)
		v.buttons = true

	case dtSettings:
		v.items = []detailItem{
			{label: "RAM", detail: fmt.Sprintf("%d MB", s.RAMMB), act: "set-ram"},
			{label: "Görüş uzaklığı", detail: fmt.Sprintf("%d parça", orInt(s.ViewDistance, 10)), act: "set-view"},
			{label: "Simülasyon uzaklığı", detail: fmt.Sprintf("%d parça", orInt(s.SimDistance, 10)), act: "set-sim"},
			{label: "Maksimum oyuncu", detail: itoa(orInt(s.MaxPlayers, 20)), act: "set-maxplayers"},
			{label: "Tam performans", detail: acikKapali(s.FullPerf), act: "toggle-fullperf"},
			{label: "Otomatik başlat", detail: acikKapali(s.Autostart), act: "toggle-autostart"},
			{label: "İnternete aç (WAN)", detail: acikKapali(s.WAN.Enabled), act: "toggle-wan"},
		}
		v.footTitle = "SERVER.PROPERTIES — oluşturulurken yazıldı"
		v.foot = [][2]string{
			{"MOTD", orDash(s.MOTD)},
			{"Oyun modu", gamemodeLabel(s.Gamemode)},
			{"Zorluk", difficultyLabel(s.Difficulty)},
			{"Hardcore", evetHayir(s.Hardcore)},
			{"PvP", acikKapali(s.PVP)},
			{"Çevrimiçi doğrulama", acikKapali(s.OnlineMode)},
			{"Beyaz liste", acikKapali(s.Whitelist)},
			{"Dünya tohumu", orDash(s.LevelSeed)},
		}

	case dtPlayers:
		p := d.players
		online, max := p.Online, p.Max
		if max == 0 {
			online, max = s.Players, orInt(s.MaxPlayers, 20)
		}
		v.info = [][2]string{{"Çevrimiçi", fmt.Sprintf("%d / %d", online, max)}}
		for _, pl := range p.Players {
			v.items = append(v.items, detailItem{label: pl.Name, act: "player", value: pl.Name})
		}
		if running(s) {
			v.empty = "Çevrimiçi oyuncu yok."
		} else {
			v.empty = "Sunucu kapalı — oyuncuları görmek için başlatın (s)."
		}

	case dtSoftware:
		a.softwareView(d, s, &v)

	case dtFiles:
		loc := "/" + strings.TrimPrefix(strings.TrimPrefix(d.filesPath, "."), "/")
		v.info = [][2]string{{"Konum", loc}}
		if d.filesPath != "." {
			v.items = append(v.items, detailItem{label: "..", detail: "üst klasör", act: "dir", value: parentDir(d.filesPath)})
		}
		for _, f := range d.files {
			if f.IsDir {
				v.items = append(v.items, detailItem{label: f.Name + "/", detail: "klasör", act: "dir", value: f.Path})
			} else {
				v.items = append(v.items, detailItem{label: f.Name, detail: bytesShort(uint64(f.Size))})
			}
		}
		v.empty = "Klasör boş."

	case dtWorlds:
		v.info = [][2]string{{"Dünya sayısı", itoa(len(d.worlds))}}
		for _, w := range d.worlds {
			v.items = append(v.items, detailItem{label: w.Name, detail: bytesShort(uint64(w.SizeBytes))})
		}
		v.empty = "Dünya bulunamadı — sunucu hiç açılmamış olabilir."

	case dtBackups:
		v.info = backupPolicyRows(s, d.backupPol, nowFunc())
		v.items = append(v.items,
			detailItem{label: "Yeni yedek al", detail: "c", act: "backup-create", accent: true},
			detailItem{label: "Otomatik yedek planı…", detail: "o", act: "backup-policy", accent: true})
		for _, b := range d.backups {
			kind, bk := "manuel", fbui.EventInfo
			switch b.Type {
			case "auto":
				kind = "otomatik"
			case "restore-point":
				kind, bk = "geri dönüş", fbui.EventWarn
			}
			v.items = append(v.items, detailItem{
				label:  b.Name,
				detail: b.CreatedAt.Format("2006-01-02 15:04") + " · " + bytesShort(uint64(b.SizeBytes)),
				badge:  kind, badgeKind: bk, act: "backup", value: b,
			})
		}
		if len(d.backups) == 0 && !v.loading {
			v.note = []string{"Henüz yedek yok. Sürüm değiştirmeden önce MCOS otomatik yedek alır."}
		}

	case dtAccess:
		v.info = [][2]string{
			{"Çevrimiçi doğrulama", acikKapali(s.OnlineMode)},
			{"Beyaz liste zorunlu", acikKapali(s.Whitelist)},
		}
		v.items = append(v.items,
			detailItem{label: "Oyuncuya op yetkisi ver…", act: "access-op", accent: true},
			detailItem{label: "Beyaz listeye oyuncu ekle…", act: "access-wl", accent: true})
		for _, n := range d.access.ops {
			v.items = append(v.items, detailItem{label: n, badge: "op", badgeKind: fbui.EventBusy,
				act: "access", value: accessEntry{name: n, list: "op"}})
		}
		for _, n := range d.access.whitelist {
			v.items = append(v.items, detailItem{label: n, badge: "beyaz liste", badgeKind: fbui.EventOK,
				act: "access", value: accessEntry{name: n, list: "whitelist"}})
		}
		for _, n := range d.access.banned {
			v.items = append(v.items, detailItem{label: n, badge: "yasaklı", badgeKind: fbui.EventError,
				act: "access", value: accessEntry{name: n, list: "banned"}})
		}
		if !running(s) {
			v.note = []string{"Değişiklikler sunucu konsolundan uygulanır; önce sunucuyu başlatın (s)."}
		}

	case dtWAN:
		v.info = [][2]string{
			{"İnternete açık", acikKapali(s.WAN.Enabled)},
			{"Genel adres", orDash(s.WAN.Hostname)},
		}
		v.items = []detailItem{{label: "İnternete aç", detail: acikKapali(s.WAN.Enabled), act: "toggle-wan"}}
		v.note = []string{
			"Açıkken sunucu başlatılınca ssh tüneli açılır ve genel adres burada belirir.",
			"Kalıcı bir adres için Tünel (playit) bölümünü kullanın.",
		}

	case dtPerf:
		prio := string(s.Priority)
		switch s.Priority {
		case model.PriorityLow:
			prio = "Düşük"
		case model.PriorityHigh:
			prio = "Yüksek"
		case model.PriorityNormal, "":
			prio = "Normal"
		}
		aff := "tümü"
		if len(s.CPUAffinity) > 0 {
			parts := make([]string, len(s.CPUAffinity))
			for i, c := range s.CPUAffinity {
				parts[i] = itoa(c)
			}
			aff = strings.Join(parts, ",")
		}
		uptime := "—"
		if s.State == model.StateRunning && s.UptimeSec > 0 {
			uptime = uptimeShort(s.UptimeSec)
		}
		v.info = [][2]string{
			{"RAM limiti", fmt.Sprintf("%d MB", s.RAMMB)},
			{"CPU kotası", cpuQuotaLabel(s.CPUQuota)},
			{"CPU çekirdekleri", aff},
			{"Öncelik", prio},
			{"Tam performans", acikKapali(s.FullPerf)},
			{"JVM profili", orDash(s.JVMFlags)},
			{"Çalışma süresi", uptime},
		}
		if st != nil {
			v.info = append(v.info,
				[2]string{"Sistem CPU", fmt.Sprintf("%%%.0f · %d çekirdek", st.CPU.UsagePct, st.CPU.Cores)},
				[2]string{"Sistem belleği", bytesShort(st.Memory.UsedBytes) + " / " + bytesShort(st.Memory.TotalBytes)})
		}

	case dtNetwork:
		ip := ""
		if st != nil {
			ip = st.Net.LocalIP
		}
		lan := "—"
		// Ortak dünya proxy'si açıksa oyuncunun adresi proxy'nin portudur
		// (sunucu iç porta taşındı; iç port dışarıdan bağlantı kabul etmez).
		if ip != "" {
			lan = fmt.Sprintf("%s:%d", ip, s.ConnectPort())
		}
		v.info = [][2]string{
			{"Sunucu IP (yerel)", orDash(ip)},
			{"Port", itoa(s.ConnectPort())},
			{"Yerel ağdan bağlan", lan},
			{"İnternete açık", acikKapali(s.WAN.Enabled)},
			{"Genel adres", orDash(s.WAN.Hostname)},
		}
	}
	return v
}

// softwareView fills the Yazılım tab. d.mu TUTULARAK çağrılır.
func (a *App) softwareView(d *ServerDetail, s *model.Server, v *detailView) {
	dir := contentDir(s)
	kind := contentKind(s)
	content := "—"
	if dir != "" {
		content = contentKindTitle(s) + " · " + dir + "/"
	}
	v.info = [][2]string{
		{"Yazılım", softwareLabel(s.Software) + " " + s.MCVersion},
		{"İçerik", content},
	}
	if !canInstallContent(s) {
		v.note = noContentLines(s)
		v.loading, v.err = false, ""
		return
	}

	if d.swView == swResults {
		sr := d.search
		v.info = append(v.info, [2]string{"Arama", "“" + sr.query + "”"})
		v.loading, v.err = sr.busy, sr.err
		if !sr.done {
			return
		}
		v.header = searchHeader(sr.res, len(sr.res.Items))
		own := s.Software.ModrinthLoader()
		for _, it := range sr.res.Items {
			det := downloadsShort(it.Downloads) + " indirme"
			if it.Loader != "" && !strings.EqualFold(it.Loader, own) {
				det = it.Loader + " uyumlu · " + det
			}
			title := it.Title
			if title == "" {
				title = it.Slug
			}
			v.items = append(v.items, detailItem{label: title, detail: det,
				badge: kind, badgeKind: fbui.EventInfo, act: "result", value: it})
		}
		if len(v.items) == 0 && sr.err == "" {
			if sr.res.GameVersion != "" {
				v.items = append(v.items, detailItem{
					label: "Sürüm süzgecini kaldır ve yeniden ara", detail: "v",
					act: "search-any", accent: true})
				v.note = []string{"Birçok " + kind + " yeni Minecraft sürümleri için ayrıca etiketlenmez;",
					"süzgeçsiz aramada sunucunun sürüm çizgisine en yakın yapı seçilir."}
			} else {
				v.empty = "Sonuç yok. Başka bir ad deneyin (/)."
			}
		}
		return
	}

	v.items = append(v.items, detailItem{label: "Modrinth'te ara…", detail: "/", act: "search-new", accent: true})
	title := "KURULU EKLENTİLER"
	if kind == "mod" {
		title = "KURULU MODLAR"
	}
	v.header = fmt.Sprintf("%s (%d)", title, len(d.installed))
	for _, f := range d.installed {
		name := strings.TrimSuffix(f.Name, ".jar")
		badge, bk := kind, fbui.EventInfo
		if strings.HasSuffix(strings.ToLower(f.Name), ".disabled") {
			badge, bk = "devre dışı", fbui.EventWarn
		}
		v.items = append(v.items, detailItem{label: name, detail: bytesShort(uint64(f.Size)),
			badge: badge, badgeKind: bk, act: "installed", value: f})
	}
	if len(d.installed) == 0 && !v.loading {
		v.note = []string{"Henüz kurulu " + kind + " yok."}
	}
}

// ── Metin sığdırma ──────────────────────────────────────────────────────────

// fitText makes s safe to draw in maxCols cells.
//
// İki sorun, tek yer:
//
//  1. Modrinth başlıkları serbest metin: Çince, emoji, birleştirici işaretler.
//     Gömülü fontta olmayan glif u.Text'te KIRMIZI bir kutu olarak çizilir —
//     font hatası için doğru, ama bir eklentinin adında "hata" gibi görünür.
//     Eksik glif dizisi TEK bir "?" olur: görünür (bir şey olduğu belli) ama
//     satırı bozmaz. Emoji + değişken seçici gibi ardışık eksikler tek "?".
//  2. Uzun ad satırın sağındaki rozetin üstüne biner: "…" ile kırpılır.
func fitText(f *fbfont.Face, s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	out := make([]rune, 0, len(s))
	prevMissing := false
	for _, r := range s {
		if r == '\n' || r == '\t' || r == '\r' {
			r = ' '
		}
		if unicode.IsControl(r) {
			continue
		}
		if f != nil && r != ' ' && f.Glyph(r).Missing {
			if !prevMissing {
				out = append(out, '?')
			}
			prevMissing = true
			continue
		}
		prevMissing = false
		out = append(out, r)
	}
	if len(out) <= maxCols {
		return string(out)
	}
	if maxCols == 1 {
		return "…"
	}
	return strings.TrimRight(string(out[:maxCols-1]), " ") + "…"
}

// fit is fitText for a pixel width on the current UI.
func (a *App) fit(s string, maxW int) string {
	u := a.ui
	if u.F.CellW <= 0 {
		return s
	}
	return fitText(u.F, s, maxW/u.F.CellW)
}

// ── Çizim ───────────────────────────────────────────────────────────────────

// drawServerDetail paints the detail inside the content column.
//
// Bütün ölçüler u.F.CellW/CellH ve u.M'den türer: panel çalışırken çözünürlük
// değişebiliyor (display_live.go resizeCanvas) ve sabit piksel 1024x768'de
// taşar, 4K'da kaybolurdu.
func (a *App) drawServerDetail(r image.Rectangle, d *ServerDetail) {
	u := a.ui
	s := a.detailServer(d)
	title := "Sunucular"
	if s != nil {
		title = "Sunucular › " + s.Name
	}
	in := a.contentPanel(r, a.fit(title, r.Dx()-u.M.PadX*2))
	if s == nil {
		u.Text(in.Min.X, in.Min.Y, "Sunucu bulunamadı — Esc ile listeye dönün.", u.Pal.TextDim)
		return
	}

	// Başlık satırı: durum noktası, ad, yazılım, sağda durum rozeti.
	y := in.Min.Y
	kind, label, col := serverBadge(u, s)
	if kind == fbui.EventBusy {
		u.Spinner(in.Min.X, y, a.spinFrame(), col)
	} else {
		u.StatusDot(in.Min.X, y, col)
	}
	bw := u.TextWidth(label) + u.F.CellW*2
	nameX := in.Min.X + u.F.CellW + u.M.Gap
	nameMax := in.Max.X - bw - u.M.Gap*2 - nameX
	sub := softwareLabel(s.Software) + " " + s.MCVersion + " · :" + itoa(s.Port)
	name := a.fit(s.Name, nameMax/2)
	nx := u.Text(nameX, y, name, u.Pal.Text)
	u.Text(nx+u.M.Gap*2, y, a.fit(sub, nameMax-(nx-nameX)-u.M.Gap*2), u.Pal.TextDim)
	u.Badge(in.Max.X-bw, y-u.F.CellH/6, label, col)
	y += u.F.CellH + u.M.PadY + u.F.CellH/3

	y = a.drawDetailTabs(in, y, d)
	y += u.M.PadY

	body := image.Rect(in.Min.X, y, in.Max.X, in.Max.Y)
	if d.tab == dtConsole {
		a.drawDetailConsole(body, d, s)
		return
	}
	a.drawDetailView(body, d, a.detailViewFor(d, s))
}

// tabWindow picks which tabs fit, keeping the active one visible.
//
// Ayrı bir fonksiyon, çünkü sınanması gereken kural tam olarak bu: on iki
// sekme 1024 genişlikte tek satıra SIĞMAZ. Eski panel sığmayınca yalnızca
// etkin sekmeyi yazıyordu; burada etkin sekmenin çevresi gösterilir ve
// kenarlarda devamı olduğunu söyleyen oklar çizilir.
func tabWindow(widths []int, gap, avail, active int) (start, end int) {
	n := len(widths)
	total := 0
	for i, w := range widths {
		total += w
		if i > 0 {
			total += gap
		}
	}
	if total <= avail || n == 0 {
		return 0, n
	}
	start, end = active, active+1
	used := widths[active]
	for {
		grew := false
		if end < n && used+gap+widths[end] <= avail {
			used += gap + widths[end]
			end++
			grew = true
		}
		if start > 0 && used+gap+widths[start-1] <= avail {
			start--
			used += gap + widths[start]
			grew = true
		}
		if !grew {
			return start, end
		}
	}
}

func (a *App) drawDetailTabs(in image.Rectangle, y int, d *ServerDetail) int {
	u := a.ui
	h := u.F.CellH + u.F.CellH/2
	padX := u.F.CellW
	gap := u.F.CellW / 2
	if gap < 2 {
		gap = 2
	}
	arrow := u.F.CellW * 3
	widths := make([]int, dtCount)
	for i := detailTab(0); i < dtCount; i++ {
		widths[i] = u.TextWidth(i.Name()) + padX*2
	}
	start, end := tabWindow(widths, gap, in.Dx(), int(d.tab))
	if start > 0 || end < int(dtCount) {
		// Oklara yer aç ve pencereyi yeniden hesapla.
		start, end = tabWindow(widths, gap, in.Dx()-arrow*2, int(d.tab))
	}
	x := in.Min.X
	ty := y + (h-u.F.CellH)/2
	// Kenardaki oklar sekme hapı gibi bir zemin üstünde: çıplak küçük bir
	// üçgen 4K'da neredeyse görünmüyordu ve tıklanabilir olduğu belli değildi.
	arrowPill := func(ax, dir int, target int) {
		rc := image.Rect(ax, y, ax+arrow-gap, y+h)
		u.P.FillRoundRect(rect(rc), float64(h)/2, u.Pal.Raised)
		u.Chevron(rc.Min.X+(rc.Dx()-u.F.CellW)/2, ty, dir, u.Pal.Text)
		a.addShortcutZone(rc, fmt.Sprintf("detail-tab-%d", target))
	}
	if start > 0 {
		arrowPill(x, 1, start-1)
		x += arrow
	}
	for i := start; i < end; i++ {
		t := detailTab(i)
		rc := image.Rect(x, y, x+widths[i], y+h)
		if t == d.tab {
			u.P.FillRoundRect(rect(rc), float64(h)/2, u.Pal.Accent)
			u.TextCenter(rc.Min.X, rc.Max.X, ty, t.Name(), u.Pal.TextOn)
		} else {
			u.P.FillRoundRect(rect(rc), float64(h)/2, u.Pal.Raised)
			u.TextCenter(rc.Min.X, rc.Max.X, ty, t.Name(), u.Pal.TextDim)
		}
		a.addShortcutZone(rc, fmt.Sprintf("detail-tab-%d", i))
		x = rc.Max.X + gap
	}
	if end < int(dtCount) {
		arrowPill(in.Max.X-arrow+gap, 0, end)
	}
	y += h + u.M.PadY/2
	u.Divider(in.Min.X, in.Max.X, y)
	return y + u.M.PadY/2
}

// drawDetailView draws info lines, a note, the list and a footer.
func (a *App) drawDetailView(r image.Rectangle, d *ServerDetail, v detailView) {
	u := a.ui
	y := r.Min.Y
	lineH := u.F.CellH + u.M.PadY/2

	y = a.drawKV(r, y, v.info)

	if v.buttons {
		s := a.detailServer(d)
		y += u.M.PadY
		if s != nil && y+u.M.ButtonH <= r.Max.Y {
			startStyle, stopStyle := fbui.ButtonPrimary, fbui.ButtonSecondary
			if running(s) {
				startStyle, stopStyle = fbui.ButtonSecondary, fbui.ButtonPrimary
			}
			btns := []fbui.Btn{
				{Label: "Başlat", Key: "s", Style: startStyle},
				{Label: "Durdur", Key: "x", Style: stopStyle},
				{Label: "Yeniden başlat", Key: "r", Style: fbui.ButtonSecondary},
			}
			// Sığmıyorsa (çok dar ekran) düğmeler çizilmez; kısayollar alt
			// çubukta zaten yazılı.
			need := 0
			for i, b := range btns {
				if i > 0 {
					need += u.M.ButtonGap
				}
				need += u.ButtonWidth(b.Label, b.Key)
			}
			if need <= r.Dx() {
				rects := u.ButtonRow(r.Min.X, y, btns, -1)
				for i, rc := range rects {
					a.addShortcutZone(rc, btns[i].Key)
				}
				y += u.M.ButtonH + u.M.PadY
			}
		}
	}

	if len(v.note) > 0 {
		y += u.M.PadY / 2
		for _, n := range v.note {
			for _, ln := range u.WrapLines(n, r.Dx()) {
				if y+u.F.CellH > r.Max.Y {
					break
				}
				u.Text(r.Min.X, y, ln, u.Pal.TextDim)
				y += u.F.CellH
			}
		}
		y += u.M.PadY / 2
	}

	if v.err != "" {
		y += u.M.PadY / 2
		u.WarnTriangle(r.Min.X, y, u.Pal.Error)
		u.Text(r.Min.X+u.F.CellW+u.M.Gap, y, a.fit(v.err, r.Dx()-u.F.CellW-u.M.Gap), u.Pal.Error)
		y += lineH
	}

	// Alt bilgi (salt okunur alanlar) listenin ALTINA sabitlenir; liste
	// kalan yüksekliğe sığdırılır.
	footH := 0
	if len(v.foot) > 0 {
		footH = u.M.PadY*2 + u.F.CellH + len(v.foot)*lineH
	}
	listBottom := r.Max.Y - footH
	if footH > 0 && listBottom-y < u.M.RowH*3 {
		// Liste için yer yoksa alt bilgiden vazgeç: seçilebilir satırlar
		// salt okunur bilgiden önemli.
		footH, listBottom = 0, r.Max.Y
	}

	if v.header != "" && y+u.F.CellH <= listBottom {
		y += u.M.PadY / 2
		u.Text(r.Min.X, y, a.fit(v.header, r.Dx()), u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
	}

	if v.loading && len(v.items) == 0 {
		u.Spinner(r.Min.X, y, a.spinFrame(), u.Pal.Accent)
		u.Text(r.Min.X+u.F.CellW+u.M.Gap, y, "Yükleniyor…", u.Pal.TextDim)
		y += lineH
	} else if len(v.items) == 0 && v.empty != "" {
		u.Text(r.Min.X, y, a.fit(v.empty, r.Dx()), u.Pal.TextFaint)
		y += lineH
	}

	if len(v.items) > 0 {
		if v.header == "" && len(v.info) > 0 {
			// Bilgi satırlarıyla liste arasında nefes payı; yoksa ilk seçili
			// satırın vurgusu son bilgi satırına yapışıyordu.
			y += u.M.PadY / 2
		}
		y = a.drawDetailItems(image.Rect(r.Min.X, y, r.Max.X, listBottom), d, v.items, v.loading)
	}

	if footH > 0 {
		// Alt bilgi listenin HEMEN altına gelir; liste uzunsa alta
		// sabitlenmiş yerine iner. Her zaman alta yapıştırmak kısa bir
		// listeyle arasında yarım ekranlık boşluk bırakıyordu.
		fy := y + u.M.PadY
		if fy > r.Max.Y-footH+u.M.PadY {
			fy = r.Max.Y - footH + u.M.PadY
		}
		u.Divider(r.Min.X, r.Max.X, fy)
		fy += u.M.PadY
		u.Text(r.Min.X, fy, a.fit(v.footTitle, r.Dx()), u.Pal.TextFaint)
		fy += u.F.CellH + u.M.PadY/2
		a.drawKV(image.Rect(r.Min.X, fy, r.Max.X, r.Max.Y), fy, v.foot)
	}
}

// drawKV draws aligned label/value pairs, clipped to r.
func (a *App) drawKV(r image.Rectangle, y int, rows [][2]string) int {
	u := a.ui
	if len(rows) == 0 {
		return y
	}
	lineH := u.F.CellH + u.M.PadY/2
	keyCols := 0
	for _, kv := range rows {
		if n := len([]rune(kv[0])); n > keyCols {
			keyCols = n
		}
	}
	keyW := (keyCols + 2) * u.F.CellW
	if keyW > r.Dx()/2 {
		keyW = r.Dx() / 2
	}
	for _, kv := range rows {
		if y+u.F.CellH > r.Max.Y {
			break
		}
		u.Text(r.Min.X, y, a.fit(kv[0], keyW-u.F.CellW), u.Pal.TextDim)
		u.Text(r.Min.X+keyW, y, a.fit(kv[1], r.Dx()-keyW), u.Pal.Text)
		y += lineH
	}
	return y
}

// drawDetailItems draws the selectable list, windowed around the cursor.
//
// Returns the y just past the last drawn row.
func (a *App) drawDetailItems(r image.Rectangle, d *ServerDetail, items []detailItem, loading bool) int {
	u := a.ui
	// Liste satırı panelin genel satırından biraz yüksek: sağdaki rozet
	// metinden uzun ve üst üste binmemesi için satırda nefes payı ister.
	rowH := u.M.RowH + u.F.CellH/4
	maxRows := r.Dy() / rowH
	if maxRows < 1 {
		return r.Min.Y
	}
	cur := d.cursor[d.tab]
	if cur >= len(items) {
		cur = len(items) - 1
	}
	if cur < 0 {
		cur = 0
	}
	n := len(items)
	showPos := n > maxRows
	if showPos && maxRows > 1 {
		// Son satır "4-12 / 30" konum göstergesine ayrılır.
		maxRows--
	}
	start := 0
	if n > maxRows {
		start = cur - maxRows/2
		if start < 0 {
			start = 0
		}
		if start+maxRows > n {
			start = n - maxRows
		}
	}
	end := start + maxRows
	if end > n {
		end = n
	}

	// Rozetler ORTAK bir sütunda: her satır kendi rozet genişliği kadar sola
	// kaysaydı "boyut · tarih" ayrıntıları satırdan satıra hizasız dururdu
	// (2560x1440 Yedekler ekran görüntüsünde görüldü).
	badgeCol := 0
	for i := start; i < end; i++ {
		if b := items[i].badge; b != "" {
			if w := u.TextWidth(b) + u.F.CellW*2; w > badgeCol {
				badgeCol = w
			}
		}
	}

	y := r.Min.Y
	for i := start; i < end; i++ {
		it := items[i]
		row := image.Rect(r.Min.X-u.M.PadX/2, y, r.Max.X+u.M.PadX/2, y+rowH)
		a.addShortcutZone(row, fmt.Sprintf("detail-row-%d", i))
		cx, col := a.rowHighlight(row, i == cur)
		if it.accent && i != cur {
			col = u.Pal.Accent
		}
		if it.disabled {
			col = u.Pal.TextFaint
		}
		ty := y + (rowH-u.F.CellH)/2

		right := row.Max.X - u.M.PadX
		if it.badge != "" {
			_, bc := u.EventColors(it.badgeKind)
			if it.badgeKind == fbui.EventInfo {
				bc = u.Pal.TextDim
			}
			a.rowBadge(right, y, rowH, it.badge, bc)
		}
		if badgeCol > 0 {
			right -= badgeCol + u.M.Gap
		}
		labelMax := right - cx
		if it.detail != "" {
			// Ayrıntı en fazla satırın üçte biri; ad her zaman öncelikli.
			dw := u.TextWidth(it.detail)
			if lim := (right - cx) / 3; dw > lim {
				dw = lim
			}
			det := a.fit(it.detail, dw)
			dw = u.TextWidth(det)
			u.Text(right-dw, ty, det, u.Pal.TextDim)
			labelMax = right - dw - u.M.Gap - cx
		}
		u.Text(cx, ty, a.fit(it.label, labelMax), col)
		y += rowH
	}
	bottom := y
	if showPos {
		bottom = y + rowH
		pos := fmt.Sprintf("%d–%d / %d", start+1, end, n)
		u.TextRight(r.Max.X, y+(rowH-u.F.CellH)/2, pos, u.Pal.TextFaint)
		if start > 0 {
			u.Chevron(r.Min.X, y+(rowH-u.F.CellH)/2, 2, u.Pal.TextFaint)
		}
		if end < n {
			u.Chevron(r.Min.X+u.F.CellW*2, y+(rowH-u.F.CellH)/2, 3, u.Pal.TextFaint)
		}
	}
	if loading {
		// Yenileme sürerken eski liste görünür kalır; köşede küçük gösterge.
		u.Spinner(r.Max.X-u.F.CellW, r.Min.Y-u.F.CellH-u.M.PadY, a.spinFrame(), u.Pal.Accent)
	}
	return bottom
}

// rowBadge draws a pill that ENDS at right and fits inside one list row.
// Returns its width.
//
// u.Badge kullanılmaz: onun yüksekliği (CellH·4/3) satır yüksekliğine EŞİT,
// ardışık satırlardaki rozetler birbirine değip tek bir sütun gibi okunuyordu
// (1280x800 ekran görüntüsünde görüldü). Satırın içinde nefes payı bırakan
// daha alçak bir hap çiziyoruz.
func (a *App) rowBadge(right, y, rowH int, label string, c color.RGBA) int {
	u := a.ui
	h := u.F.CellH + u.F.CellH/6
	if h > rowH-2 {
		h = rowH - 2
	}
	margin := (rowH - h) / 2
	w := u.TextWidth(label) + u.F.CellW*2
	rc := fbdraw.R(float64(right-w), float64(y+margin), float64(w), float64(h))
	u.P.FillRoundRect(rc, float64(h)/2, fbdraw.Alpha(c, 0.18))
	u.P.StrokeRoundRect(rc, float64(h)/2, u.M.Stroke, fbdraw.Alpha(c, 0.8))
	u.TextCenter(right-w, right, y+(rowH-u.F.CellH)/2, label, c)
	return w
}

// consoleColor highlights warnings/errors like the old panel did.
func (a *App) consoleColor(line string) color.RGBA {
	u := a.ui
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "error") || strings.Contains(low, "exception") || strings.Contains(low, "severe"):
		return u.Pal.Error
	case strings.Contains(low, "warn"):
		return u.Pal.Warn
	case strings.Contains(low, "done ("):
		return u.Pal.OK
	}
	return u.Pal.Text
}

// drawDetailConsole draws the live console with a command hint underneath.
func (a *App) drawDetailConsole(r image.Rectangle, d *ServerDetail, s *model.Server) {
	u := a.ui
	hintH := u.F.CellH + u.M.PadY*2
	box := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-hintH)
	if box.Dy() < u.F.CellH*2 {
		box.Max.Y = r.Max.Y
		hintH = 0
	}
	u.P.FillRoundRect(fbdraw.R(float64(box.Min.X), float64(box.Min.Y), float64(box.Dx()), float64(box.Dy())),
		u.M.RadiusSmall, u.Pal.Bg)

	d.mu.Lock()
	lines := append([]string(nil), d.console...)
	errText := d.errs[dtConsole]
	d.mu.Unlock()

	padX := u.F.CellW
	padY := u.M.PadY / 2
	inner := image.Rect(box.Min.X+padX, box.Min.Y+padY, box.Max.X-padX, box.Max.Y-padY)
	rows := inner.Dy() / u.F.CellH
	if rows < 1 {
		rows = 1
	}
	back := d.consoleBack
	if back > len(lines) {
		back = len(lines)
	}
	endIdx := len(lines) - back
	startIdx := endIdx - rows
	if startIdx < 0 {
		startIdx = 0
	}
	y := inner.Min.Y
	if len(lines) == 0 {
		msg := "Henüz konsol çıktısı yok."
		if !running(s) {
			msg = "Sunucu kapalı — başlatınca çıktı burada akar (s)."
		}
		if a.offline() {
			msg = "mcosd bağlantısı yok."
		}
		u.Text(inner.Min.X, y, a.fit(msg, inner.Dx()), u.Pal.TextFaint)
	}
	for _, ln := range lines[startIdx:endIdx] {
		u.Text(inner.Min.X, y, a.fit(ln, inner.Dx()), a.consoleColor(ln))
		y += u.F.CellH
	}
	if back > 0 {
		tag := fmt.Sprintf("%d satır geride · End en alta", back)
		u.TextRight(inner.Max.X, inner.Min.Y, a.fit(tag, inner.Dx()/2), u.Pal.Warn)
	}
	if hintH == 0 {
		return
	}
	hy := box.Max.Y + u.M.PadY
	switch {
	case errText != "":
		u.Text(r.Min.X, hy, a.fit("Konsol okunamadı: "+errText, r.Dx()), u.Pal.Error)
	case running(s):
		x := u.Text(r.Min.X, hy, "Enter", u.Pal.Accent)
		u.Text(x+u.M.Gap, hy, a.fit("komut gönder   ↑↓ geçmişte kaydır", r.Dx()-(x-r.Min.X)-u.M.Gap), u.Pal.TextDim)
	default:
		u.Text(r.Min.X, hy, a.fit("Komut göndermek için sunucuyu başlatın (s).", r.Dx()), u.Pal.TextFaint)
	}
}
