package fbpanel

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcos/internal/fbui"
)

// Kullanıcının şikâyeti: "Java kur'a bastım, başka şeyler yapınca işlem
// gidiyor". İş sürerken başka bir mesaj ve pencere açılması (clearBusyLocked)
// iş göstergesini SİLMEMELİ; mesaj kısa süre öne çıkıp sonra iş geri gelmeli.
func TestSurenIsBaskaMesajlarlaKaybolmaz(t *testing.T) {
	a, _ := newTestApp(t)
	simdi := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)
	eski := nowFunc
	nowFunc = func() time.Time { return simdi }
	defer func() { nowFunc = eski }()

	bitir := make(chan struct{})
	a.runJob("java-17", "Java 17 indirilip kuruluyor", func() (string, error) {
		<-bitir
		return "Java 17 kuruldu", nil
	}, nil)
	defer close(bitir)

	simdi = simdi.Add(42 * time.Second)
	if e := a.statusEvent(); e == nil || e.Kind != fbui.EventBusy || !strings.Contains(e.Text, "Java 17") || !strings.Contains(e.Text, "0:42") {
		t.Fatalf("durum çubuğu süren işi göstermiyor: %+v", e)
	}

	// Başka bir eylem mesaj yazdı ve bir pencere açıldı (Busy olayı silindi).
	a.Emit(fbui.EventOK, "Çözünürlük kaydedildi")
	a.mu.Lock()
	a.events = append(a.events, fbui.Event{Kind: fbui.EventBusy, Text: "durum alınıyor…", At: simdi})
	a.clearBusyLocked()
	a.mu.Unlock()
	if e := a.statusEvent(); e == nil || e.Text != "Çözünürlük kaydedildi" {
		t.Fatalf("yeni mesaj kısa süre öne çıkmalı: %+v", e)
	}
	simdi = simdi.Add(jobMsgHold + time.Second)
	if e := a.statusEvent(); e == nil || !strings.Contains(e.Text, "Java 17") {
		t.Fatalf("mesajdan sonra süren iş YENİDEN görünmeli: %+v", e)
	}
	if !a.runningJobs()["java-17"] {
		t.Fatal("iş kayıttan düştü")
	}
}

// İkinci basış ikinci kurulum başlatmamalı.
func TestAyniIsIkinciKezBaslamaz(t *testing.T) {
	a, _ := newTestApp(t)
	var n int32
	bitir := make(chan struct{})
	is := func() (string, error) { atomic.AddInt32(&n, 1); <-bitir; return "tamam", nil }
	if !a.runJob("java-17", "Java 17", is, nil) {
		t.Fatal("ilk iş başlamadı")
	}
	if a.runJob("java-17", "Java 17", is, nil) {
		t.Fatal("aynı iş ikinci kez başladı")
	}
	close(bitir)
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("iş %d kez çalıştı", n)
	}
	if e := a.LastEvent(); e == nil || e.Kind != fbui.EventOK || e.Text != "tamam" {
		t.Fatalf("bitişte bildirim düşmedi: %+v", e)
	}
	if len(a.runningJobs()) != 0 {
		t.Fatal("biten iş kayıtta kaldı")
	}
}

func TestIsHatasiNedeniyleBildirilir(t *testing.T) {
	a, _ := newTestApp(t)
	cagrildi := false
	a.runJob("wifi", "Ev ağına bağlanılıyor", func() (string, error) {
		return "Ev bağlantısı başarısız", errors.New("parola yanlış")
	}, func() { cagrildi = true })
	time.Sleep(50 * time.Millisecond)
	e := a.LastEvent()
	if e == nil || e.Kind != fbui.EventError || !strings.Contains(e.Text, "parola yanlış") {
		t.Fatalf("hata nedeniyle bildirilmedi: %+v", e)
	}
	if cagrildi {
		t.Fatal("başarısız işte 'after' çağrılmamalı")
	}
}
