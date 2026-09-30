// Package catalog is a small client for the Modrinth API (https://modrinth.com),
// used to search and install mods, plugins, datapacks, and resource packs. It is
// key-free and public; MCOS uses it both for the panel's catalog browser and for
// auto-installing helper plugins like ViaVersion.
package catalog

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mcos/internal/mcver"
	"mcos/internal/version"
)

const apiBase = "https://api.modrinth.com/v2"

// userAgent is required by Modrinth's API etiquette.
const userAgent = version.UserAgent

// Project is a single Modrinth search hit.
type Project struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Downloads   int      `json:"downloads"`
	ProjectType string   `json:"project_type"`
	Categories  []string `json:"categories"`
	IconURL     string   `json:"icon_url,omitempty"`

	// Loader, projenin yükleyici zincirinin HANGİ halkasıyla bulunduğudur
	// (ör. purpur sunucusunda "paper"). Modrinth'ten gelmez; SearchCompatible
	// doldurur. Boşsa arama yükleyici süzgeci olmadan yapılmıştır.
	Loader string `json:"-"`
}

type searchResp struct {
	Hits      []Project `json:"hits"`
	TotalHits int       `json:"total_hits"`
}

// File is a downloadable artifact attached to a project version.
type File struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
	// Hashes, Modrinth'in yayımladığı özetlerdir ("sha1", "sha512").
	// DownloadTo bunlarla indirilen dosyayı doğrular.
	Hashes map[string]string `json:"hashes,omitempty"`
}

// Version is one published version of a project.
type Version struct {
	ID            string    `json:"id"`
	VersionNumber string    `json:"version_number"`
	VersionType   string    `json:"version_type"` // release | beta | alpha
	GameVersions  []string  `json:"game_versions"`
	Loaders       []string  `json:"loaders"`
	Files         []File    `json:"files"`
	DatePublished time.Time `json:"date_published"`
	// Dependencies, yazarın bildirdiği bağımlılıklardır (bkz. deps.go).
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

// Client talks to the Modrinth API.
type Client struct {
	// http, API çağrıları (arama, sürüm listesi) içindir; kısa zaman aşımı.
	http *http.Client
	// dl, dosya indirmeleri içindir. AYRI, çünkü 60 sn'lik tek bir zaman
	// aşımı yavaş bir bağlantıda 30 MB'lık bir modu hiç indirtmezdi; öte
	// yandan API çağrısının dakikalarca asılı kalması da kabul edilemez.
	dl   *http.Client
	base string
}

// New constructs a catalog client with sane timeouts.
func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 30 * time.Second},
		dl:   &http.Client{Timeout: 10 * time.Minute},
		base: apiBase,
	}
}

