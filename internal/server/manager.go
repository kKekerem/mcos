// Package server implements the Minecraft server lifecycle: installing software
// via providers, launching it with the correct Java runtime and JVM flags under
// the process supervisor, capturing its console, and tracking live state.
package server

import (
	"context"
	"fmt"
	"net/http"
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

		// ISLETIM SISTEMI duzeyinde kaynak tavani. Eskiden CPUQuota yalnizca
		// saklaniyordu ve HICBIR YERDE uygulanmiyordu; artik cgroup v2 ile
		// gercekten uygulaniyor.
		ID:     srv.ID,
		Limits: limitsForServer(srv),
		OnLimits: func(desc string) {
			m.logf("server: %q kaynak sinirlari uygulandi: %s", srv.Name, desc)
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

	// Bellek tavani yigindan (-Xmx) %25 fazla verilir.
	//
	// NEDEN FAZLA: JVM yigin disinda da bellek kullanir - metaspace, kod
	// onbellegi, dogrudan arabellekler, is parcacigi yiginlari. Tavani tam
	// -Xmx'e esitlemek, sunucuyu yigin dolmadan cekirdek tarafindan
	// oldurtur (OOM) ve kullanici sebebini anlayamaz.
	if srv.RAMMB > 0 {
		lim.MemoryMB = srv.RAMMB + srv.RAMMB/4 + 256
	}

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
