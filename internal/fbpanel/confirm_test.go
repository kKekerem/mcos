package fbpanel

import "testing"

// ════════════════════════════════════════════════════════════════════════════
// ONAY PENCERESİ: DÜĞME YAPTIĞINI SÖYLEMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// "Kapat  Enter" yazan düğme ekrandayken QEMU'da Enter'a basıldı ve sistem
// KAPANMADI: odak, yıkıcı sorularda bilerek "Vazgeç"te başlıyor ve Enter
// odaktaki düğmeyi çalıştırıyor. Yani düğme yapmadığı şeyi vaat ediyordu.
//
// Odağın Vazgeç'te başlaması DOĞRU davranış ve korunuyor; yalanlayan şey
// ipucuydu.

func TestConfirmStartsOnCancel(t *testing.T) {
	m := NewConfirmModal("Kapat?", []string{"uyarı"}, "Kapat", true, nil)
	if m.focused != 1 {
		t.Fatalf("yıkıcı onayda odak %d; Vazgeç (1) olmalı — Enter'a "+
			"refleksle basan kullanıcı makineyi kapatmamalı", m.focused)
	}
}

func TestConfirmEnterHintFollowsFocus(t *testing.T) {
	yes, no := confirmKeyHints(1) // Vazgeç odakta (açılış hâli)
	if yes == "Enter" {
		t.Error("odak Vazgeç'teyken onay düğmesinde 'Enter' yazıyor — " +
			"düğme yapmadığı şeyi vaat ediyor")
	}
	if no != "Enter" {
		t.Errorf("odaktaki düğmenin ipucu %q; 'Enter' olmalı", no)
	}

	yes, no = confirmKeyHints(0) // onay düğmesi odakta
	if yes != "Enter" {
		t.Errorf("odaktaki onay düğmesinin ipucu %q; 'Enter' olmalı", yes)
	}
	if no != "Esc" {
		t.Errorf("Vazgeç ipucu %q; 'Esc' olmalı", no)
	}
}

// Enter, ODAKTAKİ düğmeyi çalıştırır: açılışta bu Vazgeç'tir.
func TestConfirmEnterOnCancelDoesNotFire(t *testing.T) {
	a, _ := newTestApp(t)
	fired := false
	m := NewConfirmModal("Kapat?", []string{"uyarı"}, "Kapat", true,
		func(*App) { fired = true })

	if !m.Key(a, "enter") {
		t.Fatal("Enter pencereyi kapatmadı")
	}
	if fired {
		t.Fatal("odak Vazgeç'teyken Enter yıkıcı eylemi çalıştırdı")
	}
}

// Tab ile onay düğmesine geçilir; ORADA Enter çalışır.
func TestConfirmTabThenEnterFires(t *testing.T) {
	a, _ := newTestApp(t)
	fired := false
	m := NewConfirmModal("Kapat?", []string{"uyarı"}, "Kapat", true,
		func(*App) { fired = true })

	m.Key(a, "tab")
	if m.focused != 0 {
		t.Fatalf("Tab sonrası odak %d; onay düğmesi (0) olmalı", m.focused)
	}
	if yes, _ := confirmKeyHints(m.focused); yes != "Enter" {
		t.Errorf("onay düğmesi odaktayken ipucu %q; 'Enter' olmalı", yes)
	}
	if !m.Key(a, "enter") {
		t.Fatal("Enter pencereyi kapatmadı")
	}
	if !fired {
		t.Fatal("onay düğmesi odaktayken Enter eylemi çalıştırmadı")
	}
}

// Fare doğrudan düğmeye tıklarsa odak nerede olursa olsun çalışır.
func TestConfirmMouseClickFires(t *testing.T) {
	a, _ := newTestApp(t)
	fired := false
	m := NewConfirmModal("Kapat?", []string{"uyarı"}, "Kapat", true,
		func(*App) { fired = true })

	if !m.Key(a, "confirm") {
		t.Fatal("tıklama pencereyi kapatmadı")
	}
	if !fired {
		t.Fatal("onay düğmesine tıklandığı hâlde eylem çalışmadı")
	}
}
