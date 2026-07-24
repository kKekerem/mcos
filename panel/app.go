package panel

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/model"
	"mcos/panel/theme"
)

const sidebarWidth = 30

// focus targets.
const (
	focusSidebar = iota
	focusContent
)

// App is the root Bubble Tea model.
type App struct {
	cl        *Client
	th        *theme.Theme
	themeName string

	width, height int
	section       int
	focus         int

	status    *systemStatus
	servers   []*serverInfo
	java      []javaRuntime
	javaProgress map[int]java.DownloadProgress
	peers     []model.Peer
	tasks     []model.Task
	tunnels   []model.TunnelStatus
	statusErr string
	flash     string

	wifiNets []ipc.WiFiNetwork // last Wi-Fi scan (Donanım tab)
	wifiNote string

	serverCursor  int
	contentScroll int // generic scroll offset for sections without a row cursor
	wifiCursor    int // selected Wi-Fi network in the Donanım/Ağ list
	rowCursor     int // interactive selection row for Software, Settings, Share
	wifiInput     textinput.Model
	wifiConnect   bool // password prompt is open
	detail        *detailModel
	wizard        *wizardModel
	setup         *setupModel

	powerMenu   bool // güç (Kapat/Yeniden Başlat) modal is open
	powerCursor int  // 0 Kapat · 1 Yeniden Başlat · 2 Vazgeç

	config      *model.Config
	bootChecked bool
	quitting    bool
}

