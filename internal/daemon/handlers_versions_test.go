package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/log"
	"mcos/internal/mcver"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// server.versions sınamaları — AĞSIZ
// ════════════════════════════════════════════════════════════════════════════
//
// Gövdeler providers/testdata altındaki liste_*.json fikstürleri: GERÇEK API
// yanıtlarından (2026-09-27) kırpılmış, aynı biçimde. Her istek sahte bir
// sunucuya yönlendirilir; "kapalı" ana makinelere bağlantı hiç kurulmaz (ağ
// yok). Adresler gerçek adreslerdir: sınama sağlayıcıların doğru yere
// gittiğini de denetler.

const (
	adresMojang = "launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	adresPaper  = "fill.papermc.io/v3/projects/paper"
	adresFolia  = "fill.papermc.io/v3/projects/folia"
	adresFabric = "meta.fabricmc.net/v2/versions/game"
	adresSpigot = "hub.spigotmc.org/versions/"
)

type sahteSurumAgi struct {
	mu     sync.Mutex
	yanit  map[string]string // "host/yol" -> gövde
	kapali map[string]bool   // bu ana makinelere bağlanılamaz
	deneme map[string]int    // "host/yol" -> deneme sayısı (kapalılar dahil)
}

func (s *sahteSurumAgi) denendi(adres string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deneme[adres]
}

func (s *sahteSurumAgi) kapat(host string) {
	s.mu.Lock()
	s.kapali[host] = true
	s.mu.Unlock()
}

type surumYonlendir struct {
	ag    *sahteSurumAgi
	hedef *url.URL
}

func (y surumYonlendir) RoundTrip(r *http.Request) (*http.Response, error) {
	y.ag.mu.Lock()
	y.ag.deneme[r.URL.Host+r.URL.Path]++
	kapali := y.ag.kapali[r.URL.Host]
	y.ag.mu.Unlock()
	if kapali {
		return nil, errors.New("dial tcp: connect: network is unreachable")
	}
	r2 := r.Clone(r.Context())
	r2.Header.Set("X-Asil-Adres", r.URL.Host+r.URL.Path)
	r2.URL.Scheme, r2.URL.Host, r2.Host = y.hedef.Scheme, y.hedef.Host, y.hedef.Host
	return http.DefaultTransport.RoundTrip(r2)
}

// fikstur reads one of the providers' real-shape fixtures.
func fikstur(t *testing.T, ad string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "server", "providers", "testdata", ad))
	if err != nil {
		t.Fatalf("fikstür %s: %v", ad, err)
	}
	return string(b)
}

// surumAgiKur routes surumIstemcisi to a fake network and empties the cache.
func surumAgiKur(t *testing.T) *sahteSurumAgi {
	t.Helper()
	ag := &sahteSurumAgi{
		yanit: map[string]string{
			adresMojang: fikstur(t, "liste_mojang_manifest.json"),
			adresPaper:  fikstur(t, "paper_project.json"),
			adresFolia:  fikstur(t, "liste_folia_project.json"),
			adresFabric: fikstur(t, "liste_fabric_game.json"),
			adresSpigot: fikstur(t, "liste_spigot_versions.html"),
		},
		kapali: map[string]bool{},
		deneme: map[string]int{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ag.mu.Lock()
		govde, ok := ag.yanit[r.Header.Get("X-Asil-Adres")]
		ag.mu.Unlock()
		if !ok {
			http.Error(w, `{"ok":false,"error":"not_found"}`, http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, govde)
	}))
	t.Cleanup(srv.Close)
	hedef, _ := url.Parse(srv.URL)

	eskiIstemci := surumIstemcisi
	surumIstemcisi = &http.Client{Transport: surumYonlendir{ag: ag, hedef: hedef}}
	verCache.mu.Lock()
	eskiKayit := verCache.kayit
	verCache.kayit = nil
	verCache.mu.Unlock()
	t.Cleanup(func() {
		surumIstemcisi = eskiIstemci
		verCache.mu.Lock()
		verCache.kayit = eskiKayit
		verCache.mu.Unlock()
	})
	return ag
}

// geriAl ages a cache entry by d, as if it had been stored d ago.
func geriAl(sw model.Software, d time.Duration) {
	verCache.mu.Lock()
	defer verCache.mu.Unlock()
	k := verCache.kayit[sw]
	k.at = k.at.Add(-d)
	verCache.kayit[sw] = k
}

func surumDaemon() *Daemon {
	return &Daemon{log: log.New(io.Discard, log.LevelError, 8)}
}

