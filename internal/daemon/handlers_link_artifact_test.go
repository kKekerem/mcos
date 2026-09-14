package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// ORTAK DÜNYA ARTEFAKTI: hangi yazılıma NE kurulur
// ════════════════════════════════════════════════════════════════════════════
//
// Bu testler gerçek bir sunucuda görülen iki arızayı koruyor:
//
//  1. mcos-link bir FABRIC modudur. Forge ve NeoForge onu hiç tanımaz, ama
//     eski kod `SupportsMods()` kullandığı için jar oraya da kopyalanıyordu:
//     panel "mod kurulu" diyor, ortak dünya hiç çalışmıyordu.
//
//  2. fabric.mod.json fabric-api'yi SERT bağımlılık olarak bildiriyor. Eksikse
//     Fabric Loader modu atlamaz, SUNUCUYU HİÇ AÇMAZ. ensureLinkArtifact modu
//     varsayılan olarak her sunucuya kurduğu için bu, MCOS'un kurduğu her
//     Fabric sunucusunun açılmaması demekti.

func TestLinkArtifactOnlyFabricGetsTheMod(t *testing.T) {
	cases := []struct {
		sw      model.Software
		wantOK  bool
		wantDir string
	}{
		{model.SoftwareFabric, true, "mods"},
		{model.SoftwarePaper, true, "plugins"},
		{model.SoftwarePurpur, true, "plugins"},
		{model.SoftwareSpigot, true, "plugins"},
		// Fabric biçimli bir modu YÜKLEYEMEZLER; sessizce kopyalamak yerine
		// açıkça reddedilmeliler.
		{model.SoftwareForge, false, ""},
		{model.SoftwareNeoForge, false, ""},
		{model.SoftwareQuilt, false, ""},
		{model.SoftwareVanilla, false, ""},
	}
	for _, c := range cases {
		name, dir, ok := linkArtifact(c.sw)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v, %v bekleniyordu", c.sw, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if dir != c.wantDir {
			t.Errorf("%s: dizin %q, %q bekleniyordu", c.sw, dir, c.wantDir)
		}
		if name == "" {
			t.Errorf("%s: dosya adı boş", c.sw)
		}
	}
}

func TestFabricAPIPresentDetectsCommonNames(t *testing.T) {
	dir := t.TempDir()
	if fabricAPIPresent(dir) {
		t.Fatal("boş klasörde fabric-api bulundu sanıldı")
	}
	// Modrinth'in verdiği ad.
	p := filepath.Join(dir, "fabric-api-0.141.6+1.21.11.jar")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fabricAPIPresent(dir) {
		t.Fatal("fabric-api-*.jar tanınmadı")
	}
}

// Çevrimdışı önbellek dosya adının BAŞINA URL özeti ekler
// (providers.cachePath). O biçim de tanınmalı, yoksa paket içinde dosya
// varken yine ağa çıkılır.
func TestFabricAPIPresentDetectsCachePrefixedName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a1b2c3d4e5f60718-fabric-api-0.141.6+1.21.11.jar")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fabricAPIPresent(dir) {
		t.Fatal("önbellek adlandırmasıyla gelen fabric-api tanınmadı")
	}
}

// Boş bir jar SAYILMAMALI: yarım kalmış bir indirme, "var" diye geçilirse
// sunucu yine açılmaz ve sebebi bu sefer hiç görünmez.
func TestFindBundledFabricAPIIgnoresEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fabric-api-0.0.0.jar"),
		nil, 0o644); err != nil {
		t.Fatal(err)
	}

	old := linkModSearchPaths
	linkModSearchPaths = []string{dir}
	t.Cleanup(func() { linkModSearchPaths = old })

	if got := findBundledFabricAPI(); got != "" {
		t.Fatalf("boş dosya kabul edildi: %q", got)
	}
}

func TestFindBundledFabricAPIFindsRealFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fabric-api-0.141.6+1.21.11.jar")
	if err := os.WriteFile(p, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := linkModSearchPaths
	linkModSearchPaths = []string{dir}
	t.Cleanup(func() { linkModSearchPaths = old })

	if got := findBundledFabricAPI(); got != p {
		t.Fatalf("bulunan %q, %q bekleniyordu", got, p)
	}
}
