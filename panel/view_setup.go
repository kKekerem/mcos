package panel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/sysmon"
	"mcos/panel/theme"
)

// setupStep enumerates the first-boot (OOBE) wizard phases.
type setupStep int

const (
	stepIntro setupStep = iota
	stepSystem
	stepIdentity // PC name + Wi-Fi
	stepJava
	stepInstall
	stepTheme // theme + timezone
	stepBudget
	stepCluster // PC pairing
	stepFirstServer
	stepConfirm
	stepCount
)

// budgetRAM/budgetCPU are the selectable per-server resource caps (0 = sınırsız).
var budgetRAM = []int{0, 2048, 4096, 6144, 8192, 12288, 16384}
var budgetCPU = []int{0, 25, 50, 75, 100}

// timezones offered in the OOBE (kept short; full list editable later).
var timezones = []string{"Europe/Istanbul", "UTC", "Europe/London", "Europe/Berlin", "America/New_York", "Asia/Dubai"}

type setupModel struct {
	th *theme.Theme

	step   setupStep
	cursor int

	// Identity + Wi-Fi. ssid is a hidden holder set when a scanned network is
	// chosen (the user no longer types it); pass is the sub-prompt for a secured
	// network. netPassPrompt is open while that password is being entered.
	pcName        textinput.Model
	ssid          textinput.Model
	pass          textinput.Model
	networks      []ipc.WiFiNetwork
	scanNote      string
	netPassPrompt bool

	// status mirrors the daemon's live status so the Java step can gate on
	// internet + clock sync (RTC-less PCs need NTP before any TLS download).
	status *model.SystemStatus

	// Java
	javaOK        bool
	javaInstalled []model.JavaRuntime
	javaDetecting bool

	// Install options
	dlJava    bool // Download Java 17/21 during install
	dlPlugins bool // Download essential server jars

	// Theme + timezone
	themeIdx int
	tzIdx    int

	// Resource budget
	ramIdx int
	cpuIdx int

	// Cluster pairing
	clusterOn bool
	nodeName  textinput.Model
	peers     []model.Peer

	// First server (optional)
	startServer bool

	errMsg    string
	cancelled bool
	loading   bool
	installSuccess bool
	rebootCounter  int

	disks        []ipc.DiskTarget
	disksLoading bool
}

type rebootTickMsg struct{}

func doRebootTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return rebootTickMsg{}
	})
}

func newSetup(th *theme.Theme, themeName string) *setupModel {
	mk := func(ph string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 64
		ti.PromptStyle = th.Accent
		ti.TextStyle = th.Accent // Distinct color for entered text
		ti.PlaceholderStyle = th.Muted
		return ti
	}
	s := &setupModel{
		th:        theme.New(themeName),
		pcName:    mk("mcos-pc-1"),
		ssid:      mk("WiFi adı (boş = kablolu)"),
		pass:      mk("parola"),
		nodeName:  mk("mcos-1"),
		clusterOn: true,
		themeIdx:     themeIndex(themeName),
		ramIdx:       defaultRAMBudgetIdx(),
		disksLoading: true,
	}
	s.pass.EchoMode = textinput.EchoPassword
	s.pass.EchoCharacter = '•'
	s.pcName.Focus()
	return s
}

func themeIndex(name string) int {
	for i, n := range theme.Names() {
		if n == name {
			return i
		}
	}
	return 0
}

// defaultRAMBudgetIdx picks the largest cap that stays under ~75% of total RAM.
func defaultRAMBudgetIdx() int {
	totalMB := int(sysmon.Memory().TotalBytes / (1 << 20))
	limit := totalMB * 3 / 4
	best := 0
	for i, v := range budgetRAM {
		if v != 0 && v <= limit {
			best = i
		}
	}
	return best
}

