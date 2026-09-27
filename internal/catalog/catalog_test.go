package catalog

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// SAHTE MODRINTH — AĞSIZ
// ════════════════════════════════════════════════════════════════════════════
//
// Testler hiçbir soket açmaz: http.Client'ın Transport'u süreç içinde yanıt
// üretir. Veri, gerçek API'den ÖLÇÜLEN durumu taklit eder (MC 1.21.1):
//
//	worldedit  sürümleri yalnızca bukkit/paper/spigot etiketli
//	           (loaders=["purpur"] -> 0, ["folia"] -> 0, ["paper"] -> 6)
//	viaversion en yeni yapı SNAPSHOT (beta), kararlı olan 5.12.0
//
// Sahte sunucu Modrinth'in süzgeç anlamını birebir uygular: gruplar VE,
// grup içi VEYA; loaders parametresi VEYA.

type fakeProject struct {
	Project
	types    []string // project_type süzgecinin eşleştiği türler
	versions []string // aramada "versions:" süzgecinin eşleştiği MC sürümleri
}

type fakeModrinth struct {
	mu       sync.Mutex
	projects []fakeProject
	versions map[string][]Version // slug -> sürümler
	files    map[string][]byte    // URL -> içerik
	requests []string             // gelen her isteğin URL'si (sıra korunur)
	fail     error                // doluysa her istek bu taşıma hatasıyla düşer
}

func (f *fakeModrinth) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req.URL.String())
	fail := f.fail
	f.mu.Unlock()
	if fail != nil {
		return nil, fail
	}

	if body, ok := f.files[req.URL.String()]; ok {
		return respond(http.StatusOK, body), nil
	}
	p := req.URL.Path
	switch {
	case p == "/v2/search":
		return f.search(req)
	case strings.HasPrefix(p, "/v2/project/") && strings.HasSuffix(p, "/version"):
		slug := strings.TrimSuffix(strings.TrimPrefix(p, "/v2/project/"), "/version")
		return f.projectVersions(req, slug)
	}
	return respond(http.StatusNotFound, []byte(`{"error":"not_found"}`)), nil
}

func respond(code int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: code,
		Status:     http.StatusText(code),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (f *fakeModrinth) search(req *http.Request) (*http.Response, error) {
	var groups [][]string
	if raw := req.URL.Query().Get("facets"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &groups); err != nil {
			return respond(http.StatusBadRequest, []byte(err.Error())), nil
		}
	}
	query := strings.ToLower(req.URL.Query().Get("query"))
	var hits []Project
	for _, fp := range f.projects {
		if query != "" && !strings.Contains(strings.ToLower(fp.Slug+" "+fp.Title), query) {
			continue
		}
		ok := true
		for _, g := range groups {
			any := false
			for _, facet := range g {
				k, v, _ := strings.Cut(facet, ":")
				switch k {
				case "project_type":
					any = any || contains(fp.types, v)
				case "categories":
					any = any || contains(fp.Categories, v)
				case "versions":
					any = any || contains(fp.versions, v)
				}
			}
			ok = ok && any
		}
		if ok {
			hits = append(hits, fp.Project)
		}
	}
	body, _ := json.Marshal(map[string]any{"hits": hits, "total_hits": len(hits)})
	return respond(http.StatusOK, body), nil
}

func (f *fakeModrinth) projectVersions(req *http.Request, slug string) (*http.Response, error) {
	vs, ok := f.versions[slug]
	if !ok {
		return respond(http.StatusNotFound, []byte(`{"error":"not_found"}`)), nil
	}
	var loaders, gvs []string
	if raw := req.URL.Query().Get("loaders"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &loaders)
	}
	if raw := req.URL.Query().Get("game_versions"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &gvs)
	}
	out := []Version{}
	for _, v := range vs {
		if len(loaders) > 0 {
			hit := false
			for _, l := range loaders {
				hit = hit || contains(v.Loaders, l)
			}
			if !hit {
				continue
			}
		}
		if len(gvs) > 0 {
			hit := false
			for _, g := range gvs {
				hit = hit || contains(v.GameVersions, g)
			}
			if !hit {
				continue
			}
		}
		out = append(out, v)
	}
	body, _ := json.Marshal(out)
	return respond(http.StatusOK, body), nil
}