// NewWith builds a client over a custom HTTP client and API base.
//
// Testler (ağsız, sahte RoundTripper ile) ve olası bir yansı sunucu için. hc
// hem API hem indirme için kullanılır.
func NewWith(hc *http.Client, base string) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	if base == "" {
		base = apiBase
	}
	return &Client{http: hc, dl: hc, base: strings.TrimRight(base, "/")}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("modrinth %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ── Yükleyici zinciri ───────────────────────────────────────────────────────
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// Sunucunun yükleyicisi Modrinth'e OLDUĞU GİBİ soruluyordu. Oysa eklenti
// yazarlarının çoğu Purpur'u ya da Folia'yı ayrıca etiketlemez; Purpur, Paper
// eklentilerini zaten yükler. Ölçüldü (Modrinth API, MC 1.21.1):
//
//	/project/worldedit/version  loaders=["purpur"]  -> 0 sürüm
//	/project/worldedit/version  loaders=["folia"]   -> 0 sürüm
//	/project/worldedit/version  loaders=["paper"]   -> 6 sürüm
//	/project/viaversion/version loaders=["purpur"]  -> 0 sürüm (paper: 565)
//
// Sonuç: Purpur sunucusunda WorldEdit "uyumlu dosya yok" diye KURULAMIYORDU ve
// sunucu oluşturulurken otomatik yapılan ViaVersion/ViaBackwards kurulumu
// (internal/server/compat.go) Purpur'da HER ZAMAN sessizce başarısız
// oluyordu. Aramada da aynı sorun vardı: purpur ile "worldedit" aranınca
// WorldEdit'in KENDİSİ sonuçlarda yoktu, yalnızca eklentileri çıkıyordu.
//
// Artık sunucu, yükleyebildiği TÜM yükleyicilerle sorulur ve en özgül olan
// tercih edilir. Hangi halkayla bulunduğu sonuçta ve mesajda yazılır.

// loaderChains lists, for a server loader, every loader tag whose builds it can
// run — most specific first.
//
// Folia bilerek yalnızca paper'a düşer: Folia bölgesel iş parçacığını
// desteklediğini bildirmeyen eklentiyi reddeder; spigot/bukkit'e özgü eski
// yapıların bunu bildirme olasılığı neredeyse yok, onları önermek kullanıcıya
// yüklenmeyecek bir dosya indirtmek olurdu.
var loaderChains = map[string][]string{
	"purpur": {"purpur", "paper", "spigot", "bukkit"},
	"paper":  {"paper", "spigot", "bukkit"},
	"spigot": {"spigot", "bukkit"},
	"bukkit": {"bukkit"},
	"folia":  {"folia", "paper"},
	"quilt":  {"quilt", "fabric"},
}

// LoaderChain returns the loaders a server running loader can use, in order of
// preference. Unknown loaders map to themselves; "" maps to nil (no filter).
func LoaderChain(loader string) []string {
	loader = strings.ToLower(strings.TrimSpace(loader))
	if loader == "" {
		return nil
	}
	if ch, ok := loaderChains[loader]; ok {
		return append([]string(nil), ch...)
	}
	return []string{loader}
}

// chainIndex returns the position of the first chain loader present in tags,
// or -1. An empty chain matches everything at position 0.
func chainIndex(chain, tags []string) int {
	if len(chain) == 0 {
		return 0
	}
	for i, l := range chain {
		for _, t := range tags {
			if strings.EqualFold(t, l) {
				return i
			}
		}
	}
	return -1
}

// facets JSON-encodes facet groups. Groups are AND-ed; values inside one group
// are OR-ed (Modrinth semantics). Empty groups are dropped.
func facets(groups ...[]string) string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		var vals []string
		for _, v := range g {
			if v != "" {
				vals = append(vals, fmt.Sprintf("%q", v))
			}
		}
		if len(vals) > 0 {
			out = append(out, "["+strings.Join(vals, ",")+"]")
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "[" + strings.Join(out, ",") + "]"
}

// ── Arama ───────────────────────────────────────────────────────────────────

// SearchQuery describes a compatibility-aware search.
type SearchQuery struct {
	Query string
	// ProjectType: "mod", "plugin", "datapack", "resourcepack" (boş = hepsi).
	ProjectType string
	// Loader, SUNUCUNUN yükleyicisidir; zincir buradan türetilir.
	Loader string
	// GameVersion boşsa sürüm süzgeci uygulanmaz.
	GameVersion string
	Limit       int
}

// SearchResult carries the hits plus the filters that were actually applied,
// so the caller can say what was searched ("paper · 1.21.1 · 21 sonuç").
type SearchResult struct {
	Hits        []Project
	Total       int
	Loaders     []string
	GameVersion string
	ProjectType string
}

// SearchCompatible finds projects that a server with q.Loader can load.
//
// Yükleyici süzgeci TEK bir VEYA grubudur (purpur|paper|spigot|bukkit): tek
// istekte, WorldEdit gibi yalnızca paper/spigot/bukkit etiketli projeler de
// gelir. Her sonucun Loader alanı, zincirin o projede bulunan İLK halkasıdır.
func (c *Client) SearchCompatible(ctx context.Context, q SearchQuery) (SearchResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	chain := LoaderChain(q.Loader)
	gv := strings.TrimSpace(q.GameVersion)

	v := url.Values{}
	v.Set("query", q.Query)
	v.Set("limit", fmt.Sprintf("%d", limit))
	v.Set("index", "relevance")

	var groups [][]string
	if q.ProjectType != "" {
		groups = append(groups, []string{"project_type:" + q.ProjectType})
	}
	if len(chain) > 0 {
		g := make([]string, 0, len(chain))
		for _, l := range chain {
			g = append(g, "categories:"+l)
		}
		groups = append(groups, g)
	}
	if gv != "" {
		groups = append(groups, []string{"versions:" + gv})
	}
	if f := facets(groups...); f != "" {
		v.Set("facets", f)
	}

	var res searchResp
	if err := c.get(ctx, "/search", v, &res); err != nil {
		return SearchResult{}, err
	}
	for i := range res.Hits {
		if len(chain) == 0 {
			continue
		}
		if idx := chainIndex(chain, res.Hits[i].Categories); idx >= 0 {
			res.Hits[i].Loader = chain[idx]
		}
	}
	total := res.TotalHits
	if total < len(res.Hits) {
		total = len(res.Hits)
	}
	return SearchResult{
		Hits:        res.Hits,
		Total:       total,
		Loaders:     chain,
		GameVersion: gv,
		ProjectType: q.ProjectType,
	}, nil
}

// Search finds projects matching query, optionally constrained by project type
// ("mod","plugin","datapack","resourcepack" — Modrinth uses these strings),
// loader (e.g. "paper","fabric"; its compatibility chain is searched), and
// game version (e.g. "1.21.1").
func (c *Client) Search(ctx context.Context, query, projectType, loader, gameVersion string, limit int) ([]Project, error) {
	r, err := c.SearchCompatible(ctx, SearchQuery{Query: query, ProjectType: projectType,
		Loader: loader, GameVersion: gameVersion, Limit: limit})
	return r.Hits, err
}

// ── Sürüm çözümleme ─────────────────────────────────────────────────────────

// ErrNoCompatible is returned (wrapped) when no version fits the server.
var ErrNoCompatible = errors.New("uyumlu sürüm yok")

// Versions lists a project's versions filtered by loader and game version
// (both optional), newest first. loader is matched EXACTLY (no chain).
func (c *Client) Versions(ctx context.Context, idOrSlug, loader, gameVersion string) ([]Version, error) {
	var loaders []string
	if loader != "" {
		loaders = []string{loader}
	}
	return c.versionsAny(ctx, idOrSlug, loaders, gameVersion)
}

// versionsAny lists versions carrying ANY of loaders (Modrinth ORs them).
func (c *Client) versionsAny(ctx context.Context, idOrSlug string, loaders []string, gameVersion string) ([]Version, error) {
	q := url.Values{}
	if len(loaders) > 0 {
		raw, _ := json.Marshal(loaders)
		q.Set("loaders", string(raw))
	}
	if gameVersion != "" {
		q.Set("game_versions", fmt.Sprintf("[%q]", gameVersion))
	}
	var vs []Version
	if err := c.get(ctx, "/project/"+url.PathEscape(idOrSlug)+"/version", q, &vs); err != nil {
		return nil, err
	}
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].DatePublished.After(vs[j].DatePublished) })
	return vs, nil
}

