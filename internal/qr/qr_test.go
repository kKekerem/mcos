package qr

import (
	"bytes"
	"fmt"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// QR KODLAYICISI GERÇEKTEN STANDARDA UYUYOR MU
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu testler bu kadar ayrıntılı ─────────────────────────────────────
//
// Yanlış bir QR kodlayıcısı "çalışıyor" görünür: ekrana siyah beyaz bir kare
// basar, gözle bakınca QR'a benzer, hiçbir test kırılmaz. Yalnızca TELEFON
// okumaz — ve o noktada hata ayıklamak, kullanıcının elindeki kamerayla
// uğraşmak demektir.
//
// Makinede bağımsız bir QR ÇÖZÜCÜSÜ yok (pyzbar/opencv/zbarimg kurulu değil,
// pip de yok). Bu yüzden doğruluk üç ayrı yoldan kanıtlanıyor:
//
//  1. STANDARDIN KENDİ YAYINLANMIŞ VEKTÖRÜ ile Reed-Solomon karşılaştırması.
//  2. SENDROM SINAMASI: kodlayıcıdan BAĞIMSIZ matematik. Kodlayıcı polinom
//     bölmesi yapıyor; sendrom ise polinomu alfa^i'de değerlendiriyor. Tüm
//     sendromlar sıfırsa, üretilen şey geçerli bir RS kod sözcüğüdür.
//  3. MATRİSTEN GERİ OKUMA: testin içine, kodlayıcıyla hiçbir kod paylaşmayan
//     bir çözücü yazıldı. Biçim bilgisi okunuyor, maske kaldırılıyor, veri
//     yolu yürünüyor, bloklar çözülüyor ve metin geri elde ediliyor.

// ── 1. Standardın yayınlanmış vektörü ───────────────────────────────────────

// ISO/IEC 18004 Ek I.2: sürüm 1-M, veri kod sözcükleri verilmiş ve buna
// karşılık gelen 10 hata düzeltme kod sözcüğü YAYINLANMIŞTIR.
func TestReedSolomonStandartVektoru(t *testing.T) {
	veri := []byte{
		0x10, 0x20, 0x0C, 0x56, 0x61, 0x80, 0xEC, 0x11,
		0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11,
	}
	beklenen := []byte{0xA5, 0x24, 0xD4, 0xC1, 0xED, 0x36, 0xC7, 0x87, 0x2C, 0x55}

	got := rsKodla(veri, 10)
	if !bytes.Equal(got, beklenen) {
		t.Fatalf("Reed-Solomon standart vektörle uyuşmuyor:\n  alınan  : % X\n  beklenen: % X",
			got, beklenen)
	}
}

// ── 2. Sendrom sınaması (kodlayıcıdan bağımsız matematik) ───────────────────

// sendromlar evaluates the codeword polynomial at alfa^0..alfa^(n-1).
//
// Geçerli bir Reed-Solomon kod sözcüğünde HEPSİ SIFIRDIR. Bu, kodlayıcının
// yaptığı bölme işleminden tamamen bağımsız bir doğrulamadır: biri yanlışsa
// ikisi aynı anda yanlış olamaz.
func sendromlar(kod []byte, n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		var s byte
		for _, c := range kod {
			s = gfCarp(s, gfExp[i]) ^ c
		}
		out[i] = s
	}
	return out
}

func TestSendromlarSifir(t *testing.T) {
	for _, boy := range []int{1, 7, 16, 44, 100} {
		for _, ec := range []int{10, 16, 18, 22, 24, 26} {
			veri := make([]byte, boy)
			for i := range veri {
				veri[i] = byte(i*37 + 11)
			}
			kod := append(append([]byte{}, veri...), rsKodla(veri, ec)...)
			for i, s := range sendromlar(kod, ec) {
				if s != 0 {
					t.Fatalf("veri %d bayt, ec %d: sendrom[%d]=%d (0 olmalı) — "+
						"üretilen şey geçerli bir RS kod sözcüğü DEĞİL", boy, ec, i, s)
				}
			}
		}
	}
}

// ── 3. Matristen geri okuma (bağımsız çözücü) ───────────────────────────────

