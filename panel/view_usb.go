package panel

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mcos/internal/model"
	"mcos/panel/theme"
)

// USB EKRANI — takılı bellekteki mod/eklenti jar'larını sunucuya taşır.
//
// Akış (kullanıcının istediği sıra):
//
//	1. Sol menüde "USB Bellek" (yalnızca USB takılıyken görünür)
//	2. Enter → jar'lar taranır ve listelenir
//	3. ↑/↓ gezin, Space ile seç (çoklu seçim)
//	4. Enter → hedef sunucu listesi
//	5. ↑/↓ ile sunucu seç, Enter → dosyalar kopyalanır
//
// Taramanın panelde DEĞİL daemon'da yapılmasının sebebi: bölümleri bağlamak
// root gerektirir ve panel yalnızca bir RPC istemcisidir.

// usbStage is which step of the USB flow is on screen.
type usbStage int

const (
	usbStageIdle    usbStage = iota // henüz taranmadı
	usbStageScan                    // tarama sürüyor
	usbStageFiles                   // jar listesi, çoklu seçim
	usbStageServers                 // hedef sunucu seçimi
	usbStageDone                    // sonuç mesajı
)

// usbState holds the USB screen's state. App içinde tek bir örnek tutulur.
type usbState struct {
	stage    usbStage
	items    []model.USBJar
	selected map[int]bool
	cursor   int

	serverCursor int
	note         string
	err          string
}

func newUSBState() *usbState {
	return &usbState{selected: map[int]bool{}}
}

// selectedJars returns the chosen jars in LIST order.
//
// Dilim üzerinden sırayla gezilir; map üzerinde dolaşmak Go'da rastgele sıra
// verir ve kurulum sırası her çağrıda değişirdi.
func (u *usbState) selectedJars() []model.USBJar {
	var out []model.USBJar
	for i, it := range u.items {
		if u.selected[i] {
			out = append(out, it)
		}
	}
	return out
}

// eligibleServers returns servers that can accept mods/plugins.
//
// Vanilla sunucular eklenti/mod yükleyemez; listede göstermek kullanıcıyı
// yanıltır. Filtre model'in kendi yeteneği alanlarına dayanır.
func eligibleServers(all []*model.Server) []*model.Server {
	out := make([]*model.Server, 0, len(all))
	for _, s := range all {
		if s.SupportsPlugins || s.SupportsMods {
			out = append(out, s)
		}
	}
	return out
}

// usbKey handles keys while the USB section has content focus.
// Returns (cmd, handled).
func (a *App) usbKey(key string) (tea.Cmd, bool) {
	u := a.usb
	if u == nil {
		u = newUSBState()
		a.usb = u
	}

	switch u.stage {

	case usbStageIdle, usbStageDone:
		if key == "enter" || key == "r" {
			u.stage = usbStageScan
			u.note = "USB bellek taranıyor…"
			u.err = ""
			return doUSBScan(a.cl), true
		}

	case usbStageScan:
		// Tarama sürerken tuşlar yok sayılır (bağlama işlemi kesilmemeli).
		return nil, true

	case usbStageFiles:
		switch key {
		case "up", "k":
			if n := len(u.items); n > 0 {
				u.cursor = (u.cursor - 1 + n) % n
			}
			return nil, true
		case "down", "j":
			if n := len(u.items); n > 0 {
				u.cursor = (u.cursor + 1) % n
			}
			return nil, true
		case " ", "space":
			if len(u.items) > 0 {
				u.selected[u.cursor] = !u.selected[u.cursor]
			}
			return nil, true
		case "a":
			// Tümünü seç / tüm seçimi kaldır.
			all := len(u.selectedJars()) == len(u.items) && len(u.items) > 0
			for i := range u.items {
				u.selected[i] = !all
			}
			return nil, true
		case "r":
			u.stage = usbStageScan
			u.note = "yeniden taranıyor…"
			return doUSBScan(a.cl), true
		case "enter":
			if len(u.selectedJars()) == 0 {
				u.err = "Önce boşluk (Space) tuşuyla en az bir dosya seçin."
				return nil, true
			}
			if len(eligibleServers(a.servers)) == 0 {
				u.err = "Mod/eklenti destekleyen bir sunucu yok. Önce Paper, Fabric veya Forge sunucusu oluşturun."
				return nil, true
			}
			u.err = ""
			u.stage = usbStageServers
			u.serverCursor = 0
			return nil, true
		case "esc":
			u.stage = usbStageIdle
			return nil, true
		}

	case usbStageServers:
		srvs := eligibleServers(a.servers)
		switch key {
		case "up", "k":
			if n := len(srvs); n > 0 {
				u.serverCursor = (u.serverCursor - 1 + n) % n
			}
			return nil, true
		case "down", "j":
			if n := len(srvs); n > 0 {
				u.serverCursor = (u.serverCursor + 1) % n
			}
			return nil, true
		case "enter":
			if u.serverCursor >= len(srvs) {
				return nil, true
			}
			target := srvs[u.serverCursor]
			jars := u.selectedJars()
			u.stage = usbStageDone
			u.note = fmt.Sprintf("%d dosya %s sunucusuna kopyalanıyor…", len(jars), target.Name)
			u.err = ""
			return doUSBInstall(a.cl, target.ID, jars), true
		case "esc":
			u.stage = usbStageFiles
			return nil, true
		}
	}
	return nil, false
}

