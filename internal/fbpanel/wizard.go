package fbpanel

import (
	"fmt"
	"strconv"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// SUNUCU OLUŞTURMA SİHİRBAZI
// ════════════════════════════════════════════════════════════════════════════
//
// Eski terminal panelindeki sihirbazın (panel/view_wizard.go) yeni arayüze
// taşınmış hâli. `n` tuşu artık "henüz taşınmadı" demiyor.
//
// ── Kullanıcının bildirdiği mantık hatası ───────────────────────────────────
// "sunucu kurarken secilmemli" — sunucu oluştururken EŞ CİHAZ seçtirilmemeli.
// Eşleştirme sunucuya değil MAKİNEYE aittir. Burada yalnızca tek bir anahtar
// var: "PC paylaşımı kullanılsın mı?". Hangi cihazla paylaşılacağına daemon
// karar verir (uygun eş yoksa işler yerelde çalışır).
//
// ── Neden Setup ile aynı desen? ─────────────────────────────────────────────
// İki sihirbazın farklı davranması (birinde Esc geri, diğerinde iptal;
// birinde sol/sağ değer değiştirir, diğerinde değiştirmez) kullanıcıyı her
// seferinde yeniden öğrenmeye zorlar. Sayfa + satır modeli ikisinde de aynı.
//
// ── KİLİT SÖZLEŞMESİ (a.mu) ─────────────────────────────────────────────────
// Sihirbazın alanlarının ÇOĞU yalnızca çalışma döngüsünden (tuş, fare, Draw)
// okunup yazılır ve bunlar tek bir goroutine olduğu için kilit istemez.
// AMA iki iş arka planda döner: sürüm listesi isteği ve "oluştur" çağrısı.
// Onlarla paylaşılan alanlar şunlardır ve HEPSİ a.mu altında okunup yazılır:
//
//	versions, verIdx, verLoaded, verNote, verWant, verGen, verFetching
//	softwareIdx, manualVer   (arka plan yalnızca OKUR; yazan kilidi alır)
//	createDone, createErr    (arka planın sonuç bıraktığı kutu)
//
// creating ve err bu listede DEĞİLDİR: onları artık yalnızca çalışma döngüsü
// yazar (bkz. drainWizard). Sebebi, çizim yolunun (setup_draw.go içindeki
// drawWizard) bu ikisini kilitsiz okumasıdır.
//
// DİKKAT: a.mu tutulurken a.Emit / a.Invalidate / a.beginTransition /
// a.CloseWizard / a.gotoSection ÇAĞRILAMAZ — hepsi kilidi kendisi alır ve
// kilitlenmeye yol açar. Kilit altında yalnızca "…Locked" son ekli ya da
// doğrudan alan yazan yardımcılar çağrılır.

// wizStep is one page of the create-server wizard.
type wizStep int

const (
	wizTemplate wizStep = iota
	wizIdentity
	wizVersion
	wizSoftware
	wizNetwork
	wizGameplay
	wizResources
	wizLocation
	wizEULA
	wizConfirm
	wizStepCount
)

var wizTitles = [wizStepCount]string{
	wizTemplate:  "Şablon",
	wizIdentity:  "Ad",
	wizVersion:   "Sürüm",
	wizSoftware:  "Altyapı",
	wizNetwork:   "Ağ",
	wizGameplay:  "Oyun ayarları",
	wizResources: "Kaynak",
	wizLocation:  "Kurulum yeri",
	wizEULA:      "Lisans (EULA)",
	wizConfirm:   "Özet",
}

func wizSubtitle(s wizStep) string {
	switch s {
	case wizTemplate:
		return "Hazır bir başlangıç seçin; sonraki adımlarda değiştirebilirsiniz."
	case wizIdentity:
		return "Sunucunun panelde görünecek adı."
	case wizVersion:
		return "Hangi Minecraft sürümü çalışacak?"
	case wizSoftware:
		return "Eklenti mi mod mu yükleyeceğini bu belirler."
	case wizNetwork:
		return "Port ve görüş mesafeleri."
	case wizGameplay:
		return "server.properties'e yazılacak oyun kuralları."
	case wizResources:
		return "Bu sunucunun kullanabileceği kaynaklar."
	case wizLocation:
		return "Nereye kurulacak ve açılışta ne yapacak."
	case wizEULA:
		return "Mojang Son Kullanıcı Lisans Sözleşmesi."
	case wizConfirm:
		return "Son bir kez bakın; onaydan sonra kurulum başlar."
	}
	return ""
}

// ── Seçenek listeleri (eski sihirbazla AYNI) ────────────────────────────────

var wizRAMChoices = []int{1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384}
var wizGamemodes = []string{"survival", "creative", "adventure", "spectator"}
var wizDifficulties = []string{"peaceful", "easy", "normal", "hard"}

// wizTemplateItem is a quick-start preset.
type wizTemplateItem struct {
	label      string
	custom     bool
	software   model.Software
	gamemode   string
	difficulty string
	pvp        bool
	whitelist  bool
	hardcore   bool
	note       string
}

var wizTemplates = []wizTemplateItem{
	{label: "Özel (kendim ayarlarım)", custom: true,
		note: "Hiçbir alan değiştirilmez."},
	{label: "Survival — Paper", software: model.SoftwarePaper,
		gamemode: "survival", difficulty: "normal", pvp: true,
		note: "Eklenti destekli, dengeli hayatta kalma."},
	{label: "Yaratıcı — Fabric", software: model.SoftwareFabric,
		gamemode: "creative", difficulty: "peaceful",
		note: "Mod destekli, serbest inşa. Ortak dünya için de bu gerekir."},
	{label: "Vanilla — Resmi", software: model.SoftwareVanilla,
		gamemode: "survival", difficulty: "easy", pvp: true,
		note: "Saf Minecraft deneyimi."},
	{label: "Anarşi — Paper", software: model.SoftwarePaper,
		gamemode: "survival", difficulty: "hard", pvp: true,
		note: "Kuralsız, zorlu PvP."},
	{label: "Hardcore — Vanilla", software: model.SoftwareVanilla,
		gamemode: "survival", difficulty: "hard", pvp: true, hardcore: true,
		note: "Tek can, kalıcı ölüm."},
}

// Wizard holds the create-server state.
type Wizard struct {
	step   wizStep
	cursor int

	templateIdx int

	name string
	desc string

	versions  []string
	verIdx    int
	verLoaded bool
	verNote   string
	manualVer string
	via       bool

	// verWant, kullanıcının o an EKRANDA GÖRDÜĞÜ sürüm metnidir.
	//
	// Neden dizin yetmiyor: sürüm listesi yazılıma bağlıdır ve altyapı
	// değişince atılır. Yalnızca verIdx saklansaydı, yeni listede aynı
	// dizin bambaşka bir sürüme denk gelirdi. Yeni liste geldiğinde bu
	// METİN aranır. Boş = kullanıcı henüz bir seçim yapmadı → en yeni.
	verWant string
	// verGen, sürüm isteğinin kuşak sayacıdır. Her yeni istekte artar;
	// uçuştaki yanıt kendi kuşağı geçersizleşmişse UYGULANMAZ.
	verGen uint64
	// verFetching, tek uçuş bayrağıdır: aynı anda yalnızca bir sürüm
	// RPC'si açıkta olur.
	verFetching bool

	softwareIdx int

	port   string
	render string
	sim    string

	onlineMode    bool
	gamemodeIdx   int
	difficultyIdx int
	pvp           bool
	maxPlayers    string
	whitelist     bool
	hardcore      bool
	motd          string

	ramIdx       int
	cpuQuota     string
	gpuInfo      string
	clusterShare bool

	dataDir    string
	autostart  bool
	autoBackup bool
	wan        bool

	eula bool

	creating bool
	err      string

	// ── Arka plan "oluştur" yanıtının bekleme kutusu (a.mu altında) ─────
	//
	// Goroutine creating/err alanlarına DOKUNMAZ; sonucunu buraya bırakır
	// ve çalışma döngüsü drainWizard() içinde devralır. Gerekçe için
	// drainWizard'ın başındaki "Yakalanan gerçek hata" notuna bakın.
	createDone bool
	createErr  string
}

// NewWizard builds the create-server wizard with sane defaults.
func NewWizard(st *model.SystemStatus) *Wizard {
	w := &Wizard{
		name:          "Survival",
		port:          "0",
		render:        "10",
		sim:           "10",
		maxPlayers:    "20",
		cpuQuota:      "",
		softwareIdx:   1, // paper
		ramIdx:        1, // 2048
		difficultyIdx: 1, // easy
		onlineMode:    true,
		pvp:           true,
		autoBackup:    true,
		gpuInfo:       "algılanmadı (sunucu başsız çalışır)",
	}
	if st != nil && len(st.GPUs) > 0 {
		var names []string
		for _, g := range st.GPUs {
			s := strings.TrimSpace(g.Vendor + " " + g.Model)
			if s == "" {
				s = "GPU"
			}
			names = append(names, s)
		}
		w.gpuInfo = strings.Join(names, ", ")
	}
	return w
}

// InWizard reports whether the create-server wizard is open.
func (a *App) InWizard() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wizard != nil
}

