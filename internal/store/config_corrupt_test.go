package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// BOZUK AYAR DOSYASI SİSTEMİ AÇILMAZ YAPMAMALI
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Kullanıcı MCOS'u VMware'de açtığında ekranda şu vardı:
//
//	mcosd: init failed: store: decode /data/config.json:
//	       unexpected end of JSON input
//	Hata: mcosd soketi hazırlanamadı: /run/mcos/mcosd.sock
//
// config.json sıfır bayt kalmıştı; mcosd açılmayı reddetti, soket hiç
// oluşmadı, panel açılamadı. Makine TAMAMEN kullanılamaz hâle geldi — hem de
// kaybedilen tek şey tercihlerdi.

func TestLoadConfigSurvivesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("sıfır baytlık ayar dosyası açılışı DURDURDU: %v — "+
			"kullanıcı kara ekranla kalır", err)
	}
	if cfg == nil {
		t.Fatal("yapılandırma nil döndü")
	}
	if !cfg.UI.Animations {
		t.Error("varsayılanlara dönülmedi (animasyonlar kapalı geldi)")
	}
}

func TestLoadConfigSurvivesTruncatedJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	// Yarım yazılmış bir dosya: elektrik kesintisinin tipik izi.
	if err := os.WriteFile(p, []byte(`{"setupComplete":true,"nodeId":"node_ab`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadConfig(p); err != nil {
		t.Fatalf("yarım ayar dosyası açılışı DURDURDU: %v", err)
	}
}

// Bozuk dosya SİLİNMEZ: içinde Wi-Fi parolası ve düğüm kimliği olabilir.
func TestCorruptConfigIsKeptAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte("{bozuk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadConfig(p); err != nil {
		t.Fatalf("açılış durdu: %v", err)
	}

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("bozuk dosya yerinde bırakıldı — her açılışta aynı hata döner")
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "config.json.bozuk-") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("bozuk dosya saklanmadı — kanıt ve kurtarma şansı kayboldu")
	}
	b, err := os.ReadFile(filepath.Join(dir, found))
	if err != nil || string(b) != "{bozuk" {
		t.Errorf("saklanan dosyanın içeriği değişmiş: %q (%v)", b, err)
	}
}

// Bozulma TEKRARLARSA önceki kanıt ezilmemeli.
func TestRepeatedCorruptionKeepsEveryCopy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")

	for i := 0; i < 2; i++ {
		if err := os.WriteFile(p, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(p); err != nil {
			t.Fatalf("%d. açılış durdu: %v", i+1, err)
		}
	}

	ents, _ := os.ReadDir(dir)
	n := 0
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "config.json.bozuk-") {
			n++
		}
	}
	// Aynı saniyede olursa ikinci taşıma birinciyi ezebilir; en az biri
	// durmalı ve HİÇBİR açılış başarısız olmamalı. Asıl iddia budur.
	if n < 1 {
		t.Error("hiçbir kopya saklanmadı")
	}
}

// Sağlam bir dosya elbette normal okunmalı: düzeltme veriyi ÇÖPE ATMAMALI.
func TestLoadConfigStillReadsGoodFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"nodeId":"node_test","setupComplete":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "node_test" || !cfg.SetupComplete {
		t.Errorf("sağlam dosya okunamadı: nodeId=%q setupComplete=%v",
			cfg.NodeID, cfg.SetupComplete)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("sağlam dosya kenara alındı — veri kaybı")
	}
}
