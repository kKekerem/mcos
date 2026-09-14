package fbpanel

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// AÇILIŞ GEÇİŞİ — "içeri zoomlanarak OOBE'ye geç"
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "açılışta bar dolunca içe zoomlanarak hızlıca ana
// ekrana veya kurulum ekranına (OOBE) geçsin".
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// Geçiş HİÇ GÖRÜNMÜYORDU. QEMU'da 100 ms aralıkla kare alındığında 900 ms'lik
// yakınlaşmanın tek bir ara karesi bile yoktu: ekran açılış ekranından panele
// TEK KAREDE atlıyordu.
//
// Sebep geçişin kendisi değil SAATİYDİ. BeginIntro saati duvar saatinden
// başlatıyordu; ardından Run() içinde mcosd'ye BLOKLAYAN refresh() çağrıları
// geliyordu (yeni açılmış bir daemon'dan sunucu/durum/Java/küme bilgisi;
// ölçüldü, ~1.7 sn). Animasyon, hiçbir kare çizilmeden bitiyordu.
//
// Bu testler saatin ilk karede değil ARM edildiğinde başladığını koruyor.

// fakeCanvas builds an App with a solid-colour canvas (font yüklemeden).
func introTestApp(t *testing.T) *App {
	t.Helper()
	a, _ := newTestApp(t)
	return a
}

func TestIntroDoesNotExpireWhileStartupBlocks(t *testing.T) {
	a := introTestApp(t)

	from := image.NewRGBA(a.ui.Bounds())
	a.BeginIntro(from)

	// Açılışın bloklayan işlerini taklit et: geçiş süresinden UZUN bir
	// gecikme. Eski davranışta geçiş burada ölürdü.
	time.Sleep(20 * time.Millisecond)

	a.mu.Lock()
	tr := a.trans
	a.mu.Unlock()
	if tr == nil {
		t.Fatal("açılış geçişi kurulmadı")
	}
	if !tr.pending {
		t.Fatal("saat BeginIntro'da başlamış: bloklayan açılış işleri " +
			"animasyonu yiyip bitirir")
	}
	if p, done := tr.progress(); done || p != 0 {
		t.Fatalf("beklemedeki geçiş ilerlemiş: p=%v done=%v", p, done)
	}
}

func TestArmIntroStartsTheClock(t *testing.T) {
	a := introTestApp(t)
	a.BeginIntro(image.NewRGBA(a.ui.Bounds()))
	a.armIntro()

	a.mu.Lock()
	tr := a.trans
	a.mu.Unlock()
	if tr.pending {
		t.Fatal("armIntro saati başlatmadı")
	}
	if _, done := tr.progress(); done {
		t.Fatal("geçiş başlar başlamaz bitti; 900 ms sürmeli")
	}
}

// Saat başladıktan sonra süre GERÇEKTEN akmalı ve geçiş bitmeli — aksi hâlde
// panel sonsuza kadar yakınlaşma karesi çizerdi.
func TestIntroFinishesAfterItsDuration(t *testing.T) {
	a := introTestApp(t)
	a.BeginIntro(image.NewRGBA(a.ui.Bounds()))
	a.armIntro()

	a.mu.Lock()
	a.trans.start = time.Now().Add(-introDuration - time.Millisecond)
	tr := a.trans
	a.mu.Unlock()

	if p, done := tr.progress(); !done || p != 1 {
		t.Fatalf("süresi dolmuş geçiş bitmiş sayılmalı: p=%v done=%v", p, done)
	}
}

// armIntro, bekleyen bir açılış geçişi YOKKEN hiçbir şey bozmamalı: normal
// ekran geçişleri kendi saatleriyle çalışır.
func TestArmIntroIgnoresNormalTransitions(t *testing.T) {
	a := introTestApp(t)
	a.beginTransition(transFade)

	a.mu.Lock()
	before := a.trans.start
	a.mu.Unlock()

	time.Sleep(2 * time.Millisecond)
	a.armIntro()

	a.mu.Lock()
	after := a.trans.start
	kind := a.trans.kind
	a.mu.Unlock()

	if kind != transFade {
		t.Fatalf("geçiş türü değişti: %v", kind)
	}
	if !after.Equal(before) {
		t.Fatal("armIntro normal bir geçişin saatini sıfırladı")
	}
}

// Bekleyen açılış geçişi, arka planda istenen kısa bir geçişle EZİLMEMELİ.
//
// Gerçek senaryo: panel daha ilk karesini çizmeden kurulum sihirbazı kurulur
// ya da eş taraması biter; o yollar 180 ms'lik bir soluklaşma ister ve açılış
// yakınlaşmasını sessizce yok ederdi.
func TestPendingIntroSurvivesBackgroundTransition(t *testing.T) {
	a := introTestApp(t)
	a.BeginIntro(image.NewRGBA(a.ui.Bounds()))

	a.beginTransition(transFade)

	a.mu.Lock()
	kind := a.trans.kind
	pending := a.trans.pending
	a.mu.Unlock()

	if kind != transIntro {
		t.Fatalf("açılış geçişi ezildi: %v", kind)
	}
	if !pending {
		t.Fatal("açılış geçişinin saati erken başladı")
	}
}

// Animasyonlar KAPALIYKEN açılış geçişi hiç kurulmamalı: kapalıyken 8 MB'lık
// bir kare kopyası tutmak boşuna bellek demek.
func TestIntroSkippedWhenAnimationsOff(t *testing.T) {
	a := introTestApp(t)
	_, _, cfg := a.Snapshot()
	if cfg == nil {
		t.Skip("demo yapılandırması yok")
	}
	cfg.UI.Animations = false
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()

	a.BeginIntro(image.NewRGBA(a.ui.Bounds()))

	a.mu.Lock()
	tr := a.trans
	a.mu.Unlock()
	if tr != nil {
		t.Fatal("animasyonlar kapalıyken açılış geçişi kurulmamalı")
	}
}

