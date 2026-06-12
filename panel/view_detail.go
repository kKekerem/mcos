package panel

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/sysmon"
	"mcos/panel/theme"
)

// Server-panel sub-tabs (Aternos-like).
var detailTabs = []string{
	"Genel", "Konsol", "Ayarlar", "Oyuncular", "Yazılım",
	"Dosyalar", "Dünyalar", "Yedekler", "Erişim", "İnternete Aç", "Performans", "Ağ",
}

const (
	tabGeneral = iota
	tabConsole
	tabSettings
	tabPlayers
	tabSoftware
	tabFiles
	tabWorlds
	tabBackups
	tabAccess
	tabWAN
	tabPerformance
	tabNetwork
)

type detailModel struct {
	th     *theme.Theme
	id     string
	server *model.Server

	tab           int
	console       []string
	consoleCursor int64
	input         textinput.Model
	inputFocused  bool

	// M6 data caches
	backups []model.Backup
	files   []model.FileEntry
	worlds  []model.World
	players ipc.PlayersListResult
	m6Err   string

	// Players tab selection.
	playerCursor int

	// Software tab: Modrinth catalog browser.
	catalogInput   textinput.Model
	catalogFocused bool
	catalogResults []ipc.CatalogItem
	catalogCursor  int
	catalogNote    string

	// Settings edit mode
	settingCursor int
	settingMode   string // e.g. "ram", "view", "sim"
	settingInput textinput.Model
}

func newDetail(th *theme.Theme, id string) *detailModel {
	ti := textinput.New()
	ti.Placeholder = "konsol komutu (örn: say merhaba)"
	ti.Prompt = "> "
	ti.CharLimit = 256
	cat := textinput.New()
	cat.Placeholder = "mod/plugin ara (örn: worldedit)"
	cat.Prompt = "ara: "
	cat.CharLimit = 64
	set := textinput.New()
	set.Prompt = "Yeni değer: "
	return &detailModel{th: th, id: id, input: ti, catalogInput: cat, settingInput: set}
}

