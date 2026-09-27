// Package server implements the Minecraft server lifecycle: installing software
// via providers, launching it with the correct Java runtime and JVM flags under
// the process supervisor, capturing its console, and tracking live state.
package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"mcos/internal/java"
	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/store"
	"mcos/internal/supervisor"
)

// runtime holds the live, non-persisted state of one server.
type runtime struct {
	mu        sync.Mutex
	proc      *supervisor.Process
	console   *log.Ring
	state     model.ServerState
	players   int
	installMu sync.Mutex // serializes Install so create+Start can't race
}

func (r *runtime) setState(s model.ServerState) {
	r.mu.Lock()
	r.state = s
	r.mu.Unlock()
}

func (r *runtime) getState() model.ServerState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Manager owns server runtimes and coordinates install + lifecycle.
type Manager struct {
	store  *store.Store
	java   *java.Manager
	sup    *supervisor.Supervisor
	log    *log.Logger
	client *http.Client

	mu       sync.Mutex
	runtimes map[string]*runtime

	// ExtraEnv, daemon'un bir sunucu sürecine eklemek istediği ortam
	// değişkenleridir (ör. kardeş kopyanın MCOS_LINK_SELF'i). Alan olarak
	// duruyor çünkü düğüm adı cluster paketinde; server paketi onu ithal
	// ederse cluster ↔ server döngüsü oluşurdu. nil = ek yok.
	ExtraEnv func(srv *model.Server) []string
}

// NewManager constructs a server manager sharing the daemon's store, Java
// manager, and process supervisor.
func NewManager(st *store.Store, jm *java.Manager, sup *supervisor.Supervisor, lg *log.Logger) *Manager {
	return &Manager{
		store:    st,
		java:     jm,
		sup:      sup,
		log:      lg,
		client:   &http.Client{Timeout: 15 * time.Minute},
		runtimes: map[string]*runtime{},
	}
}

func (m *Manager) logf(format string, a ...any) {
	if m.log != nil {
		m.log.Infof(format, a...)
	}
}

// rt returns (creating if needed) the runtime for a server id.
func (m *Manager) rt(id string) *runtime {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runtimes[id]
	if !ok {
		r = &runtime{console: log.NewRing(3000), state: model.StateStopped}
		m.runtimes[id] = r
	}
	return r
}

// State returns the live state of a server.
func (m *Manager) State(id string) model.ServerState {
	m.mu.Lock()
	r, ok := m.runtimes[id]
	m.mu.Unlock()
	if !ok {
		return model.StateStopped
	}
	return r.getState()
}

// FillRuntime overlays live fields onto a loaded server manifest.
func (m *Manager) FillRuntime(srv *model.Server) {
	m.mu.Lock()
	r, ok := m.runtimes[srv.ID]
	m.mu.Unlock()
	if !ok {
		srv.State = model.StateStopped
		return
	}
	srv.State = r.getState()
	r.mu.Lock()
	srv.Players = r.players
	r.mu.Unlock()
	if r.proc != nil {
		srv.PID = r.proc.PID()
		srv.UptimeSec = r.proc.UptimeSec()
		srv.LastLog = r.proc.LastLine()
	}
}