// ResolveOptions tunes version resolution.
type ResolveOptions struct {
	// AnyGameVersion, Minecraft sürüm süzgecini kaldırır. Kullanıcı aramada
	// "sürüm süzgecini kaldır" dediğinde kurulum da aynı şeyi yapmalı; yoksa
	// listede görünen proje "uyumlu dosya yok" diye kurulamazdı. Yine de
	// sunucunun sürüm ÇİZGİSİNE (1.21.x) en yakın yapı tercih edilir.
	AnyGameVersion bool
}

// Resolved is the chosen file plus WHY it was chosen.
type Resolved struct {
	File    File
	Version Version
	// Loader, dosyanın hangi yükleyici etiketiyle bulunduğudur.
	Loader string
	// Wanted, sunucunun kendi yükleyicisidir.
	Wanted string
	// GameVersion, sunucunun Minecraft sürümüdür.
	GameVersion string
	// ExactGameVersion, seçilen sürümün GameVersion'ı açıkça listelediğidir.
	ExactGameVersion bool
}

// FellBack reports whether a less specific loader had to be used.
func (r Resolved) FellBack() bool {
	return r.Wanted != "" && r.Loader != "" && !strings.EqualFold(r.Loader, r.Wanted)
}

// Summary is the one-line result shown after an install, e.g.
// "worldedit (paper uyumlu) kuruldu".
//
// Describe'dan AYRI, çünkü alt çubukta tek satır var: kullanıcı önce NE
// kurulduğunu ve hangi yükleyiciyle bulunduğunu görmeli. Ayrıntılı açıklama
// (sürüm numarası, etiket uyarısı) Describe'da kalır ve günlüğe yazılır.
//
// "(X uyumlu)" YALNIZCA geri düşüşte yazılır: paper sunucusuna paper sürümü
// kurulduğunda "(paper uyumlu)" demek bilgi değil gürültüdür; purpur
// sunucusuna paper sürümü kurulduğunda ise kullanıcının bilmesi gereken tam
// olarak budur (eklenti yüklenmezse ilk bakacağı yer).
func (r Resolved) Summary(name string) string {
	if name == "" {
		name = "eklenti"
	}
	s := name + " kuruldu"
	if r.FellBack() {
		s = fmt.Sprintf("%s (%s uyumlu) kuruldu", name, r.Loader)
	}
	if r.GameVersion != "" && !r.ExactGameVersion {
		s += fmt.Sprintf(" — Minecraft %s için etiketlenmemiş", r.GameVersion)
	}
	return s
}

