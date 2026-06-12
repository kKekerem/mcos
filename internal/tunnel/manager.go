// Package tunnel manages public-tunnel processes (Serveo) and a short-code
// system that compresses long tunnel commands into a 5-character shareable code.
// The code can be entered on another PC / OS instance to resolve the original
// command and start the tunnel.
package tunnel

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcos/internal/log"
	"mcos/internal/store"
)

// Manager owns tunnel processes and the short-code registry.
type Manager struct {
	mu       sync.RWMutex
	codes    map[string]string // code → full command
	procs    map[string]*exec.Cmd
	statuses map[string]*Status
	log      *log.Logger
	store    *store.Store
}

// Status tracks a tunnel's runtime state.
type Status struct {
	Running   bool      `json:"running"`
	Command   string    `json:"command,omitempty"`
	Code      string    `json:"code,omitempty"`
	Hostname  string    `json:"hostname,omitempty"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Log       []string  `json:"log,omitempty"`
	AutoStart bool      `json:"autoStart,omitempty"`
}

// NewManager creates the tunnel manager.
func NewManager(st *store.Store, lg *log.Logger) *Manager {
	m := &Manager{
		codes:    map[string]string{},
		procs:    map[string]*exec.Cmd{},
		statuses: map[string]*Status{},
		log:      lg,
		store:    st,
	}
	m.loadCodes()
	return m
}

// Compress turns a long command into a 5-character code.
func (m *Manager) Compress(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	// Hash the command and encode first 5 chars in base62.
	h := sha256.Sum256([]byte(cmd))
	code := encodeBase62(h[:])[:5]

	m.mu.Lock()
	m.codes[code] = cmd
	m.mu.Unlock()
	m.saveCodes()
	return code
}

// Resolve returns the original command for a given 5-char code.
func (m *Manager) Resolve(code string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cmd, ok := m.codes[code]
	return cmd, ok
}

// Start runs a tunnel by code or command string.
func (m *Manager) Start(codeOrCmd string) error {
	cmdStr, ok := m.Resolve(codeOrCmd)
	if !ok {
		// treat input as raw command
		cmdStr = codeOrCmd
	}
	if strings.TrimSpace(cmdStr) == "" {
		return fmt.Errorf("empty command")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if st, ok := m.statuses[codeOrCmd]; ok && st.Running {
		return fmt.Errorf("already running")
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	// locate the tunnel binary (ssh for Serveo) or fall back to PATH
	bin := m.findBin(parts[0])
	if bin == "" {
		return fmt.Errorf("ssh not found (serveo tunnel needs the ssh client)")
	}

	ctx := exec.Command(bin, parts[1:]...)
	ctx.Stdout = &logWriter{m: m, key: codeOrCmd}
	ctx.Stderr = ctx.Stdout
	if err := ctx.Start(); err != nil {
		return err
	}

	m.procs[codeOrCmd] = ctx
	m.statuses[codeOrCmd] = &Status{
		Running:   true,
		Command:   cmdStr,
		Code:      codeOrCmd,
		PID:       ctx.Process.Pid,
		StartedAt: time.Now(),
		AutoStart: false,
	}
	if m.log != nil {
		m.log.Infof("tunnel: started %s pid=%d", codeOrCmd, ctx.Process.Pid)
	}
	go m.waitExit(codeOrCmd, ctx)
	return nil
}

func (m *Manager) waitExit(key string, cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	if st, ok := m.statuses[key]; ok {
		st.Running = false
		st.PID = 0
	}
	delete(m.procs, key)
	m.mu.Unlock()
	if m.log != nil {
		if err != nil {
			m.log.Warnf("tunnel: %s exited: %v", key, err)
		} else {
			m.log.Infof("tunnel: %s exited normally", key)
		}
	}
}

// Stop terminates a running tunnel.
func (m *Manager) Stop(key string) error {
	m.mu.Lock()
	cmd, ok := m.procs[key]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("not running")
	}
	if cmd.Process != nil {
		cmd.Process.Signal(os.Interrupt)
		time.Sleep(500 * time.Millisecond)
		cmd.Process.Kill()
	}
	return nil
}

// Status returns the status for a given tunnel key.
func (m *Manager) Status(key string) *Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.statuses[key]
	if !ok {
		return &Status{Code: key, Running: false}
	}
	cp := *st
	// copy log slice
	cp.Log = append([]string(nil), st.Log...)
	if len(cp.Log) > 50 {
		cp.Log = cp.Log[len(cp.Log)-50:]
	}
	return &cp
}

// List returns all known tunnel statuses.
func (m *Manager) List() []*Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Status, 0, len(m.statuses))
	for _, st := range m.statuses {
		cp := *st
		out = append(out, &cp)
	}
	return out
}

// SetAutoStart marks a tunnel for automatic startup.
func (m *Manager) SetAutoStart(key string, auto bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.statuses[key]; ok {
		st.AutoStart = auto
	}
}

// findBin resolves the executable for a tunnel command. Serveo tunnels start
// with "ssh", which the OpenSSH package ships at /usr/bin/ssh; any other first
// token is looked up on PATH so a custom command still works.
func (m *Manager) findBin(first string) string {
	if first == "" {
		first = "ssh"
	}
	if strings.ContainsRune(first, '/') {
		if _, err := os.Stat(first); err == nil {
			return first
		}
	}
	for _, p := range []string{
		"/usr/bin/" + first,
		"/usr/local/bin/" + first,
		"/bin/" + first,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(first); err == nil {
		return p
	}
	return ""
}

func (m *Manager) saveCodes() {
	// simplistic JSON persistence under data-root
	path := filepath.Join(m.store.Paths.Root, "tunnel-codes.json")
	b, _ := json.Marshal(m.codes)
	os.WriteFile(path, b, 0644)
}

func (m *Manager) loadCodes() {
	path := filepath.Join(m.store.Paths.Root, "tunnel-codes.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(b, &m.codes)
}

// logWriter captures stdout/stderr lines into a tunnel's log buffer.
type logWriter struct {
	m   *Manager
	key string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}
	host := parseServeoHost(line)
	w.m.mu.Lock()
	if st, ok := w.m.statuses[w.key]; ok {
		st.Log = append(st.Log, line)
		if len(st.Log) > 100 {
			st.Log = st.Log[len(st.Log)-100:]
		}
		if host != "" {
			st.Hostname = host
		}
	}
	w.m.mu.Unlock()
	return len(p), nil
}

// parseServeoHost extracts the public address Serveo prints on connect, e.g.
// "Forwarding TCP connect from serveo.net:12345" or
// "Forwarding HTTP traffic from https://name.serveo.net". Returns "" otherwise.
func parseServeoHost(line string) string {
	if !strings.Contains(line, "serveo.net") {
		return ""
	}
	for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == '"'
	}) {
		if strings.Contains(tok, "serveo.net") {
			tok = strings.TrimPrefix(tok, "https://")
			tok = strings.TrimPrefix(tok, "http://")
			tok = strings.TrimPrefix(tok, "tcp://")
			return strings.TrimRight(tok, "/.")
		}
	}
	return ""
}

// encodeBase62 converts bytes to a base62 string (0-9, a-z, A-Z).
func encodeBase62(b []byte) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var result []byte
	num := new(big.Int).SetBytes(b)
	base := big.NewInt(62)
	zero := big.NewInt(0)
	mod := new(big.Int)
	for num.Cmp(zero) > 0 {
		num.DivMod(num, base, mod)
		result = append(result, alphabet[mod.Int64()])
	}
	// reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	if len(result) == 0 {
		return "0"
	}
	return string(result)
}
