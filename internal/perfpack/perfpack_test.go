package perfpack

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcos/internal/catalog"
	"mcos/internal/model"
)

func slugs(p Pack) []string {
	out := make([]string, 0, len(p.Mods))
	for _, m := range p.Mods {
		out = append(out, m.Slug)
	}
	return out
}

// Yazılım -> liste eşlemesi: yanlış yükleyiciye mod kurmak (ör. Forge'a
// Fabric modu) sunucuyu açılışta düşürür; liste bilerek sabitlenir.
func TestForSoftwareLists(t *testing.T) {
	fabric := []string{"lithium", "ferrite-core", "krypton", "modernfix", "scalablelux", "vmp-fabric"}
	cases := []struct {
		sw      model.Software
		slugs   []string
		loaders []string
	}{
		{model.SoftwareFabric, fabric, []string{"fabric"}},
		{model.SoftwareQuilt, fabric, []string{"quilt", "fabric"}},
		{model.SoftwareNeoForge, []string{"lithium", "ferrite-core", "modernfix", "scalablelux"}, []string{"neoforge"}},
		{model.SoftwareForge, []string{"ferrite-core", "modernfix"}, []string{"forge"}},
	}
	for _, c := range cases {
		p := For(c.sw, "1.21.1")
		got := slugs(p)
		slices.Sort(got)
		want := slices.Clone(c.slugs)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: mods = %v, want %v", c.sw, got, want)
		}
		if !slices.Equal(p.Loaders, c.loaders) {
			t.Errorf("%s: loaders = %v, want %v", c.sw, p.Loaders, c.loaders)
		}
		if len(p.Settings) != 0 {
			t.Errorf("%s: mod paketinde ayar olmamalı", c.sw)
		}
	}

	for _, sw := range []model.Software{model.SoftwarePaper, model.SoftwarePurpur} {
		p := For(sw, "1.21.11")
		if len(p.Mods) != 0 || len(p.Settings) == 0 {
			t.Errorf("%s: eklenti yok, ayar olmalı (mods=%d settings=%d)", sw, len(p.Mods), len(p.Settings))
		}
		files := map[string]bool{}
		for _, s := range p.Settings {
			files[s.File] = true
		}
		if !files["spigot.yml"] || !files["config/paper-world-defaults.yml"] {
			t.Errorf("%s: dosyalar = %v", sw, files)
		}
	}
	// 1.19 öncesinde paper-world-defaults.yml yok: yalnızca spigot.yml.
	for _, s := range For(model.SoftwarePaper, "1.18.2").Settings {
		if s.File != "spigot.yml" {
			t.Errorf("1.18.2: beklenmeyen dosya %s", s.File)
		}
	}
	if p := For(model.SoftwareVanilla, "26.3"); !p.Empty() || p.Note == "" {
		t.Errorf("vanilla: paket boş ve gerekçeli olmalı: %+v", p)
	}
}

// Aynı slug iki kez olursa aynı mod iki kez indirilir ve Fabric "duplicate
// mod" ile açılmaz.
func TestForNoDuplicates(t *testing.T) {
	for _, sw := range []model.Software{model.SoftwareFabric, model.SoftwareQuilt,
		model.SoftwareNeoForge, model.SoftwareForge, model.SoftwarePaper} {
		p := For(sw, "1.21.1")
		seen := map[string]bool{}
		for _, m := range p.Mods {
			if seen[m.Slug] || seen[m.ProjectID] {
				t.Errorf("%s: %s iki kez", sw, m.Slug)
			}
			seen[m.Slug], seen[m.ProjectID] = true, true
		}
		keys := map[string]bool{}
		for _, s := range p.Settings {
			if keys[s.Key()] {
				t.Errorf("%s: ayar iki kez: %s", sw, s.Key())
			}
			keys[s.Key()] = true
		}
	}
}

