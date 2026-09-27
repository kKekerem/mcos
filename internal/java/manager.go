package java

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/store"
)

// DownloadProgress holds the live download state for a Java major version.
type DownloadProgress struct {
	Major      int    `json:"major"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Percent    int    `json:"percent"`
	Status     string `json:"status"`
	Done       bool   `json:"done"`
	Error      string `json:"error,omitempty"`
}

// Manager owns the set of installed JDKs: resolving the right major for a
// Minecraft version, installing it from Adoptium on demand, registering it, and
// binding a runtime + JVM flags for a server launch.
type Manager struct {
	store    *store.Store
	log      *log.Logger
	client   *http.Client
	mu       sync.Mutex // serializes installs (avoid two downloads of same major)
	progMu   sync.Mutex
	progress map[int]*DownloadProgress
	// builtinRoot, imajla gelen JRE'lerin arandığı kök (bkz. builtin.go).
	// Boşsa gömülü tarama kapalıdır; testler geçici bir dizin verir.
	builtinRoot string
}

// NewManager constructs a Java manager.
func NewManager(st *store.Store, lg *log.Logger) *Manager {
	return &Manager{
		store:    st,
		log:      lg,
		client:   defaultHTTPClient(),
		progress: make(map[int]*DownloadProgress),

		builtinRoot: defaultBuiltinRoot(),
	}
}

// ProgressMap returns a copy of active and recently completed download progresses.
func (m *Manager) ProgressMap() map[int]DownloadProgress {
	m.progMu.Lock()
	defer m.progMu.Unlock()
	res := make(map[int]DownloadProgress)
	for k, v := range m.progress {
		if v != nil {
			res[k] = *v
		}
	}
	return res
}

// List returns every usable runtime — the image's built-in ones plus the
// registered downloads — one per major, sorted by major.
//
// Gömülü bir ana sürüm, kayıt defterindeki aynı ana sürümü GÖLGELER (bkz.
// Get). Listede ikisini birden göstermek, "hangisi kullanılacak?" sorusunu
// kullanıcıya bırakmak olurdu; oysa cevap her zaman gömülü olandır.
func (m *Manager) List() ([]model.JavaRuntime, error) {
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return nil, err
	}
	builtin := m.Builtin()
	have := make(map[int]bool, len(builtin))
	rts := make([]model.JavaRuntime, 0, len(builtin)+len(ix.Runtimes))
	for _, rt := range builtin {
		have[rt.Major] = true
		rts = append(rts, rt)
	}
	for _, rt := range ix.Runtimes {
		if !have[rt.Major] {
			rts = append(rts, rt)
		}
	}
	sort.Slice(rts, func(i, j int) bool { return rts[i].Major < rts[j].Major })
	return rts, nil
}

// Resolve reports the Java major a Minecraft version needs, and whether it is
// already installed.
func (m *Manager) Resolve(mcVersion string) (major int, installed bool, err error) {
	major = RequiredJavaMajor(mcVersion)
	_, installed, err = m.Get(major)
	return major, installed, err
}

// Get returns the runtime for a major, if present: the image's built-in one
// first, then a registered download.
//
// Gömülü olan ÖNCE gelir. Önceki imajlarda "Java 21 kur"a basmış bir
// kullanıcının /data/java/temurin-21 kopyası kalıcı bölümde duruyor; o
// kopya imajın SHA-256'sı doğrulanmış JRE'sinden eski olabilir ve diskten
// okunur. Gömülü olan zaten RAM'dedir ve bu imajla sınanmıştır.
func (m *Manager) Get(major int) (*model.JavaRuntime, bool, error) {
	if rt := m.builtinFor(major); rt != nil {
		return rt, true, nil
	}
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return nil, false, err
	}
	rt := ix.Find(major)
	return rt, rt != nil, nil
}

// Ensure returns the runtime for a major, installing it from Adoptium if it is
// not yet present. A built-in major never touches the network.
func (m *Manager) Ensure(major int) (*model.JavaRuntime, error) {
	if rt, ok, err := m.Get(major); err != nil {
		return nil, err
	} else if ok {
		return rt, nil
	}
	return m.Install(major)
}

// Install downloads and registers a Temurin JDK for the given major version.
//
// Gömülü bir ana sürüm için HİÇBİR ŞEY indirilmez ve gömülü çalışma zamanı
// döner: sihirbazın "Gerekli Java'yı kur" adımı ve eski istemcilerin
// java.install çağrısı böylece internetsiz de başarıyla biter.
func (m *Manager) Install(major int) (*model.JavaRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-check under the lock in case a concurrent caller already installed it
	// (or the image ships it built in).
	if rt, ok, _ := m.Get(major); ok {
		return rt, nil
	}

	if err := os.MkdirAll(m.store.Paths.JavaDir(), 0o755); err != nil {
		return nil, err
	}

	prog := &DownloadProgress{
		Major:   major,
		Status:  "Bağlanıyor...",
		Percent: 0,
	}
	m.progMu.Lock()
	m.progress[major] = prog
	m.progMu.Unlock()

	fail := func(err error) (*model.JavaRuntime, error) {
		m.progMu.Lock()
		prog.Status = "Hata"
		prog.Done = true
		prog.Error = err.Error()
		m.progMu.Unlock()
		return nil, err
	}

	// Önce yerel arşiv (çevrimdışı paket, bkz. LocalArchiveDirs): internetsiz
	// makinede 26.x sunucusu da kurulabilsin. Yerel arşiv SİLİNMEZ — tohumlanmış
	// bir paket dosyasıdır, bir sonraki kurulum da onu kullanır.
	archive := localArchive(major)
	if archive != "" {
		m.logf("java: installing Temurin %d from local archive %s", major, archive)
	} else {
		var err error
		for _, image := range []string{"jre", "jdk"} {
			url := temurinURL(major, image)
			m.logf("java: installing Temurin %d from %s", major, url)
			archive, err = downloadFileWithProgress(m.client, url, m.store.Paths.JavaDir(), func(dl, tot int64) {
				m.progMu.Lock()
				defer m.progMu.Unlock()
				prog.Downloaded = dl
				prog.Total = tot
				if tot > 0 {
					prog.Percent = int((dl * 100) / tot)
				} else {
					prog.Percent = 50
				}
				prog.Status = fmt.Sprintf("İndiriliyor... %% %d (%d MB / %d MB)", prog.Percent, dl/(1024*1024), tot/(1024*1024))
			})
			if err == nil {
				break
			}
		}
		if err != nil {
			return fail(err)
		}
		defer os.Remove(archive)
	}

	m.progMu.Lock()
	prog.Status = "Çıkartılıyor ve kuruluyor..."
	prog.Percent = 95
	m.progMu.Unlock()

	dest := filepath.Join(m.store.Paths.JavaDir(), fmt.Sprintf("temurin-%d", major))
	_ = os.RemoveAll(dest)
	javaHome, err := extractArchive(archive, dest)
	if err != nil {
		m.progMu.Lock()
		prog.Status = "Hata"
		prog.Done = true
		prog.Error = err.Error()
		m.progMu.Unlock()
		return nil, fmt.Errorf("java: extract: %w", err)
	}
	javaBin := filepath.Join(javaHome, "bin", javaExe())

	rt := model.JavaRuntime{
		Major:       major,
		Version:     probeVersion(javaBin),
		Vendor:      "temurin",
		Path:        javaHome,
		JavaBin:     javaBin,
		InstalledAt: time.Now(),
	}
	if err := m.register(rt); err != nil {
		m.progMu.Lock()
		prog.Status = "Hata"
		prog.Done = true
		prog.Error = err.Error()
		m.progMu.Unlock()
		return nil, err
	}
	m.logf("java: installed Temurin %d (%s) at %s", major, rt.Version, javaHome)

	m.progMu.Lock()
	prog.Status = "Tamamlandı"
	prog.Percent = 100
	prog.Done = true
	m.progMu.Unlock()

	return &rt, nil
}

// Remove unregisters a major and deletes its files.
//
// Gömülü bir ana sürümün yalnızca kayıt defterindeki (indirilmiş, gölgede
// kalan) kopyası silinir; kopya yoksa ErrBuiltinRemove döner. Gömülü dosyalar
// rootfs'te durur ve silinmez.
func (m *Manager) Remove(major int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return err
	}
	if m.builtinFor(major) != nil && ix.Find(major) == nil {
		return fmt.Errorf("java %d: %w", major, ErrBuiltinRemove)
	}
	out := ix.Runtimes[:0]
	var removed *model.JavaRuntime
	for i := range ix.Runtimes {
		if ix.Runtimes[i].Major == major {
			r := ix.Runtimes[i]
			removed = &r
			continue
		}
		out = append(out, ix.Runtimes[i])
	}
	ix.Runtimes = out
	if err := m.store.SaveJavaIndex(ix); err != nil {
		return err
	}
	if removed != nil && removed.Path != "" {
		// Only remove dirs we manage (under the java dir).
		if filepath.Dir(filepath.Clean(removed.Path)) == filepath.Clean(m.store.Paths.JavaDir()) ||
			isUnderManaged(removed.Path, m.store.Paths.JavaDir()) {
			_ = os.RemoveAll(managedRoot(removed.Path, m.store.Paths.JavaDir()))
		}
	}
	return nil
}

// BindForServer resolves the java binary path and JVM args for a server launch,
// honoring a manual JavaPath override and auto-installing the required runtime.
func (m *Manager) BindForServer(srv *model.Server) (javaBin string, args []string, err error) {
	if srv.JavaPath != "" {
		javaBin = srv.JavaPath
	} else {
		rt, err := m.Ensure(srv.JavaMajor)
		if err != nil {
			return "", nil, err
		}
		javaBin = rt.JavaBin
	}
	args = JVMArgs(srv.JVMFlags, srv.RAMMB, srv.JavaMajor)
	return javaBin, args, nil
}

// register inserts/replaces a runtime in the index.
func (m *Manager) register(rt model.JavaRuntime) error {
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return err
	}
	replaced := false
	for i := range ix.Runtimes {
		if ix.Runtimes[i].Major == rt.Major {
			ix.Runtimes[i] = rt
			replaced = true
			break
		}
	}
	if !replaced {
		ix.Runtimes = append(ix.Runtimes, rt)
	}
	return m.store.SaveJavaIndex(ix)
}

func (m *Manager) logf(format string, a ...any) {
	if m.log != nil {
		m.log.Infof(format, a...)
	}
}

func javaExe() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// probeVersion runs `java -version` and returns the first line, best-effort.
func probeVersion(javaBin string) string {
	cmd := exec.Command(javaBin, "-version")
	var buf bytes.Buffer
	cmd.Stderr = &buf
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	line := buf.String()
	if i := bytes.IndexByte([]byte(line), '\n'); i > 0 {
		line = line[:i]
	}
	return line
}

// isUnderManaged reports whether p lives under the managed java dir.
func isUnderManaged(p, javaDir string) bool {
	rel, err := filepath.Rel(filepath.Clean(javaDir), filepath.Clean(p))
	return err == nil && rel != ".." && !bytes.HasPrefix([]byte(rel), []byte(".."))
}

// managedRoot returns the temurin-<major> dir under javaDir that contains p,
// so Remove deletes the whole runtime tree, not just JAVA_HOME.
func managedRoot(p, javaDir string) string {
	clean := filepath.Clean(p)
	dir := clean
	for {
		parent := filepath.Dir(dir)
		if parent == filepath.Clean(javaDir) || parent == dir {
			return dir
		}
		dir = parent
	}
}