// Describe explains the choice in one Turkish phrase (for panel messages).
func (r Resolved) Describe() string {
	var parts []string
	switch {
	case r.FellBack():
		parts = append(parts, fmt.Sprintf("%s için ayrı sürüm yok, %s sürümü kullanıldı", r.Wanted, r.Loader))
		if strings.EqualFold(r.Wanted, "folia") {
			parts = append(parts, "Folia yalnızca 'folia-supported' bildiren eklentileri yükler, bu dosya yüklenmeyebilir")
		}
	case r.Loader != "":
		parts = append(parts, r.Loader+" sürümü")
	}
	if r.Version.VersionNumber != "" {
		parts = append(parts, "sürüm "+r.Version.VersionNumber)
	}
	if r.GameVersion != "" && !r.ExactGameVersion {
		mc := strings.Join(r.Version.GameVersions, ", ")
		if len(r.Version.GameVersions) > 3 {
			mc = strings.Join(r.Version.GameVersions[len(r.Version.GameVersions)-3:], ", ") + "…"
		}
		parts = append(parts, fmt.Sprintf("Minecraft %s için etiketlenmemiş (%s), sunucu yüklemeyebilir", r.GameVersion, mc))
	}
	return strings.Join(parts, "; ")
}

// typeRank orders release < beta < alpha.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// BestFile yalnızca TARİHE bakıyordu. Ölçüldü (MC 1.21.1, paper):
//
//	viaversion: en yeni = 5.12.1-SNAPSHOT+1069 (beta), en yeni kararlı = 5.12.0
//	worldedit:  en yeni = 7.3.10-beta-01 (beta),        en yeni kararlı = 7.3.9
//
// Yani otomatik ViaVersion kurulumu ve paneldeki "Kur" düğmesi kararlı sürüm
// varken GELİŞTİRME yapısını kuruyordu. Kararlı sürüm yoksa beta yine seçilir.
func typeRank(t string) int {
	switch strings.ToLower(t) {
	case "", "release":
		return 0
	case "beta":
		return 1
	default: // alpha ve bilinmeyenler
		return 2
	}
}

