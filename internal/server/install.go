package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"mcos/internal/java"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mcos/internal/model"
	"mcos/internal/server/providers"
)

// launchInfo is persisted per server so Start knows how to launch it without
// re-running the provider. Stored as <serverData>/.mcos-launch.json.
type launchInfo struct {
	providers.InstallResult
	Installed bool `json:"installed"`
}

const launchFile = ".mcos-launch.json"

func (m *Manager) launchPath(id string) string {
	return filepath.Join(m.store.Paths.ServerData(id), launchFile)
}

// IsInstalled reports whether the server's jar/args have been prepared.
func (m *Manager) IsInstalled(srv *model.Server) bool {
	li, err := m.loadLaunch(srv.ID)
	return err == nil && li.Installed
}

// MarkUninstalled drops the recorded launch info so the next EnsureInstalled
// re-downloads and re-prepares the server (used for version/software changes).
func (m *Manager) MarkUninstalled(id string) {
	_ = os.Remove(m.launchPath(id))
}

func (m *Manager) loadLaunch(id string) (*launchInfo, error) {
	data, err := os.ReadFile(m.launchPath(id))
	if err != nil {
		return nil, err
	}
	var li launchInfo
	if err := json.Unmarshal(data, &li); err != nil {
		return nil, err
	}
	return &li, nil
}

func (m *Manager) saveLaunch(id string, li *launchInfo) error {
	data, _ := json.MarshalIndent(li, "", "  ")
	return os.WriteFile(m.launchPath(id), data, 0o644)
}

// Install downloads/prepares the server software and writes eula + properties.
// It resolves and (if needed) installs the required Java runtime first, since
// some providers (Forge/NeoForge/Quilt/Spigot) run an installer with Java.
func (m *Manager) Install(ctx context.Context, srv *model.Server) error {
	prov, ok := providers.Get(srv.Software)
	if !ok {
		return fmt.Errorf("server: no provider for %q", srv.Software)
	}
	dataDir := m.ensureDataDir(srv)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	// Kurucular (Forge/NeoForge) da Java ile çalışır: eski kayıttaki düşük
	// Java burada da yükseltilir, yoksa 26.x kurucusu Java 21'de düşer.
	if java.RaiseToRequired(srv) {
		if err := m.store.SaveServer(srv); err != nil {
			m.logf("server: %q kaydedilemedi: %v", srv.Name, err)
		}
	}

	// Ensure Java is available (installers need it; jar flavors don't, but we
	// resolve it anyway so the later Start is fast and offline-safe).
	rt, err := m.java.Ensure(srv.JavaMajor)
	javaBin := ""
	if err == nil {
		javaBin = rt.JavaBin
	} else {
		m.logf("server: java %d not ready (%v); jar-only flavors can still install", srv.JavaMajor, err)
	}

	m.logf("server: installing %s %s for %q", srv.Software, srv.MCVersion, srv.Name)
	res, err := prov.Install(ctx, srv.MCVersion, dataDir, javaBin, m.client, m.log)
	if err != nil {
		return err
	}

	if err := writeEULA(dataDir); err != nil {
		return err
	}
	if err := applyProperties(dataDir, srv); err != nil {
		return err
	}

	// Old-client compatibility: pull ViaVersion/ViaBackwards for plugin servers.
	if srv.AllowOldVersions {
		m.installViaVersion(ctx, srv, dataDir)
	}

	li := &launchInfo{InstallResult: *res, Installed: true}
	if err := m.saveLaunch(srv.ID, li); err != nil {
		return err
	}
	m.logf("server: installed %q (launch: %s)", srv.Name, describeLaunch(res))
	return nil
}

func describeLaunch(r *providers.InstallResult) string {
	switch {
	case r.JarFile != "":
		return "-jar " + r.JarFile
	case r.ArgsFile != "":
		return "@" + r.ArgsFile
	case r.Script != "":
		return "script " + r.Script
	default:
		return "unknown"
	}
}

