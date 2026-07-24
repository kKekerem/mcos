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

var ramChoices = []int{1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384}

var commonVersions = []string{"1.8.8", "1.12.2", "1.16.5", "1.18.2", "1.19.4", "1.20.4", "1.21.1"}

var gamemodes = []string{"survival", "creative", "adventure", "spectator"}
var difficulties = []string{"peaceful", "easy", "normal", "hard"}

// wizardStep is one page of the multi-step server creation wizard. The order
// follows the requested flow: şablon → ad → sürüm → altyapı → ağ → oyun ayarları
// → kaynak → kurulum yeri → EULA → onay.
type wizardStep int

const (
	wsTemplate  = iota // quick-start template (or "özel")
	wsIdentity         // name, description
	wsVersion          // minecraft version (live picker), ViaVersion toggle
	wsSoftware         // server flavour (paper/fabric/...)
	wsNetwork          // port, render + simulation distance
	wsGameplay         // online-mode, gamemode, difficulty, pvp, max-players, whitelist, hardcore, motd
	wsResources        // RAM, CPU, GPU (info), cluster share
	wsLocation         // install dir + autostart/backup/wan
	wsEULA             // Minecraft EULA acceptance
	wsConfirm
	wsCount
)

type wizardModel struct {
	th *theme.Theme

	step   wizardStep
	cursor int // field index within current step

	// Step 0: Template
	templateIdx int

	// Step 1: Identity
	name textinput.Model
	desc textinput.Model

	// Step 2: Version (live picker + manual override)
	version    textinput.Model // manual override (custom/old versions)
	versions   []string        // fetched from daemon (Mojang)
	verIdx     int             // index into versions
	verLoaded  bool
	viaVersion bool

	// Step 3: Software
	softwareIdx int

	// Step 4: Network
	port           textinput.Model
	renderDistance textinput.Model
	simDistance    textinput.Model

	// Step 5: Gameplay
	onlineMode    bool
	gamemodeIdx   int
	difficultyIdx int
	pvp           bool
	maxPlayers    textinput.Model
	whitelist     bool
	hardcore      bool
	motd          textinput.Model

	// Step 6: Resources
	ramIdx       int
	cpuQuota     textinput.Model
	gpuInfo      string
	clusterShare bool

	// Step 7: Location + flags
	dataDir    textinput.Model
	autostart  bool
	autoBackup bool
	wan        bool

	// Step 8: EULA
	eulaAccepted bool

	errMsg    string
	cancelled bool
}

func newWizard(th *theme.Theme) *wizardModel {
	mk := func(ph string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 64
		return ti
	}
	w := &wizardModel{
		th:             th,
		name:           mk("Survival"),
		desc:           mk("(opsiyonel)"),
		version:        mk("(listeden seç veya yaz)"),
		port:           mk("0 = otomatik"),
		renderDistance: mk("10"),
		simDistance:    mk("10"),
		maxPlayers:     mk("20"),
		motd:           mk("(opsiyonel)"),
		cpuQuota:       mk("sınırsız"),
		dataDir:        mk("(varsayılan)"),
		softwareIdx:    1, // paper
		ramIdx:         1, // 2048
		difficultyIdx:  1, // easy
		onlineMode:     true,
		pvp:            true,
		autoBackup:     true,
		gpuInfo:        gpuSummary(),
	}
	w.name.Focus()
	return w
}

// setVersions records the live version list and selects the newest by default.
func (w *wizardModel) setVersions(vs []string) {
	w.versions = vs
	w.verLoaded = true
	if len(vs) > 0 {
		w.verIdx = 0
	}
}

// pickedVersion returns the currently selected list version, or "".
func (w *wizardModel) pickedVersion() string {
	if w.verIdx >= 0 && w.verIdx < len(w.versions) {
		return w.versions[w.verIdx]
	}
	return ""
}