// update returns done=true when the setup should close.
func (s *setupModel) update(m tea.KeyMsg, cl *Client) (bool, tea.Cmd) {
	if s.loading {
		return false, nil
	}
	// The network step has its own interactive list/password handling.
	if s.step == stepIdentity {
		return s.updateIdentity(m, cl)
	}
	switch m.String() {
	case "esc":
		s.cancelled = true
		return true, nil
	case "tab", "enter":
		return s.advance(cl)
	case "shift+tab":
		return s.back()
	case "up":
		s.moveField(-1)
		return false, nil
	case "down":
		s.moveField(1)
		return false, nil
	case "r":
		if s.step == stepIdentity {
			s.scanNote = "taranıyor…"
			return false, doWiFiScan(cl)
		} else if s.step == stepInstall {
			s.disksLoading = true
			return false, doFetchDisks(cl)
		}
	case "left", "h":
		s.adjust(-1)
		return false, nil
	case "right", "l", " ":
		s.adjust(1)
		return false, nil
	}

	// Text input handling.
	if ti := s.activeTextInput(); ti != nil {
		var cmd tea.Cmd
		*ti, cmd = ti.Update(m)
		return false, cmd
	}
	return false, nil
}

// activeTextInput returns a pointer to the focused text field, or nil.
func (s *setupModel) activeTextInput() *textinput.Model {
	switch s.step {
	case stepIdentity:
		if s.cursor == 0 {
			return &s.pcName
		}
	case stepCluster:
		if s.cursor == 1 {
			return &s.nodeName
		}
	}
	return nil
}

// --- stepIdentity: PC name + interactive scanned network list --------------

// identityRows is the navigable row count: PC name + each network + a final
// "Kablolu / Atla" entry.
func (s *setupModel) identityRows() int { return 1 + len(s.networks) + 1 }

func (s *setupModel) isWiredRow(c int) bool { return c == 1+len(s.networks) }

// networkAt returns the network under cursor c (rows 1..len), or nil.
func (s *setupModel) networkAt(c int) *ipc.WiFiNetwork {
	idx := c - 1
	if idx >= 0 && idx < len(s.networks) {
		return &s.networks[idx]
	}
	return nil
}

func (s *setupModel) syncIdentityFocus() {
	if s.cursor == 0 {
		s.pcName.Focus()
	} else {
		s.pcName.Blur()
	}
}

// updateIdentity drives the network step: ↑↓ moves the row cursor across the PC
// name field, the scanned networks, and a wired/skip entry; Enter connects (and
// opens a password sub-prompt for a secured network) or proceeds.
func (s *setupModel) updateIdentity(m tea.KeyMsg, cl *Client) (bool, tea.Cmd) {
	s.errMsg = ""

	// Password sub-prompt for a secured network.
	if s.netPassPrompt {
		switch m.String() {
		case "esc", "shift+tab":
			s.netPassPrompt = false
			s.pass.Blur()
			return false, nil
		case "enter":
			if net := s.networkAt(s.cursor); net != nil {
				s.ssid.SetValue(net.SSID)
			}
			s.netPassPrompt = false
			s.pass.Blur()
			return s.proceedFromIdentity(cl)
		default:
			var cmd tea.Cmd
			s.pass, cmd = s.pass.Update(m)
			return false, cmd
		}
	}

	// 'r' rescans, but only when not typing into the PC name field.
	if m.String() == "r" && s.cursor != 0 {
		s.scanNote = "taranıyor…"
		return false, doWiFiScan(cl)
	}

	rows := s.identityRows()
	switch m.String() {
	case "esc":
		s.cancelled = true
		return true, nil
	case "shift+tab":
		return s.back()
	case "up":
		s.cursor = (s.cursor - 1 + rows) % rows
		s.syncIdentityFocus()
		return false, nil
	case "down":
		s.cursor = (s.cursor + 1) % rows
		s.syncIdentityFocus()
		return false, nil
	case "tab":
		return s.proceedFromIdentity(cl)
	case "enter":
		switch {
		case s.cursor == 0 || s.isWiredRow(s.cursor):
			if s.isWiredRow(s.cursor) {
				s.ssid.SetValue("") // wired / skip Wi-Fi
			}
			return s.proceedFromIdentity(cl)
		default:
			net := s.networkAt(s.cursor)
			if net == nil {
				return false, nil
			}
			if net.Secured {
				s.netPassPrompt = true
				s.pass.SetValue("")
				return false, s.pass.Focus()
			}
			s.ssid.SetValue(net.SSID) // open network: connect directly
			s.pass.SetValue("")
			return s.proceedFromIdentity(cl)
		}
	}

	// Anything else edits the PC name while it is focused.
	if s.cursor == 0 {
		var cmd tea.Cmd
		s.pcName, cmd = s.pcName.Update(m)
		return false, cmd
	}
	return false, nil
}

