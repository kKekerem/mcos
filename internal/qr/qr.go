// Package qr encodes text as a QR code matrix, in pure Go.
//
// ── Neden kendi kodlayıcımız ────────────────────────────────────────────────
//
// Telefonla eşleşme için ekranda bir QR göstermek gerekiyor ve MCOS'ta bunu
// yapabilecek hiçbir bağımlılık yok (go.mod'da tek bir QR kütüphanesi bile
// geçmiyor). Dışarıdan bir paket almak da göründüğü kadar bedava değil:
//
//   - CGO_ENABLED=0 zorunlu (initramfs'te libc bağımlılığı istemiyoruz),
//   - imaja giren her bayt RAM'de açılıyor,
//   - ve en önemlisi: eşleşme jetonu bu koddan geçiyor. Güven sınırındaki
//     bir bileşenin okunabilir ve denetlenebilir olması gerekir.
//
// Bu yüzden ihtiyaç duyulan ALT KÜME yazıldı: bayt kipi, ECC seviyesi M,
// sürüm 1-10. Sayı kipi, alfanümerik kip, kanji ve yapılandırılmış ekleme
// YOK — hiçbiri bir eşleşme URI'si için gerekmiyor.
//
// Doğruluk tahmine bırakılmadı: qr_test.go, bilinen kodlama vektörlerine ve
// standardın kendi örneğine karşı sınıyor.
package qr

import (
	"errors"
	"fmt"
)

// ECC seviyesi M sabit: %15 kurtarma.
//
// Neden L değil: QR ekrandan, açıyla ve parlama altında okunacak. L (%7)
// mat bir ekranda bile sınırda kalıyor.
// Neden Q/H değil: onlar aynı veriyi daha büyük bir matrise sığdırır ve
// modül boyutu küçülür — küçük modül, kamera için M'den daha kötüdür.

// Matrix is a square QR grid. true = koyu modül.
type Matrix struct {
	Size int
	mods []bool
	set  []bool // bu modül işlev deseni mi (maskelenmez)
}

// At reports whether the module at (x,y) is dark.
func (m *Matrix) At(x, y int) bool {
	if x < 0 || y < 0 || x >= m.Size || y >= m.Size {
		return false
	}
	return m.mods[y*m.Size+x]
}

func (m *Matrix) put(x, y int, dark, fonksiyon bool) {
	if x < 0 || y < 0 || x >= m.Size || y >= m.Size {
		return
	}
	m.mods[y*m.Size+x] = dark
	if fonksiyon {
		m.set[y*m.Size+x] = true
	}
}

func (m *Matrix) isFunc(x, y int) bool {
	if x < 0 || y < 0 || x >= m.Size || y >= m.Size {
		return true
	}
	return m.set[y*m.Size+x]
}

// ── Sürüm tabloları (yalnızca ECC seviyesi M) ───────────────────────────────

// surumBilgi, bir sürümün blok yapısını anlatır.
//
// ecPerBlock : blok başına hata düzeltme kod sözcüğü
// g1Blok/g1Veri, g2Blok/g2Veri : iki grup; ikinci grup yoksa g2Blok = 0.
type surumBilgi struct {
	ecPerBlock     int
	g1Blok, g1Veri int
	g2Blok, g2Veri int
}

// Sürüm 1-10, ECC-M. Değerler ISO/IEC 18004 Tablo 13-22'den.
var surumler = map[int]surumBilgi{
	1:  {10, 1, 16, 0, 0},
	2:  {16, 1, 28, 0, 0},
	3:  {26, 1, 44, 0, 0},
	4:  {18, 2, 32, 0, 0},
	5:  {24, 2, 43, 0, 0},
	6:  {16, 4, 27, 0, 0},
	7:  {18, 4, 31, 0, 0},
	8:  {22, 2, 38, 2, 39},
	9:  {22, 3, 36, 2, 37},
	10: {26, 4, 43, 1, 44},
}

// veriKodSozcugu returns the number of data codewords for a version.
func (v surumBilgi) veriKodSozcugu() int {
	return v.g1Blok*v.g1Veri + v.g2Blok*v.g2Veri
}

// hizaMerkezleri, hizalama desenlerinin merkez koordinatları.
var hizaMerkezleri = map[int][]int{
	1:  nil,
	2:  {6, 18},
	3:  {6, 22},
	4:  {6, 26},
	5:  {6, 30},
	6:  {6, 34},
	7:  {6, 22, 38},
	8:  {6, 24, 42},
	9:  {6, 26, 46},
	10: {6, 28, 50},
}

