package supervisor

import (
	"bytes"
	"sync"
)

// lineWriter splits incoming bytes into lines and invokes a callback per line.
// It is used to forward a child process's stdout/stderr into the log ring and
// to track the most recent line for server cards.
type lineWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	emit func(line string)
}

func newLineWriter(emit func(line string)) *lineWriter {
	return &lineWriter{emit: emit}
}

// Write implements io.Writer. Partial lines are buffered until a newline.
func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		data := w.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(bytes.TrimRight(data[:idx], "\r"))
		w.buf.Next(idx + 1)
		if w.emit != nil {
			w.emit(line)
		}
	}
	// Guard against unbounded growth on a process emitting no newlines.
	if w.buf.Len() > 1<<20 {
		line := w.buf.String()
		w.buf.Reset()
		if w.emit != nil {
			w.emit(line)
		}
	}
	return len(p), nil
}

// Flush emits any buffered partial line.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() > 0 && w.emit != nil {
		w.emit(w.buf.String())
		w.buf.Reset()
	}
}
