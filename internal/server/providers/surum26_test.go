package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// Minecraft 26.x sağlayıcı sınamaları — AĞSIZ
// ════════════════════════════════════════════════════════════════════════════
//
// testdata/ altındaki JSON'lar GERÇEK API yanıtlarından (2026-09-27)
// kırpılmış örneklerdir: piston-meta.mojang.com, fill.papermc.io v3,
// api.purpurmc.org, meta.fabricmc.net, maven.neoforged.net,
// files.minecraftforge.net. Her istek sahte bir sunucuya yönlendirilir;
// sağlayıcı kodu kendi gerçek adreslerini üretmeye devam eder, sınama o
// adreslerin DOĞRU olduğunu da denetler.

// sahteAg routes every request to one httptest server keyed by host+path.
type sahteAg struct {
	mu      sync.Mutex
	yanit   map[string]string // "host/yol" -> gövde (ya da "@dosya")
	istekte []string          // gelen "host/yol?sorgu" sırası
}

func (s *sahteAg) ekle(adres, govde string) {
	u, err := url.Parse(adres)
	if err != nil {
		panic(err)
	}
	s.yanit[u.Host+u.Path] = govde
}

func (s *sahteAg) istemci(t *testing.T) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anahtar := r.Header.Get("X-Asil-Host") + r.URL.Path
		s.mu.Lock()
		s.istekte = append(s.istekte, anahtar)
		govde, ok := s.yanit[anahtar]
		s.mu.Unlock()
		if !ok {
			http.Error(w, `{"ok":false,"error":"version_not_found"}`, http.StatusNotFound)
			return
		}
		if dosya, ok := strings.CutPrefix(govde, "@"); ok {
			b, err := os.ReadFile(filepath.Join("testdata", dosya))
			if err != nil {
				t.Errorf("fikstür %s: %v", dosya, err)
				http.Error(w, err.Error(), 500)
				return
			}
			govde = string(b)
		}
		_, _ = w.Write([]byte(govde))
	}))
	t.Cleanup(srv.Close)
	hedef, _ := url.Parse(srv.URL)
	return &http.Client{Transport: yonlendir{hedef: hedef}}
}

func (s *sahteAg) istendi(adres string) bool {
	u, _ := url.Parse(adres)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.istekte {
		if k == u.Host+u.Path {
			return true
		}
	}
	return false
}

type yonlendir struct{ hedef *url.URL }

func (y yonlendir) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("X-Asil-Host", r.URL.Host)
	r2.URL.Scheme, r2.URL.Host, r2.Host = y.hedef.Scheme, y.hedef.Host, y.hedef.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func yeniAg() *sahteAg { return &sahteAg{yanit: map[string]string{}} }

// cevrimdisiDepoGecici keeps downloadTo away from the host's /data/artifacts.
func cevrimdisiDepoGecici(t *testing.T) {
	t.Helper()
	eski := CacheDir
	CacheDir = t.TempDir()
	t.Cleanup(func() { CacheDir = eski })
}

func jarIcerigi(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "server.jar"))
	if err != nil {
		t.Fatalf("server.jar yazılmadı: %v", err)
	}
	return string(b)
}

// Vanilla 26.3: manifestte kimlik bulunur, sürüm JSON'undaki sunucu adresi
// indirilir. Sürüme özgü bir ayrıştırma yok; 26.x'in de aynı yoldan geçtiği
// sabitleniyor.
func TestVanilla26Kurulur(t *testing.T) {
	cevrimdisiDepoGecici(t)
	ag := yeniAg()
	ag.ekle(mojangManifestURL, "@mojang_manifest.json")
	ag.ekle("https://piston-meta.mojang.com/v1/packages/bc098d111a72e9f6178801544a42099bdfbb0cf2/26.3.json", "@mojang_26.3.json")
	jar := "https://piston-data.mojang.com/v1/objects/33680f5f2ac32864d6d7cf5e56a705fdb3e05f4c/server.jar"
	ag.ekle(jar, "vanilla-26.3")
	dir := t.TempDir()
	res, err := vanillaProvider{}.Install(context.Background(), "26.3", dir, "", ag.istemci(t), nil)
	if err != nil {
		t.Fatalf("vanilla 26.3: %v (istekler %v)", err, ag.istekte)
	}
	if res.JarFile != "server.jar" || jarIcerigi(t, dir) != "vanilla-26.3" || !ag.istendi(jar) {
		t.Fatalf("vanilla 26.3 yanlış kuruldu: %+v", res)
	}
}