// NewApp constructs the root model bound to a connected client and theme name.
func NewApp(cl *Client, themeName string) *App {
	wi := textinput.New()
	wi.Placeholder = "WiFi şifresi"
	wi.Prompt = "şifre: "
	wi.EchoMode = textinput.EchoPassword
	wi.EchoCharacter = '•'
	wi.CharLimit = 64
	return &App{
		cl:        cl,
		th:        theme.New(themeName),
		themeName: themeName,
		// fbterm normally emits WindowSizeMsg immediately. Keep a useful first
		// frame for framebuffer combinations that do not.
		width:     120,
		height:    36,
		section:   secServers,
		focus:     focusSidebar,
		wifiInput: wi,
	}
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(fetchConfig(a.cl), fetchStatus(a.cl), fetchServers(a.cl), fetchJava(a.cl), fetchJavaProgress(a.cl), fetchPeers(a.cl), fetchTasks(a.cl), fetchTunnels(a.cl), tick())
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case tickMsg:
		cmds := []tea.Cmd{fetchStatus(a.cl), fetchServers(a.cl), fetchPeers(a.cl), fetchTasks(a.cl), fetchTunnels(a.cl), fetchJavaProgress(a.cl), tick()}
		if a.detail != nil {
			cmds = append(cmds, fetchServer(a.cl, a.detail.id))
		}
		return a, tea.Batch(cmds...)

	case javaProgressMsg:
		if m.err == nil && m.progress != nil {
			for major, p := range m.progress {
				oldP, hadOld := a.javaProgress[major]
				if p.Done && (!hadOld || !oldP.Done) {
					if p.Error != "" {
						a.flash = fmt.Sprintf("⚠️ Java %d hatası: %s", major, p.Error)
					} else {
						a.flash = fmt.Sprintf("✓ Java %d (Temurin JDK) başarıyla kuruldu!", major)
					}
					return a, fetchJava(a.cl)
				}
			}
			a.javaProgress = m.progress
		}
		return a, nil

	case statusMsg:
		a.status = m.status
		a.statusErr = ""
		if a.setup != nil {
			a.setup.status = m.status // feed the OOBE Java gate (internet + clock)
		}
		return a, nil

	case configMsg:
		a.config = m.config
		if a.config != nil {
			// Apply the configured theme live (e.g. right after first-boot setup).
			if a.config.Theme != "" && a.config.Theme != a.themeName {
				a.themeName = a.config.Theme
				a.th = theme.New(a.config.Theme)
			}
			if !a.config.SetupComplete && a.setup == nil {
				a.setup = newSetup(a.th, a.config.Theme)
				return a, doFetchDisks(a.cl)
			}
		}
		return a, nil
	case disksMsg:
		if a.setup != nil {
			a.setup.disks = m.disks
			a.setup.disksLoading = false
		}
		return a, nil
	case serversMsg:
		a.servers = m.servers
		if a.serverCursor >= len(a.servers) {
			a.serverCursor = max(0, len(a.servers)-1)
		}
		return a, nil

	case serverMsg:
		if a.detail != nil && m.server != nil && m.server.ID == a.detail.id {
			a.detail.server = m.server
		}
		return a, nil

	case javaMsg:
		a.java = m.runtimes
		return a, nil
	case peersMsg:
		a.peers = m.peers
		if a.setup != nil {
			a.setup.peers = m.peers
		}
		return a, nil

	case wifiScanMsg:
		if a.setup != nil {
			a.setup.handleWiFiScan(m)
		} else {
			a.wifiNets = m.networks
			switch {
			case m.err != nil:
				a.wifiNote = "tarama yapılamadı (kablosuz arabirim?)"
			case len(m.networks) == 0:
				a.wifiNote = "ağ bulunamadı"
			default:
				a.wifiNote = fmt.Sprintf("%d ağ bulundu", len(m.networks))
			}
		}
		return a, nil

	case versionsMsg:
		if a.wizard != nil && m.err == nil {
			a.wizard.setVersions(m.versions)
		}
		return a, nil
	case tasksMsg:
		a.tasks = m.tasks
		return a, nil
	case tunnelsMsg:
		a.tunnels = m.tunnels
		return a, nil

	case consoleMsg, consolePollMsg,
		backupsMsg, filesMsg, worldsMsg, playersMsg, catalogMsg:
		if a.detail != nil {
			cmd := a.detail.update(msg, a.cl)
			return a, cmd
		}
		return a, nil

	case actionMsg:
		if m.err != nil {
			a.flash = "⚠ " + m.err.Error()
		} else {
			a.flash = "✓ " + m.msg
		}
		return a, fetchServers(a.cl)

	case createdMsg:
		if m.err != nil {
			if a.wizard != nil {
				a.wizard.errMsg = m.err.Error()
			}
			return a, nil
		}
		a.wizard = nil
		a.flash = "✓ sunucu oluşturuldu"
		return a, tea.Batch(fetchServers(a.cl), fetchStatus(a.cl))

	case javaSetupMsg:
		if a.setup != nil {
			_, cmd := a.setup.handleJavaSetup(m)
			return a, cmd
		}
		return a, nil
	case installDoneMsg:
		if a.setup != nil {
			_, cmd := a.setup.handleInstallDone(m)
			return a, cmd
		}
		return a, nil
	case rebootTickMsg:
		if a.setup != nil {
			_, cmd := a.setup.handleRebootTick()
			return a, cmd
		}
		return a, nil
	case setupDoneMsg:
		startServer := a.setup != nil && a.setup.startServer
		a.setup = nil
		a.flash = "✓ kurulum tamamlandı"
		if startServer {
			a.openWizard()
			return a, tea.Batch(fetchConfig(a.cl), fetchStatus(a.cl), doFetchVersions(a.cl, ""))
		}
		return a, tea.Batch(fetchConfig(a.cl), fetchStatus(a.cl))
	case errMsg:
		a.statusErr = m.err.Error()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(m)
	}
	return a, nil
}

