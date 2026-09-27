package supervisor

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// BeforeStart HER başlatmada çalışmalı ve argümanları o anki koşullara göre
// değiştirebilmeli (OOM sonrası yeniden başlatma küçük yığınla açılsın).
func TestBeforeStartArgumanlariHerBaslatmadaGunceller(t *testing.T) {
	var mu sync.Mutex
	var satirlar []string
	n := 0
	spec := Spec{
		Path:   "/bin/sh",
		Args:   []string{"-c", "echo ESKI"},
		OnLine: func(l string) { mu.Lock(); satirlar = append(satirlar, l); mu.Unlock() },
		BeforeStart: func(sp *Spec) []string {
			n++
			sp.Args = []string{"-c", "echo YENI" + string(rune('0'+n))}
			return []string{"UYARI" + string(rune('0'+n))}
		},
	}
	p := NewProcess(spec)
	for i := 0; i < 2; i++ {
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			t.Fatal("süreç bitmedi")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	hepsi := strings.Join(satirlar, "|")
	for _, beklenen := range []string{"UYARI1", "YENI1", "UYARI2", "YENI2"} {
		if !strings.Contains(hepsi, beklenen) {
			t.Fatalf("%q yok; satırlar: %s", beklenen, hepsi)
		}
	}
	if strings.Contains(hepsi, "ESKI") {
		t.Fatalf("eski argümanlar kullanıldı: %s", hepsi)
	}
}

// Başlatma hatası kilitlenmeye yol açmamalı: eskiden hata satırı kilit
// altındaki satır yazıcısından geçiyor, geri çağrısı aynı kilidi istiyordu.
func TestBaslatmaHatasiKilitlenmez(t *testing.T) {
	var mu sync.Mutex
	var satir string
	p := NewProcess(Spec{
		Path:   "/olmayan/java",
		OnLine: func(l string) { mu.Lock(); satir = l; mu.Unlock() },
	})
	bitti := make(chan error, 1)
	go func() { bitti <- p.Start() }()
	select {
	case err := <-bitti:
		if err == nil {
			t.Fatal("olmayan ikili için hata beklenirdi")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start kilitlendi (hata satırı kilit altında yazılıyor)")
	}
	_ = p.State() // kilit serbest mi
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(satir, "başlatılamadı") {
		t.Fatalf("hata satırı kullanıcıya iletilmedi: %q", satir)
	}
}
