package fbpanel

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
	"mcos/internal/model"
	"mcos/internal/version"
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
// Eski panelle AYNI yerleşim. Alt çubuk yeni.
//
// ── ODAK ────────────────────────────────────────────────────────────────────
// Kullanıcı "soldaki menüden sağa geçince hangisinin aktif olduğu belli
// olmuyor" dedi. Artık odak ÜÇ ayrı işaretle belli olur:
//
//  1. Odaklı sütunun paneli vurgu renginde ve KALIN çerçeveli.
//  2. Odaksız sütundaki seçili satır SOLUK vurgulanır (RowDimmed).
//  3. Ok işareti (chevron) yalnızca odaklı sütunda çizilir.
//
// Üçü birden: renk körlüğünde bile fark edilir.
func (a *App) Draw() {
	u := a.ui
	b := u.Bounds()

	// Tıklanabilir alanlar HER KARE yeniden kurulur. Eski kareden kalan bir
	// dikdörtgen, ekran değiştikten sonra yanlış eylemi tetiklerdi.
	a.resetZones()

	if a.Asleep() {
		// Uykuda hiçbir şey çizilmez: ekran tamamen siyah.
		u.P.Fill(b, color.RGBA{A: 255})
		return
	}

	// Kilit ekranı HER ŞEYİN ÖNÜNDE: parola girilmeden altındaki hiçbir
	// şey çizilmez. (Altını çizip üstüne pencere koymak, ekran görüntüsü
	// alan birine sistem durumunu sızdırırdı.)
	if a.Locked() {
		a.drawLock(b)
		if x, y, ok := a.PointerPos(); ok {
			u.Cursor(x, y)
		}
		return
	}

	// Sihirbazlar kendi düzenlerini çizer (kenar çubuğu yok).
	//
	// SIRA: ilk kurulum önce gelir. İkisi birden açıksa (olmamalı) ilk
	// kurulum daha temel olandır.
	if s := a.setupState(); s != nil {
		a.drawSetup(s, b)
		return
	}
	if w := a.wizardState(); w != nil {
		a.drawWizard(w, b)
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
	//
	// Perde bir kez hesaplanıp saklanır: 1080p bulanıklık ~50 ms sürüyor,
	// her karede yapmak arayüzü dondururdu.
	if m := a.ActiveModal(); m != nil {
		if !a.scrim.Restore(u) {
			a.scrim.Capture(u)
			a.scrim.Restore(u)
		}
		cols, rows := m.Size()
		in := u.Modal(cols*u.F.CellW, rows*u.F.CellH, m.Title())
		m.Draw(a, in)
	}

	keys := a.shortcuts()
	_, caps := u.StatusBar(a.LastEvent(), keys, a.spinFrame())
	// Kısayol kapakları TIKLANABİLİR: fareyle çalışan bir kullanıcı,
	// klavyedeki karşılığını bilmeden aynı eylemi yapabilmeli.
	for i := range keys {
		if i < len(caps) {
			a.addShortcutZone(caps[i], shortcutKeyFor(keys[i].Key))
		}
	}

	// Geçiş, imleçten ÖNCE uygulanır: imleç karıştırılırsa hayalet bırakır.
	a.applyTransition(main)

	if x, y, ok := a.PointerPos(); ok {
		if a.busy() {
			u.CursorBusy(x, y, a.spinFrame())
		} else {
			u.Cursor(x, y)
		}
	}
}

// shortcutKeyFor maps a status-bar key cap label to the keystroke it sends.
//
// Kapaklarda "↑↓" gibi görsel etiketler var; bunları olduğu gibi tuş olarak
// göndermek hiçbir şey yapmaz. Eşleme burada, tek yerde.
func shortcutKeyFor(label string) string {
	switch label {
	case "Enter":
		return "enter"
	case "Esc":
		return "esc"
	case "↑↓":
		return "down"
	case "F12":
		return "f12"
	}
	// Tek harfli kısayollar ("n", "w", "e", "r", "t", "g") doğrudan geçer.
	if len([]rune(label)) == 1 {
		return label
	}
	return ""
}

// busy reports whether a long-running action is in progress.
func (a *App) busy() bool {
	if a.scanning() {
		return true
	}
	e := a.LastEvent()
	return e != nil && e.Kind == fbui.EventBusy
}

// contentFocused reports whether the keyboard is in the content column.
func (a *App) contentFocused() bool { return a.Focus() == FocusContent }

// sidebarWidth scales with the font so the labels always fit.
func (a *App) sidebarWidth() int {
	longest := 0
	for _, n := range sectionNames {
		if r := len([]rune(n)); r > longest {
			longest = r
		}
	}
	// ad + ok işareti + rozet payı + iki yandan dolgu
	return (longest+5)*a.ui.F.CellW + a.ui.M.PadX*2
}

func (a *App) spinFrame() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.spin
}

