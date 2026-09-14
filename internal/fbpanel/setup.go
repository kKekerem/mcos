package fbpanel

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// İLK KURULUM SİHİRBAZI (OOBE)
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden yeniden yazıldı ───────────────────────────────────────────────────
// Sihirbaz eski terminal panelinde (panel/view_setup.go) yaşıyordu. Yeni
// arayüz artık her şeyi pikselle çiziyor ve kullanıcı açıkça şunu istedi:
//
//	"ilk kurulumda boot animasyonu bitince iceri zoomlanarak blur felan ile
//	 oobe nin ilk ekranı gelsin, sonra klasik gecis animasyonu olsun"
//
// Bu, sihirbazın AÇILIŞ EKRANIYLA AYNI tuvalde olmasını gerektirir. İki ayrı
// program arasında zoom/blur geçişi yapılamaz.
//
// ── Kullanıcının bildirdiği mantık hatası ───────────────────────────────────
// Aynen: "mantık hatası var, oobe de eklenmemeli, sunucu kurarken
// secilmemeli, bu acılsın mı [olmalı]".
//
// Haklı. Eski sihirbazda "PC eşleştirme" adımı, o an ağda bulunan cihazları
// listeleyip eşleştirmeye çalışıyordu. Ama İLK KURULUMDA öbür makine henüz
// KURULMAMIŞTIR — liste her zaman boştur ve kullanıcı özelliğin bozuk
// olduğunu sanır. Aynı şekilde sunucu oluştururken eş seçtirmek yanlıştır:
// eşleştirme sunucuya değil MAKİNEYE aittir.
//
// Artık OOBE'de yalnızca "bu özellik açılsın mı?" sorusu var. Eşleştirmenin
// kendisi MCOS Paylaşım ekranında yapılır (screen_peers.go).
//
// ── EŞZAMANLILIK KURALI ─────────────────────────────────────────────────────
// Setup alanlarını İKİ ayrı yürütme bağlamı kullanır:
//
//   - ANA DÖNGÜ: tuşlar, tıklamalar, Tick ve Draw (run.go).
//   - ARKA PLAN: Wi-Fi taraması, Java denetimi, playit kurulumu, disk
//     listesi, ayar kaydı ve mcos-install. Hepsi goroutine'dedir, çünkü
//     hiçbiri çizimi bloklamamalıdır (bir ağ taraması 25 saniye sürebilir).
//
// -- Yakalanan gerçek hata ---------------------------------------------------
// Bu alanlar KİLİTSİZ paylaşılıyordu. `javaNote` bir string'dir (işaretçi +
// uzunluk): Java sayfasında setupTick her 80 ms'de "kirli" diyordu, yani çizim
// döngüsü alanı tam da RPC goroutine'i onu yazarken okuyordu. Kullanıcının
// gördüğü: Java durumu bazen boş kalıyor, bazen de panel yazı tipi çizicisinin
// içinde çöküyordu. `disks` bir dilimdir (3 kelime): yırtık bir başlıkta
// len() denetimi geçiyor ama işaretçi daha kısa bir diziyi gösteriyordu —
// yani YANLIŞ DİSKE kurulum.
//
// Kural artık tek: Setup'ın HER alanı yalnızca a.mu altında okunur ve yazılır.
// Çizim yolu kareye BİR KEZ kilitli kopya alır (setupSnapshot); arka plan
// yazıları setupUpdate'ten geçer.
//
// DİKKAT: a.mu tutulurken kendi kilidini alan bir App yöntemi ÇAĞIRILAMAZ
// (Emit, Invalidate, OpenModal, Snapshot, beginTransition, applyTheme…) —
// sonuç kendi kendine kilitlenmedir.

// setupStep is one page of the wizard.
type setupStep int

const (
	stepWelcome setupStep = iota
	stepSystem
	stepIdentity // PC adı + Wi-Fi
	stepJava
	stepLook     // tema + zaman dilimi
	stepBudget   // kaynak sınırları
	stepFeatures // PC paylaşımı, playit, fare, animasyon
	stepSecurity // isteğe bağlı parola
	stepSummary
	stepInstall // diske kalıcı kurulum (isteğe bağlı)
	setupStepCount
)

// setupTitles are the page headings.
var setupTitles = [setupStepCount]string{
	stepWelcome:  "Hoş geldiniz",
	stepSystem:   "Bu bilgisayar",
	stepIdentity: "Ad ve ağ",
	stepJava:     "Java",
	stepLook:     "Görünüm",
	stepBudget:   "Kaynak sınırı",
	stepFeatures: "Özellikler",
	stepSecurity: "Güvenlik",
	stepSummary:  "Özet",
	stepInstall:  "Diske kur",
}

// budgetRAM/budgetCPU are the selectable per-server caps (0 = sınırsız).
//
// Eski sihirbazla AYNI değerler: kullanıcı yükseltmede alışkın olduğu
// seçenekleri bulmalı.
var budgetRAM = []int{0, 2048, 4096, 6144, 8192, 12288, 16384}
var budgetCPU = []int{0, 25, 50, 75, 100}

// timezones offered in the wizard (kısa tutuldu; sonradan değiştirilebilir).
var timezones = []string{
	"Europe/Istanbul", "UTC", "Europe/London", "Europe/Berlin",
	"America/New_York", "Asia/Dubai",
}

// Setup holds the wizard state.
//
// App'in İÇİNDE yaşar (a.setup) çünkü aynı tuvali, aynı tuş döngüsünü ve
// aynı geçiş animasyonlarını kullanır. Ayrı bir program olsaydı açılış
// ekranından buraya yumuşak geçiş imkânsız olurdu.
//
// Alanların TAMAMI a.mu tarafından korunur (bkz. dosya başındaki kural).
type Setup struct {
	step   setupStep
	cursor int

	// Kimlik
	pcName      string
	ssid        string
	wifiPass    string
	wifiSecured bool
	networks    []ipc.WiFiNetwork
	netNote     string

	// Görünüm
	themeIdx int
	tzIdx    int

	// Kaynak
	ramIdx int
	cpuIdx int

	// Özellikler
	sharing      bool
	playitSetup  bool
	mouse        bool
	touchpad     bool
	animations   bool
	playitNote   string
	playitOK     bool
	javaNote     string
	javaOK       bool
	javaChecking bool

	// Güvenlik
	password string
	// clearPassword, kullanıcının "Parola kullanma" dediğini söyler.
	//
	// Boş bir `password` "parola YOK" ile "parolaya DOKUNMA" arasında ayrım
	// yapamaz; sihirbaz yeniden çalıştırılabildiği için ikisi farklı şeydir.
	clearPassword bool
	// hadPassword, sihirbaz başlarken yapılandırmada parola olup olmadığıdır.
	//
	// Satır ve özet, kullanıcının SEÇİMİNİ değil KAYDEDİLECEK DURUMU
	// göstermek zorunda; bunun için mevcut durumu bilmek gerekir.
	hadPassword bool

	// Kurulum
	disks         []ipc.DiskTarget
	diskIdx       int
	disksLoading  bool
	installing    bool
	installDone   bool
	installMsg    string
	rebootCounter int

	saving bool
	// saveDone, kaydetme goroutine'inin bittiğini ANA DÖNGÜYE bildirir.
	// Adım değişimini goroutine'in kendisi yapamaz (bkz. setupSave).
	saveDone bool
	err      string
}

