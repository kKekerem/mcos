package server

import (
	"strings"
	"testing"
)

// Kullanıcının durumu: 2048 MB istenmiş, VirtualBox'ta MCOS açıldıktan sonra
// ~1700 MB boş. Eskiden -Xmx2048M + AlwaysPreTouch ile açılıp OOM ile
// ölüyordu ("signal: killed").
func TestYiginBosBellegeSigdirilir(t *testing.T) {
	p := planMemory(2048, 1700)
	if !p.Reduced || p.HeapMB >= 2048 {
		t.Fatalf("1700 MB boşta 2048 MB yığın küçültülmeliydi: %+v", p)
	}
	if memNeedMB(p.HeapMB) > 1700-memReserveMB {
		t.Fatalf("küçültülmüş yığın bile sığmıyor: yığın %d, gereken %d, kullanılabilir %d",
			p.HeapMB, memNeedMB(p.HeapMB), 1700-memReserveMB)
	}
	if p.PreTouch {
		t.Fatal("bellek darken ön dokunma kapatılmalı")
	}
	if p.HeapMB%128 != 0 {
		t.Fatalf("yığın 128 MB katı olmalı: %d", p.HeapMB)
	}
}

func TestBolBellekteAyarDegismez(t *testing.T) {
	p := planMemory(2048, 12000)
	if p.Reduced || p.HeapMB != 2048 || !p.PreTouch {
		t.Fatalf("bol bellekte kullanıcının ayarı aynen kalmalı: %+v", p)
	}
	// Sığıyor ama ön dokunma için pay yok: yığın aynı, ön dokunma kapalı.
	p = planMemory(2048, memReserveMB+memNeedMB(2048)+100)
	if p.Reduced || p.HeapMB != 2048 || p.PreTouch {
		t.Fatalf("sığan ama dar bellekte yığın aynı, ön dokunma kapalı olmalı: %+v", p)
	}
}

func TestOlculemeyenBellekteDokunulmaz(t *testing.T) {
	if p := planMemory(4096, 0); p.Reduced || p.HeapMB != 4096 {
		t.Fatalf("MemAvailable okunamadıysa tahminle kesilmemeli: %+v", p)
	}
}

func TestAltSinirVeIstenenUstSinir(t *testing.T) {
	if p := planMemory(4096, 600); p.HeapMB != memMinHeapMB {
		t.Fatalf("çok dar bellekte alt sınır %d olmalı: %+v", memMinHeapMB, p)
	}
	if p := planMemory(256, 600); p.HeapMB != 256 || p.Reduced {
		t.Fatalf("istenenden BÜYÜK yığın verilmemeli: %+v", p)
	}
}

func TestOnDokunmaBayragiAyiklanir(t *testing.T) {
	a := dropPreTouch([]string{"-Xms1G", "-XX:+AlwaysPreTouch", "-Xmx1G"})
	if strings.Contains(strings.Join(a, " "), "AlwaysPreTouch") || len(a) != 2 {
		t.Fatalf("AlwaysPreTouch ayıklanmadı: %v", a)
	}
}
