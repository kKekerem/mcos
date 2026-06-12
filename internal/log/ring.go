package log

import "sync"

// Ring is a fixed-capacity, concurrency-safe circular buffer of log entries.
// It backs the live console view: the panel polls Tail/Since to render recent
// output without the daemon holding the whole history in memory.
type Ring struct {
	mu    sync.RWMutex
	buf   []Entry
	size  int
	start int   // index of oldest entry
	count int   // number of valid entries
	seq   int64 // monotonically increasing id of the next entry
}

// NewRing creates a ring holding up to size entries (minimum 1).
func NewRing(size int) *Ring {
	if size < 1 {
		size = 1
	}
	return &Ring{buf: make([]Entry, size), size: size}
}

// Add appends an entry, evicting the oldest if full.
func (r *Ring) Add(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := (r.start + r.count) % r.size
	if r.count == r.size {
		// full: overwrite oldest, advance start
		r.buf[r.start] = e
		r.start = (r.start + 1) % r.size
	} else {
		r.buf[idx] = e
		r.count++
	}
	r.seq++
}

// Seq returns the id that will be assigned to the next added entry. Callers can
// use this as a cursor with Since.
func (r *Ring) Seq() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seq
}

// Tail returns up to n most recent entries (oldest first).
func (r *Ring) Tail(n int) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n <= 0 || n > r.count {
		n = r.count
	}
	out := make([]Entry, 0, n)
	startOffset := r.count - n
	for i := 0; i < n; i++ {
		idx := (r.start + startOffset + i) % r.size
		out = append(out, r.buf[idx])
	}
	return out
}

// Since returns entries added after cursor (an earlier Seq value), along with
// the new cursor. If the cursor is older than the buffer window, it returns the
// full window. This lets the console view fetch only fresh lines.
func (r *Ring) Since(cursor int64) ([]Entry, int64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	newCursor := r.seq
	// number of entries added since cursor
	delta := newCursor - cursor
	if delta <= 0 {
		return nil, newCursor
	}
	if delta > int64(r.count) {
		delta = int64(r.count)
	}
	n := int(delta)
	out := make([]Entry, 0, n)
	startOffset := r.count - n
	for i := 0; i < n; i++ {
		idx := (r.start + startOffset + i) % r.size
		out = append(out, r.buf[idx])
	}
	return out, newCursor
}
