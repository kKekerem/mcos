// Package fbpanel is the MCOS interface drawn directly to the framebuffer.
//
// ── Neden var? ──────────────────────────────────────────────────────────────
// Eski panel bir TERMİNAL uygulamasıydı (Bubble Tea + fbterm). Terminalde her
// şey karakter hücresine oturur: çerçeveler ╭─╮ karakterleriyle, işaretler
// ● ○ ile, ikonlar emoji ile çizilir. Bunun üç sonucu vardı:
//
//  1. Köşeler gerçekten yuvarlak değildi; dört ayrı karakterdi ve font
//     değişince kopuyordu.
//  2. Bazı işaretler gömülü fontta yoktu; fbterm yedek fonta düşüyor, o
//     fontun farklı genişliği tüm satırı kaydırıyordu.
//  3. Açılır pencerenin arkasını bulanıklaştırmak imkânsızdı — hücrenin
//     "altı" diye bir şey yok.
//
// Bu paket pikselleri kendisi çizer: çerçeveler gerçek Bézier yayı, işaretler
// gerçek daire, arka plan gerçekten bulanıklaşıyor.
//
// ── Düzen SABİT ─────────────────────────────────────────────────────────────
// Bölüm sırası ve adları eski panelle AYNI (panel/sidebar.go). Değişen tek şey
// çizim biçimi. Kullanıcı aynı yerde aynı şeyi bulmalı.
package fbpanel

import (
	"image"
	"sync"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
)

// Section identifies a sidebar entry. Sıra panel/sidebar.go ile AYNI olmalı.
type Section int

const (
	SecDashboard Section = iota
	SecServers
	SecUSB // yalnızca USB takılıyken görünür
	SecSoftware
	SecPerformance
	SecDevices
	SecNetwork
	SecTunnel
	SecPeers
	SecPower
	SecSettings
	secCount
)

// sectionNames are the sidebar labels, in order.
//
// "Tünel (Serveo)" ARTIK YOK: Serveo kaldırıldı, yerine playit geldi.
// "Güç" yeni: uyku kipi oradan açılır.
var sectionNames = [secCount]string{
	SecDashboard:   "Sistem Durumu",
	SecServers:     "Sunucular",
	SecUSB:         "USB Bellek",
	SecSoftware:    "Yazılım",
	SecPerformance: "Performans",
	SecDevices:     "Donanım",
	SecNetwork:     "Ağ",
	SecTunnel:      "Tünel (playit)",
	SecPeers:       "MCOS Paylaşım",
	SecPower:       "Güç",
	SecSettings:    "Ayarlar",
}

// Name returns the sidebar label for a section.
func (s Section) Name() string {
	if s < 0 || s >= secCount {
		return ""
	}
	return sectionNames[s]
}

// maxEvents is how many live status lines are kept.
//
// Alt çubuk yalnızca sonuncuyu gösterir, ama geçmiş "Sistem Durumu" ekranında
// listelenir: kullanıcı sunucunun neden çöktüğünü sonradan okuyabilmeli.
const maxEvents = 64

// App is the framebuffer panel.
type App struct {
	ui *fbui.UI
	cl *ipcclient.Client

	mu      sync.Mutex
	section Section
	cursor  int
	focus   Focus

	status  *model.SystemStatus
	servers []*model.Server
	cfg     *model.Config

	events []fbui.Event
	modal  Modal
	scrim  fbui.ScrimCache

	// asleep: ekran kapalı, sunucular çalışmaya devam ediyor.
	asleep   bool
	sleepMsg string

	spin  int
	dirty bool

	// lastErr is surfaced in the status bar instead of being swallowed.
	lastErr string
}

// Modal is an overlay drawn on top of the current screen.
//
// nil ise açılır pencere yoktur. Arayüz katmanının modal'ı bilmesi gerekir
// çünkü tuşlar önce ona gider.
type Modal interface {
	// Title is shown in the dialog header.
	Title() string
	// Size returns the dialog size in character cells.
	Size() (cols, rows int)
	// Draw paints the dialog body inside r.
	Draw(a *App, r image.Rectangle)
	// Key handles a keystroke. done=true closes the dialog.
	Key(a *App, key string) (done bool)
}

