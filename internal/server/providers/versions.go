package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"mcos/internal/mcver"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// YAZILIM BAŞINA MINECRAFT SÜRÜM LİSTESİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// server.versions yazılım parametresini yok sayıyor, HER yazılım için
// Mojang'ın sürüm listesini döndürüyordu. 2026-09-27'de gerçek API'lerle
// ölçüldü:
//
//   - Paper, Purpur ve Folia düz "26.1"i yayımlamıyor (yalnızca 26.1.1 ve
//     26.1.2); sihirbaz onu öneriyor, kurulum 404 ile düşüyordu.
//   - Folia'nın en yenisi 26.2; sihirbaz Folia için 26.3'ü "en yeni" diye
//     sunuyordu ve kurulum "Desteklenen sürümler: …" hatasıyla bitiyordu.
//   - Forge 1.4.7 ve öncesinde kurucu jar'ı yok (maven 404); Mojang listesi
//     1.0'a kadar gidiyor.
//
// Liste artık her yazılımın KENDİ kaynağından gelir — Install'ın kullandığı
// adreslerle aynı yerden. Böylece listelenen her sürüm, aynı sağlayıcının
// kurabileceği bir sürümdür.

// VersionList is one software's selectable Minecraft releases.
type VersionList struct {
	// Versions: yalnızca TAM sürümler (ön sürüm, rc, anlık görüntü yok),
	// en yeni önce.
	Versions []string
	// LatestSnapshot yalnızca Mojang manifestinden okunur; öteki kaynaklar
	// böyle bir işaretçi yayımlamıyor.
	LatestSnapshot string
	// Source, listenin alındığı ana makine (ör. "fill.papermc.io").
	Source string
}

// versionLister is implemented by every provider that can list its releases.
type versionLister interface {
	listVersions(ctx context.Context, client *http.Client) (VersionList, error)
}

// ListVersions returns the Minecraft releases the software's provider
// publishes, newest first. Empty software means vanilla.
//
// Ağ hatasında, beklenmeyen yanıtta ya da BOŞ listede hata döner: çağıran
// (daemon) o durumda yedek listeye düşer ve yanıtı "yedek" diye işaretler.
func ListVersions(ctx context.Context, client *http.Client, sw model.Software) (VersionList, error) {
	if sw == "" {
		sw = model.SoftwareVanilla
	}
	p, ok := Get(sw)
	if !ok {
		return VersionList{}, fmt.Errorf("bilinmeyen yazılım %q", sw)
	}
	l, ok := p.(versionLister)
	if !ok {
		return VersionList{}, fmt.Errorf("%s: sürüm listesi kaynağı yok", sw)
	}
	out, err := l.listVersions(ctx, client)
	if err != nil {
		return VersionList{}, err
	}
	if len(out.Versions) == 0 {
		// Boş liste bir yanıt değil, bozuk bir yanıttır: sihirbaz boş
		// liste gösterip "sürüm seçin" demek yerine yedeğe düşsün.
		return VersionList{}, fmt.Errorf("%s: %s boş sürüm listesi döndürdü", sw, out.Source)
	}
	return out, nil
}

// ── En eski KURULABİLİR sürümler (2026-09-27'de ölçüldü) ────────────────────
//
// Kaynak listeler, sağlayıcının artık kuramadığı sürümleri de taşıyor:
const (
	// Mojang 1.2.4 ve öncesinin sürüm JSON'unda downloads.server YOK;
	// vanillaProvider "no server download (too old?)" ile düşer. 1.2.5'te var.
	vanillaEnEski = "1.2.5"
	// BuildTools'un sürüm deposu (hub.spigotmc.org/versions/) 1.8'de başlıyor;
	// 1.7.10.json 404.
	buildToolsEnEski = "1.8"
	// Forge promosyonları 1.1'e kadar gidiyor ama kurucu jar'ı 1.5.2'de
	// başlıyor: 1.1, 1.2.5, 1.3.2 ve 1.4.7 kurucuları maven'da 404.
	forgeEnEski = "1.5.2"
)

