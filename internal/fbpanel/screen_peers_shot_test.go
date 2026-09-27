package fbpanel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcos/internal/model"
)

// Eşleme ekranı GERÇEKÇİ durumlarla çizilir: sağlıklı eş, yanlış anahtar,
// güvenlik duvarı, eşleşmemiş cihaz ve açık bir ortak dünya. Sorun metinleri
// uçtan uca sınamada daemon'un GERÇEKTEN ürettiği cümlelerdir.
//
// MCOS_SHOT_DIR verilirse PNG'ler oraya yazılır (gözle bakmak için).
func peersShotApp(t *testing.T) *App {
	a, _ := newTestApp(t)
	a.mu.Lock()
	a.clusterID = peersIdentity{Secret: "4fedad6a273de8bca20be8b304ba4723",
		NodeName: "mcos-kutu", Port: 2222, Address: "192.168.1.42:2222", Enabled: true}
	now := time.Now()
	a.peers = []model.Peer{
		{ID: "id:d855", Name: "ikinci-pc", IP: "192.168.1.57", Port: 2222, Cores: 12,
			RAMMB: 15879, State: model.PeerAvailable, Paired: true, Version: "1.0.1", LastSeen: now},
		{ID: "id:a1b2", Name: "oyun-pc", IP: "192.168.1.63", Port: 2222, Cores: 8,
			RAMMB: 16384, State: model.PeerAvailable, Paired: true,
			Problem: "ortak dünya kurulamadı: anahtar yanlış — o cihaza bu MCOS'un eşleştirme anahtarını girin"},
		{ID: "id:c3d4", Name: "eski-laptop", IP: "192.168.1.70", Port: 2222, Cores: 4,
			RAMMB: 8192, State: model.PeerOffline, Paired: true,
			Problem: "yanıt yok — güvenlik duvarı 2222 portunu engelliyor olabilir"},
		{ID: "id:e5f6", Name: "salon-pc", IP: "192.168.1.81", Port: 2222, Cores: 6,
			RAMMB: 12288, State: model.PeerAvailable, Version: "1.0.1"},
	}
	a.link = model.LinkStatus{
		Mode: model.LinkSharedWorld, ServerName: "Ortak Dunya",
		Difficulty: model.DifficultyHard, SlabChunks: 32, ModInstalled: true, Handoffs: 3,
		Territories: model.Territories([]string{"ikinci-pc", "mcos-kutu"}, 32),
		Nodes: []model.LinkNode{
			{Name: "ikinci-pc", Host: "192.168.1.57", MCPort: 25565, Online: true, Players: 2},
			{Name: "mcos-kutu", Host: "192.168.1.42", MCPort: 25565, Online: true, Self: true, Players: 1},
		},
	}
	a.dirty = true
	a.mu.Unlock()
	a.gotoSection(SecPeers)
	a.setFocus(FocusContent)
	return a
}

func peersShotDir(t *testing.T) string {
	if d := os.Getenv("MCOS_SHOT_DIR"); d != "" {
		return d
	}
	return t.TempDir()
}

func TestPeersScreenShot(t *testing.T) {
	a := peersShotApp(t)
	a.Draw()
	img := a.ui.Canvas()
	if ink := countInk(img); ink < 20000 {
		t.Fatalf("eşleme ekranı neredeyse boş (%d piksel)", ink)
	}
	writePNG(t, filepath.Join(peersShotDir(t), "esleme-ekrani.png"), img)
}

// İmleç sorunlu cihazdayken nedenin TAMAMI görünmeli (satırda kırpılıyor).
func TestPeersProblemDetailShot(t *testing.T) {
	a := peersShotApp(t)
	// Satırlar: tara, IP gir, ikinci-pc, oyun-pc, …
	a.SetCursor(3)
	a.Draw()
	writePNG(t, filepath.Join(peersShotDir(t), "esleme-sorun-ayrinti.png"), a.ui.Canvas())
}

// Başarısız elle eşleştirmenin penceresi: NEDEN + YAPILACAK ŞEY.
func TestPairFailureShot(t *testing.T) {
	a := peersShotApp(t)
	err := errors.New("10.77.0.2: yanıt yok — güvenlik duvarı 2222 portunu engelliyor " +
		"olabilir (Windows'ta bir kez \"mcos-node --kur\" çalıştırın)")
	lines := pairFailureLines("10.77.0.2", err)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "kur.bat") {
		t.Error("var olmayan kur.bat'a yönlendiriliyor")
	}
	if !strings.Contains(joined, "--kur") {
		t.Errorf("güvenlik duvarı için yapılacak şey yok:\n%s", joined)
	}
	a.OpenModal(NewInfoModal("Eşleşme kurulamadı", lines))
	// Açılış animasyonu gerçek saatle ilerliyor: bitmeden alınan kare
	// geçiş ortasındaki bulanık hâli gösterir.
	for i := 0; i < 10; i++ {
		a.Draw()
		time.Sleep(60 * time.Millisecond)
	}
	a.Draw()
	writePNG(t, filepath.Join(peersShotDir(t), "esleme-hata.png"), a.ui.Canvas())
}