// ensureDataDir resolves the server's data directory, honoring a custom
// Server.DataDir by symlinking the default location to it. All other
// subsystems (Start, backup, worlds, files) keep using the default path and
// transparently follow the symlink. Best-effort: if a custom dir or symlink
// can't be created (e.g. Windows dev host without privilege, or the default
// already holds data) it logs and falls back to the default tree.
func (m *Manager) ensureDataDir(srv *model.Server) string {
	def := m.store.Paths.ServerData(srv.ID)
	custom := strings.TrimSpace(srv.DataDir)
	if custom == "" {
		return def
	}
	if err := os.MkdirAll(custom, 0o755); err != nil {
		m.logf("server: custom dataDir %q unusable (%v); using default", custom, err)
		return def
	}
	if info, err := os.Lstat(def); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return def // already linked
		}
		// Real directory at the default path: only replace it if empty so we
		// never destroy an existing world.
		if entries, _ := os.ReadDir(def); len(entries) > 0 {
			m.logf("server: %s already has data at default path; ignoring custom dataDir", srv.ID)
			return def
		}
		os.Remove(def)
	}
	_ = os.MkdirAll(filepath.Dir(def), 0o755)
	if err := os.Symlink(custom, def); err != nil {
		m.logf("server: symlink %q -> %q failed (%v); using default", def, custom, err)
		_ = os.MkdirAll(def, 0o755)
		return def
	}
	m.logf("server: data for %s stored at %q", srv.ID, custom)
	return def
}

// writeEULA accepts the Minecraft EULA so the server can start unattended.
func writeEULA(dir string) error {
	return os.WriteFile(filepath.Join(dir, "eula.txt"),
		[]byte("# Accepted via MCOS install wizard\neula=true\n"), 0o644)
}

