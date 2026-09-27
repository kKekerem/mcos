package remote

import "testing"

// ════════════════════════════════════════════════════════════════════════════
// EŞLEŞME URI'Sİ
// ════════════════════════════════════════════════════════════════════════════
//
// Bu URI bir QR'ın içine giriyor ve telefon onu okuyup MAKİNENİN TAM DENETİMİNİ
// veren bir jetonu karşı tarafa gönderiyor. Biçimdeki sessiz bir hata, jetonun
// yanlış yere gitmesi demek olabilir.

func TestPairURIGidipGeliyor(t *testing.T) {
	const (
		host = "shepherd-pac-radical-legitimate.trycloudflare.com"
		port = 443
		tok  = "342881e912fd716dd3cab3b48976c4ff"
		fp   = "A3:88:0F:E7:79:9B:52:EE:4E:25:9A:76:D7:3F:76:EF"
	)
	uri := PairURI(host, port, tok, fp)

	h, p, tk, f, err := ParsePairURI(uri)
	if err != nil {
		t.Fatalf("kendi ürettiğimiz URI çözülemedi: %v\n%s", err, uri)
	}
	if h != host || p != port || tk != tok {
		t.Errorf("alanlar bozuldu: h=%q p=%d t=%q", h, p, tk)
	}
	if f != NormalizeFingerprint(fp) {
		t.Errorf("parmak izi bozuldu: %q", f)
	}
}

// Parmak izi ÜÇ ayrı biçimde dolaşıyor; karşılaştırma tek biçim üzerinden
// olmalı, yoksa doğrulama boşuna "eşleşmedi" der.
func TestParmakIziNormalizasyonu(t *testing.T) {
	const bekle = "a3880fe779"
	for _, giris := range []string{
		"A3:88:0F:E7:79",
		"a3:88:0f:e7:79",
		"A3880FE779",
		"a3 88 0f e7 79",
		"A3-88-0F-E7-79",
	} {
		if got := NormalizeFingerprint(giris); got != bekle {
			t.Errorf("%q -> %q; %q bekleniyordu", giris, got, bekle)
		}
	}
}

// Parmak izi OLMADAN da URI üretilebilmeli (henüz sertifika yoksa), ama
// alan boş bırakılmalı — uydurma bir değer koymak felaket olurdu.
func TestParmakIziYoksaAlanYok(t *testing.T) {
	uri := PairURI("10.0.2.15", 2223, "abc", "")
	if _, _, _, f, err := ParsePairURI(uri); err != nil || f != "" {
		t.Errorf("parmak izi yokken f=%q err=%v", f, err)
	}
}

func TestBozukURIReddedilir(t *testing.T) {
	for _, tc := range []struct{ ad, uri string }{
		{"yanlis sema", "https://pair?v=1&h=a&p=1&t=b"},
		{"yanlis surum", "mcos://pair?v=9&h=a&p=1&t=b"},
		{"jeton yok", "mcos://pair?v=1&h=a&p=1"},
		{"adres yok", "mcos://pair?v=1&p=1&t=b"},
		{"port yok", "mcos://pair?v=1&h=a&t=b"},
		{"port sifir", "mcos://pair?v=1&h=a&p=0&t=b"},
	} {
		if _, _, _, _, err := ParsePairURI(tc.uri); err == nil {
			t.Errorf("%s: bozuk URI kabul edildi (%s)", tc.ad, tc.uri)
		}
	}
}

// URI, QR'a SIĞMALI. Sürüm 10 sınırı 213 bayt; uzun bir tünel adıyla bile
// altında kalmalıyız, yoksa eşleşme ekranı hiç çizilemez.
func TestURIQRSinirininAltinda(t *testing.T) {
	uri := PairURI(
		"shepherd-pac-radical-legitimate.trycloudflare.com", 443,
		"342881e912fd716dd3cab3b48976c4ff",
		"A3:88:0F:E7:79:9B:52:EE:4E:25:9A:76:D7:3F:76:EF:C3:4C:41:9F:D0:94:07:AC:7C:98:A3:38:26:01:51:4E")
	if len(uri) > 213 {
		t.Errorf("URI %d bayt — QR sürüm 10 sınırı 213; eşleşme kodu çizilemez:\n%s",
			len(uri), uri)
	}
	t.Logf("URI uzunluğu: %d bayt", len(uri))
}

// Birden çok adres: sıra KORUNMALI (telefon sırayla dener) ve yalnızca ilk
// "h"yi okuyan eski bir çözücü en olası adresi almalı.
func TestCokAdresliURI(t *testing.T) {
	hosts := []string{"192.168.1.42", "10.0.2.15"}
	uri := PairURIHosts(hosts, 2223, "76d3d64f7febb116d27838771485dfd9", "A6:79:F0")
	p, err := ParsePair(uri)
	if err != nil {
		t.Fatalf("%v\n%s", err, uri)
	}
	if len(p.Hosts) != 2 || p.Hosts[0] != hosts[0] || p.Hosts[1] != hosts[1] {
		t.Fatalf("adres sırası bozuldu: %v", p.Hosts)
	}
	if p.Fingerprint != "a679f0" {
		t.Errorf("parmak izi normalleşmedi: %q", p.Fingerprint)
	}
	if h, _, _, _, err := ParsePairURI(uri); err != nil || h != hosts[0] {
		t.Errorf("tek adresli okuyucu ilk adresi almadı: %q %v", h, err)
	}
}

// Panel en fazla üç adres koyuyor; en uzun IPv4'lerle bile QR sürüm 10'a
// (213 bayt) sığmalı.
func TestUcAdresQRSinirininAltinda(t *testing.T) {
	uri := PairURIHosts(
		[]string{"192.168.100.200", "172.31.255.254", "10.100.200.250"}, 65535,
		"342881e912fd716dd3cab3b48976c4ff",
		"A3:88:0F:E7:79:9B:52:EE:4E:25:9A:76:D7:3F:76:EF:C3:4C:41:9F:D0:94:07:AC:7C:98:A3:38:26:01:51:4E")
	if len(uri) > 213 {
		t.Errorf("URI %d bayt — sürüm 10 sınırı aşıldı:\n%s", len(uri), uri)
	}
	t.Logf("üç adresli URI: %d bayt", len(uri))
}