func (f *fakeModrinth) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeModrinth) lastRequest() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return ""
	}
	return f.requests[len(f.requests)-1]
}

const cdn = "https://cdn.test/data/"

func day(n int) time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n) }

// jarFile builds a downloadable file with a correct sha512.
func (f *fakeModrinth) jarFile(name string, body []byte) File {
	sum := sha512.Sum512(body)
	u := cdn + name
	f.files[u] = body
	return File{URL: u, Filename: name, Primary: true, Size: int64(len(body)),
		Hashes: map[string]string{"sha512": hex.EncodeToString(sum[:])}}
}

// newFake builds the measured-state fixture.
func newFake() *fakeModrinth {
	f := &fakeModrinth{versions: map[string][]Version{}, files: map[string][]byte{}}
	bpS := []string{"bukkit", "paper", "spigot"}

	f.versions["worldedit"] = []Version{
		{ID: "we-beta", VersionNumber: "7.3.10-beta-01", VersionType: "beta",
			GameVersions: []string{"1.21", "1.21.1"}, Loaders: bpS, DatePublished: day(40),
			Files: []File{f.jarFile("worldedit-bukkit-7.3.10-beta-01.jar", []byte("we-beta"))}},
		{ID: "we-739", VersionNumber: "7.3.9", VersionType: "release",
			GameVersions: []string{"1.21", "1.21.1"}, Loaders: bpS, DatePublished: day(30),
			Files: []File{f.jarFile("worldedit-bukkit-7.3.9.jar", []byte("we-739"))}},
		{ID: "we-fabric", VersionNumber: "7.3.8", VersionType: "release",
			GameVersions: []string{"1.21.1"}, Loaders: []string{"fabric", "neoforge"}, DatePublished: day(20),
			Files: []File{f.jarFile("worldedit-mod-7.3.8.jar", []byte("we-fabric"))}},
	}
	viaLoaders := []string{"fabric", "folia", "paper", "velocity"}
	f.versions["viaversion"] = []Version{
		{ID: "via-snap", VersionNumber: "5.12.1-SNAPSHOT+1069", VersionType: "beta",
			GameVersions: []string{"1.21.1"}, Loaders: viaLoaders, DatePublished: day(90),
			Files: []File{f.jarFile("ViaVersion-5.12.1-SNAPSHOT.jar", []byte("via-snap"))}},
		{ID: "via-5120", VersionNumber: "5.12.0", VersionType: "release",
			GameVersions: []string{"1.21.1"}, Loaders: viaLoaders, DatePublished: day(88),
			Files: []File{f.jarFile("ViaVersion-5.12.0.jar", []byte("via-5120"))}},
	}
	f.projects = []fakeProject{
		{Project: Project{Slug: "worldedit", Title: "WorldEdit", Downloads: 10767750,
			ProjectType: "mod", Categories: []string{"bukkit", "fabric", "folia", "forge", "paper", "spigot", "utility"}},
			types: []string{"plugin", "mod"}, versions: []string{"1.21", "1.21.1"}},
		{Project: Project{Slug: "worldedit-selection-viewer", Title: "WorldEdit Selection Viewer",
			ProjectType: "mod", Categories: []string{"paper", "purpur", "utility"}},
			types: []string{"plugin"}, versions: []string{"1.21.1"}},
		{Project: Project{Slug: "worldedit-legacy", Title: "WorldEdit Legacy Tools",
			ProjectType: "mod", Categories: []string{"bukkit", "spigot"}},
			types: []string{"plugin"}, versions: []string{"1.20.4"}},
	}
	return f
}

func newTestClient(f *fakeModrinth) *Client {
	return NewWith(&http.Client{Transport: f}, "https://api.test/v2")
}

// ════════════════════════════════════════════════════════════════════════════
// YÜKLEYİCİ ZİNCİRİ
// ════════════════════════════════════════════════════════════════════════════

