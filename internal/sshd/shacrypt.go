package sshd

import (
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Bu dosya /etc/shadow için SHA-512 crypt ("$6$") parolası üretir.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN KENDİMİZ HESAPLIYORUZ
// ════════════════════════════════════════════════════════════════════════════
//
// Parola eskiden harici "chpasswd" ile yazılıyordu. İmajdaki BusyBox'ta o
// applet KAPALI (CONFIG_CHPASSWD is not set) ve başka bir chpasswd da yok:
// kullanıcı panelden parola koyduğunda "chpasswd bulunamadı" hatası alıyor ve
// SSH'a parolayla hiç giremiyordu (QEMU'da gerçek imajla ölçüldü).
//
// Bir ikiliye bağımlı olmak, imaj yapılandırmasındaki tek bir satırın özelliği
// sessizce öldürmesi demek. Hesap saf Go'da: hiçbir harici programa, cgo'ya ya
// da libcrypt'e ihtiyaç duymaz.
//
// ── Neden SHA-512 ("$6$") ───────────────────────────────────────────────────
// İmajın libc'si glibc 2.38; OpenSSH sshd parolayı getspnam + libcrypt'in
// crypt() işleviyle doğrular. glibc libcrypt'i $1$ (MD5), $5$ (SHA-256) ve $6$
// (SHA-512) biçimlerini tanır. $6$ bunların en güçlüsü ve bugünkü dağıtımların
// varsayılanı.
//
// Algoritma Ulrich Drepper'ın "Unix crypt using SHA-256 and SHA-512"
// belirtimidir; doğruluğu belirtimin resmi test vektörleriyle sınanıyor
// (shacrypt_test.go) ve aynı parola hem host glibc'si hem de imajdaki
// BusyBox mkpasswd ile karşılaştırıldı.

const (
	// sha512DefaultRounds, belirtimin varsayılanı. Çıktıda "rounds=" yazılmaz.
	sha512DefaultRounds = 5000
	sha512MinRounds     = 1000
	sha512MaxRounds     = 999999999
	sha512MaxSalt       = 16

	// cryptAlphabet, crypt(3)'ün kendine özgü base64 alfabesi (RFC 4648 DEĞİL).
	cryptAlphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// sha512Crypt computes crypt(3) with a "$6$" setting string.
//
// setting "$6$tuz" ya da "$6$rounds=N$tuz" biçimindedir; ardından gelen
// "$özet" kısmı (varsa) yok sayılır — böylece mevcut bir shadow alanı da
// verilebilir.
func sha512Crypt(password, setting string) (string, error) {
	const prefix = "$6$"
	if !strings.HasPrefix(setting, prefix) {
		return "", errors.New("SHA-512 crypt ayarı $6$ ile başlamalı")
	}
	rest := setting[len(prefix):]

	rounds := sha512DefaultRounds
	customRounds := false
	if strings.HasPrefix(rest, "rounds=") {
		end := strings.IndexByte(rest, '$')
		if end < 0 {
			return "", errors.New("rounds= alanı $ ile bitmiyor")
		}
		n, err := strconv.ParseUint(rest[len("rounds="):end], 10, 64)
		if err != nil {
			return "", fmt.Errorf("rounds değeri geçersiz: %w", err)
		}
		// Belirtim: aralık dışındaki değer REDDEDİLMEZ, sınıra çekilir.
		switch {
		case n < sha512MinRounds:
			rounds = sha512MinRounds
		case n > sha512MaxRounds:
			rounds = sha512MaxRounds
		default:
			rounds = int(n)
		}
		customRounds = true
		rest = rest[end+1:]
	}

	salt := rest
	if i := strings.IndexByte(salt, '$'); i >= 0 {
		salt = salt[:i]
	}
	if len(salt) > sha512MaxSalt {
		salt = salt[:sha512MaxSalt]
	}

	sum := sha512Rounds([]byte(password), []byte(salt), rounds)

	var b strings.Builder
	b.WriteString(prefix)
	if customRounds {
		fmt.Fprintf(&b, "rounds=%d$", rounds)
	}
	b.WriteString(salt)
	b.WriteByte('$')
	encodeSHA512(&b, sum)
	return b.String(), nil
}

// sha512Rounds is steps 1–21 of the specification.
func sha512Rounds(pw, salt []byte, rounds int) []byte {
	// B = özet(parola + tuz + parola)
	h := sha512.New()
	h.Write(pw)
	h.Write(salt)
	h.Write(pw)
	altB := h.Sum(nil)

	// A = özet(parola + tuz + B'nin parola uzunluğu kadarı + bit deseni)
	h.Reset()
	h.Write(pw)
	h.Write(salt)
	n := len(pw)
	for ; n > sha512.Size; n -= sha512.Size {
		h.Write(altB)
	}
	h.Write(altB[:n])
	for n = len(pw); n > 0; n >>= 1 {
		if n&1 != 0 {
			h.Write(altB)
		} else {
			h.Write(pw)
		}
	}
	a := h.Sum(nil)

	// P dizisi: parolanın len(pw) kez tekrarının özetinden len(pw) bayt.
	h.Reset()
	for i := 0; i < len(pw); i++ {
		h.Write(pw)
	}
	p := repeatTo(h.Sum(nil), len(pw))

	// S dizisi: tuzun 16+A[0] kez tekrarının özetinden len(salt) bayt.
	h.Reset()
	for i := 0; i < 16+int(a[0]); i++ {
		h.Write(salt)
	}
	s := repeatTo(h.Sum(nil), len(salt))

	// Yavaşlatma döngüsü: kaba kuvvet denemesini pahalı yapan kısım.
	for i := 0; i < rounds; i++ {
		h.Reset()
		if i&1 != 0 {
			h.Write(p)
		} else {
			h.Write(a)
		}
		if i%3 != 0 {
			h.Write(s)
		}
		if i%7 != 0 {
			h.Write(p)
		}
		if i&1 != 0 {
			h.Write(a)
		} else {
			h.Write(p)
		}
		a = h.Sum(a[:0])
	}
	return a
}

// repeatTo returns n bytes made of digest repeated.
func repeatTo(digest []byte, n int) []byte {
	out := make([]byte, 0, n)
	for len(out)+len(digest) <= n {
		out = append(out, digest...)
	}
	return append(out, digest[:n-len(out)]...)
}

// sha512Order is the byte permutation of step 22e of the specification.
//
// Sıra keyfi görünür ama belirtimin parçasıdır: yanlış bir üçlü, doğru
// uzunlukta ama hiçbir crypt()'in tanımadığı bir özet üretir ve giriş sessizce
// başarısız olur.
var sha512Order = [21][3]int{
	{0, 21, 42}, {22, 43, 1}, {44, 2, 23}, {3, 24, 45}, {25, 46, 4},
	{47, 5, 26}, {6, 27, 48}, {28, 49, 7}, {50, 8, 29}, {9, 30, 51},
	{31, 52, 10}, {53, 11, 32}, {12, 33, 54}, {34, 55, 13}, {56, 14, 35},
	{15, 36, 57}, {37, 58, 16}, {59, 17, 38}, {18, 39, 60}, {40, 61, 19},
	{62, 20, 41},
}

func encodeSHA512(b *strings.Builder, sum []byte) {
	put := func(b2, b1, b0 byte, n int) {
		w := uint(b2)<<16 | uint(b1)<<8 | uint(b0)
		for ; n > 0; n-- {
			b.WriteByte(cryptAlphabet[w&0x3f])
			w >>= 6
		}
	}
	for _, o := range sha512Order {
		put(sum[o[0]], sum[o[1]], sum[o[2]], 4)
	}
	put(0, 0, sum[63], 2)
}

// newSalt returns a random 16-character crypt salt.
func newSalt() (string, error) {
	buf := make([]byte, sha512MaxSalt)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rastgele tuz üretilemedi: %w", err)
	}
	for i, c := range buf {
		// 256, 64'ün katı: mod alma alfabede eğilim üretmez.
		buf[i] = cryptAlphabet[int(c)%len(cryptAlphabet)]
	}
	return string(buf), nil
}

// HashPassword returns a fresh "$6$" shadow hash for password.
func HashPassword(password string) (string, error) {
	salt, err := newSalt()
	if err != nil {
		return "", err
	}
	return sha512Crypt(password, "$6$"+salt)
}

// validShadowHash reports whether h looks like a hash this package produced.
//
// ── Neden sıkı ──────────────────────────────────────────────────────────────
// Kalıcı dosyadan okunan değer /etc/shadow'a yazılıyor. Bozulmuş bir dosya
// (yarım yazım, elle düzenleme) oraya boş ya da ":" içeren bir alan koyarsa
// ya herkes girer ya da shadow dosyasının geri kalanı kayar. Yalnızca
// tanıdığımız biçim kabul edilir.
func validShadowHash(h string) bool {
	const prefix = "$6$"
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	rest := h[len(prefix):]
	if strings.HasPrefix(rest, "rounds=") {
		end := strings.IndexByte(rest, '$')
		if end < 0 {
			return false
		}
		if _, err := strconv.ParseUint(rest[len("rounds="):end], 10, 32); err != nil {
			return false
		}
		rest = rest[end+1:]
	}
	i := strings.IndexByte(rest, '$')
	if i < 1 || i > sha512MaxSalt {
		return false
	}
	salt, sum := rest[:i], rest[i+1:]
	// 64 baytlık özet 86 karaktere kodlanır (21×4 + 2).
	return len(sum) == 86 && onlyCryptChars(salt) && onlyCryptChars(sum)
}

func onlyCryptChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(cryptAlphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}