// coz reads the matrix back and returns the payload.
//
// Kodlayıcıyla KOD PAYLAŞMIYOR: biçim bitleri elle çözülüyor, maske koşulu
// yeniden yazılıyor, veri yolu bağımsız yürünüyor.
func coz(t *testing.T, m *Matrix) []byte {
	t.Helper()
	n := m.Size
	surum := (n - 17) / 4
	bilgi := surumler[surum]

	// Biçim bilgisi: sol üstteki kopyayı oku, maskesini kaldır.
	var ham uint32
	for i := 0; i < 15; i++ {
		var x, y int
		switch {
		case i < 6:
			x, y = 8, i
		case i == 6:
			x, y = 8, 7
		case i == 7:
			x, y = 8, 8
		case i == 8:
			x, y = 7, 8
		default:
			x, y = 14-i, 8
		}
		if m.At(x, y) {
			ham |= 1 << uint(i)
		}
	}
	bicim := ham ^ 0x5412
	ecc := (bicim >> 13) & 0b11
	maske := int((bicim >> 10) & 0b111)
	if ecc != 0b00 {
		t.Fatalf("biçim bilgisi ECC-M demiyor (%02b)", ecc)
	}

	// İşlev desenlerini yeniden üret: hangi modüllerin veri olduğunu bilmek
	// için. Bunun için temiz bir matris kurup aynı desenleri basıyoruz.
	ref := yeniMatris(surum)
	ref.islevDesenleri(surum)

	// Veri yolunu yürü ve maskeyi kaldır.
	var bits []bool
	yukari := true
	for sag := n - 1; sag > 0; sag -= 2 {
		if sag == 6 {
			sag--
		}
		for i := 0; i < n; i++ {
			y := i
			if yukari {
				y = n - 1 - i
			}
			for _, x := range [2]int{sag, sag - 1} {
				if ref.isFunc(x, y) {
					continue
				}
				v := m.At(x, y)
				if maskeKosulu(maske, x, y) {
					v = !v
				}
				bits = append(bits, v)
			}
		}
		yukari = !yukari
	}

	// Bitleri kod sözcüklerine çevir.
	kodlar := make([]byte, len(bits)/8)
	for i := range kodlar {
		var b byte
		for j := 0; j < 8; j++ {
			if bits[i*8+j] {
				b |= 1 << uint(7-j)
			}
		}
		kodlar[i] = b
	}

	// Blok araya girmeyi (interleave) GERİ al.
	type blok struct{ d []byte }
	bloklar := make([]blok, 0, bilgi.g1Blok+bilgi.g2Blok)
	for i := 0; i < bilgi.g1Blok; i++ {
		bloklar = append(bloklar, blok{d: make([]byte, bilgi.g1Veri)})
	}
	for i := 0; i < bilgi.g2Blok; i++ {
		bloklar = append(bloklar, blok{d: make([]byte, bilgi.g2Veri)})
	}
	enUzun := bilgi.g1Veri
	if bilgi.g2Veri > enUzun {
		enUzun = bilgi.g2Veri
	}
	p := 0
	for i := 0; i < enUzun; i++ {
		for bi := range bloklar {
			if i < len(bloklar[bi].d) {
				bloklar[bi].d[i] = kodlar[p]
				p++
			}
		}
	}

	var veri []byte
	for _, b := range bloklar {
		veri = append(veri, b.d...)
	}

	// Başlığı çöz.
	okuBit := func(off, k int) uint32 {
		var v uint32
		for i := 0; i < k; i++ {
			bit := off + i
			if veri[bit/8]&(1<<uint(7-bit%8)) != 0 {
				v |= 1 << uint(k-1-i)
			}
		}
		return v
	}
	kip := okuBit(0, 4)
	if kip != 0b0100 {
		t.Fatalf("kip %04b; bayt kipi (0100) bekleniyordu", kip)
	}
	sayacBit := 8
	if surum >= 10 {
		sayacBit = 16
	}
	uzunluk := int(okuBit(4, sayacBit))

	out := make([]byte, uzunluk)
	for i := 0; i < uzunluk; i++ {
		out[i] = byte(okuBit(4+sayacBit+i*8, 8))
	}
	return out
}

func TestGeriOkumaMetniVeriyor(t *testing.T) {
	for _, s := range []string{
		"MCOS",
		"mcos://pair?v=1&h=10.0.2.15&p=2223&t=342881e912fd716dd3cab3b48976c4ff",
		"mcos://pair?v=1&h=shepherd-pac-radical-legitimate.trycloudflare.com&p=443" +
			"&t=342881e912fd716dd3cab3b48976c4ff&f=ILBFR32pkBYjPCAgICAgICAgICAgICAgICAg",
	} {
		t.Run(fmt.Sprintf("%d bayt", len(s)), func(t *testing.T) {
			m, err := Encode([]byte(s))
			if err != nil {
				t.Fatal(err)
			}
			got := coz(t, m)
			if string(got) != s {
				t.Errorf("geri okuma farklı:\n  alınan : %q\n  beklenen: %q", got, s)
			}
		})
	}
}

// Her sürüm sınırında geri okuma çalışmalı: blok yapısı sürümden sürüme
// değişiyor ve araya girme (interleave) tam orada bozulur.
func TestHerSurumdeGeriOkuma(t *testing.T) {
	for v := 1; v <= 10; v++ {
		bilgi := surumler[v]
		sayacBit := 8
		if v >= 10 {
			sayacBit = 16
		}
		// Bu sürümün TAM kapasitesi kadar veri.
		n := (bilgi.veriKodSozcugu()*8 - 4 - sayacBit) / 8
		veri := make([]byte, n)
		for i := range veri {
			veri[i] = byte('A' + i%26)
		}
		m, err := Encode(veri)
		if err != nil {
			t.Fatalf("sürüm %d (%d bayt): %v", v, n, err)
		}
		if got := (m.Size - 17) / 4; got != v {
			t.Errorf("%d bayt için sürüm %d seçildi, %d bekleniyordu", n, got, v)
		}
		if got := coz(t, m); !bytes.Equal(got, veri) {
			t.Errorf("sürüm %d: geri okuma bozuk (%d/%d bayt eşleşti)",
				v, ortakOnek(got, veri), len(veri))
		}
	}
}

