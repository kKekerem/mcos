package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcos/internal/log"
	"mcos/internal/supervisor"
)

// ProcName, Velocity sürecinin süpervizördeki adıdır.
//
// Sunucu kimlikleriyle çakışmaz: sunucular rastgele onaltılık kimlikle
// kaydedilir, bu ad tire içerir.
const ProcName = "mcos-velocity"

// Runner keeps one Velocity process matching the desired config.
//
// Sunucu yöneticisinin süreç düzeneğini (internal/supervisor) kullanır:
// çökerse yeniden başlatılır, MCOS kapanırken StopAll ile düzgün kapanır.
type Runner struct {
	sup *supervisor.Supervisor
	dir string
	lg  *log.Logger

	mu sync.Mutex
	// applied, çalışan sürecin tanımı (java + jar + yapılandırma). Aynı
	// tanım yeniden uygulanınca HİÇBİR ŞEY yapılmaz: Velocity yapılandırmayı
	// canlı okumadığı için her değişiklik yeniden başlatma, yani tüm
	// oyuncuların düşmesi demek; yalnızca liste gerçekten değişince olmalı.
	applied string

	// lastMu AYRI kilit: Stop/Apply mu'yu tutarken süreci durdurur ve
	// kapanan Velocity'nin son satırları onLine'a gelir. Aynı kilit
	// kullanılsaydı çıktı gorutini beklerdi, süreç "çıktı" sayılmaz ve
	// durdurma sonsuza dek asılı kalırdı.
	lastMu sync.Mutex
	last   string
}

// NewRunner builds a runner whose data folder is dir (ör. /data/proxy).
func NewRunner(sup *supervisor.Supervisor, dir string, lg *log.Logger) *Runner {
	return &Runner{sup: sup, dir: dir, lg: lg}
}

// Dir is the proxy's data folder.
func (r *Runner) Dir() string { return r.dir }

// Running reports whether Velocity is up.
func (r *Runner) Running() bool {
	p := r.sup.Get(ProcName)
	return p != nil && p.State() == supervisor.StateRunning
}

// LastLine is Velocity's last console line (panelde neden göstermek için).
func (r *Runner) LastLine() string {
	r.lastMu.Lock()
	defer r.lastMu.Unlock()
	return r.last
}

// Apply makes the running proxy match conf; restarted=true if it (re)started.
func (r *Runner) Apply(javaBin string, jar Jar, conf string) (restarted bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := javaBin + "\n" + jar.Path + "\n" + conf
	if r.applied == key && r.Running() {
		return false, nil
	}
	if r.sup.Get(ProcName) != nil {
		// Remove süreç çıkana kadar bekler: yeni Velocity aynı portu
		// dinleyecek.
		r.sup.Remove(ProcName)
	}
	r.applied = ""
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(r.dir, "velocity.toml"), []byte(conf), 0o644); err != nil {
		return false, err
	}
	spec := supervisor.Spec{
		Dir:  r.dir,
		Path: javaBin,
		// Velocity'nin kendi önerdiği bayraklar; 512 MB bir proxy için
		// fazlasıyla yeter (oyun durumunu tutmaz, yalnızca paket aktarır).
		Args: []string{"-Xms128M", "-Xmx512M", "-XX:+UseG1GC", "-XX:G1HeapRegionSize=4M",
			"-XX:+UnlockExperimentalVMOptions", "-XX:+ParallelRefProcEnabled",
			"-XX:MaxInlineLevel=15", "-jar", jar.Path},
		StopTimeout: 20 * time.Second,
		GracefulStop: func(p *supervisor.Process) error {
			return p.WriteStdin("shutdown")
		},
		OnLine: r.onLine,
	}
	r.sup.Add(ProcName, spec, supervisor.RestartPolicy{OnCrash: true, MaxRestarts: 10, Backoff: 5 * time.Second})
	if err := r.sup.Start(ProcName); err != nil {
		r.sup.Remove(ProcName)
		return false, err
	}
	r.applied = key
	return true, nil
}

// Stop shuts Velocity down (idempotent).
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sup.Get(ProcName) != nil {
		r.sup.Remove(ProcName)
	}
	r.applied = ""
}

// onLine keeps the last line and logs only what matters.
//
// Velocity her oyuncu bağlantısını yazar; hepsini günlüğe dökmek MCOS
// günlüğünü boğardı. Uyarılar, hatalar ve "dinliyor" satırı yeter.
func (r *Runner) onLine(line string) {
	r.lastMu.Lock()
	r.last = line
	r.lastMu.Unlock()
	if r.lg == nil {
		return
	}
	if strings.Contains(line, "ERROR") || strings.Contains(line, "WARN") ||
		strings.Contains(line, "Listening on") || strings.Contains(line, "Done (") {
		r.lg.Infof("proxy: %s", line)
	}
}