// effectiveVersion prefers a manual entry, falling back to the picked one.
func (w *wizardModel) effectiveVersion() string {
	if v := strings.TrimSpace(w.version.Value()); v != "" {
		return v
	}
	return w.pickedVersion()
}

// gpuSummary renders detected GPUs as a single info string. Minecraft servers
// are headless, so this is informational only (matches the spec's resource
// step listing RAM/CPU/GPU).
func gpuSummary() string {
	gs := sysmon.GPUs()
	if len(gs) == 0 {
		return "algılanmadı (sunucu başsız çalışır)"
	}
	var names []string
	for _, g := range gs {
		s := strings.TrimSpace(g.Vendor + " " + g.Model)
		if s == "" {
			s = "GPU"
		}
		names = append(names, s)
	}
	return strings.Join(names, ", ")
}

// update returns done=true when the wizard should close (submit or cancel).
func (w *wizardModel) update(m tea.KeyMsg) (bool, tea.Cmd) {
	switch m.String() {
	case "esc":
		w.cancelled = true
		return true, nil
	case "tab":
		return w.nextStep()
	case "shift+tab":
		return w.prevStep()
	case "up":
		w.moveField(-1)
		return false, nil
	case "down":
		w.moveField(1)
		return false, nil
	case "enter":
		if w.step == wsConfirm {
			if err := w.validate(); err != "" {
				w.errMsg = err
				return false, nil
			}
			return true, nil
		}
		return w.nextStep()
	}

	// Field-specific handling.
	if w.isText(w.cursor) {
		var cmd tea.Cmd
		switch w.textField() {
		case "name":
			w.name, cmd = w.name.Update(m)
		case "desc":
			w.desc, cmd = w.desc.Update(m)
		case "version":
			w.version, cmd = w.version.Update(m)
		case "port":
			w.port, cmd = w.port.Update(m)
		case "renderDistance":
			w.renderDistance, cmd = w.renderDistance.Update(m)
		case "simDistance":
			w.simDistance, cmd = w.simDistance.Update(m)
		case "maxPlayers":
			w.maxPlayers, cmd = w.maxPlayers.Update(m)
		case "motd":
			w.motd, cmd = w.motd.Update(m)
		case "cpuQuota":
			w.cpuQuota, cmd = w.cpuQuota.Update(m)
		case "dataDir":
			w.dataDir, cmd = w.dataDir.Update(m)
		}
		return false, cmd
	}

	switch m.String() {
	case "left", "h":
		w.adjust(-1)
	case "right", "l", " ":
		w.adjust(1)
	}
	return false, nil
}

func (w *wizardModel) isText(i int) bool {
	switch w.step {
	case wsIdentity:
		return i == 0 || i == 1
	case wsVersion:
		return i == 1 // manual version override
	case wsNetwork:
		return i == 0 || i == 1 || i == 2
	case wsGameplay:
		return i == 4 || i == 7 // maxPlayers, motd
	case wsResources:
		return i == 1 // cpuQuota
	case wsLocation:
		return i == 0 // dataDir
	}
	return false
}

func (w *wizardModel) textField() string {
	switch w.step {
	case wsIdentity:
		if w.cursor == 0 {
			return "name"
		}
		return "desc"
	case wsVersion:
		if w.cursor == 1 {
			return "version"
		}
	case wsNetwork:
		switch w.cursor {
		case 0:
			return "port"
		case 1:
			return "renderDistance"
		case 2:
			return "simDistance"
		}
	case wsGameplay:
		switch w.cursor {
		case 4:
			return "maxPlayers"
		case 7:
			return "motd"
		}
	case wsResources:
		if w.cursor == 1 {
			return "cpuQuota"
		}
	case wsLocation:
		if w.cursor == 0 {
			return "dataDir"
		}
	}
	return ""
}

