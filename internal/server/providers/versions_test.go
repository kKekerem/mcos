package providers

import (
	"context"
	"slices"
	"strings"
	"testing"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// Yazılım başına sürüm listesi — AĞSIZ
// ════════════════════════════════════════════════════════════════════════════
//
// Fikstürler (testdata/liste_*.json, paper_project.json) GERÇEK yanıtlardan
// 2026-09-27'de alındı; Folia ve Purpur olduğu gibi, ötekiler kırpılmış
// ama aynı biçimde ve aynı sırada. İstekler sahteAg ile yönlendirilir ve
// gerçek adrese gidildiği ayrıca denetlenir.

func listeAgi() *sahteAg {
	ag := yeniAg()
	ag.ekle(mojangManifestURL, "@liste_mojang_manifest.json")
	ag.ekle(paperAPI+"/paper", "@paper_project.json")
	ag.ekle(paperAPI+"/folia", "@liste_folia_project.json")
	ag.ekle(purpurAPI, "@liste_purpur_project.json")
	ag.ekle(fabricMeta+"/versions/game", "@liste_fabric_game.json")
	ag.ekle(quiltMeta+"/versions/game", "@liste_quilt_game.json")
	ag.ekle(neoforgeVersionsAPI, "@liste_neoforge_versions.json")
	ag.ekle(forgePromos, "@liste_forge_promos.json")
	ag.ekle(spigotSurumDizini, "@liste_spigot_versions.html")
	return ag
}

func TestSurumListesiHerYazilim(t *testing.T) {
	ag := listeAgi()
	cl := ag.istemci(t)

	mojangTam := []string{"26.3", "26.2", "26.1.2", "26.1.1", "26.1", "1.21.11",
		"1.21.10", "1.8.9", "1.8", "1.7.10", "1.2.5"}
	for _, tc := range []struct {
		sw     model.Software
		adres  string
		kaynak string
		istek  []string // tam liste
		olmaz  []string // bu yazılımda OLMAMASI gerekenler
	}{
		// Mojang: anlık görüntü (26.4-snapshot-1, 25w46a), rc, eski
		// beta/alfa ve sunucu jar'ı olmayan 1.2.4/1.0 elenir.
		{model.SoftwareVanilla, mojangManifestURL, "launchermeta.mojang.com", mojangTam,
			[]string{"26.4-snapshot-1", "26.3-rc-3", "25w46a", "1.2.4", "1.0", "b1.8.1"}},
		// Spigot ve CraftBukkit: TestSurumListesiSpigotDizini.
		// Folia'nın en yenisi 26.2; 26.3 ve düz 26.1 yok.
		{model.SoftwareFolia, paperAPI + "/folia", "fill.papermc.io",
			[]string{"26.2", "26.1.2", "1.21.11", "1.21.8", "1.21.6", "1.21.5", "1.21.4",
				"1.20.6", "1.20.4", "1.20.2", "1.20.1", "1.19.4"},
			[]string{"26.3", "26.1"}},
		// Fabric düz 26.1'i yayımlıyor; 1.14 en eskisi.
		{model.SoftwareFabric, fabricMeta + "/versions/game", "meta.fabricmc.net",
			[]string{"26.3", "26.2", "26.1.2", "26.1.1", "26.1", "1.21.11", "1.21.10",
				"1.20.5", "1.14.4", "1.14"},
			[]string{"26.4-snapshot-1", "26.3-rc-3", "26w14a", "1.14 Pre-Release 5"}},
		{model.SoftwareQuilt, quiltMeta + "/versions/game", "meta.quiltmc.org",
			[]string{"26.3", "26.2", "26.1.2", "26.1.1", "26.1", "1.21.11", "1.21.10",
				"1.20.5", "1.14.4"},
			[]string{"26.3-rc-3", "25w46a"}},
		// NeoForge: 26.3'te yalnızca beta var ama kurulur; 1.20.1 ayrı
		// yapıt olduğu için YOK; craftmine şaka sürümü eşlenmez.
		{model.SoftwareNeoForge, neoforgeVersionsAPI, "maven.neoforged.net",
			[]string{"26.3", "26.2", "26.1.2", "26.1.1", "26.1", "1.21.11", "1.21.1",
				"1.21", "1.20.6", "1.20.4", "1.20.2"},
			[]string{"1.20.1"}},
		// Forge: kurucu 1.5.2'de başlıyor; 1.4.7 ve 1.1 promosyonda var
		// ama kurulamaz.
		{model.SoftwareForge, forgePromos, "files.minecraftforge.net",
			[]string{"26.3", "26.2", "26.1.2", "26.1.1", "26.1", "1.21.11", "1.21.1",
				"1.20.1", "1.12.2", "1.8.9", "1.7.10", "1.5.2"},
			[]string{"1.4.7", "1.1"}},
	} {
		t.Run(string(tc.sw), func(t *testing.T) {
			l, err := ListVersions(context.Background(), cl, tc.sw)
			if err != nil {
				t.Fatalf("liste: %v", err)
			}
			if !slices.Equal(l.Versions, tc.istek) {
				t.Errorf("liste:\n aldım:   %s\n istedim: %s",
					strings.Join(l.Versions, " "), strings.Join(tc.istek, " "))
			}
			for _, v := range tc.olmaz {
				if slices.Contains(l.Versions, v) {
					t.Errorf("%q listede olmamalı", v)
				}
			}
			if l.Source != tc.kaynak || !ag.istendi(tc.adres) {
				t.Errorf("kaynak %q (istendi mi: %v), beklenen %q", l.Source, ag.istendi(tc.adres), tc.kaynak)
			}
		})
	}
}

// Spigot ve CraftBukkit: liste BuildTools'un sürüm dizininden
// (hub.spigotmc.org/versions/, gerçek sayfa 2026-09-27). Mojang'da olan ama
// dizinde JSON'u olmayan 1.8.9, 1.16, 1.9.1… BuildTools'ta "Could not get
// version" ile düşer, listelenmemeli; ön sürüm JSON'ları ("1.18-rc3",
// "1.14-pre5") ve "latest.json" elenir. Mojang'a hiç gidilmez.
func TestSurumListesiSpigotDizini(t *testing.T) {
	ag := listeAgi()
	cl := ag.istemci(t)
	for _, sw := range []model.Software{model.SoftwareSpigot, model.SoftwareCraftBukkit} {
		l, err := ListVersions(context.Background(), cl, sw)
		if err != nil {
			t.Fatalf("%s: %v", sw, err)
		}
		if got := strings.Join(l.Versions[:5], " "); got != "26.3 26.2 26.1.2 26.1.1 26.1" {
			t.Errorf("%s ilk beş: %s", sw, got)
		}
		if son := l.Versions[len(l.Versions)-1]; son != "1.8" || len(l.Versions) != 68 {
			t.Errorf("%s: en eski %q, uzunluk %d (beklenen 1.8, 68)", sw, son, len(l.Versions))
		}
		for _, v := range []string{"1.8.9", "1.8.1", "1.8.2", "1.9.1", "1.9.3", "1.10.1", "1.16",
			"1.7.10", "1.18-rc3", "1.14-pre5", "latest"} {
			if slices.Contains(l.Versions, v) {
				t.Errorf("%s: %q listede olmamalı (BuildTools dizininde yok)", sw, v)
			}
		}
		for _, v := range []string{"1.8.8", "1.16.1", "1.20.3", "1.21.11"} {
			if !slices.Contains(l.Versions, v) {
				t.Errorf("%s: %q listede olmalı", sw, v)
			}
		}
		if l.Source != "hub.spigotmc.org" {
			t.Errorf("%s kaynağı %q", sw, l.Source)
		}
	}
	if ag.istendi(mojangManifestURL) {
		t.Error("Spigot listesi için Mojang manifesti istendi; kaynak BuildTools dizini olmalı")
	}
}

// Paper: gerçek proje listesi (ön sürümler dahil) — düz 26.1 yok, rc ve pre
// yok, en yeni 26.3, en eski 1.7.10.
func TestSurumListesiPaper(t *testing.T) {
	l, err := ListVersions(context.Background(), listeAgi().istemci(t), model.SoftwarePaper)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(l.Versions[:5], " "); got != "26.3 26.2 26.1.2 26.1.1 1.21.11" {
		t.Errorf("ilk beş: %s", got)
	}
	for _, v := range []string{"26.1", "26.3-rc-3", "1.21.11-pre5", "1.13-pre7"} {
		if slices.Contains(l.Versions, v) {
			t.Errorf("%q listede olmamalı", v)
		}
	}
	if son := l.Versions[len(l.Versions)-1]; son != "1.7.10" || len(l.Versions) != 55 {
		t.Errorf("en eski %q, uzunluk %d (beklenen 1.7.10, 55)", son, len(l.Versions))
	}
}

// Purpur'un metadata.current'ı 26.2 diyor ama listede 26.3 var: en yeni
// listeden hesaplanmalı. Liste eskiden yeniye geliyor; sıra ters çevrilmeli.
func TestSurumListesiPurpur(t *testing.T) {
	l, err := ListVersions(context.Background(), listeAgi().istemci(t), model.SoftwarePurpur)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(l.Versions[:5], " "); got != "26.3 26.2 26.1.2 1.21.11 1.21.10" {
		t.Errorf("ilk beş: %s", got)
	}
	if son := l.Versions[len(l.Versions)-1]; son != "1.14.1" || slices.Contains(l.Versions, "26.1") {
		t.Errorf("en eski %q ya da düz 26.1 var: %v", son, l.Versions)
	}
}

// NeoForge'da yalnızca anlık görüntü derlemesi olan bir çizgi listelenmez:
// neoforgeInstaller onu kurmaz ("+snapshot" derlemeleri atlanır). Gerçek
// adlandırma "26.1.0.0-alpha.1+snapshot-1"; 26.4 bu biçimde eklenir.
func TestSurumListesiNeoForgeYalnizcaAnlikGoruntu(t *testing.T) {
	ag := yeniAg()
	ag.ekle(neoforgeVersionsAPI, `{"isSnapshot":false,"versions":[
		"26.3.0.23-beta","26.4.0.0-alpha.1+snapshot-1","26.4.0.0-alpha.2+snapshot-2"]}`)
	l, err := ListVersions(context.Background(), ag.istemci(t), model.SoftwareNeoForge)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.Versions, []string{"26.3"}) {
		t.Fatalf("yalnızca 26.3 listelenmeliydi: %v", l.Versions)
	}
}