// Start installs (if needed), binds Java, and launches the server.
func (m *Manager) Start(ctx context.Context, srv *model.Server) error {
	r := m.rt(srv.ID)
	if s := r.getState(); s == model.StateRunning || s == model.StateStarting {
		return fmt.Errorf("server already %s", s)
	}

	// failStart, hatayı hem konsola (kullanıcı için Türkçe) hem günlüğe
	// (teknik ayrıntıyla) yazar ve durumu Error'a çeker.
	failStart := func(err error) error {
		r.setState(model.StateError)
		m.sup.Remove(srv.ID)
		m.onConsoleLine(srv.ID, "[MCOS HATA] "+UserMessage(err))
		if m.log != nil {
			m.log.Errorf("server: %q (%s) başlatma hatası: %v", srv.Name, srv.ID, err)
		}
		return err
	}

	r.setState(model.StateStarting)

	// Eski eşlemeyle (26.x -> Java 21) kaydedilmiş sunucular: kurulum ve
	// başlatma doğru Java ile yapılsın (bkz. java.RaiseToRequired).
	if java.RaiseToRequired(srv) {
		m.logf("server: %q için Java %d'e yükseltildi (Minecraft %s bunu istiyor)",
			srv.Name, srv.JavaMajor, srv.MCVersion)
		if err := m.store.SaveServer(srv); err != nil {
			m.logf("server: %q kaydedilemedi: %v", srv.Name, err)
		}
	}

	if err := m.EnsureInstalled(ctx, srv); err != nil {
		return failStart(startErr(StageInstall,
			fmt.Sprintf("%s %s sunucu yazılımı indirilemedi veya kurulamadı. "+
				"İnternet bağlantısını kontrol edin, sonra tekrar deneyin.",
				srv.Software, srv.MCVersion), err))
	}

	li, err := m.loadLaunch(srv.ID)
	if err != nil {
		return failStart(startErr(StageLaunch,
			"Kurulum kaydı okunamadı. Sunucu klasörü bozulmuş olabilir; "+
				"Yazılım sekmesinden sürümü yeniden kurun.", err))
	}

	javaBin, jvmArgs, err := m.java.BindForServer(srv)
	if err != nil {
		return failStart(startErr(StageJava,
			fmt.Sprintf("Bu sunucu Java %d gerektiriyor ama kurulamadı. "+
				"Ayarlar → Java bölümünden Java %d kurun.",
				srv.JavaMajor, srv.JavaMajor), err))
	}
	if javaBin == "" {
		return failStart(startErr(StageJava,
			fmt.Sprintf("Java %d çalıştırılabilir dosyası bulunamadı.", srv.JavaMajor), nil))
	}

	args, err := buildLaunchArgs(jvmArgs, li)
	if err != nil {
		return failStart(startErr(StageArgs,
			"Sunucu başlatma komutu oluşturulamadı. Sürümü yeniden kurmayı deneyin.", err))
	}

	dataDir := m.store.Paths.ServerData(srv.ID)
	// Ortak dünya: tohum/zorluk kurulumdan SONRA değişmiş olabilir (ortak
	// dünya sonradan açıldı ya da kurucu yeni kurulum gönderdi). Her
	// açılışta yeniden yazılmazsa iki makine farklı dünya üretir
	// (bkz. WriteLinkProperties).
	if err := WriteLinkProperties(dataDir, srv); err != nil {
		return failStart(startErr(StageLaunch,
			"server.properties yazılamadı; disk dolu ya da salt okunur olabilir.", err))
	}
	r.setState(model.StateStarting)
	r.mu.Lock()
	r.players = 0
	r.mu.Unlock()

	spec := supervisor.Spec{
		Dir:         dataDir,
		Path:        javaBin,
		Args:        args,
		StopTimeout: 60 * time.Second,
		GracefulStop: func(p *supervisor.Process) error {
			return p.WriteStdin("stop")
		},
		OnLine:      func(line string) { m.onConsoleLine(srv.ID, line) },
		OnExit:      func(code int, err error) { m.onExit(srv.ID, code, err) },
		Nice:        niceForServer(srv),
		CPUAffinity: srv.CPUAffinity,

		// ── Düzeltilen gerçek hata: ORTAM HİÇ AYARLANMIYORDU ────────────
		//
		// supervisor.Spec'in Env alanı vardı ve süreç onu kullanıyordu, ama
		// kod tabanında HİÇBİR YER doldurmuyordu (grep "Env:" -> 0 sonuç).
		//
		// Sonuç: mcos-link modu MCOS_LINK_PORT'u hiç görmüyor ve HER sunucu
		// aynı sabit portu (27893) dinlemeye çalışıyordu. Aynı makinede
		// ikinci bir sunucu açıldığında bind BAŞARISIZ oluyor, mod bunu
		// ölümcül saymayıp yalnızca uyarı basıyor ve ortak dünya o sunucuda
		// SESSİZCE çalışmıyordu.
		//
		// Tek makinede birden çok sunucu tam olarak kullanıcının istediği
		// şey olduğu için bu, özelliğin ön koşulu.
		Env: append(linkEnv(srv), m.extraEnv(srv)...),

		// ISLETIM SISTEMI duzeyinde kaynak tavani. Eskiden CPUQuota yalnizca
		// saklaniyordu ve HICBIR YERDE uygulanmiyordu; artik cgroup v2 ile
		// gercekten uygulaniyor.
		ID:     srv.ID,
		Limits: limitsForServer(srv),
		OnLimits: func(desc string) {
			m.logf("server: %q kaynak sinirlari uygulandi: %s", srv.Name, desc)
		},
		// Yığını HER başlatmada o anki boş belleğe sığdır (bkz. memfit.go):
		// bellek yetmediği için öldürülen bir sunucu otomatik yeniden
		// başlatmada küçülmüş yığınla açılır, aynı boyutla yine ölmez.
		BeforeStart: func(sp *supervisor.Spec) []string {
			return m.fitMemory(srv, li, sp)
		},
	}
	policy := supervisor.RestartPolicy{OnCrash: srv.RestartOnCrash, MaxRestarts: 10, Backoff: 5 * time.Second}

	proc := m.sup.Add(srv.ID, spec, policy)
	r.mu.Lock()
	r.proc = proc
	r.mu.Unlock()

	m.logf("server: starting %q (%s, java=%s)", srv.Name, srv.Software, javaBin)
	if err := m.sup.Start(srv.ID); err != nil {
		return failStart(startErr(StageSpawn,
			fmt.Sprintf("Java süreci başlatılamadı (%s). Dosya izinlerini ve "+
				"kalan disk alanını kontrol edin.", javaBin), err))
	}
	go m.watchStartup(srv.ID)
	return nil
}

