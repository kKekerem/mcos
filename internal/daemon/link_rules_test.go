package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"mcos/internal/model"
)

// Kurucunun LinkSpec'i oyun kurallarını KENDİ server.properties'inden taşımalı
// (iki VM'li sınamada online-mode uyuşmazlığı aktarılan oyuncuyu attırdı).
func TestLinkSpecCarriesOriginRules(t *testing.T) {
	d := newRemoteTestDaemon(t)
	dir := t.TempDir()
	// Kayıt "online" diyor ama kullanıcı dosyada kapatmış: dosya kazanır.
	os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("online-mode=false\npvp=true\ngamemode=survival\nmax-players=150\n"), 0o644)
	srv := &model.Server{ID: "k1", Name: "Ortak", Software: model.SoftwarePaper, MCVersion: "1.21.11",
		DataDir: dir, OnlineMode: true, PVP: true, Link: model.LinkConfig{Mode: model.LinkSharedWorld}}
	if err := d.store.SaveServer(srv); err != nil {
		t.Fatal(err)
	}
	spec, ok := d.LinkSpec()
	if !ok || spec.Rules == nil {
		t.Fatalf("kural taşınmadı: ok=%v %+v", ok, spec.Rules)
	}
	if spec.Rules.OnlineMode || !spec.Rules.PVP || spec.Rules.MaxPlayers != 150 {
		t.Fatalf("kurallar dosyadan okunmadı: %+v", *spec.Rules)
	}
}

// Eş MCOS'ta: gelen kurallar kopyaya geçmeli ve saklanmalı.
func TestApplyLinkSpecCarriesRulesOnMCOS(t *testing.T) {
	d := newRemoteTestDaemon(t)
	spec := model.LinkSpec{Mode: model.LinkSharedWorld, ServerName: "Ortak", Software: "paper",
		MCVersion: "1.21.11", Seed: "7", Difficulty: "normal", RAMMB: 1024, Port: 25565,
		Origin: "kurucu", OriginID: "kurucu-id",
		Rules: &model.LinkRules{OnlineMode: false, PVP: true, Gamemode: "survival", MaxPlayers: 40}}
	if _, _, err := d.ApplyLinkSpec(spec); err != nil {
		t.Fatal(err)
	}
	list, _ := d.store.ListServers()
	if len(list) != 1 {
		t.Fatalf("%d sunucu", len(list))
	}
	if list[0].OnlineMode || list[0].MaxPlayers != 40 || !list[0].Link.Rules.Equal(spec.Rules) {
		t.Fatalf("kurallar kopyaya geçmedi: online=%v oyuncu=%d kural=%+v",
			list[0].OnlineMode, list[0].MaxPlayers, list[0].Link.Rules)
	}
}
