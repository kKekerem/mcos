package fbpanel

import (
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
	"mcos/internal/remote"
)

// ════════════════════════════════════════════════════════════════════════════
// EŞLEŞME QR'I HER ÇÖZÜNÜRLÜKTE GERÇEKTEN ÇİZİLİYOR MU
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı "Android uygulamasında QR kod yeri yok" dedi. Uygulama tarafına
// tarayıcı eklendi; ama telefonun okuyacağı QR panelde HİÇ çizilmiyorsa
// tarayıcı işe yaramaz. fbui.DrawQR, QR alana sığmadığında BOŞ dikdörtgen ve
// nil hata döndürüyordu; pencere de bunu hata saymıyordu. Sonuç: küçük
// ekranlarda (VirtualBox'ın varsayılan 800x600 / 1024x768'i) QR'ın yeri boş
// kalıyor ve kullanıcıya hiçbir şey söylenmiyordu.
//
// Bu test gerçek bir uzaktan kontrol durumuyla (32 haneli jeton, tam parmak
// izi — QR'ı en büyük yapan girdi) pencereyi her yaygın çözünürlükte çizer ve
// QR'ın çizildiğini ya da çizilemiyorsa bunun AÇIKÇA yazıldığını doğrular.
// MCOS_SHOT_DIR verilirse ekran görüntüleri oraya yazılır; bağımsız bir
// çözücüyle (zxing) okutmak için.

func pairQRStatus() ipcclient.RemoteStatus {
	return ipcclient.RemoteStatus{
		Enabled: true, Running: true, Port: 2223,
		Token: "76d3d64f7febb116d27838771485dfd9",
		Fingerprint: "A6:79:F0:BA:BF:4C:F1:B5:7A:B5:58:B2:AC:B8:B7:8F:" +
			"7D:9B:9E:28:86:86:B6:D7:A3:16:02:D3:FF:4A:B2:17",
		Addresses: []string{"192.168.1.42", "10.0.2.15"},
	}
}

func qrShotApp(t *testing.T, w, h int) *App {
	t.Helper()
	f, err := fbfont.Load(AutoFontSize(h))
	if err != nil {
		t.Fatalf("font: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	a := New(fbui.NewUI(img, f, fbui.DefaultPalette), nil)
	a.SetHeadless(true)
	FillDemo(a)
	a.SetScreenSize(w, h)
	return a
}

// drawSettled draws until the open animation has finished.
func drawSettled(a *App) {
	for i := 0; i < 10; i++ {
		a.Draw()
		time.Sleep(40 * time.Millisecond)
	}
	a.Draw()
}

func TestPairQRHerCozunurlukteCiziliyor(t *testing.T) {
	st := pairQRStatus()
	dir := peersShotDir(t)
	for _, sz := range [][2]int{{800, 600}, {1024, 768}, {1280, 720}, {1280, 800}, {1920, 1080}} {
		w, h := sz[0], sz[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			a := qrShotApp(t, w, h)
			a.showPairQR(st)
			m, ok := a.modal.(*qrModal)
			if !ok {
				t.Fatalf("QR penceresi açılmadı (açık pencere: %T)", a.modal)
			}
			drawSettled(a)
			writePNG(t, filepath.Join(dir, fmt.Sprintf("qr-%dx%d.png", w, h)), a.ui.Canvas())

			if m.kutu.Empty() {
				// Çizilemediyse bunu SÖYLEMEK zorunda; boş bir kare,
				// kullanıcıya "QR yok" dedirten hatanın ta kendisi.
				if m.hata == "" {
					t.Fatalf("QR çizilmedi ve pencere hiçbir şey söylemiyor")
				}
				t.Fatalf("QR bu çözünürlükte çizilemedi: %s", m.hata)
			}
			// Telefon kameraları ekrandan modül başına ~4 pikselden küçüğünü
			// güvenilir okumuyor (1080p'de denendi, bkz. modal_qr.go).
			if px := m.kutu.Dx() / (m.modul + 2*fbui.QuietZone); px < 3 {
				t.Errorf("modül %d piksel — telefon okuyamaz", px)
			}
		})
	}
}

// QR'ın taşıdığı adres listesi: yerel ağ adresi İLK sırada olmalı, VirtualBox
// NAT adresi (10.0.2.x) telefondan ulaşılamaz — onu seçen bir QR, kullanıcıya
// "bağlanamıyorum" dedirtir.
func TestPairQRAdresSirasi(t *testing.T) {
	st := pairQRStatus()
	st.Addresses = []string{"10.0.2.15", "192.168.1.42"}
	hosts := pairHosts(st.Addresses)
	if len(hosts) == 0 || hosts[0] != "192.168.1.42" {
		t.Fatalf("yerel ağ adresi başta değil: %v", hosts)
	}
	uri := remote.PairURIHosts(hosts, st.Port, st.Token, st.Fingerprint)
	p, err := remote.ParsePair(uri)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Hosts, ",") != "192.168.1.42,10.0.2.15" {
		t.Fatalf("QR'daki adresler: %v", p.Hosts)
	}
}

// Yalnızca NAT adresi varsa pencere bunu açıkça söylemeli.
func TestPairQRYalnizcaNATUyarir(t *testing.T) {
	st := pairQRStatus()
	st.Addresses = []string{"10.0.2.15"}
	lines := strings.Join(pairQRLines(st, pairHosts(st.Addresses)), "\n")
	if !strings.Contains(lines, "Köprü") {
		t.Fatalf("VirtualBox NAT uyarısı yok:\n%s", lines)
	}
}