// handleKey processes a key; returns back=true to close the detail view.
func (d *detailModel) handleKey(m tea.KeyMsg, cl *Client) (bool, tea.Cmd) {
	// Console input mode captures most keys.
	if d.inputFocused {
		switch m.String() {
		case "esc":
			d.inputFocused = false
			d.input.Blur()
			return false, nil
		case "enter":
			cmd := strings.TrimSpace(d.input.Value())
			d.input.SetValue("")
			if cmd == "" {
				return false, nil
			}
			return false, doCommand(cl, d.id, cmd)
		default:
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(m)
			return false, cmd
		}
	}

	// Catalog search input mode (Software tab).
	if d.catalogFocused {
		switch m.String() {
		case "esc":
			d.catalogFocused = false
			d.catalogInput.Blur()
			return false, nil
		case "enter":
			q := strings.TrimSpace(d.catalogInput.Value())
			d.catalogFocused = false
			d.catalogInput.Blur()
			if q == "" {
				return false, nil
			}
			d.catalogNote = "aranıyor…"
			return false, doCatalogSearch(cl, d.id, q)
		default:
			var cmd tea.Cmd
			d.catalogInput, cmd = d.catalogInput.Update(m)
			return false, cmd
		}
	}

	// Settings input mode
	if d.settingMode != "" {
		switch m.String() {
		case "esc":
			d.settingMode = ""
			d.settingInput.Blur()
			return false, nil
		case "enter":
			val := strings.TrimSpace(d.settingInput.Value())
			mode := d.settingMode
			d.settingMode = ""
			d.settingInput.Blur()
			if val == "" {
				return false, nil
			}
			return false, func() tea.Msg {
				p := ipc.ServerUpdateParams{ID: d.id}
				if mode == "ram" {
					if v, _ := strconv.Atoi(val); v > 0 { p.RAMMB = v }
				} else if mode == "view" {
					if v, _ := strconv.Atoi(val); v > 0 { p.ViewDistance = v }
				} else if mode == "sim" {
					if v, _ := strconv.Atoi(val); v > 0 { p.SimDistance = v }
				} else if mode == "maxplayers" {
					if v, _ := strconv.Atoi(val); v > 0 { p.MaxPlayers = v }
				}
				if _, err := cl.UpdateServer(p); err != nil {
					return errMsg{err}
				}
				return actionMsg{msg: "ayar güncellendi"}
			}
		default:
			var cmd tea.Cmd
			d.settingInput, cmd = d.settingInput.Update(m)
			return false, cmd
		}
	}

	// Tab-specific navigation/actions before global keys.
	switch d.tab {
	case tabPlayers:
		if cmd, handled := d.playersKey(m, cl); handled {
			return false, cmd
		}
	case tabSoftware:
		if cmd, handled := d.softwareKey(m, cl); handled {
			return false, cmd
		}
	case tabWAN:
		switch m.String() {
		case "t", " ":
			return false, func() tea.Msg {
				en := !d.server.WAN.Enabled
				_, _ = cl.UpdateServer(ipc.ServerUpdateParams{ID: d.id, WAN: &en})
				return actionMsg{msg: "WAN erişimi güncellendi"}
			}
		}
	case tabSettings:
		if d.settingMode == "" {
			switch m.String() {
			case "up", "k":
				d.settingCursor = (d.settingCursor - 1 + 7) % 7
				return false, nil
			case "down", "j":
				d.settingCursor = (d.settingCursor + 1) % 7
				return false, nil
			case "enter":
				switch d.settingCursor {
				case 0:
					d.settingMode = "ram"
					d.settingInput.Prompt = "Yeni RAM (MB): "
					return false, d.settingInput.Focus()
				case 1:
					d.settingMode = "view"
					d.settingInput.Prompt = "Görüş Uzaklığı: "
					return false, d.settingInput.Focus()
				case 2:
					d.settingMode = "sim"
					d.settingInput.Prompt = "Simülasyon Uzaklığı: "
					return false, d.settingInput.Focus()
				case 3:
					d.settingMode = "maxplayers"
					d.settingInput.Prompt = "Max Oyuncu: "
					return false, d.settingInput.Focus()
				case 4:
					return false, func() tea.Msg {
						fp := !d.server.FullPerf
						_, _ = cl.UpdateServer(ipc.ServerUpdateParams{ID: d.id, FullPerf: &fp})
						return actionMsg{msg: "performans güncellendi"}
					}
				case 5:
					return false, func() tea.Msg {
						as := !d.server.Autostart
						_, _ = cl.UpdateServer(ipc.ServerUpdateParams{ID: d.id, Autostart: &as})
						return actionMsg{msg: "oto-başlat güncellendi"}
					}
				case 6:
					return false, func() tea.Msg {
						en := !d.server.WAN.Enabled
						_, _ = cl.UpdateServer(ipc.ServerUpdateParams{ID: d.id, WAN: &en})
						return actionMsg{msg: "WAN erişimi güncellendi"}
					}
				}
			}
		}
	}

	switch m.String() {
	case "esc", "b", "backspace":
		return true, nil
	case "left", "h", "[", "shift+tab":
		d.tab = (d.tab - 1 + len(detailTabs)) % len(detailTabs)
		return false, d.onTabEnter(cl)
	case "right", "l", "]", "tab":
		d.tab = (d.tab + 1) % len(detailTabs)
		return false, d.onTabEnter(cl)
	case "i":
		if d.tab == tabConsole {
			d.inputFocused = true
			return false, d.input.Focus()
		}
	case "c", "C":
		if d.tab == tabBackups {
			return false, doBackupCreate(cl, d.id)
		}
	case "s", "S":
		return false, doAction(cl, actStart, d.id)
	case "x", "X":
		return false, doAction(cl, actStop, d.id)
	case "r", "R":
		return false, doAction(cl, actRestart, d.id)
	}
	return false, nil
}