func (w *wizardModel) syncFocus() {
	w.name.Blur()
	w.desc.Blur()
	w.version.Blur()
	w.port.Blur()
	w.renderDistance.Blur()
	w.simDistance.Blur()
	w.maxPlayers.Blur()
	w.motd.Blur()
	w.cpuQuota.Blur()
	w.dataDir.Blur()
	switch w.textField() {
	case "name":
		w.name.Focus()
	case "desc":
		w.desc.Focus()
	case "version":
		w.version.Focus()
	case "port":
		w.port.Focus()
	case "renderDistance":
		w.renderDistance.Focus()
	case "simDistance":
		w.simDistance.Focus()
	case "maxPlayers":
		w.maxPlayers.Focus()
	case "motd":
		w.motd.Focus()
	case "cpuQuota":
		w.cpuQuota.Focus()
	case "dataDir":
		w.dataDir.Focus()
	}
}

func (w *wizardModel) fieldCount() int {
	switch w.step {
	case wsTemplate:
		return 1
	case wsIdentity:
		return 2
	case wsVersion:
		return 3
	case wsSoftware:
		return 1
	case wsNetwork:
		return 3
	case wsGameplay:
		return 8
	case wsResources:
		return 3
	case wsLocation:
		return 4
	case wsEULA:
		return 1
	case wsConfirm:
		return 1
	}
	return 1
}

func (w *wizardModel) moveField(delta int) {
	n := w.fieldCount()
	w.cursor = (w.cursor + delta + n) % n
	w.syncFocus()
}

func (w *wizardModel) nextStep() (bool, tea.Cmd) {
	if err := w.validateStep(); err != "" {
		w.errMsg = err
		return false, nil
	}
	if w.step == wsTemplate {
		w.applyTemplate()
	}
	w.errMsg = ""
	if w.step+1 >= wsCount {
		return false, nil
	}
	w.step++
	w.cursor = 0
	w.syncFocus()
	return false, nil
}

func (w *wizardModel) prevStep() (bool, tea.Cmd) {
	if w.step <= 0 {
		return false, nil
	}
	w.step--
	w.cursor = 0
	w.syncFocus()
	return false, nil
}

func (w *wizardModel) adjust(delta int) {
	switch w.step {
	case wsTemplate:
		n := len(templates)
		w.templateIdx = (w.templateIdx + delta + n) % n
	case wsVersion:
		switch w.cursor {
		case 0:
			if n := len(w.versions); n > 0 {
				w.verIdx = (w.verIdx + delta + n) % n
			}
		case 2:
			w.viaVersion = !w.viaVersion
		}
	case wsSoftware:
		if w.cursor == 0 {
			n := len(model.AllSoftware)
			w.softwareIdx = (w.softwareIdx + delta + n) % n
		}
	case wsGameplay:
		switch w.cursor {
		case 0:
			w.onlineMode = !w.onlineMode
		case 1:
			n := len(gamemodes)
			w.gamemodeIdx = (w.gamemodeIdx + delta + n) % n
		case 2:
			n := len(difficulties)
			w.difficultyIdx = (w.difficultyIdx + delta + n) % n
		case 3:
			w.pvp = !w.pvp
		case 5:
			w.whitelist = !w.whitelist
		case 6:
			w.hardcore = !w.hardcore
		}
	case wsResources:
		switch w.cursor {
		case 0:
			n := len(ramChoices)
			w.ramIdx = (w.ramIdx + delta + n) % n
		case 2:
			w.clusterShare = !w.clusterShare
		}
	case wsLocation:
		switch w.cursor {
		case 1:
			w.autostart = !w.autostart
		case 2:
			w.autoBackup = !w.autoBackup
		case 3:
			w.wan = !w.wan
		}
	case wsEULA:
		w.eulaAccepted = !w.eulaAccepted
	}
}

