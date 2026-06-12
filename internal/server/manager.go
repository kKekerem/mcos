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

	r.setState(model.StateStarting)
	if err := m.EnsureInstalled(ctx, srv); err != nil {
		r.setState(model.StateError)
		return fmt.Errorf("install: %w", err)
	}
	li, err := m.loadLaunch(srv.ID)
	if err != nil {
		return fmt.Errorf("load launch info: %w", err)
	}

	javaBin, jvmArgs, err := m.java.BindForServer(srv)
	if err != nil {
		r.setState(model.StateError)
		return fmt.Errorf("java bind: %w", err)
	}

	args, err := buildLaunchArgs(jvmArgs, li)
	if err != nil {
		return err
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
	}
	policy := supervisor.RestartPolicy{OnCrash: srv.RestartOnCrash, MaxRestarts: 10, Backoff: 5 * time.Second}

	proc := m.sup.Add(srv.ID, spec, policy)
	r.mu.Lock()
	r.proc = proc
	r.mu.Unlock()

	m.logf("server: starting %q (%s, java=%s)", srv.Name, srv.Software, javaBin)
	if err := m.sup.Start(srv.ID); err != nil {
		r.setState(model.StateError)
		return err
	}
	return nil
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
		if p := m.rt(srv.ID).proc; p != nil {
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

// onConsoleLine records output and updates derived state (running / players).
func (m *Manager) onConsoleLine(id, line string) {
	r := m.rt(id)
	r.console.Add(log.Entry{Time: time.Now(), Message: line})

	switch {
	case strings.Contains(line, "Done (") && strings.Contains(line, "For help"):
		if r.getState() == model.StateStarting {
			r.setState(model.StateRunning)
			m.logf("server: %s is up", id)
		}
	case strings.Contains(line, "joined the game"):
		r.mu.Lock()
		r.players++
		r.mu.Unlock()
	case strings.Contains(line, "left the game"):
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
		// Crash: supervisor may restart; reflect that as starting, else error.
		r.setState(model.StateError)
	}
}