// New creates the panel bound to a canvas and a daemon client.
func New(ui *fbui.UI, cl *ipcclient.Client) *App {
	a := &App{ui: ui, cl: cl, dirty: true}
	a.Emit(fbui.EventInfo, "MCOS hazır")
	return a
}

// Emit appends a live status event.
//
// Kullanıcının isteği: "sunucuyu actıysak altta sunucu acılıyor basladı gibi
// uyarılar olacak". Her eylem buradan geçer, böylece alt çubuk her zaman ne
// olduğunu söyler ve hiçbir hata sessizce kaybolmaz.
func (a *App) Emit(kind fbui.EventKind, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, fbui.Event{Kind: kind, Text: text, At: time.Now()})
	if len(a.events) > maxEvents {
		a.events = a.events[len(a.events)-maxEvents:]
	}
	a.dirty = true
}

// LastEvent returns the most recent event, or nil.
func (a *App) LastEvent() *fbui.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) == 0 {
		return nil
	}
	e := a.events[len(a.events)-1]
	return &e
}

// Events returns a copy of the event history, newest last.
func (a *App) Events() []fbui.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]fbui.Event, len(a.events))
	copy(out, a.events)
	return out
}

// Invalidate marks the screen as needing a redraw.
func (a *App) Invalidate() {
	a.mu.Lock()
	a.dirty = true
	a.mu.Unlock()
}

// Dirty reports and clears the redraw flag.
func (a *App) Dirty() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.dirty
	a.dirty = false
	return d
}

// Asleep reports whether the screen is currently blanked.
func (a *App) Asleep() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asleep
}

// Sleep blanks the screen. Servers keep running: this only affects the display.
//
// Kullanıcının isteği: "uyku modu ekle oradan arkada sunucular kapanacak ama
// ekran gidecek". DİKKAT — sunucular KAPANMAZ. Sunucuları durdurmak, uzaktaki
// oyuncuları atmak demektir; "uyku" yalnızca ekranı kapatır.
func (a *App) Sleep() {
	a.mu.Lock()
	a.asleep = true
	a.dirty = true
	a.mu.Unlock()
	a.Emit(fbui.EventInfo, "Ekran uykuda — sunucular çalışmaya devam ediyor")
}

// Wake turns the screen back on.
func (a *App) Wake() bool {
	a.mu.Lock()
	was := a.asleep
	a.asleep = false
	a.dirty = true
	a.mu.Unlock()
	return was
}

// SetSleepNote records how sleep was achieved (hardware blank vs painted black).
func (a *App) SetSleepNote(s string) {
	a.mu.Lock()
	a.sleepMsg = s
	a.mu.Unlock()
}

// Snapshot returns the current daemon state under one lock.
func (a *App) Snapshot() (*model.SystemStatus, []*model.Server, *model.Config) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status, a.servers, a.cfg
}

// SetStatus stores a freshly polled status.
func (a *App) SetStatus(st *model.SystemStatus) {
	a.mu.Lock()
	a.status = st
	a.dirty = true
	a.mu.Unlock()
}

// SetServers stores a freshly polled server list and reports state changes.
//
// Sunucu durumu değiştiğinde olay üretilir: kullanıcı bir sunucuyu başlattığında
// alt çubukta "açılıyor" ve sonra "çalışıyor" görmeli. Bunu POLLING'den türetmek,
// her eylem yerinde ayrı ayrı bildirim yazmaktan daha güvenilir — daemon
// tarafında olan değişiklikler de görünür.
func (a *App) SetServers(list []*model.Server) {
	a.mu.Lock()
	prev := map[string]model.ServerState{}
	for _, s := range a.servers {
		prev[s.ID] = s.State
	}
	var emits []fbui.Event
	for _, s := range list {
		old, seen := prev[s.ID]
		if !seen || old == s.State {
			continue
		}
		if k, msg := stateEvent(s); msg != "" {
			emits = append(emits, fbui.Event{Kind: k, Text: msg, At: time.Now()})
		}
	}
	a.servers = list
	a.events = append(a.events, emits...)
	if len(a.events) > maxEvents {
		a.events = a.events[len(a.events)-maxEvents:]
	}
	a.dirty = true
	a.mu.Unlock()
}