// startupGrace, "Done (…)!" satırı beklenirken tanınan süre. Bu sürenin
// sonunda süreç hâlâ yaşıyorsa sunucu çalışıyor kabul edilir.
const startupGrace = 3 * time.Minute

// watchStartup keeps a server from being stuck on "BAŞLIYOR" forever.
//
// Durum geçişi yalnızca konsolda "Done (…)!" satırı görülünce yapılıyordu
// (bkz. onConsoleLine). Forge/NeoForge ve bazı sürümler bu satırı farklı
// biçimde bastığı için sunucu gerçekten çalışsa bile panel sonsuza kadar
// "BAŞLIYOR" gösteriyordu. Burada süre dolduğunda sürecin canlı olup
// olmadığına bakıp durumu netleştiriyoruz.
func (m *Manager) watchStartup(id string) {
	time.Sleep(startupGrace)

	r := m.rt(id)
	if r.getState() != model.StateStarting {
		return // zaten Running/Error/Stopped oldu
	}
	r.mu.Lock()
	proc := r.proc
	r.mu.Unlock()
	if proc == nil {
		return
	}

	if proc.State() == supervisor.StateRunning {
		r.setState(model.StateRunning)
		m.onConsoleLine(id, fmt.Sprintf(
			"[MCOS] Sunucu %.0f dakikadır çalışıyor ama beklenen \"Done\" satırını yazmadı; "+
				"çalışıyor kabul edildi.", startupGrace.Minutes()))
		m.logf("server: %s uzun süre BAŞLIYOR kaldı, süreç canlı olduğu için ÇALIŞIYOR yapıldı", id)
		return
	}

	r.setState(model.StateError)
	m.onConsoleLine(id, "[MCOS HATA] Sunucu başlatılamadı: süreç açılış sırasında sonlandı. "+
		"Konsol çıktısındaki son satırlara bakın.")
}

// niceForServer maps a server's priority and full-performance flag to a Unix
// nice value (lower = more CPU). Full-performance pins it well above normal.
func niceForServer(srv *model.Server) int {
	n := 0
	switch srv.Priority {
	case model.PriorityLow:
		n = 10
	case model.PriorityHigh:
		n = -5
	}
	if srv.FullPerf {
		n = -10
	}
	return n
}