// drawSidebar paints the navigation column.
func (a *App) drawSidebar(r image.Rectangle) {
	u := a.ui
	focused := !a.contentFocused()
	in := u.Panel(r, "", focused)

	st, _, _ := a.Snapshot()

	y := in.Min.Y
	x := u.Text(in.Min.X, y, "MCOS", u.Pal.Accent)
	ver := version.Display()
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
		ty := y + (u.M.RowH-u.F.CellH)/2

		// Tıklama alanı, çizilen satırın AYNISI.
		a.addZone(row, zoneSidebar, int(s))

		var cx int
		col := u.Pal.TextDim
		switch {
		case s == cur && focused:
			cx = u.Row(row, true)
			col = u.Pal.Accent
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		case s == cur:
			// Bu bölümdeyiz ama klavye sağda: soluk vurgulama.
			cx = u.RowDimmed(row)
			col = u.Pal.Text
		default:
			// Fare üzerindeyse ÜÇÜNCÜ bir görünüm: seçiliden soluk,
			// normalden belirgin. Aynı görünüm olsaydı kullanıcı
			// Enter'a bastığında hangisinin çalışacağını bilemezdi.
			if a.hoverSidebar(s) {
				u.HoverRow(row)
				col = u.Pal.Text
			}
			cx = u.Row(row, false)
		}
		u.Text(cx, ty, s.Name(), col)

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
		if len(servers) == 0 {
			return "", u.Pal.TextFaint
		}
		up := 0
		for _, sv := range servers {
			if sv.State == model.StateRunning {
				up++
			}
		}
		c := u.Pal.TextFaint
		if up > 0 {
			c = u.Pal.OK
		}
		return fmt.Sprintf("%d/%d", up, len(servers)), c
	case SecNetwork:
		if st != nil && !st.Net.Internet {
			return "yok", u.Pal.Warn
		}
	case SecSoftware:
		if st != nil {
			return itoa(len(st.JavaVersions)), u.Pal.TextFaint
		}
	case SecPeers:
		if st != nil && st.PeersOnline > 0 {
			return itoa(st.PeersOnline), u.Pal.OK
		}
	case SecTunnel:
		a.mu.Lock()
		n := 0
		for _, t := range a.tunnels {
			if t.Running {
				n++
			}
		}
		a.mu.Unlock()
		if n > 0 {
			return "açık", u.Pal.OK
		}
	}
	return "", u.Pal.TextFaint
}

// contentPanel draws the section frame with the right focus styling.
//
// TÜM bölümler bunu kullanır; odak görünümü tek yerde tanımlı olsun diye.
func (a *App) contentPanel(r image.Rectangle, title string) image.Rectangle {
	return a.ui.Panel(r, title, a.contentFocused())
}

// drawContent dispatches to the per-section renderer.
func (a *App) drawContent(r image.Rectangle) {
	switch a.Section() {
	case SecDashboard:
		a.drawDashboard(r)
	case SecServers:
		a.drawServers(r)
	case SecUSB:
		a.drawUSB(r)
	case SecSoftware:
		a.drawSoftware(r)
	case SecPerformance:
		a.drawPerformance(r)
	case SecDevices:
		a.drawDevices(r)
	case SecNetwork:
		a.drawNetwork(r)
	case SecDisplay:
		a.drawDisplay(r)
	case SecTunnel:
		a.drawTunnel(r)
	case SecPeers:
		a.drawPeers(r)
	case SecPower:
		a.drawPower(r)
	case SecSettings:
		a.drawSettings(r)
	}
}

// ── Ortak yardımcılar ───────────────────────────────────────────────────────

// waiting draws the "still connecting" placeholder.
func (a *App) waiting(in image.Rectangle) {
	a.ui.Text(in.Min.X, in.Min.Y, "Daemon'a bağlanılıyor…", a.ui.Pal.TextDim)
}