// proceedFromIdentity validates the PC name, applies network settings, and advances to the Java step.
func (s *setupModel) proceedFromIdentity(cl *Client) (bool, tea.Cmd) {
	if strings.TrimSpace(s.pcName.Value()) == "" {
		s.errMsg = "PC adı gerekli"
		s.cursor = 0
		s.syncIdentityFocus()
		return false, nil
	}
	s.goTo(stepJava)
	s.javaDetecting = true
	ssid := s.effectiveSSID()
	pass := s.pass.Value()

	return false, func() tea.Msg {
		if ssid != "" {
			_ = cl.WiFiApply(ssid, pass)
		} else {
			_ = cl.WiredUp()
		}
		// Give the daemon a moment to finish NTP sync (started implicitly by applying net).
		time.Sleep(2 * time.Second)

		rts, err := cl.JavaList()
		return javaSetupMsg{ok: err == nil && len(rts) > 0, runtimes: rts}
	}
}

func (s *setupModel) moveField(delta int) {
	n := s.fieldCount()
	s.cursor = (s.cursor + delta + n) % n
	s.syncFocus()
}

func (s *setupModel) fieldCount() int {
	switch s.step {
	case stepInstall:
		return len(s.disks) + 1 + 2 // + skip + 2 options
	case stepIdentity:
		return 3 // pcName, ssid, pass
	case stepTheme:
		return 2 // theme, timezone
	case stepBudget:
		return 2 // ram, cpu
	case stepCluster:
		return 2 // toggle, node name
	case stepFirstServer:
		return 1 // toggle
	default:
		return 1
	}
}

func (s *setupModel) syncFocus() {
	s.pcName.Blur()
	s.ssid.Blur()
	s.pass.Blur()
	s.nodeName.Blur()
	if ti := s.activeTextInput(); ti != nil {
		ti.Focus()
	}
}

func (s *setupModel) advance(cl *Client) (bool, tea.Cmd) {
	s.errMsg = ""
	switch s.step {
	case stepIntro:
		s.goTo(stepSystem)
	case stepSystem:
		s.goTo(stepIdentity)
		// Kick off a Wi-Fi scan so the network list is ready. (stepIdentity is
		// then driven entirely by updateIdentity.)
		s.scanNote = "taranıyor…"
		return false, doWiFiScan(cl)
	case stepIdentity:
		// stepIdentity is handled by proceedFromIdentity
		return false, nil
	case stepJava:
		s.goTo(stepInstall)
		s.disksLoading = true
		return false, doFetchDisks(cl)
	case stepInstall:
		if s.cursor < len(s.disks) {
			disk := s.disks[s.cursor]
			s.loading = true
			return false, doInstallOS(cl, disk.Device, s.dlJava, s.dlPlugins)
		}
		s.goTo(stepTheme)
	case stepTheme:
		s.goTo(stepBudget)
	case stepBudget:
		s.goTo(stepCluster)
	case stepCluster:
		if strings.TrimSpace(s.nodeName.Value()) == "" {
			s.errMsg = "Düğüm adı gerekli"
			return false, nil
		}
		s.goTo(stepFirstServer)
	case stepFirstServer:
		s.goTo(stepConfirm)
	case stepConfirm:
		return true, doSetupSave(cl, s.buildConfig(), s.effectiveSSID(), s.pass.Value())
	}
	return false, nil
}

func (s *setupModel) back() (bool, tea.Cmd) {
	if s.step > stepInstall {
		s.step--
		s.cursor = 0
		s.syncFocus()
	}
	return false, nil
}