// Paper 26.3'te (2026-09-27) YALNIZCA ALPHA derlemeler var: kararlı yoksa en
// yeni derleme kurulmalı, "derleme yok" denmemeli. 1.21.11'de kararlı seçilir.
func TestPaper26DerlemeSecimi(t *testing.T) {
	cevrimdisiDepoGecici(t)
	ag := yeniAg()
	ag.ekle(paperAPI+"/paper/versions/26.3/builds", "@paper_26.3_builds.json")
	ag.ekle(paperAPI+"/paper/versions/1.21.11/builds", "@paper_1.21.11_builds.json")
	j263 := "https://fill-data.papermc.io/v1/objects/dd64988a011729e6812f2fff1be32ba5c572ecdc8890e6abc7a56aa91b53f77d/paper-26.3-49.jar"
	j1211 := "https://fill-data.papermc.io/v1/objects/5ffef465eeeb5f2a3c23a24419d97c51afd7dbb4923ff42df9a3f58bba1ccfba/paper-1.21.11-132.jar"
	ag.ekle(j263, "paper-26.3-49")
	cl := ag.istemci(t)
	p := paperLikeProvider{project: "paper"}

	dir := t.TempDir()
	if _, err := p.Install(context.Background(), "26.3", dir, "", cl, nil); err != nil {
		t.Fatalf("paper 26.3: %v", err)
	}
	if jarIcerigi(t, dir) != "paper-26.3-49" {
		t.Fatal("paper 26.3: en yeni (ALPHA 49) derleme indirilmedi")
	}

	ag.ekle(j1211, "paper-1.21.11-132")
	dir = t.TempDir()
	if _, err := p.Install(context.Background(), "1.21.11", dir, "", cl, nil); err != nil {
		t.Fatalf("paper 1.21.11: %v", err)
	}
	if jarIcerigi(t, dir) != "paper-1.21.11-132" {
		t.Fatal("paper 1.21.11: en yeni STABLE derleme indirilmedi")
	}
}

// Paper "26.1"i YAYIMLAMIYOR (yalnızca 26.1.1 ve 26.1.2; 404
// version_not_found). Hata iletisi desteklenen TAM sürümleri, en yeni önce,
// göstermeli. Eski sıralama "26.3-rc-3, 26.3, 26.2-rc-2, …, 1.21.9-rc1"
// veriyordu ve 1.21.11 ilk sekizde yoktu.
func TestPaper26DesteklenmeyenSurumIletisi(t *testing.T) {
	cevrimdisiDepoGecici(t)
	ag := yeniAg()
	ag.ekle(paperAPI+"/paper", "@paper_project.json")
	_, err := paperLikeProvider{project: "paper"}.Install(context.Background(), "26.1", t.TempDir(), "", ag.istemci(t), nil)
	if err == nil {
		t.Fatal("paper 26.1 kurulmamalıydı (Paper bu sürümü yayımlamıyor)")
	}
	want := "Desteklenen sürümler: 26.3, 26.2, 26.1.2, 26.1.1, 1.21.11, 1.21.10, 1.21.9, 1.21.8, …"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("ileti:\n %s\nşunu içermeliydi:\n %s", err, want)
	}
}

// Purpur indirme adresi DERLEME NUMARASINI taşımalı: çevrimdışı depo URL'e
// göre anahtarlıyor, ".../latest/download" hep ilk indirilen derlemeyi verirdi.
func TestPurpur26DerlemeAdresi(t *testing.T) {
	cevrimdisiDepoGecici(t)
	ag := yeniAg()
	ag.ekle(purpurAPI+"/26.3", "@purpur_26.3.json")
	ag.ekle(purpurAPI+"/26.3/2641/download", "purpur-26.3-2641")
	dir := t.TempDir()
	if _, err := (purpurProvider{}).Install(context.Background(), "26.3", dir, "", ag.istemci(t), nil); err != nil {
		t.Fatalf("purpur 26.3: %v (istekler %v)", err, ag.istekte)
	}
	if jarIcerigi(t, dir) != "purpur-26.3-2641" {
		t.Fatal("purpur: 2641 derlemesi indirilmedi")
	}
}

