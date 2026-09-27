package sshd

import (
	"strings"
	"testing"
)

// Drepper'ın "Unix crypt using SHA-256 and SHA-512" belirtimindeki SHA-512
// test vektörlerinin TAMAMI. Her biri, imajın kendi glibc 2.38 libcrypt'iyle
// (os/buildroot/output/target/lib/libcrypt.so.1, hedefin ld-linux'u ile
// host'ta çalıştırılarak) bire bir doğrulandı — sshd'nin girişte kullanacağı
// crypt() tam olarak odur.
var sha512Vectors = []struct{ setting, password, want string }{
	{"$6$saltstring", "Hello world!",
		"$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1"},
	{"$6$rounds=10000$saltstringsaltstring", "Hello world!",
		"$6$rounds=10000$saltstringsaltst$OW1/O6BYHV6BcXZu8QVeXbDWra3Oeqh0sbHbbMCVNSnCM/UrjmM0Dp8vOuZeHBy/YTBmSK6H9qs/y3RnOaw5v."},
	{"$6$rounds=5000$toolongsaltstring", "This is just a test",
		"$6$rounds=5000$toolongsaltstrin$lQ8jolhgVRVhY4b5pZKaysCLi0QBxGoNeKQzQ3glMhwllF7oGDZxUhx1yxdYcz/e1JSbq3y6JMxxl8audkUEm0"},
	{"$6$rounds=1400$anotherlongsaltstring",
		"a very much longer text to encrypt.  This one even stretches over morethan one line.",
		"$6$rounds=1400$anotherlongsalts$POfYwTEok97VWcjxIiSOjiykti.o/pQs.wPvMxQ6Fm7I6IoYN3CmLs66x9t0oSwbtEW7o7UmJEiDwGqd8p4ur1"},
	{"$6$rounds=77777$short", "we have a short salt string but not a short password",
		"$6$rounds=77777$short$WuQyW2YR.hBNpjjRhpYD/ifIw05xdfeEyQoMxIXbkvr0gge1a1x3yRULJ5CCaUeOxFmtlcGZelFl5CxtgfiAc0"},
	{"$6$rounds=123456$asaltof16chars..", "a short string",
		"$6$rounds=123456$asaltof16chars..$BtCwjqMJGx5hrJhZywWvt0RLE8uZ4oPwcelCjmw2kSYu.Ec6ycULevoBK25fs2xXgMNrCzIMVcgEJAstJeonj1"},
	// Alt sınırın altındaki tur sayısı REDDEDİLMEZ, 1000'e çekilir (glibc
	// davranışı; host'taki libxcrypt bunu reddettiği için yalnızca hedefin
	// glibc'siyle doğrulanabildi).
	{"$6$rounds=10$roundstoolow", "the minimum number is still observed",
		"$6$rounds=1000$roundstoolow$kUMsbe306n21p9R.FRkW3IGn.S9NPN0x50YhH1xhLsPuWGsUSklZt58jaTfF4ZEQpyUNGc0dqbpBYYBaHHrsX."},
}

func TestSHA512CryptSpecVectors(t *testing.T) {
	for _, v := range sha512Vectors {
		got, err := sha512Crypt(v.password, v.setting)
		if err != nil {
			t.Fatalf("%s: %v", v.setting, err)
		}
		if got != v.want {
			t.Errorf("%s:\n got  %s\n want %s", v.setting, got, v.want)
		}
		// Belirtimin çıktısı yeniden ayar olarak verildiğinde aynı sonucu
		// vermeli: doğrulama (crypt(parola, shadow_alanı)) tam olarak budur.
		again, err := sha512Crypt(v.password, got)
		if err != nil || again != got {
			t.Errorf("%s: kendi çıktısıyla doğrulanamadı: %q %v", v.setting, again, err)
		}
	}
}