func (s *setupModel) goTo(step setupStep) {
	s.step = step
	s.cursor = 0
	s.syncFocus()
}

func (s *setupModel) adjust(delta int) {
	switch s.step {
	case stepInstall:
		if s.cursor == len(s.disks)+1 {
			s.dlJava = !s.dlJava
		} else if s.cursor == len(s.disks)+2 {
			s.dlPlugins = !s.dlPlugins
		}
	case stepTheme:
		switch s.cursor {
		case 0:
			n := len(theme.Names())
			s.themeIdx = (s.themeIdx + delta + n) % n
			s.th = theme.New(theme.Names()[s.themeIdx]) // live preview
		case 1:
			n := len(timezones)
			s.tzIdx = (s.tzIdx + delta + n) % n
		}
	case stepBudget:
		switch s.cursor {
		case 0:
			n := len(budgetRAM)
			s.ramIdx = (s.ramIdx + delta + n) % n
		case 1:
			n := len(budgetCPU)
			s.cpuIdx = (s.cpuIdx + delta + n) % n
		}
	case stepCluster:
		if s.cursor == 0 {
			s.clusterOn = !s.clusterOn
		}
	case stepFirstServer:
		s.startServer = !s.startServer
	}
}

// effectiveSSID returns the chosen Wi-Fi SSID (manual entry wins).
func (s *setupModel) effectiveSSID() string { return strings.TrimSpace(s.ssid.Value()) }

func (s *setupModel) buildConfig() *model.Config {
	cfg := model.DefaultConfig()
	cfg.SetupComplete = true
	cfg.Cluster.NodeName = strings.TrimSpace(s.nodeName.Value())
	cfg.Cluster.Enabled = s.clusterOn
	cfg.WiFiSSID = s.effectiveSSID()
	cfg.WiFiPassword = strings.TrimSpace(s.pass.Value())
	cfg.Theme = theme.Names()[s.themeIdx]
	cfg.Timezone = timezones[s.tzIdx]
	cfg.Budget = model.ResourceBudget{
		MaxServerRAMMB:      budgetRAM[s.ramIdx],
		MaxServerCPUPercent: budgetCPU[s.cpuIdx],
	}
	if cfg.NodeID == "" {
		cfg.NodeID = generateNodeID()
	}
	cfg.HardwareID = sysmon.HardwareID()
	return cfg
}

func (s *setupModel) handleJavaSetup(m javaSetupMsg) (bool, tea.Cmd) {
	s.javaDetecting = false
	s.loading = false
	s.javaOK = m.ok
	s.javaInstalled = m.runtimes
	return false, nil
}

func (s *setupModel) handleInstallDone(m installDoneMsg) (bool, tea.Cmd) {
	s.loading = false
	if m.err != nil {
		s.errMsg = m.err.Error()
		return false, nil
	}
	s.installSuccess = true
	s.rebootCounter = 10
	return false, doRebootTick()
}

func (s *setupModel) handleRebootTick() (bool, tea.Cmd) {
	s.rebootCounter--
	if s.rebootCounter <= 0 {
		_ = exec.Command("reboot", "-f").Run()
		return true, nil
	}
	return false, doRebootTick()
}

func (s *setupModel) handleWiFiScan(m wifiScanMsg) {
	if m.err != nil {
		s.scanNote = "tarama yapılamadı (kablolu bağlantı?)"
		return
	}
	s.networks = m.networks
	if len(m.networks) == 0 {
		s.scanNote = "ağ bulunamadı"
	} else {
		s.scanNote = fmt.Sprintf("%d ağ bulundu", len(m.networks))
	}
}

// --- views -----------------------------------------------------------------

