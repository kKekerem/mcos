package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	for _, p := range kv {
		if err := setProperty(dir, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
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
