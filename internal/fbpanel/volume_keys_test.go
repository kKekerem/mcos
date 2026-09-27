package fbpanel

import (
	"testing"

	"mcos/internal/sound"
)

// ════════════════════════════════════════════════════════════════════════════
// SES TUŞLARI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "ses arttırma f3 kısma f4 olsun".
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Ses tuşları Key()'in ortasındaki switch'teydi; kurulum sihirbazı ve her açık
// pencere tuşu ondan ÖNCE yutuyordu. İlk açılışta — sesin en çok ayarlanmak
// istendiği anda — hiçbiri çalışmıyordu. Bu testler o durumları tek tek sınıyor.

// sesSabitle, testin sonunda seviyeyi eski hâline getirir.
func sesSabitle(t *testing.T, v int) {
	t.Helper()
	eski := sound.Volume()
	sound.SetVolume(v)
	t.Cleanup(func() { sound.SetVolume(eski) })
}

func TestF3SesiAcarF4Kisar(t *testing.T) {
	a, _ := newTestApp(t)
	sesSabitle(t, 50)

	a.Key("f3")
	if got := sound.Volume(); got <= 50 {
		t.Fatalf("F3 sesi açmadı: %d", got)
	}
	yuksek := sound.Volume()
	a.Key("f4")
	if got := sound.Volume(); got >= yuksek {
		t.Fatalf("F4 sesi kısmadı: %d -> %d", yuksek, got)
	}
}

// Dizüstülerin medya tuşları (Fn+F3) TTY'ye karakter göndermez; fbinput
// onları evdev'den bu adlarla iletir.
func TestMedyaTuslariCalisir(t *testing.T) {
	a, _ := newTestApp(t)
	sesSabitle(t, 50)

	a.Key("volumeup")
	if sound.Volume() <= 50 {
		t.Fatal("volumeup sesi açmadı")
	}
	a.Key("volumedown")
	a.Key("volumedown")
	if sound.Volume() >= 50 {
		t.Fatal("volumedown sesi kısmadı")
	}
}

func TestSesTuslariKurulumSihirbazindaCalisir(t *testing.T) {
	a, _ := newTestApp(t)
	sesSabitle(t, 50)
	a.StartSetup()
	if a.setupState() == nil {
		t.Fatal("kurulum sihirbazı başlamadı; test bir şey kanıtlamıyor")
	}

	a.Key("f3")
	if sound.Volume() <= 50 {
		t.Fatal("kurulum sihirbazında F3 çalışmıyor")
	}
}

func TestSesTuslariAcikPenceredeCalisir(t *testing.T) {
	a, _ := newTestApp(t)
	sesSabitle(t, 50)
	a.OpenModal(NewConfirmModal("Emin misiniz?", []string{"deneme"}, "Tamam", false, nil))
	if a.ActiveModal() == nil {
		t.Fatal("pencere açılmadı; test bir şey kanıtlamıyor")
	}

	a.Key("f4")
	if sound.Volume() >= 50 {
		t.Fatal("açık pencerede F4 çalışmıyor")
	}
	// Pencere ses tuşuyla KAPANMAMALI.
	if a.ActiveModal() == nil {
		t.Fatal("ses tuşu açık pencereyi kapattı")
	}
}