func (s *setupModel) view(w, h int, peers []model.Peer) string {
	s.peers = peers
	th := s.th
	var body string
	switch s.step {
	case stepInstall:
		body = s.viewInstall()
	case stepIntro:
		body = s.viewIntro()
	case stepSystem:
		body = s.viewSystem()
	case stepIdentity:
		body = s.viewIdentity()
	case stepJava:
		body = s.viewJava()
	case stepTheme:
		body = s.viewTheme()
	case stepBudget:
		body = s.viewBudget()
	case stepCluster:
		body = s.viewCluster()
	case stepFirstServer:
		body = s.viewFirstServer()
	case stepConfirm:
		body = s.viewConfirm()
	}

	progress := fmt.Sprintf("Adım %d / %d", int(s.step)+1, int(stepCount))
	help := th.Muted.Render("tab/enter ileri · shift+tab geri · ↑↓ alan · ←→/space değiştir · esc iptal")
	if s.errMsg != "" {
		help = lipgloss.NewStyle().Foreground(th.P.Red).Render("⚠ "+s.errMsg) + "\n" + help
	}
	cardBody := lipgloss.JoinVertical(lipgloss.Left,
		th.CardTitle.Render("MCOS Kurulumu  ")+th.Muted.Render(progress), "", body, "", help)

	boxW := 74
	if boxW > w-4 {
		boxW = w - 4
	}
	box := th.Card.Width(boxW).Padding(1, 2).Render(cardBody)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}

func (s *setupModel) viewInstall() string {
	th := s.th
	if s.installSuccess {
		return th.Accent.Render(fmt.Sprintf("🎉 KURULUM BAŞARILI!\n\nLütfen MCOS kurulum USB'sini ŞİMDİ ÇIKARIN.\nSistem %d saniye içinde yeniden başlatılacak...", s.rebootCounter))
	}
	if s.loading {
		msg := "İşletim sistemi kalıcı diske kuruluyor... Lütfen bekleyin."
		if s.dlJava || s.dlPlugins {
			msg = "Java ve eklentiler indiriliyor ve sistem kuruluyor... Bu işlem internet hızınıza bağlı olarak zaman alabilir."
		}
		return th.Muted.Render(msg)
	}
	if s.disksLoading {
		return th.Muted.Render("Diskler aranıyor...")
	}
	lines := []string{
		th.Val.Render("MCOS Kurulumu - Disk Seçimi"),
		th.Muted.Render("(Yenilemek için 'r' tuşuna basın)"),
		"",
	}
	for i, d := range s.disks {
		label := fmt.Sprintf("%-10s %s (%.1f GB)", d.Device, truncate(d.Model, 20), float64(d.SizeBytes)/(1<<30))
		if d.HasPersist {
			label += " [MCOS KURULU]"
		}
		
		marker := "( )"
		if s.cursor == i {
			marker = th.Accent.Render("(*)")
			label = th.Val.Render(label)
		} else {
			label = th.Muted.Render(label)
		}
		
		lines = append(lines, fmt.Sprintf("  %s %s", marker, label))
	}
	
	// Add skip option
	skipMarker := "( )"
	skipLabel := "Atla (Sadece RAM'de çalıştır, kalıcı veri yok)"
	if s.cursor == len(s.disks) {
		skipMarker = th.Accent.Render("(*)")
		skipLabel = th.Val.Render(skipLabel)
	} else {
		skipLabel = th.Muted.Render(skipLabel)
	}
	lines = append(lines, fmt.Sprintf("  %s %s", skipMarker, skipLabel))

	lines = append(lines, "", th.Val.Render("Ek Seçenekler:"))

	checkJava := "[ ]"
	if s.dlJava {
		checkJava = th.Accent.Render("[x]")
	}
	labelJava := "Java 17 ve 21 sürümlerini şimdi indir"
	if s.cursor == len(s.disks)+1 {
		labelJava = th.Val.Render(labelJava)
	} else {
		labelJava = th.Muted.Render(labelJava)
	}
	lines = append(lines, fmt.Sprintf("  %s %s", checkJava, labelJava))

	checkPlug := "[ ]"
	if s.dlPlugins {
		checkPlug = th.Accent.Render("[x]")
	}
	labelPlug := "Temel sunucu dosyalarını (Paper/Fabric) önceden indir"
	if s.cursor == len(s.disks)+2 {
		labelPlug = th.Val.Render(labelPlug)
	} else {
		labelPlug = th.Muted.Render(labelPlug)
	}
	lines = append(lines, fmt.Sprintf("  %s %s", checkPlug, labelPlug))
	
	lines = append(lines, "", lipgloss.NewStyle().Foreground(th.P.Red).Render("⚠ DİKKAT: Seçilen disk tamamen silinecektir!"))
	return strings.Join(lines, "\n")
}

