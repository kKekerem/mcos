package store

import (
	"os"
	"testing"
)

// ── Eksik yapılandırma dosyası: varsayılanlar korunmalı ─────────────────────
//
// MCOS ilk açılışta BİLEREK eksik bir dosya yazar (etc/init.d/S03mcosdata),
// küme kimliği sihirbazdan önce de sabit olsun diye:
//
//	{"setupComplete":false,"nodeId":"node_…"}
//
// Bu testler o dosyanın varsayılanları SIFIRLAMADIĞINI koruyor. Eskiden
// sıfırlıyordu; kullanıcıya görünen sonucu şuydu: açılış ekranından panele
// geçişte yakınlaşma efekti hiç oynamıyor, fare ve touchpad hiç çalışmıyordu.

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := t.TempDir() + "/config.json"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPartialConfigKeepsUIDefaults(t *testing.T) {
	p := writeTempConfig(t, `{"setupComplete":false,"nodeId":"node_abc"}`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UI.Animations {
		t.Error("animations: eksik dosya animasyonları kapatmamalı; " +
			"açılış yakınlaşması ve tüm ekran geçişleri buna bağlı")
	}
	if !cfg.UI.BootAnimation {
		t.Error("bootAnimation: eksik dosya açılış ekranını kapatmamalı")
	}
	if !cfg.UI.Mouse {
		t.Error("mouse: eksik dosya fareyi kapatmamalı")
	}
	if !cfg.UI.Touchpad {
		t.Error("touchpad: eksik dosya touchpad'i kapatmamalı")
	}
	if cfg.UI.PointerSpeed != 100 {
		t.Errorf("pointerSpeed = %d, 100 bekleniyordu", cfg.UI.PointerSpeed)
	}
	if cfg.Theme == "" {
		t.Error("theme: eksik dosya temayı boşaltmamalı")
	}
	if cfg.Tier.Mode == "" {
		t.Error("tier.mode: eksik dosya katman kipini boşaltmamalı")
	}
}

// Dosyada YAZILI olan değer varsayılanı EZMELİ — yoksa kullanıcı bir ayarı
// kapatamaz. Düzeltmenin ters yönde kaza yapmadığını gösterir.
func TestExplicitFalseOverridesDefault(t *testing.T) {
	p := writeTempConfig(t, `{"ui":{"animations":false,"mouse":false}}`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Animations {
		t.Error("animations: dosyada false yazıyor, açık kalmamalı")
	}
	if cfg.UI.Mouse {
		t.Error("mouse: dosyada false yazıyor, açık kalmamalı")
	}
	if !cfg.UI.Touchpad {
		t.Error("touchpad: dosyada geçmiyor, varsayılanı (açık) korumalı")
	}
}

// Aralık dışı bir değer OKUMA sırasında düzeltilmeli; her çağıranın ayrıca
// Normalize çağırmasına güvenmek bir gün unutulur.
func TestLoadNormalizesPointerSpeed(t *testing.T) {
	p := writeTempConfig(t, `{"ui":{"pointerSpeed":0}}`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.PointerSpeed != 100 {
		t.Errorf("pointerSpeed = %d; sıfır hız imleci dondururdu",
			cfg.UI.PointerSpeed)
	}
}

// Dosya HİÇ yoksa tam varsayılan dönmeli (eski davranış korunuyor).
func TestMissingConfigReturnsDefaults(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir() + "/yok.json")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UI.Animations || cfg.Theme == "" {
		t.Fatal("eksik dosyada tam varsayılan beklenir")
	}
}

// Küme portu gibi sayısal alanlar da sıfırlanmamalı: sıfır port, eşleştirme
// sunucusunu rastgele bir porta bağlardı ve hiçbir eş onu bulamazdı.
func TestPartialConfigKeepsClusterDefaults(t *testing.T) {
	p := writeTempConfig(t, `{"setupComplete":true}`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster.Port == 0 {
		t.Error("cluster.port: eksik dosya eşleştirme portunu sıfırlamamalı")
	}
	if cfg.Cluster.NodeName == "" {
		t.Error("cluster.nodeName: eksik dosya düğüm adını boşaltmamalı")
	}
}