func surumIste(t *testing.T, d *Daemon, ctx context.Context, sw model.Software) ipc.ServerVersionsResult {
	t.Helper()
	raw, _ := json.Marshal(ipc.ServerVersionsParams{Software: sw})
	out, err := d.handleServerVersions(ctx, raw)
	if err != nil {
		t.Fatalf("%s: %v", sw, err)
	}
	return out.(ipc.ServerVersionsResult)
}

func ilkBes(vs []string) []string { return vs[:min(5, len(vs))] }

// Liste YAZILIMA göre gelir. Eski işleyici parametreyi okumuyordu: Paper
// için 26.1 (Paper 404 veriyor), Folia için 26.3 (Folia'nın en yenisi 26.2)
// öneriliyordu.
func TestSurumListesiYazilimaGore(t *testing.T) {
	surumAgiKur(t)
	d := surumDaemon()
	ctx := context.Background()

	paper := surumIste(t, d, ctx, model.SoftwarePaper)
	if got := strings.Join(ilkBes(paper.Versions), " "); got != "26.3 26.2 26.1.2 26.1.1 1.21.11" {
		t.Errorf("paper ilk beş: %s", got)
	}
	if slices.Contains(paper.Versions, "26.1") {
		t.Error(`paper listesinde "26.1" var; Paper bu sürümü yayımlamıyor (404)`)
	}
	if paper.Fallback || paper.Source != "fill.papermc.io" || paper.Latest != "26.3" {
		t.Errorf("paper yanıtı: yedek=%v kaynak=%q latest=%q", paper.Fallback, paper.Source, paper.Latest)
	}

	folia := surumIste(t, d, ctx, model.SoftwareFolia)
	if folia.Latest != "26.2" || slices.Contains(folia.Versions, "26.3") {
		t.Errorf("folia en yenisi 26.2 olmalı, 26.3 olmamalı: %v", folia.Versions)
	}

	// Fabric düz 26.1'i YAYIMLIYOR: aynı sürüm bir yazılımda var, ötekinde yok.
	fabric := surumIste(t, d, ctx, model.SoftwareFabric)
	if !slices.Contains(fabric.Versions, "26.1") || fabric.Source != "meta.fabricmc.net" {
		t.Errorf("fabric listesi: %v (kaynak %q)", fabric.Versions, fabric.Source)
	}

	// Parametresiz istek (eski istemci) = vanilla; anlık görüntü listede
	// yok ama işaretçisi duruyor.
	van := surumIste(t, d, ctx, "")
	if van.Source != "launchermeta.mojang.com" || van.LatestSnapshot != "26.4-snapshot-1" {
		t.Errorf("vanilla yanıtı: %+v", van)
	}
	for _, res := range []ipc.ServerVersionsResult{paper, folia, fabric, van} {
		for i, v := range res.Versions {
			if !mcver.IsRelease(v) {
				t.Errorf("%s listesinde tam sürüm olmayan %q", res.Source, v)
			}
			if i > 0 && !mcver.Newer(res.Versions[i-1], v) {
				t.Errorf("%s sırası bozuk: %q, %q", res.Source, res.Versions[i-1], v)
			}
		}
	}
}

// Önbellek YAZILIM BAŞINA ve bir saat: aynı yazılım ağa ikinci kez gitmez,
// başka bir yazılım kendi listesini alır, süre dolunca yeniden çekilir.
func TestSurumOnbellegiYazilimBasina(t *testing.T) {
	ag := surumAgiKur(t)
	d := surumDaemon()
	ctx := context.Background()

	ilk := surumIste(t, d, ctx, model.SoftwarePaper)
	ikinci := surumIste(t, d, ctx, model.SoftwarePaper)
	if n := ag.denendi(adresPaper); n != 1 {
		t.Fatalf("paper listesi %d kez çekildi; önbellekten gelmeliydi", n)
	}
	if !slices.Equal(ilk.Versions, ikinci.Versions) {
		t.Fatal("önbellekten dönen liste farklı")
	}

	// Tek ortak kayıt olsaydı Fabric, Paper'ın listesini alırdı.
	fabric := surumIste(t, d, ctx, model.SoftwareFabric)
	if n := ag.denendi(adresFabric); n != 1 || !slices.Contains(fabric.Versions, "26.1") {
		t.Fatalf("fabric kendi listesini almadı (istek %d): %v", n, ilkBes(fabric.Versions))
	}

	geriAl(model.SoftwarePaper, 59*time.Minute)
	surumIste(t, d, ctx, model.SoftwarePaper)
	if n := ag.denendi(adresPaper); n != 1 {
		t.Fatalf("59 dakikalık kayıt taze sayılmalıydı (istek %d)", n)
	}
	geriAl(model.SoftwarePaper, 2*time.Minute)
	surumIste(t, d, ctx, model.SoftwarePaper)
	if n := ag.denendi(adresPaper); n != 2 {
		t.Fatalf("bir saati geçen kayıt yeniden çekilmeliydi (istek %d)", n)
	}
}