// İnternet yokken HİÇBİR ağ isteği yapılmaz (panel dakikalarca beklemesin)
// ve kullanıcının istediği ifade döner.
func TestApplyOfflineSkips(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "olmamalı", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	in := &Installer{Catalog: catalog.NewWith(srv.Client(), srv.URL), Online: func() bool { return false }}
	res, err := in.Apply(context.Background(), Request{Software: model.SoftwareFabric, MC: "1.21.1", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Offline || !strings.Contains(res.State.Note, OfflineNote) {
		t.Fatalf("offline = %v, note = %q", res.Offline, res.State.Note)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("internet yokken %d istek yapıldı", n)
	}
	if es, _ := os.ReadDir(filepath.Join(dir, "mods")); len(es) != 0 {
		t.Fatalf("mods/ boş olmalı: %v", es)
	}
}

// fakeCatalog is an in-memory Modrinth: slug -> sürümler.
type fakeCatalog struct {
	versions map[string][]catalog.Version
}

func (f *fakeCatalog) Versions(_ context.Context, slug, loader, _ string) ([]catalog.Version, error) {
	var out []catalog.Version
	for _, v := range f.versions[slug] {
		if slices.Contains(v.Loaders, loader) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeCatalog) DownloadTo(_ context.Context, file catalog.File, dir string) (string, error) {
	if file.Filename == "" {
		return "", errors.New("adsız dosya")
	}
	p := filepath.Join(dir, file.Filename)
	return p, os.WriteFile(p, []byte("jar"), 0o644)
}

func ver(slug, mc string, deps ...catalog.Dependency) catalog.Version {
	return catalog.Version{VersionNumber: "1.0", VersionType: "release", GameVersions: []string{mc},
		Loaders: []string{"fabric"}, DatePublished: time.Unix(1, 0), Dependencies: deps,
		Files: []catalog.File{{Filename: slug + "-1.0.jar", Primary: true}}}
}

// writeModJar writes a jar with a fabric.mod.json (kullanıcının kendi modu).
func writeModJar(t *testing.T, path, id string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("fabric.mod.json")
	w.Write([]byte(`{"id":"` + id + `"}`))
	z.Close()
	f.Close()
}

// Kullanıcının kendi Lithium'u (farklı dosya adıyla) ikinci kez kurulmaz;
// sürüm için yapısı olmayan mod sessizce atlanır; bilinmeyen zorunlu
// bağımlılık isteyen mod atlanır, fabric-api klasördeyse karşılanmış sayılır.
func TestApplyOnlineDedupAndSkip(t *testing.T) {
	const mc = "1.21.1"
	fc := &fakeCatalog{versions: map[string][]catalog.Version{
		"lithium":      {ver("lithium", mc)},
		"ferrite-core": {ver("ferrite-core", mc)},
		"krypton":      {ver("krypton", "1.20.1")}, // bu sürüm için yapı yok
		"modernfix": {ver("modernfix", mc, catalog.Dependency{ProjectID: "BILINMEYEN",
			DependencyType: "required"})},
		"scalablelux": {ver("scalablelux", mc, catalog.Dependency{ProjectID: fabricAPIProjectID,
			DependencyType: "required"})},
	}}
	dir := t.TempDir()
	mods := filepath.Join(dir, "mods")
	os.MkdirAll(mods, 0o755)
	writeModJar(t, filepath.Join(mods, "Lithium-benim.jar"), "lithium")
	writeModJar(t, filepath.Join(mods, "fabric-api-x.jar"), "fabric-api")

	in := &Installer{Catalog: fc, Online: func() bool { return true }}
	res, err := in.Apply(context.Background(), Request{Software: model.SoftwareFabric, MC: mc, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range res.Installed {
		got = append(got, it.Slug)
	}
	slices.Sort(got)
	if want := []string{"ferrite-core", "scalablelux"}; !slices.Equal(got, want) {
		t.Fatalf("kurulan = %v, want %v (atlanan: %+v)", got, want, res.Skipped)
	}
	skipped := map[string]bool{}
	for _, s := range res.Skipped {
		skipped[s.Name] = true
	}
	for _, n := range []string{"Lithium", "Krypton", "ModernFix", "VMP"} {
		if !skipped[n] {
			t.Errorf("%s atlanmalıydı: %+v", n, res.Skipped)
		}
	}

	// İkinci çalışma: zaten kurulu olanlar yeniden indirilmez.
	prev := res.State
	res2, err := in.Apply(context.Background(), Request{Software: model.SoftwareFabric, MC: mc, DataDir: dir, Prev: &prev})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Installed) != 0 || len(res2.Kept) != 2 {
		t.Fatalf("ikinci çalışma: installed=%v kept=%v", res2.Installed, res2.Kept)
	}
}

// Paper'ın dosyaları ilk açılışta oluşur: önce "bekleyen", dosya oluşunca
// yazılır; kullanıcının değiştirdiği değere dokunulmaz.
func TestPaperSettingsPendingUntilFirstStart(t *testing.T) {
	dir := t.TempDir()
	in := &Installer{Online: func() bool { return false }}
	res, err := in.Apply(context.Background(), Request{Software: model.SoftwarePaper, MC: "1.21.11", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.State.Pending) == 0 || len(res.Changed) != 0 {
		t.Fatalf("pending=%v changed=%v", res.State.Pending, res.Changed)
	}
	if _, err := os.Stat(filepath.Join(dir, "spigot.yml")); !os.IsNotExist(err) {
		t.Fatal("spigot.yml ilk açılıştan önce yazılmamalı")
	}

	// "İlk açılış": sunucu varsayılan dosyaları yazdı.
	os.WriteFile(filepath.Join(dir, "spigot.yml"), []byte("settings:\n  save-user-cache-on-stop-only: false\n  debug: false\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	os.WriteFile(filepath.Join(dir, "config", "paper-world-defaults.yml"),
		[]byte("_version: 31\nenvironment:\n  optimize-explosions: false\nchunks:\n  prevent-moving-into-unloaded-chunks: true\n"), 0o644)

	prev := res.State
	res2 := in.ApplySettings(Request{Software: model.SoftwarePaper, MC: "1.21.11", DataDir: dir, Prev: &prev})
	if len(res2.State.Pending) != 0 {
		t.Fatalf("dosyalar varken bekleyen kalmamalı: %v", res2.State.Pending)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "spigot.yml"))
	if !strings.Contains(string(b), "save-user-cache-on-stop-only: true") || !strings.Contains(string(b), "debug: false") {
		t.Fatalf("spigot.yml:\n%s", b)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "config", "paper-world-defaults.yml"))
	if !strings.Contains(string(b), "optimize-explosions: true") || !strings.Contains(string(b), "_version: 31") {
		t.Fatalf("paper-world-defaults.yml:\n%s", b)
	}
}