// playersKey handles selection + moderation actions on the Players tab.
func (d *detailModel) playersKey(m tea.KeyMsg, cl *Client) (tea.Cmd, bool) {
	n := len(d.players.Players)
	switch m.String() {
	case "up", "k":
		if n > 0 {
			d.playerCursor = (d.playerCursor - 1 + n) % n
		}
		return nil, true
	case "down", "j":
		if n > 0 {
			d.playerCursor = (d.playerCursor + 1) % n
		}
		return nil, true
	}
	if n == 0 || d.playerCursor >= n {
		return nil, false
	}
	name := d.players.Players[d.playerCursor].Name
	switch m.String() {
	case "o":
		return doPlayerCmd(cl, d.id, "op "+name), true
	case "d":
		return doPlayerCmd(cl, d.id, "deop "+name), true
	case "K":
		return doPlayerCmd(cl, d.id, "kick "+name), true
	case "B":
		return doPlayerCmd(cl, d.id, "ban "+name), true
	case "u":
		return doPlayerCmd(cl, d.id, "pardon "+name), true
	case "w":
		return doPlayerCmd(cl, d.id, "whitelist add "+name), true
	case "W":
		return doPlayerCmd(cl, d.id, "whitelist remove "+name), true
	}
	return nil, false
}

// softwareKey handles the catalog browser on the Software tab.
func (d *detailModel) softwareKey(m tea.KeyMsg, cl *Client) (tea.Cmd, bool) {
	switch m.String() {
	case "/":
		d.catalogFocused = true
		return d.catalogInput.Focus(), true
	case "up", "k":
		if len(d.catalogResults) > 0 {
			d.catalogCursor = (d.catalogCursor - 1 + len(d.catalogResults)) % len(d.catalogResults)
		}
		return nil, true
	case "down", "j":
		if len(d.catalogResults) > 0 {
			d.catalogCursor = (d.catalogCursor + 1) % len(d.catalogResults)
		}
		return nil, true
	case "enter":
		if d.catalogCursor < len(d.catalogResults) {
			slug := d.catalogResults[d.catalogCursor].Slug
			d.catalogNote = "kuruluyor: " + slug
			return doCatalogInstall(cl, d.id, slug), true
		}
	}
	return nil, false
}

// onTabEnter kicks console polling when the console tab is opened, and fetches
// M6 data when entering backup/files/worlds/players tabs.
func (d *detailModel) onTabEnter(cl *Client) tea.Cmd {
	switch d.tab {
	case tabConsole:
		return tea.Batch(fetchConsole(cl, d.id, d.consoleCursor), consoleTick(d.id))
	case tabBackups:
		return fetchBackups(cl, d.id)
	case tabFiles:
		return fetchFiles(cl, d.id, ".")
	case tabWorlds:
		return fetchWorlds(cl, d.id)
	case tabPlayers:
		return fetchPlayers(cl, d.id)
	}
	return nil
}

// update handles async messages addressed to the detail view.
func (d *detailModel) update(msg tea.Msg, cl *Client) tea.Cmd {
	switch m := msg.(type) {
	case consoleMsg:
		if m.id != d.id {
			return nil
		}
		for _, ln := range m.lines {
			d.console = append(d.console, ln.Text)
		}
		if len(d.console) > 500 {
			d.console = d.console[len(d.console)-500:]
		}
		d.consoleCursor = m.cursor
		return nil
	case consolePollMsg:
		if m.id != d.id || d.tab != tabConsole {
			return nil
		}
		return tea.Batch(fetchConsole(cl, d.id, d.consoleCursor), consoleTick(d.id))
	case backupsMsg:
		if m.id == d.id {
			d.backups = m.backups
			d.m6Err = ""
			if m.err != nil {
				d.m6Err = m.err.Error()
			}
		}
		return nil
	case filesMsg:
		if m.id == d.id {
			d.files = m.entries
			d.m6Err = ""
			if m.err != nil {
				d.m6Err = m.err.Error()
			}
		}
		return nil
	case worldsMsg:
		if m.id == d.id {
			d.worlds = m.worlds
			d.m6Err = ""
			if m.err != nil {
				d.m6Err = m.err.Error()
			}
		}
		return nil
	case playersMsg:
		if m.id == d.id {
			d.players = m.result
			if d.playerCursor >= len(d.players.Players) {
				d.playerCursor = 0
			}
			d.m6Err = ""
			if m.err != nil {
				d.m6Err = m.err.Error()
			}
		}
		return nil
	case catalogMsg:
		if m.id == d.id {
			d.catalogResults = m.items
			d.catalogCursor = 0
			if m.err != nil {
				d.catalogNote = "⚠ " + m.err.Error()
			} else {
				d.catalogNote = fmt.Sprintf("%d sonuç (↑↓ seç · enter kur)", len(m.items))
			}
		}
		return nil
	}
	return nil
}

