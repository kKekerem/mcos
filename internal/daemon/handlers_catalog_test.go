package daemon

import (
	"strings"
	"testing"

	"mcos/internal/catalog"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// "Sürümü yok say" YALNIZCA sürüm süzgecini kaldırmalı. Yükleyiciyi de
// kaldırsaydı purpur sunucusunda Fabric modları listelenir, kurulur ve sunucu
// onları hiç yüklemezdi.
func TestCatalogQueryAnyVersionKeepsLoader(t *testing.T) {
	srv := &model.Server{Software: model.SoftwarePurpur, MCVersion: "1.21.1"}

	q := catalogQuery(srv, ipc.CatalogSearchParams{Query: "worldedit"})
	if q.Loader != "purpur" || q.GameVersion != "1.21.1" || q.ProjectType != "plugin" {
		t.Fatalf("süzgeçli sorgu yanlış: %+v", q)
	}
	q = catalogQuery(srv, ipc.CatalogSearchParams{Query: "worldedit", AnyVersion: true})
	if q.GameVersion != "" {
		t.Errorf("AnyVersion sürüm süzgecini kaldırmadı: %q", q.GameVersion)
	}
	if q.Loader != "purpur" {
		t.Errorf("AnyVersion yükleyiciyi de değiştirdi: %q", q.Loader)
	}
}

// Panelin başlığı ("paper · 1.21.1 · 21 sonuç") uygulanan süzgeçlerden
// kurulur; bunlar tel biçimine taşınmalı, yoksa panel ne arandığını söyleyemez.
func TestCatalogResultCarriesFiltersAndLoader(t *testing.T) {
	res := catalogResult(catalog.SearchResult{
		Hits:        []catalog.Project{{Slug: "worldedit", Title: "WorldEdit", ProjectType: "mod", Loader: "paper"}},
		Total:       21,
		Loaders:     []string{"purpur", "paper", "spigot", "bukkit"},
		GameVersion: "1.21.1",
		ProjectType: "plugin",
	})
	if res.Total != 21 || res.GameVersion != "1.21.1" || len(res.Loaders) != 4 || res.ProjectType != "plugin" {
		t.Errorf("süzgeçler taşınmadı: %+v", res)
	}
	if len(res.Items) != 1 || res.Items[0].Loader != "paper" {
		t.Errorf("sonucun yükleyici halkası taşınmadı: %+v", res.Items)
	}
}

// Kurulum yanıtı: eski panel Message'ı "kuruldu: " önekiyle gösterir, o
// yüzden Message dosya adı KALMALI; yeni metin Summary'de.
func TestCatalogInstallResultSummary(t *testing.T) {
	in := catalog.Installed{
		Resolved: catalog.Resolved{Loader: "paper", Wanted: "purpur", GameVersion: "1.21.1", ExactGameVersion: true},
		Path:     "/data/servers/a/plugins/worldedit-bukkit-7.3.9.jar",
	}
	r := catalogInstallResult("worldedit", in)
	if r.Message != "worldedit-bukkit-7.3.9.jar" {
		t.Errorf("Message %q — eski istemci dosya adı bekliyor", r.Message)
	}
	if r.Summary != "worldedit (paper uyumlu) kuruldu" {
		t.Errorf("Summary %q", r.Summary)
	}
	if !strings.Contains(r.Detail, "purpur") {
		t.Errorf("ayrıntı geri düşüşü anlatmıyor: %q", r.Detail)
	}
}