// Fabric 26.3: gerçek meta yanıtında intermediary "0.0.0" (26.x gizlenmemiş);
// başlatıcı adresi yükleyici + kurucu sürümünden kurulur.
func TestFabric26BaslaticiAdresi(t *testing.T) {
	cevrimdisiDepoGecici(t)
	ag := yeniAg()
	ag.ekle(fabricMeta+"/versions/loader/26.3", "@fabric_loader_26.3.json")
	ag.ekle(fabricMeta+"/versions/installer", "@fabric_installer.json")
	jar := fabricMeta + "/versions/loader/26.3/0.19.5/1.1.2/server/jar"
	ag.ekle(jar, "fabric-26.3")
	dir := t.TempDir()
	if _, err := (fabricProvider{}).Install(context.Background(), "26.3", dir, "", ag.istemci(t), nil); err != nil {
		t.Fatalf("fabric 26.3: %v (istekler %v)", err, ag.istekte)
	}
	if jarIcerigi(t, dir) != "fabric-26.3" || !ag.istendi(jar) {
		t.Fatal("fabric 26.3 başlatıcısı yanlış adresten indirildi")
	}
}

// NeoForge: 26.x yeni bir numaralama kullanıyor (26.3 -> 26.3.0.N-beta) ve
// eski kod "1." ile başlamayanı reddediyordu; 1.x'te de metin sıralaması
// yanlış derleme seçiyordu (21.1.99 / 21.11.9-beta).
func TestNeoForgeDerlemeSecimi(t *testing.T) {
	ag := yeniAg()
	ag.ekle(neoforgeVersionsAPI, "@neoforge_versions.json")
	cl := ag.istemci(t)
	for mc, want := range map[string]string{
		"26.3":    "26.3.0.23-beta", // henüz yalnızca beta
		"26.2":    "26.2.0.88",
		"26.1.2":  "26.1.2.111",
		"26.1":    "26.1.0.19-beta", // "+snapshot" alfaları atlanır
		"1.21.11": "21.11.45",       // kararlı, betadan önce
		"1.21.1":  "21.1.252",       // sayı, metin değil
		"1.21":    "21.0.167",
	} {
		url, ver, err := neoforgeInstaller(context.Background(), cl, mc)
		if err != nil {
			t.Errorf("neoforge %s: %v", mc, err)
			continue
		}
		wantURL := neoforgeMaven + "/" + want + "/neoforge-" + want + "-installer.jar"
		if ver != want || url != wantURL {
			t.Errorf("neoforge %s: %s (%s); istenen %s", mc, ver, url, want)
		}
	}
	if _, _, err := neoforgeInstaller(context.Background(), cl, "1.19.2"); err == nil {
		t.Error("NeoForge'da olmayan 1.19.2 için hata beklenirdi")
	}
}

// Forge promosyonları 26.x için de Minecraft sürümüyle anahtarlı
// ("26.3-latest": "66.0.4"); önerilen yoksa en yeniye düşülür.
func TestForge26KurucuAdresi(t *testing.T) {
	ag := yeniAg()
	ag.ekle(forgePromos, "@forge_promos.json")
	cl := ag.istemci(t)
	for mc, want := range map[string]string{
		"26.3":    "26.3-66.0.4",    // yalnızca latest
		"26.1.2":  "26.1.2-64.1.0",  // recommended
		"1.21.11": "1.21.11-61.2.0", // recommended
	} {
		url, full, err := forgeInstaller(context.Background(), cl, mc)
		if err != nil || full != want || url != forgeMaven+"/"+want+"/forge-"+want+"-installer.jar" {
			t.Errorf("forge %s: %s %s %v; istenen %s", mc, full, url, err, want)
		}
	}
}
