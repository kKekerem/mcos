package netprobe

import (
	"net"
	"strings"
	"testing"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// İNTERNET SINAMASI DOĞRU ŞEYİ ÖLÇMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Eski sınama YALNIZCA TCP PORT 53'e bakıyordu. DNS normalde UDP kullanır;
// TCP/53 pek çok ISS, kurumsal ağ ve VPN tarafından engellenir. Sonuç: HTTPS'in
// sorunsuz çalıştığı ağlarda MCOS "internet yok" diyor ve Java indirmesini
// KAPIDA reddediyordu.
//
// Bu testler, sınamanın gerçekten HTTPS çıkışını ölçtüğünü ve başarısızlıkta
// KULLANILABİLİR bir sebep ürettiğini kilitler.

func TestHedeflerHTTPSIceriyor(t *testing.T) {
	https := 0
	for _, h := range hedefler {
		if h.https {
			https++
			if !strings.HasSuffix(h.adres, ":443") {
				t.Errorf("%q https işaretli ama 443 portunda değil", h.adres)
			}
		}
	}
	if https < 2 {
		t.Errorf("yalnızca %d HTTPS hedefi var — bir sağlayıcı engelliyse "+
			"sınama yanlışlıkla 'internet yok' der", https)
	}
}

// Sınama, TEK BAŞINA TCP/53'e dayanmamalı: eski hatanın ta kendisi buydu.
func TestYalnizca53eDayanmiyor(t *testing.T) {
	sadece53 := true
	for _, h := range hedefler {
		if h.https {
			sadece53 = false
		}
	}
	if sadece53 {
		t.Fatal("sınama yalnızca port 53 kullanıyor — düzeltilen hata geri geldi")
	}
}

// Sahte bir dinleyiciye karşı: açık bir port GÖRÜLMELİ.
func TestAcikPortGoruluyor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("dinleyici açılamadı")
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	c, err := net.DialTimeout("tcp", ln.Addr().String(), probeTimeout)
	if err != nil {
		t.Fatalf("açık porta bağlanılamadı: %v", err)
	}
	c.Close()
}

// Zaman aşımı, mobil/uydu bağlantılar için yeterli olmalı.
//
// 1500 ms sınırdaydı ve yanlış "internet yok" üretiyordu.
func TestZamanAsimiYeterli(t *testing.T) {
	if probeTimeout < 2*time.Second {
		t.Errorf("zaman aşımı %v — yavaş bağlantılarda yanlış negatif üretir",
			probeTimeout)
	}
}

// Önbellek olmalı: durum çubuğu saniyede bir yenileniyor, her seferinde beş
// soket açmak hem israf hem ağda gürültü.
func TestOnbellekVar(t *testing.T) {
	if cacheTTL < 5*time.Second {
		t.Errorf("önbellek süresi %v — her durum yenilemesinde yeniden ölçülür",
			cacheTTL)
	}
}

// Başarısızlıkta sebep BOŞ olmamalı: "İnternet yok" tek başına kullanıcıya
// ne yapacağını söylemiyor.
func TestSebepBosDegil(t *testing.T) {
	for _, dns := range []bool{true, false} {
		if s := sebep(dns); strings.TrimSpace(s) == "" {
			t.Errorf("dns=%v için sebep boş", dns)
		}
	}
}

// DNS açık ama HTTPS kapalıysa sebep bunu AYIRT ETMELİ.
func TestDNSAcikHTTPSKapaliAyirtEdiliyor(t *testing.T) {
	// rotaVar/dnsSunucusuVar bu makinede true dönecek (gerçek dosyalar).
	s := sebep(true)
	if !strings.Contains(strings.ToLower(s), "çıkış") &&
		!strings.Contains(strings.ToLower(s), "https") {
		t.Errorf("sebep %q — DNS açıkken 'dışarı çıkış engelli' denmeli", s)
	}
}

func TestInvalidateOnbellegiDusuruyor(t *testing.T) {
	mu.Lock()
	son = Result{Online: true}
	sonAt = time.Now()
	mu.Unlock()

	Invalidate()

	mu.Lock()
	bos := sonAt.IsZero()
	mu.Unlock()
	if !bos {
		t.Error("Invalidate önbelleği düşürmedi — Wi-Fi'ye bağlandıktan sonra " +
			"kullanıcı 10 saniye 'internet yok' görür")
	}
}