// tamSurumler keeps full releases not older than enEski, without duplicates,
// newest first.
//
// Sıra kaynağın sırasına BIRAKILMAZ: Purpur eskiden yeniye veriyor, Paper
// çizgi çizgi gruplıyor, NeoForge listesinden türetilen sürümler maven
// sırasıyla geliyor. Ortak mcver sırası hepsini aynı yapar ("1.21.11" >
// "1.21.8"; "26.1" > "1.21.11").
func tamSurumler(ham []string, enEski string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range ham {
		v = strings.TrimSpace(v)
		if !mcver.IsRelease(v) || seen[v] {
			continue
		}
		if enEski != "" && mcver.Compare(v, enEski) < 0 {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	mcver.SortNewestFirst(out)
	return out
}

// kaynakAdi returns the host of an API address, for VersionList.Source.
func kaynakAdi(adres string) string {
	if u, err := url.Parse(adres); err == nil && u.Host != "" {
		return u.Host
	}
	return adres
}

// ── Mojang (Vanilla) ────────────────────────────────────────────────────────

func (vanillaProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	return mojangSurumleri(ctx, client, vanillaEnEski)
}

// ── BuildTools (Spigot, CraftBukkit) ────────────────────────────────────────

// spigotSurumDizini is BuildTools' own version index: "--rev X" reads X.json
// from here and gives up if it is missing.
const spigotSurumDizini = "https://hub.spigotmc.org/versions/"

// reSpigotSurum picks the "X" out of the index's `<a href="X.json">` lines.
var reSpigotSurum = regexp.MustCompile(`href="([^"/]+)\.json"`)

// Spigot ve CraftBukkit BuildTools ile derlenir; BuildTools yalnızca bu
// dizinde JSON'u olan sürümleri derleyebilir.
//
// ── Yakalanan gerçek hata: Mojang listesi Spigot'ta olmayanları da veriyordu ─
// Liste önceden Mojang manifestinden geliyordu (1.8 ve sonrası). 2026-09-27'de
// gerçek dizinle ölçüldü: Mojang'ın 1.8.1, 1.8.2, 1.8.9, 1.9.1, 1.9.3,
// 1.10.1 ve düz 1.16 sürümleri için JSON YOK (hub.spigotmc.org/versions/
// 1.8.9.json -> 404; 1.8.8.json -> 200). En çok kullanılan PvP sürümü olan
// 1.8.9'u seçen kullanıcı, BuildTools indirildikten sonra "Could not get
// version 1.8.9" hatasıyla düşen bir kurulum alıyordu. Dizin BuildTools'un
// okuduğu kaynağın kendisi olduğu için listelenen her sürüm derlenebilir.
func (buildToolsProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	// Dizin ~10 KB; 1 MB sınırı, dizinin yerine gelebilecek bir hata
	// sayfasının belleği doldurmasını önler.
	govde, err := getText(ctx, client, spigotSurumDizini, 1<<20)
	if err != nil {
		return VersionList{}, fmt.Errorf("spigot: sürüm dizini: %w", err)
	}
	var ham []string
	for _, m := range reSpigotSurum.FindAllStringSubmatch(govde, -1) {
		// "latest", "1.18-rc3", "1.14-pre5" de dizinde: tamSurumler eler.
		ham = append(ham, m[1])
	}
	return VersionList{
		Versions: tamSurumler(ham, buildToolsEnEski),
		Source:   kaynakAdi(spigotSurumDizini),
	}, nil
}

// getText GETs url and returns at most limit bytes of its body.
//
// Yalnızca JSON olmayan kaynak için (Spigot'un HTML dizini); öteki listeler
// getJSON ile okunur.
func getText(ctx context.Context, client *http.Client, adres string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adres, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %s", adres, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return string(b), err
}

func mojangSurumleri(ctx context.Context, client *http.Client, enEski string) (VersionList, error) {
	var m struct {
		Latest struct {
			Snapshot string `json:"snapshot"`
		} `json:"latest"`
		Versions []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"versions"`
	}
	if err := getJSON(ctx, client, mojangManifestURL, &m); err != nil {
		return VersionList{}, fmt.Errorf("mojang: sürüm manifesti: %w", err)
	}
	var ham []string
	for _, v := range m.Versions {
		// "snapshot" türü ön sürümleri ve haftalık anlık görüntüleri
		// kapsar; "old_beta"/"old_alpha" (b1.8.1, a1.0.4) sunucu değil.
		if v.Type == "release" {
			ham = append(ham, v.ID)
		}
	}
	return VersionList{
		Versions:       tamSurumler(ham, enEski),
		LatestSnapshot: m.Latest.Snapshot,
		Source:         kaynakAdi(mojangManifestURL),
	}, nil
}

// ── PaperMC (Paper, Folia) ──────────────────────────────────────────────────

func (p paperLikeProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	vs, err := p.projeSurumleri(ctx, client)
	if err != nil {
		return VersionList{}, err
	}
	return VersionList{Versions: vs, Source: kaynakAdi(paperAPI)}, nil
}

// projeSurumleri reads GET /v3/projects/<proje>: sürümler çizgiye göre
// gruplanmış ("26.1": ["26.1.2", "26.1.1"]) ve ön sürümleri de içeriyor
// ("26.3-rc-3", "1.21.11-pre5").
//
// Neden /versions değil: o uç her sürümün derleme numaralarını da taşıyor
// (Paper için 65 KB, proje ucu 1 KB). Ölçüldü (2026-09-27): Paper ve
// Folia'da derlemesi OLMAYAN tek bir sürüm yok, yani proje listesi
// kurulabilir sürüm listesiyle aynı.
func (p paperLikeProvider) projeSurumleri(ctx context.Context, client *http.Client) ([]string, error) {
	var proje struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := getJSON(ctx, client, paperAPI+"/"+p.project, &proje); err != nil {
		return nil, fmt.Errorf("%s: sürüm listesi: %w", p.project, err)
	}
	var ham []string
	for _, liste := range proje.Versions {
		ham = append(ham, liste...)
	}
	return tamSurumler(ham, ""), nil
}

// ── Purpur ──────────────────────────────────────────────────────────────────

// Purpur listesi eskiden yeniye ("1.14.1", …, "26.3") geliyor.
// metadata.current'a GÜVENİLMEZ: 2026-09-27'de "26.2" diyordu, oysa
// listede ve indirmede 26.3 vardı. En yeni sürüm listeden hesaplanır.
func (purpurProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	var proje struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON(ctx, client, purpurAPI, &proje); err != nil {
		return VersionList{}, fmt.Errorf("purpur: sürüm listesi: %w", err)
	}
	return VersionList{Versions: tamSurumler(proje.Versions, ""), Source: kaynakAdi(purpurAPI)}, nil
}

// ── Fabric, Quilt ───────────────────────────────────────────────────────────

func (fabricProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	return metaOyunSurumleri(ctx, client, "fabric", fabricMeta+"/versions/game")
}

func (quiltProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	return metaOyunSurumleri(ctx, client, "quilt", quiltMeta+"/versions/game")
}

// metaOyunSurumleri reads a Fabric/Quilt style game list:
// [{"version": "26.3", "stable": true}, {"version": "26.3-rc-3", "stable": false}].
//
// İki koşul birden aranır: stable VE mcver'e göre tam sürüm. 2026-09-27
// listesinde ikisi tam örtüşüyor (528 Fabric, 441 Quilt girdisi); tam
// sürüm denetimi, meta bir gün "stable" işaretini bir ön sürüme de koyarsa
// sihirbaza ön sürüm sızmasın diye. Liste 1.14'e kadar gidiyor; Fabric'in
// desteklediği en eski sürüm de o.
func metaOyunSurumleri(ctx context.Context, client *http.Client, ad, adres string) (VersionList, error) {
	var oyun []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := getJSON(ctx, client, adres, &oyun); err != nil {
		return VersionList{}, fmt.Errorf("%s: sürüm listesi: %w", ad, err)
	}
	var ham []string
	for _, g := range oyun {
		if g.Stable {
			ham = append(ham, g.Version)
		}
	}
	return VersionList{Versions: tamSurumler(ham, ""), Source: kaynakAdi(adres)}, nil
}

// ── Forge, NeoForge ─────────────────────────────────────────────────────────

func (p forgeProvider) listVersions(ctx context.Context, client *http.Client) (VersionList, error) {
	if p.software == model.SoftwareNeoForge {
		return neoforgeSurumleri(ctx, client)
	}
	return forgeSurumleri(ctx, client)
}

// forgeSurumleri reads promotions_slim.json: anahtarlar "<mc>-latest" ve
// "<mc>-recommended". forgeInstaller aynı dosyadan seçtiği için listelenen
// her sürümün bir derlemesi vardır. 1.5.2 ve sonrası 62 sürümün kurucusu
// 2026-09-27'de tek tek denendi: beşi (1.7.2, 1.7.10, 1.8.9, 1.9.4, 1.10)
// maven'da ekli adla duruyor ve forgeMavenSurumu o adı çözmeden 404
// veriyordu; artık hepsi 200.
func forgeSurumleri(ctx context.Context, client *http.Client) (VersionList, error) {
	var promos struct {
		Promos map[string]string `json:"promos"`
	}
	if err := getJSON(ctx, client, forgePromos, &promos); err != nil {
		return VersionList{}, fmt.Errorf("forge: promosyonlar: %w", err)
	}
	var ham []string
	for k := range promos.Promos {
		for _, son := range []string{"-latest", "-recommended"} {
			if mc, ok := strings.CutSuffix(k, son); ok {
				ham = append(ham, mc)
			}
		}
	}
	return VersionList{Versions: tamSurumler(ham, forgeEnEski), Source: kaynakAdi(forgePromos)}, nil
}

// neoforgeSurumleri derives Minecraft releases from NeoForge's own versions
// ("21.1.252" -> 1.21.1, "26.3.0.23-beta" -> 26.3; bkz. mcver.FromNeoForge).
//
// Bir sürüm ancak neoforgeInstaller'ın gerçekten SEÇECEĞİ bir derlemesi
// varsa listelenir (aynı pickNeoForge): "26.1.0.0-alpha.1+snapshot-1" gibi
// anlık görüntü derlemeleri tam sürüm sunucusuna kurulmaz, tek başlarına bir
// sürümü listeye sokmamalı. Mojang listesinin aksine 1.20.1 burada YOK:
// NeoForge'un 1.20.1 derlemeleri ayrı bir yapıttır (net.neoforged:forge) ve
// sağlayıcı onu kurmuyor.
func neoforgeSurumleri(ctx context.Context, client *http.Client) (VersionList, error) {
	var resp struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON(ctx, client, neoforgeVersionsAPI, &resp); err != nil {
		return VersionList{}, fmt.Errorf("neoforge: sürüm listesi: %w", err)
	}
	var ham []string
	denendi := map[string]bool{}
	for _, v := range resp.Versions {
		mc := mcver.FromNeoForge(v)
		if mc == "" || denendi[mc] {
			continue
		}
		denendi[mc] = true
		prefix, ok := mcver.NeoForgePrefix(mc)
		if !ok {
			continue
		}
		if _, ok := pickNeoForge(resp.Versions, prefix); ok {
			ham = append(ham, mc)
		}
	}
	return VersionList{Versions: tamSurumler(ham, ""), Source: kaynakAdi(neoforgeVersionsAPI)}, nil
}