// Parola uzunluğu 64 baytı aştığında ve tam katına denk geldiğinde
// algoritmanın ayrı dalları çalışır (B'nin tamamı + kalanı). Belirtim
// vektörlerinde 64'ten uzun parola yok; ayrıca panelde Türkçe karakterli ve
// ":" içeren parola yazılabiliyor (chpasswd'ın "kullanıcı:parola" biçimi artık
// yok). Beklenen değerlerin hepsi hedefin glibc libcrypt'iyle üretildi.
func TestSHA512CryptMatchesTargetGlibc(t *testing.T) {
	for _, v := range []struct{ setting, password, want string }{
		{"$6$uzunparola", strings.Repeat("0123456789", 13), // 130 bayt
			"$6$uzunparola$8tPEn9EhY7GByjFRkjxnpqxy/aOq6UBzsgChmyzh/.9ZzWuwdPTBksXcyMSwqTn.XxHz0XX7qOpvAwwmgPVns0"},
		{"$6$sinir", strings.Repeat("a", 64), // tam bir blok
			"$6$sinir$qwL7XcYckKDuyhnPpbtaXwXgzgi0yUW0aZqBNheL0LKsA63FYiei3OP2C9W93VZSRlYk5FJmW03UFSZFjzsvf1"},
		{"$6$sinir", strings.Repeat("a", 128), // tam iki blok
			"$6$sinir$scYSFpxbbqneMLStKlJJX5n54ydGmAnb2/axmo571ia7FH48B789h819.ExaFJod8eUH10rI.VLlE8z4FQJIc0"},
		{"$6$turkce", "şifre:Çok:Güçlü",
			"$6$turkce$/leQNEuFLNhs7814HC3KAAwcym2i4jVXUBQlhX3qsuTSr.jWZdjLH3.WHbdj7YMRZLDsM3/5br4jUIOLbCT9j/"},
	} {
		got, err := sha512Crypt(v.password, v.setting)
		if err != nil {
			t.Fatal(err)
		}
		if got != v.want {
			t.Errorf("%d baytlık parola:\n got  %s\n want %s", len(v.password), got, v.want)
		}
	}
}

func TestHashPasswordFormat(t *testing.T) {
	a, err := HashPassword("yenisifre123")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := HashPassword("yenisifre123")
	if a == b {
		t.Fatal("aynı parola aynı özeti verdi: tuz rastgele değil")
	}
	if !validShadowHash(a) {
		t.Fatalf("üretilen özet kendi biçim denetimimizden geçmiyor: %s", a)
	}
	// Varsayılan tur sayısında "rounds=" yazılmaz (glibc biçimi).
	if strings.Contains(a, "rounds=") {
		t.Fatalf("varsayılan turda rounds= yazılmamalı: %s", a)
	}
	// Doğrulama: özet, ayar olarak verildiğinde kendini üretmeli.
	if again, _ := sha512Crypt("yenisifre123", a); again != a {
		t.Fatalf("doğru parola doğrulanmadı")
	}
	if wrong, _ := sha512Crypt("yanlis-sifre", a); wrong == a {
		t.Fatalf("YANLIŞ parola doğrulandı")
	}
}

func TestValidShadowHash(t *testing.T) {
	good, _ := HashPassword("x")
	for _, h := range []string{good, sha512Vectors[1].want} {
		if !validShadowHash(h) {
			t.Errorf("geçerli sayılmalı: %s", h)
		}
	}
	for _, h := range []string{
		"", "*", "!", "root",
		"$5$Gx8KRqxFYYLq7$zqX2/eZh52nivR14AaBNRNAYMseLgHFCzAqHJgMXUY5", // SHA-256: bizim ürettiğimiz değil
		"$6$tuz$kisa",
		"$6$$" + strings.Repeat("a", 86),                                // boş tuz
		"$6$tuz:ek$" + strings.Repeat("a", 86),                          // ":" shadow alanını kaydırır
		"$6$tuz$" + strings.Repeat("a", 85) + "\n",                      // satır sonu
		"$6$rounds=abc$tuz$" + strings.Repeat("a", 86),                  // bozuk tur
		"$6$" + strings.Repeat("s", 17) + "$" + strings.Repeat("a", 86), // tuz > 16
	} {
		if validShadowHash(h) {
			t.Errorf("GEÇERSİZ sayılmalı: %q", h)
		}
	}
}