// wizardState returns the wizard, or nil.
func (a *App) wizardState() *Wizard {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wizard
}

// StartWizard opens the create-server flow.
func (a *App) StartWizard() {
	st, _, _ := a.Snapshot()
	a.beginTransition(transSlideDown)
	a.mu.Lock()
	a.wizard = NewWizard(st)
	a.dirty = true
	a.mu.Unlock()
	a.Emit(fbui.EventInfo, "Yeni sunucu sihirbazı")
	a.loadVersionsAsync()
}

// CloseWizard leaves the wizard.
func (a *App) CloseWizard() {
	a.beginTransition(transSlideUp)
	a.mu.Lock()
	a.wizard = nil
	a.dirty = true
	a.mu.Unlock()
}

// loadVersionsAsync fetches the live Minecraft version list.
//
// ARKA PLANDA: Mojang'a HTTP isteği gider ve internet yoksa zaman aşımına
// kadar bekler. Ana döngüde beklemek sihirbazı açılırken dondururdu.
//
// ── Yakalanan gerçek hata (tek uçuş) ────────────────────────────────────────
// "Altyapı" satırındaki HER ok tuşu yeni bir goroutine ve yeni bir RPC
// açıyordu. ipc.Client bütün çağrıları TEK bir kilitle sıraya dizer ve
// daemon, Mojang'a 20 sn zaman aşımıyla gidip sonucu önbelleğe de almaz.
// İnternete çıkamayan bir makinede kullanıcı paper'dan fabric'e geçmek için
// altı kez sağa bastığında altı istek arka arkaya 20'şer saniye kilidi
// tutuyordu: yaklaşık iki dakika boyunca durum yoklaması, bölüm verileri ve
// Başlat/Durdur düğmeleri o kuyrukta bekliyordu. Panel çizmeye devam ettiği
// için kullanıcı donduğunu bile anlamıyor, yalnızca "hiçbir tuş çalışmıyor"
// görüyordu.
//
// Artık aynı anda TEK istek uçar. İstek sürerken gelen değişiklikler kuşak
// sayacını artırır; uçuştaki istek bitince, güncel yazılım için bir kez daha
// denenir. Böylece hem kuyruk tıkanmaz hem de son seçim mutlaka yüklenir.
func (a *App) loadVersionsAsync() {
	if a.offline() {
		return
	}
	a.mu.Lock()
	w := a.wizard
	if w == nil {
		a.mu.Unlock()
		return
	}
	// Kuşak HER çağrıda artar: uçuştaki isteğin yanıtı bayatlar.
	w.verGen++
	if w.verFetching {
		// Zaten bir istek uçuyor. İKİNCİSİNİ AÇMA — artan kuşak, o istek
		// bitince güncel yazılım için yeniden denenmesini sağlayacak.
		a.mu.Unlock()
		return
	}
	w.verFetching = true
	gen, sw := w.verGen, w.software()
	a.mu.Unlock()

	go func() {
		for {
			// Sürüm listesi SEÇİLEN YAZILIMA göre gelir: Paper'ın
			// yayınladığı sürümler Fabric'inkilerle aynı değildir.
			res, err := a.cl.ServerVersions(sw)
			nextSw, nextGen, again := a.applyVersions(w, gen, res.Versions, err)
			if !again {
				return
			}
			sw, gen = nextSw, nextGen
		}
	}()
}

