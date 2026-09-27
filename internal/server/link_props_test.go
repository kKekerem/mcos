package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// Kurulumdan SONRA ortak dünya yapılan bir sunucuda tohum/zorluk diske
// ulaşmalı. Uçtan uca sınamada kurucu MCOS'ta "level-seed=" boş ve
// "difficulty=easy" kalmıştı; düğüm 424242/hard açıldı — iki ayrı dünya.
func TestWriteLinkPropertiesAfterInstall(t *testing.T) {
	dir := t.TempDir()
	// Kurulumun bıraktığı hâl: tohum boş, zorluk easy, kullanıcı ayarı var.
	os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("motd=Benim sunucum\ndifficulty=easy\nlevel-seed=\nview-distance=7\n"), 0o644)

	srv := &model.Server{Difficulty: "hard", LevelSeed: "424242",
		Link: model.LinkConfig{Mode: model.LinkSharedWorld}}
	if err := WriteLinkProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	got := string(b)
	for _, want := range []string{"level-seed=424242", "difficulty=hard", "accepts-transfers=true",
		"motd=Benim sunucum", "view-distance=7"} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("%q yok:\n%s", want, got)
		}
	}
	if strings.Contains(got, "difficulty=easy") {
		t.Error("eski zorluk kaldı")
	}
}

// KARŞI-SINAMA: ortak dünya OLMAYAN sunucuya dokunulmaz.
func TestWriteLinkPropertiesIgnoresOrdinaryServer(t *testing.T) {
	dir := t.TempDir()
	orig := "difficulty=easy\nlevel-seed=\n"
	os.WriteFile(filepath.Join(dir, "server.properties"), []byte(orig), 0o644)
	srv := &model.Server{Difficulty: "hard", LevelSeed: "1"}
	if err := WriteLinkProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if string(b) != orig {
		t.Errorf("sıradan sunucunun ayarları değişti:\n%s", b)
	}
}

// Eş kopyası kurucunun kurallarını her açılışta yazmalı. İki sanal makineli
// sınamada kurucu online-mode=false idi, eşin kopyası true doğdu ve aktarılan
// oyuncu "unverified_username" ile atıldı.
func TestWriteLinkPropertiesPeerRules(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("online-mode=true\npvp=true\ngamemode=survival\nmotd=x\n"), 0o644)
	srv := &model.Server{Link: model.LinkConfig{Mode: model.LinkSharedWorld, OriginID: "kurucu",
		Rules: &model.LinkRules{OnlineMode: false, PVP: false, Gamemode: "creative", MaxPlayers: 300}}}
	if err := WriteLinkProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	got := string(b)
	for _, want := range []string{"online-mode=false", "pvp=false", "gamemode=creative",
		"max-players=300", "hardcore=false", "motd=x"} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("%q yok:\n%s", want, got)
		}
	}
}

// KARŞI-SINAMA: kurucuda (Rules nil) kullanıcının elle ayarı EZİLMEZ.
func TestWriteLinkPropertiesOriginKeepsOwnRules(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server.properties"), []byte("online-mode=false\npvp=false\n"), 0o644)
	srv := &model.Server{OnlineMode: true, PVP: true, Link: model.LinkConfig{Mode: model.LinkSharedWorld}}
	if err := WriteLinkProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(b), "online-mode=false\n") || !strings.Contains(string(b), "pvp=false\n") {
		t.Errorf("kurucunun kendi ayarı ezildi:\n%s", b)
	}
}

// Kurucunun kuralları dosyadan okunur (dosya kayıttan sonra elle değişmiş
// olabilir); dosya yoksa kayıttaki değer kullanılır.
func TestReadLinkRules(t *testing.T) {
	dir := t.TempDir()
	srv := &model.Server{OnlineMode: true, PVP: true, Gamemode: "survival", MaxPlayers: 20}
	r := ReadLinkRules(dir, srv)
	if !r.OnlineMode || !r.PVP || r.Gamemode != "survival" || r.MaxPlayers != 20 {
		t.Fatalf("dosyasız okuma kayıttan gelmedi: %+v", r)
	}
	os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("# yorum\nonline-mode=false\npvp=false\ngamemode=adventure\nhardcore=true\nmax-players=2000\n"), 0o644)
	r = ReadLinkRules(dir, srv)
	want := model.LinkRules{OnlineMode: false, PVP: false, Gamemode: "adventure", Hardcore: true, MaxPlayers: 2000}
	if *r != want {
		t.Fatalf("dosyadan okunan %+v, beklenen %+v", *r, want)
	}
}