// fitMemory recomputes the JVM heap, args and limits for the memory that is
// free right now. Süreç kilidi altında çağrılır (supervisor.Spec.BeforeStart).
func (m *Manager) fitMemory(srv *model.Server, li *launchInfo, sp *supervisor.Spec) []string {
	plan := planMemory(srv.RAMMB, memAvailableMB())
	eff := *srv
	eff.RAMMB = plan.HeapMB
	jvm := java.JVMArgs(srv.JVMFlags, plan.HeapMB, srv.JavaMajor)
	if !plan.PreTouch {
		jvm = dropPreTouch(jvm)
	}
	if args, err := buildLaunchArgs(jvm, li); err == nil {
		sp.Args = args
	}
	if !srv.FullPerf {
		sp.Limits = limitsForServer(&eff)
	}
	m.logf("server: %q bellek plani: istenen %d MB, bos %d MB -> yigin %d MB (on dokunma %v)",
		srv.Name, plan.WantMB, plan.AvailMB, plan.HeapMB, plan.PreTouch)
	if !plan.Reduced {
		return nil
	}
	return []string{fmt.Sprintf("[MCOS UYARI] Bu sunucuya %d MB RAM ayrılmıştı ama sistemde şu an "+
		"yalnızca %d MB boş bellek var (MCOS'un kendisi de RAM'de çalışır). Bellek yetmediği için "+
		"çekirdeğin sunucuyu öldürmemesi (OOM, \"signal: killed\") için %d MB ile başlatılıyor. "+
		"Kalıcı çözüm: sunucunun RAM ayarını düşürün, diğer sunucuları kapatın ya da makineye RAM ekleyin.",
		plan.WantMB, plan.AvailMB, plan.HeapMB)}
}

// limitsForServer maps a server's configured caps to OS-level limits.
//
// ── Neden var ───────────────────────────────────────────────────────────────
// CPUQuota alani modelde, IPC sozlesmesinde ve UC ayri ekranda vardi ama
// hicbir yerde UYGULANMIYORDU. Kullanici "%50 CPU" secip kaydediyor, sunucu
// yine tum makineyi kullaniyordu. Bu fonksiyon o bosluğu kapatir.
//
// Turbo acikken sinirlar BILEREK kaldirilir: "turbo" tam olarak bunu
// vaat ediyor.
func limitsForServer(srv *model.Server) supervisor.Limits {
	if srv.FullPerf {
		// Turbo: hicbir tavan yok. nice -10 ile birlikte, sunucu makinenin
		// tamamini kullanabilir.
		return supervisor.Limits{IOWeight: 1000}
	}
	lim := supervisor.Limits{CPUPercent: srv.CPUQuota}

	// Bellek tavanı KONMAZ.
	//
	// ── Yakalanan gerçek hata: "The server has not responded for 10 seconds"
	// Eskiden yığın*1,25+256 MB'lık memory.max ve onun %90'ı memory.high
	// konuyordu. cgroup v2 sunucunun okuyup yazdığı DÜNYA DOSYALARININ sayfa
	// önbelleğini de o gruba sayar: dünya büyüdükçe grup memory.high'a
	// dayanıyor, çekirdek her bellek isteğinde sunucuyu uyutup önbelleği geri
	// almaya zorluyordu. Oyun döngüsü saniyelerce duruyor, Paper'ın bekçisi
	// "10 saniyedir yanıt yok" yazıyordu (yavaş SD kartta en kötüsü).
	// Yığın zaten -Xmx ile sınırlı; sistem geneli OOM'a karşı yığın her
	// başlatmada boş belleğe sığdırılıyor (memfit.go).

	// Dusuk oncelikli sunucular disk bant genisliginde de geri cekilsin.
	switch srv.Priority {
	case model.PriorityLow:
		lim.IOWeight = 50
	case model.PriorityHigh:
		lim.IOWeight = 500
	default:
		lim.IOWeight = 100
	}
	return lim
}

// EnsureInstalled installs the server software if it has not been prepared yet.
// It is safe to call concurrently for the same server: a per-server lock makes
// the create-time background install and a user-triggered Start mutually
// exclusive, so the data directory is never written by two installers at once.
func (m *Manager) EnsureInstalled(ctx context.Context, srv *model.Server) error {
	r := m.rt(srv.ID)
	r.installMu.Lock()
	defer r.installMu.Unlock()
	if m.IsInstalled(srv) {
		return nil
	}
	return m.Install(ctx, srv)
}