func (s *setupModel) viewIntro() string {
	th := s.th
	return strings.Join([]string{
		th.Val.Render("MCOS'e hoş geldiniz! 🎮"),
		"",
		th.Muted.Render("MCOS, yalnızca Minecraft sunucuları yönetmek için tasarlanmış"),
		th.Muted.Render("özel bir işletim sistemidir. Bu kısa kurulum:"),
		"",
		"  * " + th.Val.Render("Donanımınızı kontrol eder"),
		"  * " + th.Val.Render("Ağ (WiFi) ve Java'yı hazırlar"),
		"  * " + th.Val.Render("Tema ve kaynak limitlerini ayarlar"),
		"  * " + th.Val.Render("Yakındaki MCOS cihazlarıyla eşleşir"),
		"  * " + th.Val.Render("İsterseniz ilk sunucunuzu kurar"),
		"",
		th.Muted.Render("Başlamak için Enter'a basın."),
	}, "\n")
}

func (s *setupModel) viewSystem() string {
	th := s.th
	cpu := sysmon.CPU()
	mem := sysmon.Memory()
	disks := sysmon.Disks()
	gpus := sysmon.GPUs()
	net := sysmon.Net()

	gpu := "algılanmadı"
	if len(gpus) > 0 {
		gpu = truncate(strings.TrimSpace(gpus[0].Vendor+" "+gpus[0].Model), 36)
	}
	disk := "—"
	if len(disks) > 0 {
		disk = fmt.Sprintf("%.0f GiB", float64(disks[0].TotalBytes)/(1<<30))
	}
	online := "çevrimdışı"
	if net.Internet {
		online = "çevrimiçi"
	}
	rows := []string{
		s.kv("İşlemci", fmt.Sprintf("%s (%d çekirdek)", truncate(cpu.Model, 30), cpu.Cores)),
		s.kv("Bellek", fmt.Sprintf("%.1f GiB", float64(mem.TotalBytes)/(1<<30))),
		s.kv("Disk", disk),
		s.kv("GPU", gpu),
		s.kv("Ağ", fmt.Sprintf("%s · %s", online, orDash(net.LocalIP))),
	}
	// Detected network adapters — the hardware that "comes later" (drivers/link).
	// Read-only here; connecting happens in the next step / Donanım tab.
	if len(net.NICs) > 0 {
		rows = append(rows, "", th.Key.Render("  Ağ adaptörleri:"))
		for i, n := range net.NICs {
			if i >= 4 {
				break
			}
			kind := n.Kind
			if kind == "" {
				kind = "?"
			}
			link := "—"
			if n.Link {
				link = "bağlı"
			}
			rows = append(rows, th.Muted.Render(fmt.Sprintf("    %s (%s) sürücü=%s link=%s %s",
				n.Name, kind, orDash(n.Driver), link, orDash(n.IPv4))))
		}
	}
	return strings.Join(append(rows, "",
		th.Muted.Render("Hiçbir şey zorunlu değil — enter ile iler/atla, esc ile geç."),
		th.Muted.Render("Donanım kaydedilir; disk başka PC'ye takılırsa kurulum yeniden başlar.")), "\n")
}