func (w *wizardModel) validateStep() string {
	switch w.step {
	case wsIdentity:
		if strings.TrimSpace(w.name.Value()) == "" {
			return "Sunucu adı gerekli"
		}
	case wsVersion:
		if w.effectiveVersion() == "" {
			return "Bir Minecraft sürümü seçin veya yazın"
		}
	case wsNetwork:
		if p := strings.TrimSpace(w.port.Value()); p != "" {
			if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 65535 {
				return "Port 0–65535 arası bir sayı olmalı"
			}
		}
		if err := validDistance(w.renderDistance.Value()); err != "" {
			return "Render distance: " + err
		}
		if err := validDistance(w.simDistance.Value()); err != "" {
			return "Simulation distance: " + err
		}
	case wsGameplay:
		if p := strings.TrimSpace(w.maxPlayers.Value()); p != "" {
			if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 1000 {
				return "Maks oyuncu 1–1000 arası olmalı"
			}
		}
	case wsEULA:
		if !w.eulaAccepted {
			return "Devam etmek için Minecraft EULA'sını kabul edin (space)"
		}
	}
	return ""
}

func validDistance(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if n, err := strconv.Atoi(s); err != nil || n < 2 || n > 32 {
		return "2–32 arası bir sayı olmalı"
	}
	return ""
}

func (w *wizardModel) validate() string {
	// Check all steps for final submission.
	if strings.TrimSpace(w.name.Value()) == "" {
		return "Sunucu adı gerekli"
	}
	if w.effectiveVersion() == "" {
		return "Minecraft sürümü gerekli"
	}
	if p := strings.TrimSpace(w.port.Value()); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 65535 {
			return "Port 0–65535 arası bir sayı olmalı"
		}
	}
	if !w.eulaAccepted {
		return "EULA kabul edilmeli"
	}
	return ""
}

// atoiDefault parses s, returning def when empty/invalid.
func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func (w *wizardModel) params() ipc.ServerCreateParams {
	port := atoiDefault(w.port.Value(), 0)
	cpu := 0
	if q := strings.TrimSpace(w.cpuQuota.Value()); q != "" && !strings.EqualFold(q, "sınırsız") {
		cpu = atoiDefault(q, 0)
	}
	online := w.onlineMode
	pvp := w.pvp
	return ipc.ServerCreateParams{
		Name:             strings.TrimSpace(w.name.Value()),
		Description:      strings.TrimSpace(w.desc.Value()),
		Software:         model.AllSoftware[w.softwareIdx],
		MCVersion:        w.effectiveVersion(),
		RAMMB:            ramChoices[w.ramIdx],
		Port:             port,
		CPUQuota:         cpu,
		ViewDistance:     atoiDefault(w.renderDistance.Value(), 10),
		SimDistance:      atoiDefault(w.simDistance.Value(), 10),
		MaxPlayers:       atoiDefault(w.maxPlayers.Value(), 20),
		MOTD:             strings.TrimSpace(w.motd.Value()),
		Gamemode:         gamemodes[w.gamemodeIdx],
		Difficulty:       difficulties[w.difficultyIdx],
		OnlineMode:       &online,
		PVP:              &pvp,
		Whitelist:        w.whitelist,
		Hardcore:         w.hardcore,
		ClusterShare:     w.clusterShare,
		DataDir:          strings.TrimSpace(w.dataDir.Value()),
		Autostart:        w.autostart,
		AutoBackup:       w.autoBackup,
		WAN:              w.wan,
		AllowOldVersions: w.viaVersion,
	}
}