// InstallExclusive (re)installs the software under the same per-server lock
// as EnsureInstalled.
//
// Yakalanan hata (uçtan uca eşleştirme sınamasında günlükten okundu):
// server.create arka planda EnsureInstalled başlatıyor, hemen ardından gelen
// server.install ise Install'u KİLİTSİZ çağırıyordu. "paper … indiriliyor"
// satırı 2 ms arayla iki kez yazıldı: iki kurulumcu aynı klasöre aynı jar'ı
// aynı anda yazıyordu (yarım/bozuk jar riski). Açık kurulum yine yapılır
// (kullanıcı "yeniden kur" demiş olabilir), yalnızca sıraya girer.
func (m *Manager) InstallExclusive(ctx context.Context, srv *model.Server) error {
	r := m.rt(srv.ID)
	r.installMu.Lock()
	defer r.installMu.Unlock()
	return m.Install(ctx, srv)
}

// Stop gracefully stops a running server.
func (m *Manager) Stop(id string) error {
	r := m.rt(id)
	if s := r.getState(); s != model.StateRunning && s != model.StateStarting {
		return fmt.Errorf("server not running")
	}
	r.setState(model.StateStopping)
	m.logf("server: stopping %s", id)
	return m.sup.Stop(id)
}

// Restart stops then starts a server.
func (m *Manager) Restart(ctx context.Context, srv *model.Server) error {
	if s := m.State(srv.ID); s == model.StateRunning || s == model.StateStarting {
		if err := m.Stop(srv.ID); err != nil {
			return err
		}
		// r.proc, kod tabanının her yerinde r.mu altında okunuyor; burada
		// kilitsiz okunuyordu (veri yarışı).
		r := m.rt(srv.ID)
		r.mu.Lock()
		p := r.proc
		r.mu.Unlock()
		if p != nil {
			p.Wait()
		}
	}
	return m.Start(ctx, srv)
}

// Command sends a raw console command to a running server.
func (m *Manager) Command(id, command string) error {
	r := m.rt(id)
	if r.getState() != model.StateRunning {
		return fmt.Errorf("server not running")
	}
	r.mu.Lock()
	proc := r.proc
	r.mu.Unlock()
	if proc == nil {
		return fmt.Errorf("no process")
	}
	return proc.WriteStdin(command)
}

// Console returns console lines since cursor plus the advanced cursor.
func (m *Manager) Console(id string, cursor int64) ([]log.Entry, int64) {
	r := m.rt(id)
	return r.console.Since(cursor)
}

// TailConsole returns the last n console lines (oldest first).
func (m *Manager) TailConsole(id string, n int) []log.Entry {
	r := m.rt(id)
	return r.console.Tail(n)
}

// StartAutostart launches every server flagged autostart (used on boot).
func (m *Manager) StartAutostart(ctx context.Context, servers []*model.Server) {
	for _, srv := range servers {
		if !srv.Autostart {
			continue
		}
		s := srv
		go func() {
			if err := m.Start(ctx, s); err != nil {
				m.logf("server: autostart %q failed: %v", s.Name, err)
			}
		}()
	}
}

// isServerReadyLine reports whether a console line signals "server is up".
//
// Eskiden koşul `Done (` VE `For help` idi. Forge/NeoForge ve bazı sürümler
// "For help" kısmını basmadığı için sunucu gerçekten açılsa bile durum
// BAŞLIYOR'da kalıyordu. Ortak ve ayırt edici imza `Done (` + `)!`:
//
//	[Server thread/INFO]: Done (12.345s)! For help, type "help"
//	[modloading-worker-0/INFO]: Done (30.1s)!
//
// Sohbet satırları da dışlanır: bir oyuncunun sohbete "Done (1s)!" yazarak
// durumu etkilemesini engeller (isPlayerEventLine ile aynı `<` kuralı).
func isServerReadyLine(line string) bool {
	if strings.Contains(line, "<") {
		return false
	}
	i := strings.Index(line, "Done (")
	if i < 0 {
		return false
	}
	return strings.Contains(line[i:], ")!")
}