func (a *App) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Setup wizard takes absolute precedence.
	if a.setup != nil {
		done, cmd := a.setup.update(m, a.cl)
		if done {
			if a.setup.cancelled {
				a.setup = nil
				return a, nil
			}
			// advance already triggered doSetupSave; wait for setupDoneMsg.
		}
		return a, cmd
	}
	// Modals take precedence.
	if a.wizard != nil {
		done, cmd := a.wizard.update(m)
		if done {
			if a.wizard.cancelled {
				a.wizard = nil
				return a, nil
			}
			params := a.wizard.params()
			return a, doCreate(a.cl, params)
		}
		return a, cmd
	}
	if a.detail != nil {
		// Let detail consume keys; it returns back=true to exit.
		back, cmd := a.detail.handleKey(m, a.cl)
		if back {
			a.detail = nil
			return a, fetchServers(a.cl)
		}
		return a, cmd
	}

	// Power modal captures keys while open.
	if a.powerMenu {
		switch m.String() {
		case "esc", "g", "q":
			a.powerMenu = false
		case "up", "k":
			a.powerCursor = (a.powerCursor + 2) % 3
		case "down", "j":
			a.powerCursor = (a.powerCursor + 1) % 3
		case "enter", " ":
			a.powerMenu = false
			switch a.powerCursor {
			case 0:
				a.flash = "kapatılıyor…"
				return a, doPower(a.cl, "poweroff")
			case 1:
				a.flash = "yeniden başlatılıyor…"
				return a, doPower(a.cl, "reboot")
			}
		}
		return a, nil
	}

	// Wi-Fi password prompt (Donanım) captures keys while open.
	if a.wifiConnect {
		switch m.String() {
		case "esc":
			a.wifiConnect = false
			a.wifiInput.Blur()
			a.wifiInput.SetValue("")
			return a, nil
		case "enter":
			ssid := a.wifiSelectedSSID()
			pass := a.wifiInput.Value()
			a.wifiConnect = false
			a.wifiInput.Blur()
			a.wifiInput.SetValue("")
			if ssid == "" {
				return a, nil
			}
			a.wifiNote = "bağlanılıyor: " + ssid + "…"
			return a, doWiFiApply(a.cl, ssid, pass)
		default:
			var cmd tea.Cmd
			a.wifiInput, cmd = a.wifiInput.Update(m)
			return a, cmd
		}
	}

	switch m.String() {
	case "ctrl+c", "q":
		a.quitting = true
		return a, tea.Quit

	case "g":
		a.powerMenu = true
		a.powerCursor = 0
		return a, nil

	case "t":
		if a.status != nil {
			return a, doTurbo(a.cl, !a.status.TurboOn)
		}

	case "1":
		if a.section == secSoftware {
			a.flash = "Java 17 (Temurin JDK) indirmesi başlatıldı…"
			return a, doJavaInstall(a.cl, 17)
		}

	case "2":
		if a.section == secSoftware {
			a.flash = "Java 21 (Temurin JDK) indirmesi başlatıldı…"
			return a, doJavaInstall(a.cl, 21)
		}

	case "3":
		if a.section == secSoftware {
			a.flash = "Java 11 (Temurin JDK) indirmesi başlatıldı…"
			return a, doJavaInstall(a.cl, 11)
		}

	case "4":
		if a.section == secSoftware {
			a.flash = "Java 8 (Temurin JDK) indirmesi başlatıldı…"
			return a, doJavaInstall(a.cl, 8)
		}

	case "p":
		if a.section == secSettings {
			a.flash = "USB kalıcı yapılıyor…"
			return a, doPersist(a.cl)
		}

	case "i":
		if a.section == secSettings {
			a.flash = "MCOS Kurulum Sihirbazı başlatılıyor…"
			a.setup = newSetup(a.th, a.themeName)
			return a, doFetchDisks(a.cl)
		}

	case "tab", "shift+tab":
		a.toggleFocus()

	case "up", "k":
		a.moveCursor(-1)
	case "down", "j":
		a.moveCursor(1)

	case "enter", "right", "l":
		return a.activate()

	case "esc", "left", "h":
		if a.focus == focusContent {
			a.focus = focusSidebar
		}

	case "n":
		a.openWizard()
		return a, doFetchVersions(a.cl, "")

	case "s", "S":
		if a.section == secServers && a.focus == focusContent && len(a.servers) > 0 {
			return a, doAction(a.cl, actStart, a.servers[a.serverCursor].ID)
		}
	case "x", "X":
		if a.section == secServers && a.focus == focusContent && len(a.servers) > 0 {
			return a, doAction(a.cl, actStop, a.servers[a.serverCursor].ID)
		}
	case "r", "R":
		if a.section == secServers && a.focus == focusContent && len(a.servers) > 0 {
			return a, doAction(a.cl, actRestart, a.servers[a.serverCursor].ID)
		}
		if a.section == secDevices || a.section == secNetwork {
			a.flash = "donanım yeniden taranıyor…"
			return a, tea.Batch(fetchStatus(a.cl), fetchJava(a.cl), fetchPeers(a.cl))
		}
	case "e":
		if a.section == secDevices || a.section == secNetwork {
			a.flash = "kablolu bağlanıyor…"
			return a, doWiredUp(a.cl)
		}
	case "w":
		if a.section == secDevices || a.section == secNetwork {
			a.wifiNote = "taranıyor…"
			a.wifiCursor = 0
			return a, doWiFiScan(a.cl)
		}
	}
	return a, nil
}