// minorLine returns "1.21" for "1.21.1" (the release line of a version).
//
// ── Yakalanan gerçek hata: 26.x ön sürüm etiketleri ayrı çizgi sayılıyordu ──
// Noktadan bölünüyordu. 1.x'te "1.21.11-rc3" -> "1.21" şans eseri doğruydu,
// ama 26.x'in taban sürümü İKİ parçalı: Modrinth'in gerçek etiketi
// "26.3-rc-3" -> "26.3-rc-3" çıkıyor, sunucunun "26.3" çizgisiyle
// eşleşmiyordu (api.modrinth.com/v2/tag/game_version, 2026-09-27). Süzgeç
// kaldırılınca 26.3 sunucusuna 26.3-rc etiketli yapı yerine daha yeni
// tarihli bir 1.21.11 yapısı seçiliyordu. Çizgi artık ortak mcver.Line.
func minorLine(v string) string { return mcver.Line(v) }

// gameAffinity: 0 = lists gv exactly, 1 = same release line, 2 = other.
func gameAffinity(v Version, gv string) int {
	if gv == "" {
		return 0
	}
	best := 2
	line := minorLine(gv)
	for _, g := range v.GameVersions {
		if g == gv {
			return 0
		}
		if minorLine(g) == line {
			best = 1
		}
	}
	return best
}

func primaryFile(v Version) (File, bool) {
	for _, f := range v.Files {
		if f.Primary {
			return f, true
		}
	}
	if len(v.Files) > 0 {
		return v.Files[0], true
	}
	return File{}, false
}

// pickVersion chooses the best version for chain/gv among vs.
//
// Öncelik: (1) zincirin en özgül halkası, (2) istenirse MC sürümüne yakınlık,
// (3) kararlılık (release > beta > alpha), (4) yenilik.
func pickVersion(vs []Version, chain []string, gv string, anyGV bool) (Version, File, int, bool) {
	type cand struct {
		v     Version
		f     File
		chain int
		aff   int
		rank  int
	}
	var cs []cand
	for _, v := range vs {
		ci := chainIndex(chain, v.Loaders)
		if ci < 0 {
			continue
		}
		f, ok := primaryFile(v)
		if !ok {
			continue
		}
		aff := 0
		if anyGV {
			aff = gameAffinity(v, gv)
		}
		cs = append(cs, cand{v: v, f: f, chain: ci, aff: aff, rank: typeRank(v.VersionType)})
	}
	if len(cs) == 0 {
		return Version{}, File{}, -1, false
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.chain != b.chain {
			return a.chain < b.chain
		}
		if a.aff != b.aff {
			return a.aff < b.aff
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return a.v.DatePublished.After(b.v.DatePublished)
	})
	return cs[0].v, cs[0].f, cs[0].chain, true
}

// Resolve picks the file to install for a server with loader and gameVersion.
//
// TEK istek: zincirin tüm halkaları aynı sorguda istenir (Modrinth VEYA'lar),
// sonra en özgül halka seçilir. Halka halka sormak aynı sonucu dört kat ağ
// trafiğiyle verirdi.
func (c *Client) Resolve(ctx context.Context, idOrSlug, loader, gameVersion string, opt ResolveOptions) (Resolved, error) {
	chain := LoaderChain(loader)
	gv := strings.TrimSpace(gameVersion)
	query := gv
	if opt.AnyGameVersion {
		query = ""
	}
	vs, err := c.versionsAny(ctx, idOrSlug, chain, query)
	if err != nil {
		return Resolved{}, err
	}
	v, f, ci, ok := pickVersion(vs, chain, gv, opt.AnyGameVersion)
	if !ok {
		return Resolved{}, fmt.Errorf("%w: %q (yükleyici=%s, mc=%s)", ErrNoCompatible,
			idOrSlug, strings.Join(chain, "/"), orAny(query))
	}
	r := Resolved{File: f, Version: v, Wanted: strings.ToLower(loader), GameVersion: gv}
	if ci >= 0 && ci < len(chain) {
		r.Loader = chain[ci]
	}
	r.ExactGameVersion = gv == "" || gameAffinity(v, gv) == 0
	return r, nil
}

func orAny(s string) string {
	if s == "" {
		return "herhangi"
	}
	return s
}

