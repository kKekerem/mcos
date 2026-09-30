package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// VELOCITY ARKA UCU — sunucuyu MCOS proxy'sinin arkasına koymak
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "DonutSMP gibi yap, tek bir IP'den çıkış versin
// hepsi, proxy olsun onları yöneten." DonutSMP'nin kurgusu: Paper
// sunucuları bir Velocity proxy'sinin arkasında durur, oyuncu yalnızca
// proxy'ye bağlanır ve sunucular arası geçiş bağlantı kopmadan olur.
//
// Bunun için her arka uç sunucu:
//   - server.properties'te online-mode=false olmalı (hesabı PROXY doğrular;
//     arka uç Mojang'a ikinci kez sorarsa oyuncu atılır),
//   - Velocity'nin "modern" yönlendirmesini açmalı (gerçek UUID/skin/IP
//     proxy'den imzalı gelir; imzasız bağlantı reddedilir — online-mode
//     kapalıyken doğrudan iç porta bağlanıp sahte ad kullanmanın önündeki
//     TEK engel budur):
//       · Paper/Purpur/Folia: config/paper-global.yml → proxies.velocity,
//       · Fabric: FabricProxy-Lite modu + config/FabricProxy-Lite.toml.
//
// Her açılışta yazılır (WriteLinkProperties gibi): kurulum sonradan
// değişebilir, eşten gelen anahtar yenilenebilir.
//
// Proxy kapanınca HER ŞEY GERİ ALINMALI: aksi halde sunucu velocity kipinde
// kalır ve doğrudan bağlanan hiçbir oyuncu giremez. Geri almanın neyi geri
// alacağını bilmesi için ayarlandığı an bir işaret dosyası (proxyMarker)
// bırakılır; işaret yoksa kullanıcının kendi ayarlarına dokunulmaz.

// proxyMarker, arka uç ayarlarının MCOS tarafından yapıldığını ve gerçek
// online-mode değerini saklar.
const proxyMarker = ".mcos-proxy.json"

// FabricProxyConfig, FabricProxy-Lite'ın ayar dosyasıdır (config/ altında).
const FabricProxyConfig = "FabricProxy-Lite.toml"

type proxyMarkerData struct {
	OnlineMode bool `json:"onlineMode"`
}

// ProxyMarkerOnlineMode returns the real online-mode saved while the server
// was set up as a proxy backend (ok=false: işaret yok).
func ProxyMarkerOnlineMode(dir string) (online, ok bool) {
	b, err := os.ReadFile(filepath.Join(dir, proxyMarker))
	if err != nil {
		return false, false
	}
	var m proxyMarkerData
	if json.Unmarshal(b, &m) != nil {
		return false, false
	}
	return m.OnlineMode, true
}

// WriteProxyBackend applies (or undoes) the Velocity backend setup.
//
// Manager.Start her açılışta WriteLinkProperties'ten SONRA çağırır: eşin
// kopyasında WriteLinkProperties kurucunun online-mode kuralını yazar, bu
// ise proxy açıkken onu false'a çeker.
func WriteProxyBackend(dir string, srv *model.Server) error {
	if srv.Link.BehindProxy() && srv.Software.VelocityBackend() {
		px := srv.Link.Proxy
		if err := setProperty(dir, "online-mode", "false"); err != nil {
			return err
		}
		// Kurucuda port İÇ porta taşındı (proxy genel portu aldı); kurulumda
		// bir kez yazılan değere güvenilirse sunucu proxy'nin portunda
		// açılmaya çalışır ve düşerdi.
		if srv.Port > 0 {
			if err := setProperty(dir, "server-port", strconv.Itoa(srv.Port)); err != nil {
				return err
			}
		}
		if srv.Software == model.SoftwareFabric {
			if err := writeFabricProxyConfig(dir, px.OnlineMode, px.Secret); err != nil {
				return err
			}
		} else if err := writePaperGlobal(dir, true, px.OnlineMode, px.Secret); err != nil {
			return err
		}
		b, _ := json.Marshal(proxyMarkerData{OnlineMode: px.OnlineMode})
		return os.WriteFile(filepath.Join(dir, proxyMarker), b, 0o644)
	}

	online, ok := ProxyMarkerOnlineMode(dir)
	if !ok {
		return nil // MCOS hiç proxy arka ucu yapmadı: kullanıcının ayarları
	}
	if err := setProperty(dir, "online-mode", strconv.FormatBool(online)); err != nil {
		return err
	}
	if srv.Port > 0 {
		if err := setProperty(dir, "server-port", strconv.Itoa(srv.Port)); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "paper-global.yml")); err == nil {
		if err := writePaperGlobal(dir, false, true, ""); err != nil {
			return err
		}
	}
	// FabricProxy-Lite proxy'siz bağlantıyı REDDEDER: mod kalırsa kimse
	// giremez. Jar ve ayarı birlikte kalkar.
	RemoveFabricProxy(filepath.Join(dir, "mods"))
	_ = os.Remove(filepath.Join(dir, "config", FabricProxyConfig))
	return os.Remove(filepath.Join(dir, proxyMarker))
}

// HasFabricProxy reports whether mods/ already has FabricProxy-Lite.
func HasFabricProxy(modsDir string) bool {
	return len(fabricProxyJars(modsDir)) > 0
}

// RemoveFabricProxy deletes FabricProxy-Lite jars from mods/.
func RemoveFabricProxy(modsDir string) {
	for _, p := range fabricProxyJars(modsDir) {
		_ = os.Remove(p)
	}
}

