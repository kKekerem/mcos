package sshd

import (
	"io"
	"strings"
	"sync"
)

// tailBuffer forwards writes and remembers the last few KB.
//
// ── Neden ───────────────────────────────────────────────────────────────────
// SSH sunucusunun çıktısı mcosd günlüğüne gidiyor ve panelden görünmüyor.
// Sunucu öldüğünde kullanıcıya "açık ama çalışmıyor" demek yetmez; sshd'nin
// kendi son sözü ("Bind to port 22 ... Address already in use" gibi) gerçek
// nedendir. Tampon sınırlı: sshd her giriş denemesini de yazar ve sınırsız
// bir tampon uzun süre açık kalan bir sunucuda belleği şişirirdi.
type tailBuffer struct {
	mu  sync.Mutex
	w   io.Writer
	max int
	buf []byte
}

func newTailBuffer(w io.Writer, max int) *tailBuffer {
	return &tailBuffer{w: w, max: max}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	t.mu.Unlock()
	if t.w != nil {
		// Asıl hedefin hatası sunucuyu durdurmamalı: günlük dosyası dolsa bile
		// SSH çalışmaya devam etsin.
		_, _ = t.w.Write(p)
	}
	return len(p), nil
}

// LastLines returns up to n last non-empty lines, oldest first, joined by " / ".
//
// Birden fazla satır: sshd port hatasında önce asıl nedeni ("Bind to port 22
// on 0.0.0.0 failed: Address already in use.") sonra genel sonucu ("Cannot
// bind any address.") yazar; yalnızca sonuncusu nedeni saklardı. Ayraç "; "
// DEĞİL: durum notları birbirinden "; " ile ayrılıyor ve panel notları ondan
// bölüyor; sshd'nin iki satırı iki ayrı not gibi görünürdü (ekran
// görüntüsünde görüldü).
func (t *tailBuffer) LastLines(n int) string {
	t.mu.Lock()
	s := string(t.buf)
	t.mu.Unlock()
	lines := strings.Split(s, "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			out = append([]string{l}, out...)
		}
	}
	return strings.Join(out, " / ")
}