// Yazılımın API'sine ulaşılamazsa Mojang listesi döner ve yanıt YEDEK diye
// işaretlenir; yedek yalnızca bir dakika tutulur.
func TestSurumAgYokMojangYedegi(t *testing.T) {
	ag := surumAgiKur(t)
	ag.kapat("fill.papermc.io")
	d := surumDaemon()
	ctx := context.Background()

	res := surumIste(t, d, ctx, model.SoftwarePaper)
	if !res.Fallback || res.Source != "launchermeta.mojang.com" || res.Latest != "26.3" {
		t.Fatalf("Mojang yedeği bekleniyordu: yedek=%v kaynak=%q latest=%q",
			res.Fallback, res.Source, res.Latest)
	}
	// Yedek, Mojang'ın listesidir: Paper'da olmayan 26.1 de içinde. Arayüz
	// bunu Fallback'ten anlar.
	if !slices.Contains(res.Versions, "26.1") {
		t.Fatalf("yedek Mojang listesi değil: %v", res.Versions)
	}

	// Bir dakika içinde ikinci istek ağa ÇIKMAZ (internetsiz makinede her
	// altyapı değişikliği yeni bir zaman aşımı beklemesin).
	surumIste(t, d, ctx, model.SoftwarePaper)
	if p, m := ag.denendi(adresPaper), ag.denendi(adresMojang); p != 1 || m != 1 {
		t.Fatalf("yedek önbellekten gelmeliydi (paper %d, mojang %d deneme)", p, m)
	}
	// Mojang'ın listesi gerçek listedir: vanilla ağa gitmeden onu alır.
	van := surumIste(t, d, ctx, model.SoftwareVanilla)
	if van.Fallback || ag.denendi(adresMojang) != 1 {
		t.Fatalf("vanilla önbellekteki gerçek Mojang listesini almalıydı: %+v", van)
	}

	// İki dakika sonra (bir saat DOLMADAN) gerçek kaynak yeniden denenir.
	// Süre sabitten türetilmiyor: yedek bir saat tutulsaydı sınama da
	// onunla birlikte kayar ve hatayı görmezdi.
	geriAl(model.SoftwarePaper, 2*time.Minute)
	ag.mu.Lock()
	delete(ag.kapali, "fill.papermc.io")
	ag.mu.Unlock()
	res = surumIste(t, d, ctx, model.SoftwarePaper)
	if res.Fallback || res.Source != "fill.papermc.io" || ag.denendi(adresPaper) != 2 {
		t.Fatalf("ağ gelince gerçek Paper listesi gelmeliydi: yedek=%v kaynak=%q", res.Fallback, res.Source)
	}
}

// Hiçbir yere ulaşılamazsa gömülü liste döner, yedek diye işaretli.
// Vanilla'da Mojang'a İKİNCİ kez gidilmez: kendi kaynağı Mojang, aynı adres
// az önce düştü, ikinci deneme yalnızca bir zaman aşımı daha bekletirdi.
func TestSurumAgTamamenYok(t *testing.T) {
	ag := surumAgiKur(t)
	for _, h := range []string{"fill.papermc.io", "launchermeta.mojang.com", "meta.fabricmc.net",
		"hub.spigotmc.org"} {
		ag.kapat(h)
	}
	d := surumDaemon()
	ctx := context.Background()

	res := surumIste(t, d, ctx, model.SoftwareFabric)
	if !res.Fallback || res.Source != yerlesikKaynak || res.Latest != fallbackVersions[0] ||
		!slices.Equal(res.Versions, fallbackVersions) {
		t.Fatalf("gömülü liste bekleniyordu: %+v", res)
	}
	if ag.denendi(adresFabric) != 1 || ag.denendi(adresMojang) != 1 {
		t.Fatalf("fabric ve Mojang birer kez denenmeliydi: %v", ag.deneme)
	}

	van := surumIste(t, d, ctx, model.SoftwareVanilla)
	if !van.Fallback || van.Source != yerlesikKaynak {
		t.Fatalf("vanilla gömülü listeye düşmeliydi: %+v", van)
	}
	if n := ag.denendi(adresMojang); n != 2 {
		t.Fatalf("vanilla için Mojang bir kez denenmeliydi (toplam %d, beklenen 2)", n)
	}

	// Spigot'un kaynağı BuildTools dizini: o düşünce Mojang da denenir.
	spigot := surumIste(t, d, ctx, model.SoftwareSpigot)
	if !spigot.Fallback || spigot.Source != yerlesikKaynak {
		t.Fatalf("spigot gömülü listeye düşmeliydi: %+v", spigot)
	}
	if ag.denendi(adresSpigot) != 1 || ag.denendi(adresMojang) != 3 {
		t.Fatalf("spigot: dizin ve Mojang birer kez denenmeliydi: %v", ag.deneme)
	}
}