func (d *detailModel) view(w, h int) string {
	th := d.th
	if d.server == nil {
		return th.Muted.Render("sunucu yükleniyor…")
	}
	s := d.server

	// Header: name + state + meta.
	header := th.Title.Render(s.Name) + "  " + stateBadge(th, s.State) +
		th.Muted.Render(fmt.Sprintf("   %s · MC %s · :%d", s.Software, s.MCVersion, s.Port))

	tabBar := d.renderTabBar(w)

	bodyH := h - 6
	if bodyH < 3 {
		bodyH = 3
	}
	body := d.tabBody(w, bodyH)

	footer := th.Muted.Render("sol/sağ: sekme · s başlat · x durdur · r yeniden · Esc geri")
	inner := lipgloss.JoinVertical(lipgloss.Left, header, "", tabBar, "", body, "", footer)
	return clampLines(inner, h)
}

// renderTabBar shows every tab when they fit on one line, otherwise a compact
// "Tab (i/N · sol/sağ)" pill so the bar never wraps and shifts the layout.
func (d *detailModel) renderTabBar(w int) string {
	th := d.th
	parts := make([]string, len(detailTabs))
	for i, name := range detailTabs {
		if i == d.tab {
			parts[i] = th.MenuActive.Render(" " + name + " ")
		} else {
			parts[i] = th.MenuItem.Render(name)
		}
	}
	joined := strings.Join(parts, " ")
	if lipgloss.Width(joined) <= w {
		return joined
	}
	return th.MenuActive.Render(" "+detailTabs[d.tab]+" ") + " " +
		th.Muted.Render(fmt.Sprintf("(%d/%d · sol/sağ)", d.tab+1, len(detailTabs)))
}

// tabBody renders the active tab, hard-clamped to h lines so an over-long tab
// (long console, big file list) can never spill past the pane.
func (d *detailModel) tabBody(w, h int) string {
	return clampLines(d.tabContent(w, h), h)
}

