package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// Paper'ın kendi ürettiği bölüm (1.20+). bungee-cord altındaki online-mode'a
// DOKUNULMAMALI; yorumlar ve diğer anahtarlar yerinde kalmalı.
const paperGlobalSample = `_version: 29
# Paper'ın yorumu
chunk-loading-basic:
  autoconfig-send-distance: true
proxies:
  bungee-cord:
    online-mode: true
  proxy-protocol: false
  velocity:
    enabled: false
    online-mode: true
    secret: ''
scoreboards:
  save-empty-scoreboard-teams: true
`

func TestSetPaperVelocityUpdatesOnlyVelocity(t *testing.T) {
	got := SetPaperVelocity(paperGlobalSample, true, false, "abc123")
	want := strings.Replace(paperGlobalSample, `  velocity:
    enabled: false
    online-mode: true
    secret: ''`, `  velocity:
    enabled: true
    online-mode: false
    secret: 'abc123'`, 1)
	if got != want {
		t.Fatalf("yalnızca velocity bölümü değişmeli:\n%s", got)
	}
	// Geri alma aynı yerden.
	back := SetPaperVelocity(got, false, true, "")
	if back != paperGlobalSample {
		t.Fatalf("geri alma özgün dosyayı vermeli:\n%s", back)
	}
}

func TestSetPaperVelocityMissingParts(t *testing.T) {
	// Dosya yok: en küçük geçerli dosya.
	if got := SetPaperVelocity("", true, true, "k"); got != "proxies:\n  velocity:\n    enabled: true\n    online-mode: true\n    secret: 'k'\n" {
		t.Fatalf("boş dosya: %q", got)
	}
	// proxies var, velocity yok: proxies'in altına, aynı girintiyle.
	in := "proxies:\n  proxy-protocol: false\nother: 1\n"
	got := SetPaperVelocity(in, true, true, "k")
	want := "proxies:\n  velocity:\n    enabled: true\n    online-mode: true\n    secret: 'k'\n  proxy-protocol: false\nother: 1\n"
	if got != want {
		t.Fatalf("velocity eklenmeli:\n%s", got)
	}
	// velocity var ama secret eksik.
	in = "proxies:\n  velocity:\n    enabled: false\nx: 1\n"
	got = SetPaperVelocity(in, true, true, "k")
	if !strings.Contains(got, "    enabled: true\n") || !strings.Contains(got, "    secret: 'k'\n") ||
		!strings.HasSuffix(got, "x: 1\n") {
		t.Fatalf("eksik anahtar eklenmeli:\n%s", got)
	}
}

// Proxy açılıp kapanınca sunucu dosyaları eski hâline dönmeli; aksi hâlde
// sunucu velocity kipinde kalır ve doğrudan kimse giremez.
func TestWriteProxyBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("online-mode=true\nserver-port=25565\nmotd=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(dir, "mods")
	_ = os.MkdirAll(mods, 0o755)
	_ = os.WriteFile(filepath.Join(mods, "FabricProxy-Lite-2.10.1.jar"), []byte("x"), 0o644)

	srv := &model.Server{Software: model.SoftwareFabric, Port: 25580, OnlineMode: true,
		Link: model.LinkConfig{Mode: model.LinkSharedWorld,
			Proxy: &model.LinkProxy{Secret: "abc", PublicPort: 25565, OnlineMode: true}}}
	if err := WriteProxyBackend(dir, srv); err != nil {
		t.Fatal(err)
	}
	props, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(props), "online-mode=false") || !strings.Contains(string(props), "server-port=25580") {
		t.Fatalf("arka uç: online-mode=false ve iç port olmalı:\n%s", props)
	}
	toml, _ := os.ReadFile(filepath.Join(dir, "config", FabricProxyConfig))
	if !strings.Contains(string(toml), `secret = "abc"`) || !strings.Contains(string(toml), "hackOnlineMode = true") {
		t.Fatalf("FabricProxy-Lite ayarı yanlış:\n%s", toml)
	}
	// Proxy açıkken kural dosyadan okunmamalı (dosya false diyor).
	if r := ReadLinkRules(dir, srv); !r.OnlineMode {
		t.Fatal("gerçek online-mode kuralı kaybolmamalı")
	}

	// Kapat: port geri, kural geri, mod ve ayar kalkar.
	srv.Link.Proxy = nil
	srv.Link.Mode = model.LinkOff
	srv.Port = 25565
	if r := ReadLinkRules(dir, srv); !r.OnlineMode {
		t.Fatal("geri alınmadan önce de gerçek kural işaret dosyasından gelmeli")
	}
	if err := WriteProxyBackend(dir, srv); err != nil {
		t.Fatal(err)
	}
	props, _ = os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(props), "online-mode=true") || !strings.Contains(string(props), "server-port=25565") {
		t.Fatalf("geri alma: online-mode ve port eski hâline dönmeli:\n%s", props)
	}
	if HasFabricProxy(mods) {
		t.Fatal("FabricProxy-Lite proxy kapanınca kalkmalı")
	}
	if _, ok := ProxyMarkerOnlineMode(dir); ok {
		t.Fatal("işaret dosyası silinmeli")
	}
	// İşaret yokken kullanıcının ayarına dokunulmaz.
	_ = os.WriteFile(filepath.Join(dir, "server.properties"), []byte("online-mode=false\n"), 0o644)
	if err := WriteProxyBackend(dir, srv); err != nil {
		t.Fatal(err)
	}
	props, _ = os.ReadFile(filepath.Join(dir, "server.properties"))
	if string(props) != "online-mode=false\n" {
		t.Fatalf("işaret yokken dosya değişmemeli: %q", props)
	}
}