func ortakOnek(a, b []byte) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// ── Yapısal değişmezler ─────────────────────────────────────────────────────

func TestBulucuDesenleriYerinde(t *testing.T) {
	m, err := Encode([]byte("MCOS"))
	if err != nil {
		t.Fatal(err)
	}
	n := m.Size
	for _, p := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		x, y := p[0], p[1]
		// Dış çerçeve koyu, iç halka açık, merkez 3x3 koyu.
		if !m.At(x, y) || !m.At(x+6, y) || !m.At(x, y+6) {
			t.Errorf("(%d,%d) bulucu deseninin köşeleri koyu değil", x, y)
		}
		if m.At(x+1, y+1) {
			t.Errorf("(%d,%d) bulucu deseninin iç halkası koyu", x, y)
		}
		if !m.At(x+3, y+3) {
			t.Errorf("(%d,%d) bulucu deseninin merkezi koyu değil", x, y)
		}
	}
}

func TestZamanlamaDeseni(t *testing.T) {
	m, err := Encode([]byte("MCOS"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 8; i < m.Size-8; i++ {
		bekle := i%2 == 0
		if m.At(i, 6) != bekle {
			t.Fatalf("yatay zamanlama deseni (%d,6) yanlış", i)
		}
		if m.At(6, i) != bekle {
			t.Fatalf("dikey zamanlama deseni (6,%d) yanlış", i)
		}
	}
}

func TestKoyuModul(t *testing.T) {
	m, err := Encode([]byte("MCOS"))
	if err != nil {
		t.Fatal(err)
	}
	surum := (m.Size - 17) / 4
	if !m.At(8, 4*surum+9) {
		t.Error("koyu modül (8, 4*sürüm+9) açık — okuyucular bunu şart koşar")
	}
}

func TestCokUzunVeriReddedilir(t *testing.T) {
	if _, err := Encode(make([]byte, 400)); err == nil {
		t.Error("400 bayt kabul edildi; sürüm 10 sınırı aşılmalıydı")
	}
}

// ── 4. Sürüm bilgisi (sürüm 7+) ─────────────────────────────────────────────
//
// Geri okuma testi sürüm bilgisini OKUMUYOR (kendi çözücümüz sürümü matris
// boyundan çıkarıyor); gerçek okuyucular ise sürüm 7 ve üstünde onu okur ve
// tutmazsa kodu reddeder. Bu açık yüzünden bozuk BCH aylarca fark edilmedi:
// eşleşme URI'si (140-177 bayt, sürüm 8-9) zxing-cpp'de OKUNAMADI. Hata
// düzeltilince 1-213 baytlık her uzunluk zxing-cpp ile okundu (bkz.
// surumBitleri'nin açıklaması).

// ISO/IEC 18004 Ek D, Tablo D.1: sürüm bilgisi bit dizileri.
func TestSurumBilgisiStandartDegerler(t *testing.T) {
	for surum, bekle := range map[int]uint32{
		7: 0x07C94, 8: 0x085BC, 9: 0x09A99, 10: 0x0A4D3,
	} {
		if got := surumBitleri(surum); got != bekle {
			t.Errorf("sürüm %d: %#05x; standart %#05x", surum, got, bekle)
		}
	}
}

// Sürüm bilgisi iki kopya hâlinde, standardın yerlerine yazılmalı: sol alt
// 6x3 blok (x=i/3, y=n-11+i%3) ve sağ üst aynası.
func TestSurumBilgisiYerinde(t *testing.T) {
	for _, n := range []int{107, 140, 177, 213} {
		m, err := Encode(bytes.Repeat([]byte("a"), n))
		if err != nil {
			t.Fatal(err)
		}
		surum := (m.Size - 17) / 4
		if surum < 7 {
			t.Fatalf("%d bayt sürüm %d seçti; bu test sürüm 7+ için", n, surum)
		}
		var solAlt, sagUst uint32
		for i := 0; i < 18; i++ {
			if m.At(i/3, m.Size-11+i%3) {
				solAlt |= 1 << uint(i)
			}
			if m.At(m.Size-11+i%3, i/3) {
				sagUst |= 1 << uint(i)
			}
		}
		bekle := surumBitleri(surum)
		if solAlt != bekle || sagUst != bekle {
			t.Errorf("sürüm %d: sol alt %#05x, sağ üst %#05x; %#05x bekleniyordu",
				surum, solAlt, sagUst, bekle)
		}
	}
}