// stateEvent maps a server state change to a status line.
func stateEvent(s *model.Server) (fbui.EventKind, string) {
	switch s.State {
	case model.StateStarting:
		return fbui.EventBusy, s.Name + " açılıyor…"
	case model.StateRunning:
		return fbui.EventOK, s.Name + " çalışıyor"
	case model.StateStopping:
		return fbui.EventBusy, s.Name + " kapatılıyor…"
	case model.StateStopped:
		return fbui.EventInfo, s.Name + " durdu"
	case model.StateError:
		return fbui.EventError, s.Name + " hata verdi"
	}
	return fbui.EventInfo, ""
}

// SetConfig stores the daemon config and applies its theme.
func (a *App) SetConfig(cfg *model.Config) {
	a.mu.Lock()
	a.cfg = cfg
	if cfg != nil && fbui.ValidTheme(cfg.Theme) {
		a.ui.Pal = fbui.DefaultPalette.WithAccent(cfg.Theme)
	}
	a.dirty = true
	a.mu.Unlock()
}

// Fail records an error so the status bar can show it.
//
// Eski panelde birçok hata `_, _ =` ile yutuluyordu ve kullanıcı neden hiçbir
// şey olmadığını anlamıyordu. Burada her hata görünür.
func (a *App) Fail(context string, err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.lastErr = context + ": " + err.Error()
	a.mu.Unlock()
	a.Emit(fbui.EventError, context+": "+err.Error())
}

// visibleSections returns the sections shown right now, in order.
//
// USB bölümü yalnızca gerçekten bir bellek takılıyken listelenir (eski panelle
// aynı kural): boş bir "USB" satırı kullanıcıyı yanıltır.
func (a *App) visibleSections() []Section {
	out := make([]Section, 0, secCount)
	for s := Section(0); s < secCount; s++ {
		if s == SecUSB {
			st, _, _ := a.Snapshot()
			if st == nil || !st.USB.Present {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// Section returns the currently selected section.
func (a *App) Section() Section {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.section
}

// Cursor returns the in-section row cursor.
func (a *App) Cursor() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cursor
}

// SetCursor moves the row cursor.
func (a *App) SetCursor(c int) {
	a.mu.Lock()
	a.cursor = c
	a.dirty = true
	a.mu.Unlock()
}

// gotoSection switches sections and resets the row cursor.
func (a *App) gotoSection(s Section) {
	a.mu.Lock()
	a.section = s
	a.cursor = 0
	a.dirty = true
	a.mu.Unlock()
}

// OpenModal shows a dialog and captures the blurred backdrop once.
func (a *App) OpenModal(m Modal) {
	a.mu.Lock()
	a.modal = m
	a.dirty = true
	a.mu.Unlock()
	// Perde ÖNBELLEĞİ geçersiz kılınır: arkadaki ekran pencere açılırken
	// yeniden çizilecek ve bulanıklık o görüntüden alınacak.
	a.scrim.Invalidate()
}

// CloseModal dismisses the current dialog.
func (a *App) CloseModal() {
	a.mu.Lock()
	a.modal = nil
	a.dirty = true
	a.mu.Unlock()
	a.scrim.Invalidate()
}

// ActiveModal returns the open dialog, or nil.
func (a *App) ActiveModal() Modal {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modal
}

// Tick advances animations. Returns true if a redraw is needed.
func (a *App) Tick() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.spin++
	// Yalnızca bir şey DÖNÜYORSA yeniden çizim iste: boştayken her 100 ms'de
	// tüm ekranı çizmek bir sunucu makinesinde boşuna CPU yakar.
	for _, s := range a.servers {
		if s.State == model.StateStarting || s.State == model.StateStopping {
			return true
		}
	}
	if len(a.events) > 0 && a.events[len(a.events)-1].Kind == fbui.EventBusy {
		return true
	}
	return false
}
