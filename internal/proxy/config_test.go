package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// velocity.toml: sözleşmenin proxy tarafı (modern yönlendirme, BungeeCord
// kanalı, arka uçlar, try) ve Velocity 4 / 3 biçim farkı.
func TestRenderVelocityToml(t *testing.T) {
	c := Config{
		Bind:       "0.0.0.0:25565",
		OnlineMode: true,
		SecretFile: "/data/mcos/proxy.secret",
		MOTD:       "Benim <b>Dünyam</b>",
		MaxPlayers: 40,
		Backends: []Backend{
			{Name: "mcos-1", Addr: "127.0.0.1:25580"},
			{Name: "pc-b", Addr: "192.168.1.20:25565"},
		},
		Try: []string{"mcos-1"},
	}
	got := Render(c)
	for _, want := range []string{
		`config-version = "2.9"`,
		`bind = "0.0.0.0:25565"`,
		`online-mode = true`,
		`player-info-forwarding-mode = "MODERN"`,
		`forwarding-secret-file = "/data/mcos/proxy.secret"`,
		`motd = "Benim bDünyam/b"`,
		`show-max-players = 40`,
		"[servers]\n\"mcos-1\" = \"127.0.0.1:25580\"\n\"pc-b\" = \"192.168.1.20:25565\"\ntry = [\"mcos-1\"]",
		"[forced-hosts]\n",
		`bungee-plugin-message-channel = true`,
		"[ping-passthrough]\ndescription = true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("velocity.toml içinde yok: %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, `ping-passthrough = "`) {
		t.Errorf("Velocity 4 biçiminde dizge ping-passthrough olmamalı:\n%s", got)
	}

	c.Legacy, c.OnlineMode = true, false
	old := Render(c)
	for _, want := range []string{`config-version = "2.8"`, `ping-passthrough = "DESCRIPTION"`,
		`online-mode = false`, `force-key-authentication = false`} {
		if !strings.Contains(old, want) {
			t.Errorf("Velocity 3 biçiminde yok: %q", want)
		}
	}
	if strings.Contains(old, "[ping-passthrough]") {
		t.Errorf("Velocity 3 biçiminde tablo ping-passthrough olmamalı")
	}
}

func TestLoadOrCreateSecretIsStable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcos", "proxy.secret")
	a, err := LoadOrCreateSecret(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || strings.Trim(a, secretAlphabet) != "" {
		t.Fatalf("anahtar 32 harf/rakam olmalı: %q", a)
	}
	b, _ := LoadOrCreateSecret(p)
	if a != b {
		t.Fatalf("anahtar her çağrıda değişmemeli: %q != %q", a, b)
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != a {
		t.Fatalf("dosyada satır sonu olmamalı: %q", raw)
	}
}

func TestNewerVersion(t *testing.T) {
	if !newerVersion("4.10.0", "4.9.1") || newerVersion("3.5.1", "4.2.0") {
		t.Fatal("sürümler sayısal karşılaştırılmalı")
	}
}