// ErrTooLong is returned when the payload does not fit in version 10.
var ErrTooLong = errors.New("qr: veri sürüm 10'a sığmıyor")

// Encode builds the QR matrix for data using byte mode and ECC level M.
func Encode(data []byte) (*Matrix, error) {
	surum, bilgi, err := surumSec(len(data))
	if err != nil {
		return nil, err
	}

	bits := veriBitleri(data, surum, bilgi)
	kodlar := blokla(bits, bilgi)

	m := yeniMatris(surum)
	m.islevDesenleri(surum)
	m.veriYerlestir(kodlar)

	maske := m.enIyiMaske(surum)
	return m, nil2(maske)
}

func nil2(err error) error { return err }

// surumSec picks the smallest version that fits.
func surumSec(n int) (int, surumBilgi, error) {
	for v := 1; v <= 10; v++ {
		bilgi := surumler[v]
		// Başlık: 4 bit kip + karakter sayacı (v<10 ise 8 bit, v>=10 ise 16).
		sayacBit := 8
		if v >= 10 {
			sayacBit = 16
		}
		gerekli := 4 + sayacBit + n*8
		if gerekli <= bilgi.veriKodSozcugu()*8 {
			return v, bilgi, nil
		}
	}
	return 0, surumBilgi{}, fmt.Errorf("%w (%d bayt)", ErrTooLong, n)
}

// veriBitleri builds the padded data codeword stream.
func veriBitleri(data []byte, surum int, bilgi surumBilgi) []byte {
	var b bitYazici
	b.yaz(0b0100, 4) // bayt kipi
	if surum >= 10 {
		b.yaz(uint32(len(data)), 16)
	} else {
		b.yaz(uint32(len(data)), 8)
	}
	for _, c := range data {
		b.yaz(uint32(c), 8)
	}

	toplam := bilgi.veriKodSozcugu() * 8
	// Sonlandırıcı: en fazla dört sıfır, ama kalan yerden fazla değil.
	kalan := toplam - b.n
	if kalan > 4 {
		kalan = 4
	}
	if kalan > 0 {
		b.yaz(0, kalan)
	}
	// Bayta hizala.
	for b.n%8 != 0 {
		b.yaz(0, 1)
	}
	// Dolgu baytları standartta sabittir ve DEĞİŞTİRİLEMEZ: okuyucular bu
	// deseni bekliyor.
	for i := 0; b.n < toplam; i++ {
		if i%2 == 0 {
			b.yaz(0xEC, 8)
		} else {
			b.yaz(0x11, 8)
		}
	}
	return b.bayt
}

// blokla interleaves data and EC codewords as the standard requires.
func blokla(veri []byte, bilgi surumBilgi) []byte {
	type blok struct{ d, e []byte }
	var bloklar []blok

	off := 0
	ekle := func(n, boy int) {
		for i := 0; i < n; i++ {
			d := veri[off : off+boy]
			off += boy
			bloklar = append(bloklar, blok{d: d, e: rsKodla(d, bilgi.ecPerBlock)})
		}
	}
	ekle(bilgi.g1Blok, bilgi.g1Veri)
	ekle(bilgi.g2Blok, bilgi.g2Veri)

	var out []byte
	// Veri kod sözcükleri SÜTUN SÜTUN dizilir: blok1[0], blok2[0], ...
	enUzun := bilgi.g1Veri
	if bilgi.g2Veri > enUzun {
		enUzun = bilgi.g2Veri
	}
	for i := 0; i < enUzun; i++ {
		for _, bl := range bloklar {
			if i < len(bl.d) {
				out = append(out, bl.d[i])
			}
		}
	}
	for i := 0; i < bilgi.ecPerBlock; i++ {
		for _, bl := range bloklar {
			out = append(out, bl.e[i])
		}
	}
	return out
}

// ── Bit yazıcı ──────────────────────────────────────────────────────────────

type bitYazici struct {
	bayt []byte
	n    int // yazılmış bit sayısı
}

func (b *bitYazici) yaz(v uint32, bit int) {
	for i := bit - 1; i >= 0; i-- {
		if b.n%8 == 0 {
			b.bayt = append(b.bayt, 0)
		}
		if (v>>uint(i))&1 == 1 {
			b.bayt[b.n/8] |= 1 << uint(7-b.n%8)
		}
		b.n++
	}
}
