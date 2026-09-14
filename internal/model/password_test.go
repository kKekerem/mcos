package model

import (
	"strings"
	"testing"
)

// İSTEĞE BAĞLI panel parolasının testleri.
//
// Buradaki bir hata iki yönde de felakettir: parola doğrulanmazsa koruma
// yoktur; parola yanlış doğrulanırsa kullanıcı KENDİ panelinden kilitlenir
// ve tek çıkış yolu yeniden kurulumdur.

func TestNoPasswordMeansAlwaysAllowed(t *testing.T) {
	var s SecurityConfig
	if s.PasswordSet() {
		t.Error("boş yapılandırma parola kurulu diyor")
	}
	// Parolasız bir sistemde her giriş geçerli olmalı: çağıranın önce
	// PasswordSet() kontrol etmesi gerekmemeli.
	for _, attempt := range []string{"", "ne olursa"} {
		if !s.VerifyPassword(attempt) {
			t.Errorf("parolasız sistemde %q reddedildi", attempt)
		}
	}
}

func TestSetAndVerifyPassword(t *testing.T) {
	var s SecurityConfig
	if err := s.SetPassword("kale1453"); err != nil {
		t.Fatalf("parola kurulamadı: %v", err)
	}
	if !s.PasswordSet() {
		t.Fatal("parola kuruldu ama PasswordSet false")
	}
	// Düz metin HİÇBİR YERDE durmamalı.
	if strings.Contains(s.PasswordHash, "kale1453") ||
		strings.Contains(s.PasswordSalt, "kale1453") {
		t.Fatal("parola düz metin olarak saklanmış")
	}
	if s.Iterations <= 0 {
		t.Error("tur sayısı kaydedilmemiş — ileride doğrulama kırılır")
	}

	if !s.VerifyPassword("kale1453") {
		t.Error("doğru parola reddedildi")
	}
	for _, wrong := range []string{"kale1454", "KALE1453", "", "kale145"} {
		if s.VerifyPassword(wrong) {
			t.Errorf("yanlış parola kabul edildi: %q", wrong)
		}
	}
}