// kvList draws aligned key/value lines and returns the new y.
func (a *App) kvList(in image.Rectangle, y int, keyCols int,
	rows [][3]any) int {
	u := a.ui
	for _, kv := range rows {
		k, _ := kv[0].(string)
		v, _ := kv[1].(string)
		c, ok := kv[2].(color.RGBA)
		if !ok {
			c = u.Pal.Text
		}
		if v == "" {
			continue
		}
		u.Text(in.Min.X, y, k, u.Pal.TextDim)
		u.Text(in.Min.X+u.F.CellW*keyCols, y, v, c)
		y += u.F.CellH + u.M.PadY/2
	}
	return y
}

// sectionHint draws a dim hint line at a given y.
func (a *App) hint(in image.Rectangle, y int, lines ...string) int {
	u := a.ui
	for _, l := range lines {
		u.Text(in.Min.X, y, l, u.Pal.TextFaint)
		y += u.F.CellH
	}
	return y
}

// selectableRows draws a list of rows and highlights the cursor, honouring focus.
//
// Bir bölümdeki listeler bunu kullanır: imleç görünümü ve odak davranışı
// her ekranda AYNI olsun diye.
func (a *App) rowHighlight(row image.Rectangle, selected bool) (int, color.RGBA) {
	u := a.ui
	switch {
	case selected && a.contentFocused():
		return u.Row(row, true), u.Pal.Accent
	case selected:
		return u.RowDimmed(row), u.Pal.Text
	default:
		return u.Row(row, false), u.Pal.Text
	}
}

// contentRow draws a selectable content row AND registers it for the mouse.
//
// TÜM içerik listeleri bunu kullanır. Tek bir yerde olması, bir ekranın
// yanlışlıkla tıklanamaz satırlar çizmesini imkânsız kılar — ki fare
// desteği eklenen bir arayüzde en sık yapılan hata budur.
func (a *App) contentRow(row image.Rectangle, idx int) (int, color.RGBA) {
	a.addZone(row, zoneRow, idx)
	selected := a.Cursor() == idx
	if !selected && a.hoverRow(idx) {
		a.ui.HoverRow(row)
	}
	return a.rowHighlight(row, selected)
}

// ── Kısayollar ──────────────────────────────────────────────────────────────

// shortcuts returns the key hints for the current screen.
func (a *App) shortcuts() []fbui.Shortcut {
	if a.ActiveModal() != nil {
		return []fbui.Shortcut{
			{Key: "↑↓", Label: "Gezin"},
			{Key: "Enter", Label: "Seç"},
			{Key: "Esc", Label: "Kapat"},
		}
	}
	if !a.contentFocused() {
		return []fbui.Shortcut{
			{Key: "↑↓", Label: "Menü"},
			{Key: "Enter", Label: "Aç"},
			{Key: "g", Label: "Güç"},
		}
	}
	switch a.Section() {
	case SecServers:
		return []fbui.Shortcut{
			{Key: "Enter", Label: "Başlat/Durdur"},
			{Key: "n", Label: "Yeni sunucu"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecSoftware:
		return []fbui.Shortcut{
			{Key: "Enter", Label: "Java kur"},
			{Key: "r", Label: "Yenile"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecDisplay:
		return []fbui.Shortcut{
			{Key: "Enter", Label: "Değiştir"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecNetwork:
		return []fbui.Shortcut{
			{Key: "w", Label: "Wi-Fi"},
			{Key: "e", Label: "Kablolu"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecPerformance:
		return []fbui.Shortcut{
			{Key: "t", Label: "Turbo"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecUSB:
		return []fbui.Shortcut{
			{Key: "r", Label: "Tara"},
			{Key: "Enter", Label: "Kur"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecPeers:
		return []fbui.Shortcut{
			{Key: "Enter", Label: "Seç"},
			{Key: "s", Label: "Tara"},
			{Key: "i", Label: "IP gir"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecTunnel:
		return []fbui.Shortcut{
			{Key: "Enter", Label: "Adımı çalıştır"},
			{Key: "r", Label: "Yenile"},
			{Key: "Esc", Label: "Menü"},
		}
	case SecSettings:
		return []fbui.Shortcut{
			{Key: "Enter", Label: "Değiştir"},
			{Key: "Esc", Label: "Menü"},
		}
	}
	return []fbui.Shortcut{
		{Key: "↑↓", Label: "Gezin"},
		{Key: "Enter", Label: "Seç"},
		{Key: "Esc", Label: "Menü"},
	}
}

// ── Biçimlendirme yardımcıları ──────────────────────────────────────────────

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