func fabricProxyJars(modsDir string) []string {
	ents, err := os.ReadDir(modsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := strings.ToLower(e.Name())
		if !e.IsDir() && strings.HasSuffix(n, ".jar") && strings.Contains(n, "fabricproxy-lite") {
			out = append(out, filepath.Join(modsDir, e.Name()))
		}
	}
	return out
}

// writeFabricProxyConfig writes config/FabricProxy-Lite.toml (tamamı bizim).
func writeFabricProxyConfig(dir string, online bool, secret string) error {
	cfg := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg, FabricProxyConfig), []byte(FabricProxyTOML(online, secret)), 0o644)
}

// FabricProxyTOML renders FabricProxy-Lite's settings.
//
// hackOnlineMode: proxy online-mode'daysa gerçek (Mojang) UUID'ler kullanılır;
// kapalıysa proxy'nin çevrimdışı UUID'leri. Kural her düğümde aynı olmalı,
// yoksa aynı oyuncu iki yarıda iki ayrı kişi olur (envanteri kaybolur).
func FabricProxyTOML(online bool, secret string) string {
	return fmt.Sprintf(`# MCOS her açılışta yazar: bu sunucu MCOS Velocity proxy'sinin arka ucudur.
hackOnlineMode = %t
hackEarlySend = false
hackMessageChain = true
disconnectMessage = "Bu sunucuya yalnızca MCOS proxy adresinden bağlanılır."
secret = %q
`, online, secret)
}

// writePaperGlobal updates config/paper-global.yml's proxies.velocity.
func writePaperGlobal(dir string, enabled, online bool, secret string) error {
	cfg := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return err
	}
	path := filepath.Join(cfg, "paper-global.yml")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next := SetPaperVelocity(string(old), enabled, online, secret)
	if next == string(old) {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o644)
}

// SetPaperVelocity sets proxies.velocity.{enabled,online-mode,secret} in a
// paper-global.yml text and leaves every other line as it was.
//
// NEDEN SATIR SATIR: dosya Paper'ın kendi yorumlarını ve kullanıcının
// ayarlarını taşıyor; bir YAML kütüphanesiyle okuyup yazmak yorumları siler
// ve sırayı bozardı. Yalnızca "proxies:" → "velocity:" bloğundaki üç
// anahtar değişir; aynı adlı anahtarlar başka bloklarda da var
// (proxies.bungee-cord.online-mode) ve onlara dokunulmamalı.
//
// Dosya yoksa (sunucu hiç açılmamış) en küçük geçerli dosya yazılır; Paper
// "_version" yoksa en güncel sürümü varsayar ve eksikleri kendisi doldurur.
func SetPaperVelocity(content string, enabled, online bool, secret string) string {
	crlf := strings.Contains(content, "\r\n")
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	vals := [][2]string{
		{"enabled", strconv.FormatBool(enabled)},
		{"online-mode", strconv.FormatBool(online)},
		{"secret", "'" + strings.ReplaceAll(secret, "'", "''") + "'"},
	}
	block := func(indent int) []string {
		out := []string{strings.Repeat(" ", indent) + "velocity:"}
		for _, kv := range vals {
			out = append(out, strings.Repeat(" ", indent+2)+kv[0]+": "+kv[1])
		}
		return out
	}

	pi := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == "proxies:" {
			pi = i
			break
		}
	}
	if pi < 0 {
		lines = append(lines, "proxies:")
		lines = append(lines, block(2)...)
		return joinLines(lines, crlf)
	}

	// proxies bloğunun sonu: girintisiz ilk dolu satır.
	pend := blockEnd(lines, pi, 0)
	vi, vind := -1, 2
	for j := pi + 1; j < pend; j++ {
		if strings.TrimSpace(lines[j]) == "velocity:" {
			vi, vind = j, indentOf(lines[j])
			break
		}
	}
	if vi < 0 {
		// velocity yok: proxies'in ilk çocuğunun girintisiyle eklenir.
		for j := pi + 1; j < pend; j++ {
			if isContent(lines[j]) {
				vind = indentOf(lines[j])
				break
			}
		}
		ins := block(vind)
		lines = append(lines[:pi+1], append(ins, lines[pi+1:]...)...)
		return joinLines(lines, crlf)
	}

	vend := blockEnd(lines, vi, vind)
	kind := vind + 2
	for j := vi + 1; j < vend; j++ {
		if isContent(lines[j]) {
			kind = indentOf(lines[j])
			break
		}
	}
	var missing []string
	for _, kv := range vals {
		found := false
		for j := vi + 1; j < vend; j++ {
			l := lines[j]
			if indentOf(l) == kind && strings.HasPrefix(strings.TrimSpace(l), kv[0]+":") {
				lines[j] = strings.Repeat(" ", kind) + kv[0] + ": " + kv[1]
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, strings.Repeat(" ", kind)+kv[0]+": "+kv[1])
		}
	}
	if len(missing) > 0 {
		lines = append(lines[:vi+1], append(missing, lines[vi+1:]...)...)
	}
	return joinLines(lines, crlf)
}

// blockEnd returns the index of the first content line after start whose
// indent is <= indent (bloğun bittiği yer).
func blockEnd(lines []string, start, indent int) int {
	for j := start + 1; j < len(lines); j++ {
		if isContent(lines[j]) && indentOf(lines[j]) <= indent {
			return j
		}
	}
	return len(lines)
}

func isContent(l string) bool {
	t := strings.TrimSpace(l)
	return t != "" && !strings.HasPrefix(t, "#")
}

func indentOf(l string) int {
	return len(l) - len(strings.TrimLeft(l, " "))
}

func joinLines(lines []string, crlf bool) string {
	sep := "\n"
	if crlf {
		sep = "\r\n"
	}
	return strings.Join(lines, sep) + sep
}