// handleUSBScanResult folds a scan reply into the USB screen state.
func (a *App) handleUSBScanResult(msg usbScanMsg) {
	if a.usb == nil {
		a.usb = newUSBState()
	}
	u := a.usb
	u.items = msg.items
	u.selected = map[int]bool{}
	u.cursor = 0

	switch {
	case msg.err != nil:
		u.stage = usbStageIdle
		u.err = "Tarama başarısız: " + msg.err.Error()
		u.note = ""
	case len(msg.items) == 0:
		u.stage = usbStageIdle
		u.err = ""
		u.note = "USB bellekte .jar dosyası bulunamadı. Dosyaları belleğe kopyalayıp 'r' ile yeniden tarayın."
	default:
		u.stage = usbStageFiles
		u.err = ""
		u.note = fmt.Sprintf("%d dosya bulundu.", len(msg.items))
	}
}

// renderUSB draws the USB screen for the current stage.
func (a *App) renderUSB(w, h int) string {
	th := a.th
	if a.usb == nil {
		a.usb = newUSBState()
	}
	u := a.usb

	var b strings.Builder

	switch u.stage {

	case usbStageIdle, usbStageScan:
		b.WriteString(th.Heading.Render(theme.IconUSB+" USB Bellekten Mod / Eklenti Yükle") + "\n\n")
		if a.status != nil && a.status.USB.Present {
			b.WriteString(InfoRow(th, "Takılı bölüm", fmt.Sprintf("%d", a.status.USB.Partitions)) + "\n\n")
		}
		if u.stage == usbStageScan {
			b.WriteString(th.Body.Render(theme.IconWait+" "+u.note) + "\n")
		} else {
			b.WriteString(th.Body.Render("USB belleğinizdeki .jar dosyalarını tarayıp sunucularınıza kopyalayabilirsiniz.") + "\n\n")
			b.WriteString(Button(th, "Taramayı Başlat", "Enter", true) + "\n")
		}
		if u.note != "" && u.stage != usbStageScan {
			b.WriteString("\n" + th.Muted.Render(u.note) + "\n")
		}
		if u.err != "" {
			b.WriteString("\n" + th.Error.Render(theme.IconWarn+" "+u.err) + "\n")
		}

	case usbStageFiles:
		sel := len(u.selectedJars())
		b.WriteString(th.Heading.Render(theme.IconUSB+" USB'deki Dosyalar") +
			th.Muted.Render(fmt.Sprintf("   %d dosya · %d seçili", len(u.items), sel)) + "\n\n")

		rows := make([]string, len(u.items))
		for i, it := range u.items {
			box := theme.IconUnchcked
			if u.selected[i] {
				box = theme.IconChecked
			}
			line := fmt.Sprintf("%s  %-*s  %9s  %s",
				box,
				theme.ListNameWidth, Truncate(it.Name, theme.ListNameWidth),
				humanSize(it.SizeBytes),
				it.Origin())
			if i == u.cursor {
				rows[i] = th.Selected.Render(theme.IconCursor + " " + line)
			} else {
				rows[i] = th.Body.Render("  " + line)
			}
		}
		b.WriteString(List(th, rows, u.cursor, theme.ListHeight(h)))
		if u.err != "" {
			b.WriteString("\n" + th.Error.Render(theme.IconWarn+" "+u.err))
		}
		b.WriteString("\n\n" + RenderKeyHints(th, []KeyHint{
			{"↑/↓", "gezin"},
			{"Space", "seç"},
			{"a", "tümü"},
			{"Enter", "sunucu seç"},
			{"r", "yeniden tara"},
			{"Esc", "geri"},
		}, w))

	case usbStageServers:
		srvs := eligibleServers(a.servers)
		jars := u.selectedJars()
		b.WriteString(th.Heading.Render(theme.IconServer+" Hedef Sunucu Seçin") +
			th.Muted.Render(fmt.Sprintf("   %d dosya taşınacak", len(jars))) + "\n\n")
		b.WriteString(th.Muted.Render("Yalnızca mod/eklenti destekleyen sunucular listelenir.") + "\n\n")

		rows := make([]string, len(srvs))
		for i, s := range srvs {
			kind := "eklenti"
			if s.SupportsMods && !s.SupportsPlugins {
				kind = "mod"
			} else if s.SupportsMods && s.SupportsPlugins {
				kind = "mod+eklenti"
			}
			line := fmt.Sprintf("%-*s  %-12s  %-8s  %s",
				theme.ListNameWidth, Truncate(s.Name, theme.ListNameWidth),
				s.Software, s.MCVersion, kind)
			if i == u.serverCursor {
				rows[i] = th.Selected.Render(theme.IconCursor + " " + line)
			} else {
				rows[i] = th.Body.Render("  " + line)
			}
		}
		b.WriteString(List(th, rows, u.serverCursor, theme.ListHeight(h)))
		b.WriteString("\n\n" + RenderKeyHints(th, []KeyHint{
			{"↑/↓", "gezin"},
			{"Enter", "kopyala"},
			{"Esc", "dosya listesine dön"},
		}, w))

	case usbStageDone:
		b.WriteString(th.Heading.Render(theme.IconUSB+" Sonuç") + "\n\n")
		if u.err != "" {
			b.WriteString(th.Error.Render(theme.IconFail+" "+u.err) + "\n")
		} else {
			b.WriteString(th.OK.Render(theme.IconOK+" "+u.note) + "\n")
		}
		b.WriteString("\n" + RenderKeyHints(th, []KeyHint{
			{"Enter", "yeniden tara"},
			{"Esc", "menü"},
		}, w))
	}

	return a.contentFrame(w, h, "USB Bellek", b.String())
}