// NewSetup builds the wizard with sensible defaults.
func NewSetup(cfg *model.Config, st *model.SystemStatus) *Setup {
	s := &Setup{
		pcName:     "mcos-pc-1",
		themeIdx:   0,
		tzIdx:      0,
		ramIdx:     0,
		cpuIdx:     0,
		sharing:    false, // KAPALI varsayılan: ağa açık bir port dinler
		mouse:      true,
		touchpad:   true,
		animations: true,
		// playit kurulumu VARSAYILAN AÇIK: ikili zaten imajda, yalnızca
		// doğrulanıyor. Kullanıcı hesap bağlamaya zorlanmıyor — o adım
		// Tünel ekranında, istendiğinde yapılır.
		playitSetup: true,
	}
	if cfg != nil {
		if cfg.Hostname != "" {
			s.pcName = cfg.Hostname
		}
		for i, name := range fbui.ThemeOrder {
			if name == cfg.Theme {
				s.themeIdx = i
			}
		}
		for i, tz := range timezones {
			if tz == cfg.Timezone {
				s.tzIdx = i
			}
		}
		s.sharing = cfg.Cluster.Enabled
		ui := cfg.UI.Normalize()
		s.mouse, s.touchpad = ui.Mouse, ui.Touchpad
		s.animations = ui.Animations
		s.hadPassword = cfg.Security.PasswordSet()
	}
	if st != nil && st.SystemName != "" && cfg != nil && cfg.Hostname == "" {
		s.pcName = st.SystemName
	}
	return s
}

// InSetup reports whether the wizard is active.
func (a *App) InSetup() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.setup != nil
}

// StartSetup enters the wizard.
func (a *App) StartSetup() {
	// Tek çağrıda hem durum hem yapılandırma: iki ayrı Snapshot, aralarında
	// değişen bir yapılandırmayla tutarsız bir başlangıç üretebilirdi.
	st, _, cfg := a.Snapshot()
	a.mu.Lock()
	a.setup = NewSetup(cfg, st)
	a.dirty = true
	a.mu.Unlock()
	a.Emit(fbui.EventInfo, "İlk kurulum başlıyor")
}

// FinishSetup leaves the wizard and shows the panel.
func (a *App) FinishSetup() {
	a.beginTransition(transFade)
	a.mu.Lock()
	a.setup = nil
	a.dirty = true
	a.mu.Unlock()
}

// SetupNeeded reports whether first-boot configuration is pending.
func (a *App) SetupNeeded() bool {
	_, _, cfg := a.Snapshot()
	return cfg != nil && !cfg.SetupComplete
}

// setupState returns the wizard, or nil.
func (a *App) setupState() *Setup {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.setup
}

// setupUpdate applies a field change to the wizard under a.mu.
//
// Arka plandaki her yazı buradan geçer. fn YALNIZCA alan yazmalıdır: a.mu'yu
// alan bir App yöntemini çağırırsa kilit kendi kendine kilitlenir.
func (a *App) setupUpdate(s *Setup, fn func(s *Setup)) {
	if s == nil {
		return
	}
	a.mu.Lock()
	fn(s)
	a.dirty = true
	a.mu.Unlock()
}

// setupBusy reports whether a long, uninterruptible operation owns the wizard.
//
// Kaydetme diske yazarken, kurulum diski bölerken kullanıcı sayfayı
// DEĞİŞTİREMEMELİ: ikisi de yarıda bırakılamaz ve ikisinin de sonucunu
// gösterecek tek yer o sayfadır.
func (a *App) setupBusy(s *Setup) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return s.saving || s.installing
}

// ── Satır modeli ────────────────────────────────────────────────────────────

// setupRowKind identifies what a wizard row does.
type setupRowKind int

const (
	rowContinue setupRowKind = iota
	rowBack
	rowText   // metin girişi açar
	rowPick   // liste penceresi açar
	rowToggle // yerinde açar/kapatır
	rowAction // özel eylem
	rowInfo   // seçilemez bilgi satırı
)

// setupRow is one line on a wizard page.
type setupRow struct {
	kind  setupRowKind
	key   string
	label string
	value string
	on    bool
	// hint, satırın altında soluk yazılır.
	hint string
}

// rows builds the selectable rows for the current page.
//
// Kendi kilidini alır: hem ana döngüden (tuş/tıklama) hem de çizimden
// çağrılır ve okuduğu alanların yarısını arka plan goroutine'leri yazar.
func (s *Setup) rows(a *App) []setupRow {
	a.mu.Lock()
	defer a.mu.Unlock()
	return s.rowsLocked()
}