// isPlayerEventLine reports whether a line is a genuine join/leave event rather
// than a chat message that merely contains the phrase.
//
// Sohbet satırları oyuncu adını `<isim>` biçiminde taşır:
//
//	[Server thread/INFO]: <Ahmet> ben de joined the game    ← sohbet, sayılmamalı
//	[Server thread/INFO]: Ahmet joined the game             ← gerçek olay
//
// Bu yüzden `<` içeren satırlar reddedilir ve olay ifadesinin satırın SONUNDA
// olması istenir.
func isPlayerEventLine(line, suffix string) bool {
	if strings.Contains(line, "<") {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(line, " \t\r"), suffix)
}

// onConsoleLine records output and updates derived state (running / players).
func (m *Manager) onConsoleLine(id, line string) {
	r := m.rt(id)
	r.console.Add(log.Entry{Time: time.Now(), Message: line})

	switch {
	case isServerReadyLine(line):
		if r.getState() == model.StateStarting {
			r.setState(model.StateRunning)
			m.logf("server: %s açıldı", id)
		}
	case isPlayerEventLine(line, "joined the game"):
		r.mu.Lock()
		r.players++
		r.mu.Unlock()
	case isPlayerEventLine(line, "left the game"):
		r.mu.Lock()
		if r.players > 0 {
			r.players--
		}
		r.mu.Unlock()
	}
}

// onExit transitions state when the process exits.
func (m *Manager) onExit(id string, code int, err error) {
	r := m.rt(id)
	r.mu.Lock()
	proc := r.proc
	r.players = 0
	r.mu.Unlock()
	requested := proc != nil && proc.Requested()
	if requested {
		r.setState(model.StateStopped)
	} else {
		r.setState(model.StateError)
		m.onConsoleLine(id, fmt.Sprintf("[MCOS HATA] Sunucu süreci beklenmeyen bir şekilde durdu (Çıkış kodu: %d, Hata: %v)", code, err))
	}
}

// linkEnv builds the environment for a Minecraft process.
//
// ── Neden os.Environ() de ekleniyor ─────────────────────────────────────────
//
// supervisor, Env doluysa süreç ortamını TAMAMEN onunla değiştiriyor
// (process.go: cmd.Env = p.spec.Env). Yalnızca MCOS_LINK_PORT verseydik JVM
// PATH, HOME ve TZ olmadan başlardı — Java bunları kullanıyor ve eksikliği
// "çalışıyor ama saat yanlış / geçici dosya yazamıyor" gibi anlaşılmaz
// belirtiler üretir.
func linkEnv(srv *model.Server) []string {
	port := srv.Link.LinkPort
	if port <= 0 {
		port = model.DefaultLinkPort
	}
	env := append([]string{}, os.Environ()...)
	env = append(env,
		fmt.Sprintf("MCOS_LINK_PORT=%d", port),
		// ŞEMA ŞART ("http://"). Burada eskiden "127.0.0.1:27892" yazıyordu;
		// eklentinin Coordinator'ı bunu olduğu gibi taban URL sayıyor ve
		// URI.create("127.0.0.1:27892/link/topology") Java'da
		// "Illegal character in scheme name at index 0" fırlatıyor. Sonuç:
		// mod koordinatöre HİÇ bağlanamıyor, topolojiyi "kapalı" sayıyor ve
		// ortak dünya her sunucuda sessizce devre dışı kalıyordu.
		linkCoordinatorURL(),
		// Sunucunun kendi kimliği: mod, topolojide hangi düğüm olduğunu
		// bundan biliyor. Eskiden adrese göre tahmin ediliyordu ve aynı
		// makinedeki iki sunucu AYNI adrese sahip olduğu için ayırt
		// edilemiyordu.
		"MCOS_SERVER_ID="+srv.ID,
	)
	return env
}

// Note writes an MCOS line into a server's console (kullanıcı nedenini
// panelde görsün; günlük dosyası panelden okunmuyor).
func (m *Manager) Note(id, line string) { m.onConsoleLine(id, line) }

// extraEnv returns the daemon-supplied variables for srv (bkz. ExtraEnv).
func (m *Manager) extraEnv(srv *model.Server) []string {
	if m.ExtraEnv == nil {
		return nil
	}
	return m.ExtraEnv(srv)
}

// linkCoordinatorURL is the base URL the mod uses to reach this daemon.
func linkCoordinatorURL() string {
	return fmt.Sprintf("MCOS_LINK_COORDINATOR=http://127.0.0.1:%d", model.CoordinatorPort)
}