// ════════════════════════════════════════════════════════════════════════════
// METİN ALANI ANİMASYONLARI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "yazma animasyonu şifre girerken falan, silerken".
//
// Buradaki asıl koruma şu: panel boştayken hiçbir kare çizmez (App.Tick).
// Pencerenin kendi animasyonu, yeniden çizim İSTEMEZSE donar — imleç
// "yanıp sönüyor" diye yazılmış olmasına rağmen gerçekte hiç yanıp
// sönmüyordu, çünkü pencere açıkken yeniden çizim isteyen bir koşul yoktu.

func TestTextModalAlwaysWantsSlowRedraw(t *testing.T) {
	m := NewTextModal("Test", "", nil)
	if !m.Animating(false) {
		t.Fatal("metin alanı yavaş tikte yeniden çizim istemiyor: " +
			"imleç hiç yanıp sönmez")
	}
}

func TestTypingRequestsFastRedraw(t *testing.T) {
	a := introTestApp(t)
	m := NewTextModal("Test", "", nil).Masked()

	if m.Animating(true) {
		t.Fatal("boştaki alan 60 kare/sn istiyor: boşuna CPU")
	}
	m.Key(a, "k")
	if !m.Animating(true) {
		t.Fatal("yazma animasyonu hızlı yeniden çizim istemiyor: " +
			"150 ms'lik hareket iki kareye düşer ve kekemeleşir")
	}
}

func TestBackspaceRequestsFastRedraw(t *testing.T) {
	a := introTestApp(t)
	m := NewTextModal("Test", "", nil).Masked().WithValue("abc")

	m.typedAt = time.Time{} // WithValue animasyon başlatmaz; emin olalım
	if m.Animating(true) {
		t.Fatal("boştaki alan hızlı yeniden çizim istemiyor olmalı")
	}
	m.Key(a, "backspace")
	if !m.Animating(true) {
		t.Fatal("silme animasyonu hızlı yeniden çizim istemiyor")
	}
	if m.Value() != "ab" {
		t.Fatalf("silme metni bozdu: %q", m.Value())
	}
}

// Sınıra dayanan bir karakter SESSİZCE yutulmamalı: alan sarsılır.
// Sessiz yutma, kullanıcıya "tuşum çalışmıyor" dedirtir — özellikle maskeli
// bir alanda, çünkü orada yazdığını okuyup sayamaz.
func TestFullFieldShakesInsteadOfSwallowing(t *testing.T) {
	a := introTestApp(t)
	m := NewTextModal("Test", "", nil).WithMaxLen(2).WithValue("ab")

	m.Key(a, "c")
	if m.Value() != "ab" {
		t.Fatalf("sınır aşıldı: %q", m.Value())
	}
	if m.shakeAt.IsZero() {
		t.Fatal("sınıra dayanınca sarsılma yok — karakter sessizce yutuldu")
	}
}

// Doğrulama reddettiğinde pencere AÇIK kalmalı, metin korunmalı ve alan
// sarsılmalı: sarsılma, hata metninden önce algılanır.
func TestRejectedInputShakesAndKeepsText(t *testing.T) {
	a := introTestApp(t)
	m := NewTextModal("Test", "", nil).
		WithValue("kisa").
		WithValidate(func(string) string { return "çok kısa" })

	if done := m.Key(a, "enter"); done {
		t.Fatal("reddedilen giriş pencereyi kapattı: kullanıcı metnini kaybeder")
	}
	if m.Value() != "kisa" {
		t.Fatalf("metin kayboldu: %q", m.Value())
	}
	if m.shakeAt.IsZero() {
		t.Fatal("reddedilen girişte sarsılma yok")
	}
	if m.errText == "" {
		t.Fatal("hata metni gösterilmiyor")
	}
}

// Ctrl+U bütün alanı temizler ve dalga animasyonunu başlatır.
func TestClearStartsWaveAnimation(t *testing.T) {
	a := introTestApp(t)
	m := NewTextModal("Test", "", nil).Masked().WithValue("parola")

	m.Key(a, "ctrl+u")
	if m.Value() != "" {
		t.Fatalf("alan temizlenmedi: %q", m.Value())
	}
	if m.clearN != 6 {
		t.Fatalf("temizlenen işaret sayısı %d, 6 bekleniyordu", m.clearN)
	}
	if !m.Animating(true) {
		t.Fatal("temizleme animasyonu yeniden çizim istemiyor")
	}
}

// Maskeli alan çizimi ÇÖKMEMELİ: noktalar artık metin değil daire olarak
// çiziliyor ve boyutları animasyona bağlı.
func TestMaskedFieldDraws(t *testing.T) {
	a, img := newTestApp(t)
	m := NewTextModal("Parola", "Girin", nil).Masked().WithValue("abc")

	// Tuvali belirgin bir renge boya ki çizim gerçekten bir şey yazsın.
	for i := range img.Pix {
		img.Pix[i] = 0
	}
	a.OpenModal(m)
	m.Key(a, "x") // yazma animasyonunu başlat
	a.Draw()

	if !anyNonZero(img) {
		t.Fatal("maskeli alan hiçbir şey çizmedi")
	}
}

func anyNonZero(img *image.RGBA) bool {
	for i := 0; i < len(img.Pix); i += 4 {
		if (color.RGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}) !=
			(color.RGBA{}) {
			return true
		}
	}
	return false
}