// applyProperties writes the MCOS-managed keys into server.properties. Numeric
// and string fields only override Minecraft's defaults when set (non-zero /
// non-empty); the booleans (online-mode, pvp, hardcore, white-list) are always
// written because the create handler always resolves them to a concrete value.
func applyProperties(dir string, srv *model.Server) error {
	kv := [][2]string{
		{"server-port", fmt.Sprintf("%d", srv.Port)},
		{"online-mode", boolStr(srv.OnlineMode)},
		{"pvp", boolStr(srv.PVP)},
		{"hardcore", boolStr(srv.Hardcore)},
		{"white-list", boolStr(srv.Whitelist)},
	}
	if srv.ViewDistance > 0 {
		kv = append(kv, [2]string{"view-distance", fmt.Sprintf("%d", srv.ViewDistance)})
	}
	if srv.SimDistance > 0 {
		kv = append(kv, [2]string{"simulation-distance", fmt.Sprintf("%d", srv.SimDistance)})
	}
	if srv.MaxPlayers > 0 {
		kv = append(kv, [2]string{"max-players", fmt.Sprintf("%d", srv.MaxPlayers)})
	}
	if strings.TrimSpace(srv.MOTD) != "" {
		kv = append(kv, [2]string{"motd", srv.MOTD})
	}
	if g := strings.TrimSpace(srv.Gamemode); g != "" {
		kv = append(kv, [2]string{"gamemode", g})
	}
	if d := strings.TrimSpace(srv.Difficulty); d != "" {
		kv = append(kv, [2]string{"difficulty", d})
	}
	// Tohum: ortak dünyada her düğüm AYNI araziyi üretmek zorunda.
	// Boşsa yazılmaz — Minecraft kendi rastgele tohumunu seçer.
	if s := strings.TrimSpace(srv.LevelSeed); s != "" {
		kv = append(kv, [2]string{"level-seed", s})
	}

	// ── Düzeltilen gerçek hata: accepts-transfers HİÇ YAZILMIYORDU ──────
	//
	// Ortak dünya (MCOS Link) oyuncuyu sınırda öteki sunucuya aktarmak için
	// ServerTransferS2CPacket kullanıyor. Minecraft'ta HEDEF sunucu, gelen
	// aktarımı kabul etmek için "accepts-transfers=true" ayarını ister ve
	// VARSAYILAN DEĞER FALSE'tur.
	//
	// Bu ayar hiçbir yerde yazılmıyordu. Eklentinin kendi javadoc'u onu
	// "zorunlu ön koşul" ilan ediyor (McosLinkPlugin.java:39-47) ama Go
	// tarafı yazmıyordu — yani ortak dünya özelliği SON ADIMDA kırıktı:
	// oyuncu sınırı geçiyor, veri gidiyor, aktarım paketi yollanıyor ve
	// hedef sunucu istemciyi REDDEDİYORDU.
	//
	// Kullanıcının "mcos eşleme çalışsın" demesinin sebebi büyük olasılıkla
	// buydu: her şey doğru görünüyor, yalnızca son adım sessizce düşüyor.
	//
	// Her zaman yazılıyor (yalnızca ortak dünyada değil): ayarın açık olması
	// tek başına bir risk değil — aktarımı ancak MCOS'un kendi eklentisi
	// başlatabilir ve hedef zaten kimlik doğrulaması yapıyor.
	kv = append(kv, [2]string{"accepts-transfers", "true"})
	for _, p := range kv {
		if err := setProperty(dir, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}

// ReadLinkRules reads the rules a shared world must share from the ORIGIN's
// server.properties.
//
// Dosya gerçeğin kaynağıdır: kullanıcı online-mode'u sonradan panelin dosya
// düzenleyicisinden değiştirmiş olabilir; kayıttaki alan ise yalnızca
// oluşturmadaki değeri tutar. Dosya yoksa ya da anahtar eksikse kayıttaki
// değer kullanılır.
func ReadLinkRules(dir string, srv *model.Server) *model.LinkRules {
	r := &model.LinkRules{
		OnlineMode: srv.OnlineMode, Gamemode: srv.Gamemode, Hardcore: srv.Hardcore,
		PVP: srv.PVP, MaxPlayers: srv.MaxPlayers,
	}
	f, err := os.Open(filepath.Join(dir, "server.properties"))
	if err != nil {
		return r
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "online-mode":
			r.OnlineMode = v == "true"
		case "pvp":
			r.PVP = v == "true"
		case "hardcore":
			r.Hardcore = v == "true"
		case "gamemode":
			if v != "" {
				r.Gamemode = v
			}
		case "max-players":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.MaxPlayers = n
			}
		}
	}
	return r
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// setProperty creates or updates a key in server.properties.
func setProperty(dir, key, value string) error {
	path := filepath.Join(dir, "server.properties")
	lines := []string{}
	found := false
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, key+"=") {
				line = key + "=" + value
				found = true
			}
			lines = append(lines, line)
		}
		f.Close()
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// WriteLinkProperties writes the keys a shared world MUST agree on.
//
// ── Yakalanan gerçek hata (uçtan uca sınamada ölçüldü) ──────────────────────
// applyProperties yalnızca KURULUMDA bir kez çalışır. Sunucu önce kurulup
// sonra "ortak dünya" yapılınca tohum ve zorluk yalnızca kayda yazılıyordu;
// kurucu MCOS'ta server.properties "level-seed=" boş ve "difficulty=easy"
// kaldı, düğüm ise 424242/hard ile açıldı — iki AYRI dünya.
//
// Bu yüzden ortak dünya sunucusu HER BAŞLATILIŞTA bu anahtarları yeniden
// yazar (bkz. Manager.Start). Yalnızca üç anahtar: kullanıcının elle
// düzenlediği diğer ayarlara dokunulmaz.
func WriteLinkProperties(dir string, srv *model.Server) error {
	if srv.Link.Mode != model.LinkSharedWorld {
		return nil
	}
	kv := [][2]string{{"accepts-transfers", "true"}}
	// Kardeş kopya: portu MCOS atadı ve (başka bir sunucu o portu alırsa)
	// yeniden atayabilir. Kurulumda bir kez yazılan değere güvenilirse
	// kardeş ana sunucunun portunda açılmaya çalışıp düşerdi.
	if srv.IsSibling() && srv.Port > 0 {
		kv = append(kv, [2]string{"server-port", strconv.Itoa(srv.Port)})
	}
	if d := strings.TrimSpace(srv.Difficulty); d != "" {
		kv = append(kv, [2]string{"difficulty", d})
	}
	// Tohum: dünya zaten varsa Minecraft bunu okumaz (level.dat kazanır),
	// ama yazmanın zararı yok ve dünya silinip yeniden üretilirse doğru
	// arazi çıkar.
	if s := strings.TrimSpace(srv.LevelSeed); s != "" {
		kv = append(kv, [2]string{"level-seed", s})
	}
	// Eş kopyası: kurucunun oyun kuralları (bkz. model.LinkRules; online-mode
	// uyuşmazlığı aktarılan oyuncuyu attırıyordu).
	if r := srv.Link.Rules; r != nil {
		kv = append(kv,
			[2]string{"online-mode", strconv.FormatBool(r.OnlineMode)},
			[2]string{"pvp", strconv.FormatBool(r.PVP)},
			[2]string{"hardcore", strconv.FormatBool(r.Hardcore)})
		if r.Gamemode != "" {
			kv = append(kv, [2]string{"gamemode", r.Gamemode})
		}
		if r.MaxPlayers > 0 {
			kv = append(kv, [2]string{"max-players", strconv.Itoa(r.MaxPlayers)})
		}
	}
	for _, p := range kv {
		if err := setProperty(dir, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}