func TestLoaderChain(t *testing.T) {
	cases := map[string][]string{
		"purpur":   {"purpur", "paper", "spigot", "bukkit"},
		"paper":    {"paper", "spigot", "bukkit"},
		"spigot":   {"spigot", "bukkit"},
		"bukkit":   {"bukkit"},
		"folia":    {"folia", "paper"},
		"quilt":    {"quilt", "fabric"},
		"fabric":   {"fabric"},
		"neoforge": {"neoforge"},
		"":         nil,
	}
	for in, want := range cases {
		got := LoaderChain(in)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("LoaderChain(%q) = %v, %v bekleniyordu", in, got, want)
		}
	}
	// Dönen dilim paylaşılan tabloyu DEĞİŞTİREMEMELİ.
	LoaderChain("purpur")[0] = "bozuk"
	if LoaderChain("purpur")[0] != "purpur" {
		t.Error("LoaderChain iç tabloyu dışarı sızdırıyor")
	}
}

// GERÇEK HATA: purpur sunucusunda WorldEdit "uyumlu dosya yok" diye
// kurulamıyordu (ölçüldü: loaders=["purpur"] -> 0 sürüm).
func TestResolvePurpurFallsBackToPaper(t *testing.T) {
	f := newFake()
	c := newTestClient(f)

	r, err := c.Resolve(context.Background(), "worldedit", "purpur", "1.21.1", ResolveOptions{})
	if err != nil {
		t.Fatalf("purpur sunucusu için WorldEdit çözümlenemedi: %v", err)
	}
	if r.Loader != "bukkit" && r.Loader != "paper" && r.Loader != "spigot" {
		t.Fatalf("yükleyici %q — zincirden bir halka bekleniyordu", r.Loader)
	}
	// Zincirin EN ÖZGÜL halkası (paper) seçilmeli.
	if r.Loader != "paper" {
		t.Errorf("seçilen halka %q, paper bekleniyordu", r.Loader)
	}
	if !r.FellBack() {
		t.Error("FellBack() false — geri düşüş bildirilmiyor")
	}
	if d := r.Describe(); !strings.Contains(d, "purpur için ayrı sürüm yok") || !strings.Contains(d, "paper") {
		t.Errorf("mesaj hangi yükleyiciyle bulunduğunu söylemiyor: %q", d)
	}
	// Tek istek: zincir halka halka sorulmamalı.
	if n := f.requestCount(); n != 1 {
		t.Errorf("%d istek yapıldı, 1 bekleniyordu", n)
	}
	if !strings.Contains(f.lastRequest(), "purpur") || !strings.Contains(f.lastRequest(), "bukkit") {
		t.Errorf("istek zincirin tamamını sormuyor: %s", f.lastRequest())
	}
}

func TestResolveFoliaAndQuiltFallBack(t *testing.T) {
	f := newFake()
	c := newTestClient(f)

	r, err := c.Resolve(context.Background(), "worldedit", "folia", "1.21.1", ResolveOptions{})
	if err != nil {
		t.Fatalf("folia: %v", err)
	}
	if r.Loader != "paper" {
		t.Errorf("folia -> %q, paper bekleniyordu", r.Loader)
	}
	if !strings.Contains(r.Describe(), "folia-supported") {
		t.Errorf("Folia'nın yükleme kısıtı mesajda yok: %q", r.Describe())
	}

	r, err = c.Resolve(context.Background(), "worldedit", "quilt", "1.21.1", ResolveOptions{})
	if err != nil {
		t.Fatalf("quilt: %v", err)
	}
	if r.Loader != "fabric" || r.File.Filename != "worldedit-mod-7.3.8.jar" {
		t.Errorf("quilt -> %q/%s, fabric/worldedit-mod-7.3.8.jar bekleniyordu", r.Loader, r.File.Filename)
	}
}

// Zincirin dışına ASLA taşmamalı: Forge sunucusuna Fabric yapısı kurulmaz.
func TestResolveNeverCrossesChain(t *testing.T) {
	c := newTestClient(newFake())
	_, err := c.Resolve(context.Background(), "worldedit", "forge", "1.21.1", ResolveOptions{})
	if !errors.Is(err, ErrNoCompatible) {
		t.Fatalf("forge için hata %v, ErrNoCompatible bekleniyordu", err)
	}
}