func (d *detailModel) tabContent(w, h int) string {
	th := d.th
	s := d.server
	switch d.tab {
	case tabGeneral:
		return strings.Join([]string{
			kv(th, "Durum", string(s.State)),
			kv(th, "Yazılım", string(s.Software)),
			kv(th, "Sürüm", s.MCVersion),
			kv(th, "Java", itoa(s.JavaMajor)),
			kv(th, "RAM", fmt.Sprintf("%d MB", s.RAMMB)),
			kv(th, "Port", itoa(s.Port)),
			kv(th, "Oyuncu", fmt.Sprintf("%d / %d", s.Players, orZero(s.MaxPlayers, 20))),
			kv(th, "Çalışma", fmtUptime(s.UptimeSec)),
			kv(th, "PID", itoa(s.PID)),
			kv(th, "Oto-başlat", yesno(s.Autostart)),
			"",
			th.Muted.Render("» " + truncate(orDash(s.LastLog), w-4)),
		}, "\n")
	case tabConsole:
		return d.consoleView(w, h)
	case tabSettings:
		var out []string
		
		mkField := func(idx int, label, val string) string {
			if idx == d.settingCursor {
				return th.Accent.Render(" ▶ ") + th.MenuActive.Render(fmt.Sprintf(" %-20s : %s ", label, val))
			}
			return "   " + th.Val.Render(fmt.Sprintf("%-20s", label)) + th.Muted.Render(" : ") + th.Key.Render(val)
		}

		out = append(out,
			mkField(0, "RAM Limiti", fmt.Sprintf("%d MB", s.RAMMB)),
			mkField(1, "Görüş Uzaklığı", itoa(s.ViewDistance)),
			mkField(2, "Simülasyon Uzaklığı", itoa(s.SimDistance)),
			mkField(3, "Maksimum Oyuncu", itoa(s.MaxPlayers)),
			mkField(4, "Tam Performans", yesno(s.FullPerf)),
			mkField(5, "Otomatik Başlat", yesno(s.Autostart)),
			mkField(6, "Dışarı Aç (WAN)", yesno(s.WAN.Enabled)),
			"",
		)
		if d.settingMode != "" {
			out = append(out, d.settingInput.View())
		} else {
			out = append(out, th.Muted.Render("↑/↓: Gezin  ·  Enter: Düzenle / Değiştir"))
		}
		return strings.Join(out, "\n")
	case tabSoftware:
		return d.softwareView(w, h)
	case tabPerformance:
		return strings.Join([]string{
			kv(th, "RAM limiti", fmt.Sprintf("%d MB", s.RAMMB)),
			kv(th, "CPU kota", ifZero(s.CPUQuota, "sınırsız", fmt.Sprintf("%d%%", s.CPUQuota))),
			kv(th, "Tam perf.", yesno(s.FullPerf)),
			kv(th, "Çalışma", fmtUptime(s.UptimeSec)),
		}, "\n")
	case tabNetwork:
		return strings.Join([]string{
			kv(th, "Sunucu IP (Yerel)", sysmon.Net().LocalIP),
			kv(th, "Port", itoa(s.Port)),
			kv(th, "WAN (Serveo)", yesno(s.WAN.Enabled)),
		}, "\n")
	case tabWAN:
		lines := []string{
			kv(th, "İnternete açık (WAN)", yesno(s.WAN.Enabled)),
		}
		if s.WAN.Hostname != "" {
			lines = append(lines, kv(th, "Genel adres", s.WAN.Hostname))
		}
		lines = append(lines,
			"",
			th.Muted.Render("WAN açıkken sunucu, ssh ile serveo.net üzerinden internete açılır."),
			th.Muted.Render("Adres sunucu başlayınca burada belirir."),
			"",
			th.Muted.Render("[t] Aç/Kapat"),
		)
		return strings.Join(lines, "\n")
	case tabPlayers:
		return d.playersView(w, h)
	case tabFiles:
		return d.filesView(w, h)
	case tabWorlds:
		return d.worldsView(w, h)
	case tabBackups:
		return d.backupsView(w, h)
	case tabAccess:
		return th.Muted.Render("Erişim (whitelist/ops/ban) sonraki sürümde.")
	default:
		return ""
	}
}

func (d *detailModel) consoleView(w, h int) string {
	th := d.th
	lines := d.console
	maxLines := h - 2
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	rendered := make([]string, 0, len(lines))
	for _, ln := range lines {
		rendered = append(rendered, colorConsoleLine(th, truncate(ln, w-1)))
	}
	for len(rendered) < maxLines {
		rendered = append(rendered, "")
	}
	logBox := strings.Join(rendered, "\n")

	var inputLine string
	if d.inputFocused {
		inputLine = d.input.View()
	} else {
		inputLine = th.Muted.Render("komut göndermek için 'i'")
	}
	return lipgloss.JoinVertical(lipgloss.Left, logBox, th.Muted.Render(strings.Repeat("─", w-1)), inputLine)
}

// colorConsoleLine highlights warnings/errors in Minecraft console output.
func colorConsoleLine(th *theme.Theme, line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "error") || strings.Contains(low, "exception") || strings.Contains(low, "severe"):
		return lipgloss.NewStyle().Foreground(th.P.Red).Render(line)
	case strings.Contains(low, "warn"):
		return lipgloss.NewStyle().Foreground(th.P.Yellow).Render(line)
	case strings.Contains(low, "done ("):
		return lipgloss.NewStyle().Foreground(th.P.Green).Render(line)
	default:
		return th.Val.Render(line)
	}
}

func ifZero(v int, zero, nonzero string) string {
	if v == 0 {
		return zero
	}
	return nonzero
}