// Aynı parola İKİ AYRI kurulumda farklı özet üretmeli (tuz çalışıyor mu).
func TestSaltMakesHashesUnique(t *testing.T) {
	var a, b SecurityConfig
	if err := a.SetPassword("aynisi"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetPassword("aynisi"); err != nil {
		t.Fatal(err)
	}
	if a.PasswordSalt == b.PasswordSalt {
		t.Error("iki kurulum aynı tuzu üretti")
	}
	if a.PasswordHash == b.PasswordHash {
		t.Error("aynı parola aynı özeti verdi — tuz uygulanmıyor")
	}
	// Yine de ikisi de kendi parolasını doğrulamalı.
	if !a.VerifyPassword("aynisi") || !b.VerifyPassword("aynisi") {
		t.Error("tuzlu özetler doğrulanamadı")
	}
}

func TestEmptyPasswordClearsIt(t *testing.T) {
	var s SecurityConfig
	if err := s.SetPassword("baslangic"); err != nil {
		t.Fatal(err)
	}
	s.LockOnSleep = true

	// GERÇEKTEN boş metin parolayı kaldırır. Arayüz bunu açık bir eylemle
	// yapar: "Parolayı kaldır" satırı setPanelPassword("") çağırır.
	if err := s.SetPassword(""); err != nil {
		t.Fatalf("parola kaldırılamadı: %v", err)
	}
	if s.PasswordSet() {
		t.Error("boş parola, parolayı kaldırmadı")
	}
	if s.LockOnSleep {
		t.Error("parola kalkınca uyku kilidi de kalkmalıydı")
	}
	if !s.VerifyPassword("herhangi") {
		t.Error("parola kaldırıldıktan sonra giriş engellendi")
	}
}

// TestWhitespacePasswordIsRejectedNotSilentlyCleared — ASIL HATA.
//
// ── Neden bu test var ───────────────────────────────────────────────────────
// Bu testin eski hâli, hatanın KENDİSİNİ doğru davranış sanıyordu: SetPassword
// ("   ") çağırıp parolanın kalkmasını bekliyordu.
//
// Gerçekte olan şuydu: silme kararı TrimSpace ile, uzunluk denetimi ise
// kırpılmamış metinle yapılıyordu. Kullanıcı parola olarak dört boşluk
// yazdığında arayüzün doğrulayıcısı "4 karakter, yeterli" diyor, model ise
// "boş, demek ki kaldırmak istiyor" diyordu. Panel ekrana "Panel parolası
// kaydedildi" yazıyor, ama panel PAROLASIZ kalıyordu.
//
// Yani kullanıcı, korumalı sandığı bir sistemi korumasız bırakıyordu ve bunu
// söyleyen hiçbir şey yoktu.
func TestWhitespacePasswordIsRejectedNotSilentlyCleared(t *testing.T) {
	for _, pw := range []string{" ", "   ", "    ", "\t\t\t\t", "  \n  "} {
		var s SecurityConfig
		if err := s.SetPassword("baslangic"); err != nil {
			t.Fatal(err)
		}

		err := s.SetPassword(pw)
		if err == nil {
			t.Errorf("%q parola olarak KABUL EDİLDİ", pw)
		}
		// Ve en önemlisi: eski parola DURMALI.
		if !s.PasswordSet() {
			t.Errorf("%q girişi parolayı sessizce sildi", pw)
		}
		if !s.VerifyPassword("baslangic") {
			t.Errorf("%q girişinden sonra eski parola çalışmıyor", pw)
		}
	}
}

// Baştaki/sondaki boşlukları olan geçerli bir parola AYNEN saklanmalı:
// kaydederken kırpıp doğrularken kırpmamak (ya da tersi) kullanıcıyı kendi
// parolasından kilitler.
func TestPasswordWithSurroundingSpacesRoundTrips(t *testing.T) {
	var s SecurityConfig
	const pw = "  gizli parola  "
	if err := s.SetPassword(pw); err != nil {
		t.Fatalf("geçerli parola reddedildi: %v", err)
	}
	if !s.VerifyPassword(pw) {
		t.Error("kaydedilen parola doğrulanamadı")
	}
	if s.VerifyPassword("gizli parola") {
		t.Error("kırpılmış hâli de kabul edildi — kaydetme ve doğrulama tutarsız")
	}
}

func TestShortPasswordRejected(t *testing.T) {
	var s SecurityConfig
	if err := s.SetPassword("ab"); err == nil {
		t.Fatal("çok kısa parola kabul edildi")
	}
	if s.PasswordSet() {
		t.Error("reddedilen parola yine de kaydedilmiş")
	}
}

// Bozuk bir yapılandırma dosyası (elle düzenlenmiş, yarım yazılmış)
// kullanıcıyı içeri ALMAMALI ve panik de etmemeli.
func TestCorruptHashDeniesAccess(t *testing.T) {
	cases := []SecurityConfig{
		{PasswordHash: "zzzz", PasswordSalt: "abcd", Iterations: 1000},
		{PasswordHash: "abcd", PasswordSalt: "zzzz", Iterations: 1000},
		{PasswordHash: "abcd", PasswordSalt: "", Iterations: 1000},
	}
	for i, s := range cases {
		if s.PasswordSet() && s.VerifyPassword("herhangi") {
			t.Errorf("%d: bozuk özet girişe izin verdi", i)
		}
	}
}

// Tur sayısı kaydedilmemiş ESKİ bir yapılandırma yine doğrulanabilmeli:
// aksi halde bir yükseltme kullanıcıyı kilitler.
func TestLegacyHashWithoutIterations(t *testing.T) {
	var s SecurityConfig
	if err := s.SetPassword("eskisurum"); err != nil {
		t.Fatal(err)
	}
	s.Iterations = 0 // eski dosya biçimi

	if !s.VerifyPassword("eskisurum") {
		t.Error("tur sayısı olmayan eski özet doğrulanamadı")
	}
}
