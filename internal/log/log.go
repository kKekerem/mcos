// Package log provides structured logging for the daemon plus an in-memory ring
// buffer used to stream recent lines to the panel's live console view.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level is a log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DBG"
	case LevelInfo:
		return "INF"
	case LevelWarn:
		return "WRN"
	case LevelError:
		return "ERR"
	default:
		return "???"
	}
}

// Entry is a single structured log line.
type Entry struct {
	Time    time.Time `json:"time"`
	Level   Level     `json:"level"`
	Scope   string    `json:"scope"` // subsystem, e.g. "server:srv_ab12"
	Message string    `json:"message"`
}

// Logger writes entries to an io.Writer and mirrors them into a ring buffer.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	min   Level
	ring  *Ring
	scope string
}

// New creates a logger writing to out (typically a file or os.Stderr).
func New(out io.Writer, min Level, ringSize int) *Logger {
	return &Logger{out: out, min: min, ring: NewRing(ringSize)}
}

// WithScope returns a child logger that tags every entry with scope. The ring
// buffer and output are shared with the parent.
func (l *Logger) WithScope(scope string) *Logger {
	return &Logger{out: l.out, min: l.min, ring: l.ring, scope: scope}
}

// Ring returns the shared ring buffer for console streaming.
func (l *Logger) Ring() *Ring { return l.ring }

func (l *Logger) log(lvl Level, format string, args ...any) {
	if lvl < l.min {
		return
	}
	e := Entry{Time: time.Now(), Level: lvl, Scope: l.scope, Message: fmt.Sprintf(format, args...)}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.out != nil {
		scope := e.Scope
		if scope == "" {
			scope = "-"
		}
		fmt.Fprintf(l.out, "%s %s [%s] %s\n",
			e.Time.Format("2006-01-02 15:04:05.000"), lvl, scope, e.Message)
	}
	if l.ring != nil {
		l.ring.Add(e)
	}
}

func (l *Logger) Debugf(f string, a ...any) { l.log(LevelDebug, f, a...) }
func (l *Logger) Infof(f string, a ...any)  { l.log(LevelInfo, f, a...) }
func (l *Logger) Warnf(f string, a ...any)  { l.log(LevelWarn, f, a...) }
func (l *Logger) Errorf(f string, a ...any) { l.log(LevelError, f, a...) }

// Default is a process-wide logger writing to stderr, for early startup before
// the file logger is configured.
var Default = New(os.Stderr, LevelInfo, 512)
