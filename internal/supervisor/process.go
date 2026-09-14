// Package supervisor manages long-running child processes (Minecraft servers,
// wan) with stdin command injection, output capture, graceful stop and
// crash-restart policy. It is shared by the server lifecycle and tunnel layers.
package supervisor

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// State is the lifecycle of a supervised process.
type State string

const (
	StateStopped  State = "stopped"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateExited   State = "exited"
)

// Spec describes how to launch and supervise a process.
type Spec struct {
	Dir  string   // working directory
	Path string   // executable path
	Args []string // arguments (not including Path)
	Env  []string // extra environment, appended to os.Environ by caller if needed

	// GracefulStop, if set, requests a clean shutdown (e.g. write "stop\n" to
	// stdin) before terminate/kill is used. It should return quickly.
	GracefulStop func(p *Process) error
	// StopTimeout is how long to wait after GracefulStop/terminate before kill.
	StopTimeout time.Duration

	// OnLine is invoked for every line of captured stdout/stderr.
	OnLine func(line string)
	// OnExit is invoked once when the process exits (code, err). For an
	// unrequested exit, restart policy is applied by the Supervisor.
	OnExit func(code int, err error)

	// Nice is the process scheduling priority applied after start (Linux only):
	// negative = higher priority (full-performance), positive = background.
	Nice int
	// CPUAffinity pins the process to specific CPU cores (Linux only). Empty =
	// all cores.
	CPUAffinity []int

	// ID, kaynak grubunu (cgroup) adlandirmak icin kullanilir. Bos ise
	// cgroup olusturulmaz.
	ID string
	// Limits, ISLETIM SISTEMI duzeyinde uygulanan kaynak tavanidir.
	//
	// -Xmx'ten FARKLIDIR: -Xmx yalnizca JVM yiginini sinirlar, bu ise
	// surecin TAMAMINI sinirlar. Eskiden "sunucu basina CPU payi" ayari
	// hicbir yerde uygulanmiyordu; artik burada uygulaniyor.
	Limits Limits
	// OnLimits, uygulanan sinirlari bildirir (bos = sinir yok). Cagiran
	// bunu kullaniciya gosterir: "kayitli" ile "uygulaniyor" arasindaki
	// farkin GORULEBILMESI gerekir.
	OnLimits func(desc string)
}

// Process is a single supervised child.
type Process struct {
	spec Spec

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	state     State
	pid       int
	startedAt time.Time
	lastLine  string
	requested bool // true if a stop was requested (clean exit expected)
	exitCode  int
	exitErr   error
	done      chan struct{}
}

// NewProcess creates a process from a spec; it is not started yet.
func NewProcess(spec Spec) *Process {
	if spec.StopTimeout == 0 {
		spec.StopTimeout = 30 * time.Second
	}
	return &Process{spec: spec, state: StateStopped}
}

// State returns the current lifecycle state.
func (p *Process) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// PID returns the OS process id, or 0 if not running.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// UptimeSec returns seconds since start, or 0 if not running.
func (p *Process) UptimeSec() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != StateRunning || p.startedAt.IsZero() {
		return 0
	}
	return int64(time.Since(p.startedAt).Seconds())
}

// LastLine returns the most recent captured output line.
func (p *Process) LastLine() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastLine
}

// Start launches the process and begins capturing output.
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StateRunning || p.state == StateStopping {
		return fmt.Errorf("supervisor: already running")
	}
	cmd := exec.Command(p.spec.Path, p.spec.Args...)
	cmd.Dir = p.spec.Dir
	if len(p.spec.Env) > 0 {
		cmd.Env = p.spec.Env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("supervisor: stdin pipe: %w", err)
	}
	lw := newLineWriter(func(line string) {
		p.mu.Lock()
		p.lastLine = line
		p.mu.Unlock()
		if p.spec.OnLine != nil {
			p.spec.OnLine(line)
		}
	})
	cmd.Stdout = lw
	cmd.Stderr = lw

	if err := cmd.Start(); err != nil {
		stdin.Close()
		errMsg := fmt.Sprintf("[MCOS HATA] Sunucu başlatılamadı (%s): %v", p.spec.Path, err)
		lw.Write([]byte(errMsg + "\n"))
		lw.Flush()
		return fmt.Errorf("supervisor: start: %w", err)
	}
	p.cmd = cmd
	p.stdin = stdin
	p.pid = cmd.Process.Pid
	p.state = StateRunning
	p.startedAt = time.Now()
	p.requested = false
	p.done = make(chan struct{})

	// Apply OS scheduling limits (nice / CPU affinity). No-op off Linux.
	applyLimits(p.pid, p.spec)

	// Kaynak grubunu sureci baslatir baslatmaz uygula. Once sinirlari kur,
	// sonra sureci gruba tasi (ApplyCgroup bu sirayi kendisi korur), yoksa
	// surec kisa bir sure sinirsiz calisir.
	if p.spec.ID != "" {
		if desc := ApplyCgroup(p.spec.ID, p.pid, p.spec.Limits); desc != "" {
			if p.spec.OnLimits != nil {
				p.spec.OnLimits(desc)
			}
		}
	}

	go p.wait(lw)
	return nil
}

func (p *Process) wait(lw *lineWriter) {
	err := p.cmd.Wait()

	// Kaynak grubunu temizle. Yapilmazsa her baslatma bos bir cgroup dizini
	// birakir; binlerce bos grup cekirdek bellegini bosa harcar.
	if p.spec.ID != "" {
		RemoveCgroup(p.spec.ID)
	}

	p.mu.Lock()
	code := 0
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	p.exitCode = code
	p.exitErr = err
	p.state = StateExited
	p.pid = 0
	requested := p.requested
	onExit := p.spec.OnExit
	done := p.done
	p.mu.Unlock()

	if (err != nil || code != 0) && !requested {
		errMsg := fmt.Sprintf("[MCOS HATA] Süreç beklenmeyen bir şekilde sonlandı (Çıkış kodu: %d, Hata: %v)", code, err)
		lw.Write([]byte(errMsg + "\n"))
	}
	lw.Flush()

	if onExit != nil {
		onExit(code, err)
	}
	close(done)
}

// WriteStdin sends a line to the process's stdin (newline appended).
func (p *Process) WriteStdin(line string) error {
	p.mu.Lock()
	stdin := p.stdin
	running := p.state == StateRunning
	p.mu.Unlock()
	if !running || stdin == nil {
		return fmt.Errorf("supervisor: not running")
	}
	_, err := io.WriteString(stdin, line+"\n")
	return err
}

// Requested reports whether the last/ongoing exit was due to an explicit Stop.
func (p *Process) Requested() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requested
}

// Stop requests a graceful shutdown, escalating to terminate then kill if the
// process does not exit within StopTimeout.
func (p *Process) Stop() error {
	p.mu.Lock()
	if p.state != StateRunning {
		p.mu.Unlock()
		return nil
	}
	p.state = StateStopping
	p.requested = true
	proc := p.cmd.Process
	graceful := p.spec.GracefulStop
	timeout := p.spec.StopTimeout
	done := p.done
	p.mu.Unlock()

	if graceful != nil {
		_ = graceful(p)
	} else if proc != nil {
		_ = terminate(proc)
	}

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
	}

	// Escalate: terminate, then hard kill.
	if proc != nil {
		_ = terminate(proc)
	}
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
	}
	if proc != nil {
		_ = proc.Kill()
	}
	<-done
	return nil
}

// Wait blocks until the process has exited. Returns immediately if not started.
func (p *Process) Wait() {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done != nil {
		<-done
	}
}