// applyVersions installs one version-list reply under a.mu.
//
// again=true dönerse, istek uçarken kullanıcı yazılımı değiştirmiştir:
// çağıran, dönen yazılım ve kuşakla bir kez daha dener. Tek uçuş bayrağı o
// durumda BIRAKILMAZ, yoksa araya giren bir tuş ikinci bir RPC açardı.
//
// ── Yakalanan gerçek hata (geç yanıt seçimi siliyordu) ──────────────────────
// Eski kod, yanıt geldiğinde koşulsuzca `w.verIdx = 0` yazıyordu. Kullanıcı
// "Sürüm" sayfasında 1.20.6'yı seçip bir sonraki sayfada altyapıyı Fabric
// yapınca liste yenileniyor ve seçim sessizce "en yeni"ye dönüyordu: Özet
// sayfası 1.21.11 gösteriyor, sunucu da YANLIŞ sürümle kuruluyordu. Üstelik
// yenileme başarısız olursa liste nil kalıyor, version() "" dönüyor ve
// gönderim "Sürüm: Bir sürüm seçin veya elle yazın" diyerek engelleniyordu —
// kullanıcının iki sayfa önce seçtiği sürüm için.
//
// Artık ekranda duran sürüm yeni listede ARANIR; yalnızca gerçekten yoksa
// en yeniye düşülür.
func (a *App) applyVersions(w *Wizard, gen uint64, list []string,
	err error) (model.Software, uint64, bool) {

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.wizard != w {
		// Sihirbaz kapandı (ya da yenisi açıldı): bu yanıtın sahibi yok.
		w.verFetching = false
		return "", 0, false
	}
	if gen != w.verGen {
		// Yanıt BAYAT: kullanıcı bu istek uçarken yazılımı değiştirdi.
		// Uygulamak, Fabric seçiliyken Paper'ın sürümlerini listelemek
		// olurdu. Güncel yazılım için yeniden dene.
		return w.software(), w.verGen, true
	}

	w.verFetching = false
	a.dirty = true

	if err != nil || len(list) == 0 {
		w.verNote = "sürüm listesi alınamadı — elle yazabilirsiniz"
		return "", 0, false
	}

	// Ekranda duran sürümü koru. verWant kullanıcının açık seçimidir;
	// hiç seçim yapılmadıysa o an gösterilen değer korunur.
	keep := w.verWant
	if keep == "" {
		keep = w.version()
	}
	w.versions = list
	w.verLoaded = true
	w.verNote = ""
	w.verIdx = 0 // eşleşme yoksa en yeni
	for i, v := range list {
		if v == keep {
			w.verIdx = i
			break
		}
	}
	return "", 0, false
}

// drainWizard adopts the background create reply ON THE RUN LOOP.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// "Oluştur" goroutine'i w.creating = false ve w.err = err.Error() yazıyordu.
// Bu iki alan çizim yolunda KİLİTSİZ okunuyor (setup_draw.go içindeki
// drawWizard hata metnini sayfa yapısına kopyalar, wizard_draw.go göstergeyi
// çizer). Daemon bir hata döndürdüğünde (disk dolu, geçersiz parametre) 2
// kelimelik string başlığı tam kopyalanırken değiştiği için hata bandında
// kırpık/çöp metin çıkıyor, eski işaretçi + yeni uzunluk birleşiminde metin
// çizici sınır dışına taşıyordu. Ayrıca yazma ile Tick arasında bir
// "önce-olur" bağı olmadığından "Oluşturuluyor…" göstergesi çağrı bittikten
// sonra da dönmeye devam edebiliyordu.
//
// Çözüm: goroutine sonucunu yalnızca a.mu altındaki bekleme kutusuna bırakır;
// creating ve err'i SADECE çalışma döngüsü yazar. Devralma noktası rows()
// olduğu için hem her tuşta hem her karede çalışır.
func (a *App) drainWizard(w *Wizard) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !w.createDone {
		return
	}
	w.createDone = false
	w.creating = false
	w.err = w.createErr
	w.createErr = ""
	a.dirty = true
}

// wizVersionRow copies everything the "Sürüm" row needs in ONE lock.
func (a *App) wizVersionRow(w *Wizard) (cur, manual, note string, loaded bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return w.version(), w.manualVer, w.verNote, w.verLoaded
}

// wizVersionView copies what drawWizardBody needs in ONE lock.
func (a *App) wizVersionView(w *Wizard) (note string, loaded bool, count int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return w.verNote, w.verLoaded, len(w.versions)
}

// wizardVersionList copies the version list and the selected index under a.mu.
//
// KOPYA döner: liste açılır pencerenin ömrü boyunca kullanılır ve arka plan
// isteği bu arada w.versions'ı bambaşka bir dilimle değiştirebilir.
func (a *App) wizardVersionList(w *Wizard) ([]string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(w.versions))
	copy(out, w.versions)
	return out, w.verIdx
}

// wizardVersion returns the chosen version string under a.mu.
func (a *App) wizardVersion(w *Wizard) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return w.version()
}