func (s *setupModel) viewIdentity() string {
	th := s.th
	lines := []string{
		s.field("PC adı", s.pcName.View(), s.cursor == 0),
		"",
		th.Key.Render("  Bir ağ seçin") + th.Muted.Render("  (r: yeniden tara · "+s.scanNote+")"),
	}
	if len(s.networks) == 0 {
		lines = append(lines, th.Muted.Render("    ağ taranıyor / bulunamadı — kablolu için aşağıdan ‘Kablolu / Atla’"))
	} else {
		for i, n := range s.networks {
			if i >= 8 {
				break
			}
			lines = append(lines, s.networkRow(n, s.cursor == i+1))
		}
	}
	lines = append(lines, s.field("Kablolu / Atla", "", s.isWiredRow(s.cursor)))

	if s.netPassPrompt {
		name := ""
		if net := s.networkAt(s.cursor); net != nil {
			name = net.SSID
		}
		lines = append(lines, "",
			th.Accent.Render("🔒 "+name+" parolası: ")+s.pass.View(),
			th.Muted.Render("Enter: bağlan · Esc: vazgeç"))
	} else {
		lines = append(lines, "",
			th.Muted.Render("↑↓ seç · Enter: bağlan/ileri · şifreli ağda parola istenir"))
	}
	return strings.Join(lines, "\n")
}

// networkRow renders one scanned access point, highlighted when selected.
func (s *setupModel) networkRow(n ipc.WiFiNetwork, sel bool) string {
	th := s.th
	lock := "  "
	if n.Secured {
		lock = "🔒"
	}
	label := fmt.Sprintf("%-22s %s %3d%%", truncate(n.SSID, 22), lock, n.Signal)
	if sel {
		return th.Accent.Render(" ▶ ") + th.MenuActive.Render(" "+label+" ")
	}
	return "   " + th.Val.Render(label)
}

func (s *setupModel) viewJava() string {
	th := s.th
	if s.javaDetecting {
		return th.Muted.Render("Java çalışma zamanları taranıyor…")
	}
	lines := []string{}
	if s.javaOK {
		lines = append(lines, th.Val.Render("Kurulu Java sürümleri:"))
		for _, rt := range s.javaInstalled {
			lines = append(lines, fmt.Sprintf("  • Java %d — %s", rt.Major, truncate(rt.Version, 40)))
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, th.Muted.Render("Kurulu Java bulunamadı."))
		lines = append(lines, "")
	}
	lines = append(lines, th.Accent.Render("Java Kurulumu Otomatiktir!"))
	lines = append(lines, th.Val.Render("Sunucu oluşturduğunuzda, seçilen Minecraft sürümüne"))
	lines = append(lines, th.Val.Render("en uygun Java sürümü (örn: 8, 17, 21) arkaplanda indirilecektir."))
	lines = append(lines, "")
	lines = append(lines, th.Muted.Render("İlerlemek için Enter'a basın."))
	return strings.Join(lines, "\n")
}

func (s *setupModel) viewTheme() string {
	th := s.th
	name := theme.Names()[s.themeIdx]
	swatch := theme.Swatch(theme.AccentColor(name))
	return strings.Join([]string{
		s.field("Tema", fmt.Sprintf("‹ %s ›  %s", theme.Label(name), swatch), s.cursor == 0),
		s.field("Saat dilimi", fmt.Sprintf("‹ %s ›", timezones[s.tzIdx]), s.cursor == 1),
		"",
		th.Muted.Render(fmt.Sprintf("%d tema mevcut — ←→ ile değiştirin, renkler anında uygulanır.", len(theme.Names()))),
	}, "\n")
}

func (s *setupModel) viewBudget() string {
	th := s.th
	return strings.Join([]string{
		s.field("Sunucu başına maks RAM", budgetLabel(budgetRAM[s.ramIdx], "MB"), s.cursor == 0),
		s.field("Maks CPU payı", budgetLabel(budgetCPU[s.cpuIdx], "%"), s.cursor == 1),
		"",
		th.Muted.Render("Bu limitler, yeni sunucuların aşamayacağı tavanlardır."),
		th.Muted.Render("Sıfır = sınırsız (donanım el verdiğince)."),
	}, "\n")
}