// Hata yolları: ulaşılamayan API, boş liste ve bozuk yanıt HATA döner
// (daemon o zaman yedeğe düşer); boş liste "başarı" sayılsaydı sihirbaz
// boş bir sürüm listesi gösterirdi.
func TestSurumListesiHatalari(t *testing.T) {
	ag := yeniAg() // hiçbir yanıt yok: her istek 404
	ag.ekle(paperAPI+"/folia", `{"project":{"id":"folia","name":"Folia"},"versions":{}}`)
	ag.ekle(purpurAPI, `{"project":"purpur","versions":`)
	ag.ekle(fabricMeta+"/versions/game", `[{"version":"26.4-snapshot-1","stable":false}]`)
	cl := ag.istemci(t)
	for _, sw := range []model.Software{model.SoftwarePaper, model.SoftwareFolia,
		model.SoftwarePurpur, model.SoftwareFabric, model.SoftwareVanilla, model.SoftwareSpigot} {
		if l, err := ListVersions(context.Background(), cl, sw); err == nil {
			t.Errorf("%s: hata bekleniyordu, liste: %v", sw, l.Versions)
		}
	}
	// hub.spigotmc.org Cloudflare arkasında: dizin yerine 200 ile bir
	// denetim sayfası gelirse içinde sürüm yoktur; boş liste = hata.
	ag2 := yeniAg()
	ag2.ekle(spigotSurumDizini, `<!DOCTYPE html><html><head><title>Just a moment...</title></head>`+
		`<body><a href="/cdn-cgi/styles/cf.errors.css">x</a></body></html>`)
	if l, err := ListVersions(context.Background(), ag2.istemci(t), model.SoftwareSpigot); err == nil {
		t.Errorf("spigot: denetim sayfası hata vermeliydi, liste: %v", l.Versions)
	}
	if _, err := ListVersions(context.Background(), cl, "bukkit2"); err == nil {
		t.Error("bilinmeyen yazılım hata vermeliydi")
	}
}

// Her kayıtlı sağlayıcı bir sürüm listesi kaynağı bildirmeli: yenisi
// eklenip unutulursa sihirbaz o yazılım için hep yedek listeyi gösterirdi.
func TestHerSaglayiciSurumListeler(t *testing.T) {
	for _, sw := range model.AllSoftware {
		p, ok := Get(sw)
		if !ok {
			t.Errorf("%s: sağlayıcı yok", sw)
			continue
		}
		if _, ok := p.(versionLister); !ok {
			t.Errorf("%s: sürüm listesi kaynağı yok", sw)
		}
	}
}