// wizardParams builds the create request under a.mu.
func (a *App) wizardParams(w *Wizard) ipc.ServerCreateParams {
	a.mu.Lock()
	defer a.mu.Unlock()
	return w.params()
}

// wizardValidateStep runs the current page's validation under a.mu.
func (a *App) wizardValidateStep(w *Wizard) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return w.validateStep()
}

// wizardValidateAll runs every page's validation under a.mu.
func (a *App) wizardValidateAll(w *Wizard) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return w.validateAll()
}

// clearVersionsLocked drops the software-specific version list.
//
// a.mu TUTULURKEN çağrılır.
//
// Sürüm listesi yazılıma bağlıdır: Paper'ın yayınladığı sürümler
// Fabric'inkilerle aynı değildir. Yenilemezsek kullanıcı, seçtiği yazılımda
// var olmayan bir sürüm seçebilirdi. Ama listeyi atmadan ÖNCE ekranda duran
// sürüm saklanır — yoksa kullanıcının seçimi altyapı değiştirince sessizce
// kaybolurdu.
func (w *Wizard) clearVersionsLocked() {
	if v := w.version(); v != "" {
		w.verWant = v
	}
	w.versions = nil
	w.verLoaded = false
	w.verNote = ""
}

// ── Satırlar ────────────────────────────────────────────────────────────────

func (w *Wizard) software() model.Software {
	if w.softwareIdx < 0 || w.softwareIdx >= len(model.AllSoftware) {
		return model.SoftwarePaper
	}
	return model.AllSoftware[w.softwareIdx]
}

// version returns the Minecraft version the user has settled on.
//
// a.mu TUTULURKEN çağrılır (kilitli sarmalayıcı: App.wizardVersion).
func (w *Wizard) version() string {
	if v := strings.TrimSpace(w.manualVer); v != "" {
		return v
	}
	if w.verIdx >= 0 && w.verIdx < len(w.versions) {
		return w.versions[w.verIdx]
	}
	// ── Yakalanan gerçek hata ───────────────────────────────────────────
	// Altyapı değiştiğinde liste atılır ve yeniden çekilir. Çekim
	// başarısız olursa (internet yok) liste nil kalıyor, burası "" dönüyor
	// ve gönderim "Sürüm: Bir sürüm seçin veya elle yazın" diye
	// engelleniyordu — kullanıcının iki sayfa önce seçtiği sürüm için.
	// Seçim listeden bağımsız olarak korunur.
	return w.verWant
}

