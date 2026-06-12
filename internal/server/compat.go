package server

import (
	"context"
	"path/filepath"

	"mcos/internal/catalog"
	"mcos/internal/model"
)

// installViaVersion downloads ViaVersion (+ViaBackwards) into the server's
// plugins/ directory so older clients can connect. It is only applicable to
// Bukkit-style plugin servers; mod loaders use a different project (ViaFabric)
// and are left untouched with a log note.
func (m *Manager) installViaVersion(ctx context.Context, srv *model.Server, dataDir string) {
	if !srv.SupportsPlugins {
		m.logf("server: ViaVersion not applicable for %s (%s is not plugin-based)", srv.Name, srv.Software)
		return
	}
	loader := srv.Software.ModrinthLoader()
	cat := catalog.New()
	pluginsDir := filepath.Join(dataDir, "plugins")
	for _, slug := range []string{"viaversion", "viabackwards"} {
		path, err := cat.InstallByID(ctx, slug, loader, srv.MCVersion, pluginsDir)
		if err != nil {
			m.logf("server: %s install failed (%v); old-client support may be partial", slug, err)
			continue
		}
		m.logf("server: installed %s -> %s", slug, path)
	}
}
