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
	"sync/atomic"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
)

// SetPointerDevices records which pointing devices the host found.
//
// Panel evdev'i kendisi okumaz (cmd katmanının işi); ama Ayarlar ekranında
// göstermek zorundadır, yoksa "fare desteği açık ama çalışmıyor" sorusunun
// yanıtı hiçbir yerde görünmez.
func (a *App) SetPointerDevices(names []string) {
	a.mu.Lock()
	a.pointerDevices = names
	a.dirty = true
	a.mu.Unlock()
}

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
	SecDisplay
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
	SecDisplay:     "Ekran",
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
	// wifiPickerWanted: kullanici ag listesini ISTEDI mi. Tarama saniyeler
	// surdugu icin bu arada baska bir pencere acilmis olabilir; o zaman
	// listeyi acmak, kullanicinin actigi pencereyi habersizce yok ederdi.
	wifiListWanted bool
	scrim          fbui.ScrimCache

	// Bolumlere ozgu, daemon'dan cekilen ek veriler. Ana durum (status,
	// servers, cfg) her saniye yenilenir; bunlar ise YALNIZCA o bolume
	// girildiginde cekilir - bos bir bolum icin her saniye RPC yapmak
	// uzerinde Minecraft sunucusu calisan bir makinede israftir.
	javaRuntimes []model.JavaRuntime
	peers        []model.Peer
	tunnels      []model.TunnelStatus
	usbJars      []model.USBJar
	usbScanned   bool
	wifiNote     string

	// ── PC eşleştirme / ortak dünya ─────────────────────────────────────
	// clusterID, BU makinenin eşleştirme kimliğidir (ad, adres, anahtar).
	// Kullanıcı öbür makinede elle eşleştirme yaparken bunu okur.
	clusterID peersIdentity
	// link, ortak dünyanın durumudur (dilimler, aktarım sayısı, mod).
	link model.LinkStatus
	// scanNote, radar animasyonunun altında yazan açıklamadır.
	scanNote string

	// playit, tünel ajanının son bilinen durumudur.
	playit ipcclient.PlayitStatus

	// pointerDevices, bulunan fare/touchpad adlarıdır. Ayarlar ekranı
	// gösterir: "fare çalışmıyor" şikâyetinin ilk sorusu budur.
	pointerDevices []string

	// Ekran bilgisi: gercek cozunurluk ve kullanicinin sectigi acilis modu.
	screenW, screenH int
	displayPref      string

	// asleep: ekran kapalı, sunucular çalışmaya devam ediyor.
	asleep   bool
	sleepMsg string

	// pending, onay penceresinden gelen guc eylemini tasir. Modal geri
	// cagrisi Action donduremez (arayuz bool doner), bu yuzden karar burada
	// saklanir ve ana dongu bir sonraki turda okur.
	pending Action

	spin  int
	dirty bool

	// refreshing, arka plan yoklamasının sürdüğünü söyler. Atomik, çünkü
	// hem ana döngüden hem goroutine'den okunur ve a.mu'yu beklemeden
	// denetlenmesi gerekir.
	refreshing atomic.Bool

	// ── Fare / touchpad ─────────────────────────────────────────────────
	// zones her karede yeniden kurulur: tıklanabilir her şey çizilirken
	// kendi dikdörtgenini kaydeder (bkz. pointer.go).
	zones []zone
	ptr   pointerState

	// ── Geçiş animasyonları ─────────────────────────────────────────────
	// prevFrame ve scratch TEMBEL ayrılır ve animasyonlar kapalıysa hiç
	// ayrılmaz: 1080p'de her biri 8.3 MB tutar.
	trans     *transition
	prevFrame *image.RGBA
	scratch   *image.RGBA

	// scanBusy, ağ/eş taraması sürerken radar animasyonunu açar.
	scanBusy bool

	// wizard, sunucu oluşturma sihirbazıdır. nil ise açık değildir.
	//
	// setup'tan AYRI: ikisi aynı anda açık olabilir mi? Hayır — ama ayrı
	// alanlar tutmak, hangisinin açık olduğunu tek bir bool'a sıkıştırmaktan
	// ve yanlış çizmekten daha güvenli.
	wizard *Wizard

	// setup, ilk kurulum sihirbazıdır. nil ise normal panel çizilir.
	//
	// Panelin İÇİNDE yaşar (ayrı bir program değil) çünkü açılış
	// ekranından sihirbaza yumuşak geçiş, iki ayrı süreç arasında
	// yapılamaz — tuval aynı olmalı.
	setup *Setup

	// ── Kilit ekranı ────────────────────────────────────────────────────
	// locked true iken panel yalnızca parola ekranını çizer ve hiçbir
	// kısayolu işlemez. İSTEĞE BAĞLIDIR: parola kurulmadıysa hiç devreye
	// girmez (bkz. model.SecurityConfig).
	locked    bool
	lockInput string
	lockErr   string
	// lockAn, kilit ekranındaki parola alanının yazma/silme animasyonu.
	//
	// Kullanıcının isteği: "yazma animasyonu şifre girerken falan, silerken".
	// Kilit ekranı TextModal KULLANMAZ (tam ekrandır, pencere değil), bu
	// yüzden aynı hareketin burada ayrıca kurulması gerekti.
	lockAn    lockAnim
	lockTries int
	// lockUntil, ard arda yanlış denemelerden sonraki bekleme süresinin
	// bitiş anıdır. Sıfır değer = ceza yok.
	lockUntil time.Time

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