func (d *detailModel) playersView(w, h int) string {
	th := d.th
	if d.m6Err != "" {
		return th.Muted.Render("! " + d.m6Err)
	}
	p := d.players
	head := kv(th, "Online", fmt.Sprintf("%d / %d", p.Online, p.Max))
	help := th.Muted.Render("yukarı/aşağı seç · o op · d deop · K at · B yasakla · u affet · w/W whitelist +/-")
	if len(p.Players) == 0 {
		return head + "\n\n" + th.Muted.Render("(çevrimiçi oyuncu yok — sunucu çalışıyor olmalı)") + "\n\n" + help
	}
	rows := make([]string, len(p.Players))
	for i, pl := range p.Players {
		if i == d.playerCursor {
			rows[i] = th.Accent.Render("» " + pl.Name)
		} else {
			rows[i] = "  " + th.Val.Render(pl.Name)
		}
	}
	listH := h - 4
	if listH < 1 {
		listH = 1
	}
	return head + "\n\n" + scrollList(th, rows, d.playerCursor, listH) + "\n" + help
}

func (d *detailModel) softwareView(w, h int) string {
	th := d.th
	s := d.server
	var out []string
	out = append(out,
		kv(th, "Tür", string(s.Software)),
		kv(th, "Sürüm", s.MCVersion),
		kv(th, "Java", itoa(s.JavaMajor)),
		kv(th, "Plugin/Mod", yesno(s.SupportsPlugins)+" / "+yesno(s.SupportsMods)),
		"",
	)
	// Catalog search box.
	if d.catalogFocused {
		out = append(out, d.catalogInput.View())
	} else {
		out = append(out, th.Muted.Render("'/' ile mod/plugin ara (Modrinth)"))
	}
	if d.catalogNote != "" {
		out = append(out, th.Muted.Render(d.catalogNote))
	}
	out = append(out, "")
	headStr := strings.Join(out, "\n")
	if len(d.catalogResults) == 0 {
		return headStr + "\n" + th.Muted.Render("(sonuç yok — '/' ile arama yapın)")
	}
	rows := make([]string, len(d.catalogResults))
	for i, it := range d.catalogResults {
		mark := "  "
		title := th.Val.Render(it.Title)
		if i == d.catalogCursor {
			mark = th.Accent.Render("» ")
			title = th.Accent.Render(it.Title)
		}
		rows[i] = fmt.Sprintf("%s%s %s", mark, title, th.Muted.Render(fmt.Sprintf("(%d indirme)", it.Downloads)))
	}
	listH := h - len(out)
	if listH < 1 {
		listH = 1
	}
	return headStr + "\n" + scrollList(th, rows, d.catalogCursor, listH)
}

func (d *detailModel) filesView(w, h int) string {
	th := d.th
	if d.m6Err != "" {
		return th.Muted.Render("! " + d.m6Err)
	}
	if len(d.files) == 0 {
		return th.Muted.Render("(no files)")
	}
	var out []string
	out = append(out, th.Muted.Render(fmt.Sprintf("%-30s %8s  %s", "Name", "Size", "Dir")))
	for _, f := range d.files {
		mark := " "
		if f.IsDir {
			mark = "D"
		}
		out = append(out, fmt.Sprintf("%s %-29s %8d  %s", mark, truncate(f.Name, 28), f.Size, f.Mode))
	}
	return strings.Join(out, "\n")
}

func (d *detailModel) worldsView(w, h int) string {
	th := d.th
	if d.m6Err != "" {
		return th.Muted.Render("! " + d.m6Err)
	}
	if len(d.worlds) == 0 {
		return th.Muted.Render("(no worlds found)")
	}
	var out []string
	for _, w := range d.worlds {
		out = append(out, fmt.Sprintf("• %-20s  %s", w.Name, humanSize(w.SizeBytes)))
	}
	return strings.Join(out, "\n")
}

func (d *detailModel) backupsView(w, h int) string {
	th := d.th
	if d.m6Err != "" {
		return th.Muted.Render("! " + d.m6Err)
	}
	if len(d.backups) == 0 {
		return th.Muted.Render("(no backups — press 'c' to create one)")
	}
	var out []string
	for _, b := range d.backups {
		line := fmt.Sprintf("• %-20s  %s  %s", b.Name, humanSize(b.SizeBytes), b.CreatedAt.Format("2006-01-02 15:04"))
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