// toggleFocus switches between the sidebar and the content pane.
func (a *App) toggleFocus() {
	if a.focus == focusSidebar {
		a.focus = focusContent
	} else {
		a.focus = focusSidebar
	}
}

// moveCursor moves up (-1) / down (+1) within the focused pane: the sidebar
// section list, or the active section's row cursor / scroll offset.
func (a *App) moveCursor(d int) {
	if a.focus == focusSidebar {
		a.section = (a.section + d + secCount) % secCount
		a.serverCursor, a.wifiCursor, a.rowCursor, a.contentScroll = 0, 0, 0, 0
		return
	}
	switch a.section {
	case secServers:
		a.serverCursor = clampInt(a.serverCursor+d, 0, len(a.servers)-1)
	case secSoftware:
		a.rowCursor = clampInt(a.rowCursor+d, 0, 3)
	case secDevices:
		if len(a.wifiNets) > 0 {
			a.wifiCursor = clampInt(a.wifiCursor+d, 0, len(a.wifiNets)-1)
		}
	case secSettings:
		a.rowCursor = clampInt(a.rowCursor+d, 0, 4)
	case secPeers:
		a.rowCursor = clampInt(a.rowCursor+d, 0, maxInt(0, len(a.peers)-1))
	default:
		if a.contentScroll+d >= 0 {
			a.contentScroll += d
		}
	}
}

// activate handles enter/right: drop into the content from the sidebar, or act
// on the focused row (open a server, start a Wi-Fi connect).
func (a *App) activate() (tea.Model, tea.Cmd) {
	if a.focus == focusSidebar {
		a.focus = focusContent
		a.serverCursor, a.wifiCursor, a.rowCursor, a.contentScroll = 0, 0, 0, 0
		return a, nil
	}
	switch a.section {
	case secServers:
		if len(a.servers) > 0 {
			id := a.servers[a.serverCursor].ID
			a.openDetail(id)
			return a, fetchServer(a.cl, id)
		}
	case secSoftware:
		majors := []int{17, 21, 11, 8}
		if a.rowCursor >= 0 && a.rowCursor < len(majors) {
			m := majors[a.rowCursor]
			a.flash = fmt.Sprintf("Java %d indirmesi başlatılıyor...", m)
			return a, doJavaInstall(a.cl, m)
		}
	case secDevices:
		if len(a.wifiNets) > 0 {
			a.wifiConnect = true
			a.wifiInput.SetValue("")
			return a, a.wifiInput.Focus()
		}
	case secSettings:
		switch a.rowCursor {
		case 0:
			// Cycle theme
			nextTh := "graphite-teal"
			switch a.themeName {
			case "graphite-teal":
				nextTh = "noir-purple"
			case "noir-purple":
				nextTh = "anthracite-orange"
			case "anthracite-orange":
				nextTh = "anthracite-green"
			case "anthracite-green":
				nextTh = "crimson-night"
			case "crimson-night":
				nextTh = "amber-graphite"
			}
			a.themeName = nextTh
			a.th = theme.New(nextTh)
			a.flash = "Tema değiştirildi: " + nextTh
		case 1:
			// Toggle Turbo
			if a.config != nil {
				a.config.Turbo = !a.config.Turbo
				a.flash = fmt.Sprintf("Turbo modu %v yapıldı", a.config.Turbo)
			}
		case 2:
			a.flash = "USB kalıcı yapılıyor…"
			return a, doPersist(a.cl)
		case 3:
			a.flash = "MCOS Kurulum Sihirbazı başlatılıyor…"
			a.setup = newSetup(a.th, a.themeName)
			return a, doFetchDisks(a.cl)
		case 4:
			if a.config != nil {
				a.config.AutostartServers = !a.config.AutostartServers
				a.flash = fmt.Sprintf("Oto-başlatma %v yapıldı", a.config.AutostartServers)
			}
		}
	case secPeers:
		if len(a.peers) > 0 && a.rowCursor < len(a.peers) {
			p := a.peers[a.rowCursor]
			a.flash = fmt.Sprintf("PC %s ile eşleşiliyor...", p.Name)
		}
	}
	return a, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wifiSelectedSSID returns the SSID under the Wi-Fi cursor, or "".
func (a *App) wifiSelectedSSID() string {
	if a.wifiCursor >= 0 && a.wifiCursor < len(a.wifiNets) {
		return a.wifiNets[a.wifiCursor].SSID
	}
	return ""
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (a *App) openWizard() {
	a.wizard = newWizard(a.th)
}

func (a *App) openDetail(id string) {
	a.detail = newDetail(a.th, id)
	a.focus = focusContent
}

func (a *App) View() string {
	if a.quitting {
		return ""
	}
	if a.width == 0 {
		return "MCOS paneli yükleniyor…"
	}
	if a.setup != nil {
		return a.setup.view(a.width, a.height, a.peers)
	}
	if a.wizard != nil {
		return a.th.App.Width(a.width).Height(a.height).Render(
			a.wizard.view(a.width, a.height))
	}
	if a.powerMenu {
		return a.th.App.Width(a.width).Height(a.height).Render(
			lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center,
				a.renderPowerModal(), lipgloss.WithWhitespaceChars(" ")))
	}

	// Layout: breadcrumb bar (1) + body panes + help bar (1).
	bodyH := a.height - 2
	if bodyH < 5 {
		bodyH = 5
	}
	sidebarOuter := sidebarWidth
	contentOuter := a.width - sidebarOuter
	if contentOuter < 24 {
		contentOuter = 24
	}

	sidebar := a.pane(a.renderSidebar(sidebarOuter-4, bodyH-2), sidebarOuter, bodyH, a.focus == focusSidebar)

	innerW, innerH := contentOuter-4, bodyH-2
	var content string
	if a.detail != nil {
		content = a.detail.view(innerW, innerH)
	} else {
		content = a.renderContent(innerW, innerH)
	}
	contentPane := a.pane(content, contentOuter, bodyH, a.focus == focusContent)

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, contentPane)
	return lipgloss.JoinVertical(lipgloss.Left, a.renderTopBar(), body, a.renderHelp())
}