func (w *wizardModel) view(termW, termH int) string {
	th := w.th

	var body string
	switch w.step {
	case wsTemplate:
		body = w.viewTemplate(th)
	case wsIdentity:
		body = w.viewIdentity(th)
	case wsVersion:
		body = w.viewVersion(th)
	case wsSoftware:
		body = w.viewSoftware(th)
	case wsNetwork:
		body = w.viewNetwork(th)
	case wsGameplay:
		body = w.viewGameplay(th)
	case wsResources:
		body = w.viewResources(th)
	case wsLocation:
		body = w.viewLocation(th)
	case wsEULA:
		body = w.viewEULA(th)
	case wsConfirm:
		body = w.viewConfirm(th)
	}

	stepTitle := "Yeni Sunucu"
	switch w.step {
	case wsTemplate:
		stepTitle = "Şablon Seçimi"
	case wsIdentity:
		stepTitle = "Sunucu Kimliği"
	case wsVersion:
		stepTitle = "Minecraft Sürümü"
	case wsSoftware:
		stepTitle = "Yazılım Altyapısı"
	case wsNetwork:
		stepTitle = "Ağ ve Erişim"
	case wsGameplay:
		stepTitle = "Oyun Ayarları"
	case wsResources:
		stepTitle = "Kaynak Limitleri"
	case wsLocation:
		stepTitle = "Kurulum Konumu"
	case wsEULA:
		stepTitle = "Mojang EULA"
	case wsConfirm:
		stepTitle = "Kurulum Onayı"
	}

	header := RenderHeader(th, int(w.step)+1, int(wsCount), stepTitle)
	help := RenderKeyHints(th, []KeyHint{{"Enter", "ilerle"}, {"↑/↓", "gezin"}, {"Shift+Tab", "geri"}, {"Esc", "iptal"}}, 66)
	cardBody := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", help)
	if w.errMsg != "" {
		cardBody = lipgloss.JoinVertical(lipgloss.Left, cardBody, "",
			lipgloss.NewStyle().Foreground(th.P.Red).Render("⚠️  "+w.errMsg))
	}

	boxW := 72
	if boxW > termW-4 {
		boxW = termW - 4
	}
	box := RenderCard(th, "🎮 YENİ MINECRAFT SUNUCUSU", cardBody, boxW, true)
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}

func (w *wizardModel) row(label, value string, active bool) string {
	th := w.th
	marker := "   "
	lbl := th.Key.Render(fmt.Sprintf("%-22s", label))
	val := th.Val.Render(value)
	if active {
		marker = th.Accent.Render(" ➜ ")
		lbl = th.Accent.Bold(true).Render(fmt.Sprintf("%-22s", label))
		val = th.Val.Bold(true).Render(value)
	}
	return marker + lbl + val
}

func (w *wizardModel) toggle(b bool) string {
	th := w.th
	if b {
		return th.Badge("AÇIK", th.P.Green)
	}
	return th.Badge("KAPALI", th.P.Muted)
}

func (w *wizardModel) viewIdentity(th *theme.Theme) string {
	return strings.Join([]string{
		w.row("Sunucu adı", w.name.View(), w.cursor == 0),
		w.row("Açıklama", w.desc.View(), w.cursor == 1),
	}, "\n")
}

func (w *wizardModel) viewVersion(th *theme.Theme) string {
	picked := w.pickedVersion()
	if picked == "" {
		if w.verLoaded {
			picked = "(liste boş)"
		} else {
			picked = "yükleniyor…"
		}
	}
	src := RenderKeyHints(th, []KeyHint{{"←/→", "sürüm seç"}}, 40)
	if !w.verLoaded {
		src = th.Muted.Render("Sürüm listesi alınıyor…")
	} else if len(w.versions) == 0 {
		src = th.Muted.Render("Liste alınamadı; sürümü elle yazın.")
	}
	return strings.Join([]string{
		w.row("Sürüm (liste)", "< "+picked+" >", w.cursor == 0),
		w.row("Elle sürüm (ops.)", w.version.View(), w.cursor == 1),
		w.row("ViaVersion (eski giriş)", w.toggle(w.viaVersion), w.cursor == 2),
		"",
		src,
		th.Muted.Render("ViaVersion açıksa eski istemciler de bağlanabilir (Paper/Spigot)."),
	}, "\n")
}