// BestFile resolves the newest compatible primary file for a project.
func (c *Client) BestFile(ctx context.Context, idOrSlug, loader, gameVersion string) (File, error) {
	r, err := c.Resolve(ctx, idOrSlug, loader, gameVersion, ResolveOptions{})
	return r.File, err
}

// ── İndirme ─────────────────────────────────────────────────────────────────

// DownloadTo downloads file into destDir, returning the written path. The
// destination directory is created if needed.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// Dosya doğrudan plugins/ADI.jar'a yazılıyordu. İndirme yarıda kesilirse
// (bağlantı koptu, zaman aşımı) klasörde YARIM bir jar kalıyordu; sunucu bir
// sonraki açılışta onu yüklemeye çalışıp "zip END header not found" hatası
// veriyordu ve kullanıcı sebebini göremiyordu. Artık geçici bir .part dosyasına
// yazılır, boyut ve Modrinth özeti (sha512/sha1) doğrulanır, ancak sonra
// yerine taşınır. Sunucu yalnızca *.jar yüklediği için .part dosyası zararsız.
func (c *Client) DownloadTo(ctx context.Context, file File, destDir string) (string, error) {
	if file.URL == "" {
		return "", fmt.Errorf("empty file url")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	dl := c.dl
	if dl == nil {
		dl = c.http
	}
	resp, err := dl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", file.Filename, resp.Status)
	}
	name := filepath.Base(file.Filename)
	if name == "" || name == "." || name == "/" || name == string(filepath.Separator) {
		name = "download.jar"
	}
	dest := filepath.Join(destDir, name)

	tmp, err := os.CreateTemp(destDir, ".mcos-indirme-*.part")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	fail := func(e error) (string, error) {
		tmp.Close()
		os.Remove(tmpName)
		return "", e
	}

	// Güçlü özet varsa o kullanılır; Modrinth ikisini de yayımlıyor.
	var sumName string
	var sum hash.Hash
	switch {
	case file.Hashes["sha512"] != "":
		sumName, sum = "sha512", sha512.New()
	case file.Hashes["sha1"] != "":
		sumName, sum = "sha1", sha1.New()
	}
	w := io.Writer(tmp)
	if sum != nil {
		w = io.MultiWriter(tmp, sum)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return fail(fmt.Errorf("%s indirilemedi: %w", name, err))
	}
	if file.Size > 0 && n != file.Size {
		return fail(fmt.Errorf("%s eksik indirildi: %d / %d bayt", name, n, file.Size))
	}
	if sum != nil {
		if got := hex.EncodeToString(sum.Sum(nil)); !strings.EqualFold(got, file.Hashes[sumName]) {
			return fail(fmt.Errorf("%s bozuk indirildi (%s uyuşmuyor)", name, sumName))
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return dest, nil
}

// Installed is the outcome of Install.
type Installed struct {
	Resolved
	// Path, diske yazılan dosyanın tam yoludur.
	Path string
}

// Install resolves the best file for the server and downloads it into destDir.
func (c *Client) Install(ctx context.Context, idOrSlug, loader, gameVersion, destDir string, opt ResolveOptions) (Installed, error) {
	r, err := c.Resolve(ctx, idOrSlug, loader, gameVersion, opt)
	if err != nil {
		return Installed{}, err
	}
	p, err := c.DownloadTo(ctx, r.File, destDir)
	if err != nil {
		return Installed{}, err
	}
	return Installed{Resolved: r, Path: p}, nil
}

// InstallByID searches by exact slug/id and installs the best compatible file
// into destDir. Convenience wrapper used for known helper projects.
//
// Yükleyici zinciri burada da geçerlidir: compat.go'nun Purpur/Folia
// sunucularına ViaVersion kurabilmesi buna bağlı.
func (c *Client) InstallByID(ctx context.Context, idOrSlug, loader, gameVersion, destDir string) (string, error) {
	in, err := c.Install(ctx, idOrSlug, loader, gameVersion, destDir, ResolveOptions{})
	if err != nil {
		return "", err
	}
	return in.Path, nil
}
