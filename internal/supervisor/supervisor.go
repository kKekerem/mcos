package supervisor

import (
	"sync"
	"time"

	"mcos/internal/log"
)

// RestartPolicy controls automatic restart on unexpected exit.
type RestartPolicy struct {
	OnCrash     bool          // restart if the process exits without a Stop request
	MaxRestarts int           // 0 = unlimited
	Backoff     time.Duration // delay before restarting
}

// entry pairs a process with its policy and bookkeeping.
type entry struct {
	proc     *Process
	policy   RestartPolicy
	restarts int
	stopped  bool // entry explicitly removed/stopped, do not restart
}

// Supervisor manages a set of named processes and applies restart policy.
type Supervisor struct {
	log     *log.Logger
	mu      sync.Mutex
	entries map[string]*entry
}

// New creates an empty supervisor.
func New(lg *log.Logger) *Supervisor {
	return &Supervisor{log: lg, entries: map[string]*entry{}}
}

// Add registers (but does not start) a named process with a restart policy. The
// spec's OnExit is wrapped to drive the restart logic; any caller OnExit still
// runs first.
func (s *Supervisor) Add(name string, spec Spec, policy RestartPolicy) *Process {
	s.mu.Lock()
	defer s.mu.Unlock()

	userOnExit := spec.OnExit
	spec.OnExit = func(code int, err error) {
		if userOnExit != nil {
			userOnExit(code, err)
		}
		s.handleExit(name, code, err)
	}
	p := NewProcess(spec)
	s.entries[name] = &entry{proc: p, policy: policy}
	return p
}

// Get returns the process registered under name, or nil.
func (s *Supervisor) Get(name string) *Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[name]; ok {
		return e.proc
	}
	return nil
}

// Start launches a registered process.
func (s *Supervisor) Start(name string) error {
	s.mu.Lock()
	e, ok := s.entries[name]
	if ok {
		e.stopped = false
		e.restarts = 0
	}
	s.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	return e.proc.Start()
}

// Stop requests a graceful stop and disables restart for the entry.
func (s *Supervisor) Stop(name string) error {
	s.mu.Lock()
	e, ok := s.entries[name]
	if ok {
		e.stopped = true
	}
	s.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	return e.proc.Stop()
}

// Remove stops and unregisters a process.
func (s *Supervisor) Remove(name string) {
	s.mu.Lock()
	e, ok := s.entries[name]
	if ok {
		e.stopped = true
		delete(s.entries, name)
	}
	s.mu.Unlock()
	if ok {
		_ = e.proc.Stop()
	}
}

// StopAll gracefully stops every managed process (used on daemon shutdown).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	var procs []*Process
	for _, e := range s.entries {
		e.stopped = true
		procs = append(procs, e.proc)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p *Process) { defer wg.Done(); _ = p.Stop() }(p)
	}
	wg.Wait()
}

func (s *Supervisor) handleExit(name string, code int, err error) {
	s.mu.Lock()
	e, ok := s.entries[name]
	if !ok {
		s.mu.Unlock()
		return
	}
	requested := e.proc.Requested()
	stopped := e.stopped
	policy := e.policy
	shouldRestart := !requested && !stopped && policy.OnCrash &&
		(policy.MaxRestarts == 0 || e.restarts < policy.MaxRestarts)
	if shouldRestart {
		e.restarts++
	}
	backoff := policy.Backoff
	proc := e.proc
	s.mu.Unlock()

	if s.log != nil {
		if requested || stopped {
			s.log.Infof("supervisor: %q exited (code=%d)", name, code)
		} else {
			s.log.Warnf("supervisor: %q crashed (code=%d err=%v) restart=%v", name, code, err, shouldRestart)
		}
	}
	if shouldRestart {
		if backoff > 0 {
			time.Sleep(backoff)
		}
		// Re-check the entry wasn't removed/stopped during backoff.
		s.mu.Lock()
		cur, ok := s.entries[name]
		stillWanted := ok && !cur.stopped && cur.proc == proc
		s.mu.Unlock()
		if stillWanted {
			if err := proc.Start(); err != nil && s.log != nil {
				s.log.Errorf("supervisor: restart %q failed: %v", name, err)
			}
		}
	}
}
