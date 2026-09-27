package fbui

import (
	"image"
	"image/color"

	"mcos/internal/qr"
)

// ════════════════════════════════════════════════════════════════════════════
// EKRANA QR ÇİZME
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu kadar dikkat gerekiyor ─────────────────────────────────────────
//
// QR ekrandan telefon kamerasıyla okunacak. Kameranın gördüğü şey, ekranın
// piksel ızgarası ile kameranın sensör ızgarasının çarpışmasıdır. İki şey
// yanlış olursa kod okunmaz ve sebebi görünmez:
//
//  1. MODÜL BOYUTU tam sayı piksel OLMALI. Kesirli bir ölçekte kenarlar
//     yarım piksele düşer, kamera onları gri görür ve eşikleme modülü yanlış
//     tarafa yuvarlar. Bu yüzden ölçek her zaman aşağı yuvarlanıyor.
//  2. SESSİZ BÖLGE (quiet zone) şart. Standart dört modül ister; bu bir süs
//     değil, bulucu desenlerin arka plandan ayırt edilmesini sağlayan şey.
//     Koyu bir arayüzün üstüne sessiz bölgesiz bir QR koymak, kodu okunmaz
//     yapar — ve "bazen okuyor" gibi görünür, ki hata ayıklaması en zor
//     durumdur.
//
// Renkler de bilerek saf: koyu modüller SİYAH, açık modüller BEYAZ. Panelin
// teması ne olursa olsun QR'ın kendi kontrastı korunur.

// QuietZone, standardın istediği kenar boşluğu (modül cinsinden).
const QuietZone = 4

// QRSize returns the pixel size a QR for `data` needs at the given scale.
//
// Çağıran yerin, yer ayırmadan ÖNCE boyutu bilmesi gerekiyor: pencere
// yüksekliği buna göre hesaplanıyor.
func QRSize(data []byte, olcek int) (int, error) {
	m, err := qr.Encode(data)
	if err != nil {
		return 0, err
	}
	return (m.Size + 2*QuietZone) * olcek, nil
}

// QRScaleFor picks the largest integer module size that fits in `maxPx`.
//
// Tam sayı ölçek ZORUNLU (bkz. yukarıdaki not). En küçük 2 piksel: 1 piksellik
// modül, telefon kamerasının ekrandan okuyabileceği sınırın altında.
func QRScaleFor(m *qr.Matrix, maxPx int) int {
	toplam := m.Size + 2*QuietZone
	olcek := maxPx / toplam
	if olcek < 2 {
		olcek = 2
	}
	if olcek > 12 {
		olcek = 12 // daha büyüğü yalnızca yer kaplar
	}
	return olcek
}

// DrawQR paints a QR code for data, centred in `area`, and returns the
// rectangle it actually covered.
//
// Sığmazsa hiçbir şey çizilmez ve boş dikdörtgen döner: yarım bir QR,
// okunamayan bir QR'dan daha kötüdür — kullanıcı okumaya ÇALIŞIR.
func (u *UI) DrawQR(data []byte, area image.Rectangle) (image.Rectangle, error) {
	m, err := qr.Encode(data)
	if err != nil {
		return image.Rectangle{}, err
	}

	kenar := area.Dx()
	if area.Dy() < kenar {
		kenar = area.Dy()
	}
	olcek := QRScaleFor(m, kenar)
	toplam := (m.Size + 2*QuietZone) * olcek
	if toplam > area.Dx() || toplam > area.Dy() {
		return image.Rectangle{}, nil
	}

	x0 := area.Min.X + (area.Dx()-toplam)/2
	y0 := area.Min.Y + (area.Dy()-toplam)/2
	kutu := image.Rect(x0, y0, x0+toplam, y0+toplam)

	// Sessiz bölge DAHİL beyaz zemin. Tek çağrıda: modül modül beyaz basmak
	// aynı sonucu çok daha yavaş verir.
	beyaz := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	siyah := color.RGBA{A: 255}
	u.P.Fill(kutu, beyaz)

	for y := 0; y < m.Size; y++ {
		// Aynı satırdaki ARDIŞIK koyu modüller tek dikdörtgende birleşiyor:
		// 57x57'lik bir matriste modül başına ayrı çağrı 3249 çağrı demek.
		x := 0
		for x < m.Size {
			if !m.At(x, y) {
				x++
				continue
			}
			bas := x
			for x < m.Size && m.At(x, y) {
				x++
			}
			u.P.Fill(image.Rect(
				x0+(QuietZone+bas)*olcek,
				y0+(QuietZone+y)*olcek,
				x0+(QuietZone+x)*olcek,
				y0+(QuietZone+y+1)*olcek,
			), siyah)
		}
	}
	return kutu, nil
}
