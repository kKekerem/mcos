package model

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// Bu dosya İSTEĞE BAĞLI panel parolasını üretir ve doğrular.
//
// ── Neden PBKDF2, neden düz SHA-256 değil? ──────────────────────────────────
// Düz bir SHA-256 özeti saniyede milyarlarca kez denenebilir; 6 haneli bir
// parola saniyeler içinde kırılır. PBKDF2 aynı işi KASITLI OLARAK yavaş
// yapar: 200 000 tur, bir denemeyi ~100 ms'ye çıkarır. Kullanıcı bunu fark
// etmez (girişte bir kez), saldırgan 6 haneli parolayı denemek için yıllar
// harcar.
//
// ── Neden tuz? ──────────────────────────────────────────────────────────────
// Tuz olmadan, aynı parolayı kullanan iki MCOS kurulumu aynı özeti üretir ve
// önceden hesaplanmış tablolar işe yarar. Her kurulum için rastgele 16 baytlık
// tuz bunu imkânsız kılar.
//
// ── Neden tur sayısı da saklanıyor? ─────────────────────────────────────────
// İleride donanım hızlandıkça turu artıracağız. Tur sayısı özetle birlikte
// saklanmazsa, eski kurulumlardaki parolalar bir yükseltmeden sonra
// doğrulanamaz hale gelir ve kullanıcı kendi panelinden kilitlenir.

// pbkdf2Iterations is the cost for NEW passwords.
//
// 200 000 tur SHA-256: modern bir masaüstünde ~90 ms, MCOS'un hedeflediği
// zayıf makinelerde ~350 ms. Girişte bir kez ödenen bu bedel kabul edilebilir.
const pbkdf2Iterations = 200_000

// pbkdf2KeyLen is the digest length in bytes.
const pbkdf2KeyLen = 32

// saltLen is the random salt length in bytes.
const saltLen = 16

// MinPasswordLen is the shortest accepted password.
//
// 4 hane: MCOS bir ev sunucusudur ve parola ekranı çoğu zaman klavyeyle zor
// erişilen bir köşede girilir. Daha uzun bir zorunluluk, kullanıcıyı parolayı
// tamamen kapatmaya iter — yani güvenliği AZALTIR.
const MinPasswordLen = 4

// ErrPasswordTooShort is returned by SetPassword for short input.
var ErrPasswordTooShort = errors.New("parola en az 4 karakter olmalı")

// SetPassword stores a new password hash in the config.
//
// Boş parola, parolayı KALDIRIR: kullanıcı "isteğe bağlı" derken bunu da
// kastediyor — açtıktan sonra kapatabilmeli.
func (s *SecurityConfig) SetPassword(pw string) error {
	// -- Yakalanan gercek hata ------------------------------------------
	// Silme karari TrimSpace ile, uzunluk denetimi ise KIRPILMAMIS metinle
	// yapiliyordu. Dort bosluk yazmak ikisini de gecti: dogrulayici 4 karakter
	// gordu, TrimSpace bos gordu. Sonuc, parolayi SESSIZCE SILMEKTI -- ustelik
	// arayuz "Panel parolasi kaydedildi" diyordu. Kullanici korumali
	// sandigi bir panelle kaliyordu.
	//
	// Artik yalnizca GERCEKTEN bos bir metin siler; bosluklardan olusan bir
	// giris "cok kisa" diye reddedilir.
	if pw == "" {
		s.PasswordHash = ""
		s.PasswordSalt = ""
		s.Iterations = 0
		s.LockOnSleep = false
		return nil
	}
	if len([]rune(strings.TrimSpace(pw))) < MinPasswordLen {
		return ErrPasswordTooShort
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		// Rastgelelik alınamıyorsa SABİT bir tuza düşmek felaket olurdu;
		// hata döndürüp parolayı hiç kurmamak doğru davranıştır.
		return err
	}
	sum, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return err
	}
	s.PasswordSalt = hex.EncodeToString(salt)
	s.PasswordHash = hex.EncodeToString(sum)
	s.Iterations = pbkdf2Iterations
	return nil
}

// VerifyPassword reports whether pw matches the stored hash.
//
// Parola kurulu DEĞİLSE her zaman true döner: parolasız bir sistemde her
// giriş geçerlidir. Çağıranın önce PasswordSet() kontrol etmesi gerekmez.
func (s SecurityConfig) VerifyPassword(pw string) bool {
	if !s.PasswordSet() {
		return true
	}
	salt, err := hex.DecodeString(s.PasswordSalt)
	if err != nil || len(salt) == 0 {
		return false
	}
	iter := s.Iterations
	if iter <= 0 {
		// Tur sayısı kaydedilmemiş eski bir yapılandırma: o dönemde
		// kullanılan değerle dene.
		iter = pbkdf2Iterations
	}
	want, err := hex.DecodeString(s.PasswordHash)
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	// Sabit süreli karşılaştırma: bayt bayt erken çıkan bir karşılaştırma,
	// doğru önekin uzunluğunu zamanlamadan sızdırır.
	return subtle.ConstantTimeCompare(got, want) == 1
}
