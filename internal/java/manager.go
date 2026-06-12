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

// Manager owns the set of installed JDKs: resolving the right major for a
// Minecraft version, installing it from Adoptium on demand, registering it, and
// binding a runtime + JVM flags for a server launch.
type Manager struct {
	store  *store.Store
	log    *log.Logger
	client *http.Client
	mu     sync.Mutex // serializes installs (avoid two downloads of same major)
}

// NewManager constructs a Java manager.
func NewManager(st *store.Store, lg *log.Logger) *Manager {
	return &Manager{store: st, log: lg, client: defaultHTTPClient()}
}

// List returns all registered runtimes, sorted by major.
func (m *Manager) List() ([]model.JavaRuntime, error) {
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return nil, err
	}
	rts := ix.Runtimes
	sort.Slice(rts, func(i, j int) bool { return rts[i].Major < rts[j].Major })
	return rts, nil
}

// Resolve reports the Java major a Minecraft version needs, and whether it is
// already installed.
func (m *Manager) Resolve(mcVersion string) (major int, installed bool, err error) {
	major = RequiredJavaMajor(mcVersion)
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return major, false, err
	}
	return major, ix.Find(major) != nil, nil
}

// Get returns the registered runtime for a major, if present.
func (m *Manager) Get(major int) (*model.JavaRuntime, bool, error) {
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return nil, false, err
	}
	rt := ix.Find(major)
	return rt, rt != nil, nil
}

// Ensure returns the runtime for a major, installing it from Adoptium if it is
// not yet present.
func (m *Manager) Ensure(major int) (*model.JavaRuntime, error) {
	if rt, ok, err := m.Get(major); err != nil {
		return nil, err
	} else if ok {
		return rt, nil
	}
	return m.Install(major)
}

// Install downloads and registers a Temurin JDK for the given major version.
func (m *Manager) Install(major int) (*model.JavaRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-check under the lock in case a concurrent caller already installed it.
	if rt, ok, _ := m.Get(major); ok {
		return rt, nil
	}

	url := temurinURL(major)
	m.logf("java: installing Temurin %d from %s", major, url)
	if err := os.MkdirAll(m.store.Paths.JavaDir(), 0o755); err != nil {
		return nil, err
	}
	archive, err := downloadFile(m.client, url, m.store.Paths.JavaDir())
	if err != nil {
		return nil, err
	}
	defer os.Remove(archive)

	dest := filepath.Join(m.store.Paths.JavaDir(), fmt.Sprintf("temurin-%d", major))
	_ = os.RemoveAll(dest)
	javaHome, err := extractArchive(archive, dest)
	if err != nil {
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
		return nil, err
	}
	m.logf("java: installed Temurin %d (%s) at %s", major, rt.Version, javaHome)
	return &rt, nil
}

// Remove unregisters a major and deletes its files.
func (m *Manager) Remove(major int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		return err
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
