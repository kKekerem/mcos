//go:build !windows

package supervisor

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Minecraft'ı durdurmanın DOĞRU yolu stdin'e "stop" yazmaktır: dünya
// kaydedilir, oyuncular düzgün atılır. Stop() bunun için GracefulStop'u
// çağırır — ama çağırmadan ÖNCE durumu "stopping" yapıyordu ve WriteStdin
// yalnızca "running" durumunda yazıyordu. "stop" HİÇ gitmiyordu: her durdurma
// StopTimeout (sunucularda 60 sn) bekleyip SIGTERM'e düşüyordu. Uçtan uca
// sınamada ölçüldü: mcosd kapanışı 61 sn sürdü, sunucu "code=143" (SIGTERM)
// ile çıktı.
func TestGracefulStopReachesStdin(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	p := NewProcess(Spec{
		Path: "/bin/sh",
		// "stop" satırını okuyunca temiz çık (kod 0); okuyamazsa asılı kal.
		Args:        []string{"-c", `read l; echo "aldim:$l"; exit 0`},
		StopTimeout: 5 * time.Second,
		GracefulStop: func(p *Process) error {
			return p.WriteStdin("stop")
		},
		OnLine: func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() },
	})
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	took := time.Since(t0)
	if took > 2*time.Second {
		t.Fatalf("durdurma %v sürdü — \"stop\" stdin'e ulaşmadı, zaman aşımına düştü", took)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	got := strings.Join(lines, "|")
	mu.Unlock()
	if !strings.Contains(got, "aldim:stop") {
		t.Errorf("süreç \"stop\" görmedi; çıktı: %q", got)
	}
}

// KARŞI-SINAMA: durdurma İSTENMEMİŞ bir süreçte "stopping" dışı hiçbir
// durum stdin'i açmamalı — çıkmış bir sürece yazmak hata vermeli.
func TestWriteStdinRefusedAfterExit(t *testing.T) {
	p := NewProcess(Spec{Path: "/bin/sh", Args: []string{"-c", "exit 0"}})
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for p.State() == StateRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := p.WriteStdin("stop"); err == nil {
		t.Error("çıkmış sürece yazma hata vermedi")
	}
}