func (s *setupModel) viewCluster() string {
	th := s.th
	lines := []string{
		s.field("PC eşleştirme (cluster)", s.toggle(s.clusterOn), s.cursor == 0),
		s.field("Düğüm adı", s.nodeName.View(), s.cursor == 1),
		"",
	}
	if !s.clusterOn {
		lines = append(lines, th.Muted.Render("Kapalı: bu PC yalnız başına çalışır."))
		return strings.Join(lines, "\n")
	}
	if len(s.peers) == 0 {
		lines = append(lines, th.Muted.Render("Yakında MCOS cihazı aranıyor… (otomatik keşif)"))
	} else {
		lines = append(lines, th.Val.Render("Bulunan cihazlar:"))
		for i, p := range s.peers {
			if i >= 5 {
				break
			}
			paired := ""
			if p.Paired {
				paired = " ✓"
			}
			lines = append(lines, fmt.Sprintf("  • %s (%s) — %s%s", p.Name, p.IP, p.State, paired))
		}
	}
	lines = append(lines, "", th.Muted.Render("Eşleşen iki PC, yedek/optimize işlerini paylaşır."))
	return strings.Join(lines, "\n")
}

func (s *setupModel) viewFirstServer() string {
	th := s.th
	choice := "Atla (sonra kurarım)"
	if s.startServer {
		choice = "Şimdi ilk sunucumu kur"
	}
	return strings.Join([]string{
		s.field("İlk sunucu", "‹ "+choice+" ›", s.cursor == 0),
		"",
		th.Muted.Render("Kurulum bittiğinde sunucu sihirbazı açılır (←→/space ile seç)."),
	}, "\n")
}

func (s *setupModel) viewConfirm() string {
	th := s.th
	wifi := s.effectiveSSID()
	if wifi == "" {
		wifi = "kablolu / atlandı"
	}
	first := "hayır"
	if s.startServer {
		first = "evet"
	}
	lines := []string{
		th.Val.Render("Özet"),
		s.kv("PC adı", s.pcName.Value()),
		s.kv("WiFi", wifi),
		s.kv("Tema", theme.Label(theme.Names()[s.themeIdx])),
		s.kv("Saat dilimi", timezones[s.tzIdx]),
		s.kv("RAM tavanı", budgetLabel(budgetRAM[s.ramIdx], "MB")),
		s.kv("CPU tavanı", budgetLabel(budgetCPU[s.cpuIdx], "%")),
		s.kv("Cluster", fmt.Sprintf("%v · %s", s.clusterOn, strings.TrimSpace(s.nodeName.Value()))),
		s.kv("İlk sunucu", first),
		"",
		th.Muted.Render("İsteğe bağlı: kurulumdan sonra Ayarlar'da 'p' ile USB'yi kalıcı yapabilirsiniz."),
		th.Muted.Render("Onaylamak için Enter · geri için Shift+Tab"),
	}
	return strings.Join(lines, "\n")
}

// --- small render helpers --------------------------------------------------

func (s *setupModel) field(label, value string, active bool) string {
	th := s.th
	marker := "  "
	lbl := th.Key.Render(fmt.Sprintf("%-24s", label))
	if active {
		marker = th.Accent.Render("▶ ")
		lbl = th.Accent.Render(fmt.Sprintf("%-24s", label))
	}
	return marker + lbl + th.Val.Render(value)
}

func (s *setupModel) kv(k, v string) string {
	return s.th.Key.Render(fmt.Sprintf("  %-14s", k)) + s.th.Val.Render(v)
}

func (s *setupModel) toggle(b bool) string {
	if b {
		return s.th.Badge("AÇIK", s.th.P.Green)
	}
	return s.th.Badge("KAPALI", s.th.P.Muted)
}

func budgetLabel(v int, unit string) string {
	if v == 0 {
		return "‹ sınırsız ›"
	}
	return fmt.Sprintf("‹ %d %s ›", v, unit)
}

func doSetupSave(cl *Client, cfg *model.Config, ssid, pass string) tea.Cmd {
	return func() tea.Msg {
		if err := cl.UpdateConfig(cfg); err != nil {
			return errMsg{err}
		}
		// Network was already applied in step 3 (proceedFromIdentity)
		// Ensure changes to the USB stick persist permanently.
		_, _ = cl.Persist("")
		
		return setupDoneMsg{}
	}
}

func generateNodeID() string {
	return "node_" + randomHex(8)
}

// randomHex returns n bytes of cryptographically-random hex (2n chars).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a fixed-but-unique-ish marker.
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}
