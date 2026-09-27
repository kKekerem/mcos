package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"mcos/internal/catalog"
	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/model"
	"mcos/internal/store"
)

// projectTypeFor maps a flavour to the Modrinth project type to search.
func projectTypeFor(s model.Software) string {
	switch {
	case s.SupportsPlugins():
		return "plugin"
	case s.SupportsMods():
		return "mod"
	default:
		return "datapack"
	}
}

// contentDirFor returns the install directory (relative to the server data
// root) for the flavour's content type.
func contentDirFor(s model.Software) string {
	switch {
	case s.SupportsPlugins():
		return "plugins"
	case s.SupportsMods():
		return "mods"
	default:
		return filepath.Join("world", "datapacks")
	}
}

func (d *Daemon) handleCatalogSearch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.CatalogSearchParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	srv, err := d.store.GetServer(p.ServerID)
	if err == store.ErrNotFound {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
	} else if err != nil {
		return nil, err
	}
	res, err := d.catalog.SearchCompatible(ctx, catalogQuery(srv, p))
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return catalogResult(res), nil
}

// catalogQuery builds the Modrinth query for a server.
//
// Ayrı bir fonksiyon, çünkü sınanması gereken kural tam olarak bu: yükleyici
// SUNUCUNUN yükleyicisidir (zincir catalog içinde türetilir) ve "sürümü yok
// say" yalnızca sürüm süzgecini kaldırır, yükleyiciyi DEĞİL — aksi halde
// purpur sunucusuna Fabric modu önerilirdi.
func catalogQuery(srv *model.Server, p ipc.CatalogSearchParams) catalog.SearchQuery {
	q := catalog.SearchQuery{
		Query:       p.Query,
		ProjectType: projectTypeFor(srv.Software),
		Loader:      srv.Software.ModrinthLoader(),
		GameVersion: srv.MCVersion,
		Limit:       25,
	}
	if p.AnyVersion {
		q.GameVersion = ""
	}
	return q
}

// catalogResult converts a catalog search into its wire form.
func catalogResult(res catalog.SearchResult) ipc.CatalogSearchResult {
	items := make([]ipc.CatalogItem, 0, len(res.Hits))
	for _, h := range res.Hits {
		items = append(items, ipc.CatalogItem{
			Slug:        h.Slug,
			Title:       h.Title,
			Description: h.Description,
			Downloads:   h.Downloads,
			Type:        h.ProjectType,
			Loader:      h.Loader,
		})
	}
	return ipc.CatalogSearchResult{
		Items:       items,
		Total:       res.Total,
		Loaders:     res.Loaders,
		GameVersion: res.GameVersion,
		ProjectType: res.ProjectType,
	}
}

func (d *Daemon) handleCatalogInstall(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.CatalogInstallParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Slug) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "slug is required"}
	}
	srv, err := d.store.GetServer(p.ServerID)
	if err == store.ErrNotFound {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
	} else if err != nil {
		return nil, err
	}
	dir := filepath.Join(d.store.Paths.ServerData(srv.ID), contentDirFor(srv.Software))
	in, err := d.catalog.Install(ctx, p.Slug, srv.Software.ModrinthLoader(), srv.MCVersion, dir,
		catalog.ResolveOptions{AnyGameVersion: p.AnyVersion})
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	d.log.Infof("catalog: installed %s -> %s (%s)", p.Slug, in.Path, in.Describe())
	return catalogInstallResult(p.Slug, in), nil
}

// catalogInstallResult explains an install in the panel's words.
func catalogInstallResult(slug string, in catalog.Installed) ipc.CatalogInstallResult {
	file := filepath.Base(in.Path)
	return ipc.CatalogInstallResult{
		OK:      true,
		Message: file,
		Summary: in.Summary(slug),
		File:    file,
		Loader:  in.Loader,
		Wanted:  in.Wanted,
		Detail:  in.Describe(),
	}
}

func (d *Daemon) handleServerChangeVersion(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ServerChangeVersionParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.MCVersion) == "" && !p.Software.Valid() {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "mcVersion or software required"}
	}
	srv, err := d.store.GetServer(p.ID)
	if err == store.ErrNotFound {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
	} else if err != nil {
		return nil, err
	}
	if d.servers.State(srv.ID) == model.StateRunning || d.servers.State(srv.ID) == model.StateStarting {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: "stop the server before changing version"}
	}

	// Safety net: snapshot the current install before re-installing.
	if _, err := d.backup.Create(srv.ID, "pre-version-change", "automatic before version change", false); err != nil {
		d.log.Warnf("daemon: pre-change backup failed: %v", err)
	}

	if v := strings.TrimSpace(p.MCVersion); v != "" {
		srv.MCVersion = v
	}
	if p.Software.Valid() {
		srv.Software = p.Software
		srv.SupportsPlugins = p.Software.SupportsPlugins()
		srv.SupportsMods = p.Software.SupportsMods()
	}
	srv.JavaMajor = java.RequiredJavaMajor(srv.MCVersion)
	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}
	d.servers.MarkUninstalled(srv.ID)
	go func(s *model.Server) {
		if err := d.servers.EnsureInstalled(context.Background(), s); err != nil {
			d.log.Errorf("daemon: re-install after version change failed: %v", err)
		}
		// Ortak dunya eklentisi HER sunucuya kurulur: kullanici onu
		// sonradan actiginda sunucuyu yeniden kurmak gerekmesin.
		d.ensureLinkArtifact(s)
		// Paket kuruluysa yeni sürüme göre yenilenir: eski sürümün
		// Lithium'u yeni sürümde açılışı düşürürdü.
		if s.PerfPack != nil {
			if err := d.runPerfPack(s.ID); err != nil && !errors.Is(err, errPerfPackBusy) {
				d.log.Errorf("perfpack: %q: %v", s.Name, err)
			}
		}
	}(srv.Clone())

	return ipc.OKResult{OK: true, Message: fmt.Sprintf("%s %s kuruluyor", srv.Software, srv.MCVersion)}, nil
}

func (d *Daemon) handleServerScanUSBMods(_ context.Context, _ json.RawMessage) (any, error) {
	items, err := files.ScanUSBJars()
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return ipc.USBScanResult{Items: items}, nil
}

func (d *Daemon) handleServerInstallUSBMods(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.USBInstallParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Items) == 0 {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "kurulacak dosya seçilmedi"}
	}
	srv, err := d.store.GetServer(p.ServerID)
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "sunucu bulunamadı"}
	}
	targetFolder := filepath.Join(d.store.Paths.ServerData(srv.ID), contentDirFor(srv.Software))

	copied, err := files.InstallUSBJars(p.Items, targetFolder)
	switch {
	case copied == 0:
		// Tamamen başarısız. Eski sürüm burada "0 mod/eklenti kuruldu" diye
		// BAŞARI dönüyordu; artık gerçek sebep hata olarak iletilir.
		msg := "USB'den hiçbir dosya kopyalanamadı"
		if err != nil {
			msg = err.Error()
		}
		return nil, &ipc.Error{Code: ipc.CodeInternalError, Message: msg}
	case err != nil:
		// Kısmî başarı: kopyalananı da başarısızları da bildir.
		d.log.Warnf("usb: kısmî kurulum (%d dosya): %v", copied, err)
		return ipc.OKResult{OK: true, Message: err.Error()}, nil
	default:
		d.log.Infof("usb: %d dosya %s içine kuruldu", copied, targetFolder)
		return ipc.OKResult{OK: true, Message: fmt.Sprintf("%d mod/eklenti kuruldu", copied)}, nil
	}
}