func (w *wizardModel) viewSoftware(th *theme.Theme) string {
	sw := model.AllSoftware[w.softwareIdx]
	note := "Vanilla / mod yüklenemez"
	switch {
	case sw.SupportsPlugins():
		note = "Plugin destekli (Bukkit API)"
	case sw.SupportsMods():
		note = "Mod destekli (mod loader)"
	}
	return strings.Join([]string{
		w.row("Altyapı", "< "+string(sw)+" >", w.cursor == 0),
		"",
		th.Muted.Render(note),
	}, "\n")
}

func (w *wizardModel) viewNetwork(th *theme.Theme) string {
	return strings.Join([]string{
		w.row("Port", w.port.View(), w.cursor == 0),
		w.row("Görüş uzaklığı", w.renderDistance.View(), w.cursor == 1),
		w.row("Simülasyon uzaklığı", w.simDistance.View(), w.cursor == 2),
	}, "\n")
}

func (w *wizardModel) viewResources(th *theme.Theme) string {
	return strings.Join([]string{
		w.row("RAM (MB)", fmt.Sprintf("< %d >", ramChoices[w.ramIdx]), w.cursor == 0),
		w.row("CPU kota (%)", w.cpuQuota.View(), w.cursor == 1),
		w.row("PC paylaşım", w.toggle(w.clusterShare), w.cursor == 2),
		"",
		th.Muted.Render("PC paylaşım: yedek/optimize işleri eşleşmiş PC'ye devredilir."),
	}, "\n")
}

func (w *wizardModel) viewLocation(th *theme.Theme) string {
	return strings.Join([]string{
		w.row("Kurulum dizini", w.dataDir.View(), w.cursor == 0),
		w.row("Otomatik başlat", w.toggle(w.autostart), w.cursor == 1),
		w.row("Otomatik yedek", w.toggle(w.autoBackup), w.cursor == 2),
		w.row("WAN (Serveo)", w.toggle(w.wan), w.cursor == 3),
		"",
		th.Muted.Render("(dizin boş = varsayılan daemon data-root)"),
	}, "\n")
}

func (w *wizardModel) viewConfirm(th *theme.Theme) string {
	p := w.params()
	share := "kapalı"
	if p.ClusterShare {
		share = "açık"
	}
	loc := p.DataDir
	if loc == "" {
		loc = "varsayılan"
	}
	online := "açık"
	if p.OnlineMode != nil && !*p.OnlineMode {
		online = "kapalı (cracked)"
	}
	var lines []string
	lines = append(lines, th.Val.Render("Özet"))
	lines = append(lines, fmt.Sprintf("  Ad:         %s", p.Name))
	lines = append(lines, fmt.Sprintf("  Yazılım:    %s %s", p.Software, p.MCVersion))
	lines = append(lines, fmt.Sprintf("  ViaVersion: %v", p.AllowOldVersions))
	lines = append(lines, fmt.Sprintf("  RAM:        %d MB", p.RAMMB))
	lines = append(lines, fmt.Sprintf("  Port:       %d", p.Port))
	lines = append(lines, fmt.Sprintf("  Mesafe:     render %d / sim %d", p.ViewDistance, p.SimDistance))
	lines = append(lines, fmt.Sprintf("  Oyun:       %s / %s · %d oyuncu", p.Gamemode, p.Difficulty, p.MaxPlayers))
	lines = append(lines, fmt.Sprintf("  Online/PvP: %s / %v · whitelist %v · hardcore %v", online, p.PVP != nil && *p.PVP, p.Whitelist, p.Hardcore))
	lines = append(lines, fmt.Sprintf("  PC paylaşım:%s", " "+share))
	lines = append(lines, fmt.Sprintf("  Başlat/Yedek/WAN: %v / %v / %v", p.Autostart, p.AutoBackup, p.WAN))
	lines = append(lines, fmt.Sprintf("  Kurulum:    %s", loc))
	lines = append(lines, "")
	lines = append(lines, th.Muted.Render("Onaylamak için Enter · Geri için Shift+Tab"))
	lines = append(lines, th.Muted.Render("Onay sonrası yazılım otomatik indirilip kurulur."))
	return strings.Join(lines, "\n")
}