// Kendi yükleyicisi etiketli bir yapı varsa, daha YENİ olsa bile genel halka
// seçilmemeli.
func TestResolvePrefersMostSpecificLoader(t *testing.T) {
	f := newFake()
	f.versions["tool"] = []Version{
		{VersionNumber: "2.0", VersionType: "release", GameVersions: []string{"1.21.1"},
			Loaders: []string{"paper"}, DatePublished: day(50),
			Files: []File{f.jarFile("tool-paper-2.0.jar", []byte("p"))}},
		{VersionNumber: "1.9", VersionType: "release", GameVersions: []string{"1.21.1"},
			Loaders: []string{"purpur"}, DatePublished: day(10),
			Files: []File{f.jarFile("tool-purpur-1.9.jar", []byte("u"))}},
	}
	c := newTestClient(f)
	r, err := c.Resolve(context.Background(), "tool", "purpur", "1.21.1", ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Loader != "purpur" || r.FellBack() {
		t.Errorf("purpur sunucusu %q yapısını aldı, purpur bekleniyordu", r.Loader)
	}
	r, err = c.Resolve(context.Background(), "tool", "paper", "1.21.1", ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.File.Filename != "tool-paper-2.0.jar" {
		t.Errorf("paper sunucusu %s aldı — purpur'a özgü yapı paper'a verilmemeli", r.File.Filename)
	}
}

// GERÇEK HATA: kararlı 7.3.9 varken 7.3.10-beta-01 kuruluyordu.
func TestResolvePrefersReleaseOverNewerBeta(t *testing.T) {
	c := newTestClient(newFake())
	r, err := c.Resolve(context.Background(), "worldedit", "paper", "1.21.1", ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version.VersionNumber != "7.3.9" {
		t.Errorf("seçilen %s, kararlı 7.3.9 bekleniyordu", r.Version.VersionNumber)
	}

	// Kararlı sürüm HİÇ yoksa beta yine de seçilebilmeli.
	f := newFake()
	f.versions["betaonly"] = []Version{
		{VersionNumber: "0.1-beta", VersionType: "beta", GameVersions: []string{"1.21.1"},
			Loaders: []string{"paper"}, DatePublished: day(5),
			Files: []File{f.jarFile("betaonly.jar", []byte("b"))}},
	}
	r, err = newTestClient(f).Resolve(context.Background(), "betaonly", "paper", "1.21.1", ResolveOptions{})
	if err != nil || r.Version.VersionNumber != "0.1-beta" {
		t.Errorf("yalnızca beta varken: %v %q", err, r.Version.VersionNumber)
	}
}

// Taşıma hatasında zincir DÖNGÜYE girmemeli: ağ yokken dört kat bekleme olmaz.
func TestResolveTransportErrorIsNotNoCompatible(t *testing.T) {
	f := newFake()
	f.fail = errors.New("dial tcp: no route to host")
	_, err := newTestClient(f).Resolve(context.Background(), "worldedit", "purpur", "1.21.1", ResolveOptions{})
	if err == nil || errors.Is(err, ErrNoCompatible) {
		t.Fatalf("taşıma hatası %v olarak döndü", err)
	}
	if n := f.requestCount(); n != 1 {
		t.Errorf("ağ hatasında %d istek yapıldı, 1 bekleniyordu", n)
	}
}

// Sürüm süzgeci kaldırıldığında da sunucunun sürüm ÇİZGİSİ tercih edilmeli.
func TestResolveAnyGameVersionPrefersSameLine(t *testing.T) {
	f := newFake()
	f.versions["oldplug"] = []Version{
		{VersionNumber: "3.0", VersionType: "release", GameVersions: []string{"1.20.4"},
			Loaders: []string{"paper"}, DatePublished: day(60),
			Files: []File{f.jarFile("oldplug-3.0.jar", []byte("3"))}},
		{VersionNumber: "2.5", VersionType: "release", GameVersions: []string{"1.21"},
			Loaders: []string{"paper"}, DatePublished: day(30),
			Files: []File{f.jarFile("oldplug-2.5.jar", []byte("2"))}},
	}
	c := newTestClient(f)
	if _, err := c.Resolve(context.Background(), "oldplug", "paper", "1.21.1", ResolveOptions{}); !errors.Is(err, ErrNoCompatible) {
		t.Fatalf("sürüm süzgeciyle hata %v, ErrNoCompatible bekleniyordu", err)
	}
	r, err := c.Resolve(context.Background(), "oldplug", "paper", "1.21.1", ResolveOptions{AnyGameVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version.VersionNumber != "2.5" {
		t.Errorf("seçilen %s; 1.21 çizgisindeki 2.5 bekleniyordu", r.Version.VersionNumber)
	}
	if r.ExactGameVersion || !strings.Contains(r.Describe(), "etiketlenmemiş") {
		t.Errorf("tam eşleşmeyen sürüm bildirilmiyor: exact=%v %q", r.ExactGameVersion, r.Describe())
	}
	if strings.Contains(f.lastRequest(), "game_versions") {
		t.Errorf("süzgeç kaldırıldığı hâlde istekte game_versions var: %s", f.lastRequest())
	}
}

// ════════════════════════════════════════════════════════════════════════════
// ARAMA
// ════════════════════════════════════════════════════════════════════════════

// GERÇEK HATA: purpur ile "worldedit" aranınca WorldEdit'in kendisi YOKTU.
func TestSearchCompatiblePurpurFindsPaperOnlyProjects(t *testing.T) {
	f := newFake()
	c := newTestClient(f)
	res, err := c.SearchCompatible(context.Background(), SearchQuery{
		Query: "worldedit", ProjectType: "plugin", Loader: "purpur", GameVersion: "1.21.1", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	loaders := map[string]string{}
	for _, h := range res.Hits {
		loaders[h.Slug] = h.Loader
	}
	if got, ok := loaders["worldedit"]; !ok {
		t.Fatalf("WorldEdit purpur aramasında yok: %v", loaders)
	} else if got != "paper" {
		t.Errorf("WorldEdit %q halkasıyla işaretlendi, paper bekleniyordu", got)
	}
	if loaders["worldedit-selection-viewer"] != "purpur" {
		t.Errorf("purpur etiketli proje %q ile işaretlendi", loaders["worldedit-selection-viewer"])
	}
	if _, ok := loaders["worldedit-legacy"]; ok {
		t.Error("1.21.1 için olmayan proje sürüm süzgecine rağmen geldi")
	}
	if strings.Join(res.Loaders, "/") != "purpur/paper/spigot/bukkit" || res.GameVersion != "1.21.1" {
		t.Errorf("uygulanan süzgeçler yanlış bildiriliyor: %v %q", res.Loaders, res.GameVersion)
	}
	if res.Total != len(res.Hits) {
		t.Errorf("toplam %d, %d sonuç", res.Total, len(res.Hits))
	}
}

func TestSearchWithoutVersionFilter(t *testing.T) {
	f := newFake()
	c := newTestClient(f)
	res, err := c.SearchCompatible(context.Background(), SearchQuery{
		Query: "worldedit", ProjectType: "plugin", Loader: "spigot"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range res.Hits {
		found = found || h.Slug == "worldedit-legacy"
	}
	if !found {
		t.Error("sürüm süzgeci kaldırıldığında eski sürüm projesi gelmedi")
	}
	if res.GameVersion != "" || strings.Contains(f.lastRequest(), "versions%3A") {
		t.Errorf("süzgeç kaldırıldığı hâlde sürüm facet'i gönderildi: %s", f.lastRequest())
	}
}

func TestFacetsGroupSemantics(t *testing.T) {
	got := facets([]string{"project_type:plugin"}, []string{"categories:purpur", "categories:paper"}, nil, []string{""})
	want := `[["project_type:plugin"],["categories:purpur","categories:paper"]]`
	if got != want {
		t.Errorf("facets = %s, %s bekleniyordu", got, want)
	}
	if facets() != "" {
		t.Error("boş facets boş dizgi vermeli")
	}
}

// ════════════════════════════════════════════════════════════════════════════
// KURULUM / İNDİRME
// ════════════════════════════════════════════════════════════════════════════

// compat.go'nun yolu: purpur sunucusuna ViaVersion KARARLI sürümüyle kurulmalı.
func TestInstallByIDViaVersionOnPurpur(t *testing.T) {
	f := newFake()
	dir := t.TempDir()
	p, err := newTestClient(f).InstallByID(context.Background(), "viaversion", "purpur", "1.21.1", dir)
	if err != nil {
		t.Fatalf("purpur sunucusuna ViaVersion kurulamadı: %v", err)
	}
	if filepath.Base(p) != "ViaVersion-5.12.0.jar" {
		t.Errorf("kurulan %s, kararlı ViaVersion-5.12.0.jar bekleniyordu", filepath.Base(p))
	}
	if b, _ := os.ReadFile(p); string(b) != "via-5120" {
		t.Errorf("dosya içeriği yanlış: %q", b)
	}
	assertNoPartFiles(t, dir)
}

func assertNoPartFiles(t *testing.T, dir string) {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("klasörde yarım dosya kaldı: %s", e.Name())
		}
	}
}

// GERÇEK HATA: bozuk/yarım indirme plugins/ içine .jar olarak yazılıyordu.
func TestDownloadRejectsCorruptFile(t *testing.T) {
	f := newFake()
	c := newTestClient(f)
	dir := t.TempDir()

	good := f.jarFile("good.jar", []byte("tam içerik"))
	bad := good
	bad.Filename = "bad.jar"
	bad.URL = cdn + "bad.jar"
	f.files[bad.URL] = []byte("bozuk içrk") // aynı boy, farklı özet

	if _, err := c.DownloadTo(context.Background(), bad, dir); err == nil {
		t.Fatal("özeti uyuşmayan dosya kabul edildi")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.jar")); !os.IsNotExist(err) {
		t.Error("bozuk dosya plugins klasörüne yazıldı")
	}

	short := good
	short.Filename = "short.jar"
	short.URL = cdn + "short.jar"
	short.Hashes = nil
	f.files[short.URL] = []byte("yarım")
	if _, err := c.DownloadTo(context.Background(), short, dir); err == nil {
		t.Fatal("eksik inen dosya kabul edildi")
	}
	if _, err := os.Stat(filepath.Join(dir, "short.jar")); !os.IsNotExist(err) {
		t.Error("yarım dosya plugins klasörüne yazıldı")
	}

	if p, err := c.DownloadTo(context.Background(), good, dir); err != nil {
		t.Fatalf("sağlam dosya reddedildi: %v", err)
	} else if b, _ := os.ReadFile(p); string(b) != "tam içerik" {
		t.Errorf("içerik %q", b)
	}
	assertNoPartFiles(t, dir)
}

// Sonuç mesajı hangi yükleyiciyle bulunduğunu söylemeli: purpur sunucusuna
// kurulan WorldEdit "worldedit (paper uyumlu) kuruldu" demeli. Paper
// sunucusunda aynı ek YAZILMAMALI — orada bilgi değil gürültü olur.
func TestInstallSummaryNamesTheFallbackLoader(t *testing.T) {
	f := newFake()
	c := newTestClient(f)

	in, err := c.Install(context.Background(), "worldedit", "purpur", "1.21.1", t.TempDir(), ResolveOptions{})
	if err != nil {
		t.Fatalf("purpur: %v", err)
	}
	if got, want := in.Summary("worldedit"), "worldedit (paper uyumlu) kuruldu"; got != want {
		t.Errorf("purpur özeti %q, %q bekleniyordu", got, want)
	}

	in, err = c.Install(context.Background(), "worldedit", "paper", "1.21.1", t.TempDir(), ResolveOptions{})
	if err != nil {
		t.Fatalf("paper: %v", err)
	}
	if got, want := in.Summary("worldedit"), "worldedit kuruldu"; got != want {
		t.Errorf("paper özeti %q, %q bekleniyordu", got, want)
	}

	// Sürüm süzgeci kaldırılıp etiketsiz bir yapı seçildiyse bu da söylenmeli.
	r := Resolved{Loader: "paper", Wanted: "paper", GameVersion: "1.21.4", ExactGameVersion: false}
	if s := r.Summary("worldedit"); !strings.Contains(s, "1.21.4 için etiketlenmemiş") {
		t.Errorf("etiketsiz sürüm uyarısı yok: %q", s)
	}
}