// pane wraps content in a rounded box whose border glows in the accent colour
// when focused and stays muted otherwise — the primary "where am I" cue.
func (a *App) pane(content string, outerW, outerH int, focused bool) string {
	th := a.th
	bc := th.P.Border
	if focused {
		bc = th.P.Accent
	}
	frameW, frameH := outerW-2, outerH-2 // border is outside Lip Gloss Width/Height.
	if frameW < 1 {
		frameW = 1
	}
	if frameH < 1 {
		frameH = 1
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(bc).BorderBackground(th.P.Bg).
		Background(th.P.Bg).Foreground(th.P.Text).
		Width(frameW).Height(frameH).Padding(0, 1).
		Render(clampLines(content, frameH))
}

// renderPowerModal draws the centered Kapat / Yeniden Başlat / Vazgeç dialog.
func (a *App) renderPowerModal() string {
	th := a.th
	opts := []struct{ icon, label, desc string }{
		{"●", "Kapat", "sistemi güvenle kapat"},
		{"↻", "Yeniden Başlat", "sistemi yeniden başlat"},
		{"↩", "Vazgeç", "geri dön"},
	}
	var b strings.Builder
	b.WriteString(th.CardTitle.Render("◆ Güç Menüsü") + "\n")
	b.WriteString(th.Muted.Render("Yalnızca klavye: seçim için yön tuşları, onay için Enter.") + "\n\n")
	for i, o := range opts {
		label := o.icon + " " + o.label
		if i == a.powerCursor {
			b.WriteString(RenderOptionRow(th, label, o.desc, true) + "\n")
		} else {
			b.WriteString(RenderOptionRow(th, label, o.desc, false) + "\n")
		}
	}
	b.WriteString("\n" + RenderKeyHints(th, []KeyHint{{"↑↓", "seç"}, {"Enter", "onayla"}, {"Esc", "vazgeç"}}, 44))
	return th.Card.Width(50).Padding(1, 2).Render(b.String())
}

// renderTopBar draws the breadcrumb header: MCOS > MENÜ/<bölüm> [> <öğe>],
// highlighting the part that currently has focus.
func (a *App) renderTopBar() string {
	th := a.th
	sep := th.Muted.Render(" > ")
	crumb := th.Accent.Bold(true).Render("◆ MCOS")
	sect := sectionNames[a.section]
	if a.focus == focusSidebar {
		crumb += sep + th.Title.Render("MENÜ") + sep + th.Muted.Render(sect)
	} else {
		crumb += sep + th.Muted.Render("menü") + sep + th.Title.Render(sect)
		if a.detail != nil && a.detail.server != nil {
			crumb += sep + th.Val.Render(a.detail.server.Name)
		}
	}
	right := ""
	if a.status != nil && a.status.TurboOn {
		right += th.Badge("TURBO ⚡", th.P.Accent) + " "
	}
	right += a.tierBadge() + " "
	gap := a.width - lipgloss.Width(crumb) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	return th.App.Width(a.width).Render(" " + crumb + spaces(gap) + right)
}

// renderContent dispatches to the active section's view.
func (a *App) renderContent(w, h int) string {
	switch a.section {
	case secDashboard:
		return a.renderDashboard(w, h)
	case secServers:
		return a.renderServers(w, h)
	case secSoftware:
		return a.renderSoftware(w, h)
	case secPerformance:
		return a.renderPerformance(w, h)
	case secDevices:
		return a.renderDevices(w, h)
	case secNetwork:
		return a.renderNetwork(w, h)
	case secWAN:
		return a.renderTunnel(w, h)
	case secPeers:
		return a.renderPeers(w, h)
	case secSettings:
		return a.renderSettings(w, h)
	default:
		return a.contentFrame(w, h, sectionNames[a.section], "—")
	}
}

// contentFrame composes a section title + scrollable body sized to the pane
// (the surrounding focus border is drawn by pane()). The body scrolls by
// a.contentScroll so long text sections never overflow.
func (a *App) contentFrame(w, h int, title, body string) string {
	th := a.th
	avail := h - 2
	if avail < 1 {
		avail = 1
	}

	lines := strings.Split(body, "\n")
	total := len(lines)
	start := a.contentScroll
	if start < 0 {
		start = 0
	}
	if total > 0 && start > total-1 {
		start = total - 1
	}
	end := start + avail
	if end > total {
		end = total
	}

	header := th.Title.Render(title)
	if total > avail {
		position := th.Muted.Render(fmt.Sprintf("↑↓  %d-%d / %d", start+1, end, total))
		gap := w - lipgloss.Width(header) - lipgloss.Width(position)
		if gap < 1 {
			gap = 1
		}
		header += spaces(gap) + position
	}

	dividerW := w
	if dividerW < 1 {
		dividerW = 1
	}
	divider := th.Muted.Render(strings.Repeat("─", dividerW))
	return header + "\n" + divider + "\n" + sliceScroll(body, start, avail)
}

func (a *App) renderHelp() string {
	th := a.th
	right := ""
	if a.flash != "" {
		right = th.Accent.Render(a.flash + " ")
	} else if a.statusErr != "" {
		right = lipgloss.NewStyle().Foreground(th.P.Red).Render("⚠ " + a.statusErr + " ")
	}

	var hints []KeyHint
	switch {
	case a.wifiConnect:
		hints = []KeyHint{{"type", "WiFi şifresi"}, {"Enter", "bağlan"}, {"Esc", "vazgeç"}}
	case a.detail != nil:
		hints = []KeyHint{{"←/→", "sekme"}, {"s", "başlat"}, {"x", "durdur"}, {"r", "yenile"}, {"Esc", "geri"}, {"q", "çıkış"}}
	default:
		if a.focus == focusSidebar {
			hints = []KeyHint{{"↑/↓", "menü"}, {"Enter", "seç"}, {"Tab", "pano"}, {"n", "yeni"}, {"t", "turbo"}, {"g", "güç"}, {"q", "çıkış"}}
		} else {
			hints = []KeyHint{{"↑/↓", "gezin"}, {"Enter", "aç"}, {"Esc", "menü"}, {"n", "yeni"}, {"t", "turbo"}, {"g", "güç"}, {"q", "çıkış"}}
		}
	}

	maxLeft := a.width - lipgloss.Width(right) - 2
	if maxLeft < 12 {
		maxLeft = 12
	}
	left := " " + RenderKeyHints(th, hints, maxLeft)
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return th.App.Width(a.width).Render(left + spaces(gap) + right)
}

// --- small helpers ---------------------------------------------------------

func itoa(i int) string { return strconv.Itoa(i) }

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	s := make([]byte, n)
	for i := range s {
		s[i] = ' '
	}
	return string(s)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncate shortens s to n runes with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

var _ = fmt.Sprintf