func (w *Wizard) rows(a *App) []setupRow {
	// Arka plan "oluştur" yanıtı BURADA devralınır: rows() hem her tuşta
	// (wizardKey) hem her karede (drawWizard) hem de fare tıklamasında
	// çağrılır, yani çalışma döngüsünün tek ortak geçidi budur.
	a.drainWizard(w)

	switch w.step {
	case wizTemplate:
		t := wizTemplates[w.templateIdx]
		return []setupRow{
			{kind: rowPick, key: "template", label: "Şablon", value: t.label,
				hint: t.note},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Vazgeç"},
		}

	case wizIdentity:
		return []setupRow{
			{kind: rowText, key: "name", label: "Sunucu adı", value: w.name,
				hint: "Panelde ve oyuncu listesinde bu adla görünür."},
			{kind: rowText, key: "desc", label: "Açıklama", value: w.desc,
				hint: "İsteğe bağlı."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizVersion:
		// Sürüm alanları arka plan isteğiyle paylaşılır: hepsi TEK kilitte
		// kopyalanır, yoksa satır yarı güncellenmiş bir durumu çizerdi.
		v, manual, note, loaded := a.wizVersionRow(w)
		if v == "" {
			v = "seçilmedi"
		}
		if note == "" && !loaded {
			note = "sürüm listesi alınıyor…"
		}
		return []setupRow{
			{kind: rowPick, key: "version", label: "Minecraft sürümü", value: v,
				hint: note},
			{kind: rowText, key: "manualver", label: "Elle sürüm yaz",
				value: manual,
				hint:  "Listede olmayan eski sürümler için."},
			{kind: rowToggle, key: "via", label: "ViaVersion (eski istemciler)",
				on:   w.via,
				hint: "Eklenti destekli sunucularda farklı sürümlerin bağlanmasını sağlar."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizSoftware:
		sw := w.software()
		kind := "yalnızca vanilla"
		switch {
		case sw.SupportsPlugins():
			kind = "eklenti (plugins/)"
		case sw.SupportsMods():
			kind = "mod (mods/)"
		}
		return []setupRow{
			{kind: rowPick, key: "software", label: "Sunucu yazılımı",
				value: string(sw), hint: "Destek: " + kind},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizNetwork:
		return []setupRow{
			{kind: rowText, key: "port", label: "Port", value: w.port,
				hint: "0 = boş bir port otomatik seçilir."},
			{kind: rowText, key: "render", label: "Görüş mesafesi", value: w.render,
				hint: "Chunk. Yüksek değer daha çok CPU ve bant genişliği ister."},
			{kind: rowText, key: "sim", label: "Simülasyon mesafesi", value: w.sim,
				hint: "Chunk. Görüş mesafesinden büyük olması anlamsızdır."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizGameplay:
		return []setupRow{
			{kind: rowToggle, key: "online", label: "Online mode", on: w.onlineMode,
				hint: "Kapatmak korsan istemcilere izin verir; ÖNERİLMEZ."},
			{kind: rowPick, key: "gamemode", label: "Oyun modu",
				value: wizGamemodes[w.gamemodeIdx]},
			{kind: rowPick, key: "difficulty", label: "Zorluk",
				value: wizDifficulties[w.difficultyIdx]},
			{kind: rowToggle, key: "pvp", label: "PvP", on: w.pvp},
			{kind: rowText, key: "maxplayers", label: "En fazla oyuncu",
				value: w.maxPlayers},
			{kind: rowToggle, key: "whitelist", label: "Beyaz liste",
				on:   w.whitelist,
				hint: "Yalnızca izin verilen oyuncular bağlanabilir."},
			{kind: rowToggle, key: "hardcore", label: "Hardcore", on: w.hardcore,
				hint: "Ölünce izleyici moduna geçilir; geri alınamaz."},
			{kind: rowText, key: "motd", label: "MOTD", value: w.motd,
				hint: "Sunucu listesinde görünen metin."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizResources:
		cpu := w.cpuQuota
		if strings.TrimSpace(cpu) == "" {
			cpu = "sınırsız"
		}
		return []setupRow{
			{kind: rowPick, key: "ram", label: "RAM",
				value: strconv.Itoa(wizRAMChoices[w.ramIdx]) + " MB"},
			{kind: rowText, key: "cpu", label: "CPU payı (%)", value: cpu,
				hint: "Boş = sınırsız. Bir çekirdeğin yüzdesi."},
			{kind: rowInfo, key: "gpu", label: "Ekran kartı", value: w.gpuInfo,
				hint: "Bilgi amaçlı — Minecraft sunucusu başsız çalışır."},
			{kind: rowToggle, key: "cluster", label: "PC paylaşımı kullanılsın",
				on: w.clusterShare,
				hint: "Yedekleme ve günlük analizi gibi yan işler eşleşmiş bir " +
					"cihaza verilir. Hangi cihaz olduğunu sistem seçer."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizLocation:
		dir := w.dataDir
		if dir == "" {
			dir = "varsayılan"
		}
		return []setupRow{
			{kind: rowText, key: "datadir", label: "Kurulum klasörü", value: dir,
				hint: "Boş bırakın = /data altındaki varsayılan yer."},
			{kind: rowToggle, key: "autostart", label: "Açılışta başlat",
				on: w.autostart},
			{kind: rowToggle, key: "autobackup", label: "Otomatik yedek",
				on: w.autoBackup},
			{kind: rowToggle, key: "wan", label: "Tünel (internete aç)", on: w.wan,
				hint: "playit hesabı bağlı değilse sonradan Tünel ekranından bağlanır."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizEULA:
		return []setupRow{
			{kind: rowToggle, key: "eula", label: "Mojang EULA'sını kabul ediyorum",
				on:   w.eula,
				hint: "https://aka.ms/MinecraftEULA — kabul etmeden sunucu kurulamaz."},
			{kind: rowContinue, label: "Devam"},
			{kind: rowBack, label: "Geri"},
		}

	case wizConfirm:
		return []setupRow{
			{kind: rowContinue, label: "Sunucuyu oluştur"},
			{kind: rowBack, label: "Geri"},
		}
	}
	return nil
}

// ── Tuşlar ──────────────────────────────────────────────────────────────────

func (a *App) wizardKey(key string) Action {
	w := a.wizardState()
	if w == nil {
		return ActNone
	}
	defer a.Invalidate()

	rows := w.rows(a)
	n := len(rows)

	switch key {
	case "up", "k":
		if n > 0 {
			w.cursor = (w.cursor - 1 + n) % n
		}
	case "down", "j", "tab":
		if n > 0 {
			w.cursor = (w.cursor + 1) % n
		}
	case "esc":
		a.wizardBack(w)
	case "left", "h":
		a.wizardAdjust(w, -1)
	case "right", "l":
		a.wizardAdjust(w, 1)
	case "enter":
		if w.cursor >= 0 && w.cursor < n {
			a.wizardActivate(w, rows[w.cursor])
		}
	case "ctrl+c":
		return ActQuit
	}
	return ActNone
}

func (a *App) wizardBack(w *Wizard) {
	if w.step == wizTemplate {
		a.CloseWizard()
		return
	}
	a.beginTransition(transSlideUp)
	w.step--
	w.cursor = 0
	w.err = ""
}

func (a *App) wizardNext(w *Wizard) {
	if w.step == wizConfirm {
		a.createServer(w)
		return
	}
	if msg := a.wizardValidateStep(w); msg != "" {
		w.err = msg
		return
	}
	a.beginTransition(transSlideDown)
	w.step++
	w.cursor = 0
	w.err = ""
}

// validateStep checks the current page before moving on.
//
// Doğrulamayı SAYFA BAŞINA yapmak şart: hepsini sona bırakmak, kullanıcıyı
// hatayı düzeltmek için sekiz sayfa geri göndermek demektir.
//
// a.mu TUTULURKEN çağrılır (version() paylaşılan alanları okur). Kilitli
// sarmalayıcı: App.wizardValidateStep.
func (w *Wizard) validateStep() string {
	switch w.step {
	case wizIdentity:
		if strings.TrimSpace(w.name) == "" {
			return "Sunucu adı boş olamaz"
		}
	case wizVersion:
		if w.version() == "" {
			return "Bir sürüm seçin veya elle yazın"
		}
	case wizNetwork:
		if p, err := strconv.Atoi(strings.TrimSpace(w.port)); err != nil ||
			p < 0 || p > 65535 {
			return "Port 0–65535 arasında bir sayı olmalı"
		}
	case wizGameplay:
		if n, err := strconv.Atoi(strings.TrimSpace(w.maxPlayers)); err != nil ||
			n < 1 {
			return "En fazla oyuncu en az 1 olmalı"
		}
	case wizResources:
		if q := strings.TrimSpace(w.cpuQuota); q != "" {
			n, err := strconv.Atoi(q)
			if err != nil || n < 1 || n > 100 {
				return "CPU payı 1–100 arası olmalı (boş = sınırsız)"
			}
		}
	case wizEULA:
		if !w.eula {
			return "Sunucu kurabilmek için EULA kabul edilmeli"
		}
	}
	return ""
}

func (a *App) wizardAdjust(w *Wizard, delta int) {
	rows := w.rows(a)
	if w.cursor < 0 || w.cursor >= len(rows) {
		return
	}
	r := rows[w.cursor]
	switch r.key {
	case "template":
		a.mu.Lock()
		w.templateIdx = wrap(w.templateIdx+delta, len(wizTemplates))
		refetch := w.applyTemplate()
		a.mu.Unlock()
		if refetch {
			// Şablon altyapıyı değiştirdiyse sürüm listesi de o yazılıma
			// ait olmalı. Kilit DIŞINDA: loadVersionsAsync a.mu'yu alır.
			a.loadVersionsAsync()
		}
	case "version":
		// ── Yakalanan gerçek hata ───────────────────────────────────────
		// "Elle sürüm yaz" ile 1.12.2 girildikten sonra bu satırda sola/
		// sağa basmak HİÇBİR ŞEY yapmıyordu: verIdx ilerliyor ama
		// version() her zaman manualVer'i tercih ettiği için ekrandaki
		// değer de, gönderilen sürüm de 1.12.2 kalıyordu. Liste
		// penceresinde aynı işlem çalışıyordu (manualVer orada
		// temizleniyor); yani kullanıcının hangi yolu seçtiği, sürüm
		// değişikliğinin işe yarayıp yaramadığını belirliyordu.
		a.mu.Lock()
		if n := len(w.versions); n > 0 {
			w.verIdx = wrap(w.verIdx+delta, n)
			w.manualVer = "" // liste seçimi elle girişi geçersiz kılar
			w.verWant = w.versions[w.verIdx]
		}
		a.mu.Unlock()
	case "software":
		a.mu.Lock()
		w.softwareIdx = wrap(w.softwareIdx+delta, len(model.AllSoftware))
		w.clearVersionsLocked()
		a.mu.Unlock()
		a.loadVersionsAsync()
	case "gamemode":
		w.gamemodeIdx = wrap(w.gamemodeIdx+delta, len(wizGamemodes))
	case "difficulty":
		w.difficultyIdx = wrap(w.difficultyIdx+delta, len(wizDifficulties))
	case "ram":
		w.ramIdx = wrap(w.ramIdx+delta, len(wizRAMChoices))
	default:
		if r.kind == rowToggle {
			w.toggle(r.key)
		}
	}
}

func (w *Wizard) toggle(key string) {
	switch key {
	case "via":
		w.via = !w.via
	case "online":
		w.onlineMode = !w.onlineMode
	case "pvp":
		w.pvp = !w.pvp
	case "whitelist":
		w.whitelist = !w.whitelist
	case "hardcore":
		w.hardcore = !w.hardcore
	case "cluster":
		w.clusterShare = !w.clusterShare
	case "autostart":
		w.autostart = !w.autostart
	case "autobackup":
		w.autoBackup = !w.autoBackup
	case "wan":
		w.wan = !w.wan
	case "eula":
		w.eula = !w.eula
	}
}

// applyTemplate copies a preset's defaults. "Özel" changes nothing.
//
// a.mu TUTULURKEN çağrılır: softwareIdx ve verIdx'i arka plan sürüm isteği de
// okur. Dönen değer, altyapının gerçekten değişip değişmediğidir — değiştiyse
// çağıran sürüm listesini (kilit dışında) yenilemelidir.
func (w *Wizard) applyTemplate() bool {
	t := wizTemplates[w.templateIdx]
	if t.custom {
		return false
	}
	swChanged := false
	for i, s := range model.AllSoftware {
		if s == t.software {
			swChanged = w.softwareIdx != i
			w.softwareIdx = i
			break
		}
	}
	for i, g := range wizGamemodes {
		if g == t.gamemode {
			w.gamemodeIdx = i
			break
		}
	}
	for i, d := range wizDifficulties {
		if d == t.difficulty {
			w.difficultyIdx = i
			break
		}
	}
	w.pvp = t.pvp
	w.whitelist = t.whitelist
	w.hardcore = t.hardcore
	// Şablon "en yeni sürüm" demektir: açık seçim sıfırlanır, böylece
	// gelecek liste geldiğinde eski bir sürüme geri dönülmez.
	if swChanged {
		// Liste ARTIK BAŞKA bir yazılıma ait; olduğu gibi bırakmak, Fabric
		// seçiliyken Paper sürümü göstermek olurdu.
		w.versions = nil
		w.verLoaded = false
		w.verNote = ""
	}
	w.verIdx = 0
	w.verWant = ""
	return swChanged
}

func (a *App) wizardActivate(w *Wizard, r setupRow) {
	switch r.kind {
	case rowContinue:
		a.wizardNext(w)
	case rowBack:
		a.wizardBack(w)
	case rowToggle:
		w.toggle(r.key)
	case rowPick:
		a.wizardPick(w, r.key)
	case rowText:
		a.wizardText(w, r.key)
	}
}

func (a *App) wizardPick(w *Wizard, key string) {
	switch key {
	case "template":
		items := make([]ListItem, 0, len(wizTemplates))
		for i, t := range wizTemplates {
			items = append(items, ListItem{
				Label: t.label, Detail: t.note,
				Current: i == w.templateIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("Şablon",
			"Makul varsayılanları doldurur.", items,
			func(app *App, _ int, it ListItem) bool {
				app.mu.Lock()
				w.templateIdx = it.Value.(int)
				refetch := w.applyTemplate()
				app.mu.Unlock()
				if refetch {
					app.loadVersionsAsync()
				}
				return true
			}))

	case "version":
		list, cur := a.wizardVersionList(w)
		if len(list) == 0 {
			a.OpenModal(NewInfoModal("Sürüm listesi yok", []string{
				"Mojang sürüm listesi alınamadı.",
				"",
				"İnternet yoksa sürümü ELLE yazabilirsiniz:",
				"bir alt satırdaki 'Elle sürüm yaz' alanını kullanın.",
			}))
			return
		}
		items := make([]ListItem, 0, len(list))
		for i, v := range list {
			it := ListItem{Label: v, Current: i == cur, Value: i}
			if i == 0 {
				it.Badge, it.BadgeKind = "en yeni", fbui.EventOK
			}
			items = append(items, it)
		}
		a.OpenModal(NewListModal("Minecraft sürümü",
			fmt.Sprintf("%d sürüm listelendi.", len(list)), items,
			func(app *App, _ int, it ListItem) bool {
				// Seçim DİZİNLE değil METİNLE uygulanır: pencere açıkken
				// arka plan isteği listeyi değiştirmiş olabilir ve aynı
				// dizin bambaşka bir sürüme denk gelirdi.
				app.mu.Lock()
				w.manualVer = "" // liste seçimi elle girişi geçersiz kılar
				w.verWant = it.Label
				for i, v := range w.versions {
					if v == it.Label {
						w.verIdx = i
						break
					}
				}
				app.mu.Unlock()
				return true
			}))

	case "software":
		items := make([]ListItem, 0, len(model.AllSoftware))
		for i, s := range model.AllSoftware {
			detail := "vanilla"
			switch {
			case s.SupportsPlugins():
				detail = "eklenti (plugins/)"
			case s.SupportsMods():
				detail = "mod (mods/)"
			}
			items = append(items, ListItem{
				Label: string(s), Detail: detail,
				Current: i == w.softwareIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("Sunucu yazılımı",
			"Eklenti mi mod mu yükleyeceğini bu belirler.", items,
			func(app *App, _ int, it ListItem) bool {
				app.mu.Lock()
				w.softwareIdx = it.Value.(int)
				w.clearVersionsLocked()
				app.mu.Unlock()
				// Kilit DIŞINDA: loadVersionsAsync a.mu'yu kendisi alır.
				app.loadVersionsAsync()
				return true
			}))

	case "gamemode":
		a.pickFromList(w, "Oyun modu", wizGamemodes, &w.gamemodeIdx)
	case "difficulty":
		a.pickFromList(w, "Zorluk", wizDifficulties, &w.difficultyIdx)

	case "ram":
		items := make([]ListItem, 0, len(wizRAMChoices))
		for i, v := range wizRAMChoices {
			items = append(items, ListItem{
				Label: strconv.Itoa(v) + " MB", Current: i == w.ramIdx, Value: i,
			})
		}
		a.OpenModal(NewListModal("RAM",
			"Sunucunun ayırabileceği bellek (-Xmx).", items,
			func(app *App, _ int, it ListItem) bool {
				w.ramIdx = it.Value.(int)
				return true
			}))
	}
}

// pickFromList opens a simple string picker bound to an index.
func (a *App) pickFromList(w *Wizard, title string, opts []string, idx *int) {
	items := make([]ListItem, 0, len(opts))
	for i, o := range opts {
		items = append(items, ListItem{Label: o, Current: i == *idx, Value: i})
	}
	a.OpenModal(NewListModal(title, "", items,
		func(app *App, _ int, it ListItem) bool {
			*idx = it.Value.(int)
			return true
		}))
}

func (a *App) wizardText(w *Wizard, key string) {
	type field struct {
		title, prompt, hint string
		value               *string
		maxLen              int
		validate            func(string) string
	}
	numeric := func(min, max int, unit string) func(string) string {
		return func(s string) string {
			s = strings.TrimSpace(s)
			if s == "" {
				return ""
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				return "Yalnızca rakam yazın"
			}
			if n < min || n > max {
				return fmt.Sprintf("%d–%d arası olmalı%s", min, max, unit)
			}
			return ""
		}
	}

	fields := map[string]field{
		"name": {"Sunucu adı", "Panelde bu adla görünecek.", "",
			&w.name, 40, func(s string) string {
				if strings.TrimSpace(s) == "" {
					return "Ad boş olamaz"
				}
				return ""
			}},
		"desc":      {"Açıklama", "İsteğe bağlı.", "", &w.desc, 80, nil},
		"manualver": {"Sürüm", "Listede olmayan bir sürüm yazın.", "Örn: 1.12.2", &w.manualVer, 20, nil},
		"port":      {"Port", "0 = otomatik boş port.", "", &w.port, 5, numeric(0, 65535, "")},
		"render":    {"Görüş mesafesi", "Chunk cinsinden.", "", &w.render, 2, numeric(2, 32, " chunk")},
		"sim":       {"Simülasyon mesafesi", "Chunk cinsinden.", "", &w.sim, 2, numeric(2, 32, " chunk")},
		"maxplayers": {"En fazla oyuncu", "Aynı anda bağlanabilecek kişi sayısı.", "",
			&w.maxPlayers, 4, numeric(1, 1000, "")},
		"motd": {"MOTD", "Sunucu listesinde görünen metin.", "", &w.motd, 60, nil},
		"cpu": {"CPU payı", "Bir çekirdeğin yüzdesi. Boş = sınırsız.", "",
			&w.cpuQuota, 3, numeric(1, 100, "")},
		"datadir": {"Kurulum klasörü", "Boş = varsayılan yer.", "Örn: /data/sunucular/survival",
			&w.dataDir, 120, nil},
	}

	f, ok := fields[key]
	if !ok {
		return
	}
	m := NewTextModal(f.title, f.prompt, func(app *App, v string) {
		// KİLİT ALTINDA: "manualver" alanını arka plan sürüm isteği de
		// okur (version() içinde). Kilitsiz yazmak, 2 kelimelik string
		// başlığının yarısı yazılmışken okunmasına yol açardı.
		app.mu.Lock()
		*f.value = strings.TrimSpace(v)
		app.mu.Unlock()
		w.err = ""
	}).WithValue(*f.value).WithMaxLen(f.maxLen)
	if f.hint != "" {
		m = m.WithHint(f.hint)
	}
	if f.validate != nil {
		m = m.WithValidate(f.validate)
	}
	a.OpenModal(m)
}

// ── Oluşturma ───────────────────────────────────────────────────────────────

// params builds the create request.
//
// a.mu TUTULURKEN çağrılır (version() ve software() paylaşılan alanları
// okur). Kilitli sarmalayıcı: App.wizardParams.
func (w *Wizard) params() ipc.ServerCreateParams {
	atoi := func(s string, def int) int {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return def
		}
		return n
	}
	online, pvp := w.onlineMode, w.pvp
	return ipc.ServerCreateParams{
		Name:             strings.TrimSpace(w.name),
		Description:      strings.TrimSpace(w.desc),
		Software:         w.software(),
		MCVersion:        w.version(),
		RAMMB:            wizRAMChoices[w.ramIdx],
		Port:             atoi(w.port, 0),
		CPUQuota:         atoi(w.cpuQuota, 0),
		ViewDistance:     atoi(w.render, 10),
		SimDistance:      atoi(w.sim, 10),
		MaxPlayers:       atoi(w.maxPlayers, 20),
		MOTD:             strings.TrimSpace(w.motd),
		Gamemode:         wizGamemodes[w.gamemodeIdx],
		Difficulty:       wizDifficulties[w.difficultyIdx],
		OnlineMode:       &online,
		PVP:              &pvp,
		Whitelist:        w.whitelist,
		Hardcore:         w.hardcore,
		ClusterShare:     w.clusterShare,
		DataDir:          strings.TrimSpace(w.dataDir),
		Autostart:        w.autostart,
		AutoBackup:       w.autoBackup,
		WAN:              w.wan,
		AllowOldVersions: w.via,
	}
}

// createServer submits the wizard.
//
// ARKA PLANDA: daemon sunucu klasörünü hazırlar ve kurulumu tetikler; bu
// saniyeler sürebilir. Ana döngüde beklemek "oluşturuluyor" göstergesini
// tam da beklerken dondururdu.
func (a *App) createServer(w *Wizard) {
	if w.creating {
		return
	}
	if a.offline() {
		w.err = "daemon bağlantısı yok"
		return
	}
	if msg := a.wizardValidateAll(w); msg != "" {
		w.err = msg
		return
	}
	w.creating = true
	a.Emit(fbui.EventBusy, w.name+" oluşturuluyor…")

	p := a.wizardParams(w)
	go func() {
		srv, err := a.cl.Create(p)
		// Sonuç ÖNCE bekleme kutusuna bırakılır: creating/err alanlarını
		// yalnızca çalışma döngüsü yazar (bkz. drainWizard).
		a.finishCreate(w, err)
		if err != nil {
			a.Fail("sunucu oluşturulamadı", err)
			return
		}
		name := ""
		if srv != nil {
			name = srv.Name
		}
		// Emit ve refreshAsync KOŞULSUZ: sihirbaz kapanmış olsa bile
		// "oluşturuldu" bildirimi ve listenin yenilenmesi kullanıcıya
		// borçtur.
		a.Emit(fbui.EventOK, name+" oluşturuldu — yazılım indiriliyor")
		a.refreshAsync()
		// Ekranı YALNIZCA gönderimin yapıldığı sihirbaz hâlâ açıksa taşı.
		if a.closeWizardIf(w) {
			a.gotoSection(SecServers)
			a.setFocus(FocusContent)
		}
	}()
}

// finishCreate parks the create reply for the run loop.
func (a *App) finishCreate(w *Wizard, err error) {
	a.mu.Lock()
	w.createDone = true
	w.createErr = ""
	if err != nil {
		w.createErr = err.Error()
	}
	a.dirty = true
	a.mu.Unlock()
}

// closeWizardIf closes the wizard ONLY if it is still the one passed in.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// createServer'ın yanıtı geldiğinde koşulsuzca a.CloseWizard() + Sunucular
// bölümüne atlama yapılıyordu. "Oluştur" çağrısı, tıkanmış bir sürüm isteğinin
// arkasında saniyelerce bekleyebiliyor ve Esc bu sırada engellenmiyordu; yani
// kullanıcı "Oluşturuluyor…" göstergesi donmuş görünürken Esc'e basıp `n` ile
// İKİNCİ bir sunucu sihirbazı açtığında, A sunucusunun geç gelen yanıtı B'nin
// yarı doldurulmuş sihirbazını kapatıyordu: B için yazılan her alan hiçbir
// açıklama olmadan siliniyordu. Daha hafif hâli için ikinci sihirbaza bile
// gerek yoktu — geç yanıt, kullanıcıyı gittiği ekrandan koparıyordu.
//
// Denetim ve kapatma TEK kilitte yapılır; "önce sor, sonra kapat" deseni
// aradaki Esc'i kaçırırdı.
func (a *App) closeWizardIf(w *Wizard) bool {
	a.mu.Lock()
	if a.wizard != w {
		a.mu.Unlock()
		return false
	}
	a.wizard = nil
	a.dirty = true
	a.mu.Unlock()
	// beginTransition kendi kilidini alır: kilit ALTINDA çağrılamaz.
	a.beginTransition(transSlideUp)
	return true
}

// validateAll re-runs every page's validation before submitting.
//
// Kullanıcı sayfaları atlayarak (fare ile) ilerlemiş olabilir; son bir
// denetim, daemon'a geçersiz veri göndermeyi engeller.
//
// a.mu TUTULURKEN çağrılır. Kilitli sarmalayıcı: App.wizardValidateAll.
func (w *Wizard) validateAll() string {
	saved := w.step
	defer func() { w.step = saved }()
	for s := wizStep(0); s < wizConfirm; s++ {
		w.step = s
		if msg := w.validateStep(); msg != "" {
			return wizTitles[s] + ": " + msg
		}
	}
	return ""
}
