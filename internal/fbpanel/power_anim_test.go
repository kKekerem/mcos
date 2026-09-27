package fbpanel

import (
	"errors"
	"testing"
	"time"

	"mcos/internal/model"
)

// Kapanış animasyonunun testleri.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Bu animasyon HAYATINDA BİR KEZ, makine kapanırken oynuyor. Bozulduğunda
// kimse hata raporu yazamaz: ekran ya donar ya da kullanıcı kapanmanın
// takıldığını sanıp güç düğmesini basılı tutar. Bir önceki oturumda AÇILIŞ
// geçişinin hiç oynamadığı da tam olarak böyle fark edilmemişti — tek kare
// bile çizilmiyordu ve kimse anlamamıştı.
//
// Bu yüzden testler KARE SAYAR: fonksiyonun var olması değil, gerçekten
// oynadığı ve siyahla bittiği doğrulanıyor.

// allBlack reports whether every pixel is opaque black.
func allBlack(a *App) bool {
	pix := a.ui.Canvas().Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i] != 0 || pix[i+1] != 0 || pix[i+2] != 0 {
			return false
		}
	}
	return true
}

// nonBlackPixels counts pixels that are not pure black.
func nonBlackPixels(a *App) int {
	pix := a.ui.Canvas().Pix
	n := 0
	for i := 0; i < len(pix); i += 4 {
		if pix[i] != 0 || pix[i+1] != 0 || pix[i+2] != 0 {
			n++
		}
	}
	return n
}

// Kapanış ekranı her iki eylem için de çizilmeli ve BOŞ olmamalı.
func TestPowerScreenDrawsBothActions(t *testing.T) {
	a, _ := newTestApp(t)

	for _, act := range []Action{ActPoweroff, ActReboot} {
		a.drawPowerScreen(act, 3)
		if n := nonBlackPixels(a); n < 1000 {
			t.Errorf("%v kapanış ekranı neredeyse boş: %d piksel", act, n)
		}
	}

	title, note := PowerLabel(ActReboot)
	if title != "Yeniden başlatılıyor" || note == "" {
		t.Errorf("yeniden başlatma başlığı yanlış: %q / %q", title, note)
	}
	title, _ = PowerLabel(ActPoweroff)
	if title != "Kapatılıyor" {
		t.Errorf("kapatma başlığı yanlış: %q", title)
	}
}

// Animasyon KAPALIYKEN bile ekran sonunda siyah olmalı ve kapanış ekranı bir
// kez gösterilmeli: animasyonu kapatmak "hiçbir şey gösterme" demek değil.
func TestPowerOutroWithAnimationsOffEndsBlack(t *testing.T) {
	a, _ := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = false
	a.SetConfig(cfg)

	flips := 0
	a.PlayPowerOutro(func() error { flips++; return nil }, ActPoweroff)

	if flips < 2 {
		t.Errorf("animasyon kapalıyken de en az iki kare basılmalı (ekran + siyah), %d", flips)
	}
	if !allBlack(a) {
		t.Error("son kare siyah olmalı")
	}
}

// Animasyon AÇIKKEN gerçekten oynamalı: tek kareye atlamamalı ve ara
// karelerde ne panelin kendisi ne de düz siyah olmalı.
func TestPowerOutroAnimatesAndEndsBlack(t *testing.T) {
	a, _ := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = true
	a.SetConfig(cfg)

	// Başlangıç: panelin normal karesi.
	a.Draw()

	flips := 0
	sawPartial := false
	levels := map[int]bool{}
	a.PlayPowerOutro(func() error {
		flips++
		// Her karenin "ne kadarı siyah" değeri toplanıyor. Gerçekten oynayan
		// bir geçişte bu sayı kare kare değişir; tek karede atlayan bir
		// geçişte iki değerden fazlası görülmez.
		n := nonBlackPixels(a)
		total := len(a.ui.Canvas().Pix) / 4
		levels[n*100/total] = true
		if n > total/100 && n < total*95/100 {
			sawPartial = true
		}
		return nil
	}, ActReboot)

	// 1,64 saniyelik animasyon 16 ms'lik karelerle ~100 kare eder. Eşiği
	// düşük tutuyoruz: yavaş bir makinede kare atlanabilir, ama
	// "hiç oynamadı" (tek kare) durumu yakalanmalı.
	if flips < 12 {
		t.Errorf("animasyon oynamamış görünüyor: yalnızca %d kare", flips)
	}
	if len(levels) < 2 {
		t.Errorf("kareler hiç değişmemiş (%d farklı doluluk) — geçiş oynamıyor",
			len(levels))
	}
	// Ara kare denetimi ZAMANA duyarlı; yarış dedektörü altında kare sayısı
	// beşte bire düşüyor ve eşiklerin arasına denk gelen kare kalmayabiliyor
	// (bkz. race_on_test.go).
	if !raceEnabled && !sawPartial {
		t.Error("hiçbir ara kare yakalanmadı — geçiş tek karede atlamış olabilir")
	}
	if !allBlack(a) {
		t.Error("son kare siyah olmalı: kullanıcı kapanırken panel artığı görmemeli")
	}
}

// Çizim hatası kapanmayı ENGELLEMEMELİ: flip hata dönerse animasyon kesilir
// ama fonksiyon döner (çağıran hemen ardından poweroff çalıştırıyor).
func TestPowerOutroStopsOnFlipError(t *testing.T) {
	a, _ := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = true
	a.SetConfig(cfg)

	done := make(chan struct{})
	go func() {
		a.PlayPowerOutro(func() error { return errFlip }, ActPoweroff)
		close(done)
	}()

	select {
	case <-done:
	case <-timeAfter(3):
		t.Fatal("flip hatasında animasyon takıldı — makine kapanamazdı")
	}
}

// ── Test yardımcıları ───────────────────────────────────────────────────────

// errFlip, çizim hatasını taklit eder.
var errFlip = errors.New("ekran yazılamadı")

// timeAfter is a tiny wrapper so the test reads without importing time twice.
func timeAfter(sec int) <-chan time.Time {
	return time.After(time.Duration(sec) * time.Second)
}