// rowsLocked builds the rows. Çağıran a.mu'yu TUTUYOR olmalıdır.
func (s *Setup) rowsLocked() []setupRow {
	switch s.step {
	case stepWelcome:
		return []setupRow{
			{kind: rowContinue, label: "Başla"},
		}

	case stepSystem:
		return []setupRow{
			{kind: rowContinue, label: "Devam"},
		}

	case stepIdentity:
		net := s.ssid
		if net == "" {
			net = "kablolu / seçilmedi"
		}
		return []setupRow{
			{kind: rowText, key: "pcname", label: "Bilgisayar adı", value: s.pcName,
				hint: "Ağda bu adla görünür."},
			{kind: rowAction, key: "wifi", label: "Kablosuz ağ", value: net,
				hint: "Kablolu bağlantı kullanıyorsanız boş bırakın."},
			{kind: rowAction, key: "wired", label: "Kablolu bağlantıyı dene",
				hint: "DHCP ile IP alır."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case stepJava:
		return []setupRow{
			{kind: rowAction, key: "java-check", label: "Java'yı denetle",
				value: s.javaNote},
			{kind: rowAction, key: "java-install", label: "Gerekli Java'yı kur",
				hint: "İnternet gerekir; çevrimdışı pakette de gelir."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case stepLook:
		return []setupRow{
			{kind: rowPick, key: "theme", label: "Tema",
				value: fbui.ThemeLabel(fbui.ThemeOrder[s.themeIdx])},
			{kind: rowPick, key: "tz", label: "Zaman dilimi",
				value: timezones[s.tzIdx]},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case stepBudget:
		return []setupRow{
			{kind: rowPick, key: "ram", label: "Sunucu başına en fazla RAM",
				value: budgetLabel(budgetRAM[s.ramIdx], "MB")},
			{kind: rowPick, key: "cpu", label: "Sunucu başına en fazla CPU",
				value: budgetLabel(budgetCPU[s.cpuIdx], "%")},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case stepFeatures:
		return []setupRow{
			{kind: rowToggle, key: "sharing", label: "PC paylaşımı", on: s.sharing,
				hint: "Ağdaki diğer MCOS cihazlarıyla iş paylaşımı. " +
					"Eşleştirme sonra, MCOS Paylaşım ekranından yapılır."},
			{kind: rowToggle, key: "playit", label: "playit tünel servisi",
				on: s.playitSetup, value: s.playitNote,
				hint: "Ajanı kurar. Hesap bağlama sonra, Tünel ekranından."},
			{kind: rowToggle, key: "mouse", label: "Fare desteği", on: s.mouse},
			{kind: rowToggle, key: "touchpad", label: "Touchpad desteği",
				on: s.touchpad},
			{kind: rowToggle, key: "anim", label: "Animasyonlar",
				on:   s.animations,
				hint: "Ekran geçişleri ve açılış animasyonu."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case stepSecurity:
		return []setupRow{
			{kind: rowAction, key: "password", label: "Panel parolası",
				value: s.passwordStateLocked(),
				hint:  "İSTEĞE BAĞLI. Diski şifrelemez; yalnızca paneli kilitler."},
			{kind: rowAction, key: "password-clear", label: "Parola kullanma"},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case stepSummary:
		return []setupRow{
			{kind: rowContinue, label: "Ayarları kaydet ve bitir"},
			{kind: rowBack, label: "Geri"},
		}

	case stepInstall:
		if s.installDone {
			return []setupRow{
				{kind: rowAction, key: "reboot", label: "Şimdi yeniden başlat"},
				{kind: rowAction, key: "finish", label: "Panele geç"},
			}
		}
		// -- Yakalanan gerçek hata ------------------------------------------
		// Kurulum SÜRERKEN de disk satırları, "Diskleri yeniden tara" ve
		// "Kurulum yapma, panele geç" satırları çizilmeye, farenin altında
		// vurgulanmaya ve TIKLANMAYA devam ediyordu. Sihirbazda tek tık bir
		// satırı çalıştırır; imleç de zaten seçilen diskin üzerinde duruyordu.
		//
		// Kullanıcının gördüğü: "birkaç dakika sürebilir" yazısına sabırsızlanıp
		// bir kez daha Enter'a basmak ya da listeye dokunmak "TÜM VERİ SİLİNECEK"
		// penceresini yeniden açıyor, "Kur" demek İKİNCİ bir mcos-install
		// başlatıyordu. mcos-install'da kilit dosyası yok: iki koşu birbirinin
		// bağlama noktalarını söküyor, aynı /tmp/mcos-install.status dosyasına
		// yazıyor ve geriye AÇILMAYAN bir disk kalıyordu. Önce biten koşu
		// installing'i temizleyip "Kurulum başarısız" yazıyor, öteki hâlâ diske
		// yazarken sayfa disk listesine geri düşüyordu.
		//
		// Artık kurulum sürerken sayfa hiçbir çalıştırılabilir satır sunmaz;
		// yıkıcı yol ayrıca runDiskInstall/confirmDiskInstall içinde de
		// kilit altında kapatılır (setupSave'in "if s.saving" ile kendini
		// koruduğu gibi).
		if s.installing {
			return []setupRow{{
				kind:  rowInfo,
				label: "Kurulum sürüyor — bu sayfadan ayrılmayın",
				hint: "Bitince sonucu ve yeniden başlatma seçeneği burada " +
					"belirir.",
			}}
		}
		rows := []setupRow{}
		for i, d := range s.disks {
			rows = append(rows, setupRow{
				kind:  rowAction,
				key:   "disk:" + strconv.Itoa(i),
				label: d.Device,
				value: fmt.Sprintf("%s · %s", bytesShort(d.SizeBytes), d.Model),
			})
		}
		rows = append(rows,
			setupRow{kind: rowAction, key: "disks-refresh", label: "Diskleri yeniden tara"},
			setupRow{kind: rowAction, key: "finish", label: "Kurulum yapma, panele geç"})
		return rows
	}
	return nil
}

// passwordStateLocked describes what will be WRITTEN, not what was typed.
//
// -- Yakalanan gerçek hata ---------------------------------------------------
// Satır ve özet yalnızca s.password'e bakıyordu: parolası olan bir kullanıcı
// sihirbazı Ayarlar'dan yeniden çalıştırıp "Parola kullanma" dediğinde ekran
// "yok" diyordu ama setupSave parolaya HİÇ dokunmuyordu (yalnızca
// `if s.password != ""` dalı vardı). Panel eski parolayla kilitlenmeye devam
// ediyordu — üstelik sihirbazı tam da unuttuğu parolayı kaldırmak için
// çalıştıran kullanıcının başka bir yolu yoktu.
//
// Çağıran a.mu'yu TUTUYOR olmalıdır.
func (s *Setup) passwordStateLocked() string {
	switch {
	case s.password != "":
		return "kurulacak"
	case s.clearPassword && s.hadPassword:
		return "kaldırılacak"
	case s.hadPassword:
		return "kurulu"
	}
	return "yok"
}

func budgetLabel(v int, unit string) string {
	if v == 0 {
		return "sınırsız"
	}
	if unit == "%" {
		return "%" + strconv.Itoa(v)
	}
	return strconv.Itoa(v) + " " + unit
}

// ── Çizim için kilitli kopya ────────────────────────────────────────────────

// setupView is one frame's copy of the wizard state the renderer reads.
//
// Çizim yolu alanları tek tek okusaydı, her okuma arka plan yazılarıyla
// yarışırdı; üstelik aynı karenin başı ve sonu FARKLI durumları gösterebilirdi
// (satırlar "kuruluyor", gövde "disk seçin"). Tek kilitli kopya ikisini de
// çözer.
type setupView struct {
	step   setupStep
	cursor int
	err    string
	rows   []setupRow

	pcName  string
	ssid    string
	netNote string

	themeIdx    int
	tzIdx       int
	ramIdx      int
	cpuIdx      int
	sharing     bool
	playitSetup bool
	mouse       bool
	touchpad    bool
	animations  bool
	password    string

	javaNote     string
	javaOK       bool
	javaChecking bool

	diskCount    int
	disksLoading bool
	installing   bool
	installDone  bool
	installMsg   string
	saving       bool
}

// setupSnapshot copies every field the renderer touches under ONE lock.
func (a *App) setupSnapshot(s *Setup) setupView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return setupView{
		step:   s.step,
		cursor: s.cursor,
		err:    s.err,
		rows:   s.rowsLocked(),

		pcName:  s.pcName,
		ssid:    s.ssid,
		netNote: s.netNote,

		themeIdx:    s.themeIdx,
		tzIdx:       s.tzIdx,
		ramIdx:      s.ramIdx,
		cpuIdx:      s.cpuIdx,
		sharing:     s.sharing,
		playitSetup: s.playitSetup,
		mouse:       s.mouse,
		touchpad:    s.touchpad,
		animations:  s.animations,
		password:    s.passwordStateLocked(),

		javaNote:     s.javaNote,
		javaOK:       s.javaOK,
		javaChecking: s.javaChecking,

		diskCount:    len(s.disks),
		disksLoading: s.disksLoading,
		installing:   s.installing,
		installDone:  s.installDone,
		installMsg:   s.installMsg,
		saving:       s.saving,
	}
}

// ── Tuşlar ──────────────────────────────────────────────────────────────────

// setupKey handles a keystroke while the wizard is active.
func (a *App) setupKey(key string) Action {
	s := a.setupState()
	if s == nil {
		return ActNone
	}
	defer a.Invalidate()

	switch key {
	case "ctrl+c":
		// Kurulumu yarıda bırakmak yerine kapatmak isteyen kullanıcı için.
		// Kurulum sürerken bile bırakılır: kilitlenmiş bir yardımcı programda
		// çıkışsız bir ekran, ikinci bir kurulumdan da kötüdür.
		return ActQuit
	case "f12":
		return ActLegacyPanel
	}

	// -- Yakalanan gerçek hata ------------------------------------------------
	// Kaydetme sürerken hiçbir tuş engellenmiyordu. Özet sayfasında Enter'a
	// basan kullanıcı, USB'ye yazma saniyeler sürerken Esc'e bastığında ana
	// döngü s.step--'i, kaydetme goroutine'i ise s.step = stepInstall'ı AYNI
	// ANDA yazıyordu. Hangisinin kazandığı rastgeleydi: kullanıcı ya
	// yapılandırma çoktan SetupComplete=true olarak yazılmışken Güvenlik
	// sayfasında kalıyor (ve her şeyi ikinci kez kaydetmek zorunda kalıyor),
	// ya da geri gittiğini sanırken sayfa "Diske kur"a atlıyordu.
	//
	// Artık adım değişimini yalnızca ANA DÖNGÜ yapar (bkz. setupTick) ve iş
	// sürerken gezinme tuşları yok sayılır.
	if a.setupBusy(s) {
		return ActNone
	}

	switch key {
	case "up", "k":
		a.setupMoveCursor(s, -1)
		return ActNone
	case "down", "j", "tab":
		a.setupMoveCursor(s, 1)
		return ActNone
	case "esc":
		// Geri: ilk sayfada hiçbir şey yapmaz. Sihirbazdan ÇIKIŞ YOK —
		// yarım yapılandırılmış bir sistem, hiç yapılandırılmamış bir
		// sistemden daha kötüdür.
		a.setupBack(s)
		return ActNone
	case "left", "h":
		a.setupAdjust(s, -1)
		return ActNone
	case "right", "l":
		a.setupAdjust(s, 1)
		return ActNone
	case "enter":
		a.mu.Lock()
		rows := s.rowsLocked()
		cur := s.cursor
		a.mu.Unlock()
		if cur >= 0 && cur < len(rows) {
			return a.setupActivate(s, rows[cur])
		}
		return ActNone
	}
	return ActNone
}

// setupMoveCursor wraps the row cursor around the current page.
func (a *App) setupMoveCursor(s *Setup, delta int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(s.rowsLocked())
	if n > 0 {
		s.cursor = (s.cursor + delta + n) % n
	}
	a.dirty = true
}

// setupAdjust changes a value in place with the arrow keys.
//
// Sol/sağ ile değiştirmek, her seçenek için pencere açmaktan hızlıdır ve
// klavye-yalnız kullanımda doğal gelir.
func (a *App) setupAdjust(s *Setup, delta int) {
	a.mu.Lock()
	rows := s.rowsLocked()
	if s.cursor < 0 || s.cursor >= len(rows) {
		a.mu.Unlock()
		return
	}
	r := rows[s.cursor]
	theme := ""
	switch r.key {
	case "theme":
		s.themeIdx = wrap(s.themeIdx+delta, len(fbui.ThemeOrder))
		theme = fbui.ThemeOrder[s.themeIdx]
	case "tz":
		s.tzIdx = wrap(s.tzIdx+delta, len(timezones))
	case "ram":
		s.ramIdx = wrap(s.ramIdx+delta, len(budgetRAM))
	case "cpu":
		s.cpuIdx = wrap(s.cpuIdx+delta, len(budgetCPU))
	}
	a.dirty = true
	a.mu.Unlock()

	// applyTheme ve setupToggle KİLİT DIŞINDA: ikisi de a.mu'yu kendisi alır.
	if theme != "" {
		a.applyTheme(theme)
		return
	}
	if r.kind == rowToggle {
		a.setupToggle(s, r.key)
	}
}

func wrap(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i%n + n) % n
}

func (a *App) setupToggle(s *Setup, key string) {
	a.mu.Lock()
	checkPlayit := false
	switch key {
	case "sharing":
		s.sharing = !s.sharing
	case "playit":
		s.playitSetup = !s.playitSetup
		checkPlayit = s.playitSetup
	case "mouse":
		s.mouse = !s.mouse
	case "touchpad":
		s.touchpad = !s.touchpad
	case "anim":
		s.animations = !s.animations
	}
	a.dirty = true
	a.mu.Unlock()

	if checkPlayit {
		a.checkPlayit(s)
	}
}

// setupActivate performs a row's action.
func (a *App) setupActivate(s *Setup, r setupRow) Action {
	switch r.kind {
	case rowContinue:
		a.setupNext(s)
		return ActNone
	case rowBack:
		a.setupBack(s)
		return ActNone
	case rowToggle:
		a.setupToggle(s, r.key)
		return ActNone
	case rowPick:
		a.setupPick(s, r.key)
		return ActNone
	case rowText:
		a.setupTextInput(s, r.key)
		return ActNone
	case rowAction:
		return a.setupAction(s, r.key)
	}
	return ActNone
}

func (a *App) setupNext(s *Setup) {
	a.mu.Lock()
	step := s.step
	busy := s.saving || s.installing
	a.mu.Unlock()
	if busy {
		return
	}
	if step == stepSummary {
		a.setupSave(s)
		return
	}
	if step+1 >= setupStepCount {
		a.FinishSetup()
		return
	}
	// beginTransition KİLİT DIŞINDA: kendi kilidini alır.
	a.beginTransition(transSlideDown)
	a.setupUpdate(s, func(s *Setup) {
		s.step++
		s.cursor = 0
		s.err = ""
	})
	a.setupEnter(s)
}

func (a *App) setupBack(s *Setup) {
	a.mu.Lock()
	// Kurulum ya da kaydetme sürerken geri gitmek, işin sonucunu gösterecek
	// TEK sayfayı terk etmek demektir (fare ile "Geri" düğmesine basmak da
	// buraya düşer).
	stay := s.step == stepWelcome || s.saving || s.installing
	a.mu.Unlock()
	if stay {
		return
	}
	a.beginTransition(transSlideUp)
	a.setupUpdate(s, func(s *Setup) {
		s.step--
		s.cursor = 0
		s.err = ""
	})
}

// setupEnter runs the work a page needs when it opens.
func (a *App) setupEnter(s *Setup) {
	a.mu.Lock()
	step := s.step
	playit := s.playitSetup
	a.mu.Unlock()

	switch step {
	case stepJava:
		a.checkJava(s)
	case stepFeatures:
		if playit {
			a.checkPlayit(s)
		}
	case stepInstall:
		a.loadDisks(s)
	}
}

// ── Adım eylemleri ──────────────────────────────────────────────────────────

func (a *App) setupTextInput(s *Setup, key string) {
	if key != "pcname" {
		return
	}
	a.mu.Lock()
	cur := s.pcName
	a.mu.Unlock()

	a.OpenModal(NewTextModal("Bilgisayar adı",
		"Bu makine ağda bu adla görünecek.",
		func(app *App, v string) {
			name := strings.TrimSpace(v)
			app.setupUpdate(s, func(s *Setup) { s.pcName = name })
		}).
		WithValue(cur).
		WithMaxLen(32).
		WithHint("Harf, rakam ve tire kullanın.").
		WithValidate(validHostname))
}

// validHostname rejects names the network stack cannot use.
//
// Doğrulama BURADA yapılır: geçersiz bir ad kaydedilirse sethostname sessizce
// başarısız olur ve kullanıcı adın neden değişmediğini asla anlayamaz.
func validHostname(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Ad boş olamaz"
	}
	if len(s) > 32 {
		return "Ad en fazla 32 karakter olabilir"
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-'
		if !ok {
			return "Yalnızca harf, rakam ve tire kullanılabilir"
		}
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return "Ad tire ile başlayamaz veya bitemez"
	}
	return ""
}

func (a *App) setupPick(s *Setup, key string) {
	a.mu.Lock()
	themeIdx, tzIdx, ramIdx, cpuIdx := s.themeIdx, s.tzIdx, s.ramIdx, s.cpuIdx
	a.mu.Unlock()

	switch key {
	case "theme":
		items := make([]ListItem, 0, len(fbui.ThemeOrder))
		for i, name := range fbui.ThemeOrder {
			items = append(items, ListItem{
				Label: fbui.ThemeLabel(name), Detail: name,
				Current: i == themeIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("Tema", "Arayüz vurgu rengi.", items,
			func(app *App, _ int, it ListItem) bool {
				idx := it.Value.(int)
				app.setupUpdate(s, func(s *Setup) { s.themeIdx = idx })
				// applyTheme kendi kilidini alır: setupUpdate'ten SONRA.
				app.applyTheme(fbui.ThemeOrder[idx])
				return true
			}))

	case "tz":
		items := make([]ListItem, 0, len(timezones))
		for i, tz := range timezones {
			items = append(items, ListItem{
				Label: tz, Current: i == tzIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("Zaman dilimi",
			"Günlük kayıtları ve yedek adları bu saate göre yazılır.", items,
			func(app *App, _ int, it ListItem) bool {
				idx := it.Value.(int)
				app.setupUpdate(s, func(s *Setup) { s.tzIdx = idx })
				return true
			}))

	case "ram":
		items := make([]ListItem, 0, len(budgetRAM))
		for i, v := range budgetRAM {
			items = append(items, ListItem{
				Label: budgetLabel(v, "MB"), Current: i == ramIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("Sunucu başına en fazla RAM",
			"Bir sunucunun ayırabileceği en yüksek bellek.", items,
			func(app *App, _ int, it ListItem) bool {
				idx := it.Value.(int)
				app.setupUpdate(s, func(s *Setup) { s.ramIdx = idx })
				return true
			}))

	case "cpu":
		items := make([]ListItem, 0, len(budgetCPU))
		for i, v := range budgetCPU {
			items = append(items, ListItem{
				Label: budgetLabel(v, "%"), Current: i == cpuIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("Sunucu başına en fazla CPU",
			"Bir çekirdeğin yüzdesi olarak üst sınır.", items,
			func(app *App, _ int, it ListItem) bool {
				idx := it.Value.(int)
				app.setupUpdate(s, func(s *Setup) { s.cpuIdx = idx })
				return true
			}))
	}
}

func (a *App) setupAction(s *Setup, key string) Action {
	// Kurulum sürerken hiçbir eylem çalışmaz. Satırlar zaten gizlenmiştir
	// (rowsLocked); bu, geriye kalan her yolu da (eski bir tıklama bölgesi,
	// bir kısayol) kapatan ikinci settir.
	if a.setupBusy(s) {
		return ActNone
	}

	switch key {
	case "wifi":
		a.setupWiFi(s)
	case "wired":
		a.connectWired()
	case "java-check":
		a.checkJava(s)
	case "java-install":
		a.setupInstallJava(s)
	case "password":
		a.setupPassword(s)
	case "password-clear":
		// Niyet AÇIKÇA saklanır: boş bir parola "kaldır" mı "dokunma" mı
		// demek, yalnızca buradan anlaşılır (bkz. passwordStateLocked).
		a.setupUpdate(s, func(s *Setup) {
			s.password = ""
			s.clearPassword = true
		})
		a.Emit(fbui.EventInfo, "Parola kullanılmayacak")
	case "disks-refresh":
		a.loadDisks(s)
	case "finish":
		a.FinishSetup()
	case "reboot":
		return ActReboot
	default:
		if strings.HasPrefix(key, "disk:") {
			idx, err := strconv.Atoi(strings.TrimPrefix(key, "disk:"))
			if err == nil {
				a.confirmDiskInstall(s, idx)
			}
		}
	}
	return ActNone
}

// setupWiFi scans and shows the network picker with the radar animation.
func (a *App) setupWiFi(s *Setup) {
	if a.offline() {
		return
	}
	if a.scanning() {
		return
	}
	a.setScanning(true)
	a.setScanNote("Kablosuz ağlar aranıyor…")

	go func() {
		nets, err := a.cl.WiFiScan()
		a.setScanning(false)
		a.setScanNote("")
		if err != nil {
			a.Fail("tarama yapılamadı", err)
			a.setupUpdate(s, func(s *Setup) {
				s.netNote = "Tarama başarısız — kablolu bağlantıyı deneyin."
			})
			return
		}

		a.mu.Lock()
		s.networks = nets
		cur := s.ssid
		a.dirty = true
		a.mu.Unlock()

		items := make([]ListItem, 0, len(nets))
		for _, n := range nets {
			badge, kind := "açık", fbui.EventInfo
			if n.Secured {
				badge, kind = "korumalı", fbui.EventWarn
			}
			items = append(items, ListItem{
				Label:     n.SSID,
				Detail:    fmt.Sprintf("%%%d", n.Signal),
				Badge:     badge,
				BadgeKind: kind,
				Current:   n.SSID == cur,
				Value:     n,
			})
		}

		// -- Yakalanan gerçek hata ---------------------------------------
		// Tarama arka planda sürerken sihirbaz KULLANILABİLİR kalır — öyle
		// olmalı da. Ama sonuç geldiğinde OpenModal koşulsuz çağrılıyordu ve
		// OpenModal a.modal'ı denetlemeden EZİYOR (app.go).
		//
		// Kullanıcının gördüğü: "Ad ve ağ" sayfasında "Kablosuz ağ"a basıp
		// radarı başlatmak, sonra bir satır yukarı çıkıp "Bilgisayar adı"na
		// girip yazmaya başlamak. nmcli birkaç saniye sonra dönünce metin
		// penceresi yazılanlarla birlikte yok oluyor, sonraki harfler ağ
		// listesine gidiyordu. Sihirbaz bitmişse (a.setup değişmişse) liste
		// büsbütün ilgisiz bir ekranın üstüne açılıyordu.
		if !a.wifiPickerWanted(s) {
			a.Emit(fbui.EventInfo, fmt.Sprintf(
				"%d ağ bulundu — 'Kablosuz ağ' satırından seçebilirsiniz",
				len(nets)))
			return
		}

		a.OpenModal(NewListModal("Kablosuz ağ seç",
			fmt.Sprintf("%d ağ bulundu.", len(nets)), items,
			func(app *App, _ int, it ListItem) bool {
				net := it.Value.(ipc.WiFiNetwork)
				app.setupUpdate(s, func(s *Setup) {
					s.ssid = net.SSID
					s.wifiSecured = net.Secured
					if !net.Secured {
						s.wifiPass = ""
					}
				})
				if net.Secured {
					app.OpenModal(NewPasswordModal(net.SSID,
						func(app2 *App, pass string) {
							app2.setupUpdate(s, func(s *Setup) {
								s.wifiPass = pass
							})
							app2.setupApplyWiFi(s)
						}))
					return false // parola penceresi zaten açıldı
				}
				app.setupApplyWiFi(s)
				return true
			}).WithEmpty("Ağ bulunamadı. Kablolu bağlantıyı deneyin."))
	}()
}

// wifiPickerWanted reports whether the network list may still be shown.
//
// ÜÇ koşul birden: sihirbaz hâlâ AYNI sihirbaz olmalı, kullanıcı hâlâ ağ
// sayfasında olmalı ve o arada başka bir pencere AÇMAMIŞ olmalı.
func (a *App) wifiPickerWanted(s *Setup) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.setup == s && a.modal == nil && s.step == stepIdentity
}

// setupApplyWiFi connects right away so the Java step can download.
//
// ŞART: Java indirmesi internet ister. Ağı özet ekranında uygulamak, Java
// adımında "internet yok" hatası vermek demektir — kullanıcı da neden
// olduğunu anlamaz.
func (a *App) setupApplyWiFi(s *Setup) {
	if a.offline() {
		return
	}
	a.mu.Lock()
	ssid, pass := s.ssid, s.wifiPass
	a.mu.Unlock()

	a.Emit(fbui.EventBusy, ssid+" ağına bağlanılıyor…")
	go func() {
		if err := a.cl.WiFiApply(ssid, pass); err != nil {
			a.Fail(ssid+" bağlantısı başarısız", err)
			a.setupUpdate(s, func(s *Setup) {
				s.netNote = "Bağlanılamadı: " + err.Error()
			})
			return
		}
		a.setupUpdate(s, func(s *Setup) { s.netNote = ssid + " bağlandı" })
		a.Emit(fbui.EventOK, ssid+" ağına bağlanıldı")
	}()
}

func (a *App) checkJava(s *Setup) {
	if a.offline() {
		a.setupUpdate(s, func(s *Setup) { s.javaNote = "daemon bağlantısı yok" })
		return
	}
	a.setupUpdate(s, func(s *Setup) { s.javaChecking = true })

	go func() {
		rt, err := a.cl.JavaList()
		// Sonuç ve "denetleniyor" bayrağı TEK yazıda yayımlanır: ikisini ayrı
		// ayrı yazmak, çizimin bir kareyi "denetim bitti ama not eski" hâlinde
		// yakalamasına izin verirdi.
		a.setupUpdate(s, func(s *Setup) {
			s.javaChecking = false
			switch {
			case err != nil:
				s.javaNote = "denetlenemedi: " + err.Error()
			case len(rt) == 0:
				s.javaOK = false
				s.javaNote = "kurulu değil"
			default:
				var vs []string
				for _, r := range rt {
					vs = append(vs, strconv.Itoa(r.Major))
				}
				s.javaOK = true
				s.javaNote = "kurulu: " + strings.Join(vs, ", ")
			}
		})
	}()
}

func (a *App) setupInstallJava(s *Setup) {
	if a.offline() {
		return
	}
	st, _, _ := a.Snapshot()
	if st != nil && !st.Net.Internet {
		a.Emit(fbui.EventError,
			"İnternet yok — önce ağ adımına dönüp bağlanın")
		return
	}
	// 21, Minecraft 1.20.5+ için gereken sürümdür; sihirbazda tek seçenek
	// sunmak kullanıcıyı "hangisi?" sorusundan kurtarır.
	const major = 21
	a.Emit(fbui.EventBusy, "Java 21 indiriliyor…")
	go func() {
		rt, err := a.cl.JavaInstall(major)
		if err != nil {
			a.Fail("Java kurulamadı", err)
			a.setupUpdate(s, func(s *Setup) { s.javaNote = "kurulum başarısız" })
			return
		}
		a.setupUpdate(s, func(s *Setup) {
			s.javaOK = true
			s.javaNote = "kuruldu: " + rt.Version
		})
		a.Emit(fbui.EventOK, "Java "+strconv.Itoa(rt.Major)+" kuruldu")
	}()
}

func (a *App) checkPlayit(s *Setup) {
	if a.offline() {
		a.setupUpdate(s, func(s *Setup) { s.playitNote = "daemon bağlantısı yok" })
		return
	}
	go func() {
		msg, err := a.cl.PlayitInstall()
		if err != nil {
			a.setupUpdate(s, func(s *Setup) {
				s.playitOK = false
				s.playitNote = "imajda yok"
			})
			return
		}
		note := strings.TrimSpace(msg)
		a.setupUpdate(s, func(s *Setup) {
			s.playitOK = true
			s.playitNote = note
		})
	}()
}

func (a *App) setupPassword(s *Setup) {
	a.OpenModal(NewTextModal("Panel parolası",
		"İsteğe bağlı. Boş bırakırsanız parola kullanılmaz.",
		func(app *App, pw string) {
			pw = strings.TrimSpace(pw)
			if pw == "" {
				// Boş bırakmak da AÇIK bir karardır: "parola kullanma" ile
				// aynı anlama gelir ve kayıtlı parolayı kaldırır.
				app.setupUpdate(s, func(s *Setup) {
					s.password = ""
					s.clearPassword = true
				})
				return
			}
			// İki kez sor: görünmeyen bir alanda tek harflik hata,
			// kullanıcıyı kendi panelinden kilitler.
			app.OpenModal(NewTextModal("Parolayı doğrula",
				"Aynı parolayı bir kez daha girin.",
				func(app2 *App, again string) {
					if again != pw {
						app2.Emit(fbui.EventError, "Parolalar eşleşmedi")
						return
					}
					app2.setupUpdate(s, func(s *Setup) {
						s.password = pw
						s.clearPassword = false
					})
					app2.Emit(fbui.EventOK, "Parola ayarlandı")
				}).Masked().WithOK("Kaydet").
				WithValidate(func(v string) string {
					if v != pw {
						return "Parolalar eşleşmiyor"
					}
					return ""
				}))
		}).
		Masked().
		WithOK("Devam").
		WithHint("En az " + strconv.Itoa(model.MinPasswordLen) + " karakter.").
		WithValidate(func(v string) string {
			if v == "" {
				return ""
			}
			// KIRPILMIS uzunluk: model de oyle deger biciyor. Ikisi ayrismis
			// olsaydi arayuz dort boslugu kabul eder, model onu reddederdi.
			if len([]rune(strings.TrimSpace(v))) < model.MinPasswordLen {
				return "En az " + strconv.Itoa(model.MinPasswordLen) + " karakter"
			}
			return ""
		}))
}

// ── Kaydetme ────────────────────────────────────────────────────────────────

// setupSave writes every collected setting, then moves to the install page.
//
// SIRA KRİTİK: önce kaydet, sonra kur. Eski sihirbazda kurulum önce
// çalışıyordu ve doSetupSave hiç çağrılmıyordu — kullanıcının girdiği her
// şey kayboluyordu. (bkz. panel/view_setup.go üstündeki not.)
func (a *App) setupSave(s *Setup) {
	if a.offline() {
		a.setupUpdate(s, func(s *Setup) {
			s.err = "daemon bağlantısı yok — ayarlar kaydedilemez"
		})
		return
	}

	// Snapshot KİLİT DIŞINDA: kendi kilidini alır.
	_, _, cur := a.Snapshot()

	a.mu.Lock()
	if s.saving {
		a.mu.Unlock()
		return // arka arkaya Enter: aynı yapılandırmayı iki kez kaydetme
	}
	s.saving = true
	s.err = ""
	// Toplanan her değerin KOPYASI alınır: yapılandırma kilit DIŞINDA kurulur,
	// çünkü SetPassword (PBKDF2) yüz binlerce tur döner ve kilidi o kadar süre
	// tutmak çizim döngüsünü dondururdu.
	pcName := s.pcName
	themeIdx, tzIdx, ramIdx, cpuIdx := s.themeIdx, s.tzIdx, s.ramIdx, s.cpuIdx
	sharing, playit := s.sharing, s.playitSetup
	mouse, touchpad, animations := s.mouse, s.touchpad, s.animations
	ssid, wifiPass := s.ssid, s.wifiPass
	password, clearPassword := s.password, s.clearPassword
	a.dirty = true
	a.mu.Unlock()

	a.Emit(fbui.EventBusy, "Ayarlar kaydediliyor…")

	next := model.DefaultConfig()
	if cur != nil {
		c := *cur
		next = &c
	}

	next.Hostname = pcName
	next.Theme = fbui.ThemeOrder[themeIdx]
	next.Timezone = timezones[tzIdx]
	next.Budget.MaxServerRAMMB = budgetRAM[ramIdx]
	next.Budget.MaxServerCPUPercent = budgetCPU[cpuIdx]
	next.Cluster.Enabled = sharing
	// -- Yakalanan gercek hata --------------------------------------------
	// Burasi eskiden adi YALNIZCA bos oldugunda yaziyordu. Ama `next`
	// DefaultConfig'ten (ya da mevcut yapilandirmadan) turetiliyor ve
	// DefaultConfig NodeName'i "mcos-1" olarak sabitliyor: yani kosul ASLA
	// bos olmuyordu ve kullanicinin girdigi ad hicbir zaman yazilmiyordu.
	//
	// Sonucu yalnizca kozmetik degildi: kume kimligi de bu ada bakiyordu,
	// dolayisiyla iki taze makine de kendine "mcos-1" deyip birbirini
	// "kendisi" saniyordu. (Kimlik artik ayri bir degerdir -- bkz.
	// internal/cluster/identity.go -- ama ADIN da dogru olmasi gerekir:
	// kullanici es listesinde hangi makine oldugunu ondan anlar.)
	if n := strings.TrimSpace(pcName); n != "" {
		next.Cluster.NodeName = n
	}
	next.WAN.Autostart = playit
	// Sihirbazin SORMADIGI alanlar korunur.
	//
	// Eskiden burada sifirdan bir UIConfig kuruluyordu ve TapToClick ile
	// PointerSpeed sabit degerlere eziliyordu. Kurulum yeniden calisabilir
	// (donanim degisikligi yeniden kuruluma yol acar); o durumda
	// kullanicinin ayarladigi imlec hizi sessizce 100'e donerdi.
	ui := next.UI
	ui.Mouse = mouse
	ui.Touchpad = touchpad
	ui.Animations = animations
	ui.BootAnimation = animations
	if ui.PointerSpeed <= 0 {
		ui.PointerSpeed = model.DefaultUI().PointerSpeed
	}
	next.UI = ui.Normalize()
	if ssid != "" {
		next.WiFiSSID = ssid
		next.WiFiPassword = wifiPass
	}
	// -- Yakalanan gerçek hata --------------------------------------------
	// Burada yalnızca `if password != ""` dalı vardı. "Parola kullanma" diyen
	// kullanıcının seçimi hiçbir yere yazılmıyordu: özet "Panel parolası: yok"
	// diyor, kayıttan sonra panel ESKİ parolayla kilitleniyordu. Artık
	// "kaldır" niyeti de yazılıyor (SetPassword("") özeti ve tuzu siler).
	switch {
	case password != "":
		if err := next.Security.SetPassword(password); err != nil {
			a.setupUpdate(s, func(s *Setup) {
				s.saving = false
				s.err = "parola ayarlanamadı: " + err.Error()
			})
			return
		}
	case clearPassword:
		if err := next.Security.SetPassword(""); err != nil {
			a.setupUpdate(s, func(s *Setup) {
				s.saving = false
				s.err = "parola kaldırılamadı: " + err.Error()
			})
			return
		}
	}
	next.SetupComplete = true
	savedPassword := next.Security.PasswordSet()

	go func() {
		if err := a.cl.UpdateConfig(next); err != nil {
			a.setupUpdate(s, func(s *Setup) {
				s.saving = false
				s.err = "ayarlar kaydedilemedi: " + err.Error()
			})
			a.Fail("ayarlar kaydedilemedi", err)
			return
		}
		a.SetConfig(next)
		// USB'yi kalıcı yap: ayarların yeniden başlatmayı atlatması için
		// şart. Başarısızlığı ölümcül değil (diske kurulum zaten ayrı).
		if _, err := a.cl.Persist(""); err != nil {
			a.Emit(fbui.EventWarn, "USB kalıcı yapılamadı: "+err.Error())
		}
		a.Emit(fbui.EventOK, "Ayarlar kaydedildi")

		// -- Yakalanan gerçek hata ------------------------------------------
		// Eskiden adım değişimini BU goroutine yapıyordu
		// (s.step = stepInstall; s.cursor = 0). Ana döngü aynı anda Esc ile
		// s.step--'i yazabiliyordu ve biri ötekini siliyordu: yapılandırma
		// SetupComplete=true olarak yazılmış olmasına rağmen sihirbaz Güvenlik
		// sayfasında kalıyor, kullanıcı her şeyi ikinci kez kaydetmek zorunda
		// kalıyordu (ya da tersi: geri gittiğini sanırken "Diske kur"a
		// atlıyordu).
		//
		// Artık yalnızca bir BAYRAK yazılır; sayfa değişimini ana döngü
		// yapar (setupTick → finishSetupSave).
		a.setupUpdate(s, func(s *Setup) {
			s.saving = false
			s.saveDone = true
			// Kaydedilen durum artık yapılandırmadadır: satır ve özet bundan
			// sonra "kurulu"/"yok" derken buna baksın.
			s.hadPassword = savedPassword
			s.password = ""
			s.clearPassword = false
		})
	}()
}

// finishSetupSave moves the wizard to the install page. ANA DÖNGÜDE çalışır.
func (a *App) finishSetupSave(s *Setup) {
	a.beginTransition(transSlideDown)
	a.setupUpdate(s, func(s *Setup) {
		s.step = stepInstall
		s.cursor = 0
		s.err = ""
	})
	a.setupEnter(s)
}

// ── Diske kurulum ───────────────────────────────────────────────────────────

func (a *App) loadDisks(s *Setup) {
	if a.offline() {
		return
	}
	a.setupUpdate(s, func(s *Setup) { s.disksLoading = true })

	go func() {
		disks, err := a.cl.Disks()
		a.setupUpdate(s, func(s *Setup) {
			s.disksLoading = false
			if err != nil {
				s.err = "diskler listelenemedi: " + err.Error()
				return
			}
			s.disks = disks
		})
	}()
}

func (a *App) confirmDiskInstall(s *Setup, idx int) {
	a.mu.Lock()
	// Kurulum sürerken (ya da bittikten sonra) onay penceresi HİÇ açılmaz:
	// açılan pencerenin "Kur" düğmesi ikinci bir mcos-install başlatırdı.
	// Dizin denetimi ve disk kopyası da KİLİT ALTINDA: s.disks'i arka planda
	// loadDisks yazıyor ve kilitsiz okunan bir dilim başlığı yırtılabilir.
	if s.installing || s.installDone || idx < 0 || idx >= len(s.disks) {
		a.mu.Unlock()
		return
	}
	d := s.disks[idx]
	a.mu.Unlock()

	a.OpenModal(NewConfirmModal("MCOS'u diske kur?",
		[]string{
			d.Device + " üzerindeki TÜM VERİ SİLİNECEK.",
			fmt.Sprintf("%s · %s", bytesShort(d.SizeBytes), d.Model),
			"Kurulumdan sonra sistem diskten açılır ve RAM'de değil,",
			"diskte kalıcı olarak çalışır.",
		},
		"Kur", true,
		func(app *App) { app.runDiskInstall(s, d) }))
}

// runDiskInstall launches the installer helper.
//
// Panel bölüm açmaz, dosya sistemi oluşturmaz: bunlar mcos-install'ın
// işidir ve ayrı bir program olarak SINANABİLİR. Panel yalnızca çağırır ve
// çıktısını gösterir.
func (a *App) runDiskInstall(s *Setup, d ipc.DiskTarget) {
	// -- Yakalanan gerçek hata --------------------------------------------
	// Bu işlev hiçbir şey denetlemiyordu: ikinci bir çağrı ikinci bir
	// mcos-install başlatıyordu. mcos-install'da kilit dosyası yok; iki koşu
	// aynı diski bölüyor, birbirinin bağlama noktalarını söküyor ve aynı
	// /tmp/mcos-install.status dosyasına yazıyordu. Kullanıcının gördüğü:
	// "Kurulum başarısız" (önce biten koşudan) ve AÇILMAYAN bir disk.
	//
	// Bayrak setupSave'in "if s.saving" kalıbıyla aynı: sınama ve kurma TEK
	// kilit altında yapılır, yoksa iki tıklama arasında yine sızabilirdi.
	a.mu.Lock()
	if s.installing || s.installDone {
		a.mu.Unlock()
		return
	}
	s.installing = true
	s.installMsg = "Kurulum başladı…"
	a.dirty = true
	a.mu.Unlock()

	a.Emit(fbui.EventBusy, d.Device+" diskine kuruluyor…")

	go func() {
		out, err := runHelper("mcos-install", "--device", d.Device, "--yes")
		if err != nil {
			a.setupUpdate(s, func(s *Setup) {
				s.installing = false
				s.installMsg = "Kurulum başarısız: " + lastLine(out)
			})
			a.Fail("kurulum başarısız", err)
			return
		}
		msg := lastLine(out)
		if msg == "" {
			msg = "Kurulum tamamlandı."
		}
		// İmleç BURADA sıfırlanmaz. s.cursor'ı kilit ALMADAN yazan bir yol
		// var (tıklama: pointer.go, dispatchClick) ve o ana döngüde çalışır;
		// arka plandan da yazmak, kilitli/kilitsiz karışık bir erişim — yani
		// gerçek bir veri yarışı — olurdu. Satır kümesi değiştiği için imleci
		// aralığa çekme işini ana döngü üstlenir (setupTick).
		a.setupUpdate(s, func(s *Setup) {
			s.installing = false
			s.installDone = true
			s.installMsg = msg
		})
		a.Emit(fbui.EventOK, "Kurulum tamamlandı — yeniden başlatabilirsiniz")
	}()
}

// lastLine returns the last non-empty line of a helper's output.
//
// Yardımcı programlar çok satır yazar; kullanıcıya EN SON söylediği şey
// gösterilmeli — genelde sonucu odur.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// setupTick advances the wizard's own timers (install countdown).
//
// ANA DÖNGÜDE çalışır (run.go). Arka planın hazırladığı sayfa değişimi de
// burada uygulanır: adımı yazan tek yürütme bağlamı bu olmalıdır.
func (a *App) setupTick() bool {
	s := a.setupState()
	if s == nil {
		return false
	}

	a.mu.Lock()
	saveDone := s.saveDone
	if saveDone {
		s.saveDone = false
	}
	busy := s.installing || s.disksLoading || s.javaChecking || s.saving
	counting := s.installDone && s.rebootCounter > 0
	if counting {
		s.rebootCounter--
	}
	// Arka planda değişen bir sayfa (disk listesi geldi, kurulum bitti)
	// satır sayısını düşürebilir ve imleç listenin DIŞINDA kalabilir: o
	// hâlde Enter hiçbir şey yapmaz ve kullanıcı sihirbazın kilitlendiğini
	// sanar. Kırpma ANA DÖNGÜDE yapılır — s.cursor'a kilitsiz dokunan tek
	// yol da (tıklama, pointer.go) burada çalışır.
	if n := len(s.rowsLocked()); n > 0 && s.cursor >= n {
		s.cursor = 0
		a.dirty = true
	}
	a.mu.Unlock()

	if saveDone {
		a.finishSetupSave(s)
		return true
	}
	return busy || counting
}

// setupElapsed is used by the welcome page's animation.
func setupElapsed(start time.Time) float64 {
	return time.Since(start).Seconds()
}
