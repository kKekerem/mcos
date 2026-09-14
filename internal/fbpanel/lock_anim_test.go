package fbpanel

import (
	"testing"
	"time"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// KİLİT EKRANI PAROLA ALANI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "yazma animasyonu şifre girerken falan, silerken".
//
// Kilit ekranı TextModal KULLANMAZ (tam ekrandır, pencere değil). Bu yüzden
// metin alanına eklenen hareket buraya kendiliğinden gelmez ve ayrıca
// sınanması gerekir — iki parola alanının farklı davranması, aynı sistemde
// iki ayrı arayüz gibi hissettirir.

// lockTestApp builds a locked panel with a password set.
func lockTestApp(t *testing.T) *App {
	t.Helper()
	a, _ := newTestApp(t)

	_, _, cfg := a.Snapshot()
	if cfg == nil {
		cfg = model.DefaultConfig()
	}
	// Gerçek bir parola kur: Lock() parolasızken hiçbir şey yapmaz.
	if err := cfg.Security.SetPassword("gizli"); err != nil {
		t.Fatalf("parola kurulamadı: %v", err)
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()

	a.Lock()
	if !a.Locked() {
		t.Fatal("panel kilitlenmedi")
	}
	return a
}

func TestLockTypingAnimates(t *testing.T) {
	a := lockTestApp(t)

	if a.needsFastRedraw() {
		t.Fatal("boştaki kilit ekranı 60 kare/sn istiyor: boşuna CPU")
	}
	a.lockKey("g")

	a.mu.Lock()
	typed := a.lockAn.typedAt
	input := a.lockInput
	a.mu.Unlock()

	if input != "g" {
		t.Fatalf("karakter alana gitmedi: %q", input)
	}
	if typed.IsZero() {
		t.Fatal("yazma animasyonu başlamadı")
	}
	if !a.needsFastRedraw() {
		t.Fatal("yazma animasyonu hızlı yeniden çizim istemiyor: " +
			"150 ms'lik hareket iki kareye düşer ve kekemeleşir")
	}
}

func TestLockBackspaceLeavesGhost(t *testing.T) {
	a := lockTestApp(t)
	a.lockKey("a")
	a.lockKey("b")
	a.lockKey("backspace")

	a.mu.Lock()
	an := a.lockAn
	input := a.lockInput
	a.mu.Unlock()

	if input != "a" {
		t.Fatalf("silme metni bozdu: %q", input)
	}
	if an.delAt.IsZero() {
		t.Fatal("silme animasyonu başlamadı")
	}
	if an.delIdx != 1 {
		t.Fatalf("hayaletin yeri yanlış: delIdx=%d, 1 bekleniyordu", an.delIdx)
	}
	if !an.typedAt.IsZero() {
		t.Fatal("yazma animasyonu iptal edilmedi: hayalet ile yeni işaret " +
			"aynı yerde üst üste çizilir")
	}
	if !an.removing() {
		t.Fatal("hayalet çizilmiyor sayılıyor")
	}
}

func TestLockWrongPasswordShakes(t *testing.T) {
	a := lockTestApp(t)
	for _, k := range []string{"y", "a", "n", "l", "i", "s"} {
		a.lockKey(k)
	}
	a.lockKey("enter")

	a.mu.Lock()
	an := a.lockAn
	errText := a.lockErr
	input := a.lockInput
	a.mu.Unlock()

	if a.Locked() == false {
		t.Fatal("yanlış parola kilidi açtı")
	}
	if an.shakeAt.IsZero() {
		t.Fatal("yanlış parolada sarsılma yok")
	}
	if errText == "" {
		t.Fatal("hata metni gösterilmiyor")
	}
	if input != "" {
		t.Fatalf("yanlış denemeden sonra alan temizlenmedi: %q", input)
	}
}

func TestLockClearStartsWave(t *testing.T) {
	a := lockTestApp(t)
	for _, k := range []string{"a", "b", "c", "d"} {
		a.lockKey(k)
	}
	a.lockKey("ctrl+u")

	a.mu.Lock()
	an := a.lockAn
	input := a.lockInput
	a.mu.Unlock()

	if input != "" {
		t.Fatalf("alan temizlenmedi: %q", input)
	}
	if an.clearN != 4 {
		t.Fatalf("temizlenen işaret sayısı %d, 4 bekleniyordu", an.clearN)
	}
	if !an.removing() {
		t.Fatal("temizleme dalgası çizilmiyor sayılıyor")
	}
}

// Sınıra dayanan karakter SESSİZCE yutulmamalı. Maskeli bir alanda kullanıcı
// yazdığını okuyup sayamaz; sessiz yutma ona "klavyem çalışmıyor" dedirtir.
func TestLockFullFieldShakes(t *testing.T) {
	a := lockTestApp(t)
	a.mu.Lock()
	a.lockInput = string(make([]rune, 64))
	a.lockAn.reset()
	a.mu.Unlock()

	a.lockKey("x")

	a.mu.Lock()
	an := a.lockAn
	n := len([]rune(a.lockInput))
	a.mu.Unlock()

	if n != 64 {
		t.Fatalf("sınır aşıldı: %d karakter", n)
	}
	if an.shakeAt.IsZero() {
		t.Fatal("sınırda sarsılma yok — karakter sessizce yutuldu")
	}
}

// Doğru parola kilidi açmalı ve animasyon durumu geride kalmamalı.
func TestLockCorrectPasswordUnlocks(t *testing.T) {
	a := lockTestApp(t)
	for _, k := range []string{"g", "i", "z", "l", "i"} {
		a.lockKey(k)
	}
	a.lockKey("enter")

	if a.Locked() {
		t.Fatal("doğru parola kilidi açmadı")
	}
}

// Kilit ekranı çizimi ÇÖKMEMELİ: noktalar artık metin değil, animasyona bağlı
// yarıçapı olan daireler.
func TestLockScreenDraws(t *testing.T) {
	a, img := newTestApp(t)
	_, _, cfg := a.Snapshot()
	if cfg == nil {
		cfg = model.DefaultConfig()
	}
	if err := cfg.Security.SetPassword("gizli"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	a.Lock()

	for _, k := range []string{"a", "b", "c"} {
		a.lockKey(k)
	}
	for i := range img.Pix {
		img.Pix[i] = 0
	}
	a.Draw()
	if !anyNonZero(img) {
		t.Fatal("kilit ekranı hiçbir şey çizmedi")
	}

	// Silme hayaleti çizilirken de çökmemeli.
	a.lockKey("backspace")
	a.Draw()

	// Temizleme dalgası çizilirken de.
	a.lockKey("ctrl+u")
	a.Draw()
}

// Animasyon bittiğinde hızlı yeniden çizim İSTENMEMELİ: pencere açık kaldığı
// sürece 60 kare/sn çizmek, üstünde Minecraft sunucusu koşan bir makinede
// boşuna CPU demek.
func TestLockStopsAskingForFramesWhenIdle(t *testing.T) {
	a := lockTestApp(t)
	a.lockKey("a")
	if !a.needsFastRedraw() {
		t.Fatal("yazma animasyonu kare istemiyor")
	}

	a.mu.Lock()
	a.lockAn.typedAt = time.Now().Add(-typePopDur - time.Millisecond)
	a.mu.Unlock()

	if a.needsFastRedraw() {
		t.Fatal("animasyon bittiği hâlde hâlâ 60 kare/sn isteniyor")
	}
}