// Spigot/CraftBukkit listesi BuildTools'un sürüm dizininden gelir: Mojang'da
// olan ama dizinde olmayan 1.8.9 (BuildTools "Could not get version 1.8.9"
// ile düşer, 2026-09-27'de ölçüldü) listelenmez. Dizine ulaşılamazsa Mojang
// listesi YEDEK diye döner — eskiden Spigot "Mojang tabanlı" sayılıyordu ve
// bu adım atlanıp gömülü listeye düşülüyordu.
func TestSurumSpigotKendiDizini(t *testing.T) {
	ag := surumAgiKur(t)
	d := surumDaemon()
	ctx := context.Background()

	res := surumIste(t, d, ctx, model.SoftwareSpigot)
	if res.Fallback || res.Source != "hub.spigotmc.org" || res.Latest != "26.3" {
		t.Fatalf("spigot yanıtı: yedek=%v kaynak=%q latest=%q", res.Fallback, res.Source, res.Latest)
	}
	for _, v := range []string{"1.8.9", "1.16", "1.9.1"} {
		if slices.Contains(res.Versions, v) {
			t.Errorf("spigot listesinde %q var; BuildTools dizininde yok", v)
		}
	}
	if ag.denendi(adresMojang) != 0 {
		t.Fatal("spigot listesi için Mojang'a gidildi")
	}

	ag.kapat("hub.spigotmc.org")
	cb := surumIste(t, d, ctx, model.SoftwareCraftBukkit)
	if !cb.Fallback || cb.Source != "launchermeta.mojang.com" || !slices.Contains(cb.Versions, "26.1") {
		t.Fatalf("dizin yokken Mojang yedeği bekleniyordu: yedek=%v kaynak=%q", cb.Fallback, cb.Source)
	}
}

// İptal edilen bir isteğin yedek yanıtı önbelleğe YAZILMAZ: o yanıt ağın
// değil iptalin sonucu.
func TestSurumIptalOnbelleklenmez(t *testing.T) {
	surumAgiKur(t)
	d := surumDaemon()
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	res := surumIste(t, d, ctx, model.SoftwarePaper)
	if !res.Fallback {
		t.Fatalf("iptal edilen istek yedek dönmeliydi: %+v", res)
	}
	res = surumIste(t, d, context.Background(), model.SoftwarePaper)
	if res.Fallback || res.Source != "fill.papermc.io" {
		t.Fatalf("iptalin yedeği önbellekte kalmış: yedek=%v kaynak=%q", res.Fallback, res.Source)
	}
}

func TestSurumBilinmeyenYazilim(t *testing.T) {
	surumAgiKur(t)
	_, err := surumDaemon().handleServerVersions(context.Background(), json.RawMessage(`{"software":"bukkit2"}`))
	var ie *ipc.Error
	if !errors.As(err, &ie) || ie.Code != ipc.CodeInvalidParams {
		t.Fatalf("bilinmeyen yazılım geçersiz parametre olmalı: %v", err)
	}
}

// Gerçek ağla uçtan uca: işleyici her yazılım için kendi kaynağından tam
// sürüm listesi almalı. Ağ gerektirir; MCOS_LIVE_DL=1 olmadan atlanır.
func TestCanliSurumListeleri(t *testing.T) {
	if os.Getenv("MCOS_LIVE_DL") == "" {
		t.Skip("MCOS_LIVE_DL=1 ile çalıştırın")
	}
	verCache.mu.Lock()
	eski := verCache.kayit
	verCache.kayit = nil
	verCache.mu.Unlock()
	t.Cleanup(func() {
		verCache.mu.Lock()
		verCache.kayit = eski
		verCache.mu.Unlock()
	})
	d := surumDaemon()
	for _, sw := range model.AllSoftware {
		res := surumIste(t, d, context.Background(), sw)
		if res.Fallback || len(res.Versions) == 0 {
			t.Errorf("%s: gerçek liste alınamadı: %+v", sw, res)
			continue
		}
		t.Logf("%-11s %-26s %s", sw, res.Source, strings.Join(ilkBes(res.Versions), ", "))
	}
}