// Animated is implemented by dialogs that move on their own.
//
// ── Neden gerekliydi ────────────────────────────────────────────────────────
// Panel boştayken hiçbir kare çizmez (bkz. App.Tick): üzerinde Minecraft
// sunucusu koşan bir makinede saniyede 60 kez tüm ekranı çizmek boşuna CPU
// demek. Ama bu, açık bir pencerenin KENDİ animasyonunu da donduruyordu —
// metin alanındaki imleç, "yanıp sönüyor" diye yazılmış olmasına rağmen
// gerçekte HİÇ yanıp sönmüyordu, çünkü pencere açıkken yeniden çizim isteyen
// bir koşul yoktu.
//
// fast ayrımı bilerek: kısa ve hızlı hareketler (yazma, silme, sarsılma) 60
// kare/sn ister; imleç yanıp sönmesi 12 kare/sn ile aynı görünür. İkisini tek
// bayrağa bağlamak ya imleci tökezletir ya da pencere açık kaldığı sürece
// makineyi boşuna meşgul ederdi.
type Animated interface {
	// Animating reports whether the dialog needs a redraw on this tick.
	// fast=true is the ~60 fps branch, fast=false the ~12 fps branch.
	Animating(fast bool) bool
}

// needsFastRedraw reports whether anything on screen wants a 60 fps redraw.
//
// İki kaynak var ve ikisi de KISA süreli hareketler: açık bir pencerenin
// kendi animasyonu (metin alanı) ve kilit ekranındaki parola alanı. İkisi de
// 80 ms'lik animasyon tikine bağlanamaz — 150 ms'lik bir "yerine oturma"
// orada iki kareye düşer ve kekemeleşir.
func (a *App) needsFastRedraw() bool {
	a.mu.Lock()
	m := a.modal
	lockBusy := a.locked && a.lockAn.animating()
	a.mu.Unlock()
	if lockBusy {
		return true
	}
	an, ok := m.(Animated)
	return ok && an.Animating(true)
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
//
// Bölüm değişiminde bir GEÇİŞ başlatılır: yeni içerik, gidilen yöne göre
// yukarıdan veya aşağıdan kayarak gelir. Yön bilgisi kullanıcıya "listede
// nereye gittim?" sorusunu yanıtlatır — sert kesme bunu söylemez.
func (a *App) gotoSection(s Section) {
	a.mu.Lock()
	old := a.section
	a.mu.Unlock()

	if old != s {
		kind := transSlideDown
		if s < old {
			kind = transSlideUp
		}
		// Kilit ALTINDA çağrılamaz: beginTransition kendi kilidini alır.
		a.beginTransition(kind)
	}

	a.mu.Lock()
	a.section = s
	a.cursor = 0
	a.dirty = true
	a.mu.Unlock()
}

// OpenModal shows a dialog and captures the blurred backdrop once.
//
// Pencere SOLUKLAŞARAK gelir: anında beliren bir pencere, ekranın
// değiştiğini değil "bir şey patladığını" hissettirir. Geçiş, arkadaki
// bulanıklığın da yumuşakça oturmasını sağlar.
//
// beginTransition KİLİT DIŞINDA çağrılır: kendi kilidini alır ve içeride
// çağırmak kilitlenmeye yol açardı.
func (a *App) OpenModal(m Modal) {
	a.beginTransition(transFade)
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
	a.beginTransition(transFade)
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

	// Süren bir geçiş her karede yeniden çizilmeli, yoksa animasyon donar.
	if a.trans != nil {
		return true
	}
	// Kendi animasyonu olan bir pencere açıksa (metin alanı imleci gibi).
	if an, ok := a.modal.(Animated); ok && an.Animating(false) {
		return true
	}
	// Tarama radarı dönüyorsa aynı şekilde.
	if a.scanBusy {
		return true
	}
	// Kilit ekranındaki nefes alan halka.
	if a.locked {
		return true
	}
	// Sihirbaz: karşılama logosu nefes alır, tarama radarı döner,
	// kurulum göstergesi ilerler.
	if a.setup != nil {
		return true
	}
	// Sunucu sihirbazı: sürüm listesi beklenirken gösterge döner.
	if a.wizard != nil && (!a.wizard.verLoaded || a.wizard.creating) {
		return true
	}
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
	// İmleç görünürken, boşta kaldığında KENDİLİĞİNDEN kaybolur; o anı
	// yakalamak için bir kare daha gerekir.
	if a.ptr.visible && time.Since(a.ptr.moved) > pointerHideAfter {
		a.ptr.visible = false
		return true
	}
	return false
}
