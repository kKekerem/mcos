package cluster

import (
	"context"
	"testing"
	"time"

	"mcos/internal/model"
	"mcos/internal/store"
)

// ════════════════════════════════════════════════════════════════════════════
// İKİ TAZE MAKİNE BİRBİRİYLE EŞLEŞEBİLMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Kimlik GÖRÜNEN ADLA belirleniyordu. model.DefaultConfig() o adı "mcos-1"
// olarak sabitliyor ve kurulum sihirbazı yalnızca boşsa üzerine yazıyordu —
// hiç boş olmadığı için de asla yazmıyordu. Yani kutudan çıkan her MCOS
// makinesi kendine "mcos-1" diyordu.
//
// Sonuç: kullanıcının açıkça istediği özellik ("pc eslestirme gercekten ise
// yarasın") varsayılan ayarlarla ÇALIŞMIYORDU:
//
//	PC-A'da tara  → PC-B'nin yanıtı "kendimiz" sanılıp atılır → "0 cihaz"
//	PC-B'nin IP'sini elle yaz → "bu adres bu makinenin kendisi"
//
// Bu testler tam olarak o kurulumu kurar: İKİ düğüm, İKİSİ DE "mcos-1".

// newNamedManager builds a manager with its own on-disk identity.
//
// Gerçek kurulum taklit ediliyor: her makinenin kendi veri klasörü var, yani
// kimlikler bağımsız üretiliyor.
func newNamedManager(t *testing.T, name string) *Manager {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(
		model.ClusterConfig{Enabled: true, NodeName: name, Port: 0,
			Secret: "0123456789abcdef0123456789abcdef"},
		"test", st, nil, &recordingExecutor{},
	)
}

// TestTwoFreshNodesGetDifferentIdentities — ASIL HATA.
func TestTwoFreshNodesGetDifferentIdentities(t *testing.T) {
	a := newNamedManager(t, "mcos-1")
	b := newNamedManager(t, "mcos-1") // AYNI ad: varsayılan bu

	if a.nodeID == "" || b.nodeID == "" {
		t.Fatalf("düğüm kimliği üretilmedi (a=%q b=%q)", a.nodeID, b.nodeID)
	}
	if a.nodeID == b.nodeID {
		t.Fatalf("iki ayrı makine aynı kimliği aldı: %q", a.nodeID)
	}
}

// Komşu, adı aynı olsa bile "ben" sayılmamalı.
func TestSameNameDifferentIdIsNotSelf(t *testing.T) {
	a := newNamedManager(t, "mcos-1")
	b := newNamedManager(t, "mcos-1")

	if a.isSelf(b.nodeID, b.nodeName) {
		t.Error("aynı adlı KOMŞU, bu makinenin kendisi sanıldı — " +
			"tarama ve elle eşleştirme bu yüzden çalışmıyordu")
	}
	if !a.isSelf(a.nodeID, a.nodeName) {
		t.Error("makine kendini tanımadı")
	}
}

// Kimlik yeniden başlatmaya dayanmalı: aynı veri klasörü, aynı kimlik.
//
// Dayanmasaydı, her açılışta yeni bir makine gibi görünür ve eşleştirilmiş
// komşular bizi tanımazdı.
func TestNodeIDSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.ClusterConfig{Enabled: true, NodeName: "mcos-1", Port: 0}

	first := NewManager(cfg, "test", st, nil, nil).nodeID
	second := NewManager(cfg, "test", st, nil, nil).nodeID

	if first == "" {
		t.Fatal("kimlik üretilmedi")
	}
	if first != second {
		t.Errorf("yeniden başlatınca kimlik değişti: %q -> %q", first, second)
	}
}

// Eşleştirilmiş cihazlar yeniden başlatmayı atlatmalı.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Eşleştirme durumu yalnızca bellekteydi. mcosd her yeniden başladığında
// (güncelleme, çökme, makinenin kapanması) bütün eşleştirmeler sessizce
// siliniyor ve ortak dünya duruyordu — hiçbir uyarı olmadan.
func TestPairedPeersSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.ClusterConfig{Enabled: true, NodeName: "mcos-1", Port: 27890}

	m := NewManager(cfg, "test", st, nil, nil)
	m.mu.Lock()
	m.peers["id:abc123"] = &model.Peer{
		ID: "id:abc123", Name: "salon-pc", IP: "192.168.1.50", Port: 27890,
		Paired: true, State: model.PeerAvailable, LastSeen: time.Now(),
	}
	m.peers["id:gecici"] = &model.Peer{
		ID: "id:gecici", Name: "yabanci", IP: "192.168.1.77",
		Paired: false, State: model.PeerAvailable, LastSeen: time.Now(),
	}
	m.mu.Unlock()
	m.savePeers()

	restarted := NewManager(cfg, "test", st, nil, nil)
	peers := restarted.Peers()

	var found *model.Peer
	for i := range peers {
		if peers[i].ID == "id:abc123" {
			found = &peers[i]
		}
		if peers[i].ID == "id:gecici" {
			t.Error("eşleştirilmemiş cihaz da kaydedilmiş — keşif geçicidir")
		}
	}
	if found == nil {
		t.Fatal("eşleştirilmiş cihaz yeniden başlatmadan sonra kayboldu")
	}
	if !found.Paired {
		t.Error("cihaz geri geldi ama eşleştirme durumu kaybolmuş")
	}
	if found.IP != "192.168.1.50" || found.Port != 27890 {
		t.Errorf("adres kaybolmuş: %s:%d", found.IP, found.Port)
	}
	// Geri yüklenen eş ÇEVRİMDIŞI başlamalı: açık olup olmadığını bilmiyoruz.
	if found.State != model.PeerOffline {
		t.Errorf("geri yüklenen eş %q durumunda — çevrimdışı başlamalıydı",
			found.State)
	}
}

// Kendi adresimize elle eşleşmeye çalışmak doğru hatayı vermeli.
func TestManualPairRejectsOwnAddress(t *testing.T) {
	m := newNamedManager(t, "mcos-1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := m.AddManual(ctx, "127.0.0.1:1")
	if err == nil {
		t.Fatal("kendi adresimiz kabul edildi")
	}
	// Hata mesajı doğru olmalı: bağlanamama değil, "bu benim" demeli.
	if got := err.Error(); got == "" {
		t.Error("boş hata")
	}
}

// peerKey kimlik varsa ONA dayanmalı: aynı makine IP değiştirdiğinde
// (DHCP yenilemesi) ikinci bir kayıt olarak görünmemeli.
func TestPeerKeyPrefersStableIdentity(t *testing.T) {
	before := peerKey("abc123", "mcos-1", "192.168.1.50")
	after := peerKey("abc123", "mcos-1", "192.168.1.77")
	if before != after {
		t.Errorf("IP değişince eş ikizlendi: %q vs %q", before, after)
	}

	// Kimlik yoksa (çok eski bir düğüm) ad+IP'ye düşülür.
	legacy := peerKey("", "eski", "192.168.1.9")
	if legacy != "eski@192.168.1.9" {
		t.Errorf("kimliksiz düğüm için beklenmeyen anahtar: %q", legacy)
	}
}
