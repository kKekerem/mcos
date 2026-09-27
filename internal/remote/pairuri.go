package remote

import (
	"fmt"
	"net/url"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// EŞLEŞME URI'Sİ (QR'IN İÇİNDEKİ)
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden parmak izi QR'da OLMAK ZORUNDA ────────────────────────────────────
//
// Bu protokol kendinden imzalı TLS kullanıyor ve telefon bugün ilk gördüğü
// sertifikayı SESSİZCE kabul ediyor (saf TOFU: trust on first use). TOFU'nun
// bütün güvenliği tek bir varsayıma dayanır: "ilk bağlantı saldırıya
// uğramamıştır".
//
// O varsayım, yerel ağda savunulabilir. Ama kullanıcı artık TÜNEL üzerinden
// bağlanacak — yani ilk bağlantı, kendisinin denetlemediği ağlardan geçecek.
// Orada varsayım ÇÖKER.
//
// Parmak izini QR'a koymak TOFU'yu BANT DIŞI ANAHTAR SABİTLEMESİNE çevirir:
// güven çapası optik kanaldan geçer (ekran → kamera; fiziksel olarak yerel,
// yönlendirilemez), saldırılan ağdan değil. Telefon, jetonu GÖNDERMEDEN ÖNCE
// karşı tarafın özel anahtarı elinde tuttuğunu doğrulayabilir.
//
// Sıra hayati: jeton makinenin TAM denetimini veriyor (dosya yazma, sunucu
// komutu). Yanlış tarafa gönderilen bir jeton, makinenin kaybı demektir.
//
// ── Neden özel bir şema ─────────────────────────────────────────────────────
//
// "https://..." yazsaydık telefonun tarayıcısı açılırdı. "mcos://" şemasını
// yalnızca MCOS uygulaması açar; kamera uygulaması da onu bir bağlantı diye
// göstermek yerine uygulamaya devreder.

// PairURI builds the URI encoded into the pairing QR code.
//
// Alanlar KISA tutuluyor, çünkü her karakter QR'ı büyütüyor ve büyüyen QR
// daha küçük modül demek — telefon kamerası için daha zor.
//
//	v  sürüm (ileride biçim değişirse telefon eskiyi tanısın)
//	h  adres (IP ya da tünel ana adı)
//	p  port
//	t  jeton
//	f  sertifika parmak izi (SHA-256, iki nokta ve harf boyutu OLMADAN)
//
// Parmak izi ekranda "A3:88:0F:..." biçiminde gösteriliyor; QR'da ayraçlar
// atılıyor çünkü 31 karakter kazandırıyor ve telefon karşılaştırmayı
// normalleştirerek yapıyor.
func PairURI(host string, port int, token, fingerprint string) string {
	return PairURIHosts([]string{host}, port, token, fingerprint)
}

// PairURIHosts builds the pairing URI with several candidate addresses.
//
// ── Neden birden çok adres ──────────────────────────────────────────────────
//
// Makinenin birden çok ağ adresi olabiliyor ve hangisine telefonun
// ulaşabildiğini panel BİLEMEZ. Ölçülen örnek: VirtualBox'ta NAT + Köprü iki
// bağdaştırıcı açıkken ilk adres 10.0.2.15 (NAT) çıkıyor; telefon oraya
// hiç ulaşamıyor, köprü adresine ise ulaşıyor. Tek adresli QR, kullanıcıya
// "QR okundu ama bağlanmıyor" dedirtiyordu.
//
// "h" alanı TEKRARLANIYOR (h=A&h=B): url.Values bunu doğal olarak taşır ve
// yalnızca ilkini okuyan eski bir istemci de ilk (en olası) adresle çalışır.
// Telefon adresleri sırayla /health ile dener, ilk yanıt verene bağlanır.
func PairURIHosts(hosts []string, port int, token, fingerprint string) string {
	q := url.Values{}
	q.Set("v", "1")
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			q.Add("h", h)
		}
	}
	q.Set("p", fmt.Sprintf("%d", port))
	q.Set("t", token)
	if f := NormalizeFingerprint(fingerprint); f != "" {
		q.Set("f", f)
	}
	return "mcos://pair?" + q.Encode()
}

// NormalizeFingerprint strips separators and lowercases a fingerprint.
//
// Aynı parmak izi üç ayrı biçimde dolaşıyor: ekranda iki nokta ile ve büyük
// harfle, QR'da çıplak, telefonda hesaplanmış hâliyle. Karşılaştırmanın TEK
// bir biçim üzerinden yapılması şart — yoksa doğrulama "eşleşmedi" der ve
// kullanıcı güvenlik uyarısını görmezden gelmeyi öğrenir.
func NormalizeFingerprint(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'a' && r <= 'f':
			b.WriteRune(r)
		case r >= 'A' && r <= 'F':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	return b.String()
}

// Pair is the decoded content of a pairing QR.
type Pair struct {
	Hosts       []string // denenecek adresler, öncelik sırasıyla
	Port        int
	Token       string
	Fingerprint string // NormalizeFingerprint biçiminde; boş olabilir
}

// ParsePair reads a pairing URI back. Telefon tarafının (Dart: pair_uri.dart)
// Go karşılığı ve testlerin karşı kıyası; iki çözücü aynı kuralı uygulamalı.
func ParsePair(raw string) (Pair, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Pair{}, fmt.Errorf("eşleşme adresi çözülemedi: %w", err)
	}
	if u.Scheme != "mcos" || u.Host != "pair" {
		return Pair{}, fmt.Errorf("bu bir MCOS eşleşme kodu değil")
	}
	q := u.Query()
	if q.Get("v") != "1" {
		return Pair{}, fmt.Errorf("eşleşme kodu sürümü desteklenmiyor (%q)", q.Get("v"))
	}
	var p Pair
	for _, h := range q["h"] {
		if h = strings.TrimSpace(h); h != "" {
			p.Hosts = append(p.Hosts, h)
		}
	}
	p.Token = q.Get("t")
	p.Fingerprint = NormalizeFingerprint(q.Get("f"))
	if len(p.Hosts) == 0 || p.Token == "" {
		return Pair{}, fmt.Errorf("eşleşme kodunda adres ya da jeton yok")
	}
	if _, e := fmt.Sscanf(q.Get("p"), "%d", &p.Port); e != nil || p.Port <= 0 || p.Port > 65535 {
		return Pair{}, fmt.Errorf("eşleşme kodunda geçerli port yok")
	}
	return p, nil
}

// ParsePairURI is the single-address form of ParsePair (ilk adres).
func ParsePairURI(raw string) (host string, port int, token, fingerprint string, err error) {
	p, err := ParsePair(raw)
	if err != nil {
		return "", 0, "", "", err
	}
	return p.Hosts[0], p.Port, p.Token, p.Fingerprint, nil
}
