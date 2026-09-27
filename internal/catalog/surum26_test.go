package catalog

import (
	"context"
	"testing"
)

// Minecraft 26.x: Modrinth ön sürüm etiketleri "26.3-rc-3" biçiminde
// (api.modrinth.com/v2/tag/game_version, 2026-09-27). Süzgeç kaldırılınca bir
// 26.3 sunucusu için 26.3 ÇİZGİSİNDEKİ yapı, daha yeni tarihli bir 1.21.11
// yapısına tercih edilmeli; 1.21.11 modu 26.3 sunucusunda yüklenmez.
func TestResolve26OnSurumEtiketiAyniCizgi(t *testing.T) {
	f := newFake()
	f.versions["yenimod"] = []Version{
		{VersionNumber: "4.0-rc", VersionType: "release", GameVersions: []string{"26.3-rc-3"},
			Loaders: []string{"fabric"}, DatePublished: day(10),
			Files: []File{f.jarFile("yenimod-4.0-rc.jar", []byte("4"))}},
		{VersionNumber: "3.9", VersionType: "release", GameVersions: []string{"1.21.11"},
			Loaders: []string{"fabric"}, DatePublished: day(20),
			Files: []File{f.jarFile("yenimod-3.9.jar", []byte("3"))}},
	}
	c := newTestClient(f)
	r, err := c.Resolve(context.Background(), "yenimod", "fabric", "26.3", ResolveOptions{AnyGameVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version.VersionNumber != "4.0-rc" {
		t.Fatalf("seçilen %s; 26.3 çizgisindeki 4.0-rc bekleniyordu", r.Version.VersionNumber)
	}
	if r.ExactGameVersion {
		t.Fatal("26.3-rc-3 etiketi 26.3 ile TAM eşleşme sayılmamalı")
	}
}

// "26.1" ve "26.1.2" aynı çizgi: 26.1.2 sunucusu için 26.1 yapısı, daha yeni
// bir 26.2 yapısına tercih edilir.
func TestResolve26DuzeltmeSurumuAyniCizgi(t *testing.T) {
	f := newFake()
	f.versions["mod"] = []Version{
		{VersionNumber: "2.0", VersionType: "release", GameVersions: []string{"26.2"},
			Loaders: []string{"paper"}, DatePublished: day(30),
			Files: []File{f.jarFile("mod-2.0.jar", []byte("2"))}},
		{VersionNumber: "1.5", VersionType: "release", GameVersions: []string{"26.1"},
			Loaders: []string{"paper"}, DatePublished: day(5),
			Files: []File{f.jarFile("mod-1.5.jar", []byte("1"))}},
	}
	c := newTestClient(f)
	r, err := c.Resolve(context.Background(), "mod", "paper", "26.1.2", ResolveOptions{AnyGameVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version.VersionNumber != "1.5" {
		t.Fatalf("seçilen %s; 26.1 çizgisindeki 1.5 bekleniyordu", r.Version.VersionNumber)
	}
}
