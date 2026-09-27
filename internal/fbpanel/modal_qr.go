package fbpanel

import (
	"image"

	"mcos/internal/fbui"
	"mcos/internal/qr"
)

// ════════════════════════════════════════════════════════════════════════════
// QR EŞLEŞME PENCERESİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden var ───────────────────────────────────────────────────────────────
//
// Eşleşme bugün ELLE yapılıyor: kullanıcı ekrandan IP'yi, portu ve 32 haneli
// onaltılık jetonu telefona TEK TEK yazıyor. Jetonda tek bir karakter hatası
// "yetkisiz" hatası veriyor ve hangi karakterin yanlış olduğu görünmüyor.
//
// Daha kötüsü: sertifika parmak izi ekranda KESİLİYOR ve telefon ilk gördüğü
// sertifikayı sessizce kabul ediyor. Yani bugün hiçbir bant dışı doğrulama
// YOK — yerel ağda savunulabilir, tünel üzerinden savunulamaz.
//
// QR ikisini birden çözüyor: yazım hatası ortadan kalkıyor ve parmak izi de
// optik kanaldan (ekran → kamera, fiziksel olarak yerel) geçiyor.
//
// ── Neden adres GÖMÜLMÜYOR ──────────────────────────────────────────────────
//
// Tünel adresi her cloudflared yeniden başlatmasında değişiyor. Bu yüzden
// hiçbir yere sabit adres yazılmıyor: QR EŞLEŞME ANINDA üretiliyor ve o anki
// geçerli adresi taşıyor. Adres değişse bile yeni QR doğru olur.

// qrModal shows a pairing QR plus the same values as text.
//
// Metin BİLEREK duruyor: kamerası çalışmayan ya da QR okuyamayan bir telefon
// hâlâ elle girebilmeli. QR bir kolaylık, tek yol değil.
type qrModal struct {
	baslik string
	uri    string
	satir  []string
	// hata, QR üretilemediyse sebebi. Boş bir kare göstermektense söylüyoruz.
	hata string
	// kutu, QR'ın son çizimde kapladığı alan (sessiz bölge dahil); modul,
	// matrisin kenar uzunluğu (modül sayısı). Testler bunlarla QR'ın
	// GERÇEKTEN çizildiğini ve modül başına kaç piksel düştüğünü ölçüyor.
	kutu  image.Rectangle
	modul int
}

// newQRModal builds the pairing dialog.
func newQRModal(baslik, uri string, satir []string) *qrModal {
	return &qrModal{baslik: baslik, uri: uri, satir: satir}
}

// Title implements Modal.
func (m *qrModal) Title() string { return m.baslik }

// Size implements Modal.
//
// Genişlik QR'ın kendisinden değil METNİN en uzun satırından geliyor: QR
// ölçeği kalan yere göre seçiliyor (bkz. fbui.DrawQR). Böylece pencere
// çözünürlükten bağımsız olarak dengeli duruyor.
func (m *qrModal) Size() (int, int) {
	w := 46
	for _, s := range m.satir {
		if n := len([]rune(s)) + 6; n > w {
			w = n
		}
	}
	if w > 80 {
		w = 80
	}
	// Yükseklik: metin satırları + QR için ayrılan alan + düğme satırı.
	return w, len(m.satir) + qrSatirYuksekligi + 6
}

// qrSatirYuksekligi, QR'a ayrılan yer (metin satırı cinsinden).
//
// 18 satır: 1080p'de ~360 piksel eder ve sürüm 9 bir QR'ı (53 modül + 8
// sessiz bölge = 61) 5 piksel/modül ile çizmeye yeter. Telefon kameraları
// 5 pikselden küçük modülü ekrandan güvenilir okumuyor.
const qrSatirYuksekligi = 18

// Draw implements Modal.
func (m *qrModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	y := r.Min.Y

	for _, s := range m.satir {
		u.Text(r.Min.X, y, s, u.Pal.TextDim)
		y += u.F.CellH
	}
	y += u.M.PadY

	alt := r.Max.Y - u.M.ButtonH - u.M.PadY
	alan := image.Rect(r.Min.X, y, r.Max.X, alt)
	m.kutu = image.Rectangle{}
	if alan.Dy() > 0 {
		kutu, err := u.DrawQR([]byte(m.uri), alan)
		switch {
		case err != nil:
			m.hata = "QR üretilemedi: " + err.Error()
		case kutu.Empty():
			// DrawQR sığmayan QR'ı BİLEREK çizmez (yarım QR okunmaz) ama
			// bunu hata saymaz. Eskiden pencere de saymıyordu: küçük
			// ekranlarda QR'ın yeri sessizce boş kalıyordu. Kullanıcıya ne
			// yapacağını söylüyoruz: değerler yukarıda yazılı, elle girilir.
			m.hata = "Ekran QR için küçük — yukarıdaki değerleri elle girin."
		default:
			m.hata = ""
			m.kutu = kutu
			if m.modul == 0 {
				if q, e := qr.Encode([]byte(m.uri)); e == nil {
					m.modul = q.Size
				}
			}
		}
	}
	if m.hata != "" {
		u.Text(r.Min.X, y, m.hata, u.Pal.Error)
	}

	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Kapat", Key: "Esc", Style: fbui.ButtonSecondary},
	}, 0)
	if len(btns) > 0 {
		a.addZone(btns[0], zoneModalCancel, 0)
	}
}

// Key implements Modal.
func (m *qrModal) Key(a *App, key string) bool {
	switch key {
	case "esc", "enter", "confirm":
		return true
	}
	return false
}
