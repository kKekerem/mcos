// Package linkjar, ortak dünya (MCOS Link) jar'ını sunucunun Minecraft
// sürümüne göre seçer.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN AYRI BİR PAKET
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının gerçek raporu: "PC eşleştirmede ortak dünyayı açınca 'mcos link
// kurulu değil' diyor." Sebep ölçüldü: mod yalnızca Minecraft 1.21.11 için
// derlenmişti ve daemon bunu tek bir sabitle (fabricLinkMC = "1.21.11")
// biliyordu. Minecraft takvim sürümlerine geçti (26.3, 26.2, 26.1.2 …);
// çevrimiçi kurulan bir sunucu varsayılan olarak 26.3 alıyor, mod hiç
// kurulmuyordu ve kullanıcı nedenini göremiyordu.
//
// Artık her sürüm için ayrı bir jar var ve hangisinin hangi sürüme gittiğini
// bir İNDEKS söyler. Aynı seçimi hem MCOS kutusundaki mcosd hem de Windows/
// Linux'taki mcos-node yapmak zorunda: iki makine aynı dünyanın iki yarısını
// çalıştırır ve biri başka bir jar seçerse sınırı geçen oyuncu düşer. Bu
// yüzden kural TEK YERDE, daemon'a bağımlı olmayan bu pakette.
//
// ── İndeks biçimi (mod derleyen işlerle ortak sözleşme) ─────────────────────
//
//	index-fabric.tsv, index-paper.tsv (ve başka index-*.tsv), SEKME ayraçlı:
//	<yükleyici>\t<mc-sürümü>\t<mod-jar-adı>\t<bağımlılık-jar-adı ya da ->
//
//	fabric	1.21.11	mcos-link-fabric-1.21.11.jar	fabric-api-0.141.6+1.21.11.jar
//	paper	26.3+	mcos-link-paper.jar	-
//
// "#" ile başlayan ve boş satırlar yok sayılır. Sürümün sonundaki "+" o
// sürümü VE SONRASINI kapsar. Tam eşleşen satır "+" satırına üstün gelir.
// Sürümler SAYISAL karşılaştırılır: "26.1" > "1.21.11", "1.21.10" > "1.21.9".
package linkjar

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"mcos/internal/model"
)

// Loader, jar'ın hangi yükleyici platformu için derlendiğidir.
type Loader string

const (
	// Fabric modları mods/ altından yüklenir.
	Fabric Loader = "fabric"
	// Paper eklentileri plugins/ altından yüklenir (Purpur, Spigot … aynı API).
	Paper Loader = "paper"
)

// Eski (indekssiz) düzen: /usr/lib/mcos/mods/mcos-link.jar ve
// mcos-link-paper.jar. İkisi de YALNIZCA 1.21.11 için derlenmişti: Fabric
// jar'ı 1.21.11'in iç alanlarını kullanıyor (1.21.1'de NoSuchFieldError ile
// düştüğü uçtan uca sınamada görüldü), Paper jar'ının plugin.yml'sinde
// api-version 1.21.11 yazıyor ve daha eski Paper onu "Unsupported API
// version" ile reddediyor.
const (
	LegacyFabricJar = "mcos-link.jar"
	LegacyPaperJar  = "mcos-link-paper.jar"
	LegacyMC        = "1.21.11"
	// LegacyFabricDep, eski jar'ın yanında denenen fabric-api'dir; çevrimdışı
	// paketteki adla aynı (offline-manifest.txt).
	LegacyFabricDep = "fabric-api-0.141.6+1.21.11.jar"
)

// Kind, bir seçim hatasının türüdür. Çağıranlar türe göre farklı davranır:
// yalnızca KindVersion'da sunucudaki eski jar kaldırılır.
type Kind int

const (
	// KindUnsupported: bu yazılım ortak dünya jar'ını hiç yükleyemez.
	KindUnsupported Kind = iota + 1
	// KindVersion: yazılım uygun ama bu Minecraft sürümü için jar yok.
	KindVersion
	// KindMissing: indeks ya da indeksin gösterdiği dosya yok (imaj/paket eksik).
	KindMissing
)

// Problem is a selection failure with a Turkish, user-facing reason.
type Problem struct {
	Kind Kind
	Text string
}

func (p *Problem) Error() string { return p.Text }

// KindOf returns the Problem kind of err (0 when err is not a Problem).
func KindOf(err error) Kind {
	var p *Problem
	if errors.As(err, &p) {
		return p.Kind
	}
	return 0
}

func problem(k Kind, format string, args ...any) *Problem {
	return &Problem{Kind: k, Text: fmt.Sprintf(format, args...)}
}

// Entry is one index line, resolved to absolute paths.
type Entry struct {
	Loader Loader
	// MC, "+" olmadan sürüm ("26.3").
	MC string
	// AndLater: satır "26.3+" biçimindeydi.
	AndLater bool
	// Jar, mod jar'ının MUTLAK yolu (indeksin durduğu klasörde).
	Jar string
	// Dep, bağımlılığın dosya ADI ("" = yok). Yol değil: fabric-api kök
	// dosya sistemine girmez (RAM bedeli), çevrimdışı paketle /data/artifacts'a
	// AYNI adla gelir; nerede bulunacağı FindDep'in işi.
	Dep string
	// Index, satırın okunduğu dosyanın mutlak yolu (hata iletileri için).
	Index string
}

// Result is the jar chosen for one server.
type Result struct {
	Loader Loader
	// MC, eşleşen satırın sürümü ("26.3", "+" satırıysa "26.3+").
	MC string
	// Jar, kopyalanacak mod jar'ının mutlak yolu.
	Jar string
	// Dep, bağımlılığın dosya adı ("" = yok).
	Dep string
	// DepPath, bağımlılığın yerelde bulunan kopyası; "" = bulunamadı (çağıran
	// Modrinth'e düşer).
	DepPath string
	// Legacy: eski, indekssiz düzenden geldi.
	Legacy bool
}

// Locator knows where the jars live on this machine.
//
// Arama yolları alan olarak durur, çünkü mcosd (/usr/lib/mcos/mods/link …) ile
// mcos-node (programın klasörü) başka yerlere bakar ve sınamalar sahte
// klasörler verebilmeli.
type Locator struct {
	// Dirs, index-*.tsv ve mod jar'larının arandığı klasörler; öncelik
	// sırasıyla (aynı sürüm iki klasörde varsa ilk kazanır).
	Dirs []string
	// LegacyDirs, eski düzenin (indekssiz mcos-link.jar) arandığı klasörler.
	LegacyDirs []string
	// DepDirs, bağımlılık jar'ının (fabric-api) ayrıca arandığı klasörler.
	// Önce satırın kendi klasörüne bakılır.
	DepDirs []string
}

// LoaderFor maps server software to the link loader.
//
// Kural internal/daemon/handlers_link.go linkArtifact ile AYNI: Fabric ->
// mods/, eklenti yükleyen her yazılım -> plugins/. Quilt, Forge, NeoForge ve
// vanilla REDDEDİLİR:
//   - Forge/NeoForge Fabric biçimli bir modu hiç tanımaz. Eskiden
//     SupportsMods() kullanıldığı için jar oraya da kopyalanıyor, panel "mod
//     kurulu" diyor, ortak dünya hiç çalışmıyordu.
//   - Quilt Fabric modlarını çalıştırabilir ama fabric-api'nin kendisini
//     değil; orada ayrı bir "Quilted Fabric API" gerekir ve bu kombinasyon
//     denenmedi.
//   - Vanilla ne mod ne eklenti yükler.
func LoaderFor(sw model.Software) (Loader, error) {
	switch {
	case sw == model.SoftwareFabric:
		return Fabric, nil
	case sw.SupportsPlugins():
		return Paper, nil
	case strings.TrimSpace(string(sw)) == "":
		return "", problem(KindUnsupported,
			"sunucu yazılımı bilinmiyor; ortak dünya için Fabric veya Paper seçin")
	}
	return "", problem(KindUnsupported,
		"%s ortak dünyayı desteklemiyor; Fabric veya Paper seçin", Label(sw))
}

// Label is the display name of a server software.
func Label(sw model.Software) string {
	switch sw {
	case model.SoftwareFabric:
		return "Fabric"
	case model.SoftwarePaper:
		return "Paper"
	case model.SoftwarePurpur:
		return "Purpur"
	case model.SoftwareSpigot:
		return "Spigot"
	case model.SoftwareCraftBukkit:
		return "CraftBukkit"
	case model.SoftwareFolia:
		return "Folia"
	case model.SoftwareForge:
		return "Forge"
	case model.SoftwareNeoForge:
		return "NeoForge"
	case model.SoftwareQuilt:
		return "Quilt"
	case model.SoftwareVanilla:
		return "Vanilla"
	}
	s := string(sw)
	if s == "" {
		return "?"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// noun, kullanıcıya gösterilen ad: Fabric'te "mod", Paper'da "eklenti".
func noun(l Loader) string {
	if l == Fabric {
		return "modu"
	}
	return "eklentisi"
}

func legacyJar(l Loader) string {
	if l == Fabric {
		return LegacyFabricJar
	}
	return LegacyPaperJar
}

// Entries reads every index-*.tsv in l.Dirs, in priority order.
//
// Okunamayan klasörler sessizce atlanır: /usr/lib/mcos/mods/link geliştirme
// makinesinde, dist/mods/link imajda yoktur; ikisi de olağan.
func (l Locator) Entries() []Entry {
	var out []Entry
	for _, dir := range l.Dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		names, err := os.ReadDir(abs)
		if err != nil {
			continue
		}
		var idx []string
		for _, n := range names {
			if n.IsDir() {
				continue
			}
			if ok, _ := filepath.Match("index-*.tsv", n.Name()); ok {
				idx = append(idx, n.Name())
			}
		}
		sort.Strings(idx) // ReadDir zaten sıralı; yine de sıra sözleşmedir
		for _, name := range idx {
			out = append(out, parseIndex(filepath.Join(abs, name))...)
		}
	}
	return out
}

// parseIndex reads one index file. Bozuk satırlar ATLANIR: tek bir hatalı
// satır bütün sürümleri kullanılamaz kılmamalı.
func parseIndex(path string) []Entry {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(path)
	var out []Entry
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		e := Entry{Loader: Loader(strings.ToLower(f[0])), Index: path}
		e.MC = f[1]
		if strings.HasSuffix(e.MC, "+") {
			e.AndLater = true
			e.MC = strings.TrimSpace(strings.TrimSuffix(e.MC, "+"))
			// "+" ancak sayısal bir sürümle anlamlı: "26.3-rc1+" gibi bir
			// satırın neyi kapsadığı belirsiz.
			if _, ok := parseVersion(e.MC); !ok {
				continue
			}
		}
		if e.Loader == "" || e.MC == "" || !plainName(f[2]) {
			continue
		}
		e.Jar = filepath.Join(dir, f[2])
		if len(f) >= 4 && f[3] != "" && f[3] != "-" {
			if !plainName(f[3]) {
				continue
			}
			e.Dep = f[3]
		}
		out = append(out, e)
	}
	return out
}

// plainName: indeks yalnızca dosya ADI taşır. "../x.jar" ya da mutlak bir yol
// indeksin klasörü dışındaki bir dosyayı sunucuya kopyalatırdı.
func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) &&
		filepath.Base(s) == s
}

// usable: boş dosya SAYILMAZ. Yarım kalmış bir kopya "var" diye geçilirse
// Minecraft onu yüklemeye çalışıp açılışta çöker ve nedeni görünmez.
func usable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

// Find returns the jar for a server, or a Turkish reason why there is none.
func (l Locator) Find(sw model.Software, mcVersion string) (Result, error) {
	loader, err := LoaderFor(sw)
	if err != nil {
		return Result{}, err
	}
	mc := strings.TrimSpace(mcVersion)
	if mc == "" {
		return Result{}, problem(KindVersion,
			"sunucunun Minecraft sürümü bilinmiyor; ortak dünya %s seçilemedi", noun(loader))
	}

	var mine []Entry
	for _, e := range l.Entries() {
		if e.Loader == loader {
			mine = append(mine, e)
		}
	}

	var missing []Entry
	// 1. Tam eşleşme: "+" satırından her zaman üstündür.
	for _, e := range mine {
		if e.AndLater || !SameVersion(e.MC, mc) {
			continue
		}
		if usable(e.Jar) {
			return l.result(e, e.MC), nil
		}
		missing = append(missing, e)
	}

	// 2. "+" satırları: sunucu sürümüne eşit ya da küçük en YÜKSEK taban.
	var plus []Entry
	for _, e := range mine {
		if !e.AndLater {
			continue
		}
		if c, ok := Compare(mc, e.MC); ok && c >= 0 {
			plus = append(plus, e)
		}
	}
	sort.SliceStable(plus, func(i, j int) bool {
		c, _ := Compare(plus[i].MC, plus[j].MC)
		return c > 0
	})
	for _, e := range plus {
		if usable(e.Jar) {
			return l.result(e, e.MC+"+"), nil
		}
		missing = append(missing, e)
	}

	// 3. Eski düzen: indekssiz jar, yalnızca 1.21.11.
	legacy := l.legacyPath(loader)
	if legacy != "" && SameVersion(mc, LegacyMC) {
		r := Result{Loader: loader, MC: LegacyMC, Jar: legacy, Legacy: true}
		if loader == Fabric {
			r.Dep = LegacyFabricDep
			r.DepPath = l.FindDep(LegacyFabricDep, filepath.Dir(legacy))
		}
		return r, nil
	}

	label := Label(sw)
	if len(missing) > 0 {
		e := missing[0]
		return Result{}, problem(KindMissing,
			"%s %s için ortak dünya %s kayıtlı ama %s yok ya da boş (%s)",
			label, mc, noun(loader), filepath.Base(e.Jar), e.Index)
	}
	if len(mine) == 0 && legacy == "" {
		return Result{}, problem(KindMissing,
			"ortak dünya %s bulunamadı: index-%s.tsv yok (aranan: %s)",
			noun(loader), loader, strings.Join(l.Dirs, ", "))
	}
	return Result{}, problem(KindVersion, "%s %s için ortak dünya %s yok (%s)",
		label, mc, noun(loader), supportedText(mine, legacy != "", mc))
}

func (l Locator) result(e Entry, mc string) Result {
	r := Result{Loader: e.Loader, MC: mc, Jar: e.Jar, Dep: e.Dep}
	if e.Dep != "" {
		r.DepPath = l.FindDep(e.Dep, filepath.Dir(e.Jar))
	}
	return r
}

// legacyPath finds the old, index-less jar.
func (l Locator) legacyPath(loader Loader) string {
	name := legacyJar(loader)
	for _, dir := range l.LegacyDirs {
		p := filepath.Join(dir, name)
		if usable(p) {
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
			return p
		}
	}
	return ""
}

// supportedText lists what the index covers, e.g. "desteklenen: 1.20.5–26.3".
//
// Sürüm aralığın İÇİNDE ama tam satırı yoksa (ara sürüm denenmediyse) aralık
// tek başına yanıltır: kullanıcı "1.21.3 de desteklenir" sanır. O durumda en
// yakın iki desteklenen sürüm de yazılır ki kullanıcı birini seçebilsin.
func supportedText(entries []Entry, legacy bool, mc string) string {
	type v struct {
		s     string
		later bool
	}
	var all []v
	seen := map[string]bool{}
	for _, e := range entries {
		k := e.MC
		if e.AndLater {
			k += "+"
		}
		if !seen[k] {
			seen[k] = true
			all = append(all, v{e.MC, e.AndLater})
		}
	}
	if legacy && !seen[LegacyMC] {
		all = append(all, v{LegacyMC, false})
	}
	sort.SliceStable(all, func(i, j int) bool {
		c, ok := Compare(all[i].s, all[j].s)
		if !ok {
			return all[i].s < all[j].s
		}
		if c == 0 {
			return !all[i].later && all[j].later
		}
		return c < 0
	})
	if len(all) == 0 {
		return "desteklenen sürüm yok"
	}
	lo, hi := all[0], all[len(all)-1]
	text := lo.s
	if hi.s != lo.s {
		text += "–" + hi.s
	}
	// "+" satırı EN YÜKSEK satır olmasa da üst uç açıktır: "1.21.1+" ile
	// tam "26.3" satırı olan bir indekste 26.4 de seçiliyordu ama ileti
	// "desteklenen: 1.21.1–26.3" diyordu (sınamada ölçüldü).
	open := false
	for _, x := range all {
		open = open || x.later
	}
	if open {
		text += " ve sonrası"
	}
	text = "desteklenen: " + text

	// Aralık içindeki boşluk: en yakın alt ve üst sürüm.
	cLo, ok1 := Compare(mc, lo.s)
	cHi, ok2 := Compare(mc, hi.s)
	if ok1 && ok2 && cLo > 0 && cHi < 0 {
		var below, above string
		for _, x := range all {
			c, ok := Compare(x.s, mc)
			if !ok {
				continue
			}
			if c < 0 && !x.later {
				below = x.s
			}
			if c > 0 && above == "" {
				above = x.s
			}
		}
		var near []string
		for _, s := range []string{below, above} {
			if s != "" {
				near = append(near, s)
			}
		}
		if len(near) > 0 {
			text += "; en yakın: " + strings.Join(near, ", ")
		}
	}
	return text
}

// ── Sürüm karşılaştırma ─────────────────────────────────────────────────────

// parseVersion splits "1.21.11" into [1 21 11]. Sayı olmayan parça (ön
// sürüm, anlık görüntü: "26.1-pre1", "25w14a") false döndürür: onlar yalnızca
// birebir yazımla eşleşir, "+" satırına girmez.
func parseVersion(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || (p[0] == '+' || p[0] == '-') {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// Compare compares two Minecraft versions NUMERICALLY.
//
// Metin olarak karşılaştırmak iki yerde yanlış sonuç verir ve ikisi de
// gerçek sürümlerdir: "1.21.10" < "1.21.9" ve "26.1" < "1.21.11". Eksik
// parça 0 sayılır ("1.21" == "1.21.0"). ok=false: sürümlerden biri sayısal
// değil.
func Compare(a, b string) (int, bool) {
	va, ok1 := parseVersion(a)
	vb, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := 0; i < len(va) || i < len(vb); i++ {
		x, y := 0, 0
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// SameVersion reports whether a and b name the same Minecraft version.
func SameVersion(a, b string) bool {
	if c, ok := Compare(a, b); ok {
		return c == 0
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// ── Bağımlılık (fabric-api) ─────────────────────────────────────────────────

// FindDep looks for a dependency jar by name: first in near (the index's own
// folder), then in l.DepDirs.
//
// fabric-api kök dosya sistemine GİRMEZ (her jar ~2 MB, 20 sürüm = 40 MB RAM
// olurdu); çevrimdışı paketle ISO'ya gelir ve mcos-install onu AYNI adla
// /data/artifacts'a kopyalar. Eski çevrimdışı paket ise önbellek adlandırması
// kullanıyor (providers.cachePath: "<16 hane>-<temizlenmiş ad>", URL'deki
// "+" orada "%2B" -> "_2B" olur); o biçimler de tanınır ki paketteki dosya
// varken ağa çıkılmasın.
func (l Locator) FindDep(name, near string) string {
	if !plainName(name) {
		return ""
	}
	suffixes := []string{"-" + name}
	for _, s := range []string{sanitize(name), sanitize(url.PathEscape(name)),
		sanitize(strings.ReplaceAll(name, "+", "%2B"))} {
		if s != name {
			suffixes = append(suffixes, "-"+s)
		}
	}
	dirs := append([]string{}, near)
	dirs = append(dirs, l.DepDirs...)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, name); usable(p) {
			return p
		}
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		names, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, n := range names {
			for _, suf := range suffixes {
				if strings.HasSuffix(n.Name(), suf) {
					if p := filepath.Join(dir, n.Name()); usable(p) {
						return p
					}
				}
			}
		}
	}
	return ""
}

// sanitize, providers.sanitizeName ile AYNI kural (önbellek dosya adı).
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// FabricAPIGlobs are the file-name shapes Fabric API ships under.
//
// Modrinth "fabric-api-0.141.6+1.21.11.jar" verir, önbellek başına URL özeti
// ekler ("<16 hane>-fabric-api-….jar"), eski daemon ve düğüm paketten
// kopyaladığını "fabric-api.jar" adıyla koyuyordu.
var FabricAPIGlobs = []string{
	"fabric-api-*.jar",
	"*-fabric-api-*.jar",
	"fabric_api*.jar",
	"fabric-api.jar",
}

// modInfo is the part of fabric.mod.json we read.
type modInfo struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func readModInfo(path string) (modInfo, bool) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return modInfo{}, false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "fabric.mod.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return modInfo{}, false
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return modInfo{}, false
		}
		var mi modInfo
		if json.Unmarshal(b, &mi) != nil {
			return modInfo{}, false
		}
		return mi, true
	}
	return modInfo{}, false
}

// FabricAPIsIn lists the Fabric API jars in a mods folder.
//
// Ad kalıbı TEK BAŞINA yetmez: "*-fabric-api-*.jar" kalıbı
// "harika-fabric-api-eklentisi-1.0.jar" gibi BAŞKA bir modu da tutar ve
// aşağıdaki temizlik onu silerdi. fabric.mod.json okunabiliyorsa kimlik
// "fabric-api" olmalı; okunamıyorsa (bozuk ya da sahte jar) ada güvenilir.
func FabricAPIsIn(dir string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range FabricAPIGlobs {
		matches, _ := filepath.Glob(filepath.Join(dir, g))
		for _, m := range matches {
			if seen[m] || !usable(m) {
				continue
			}
			seen[m] = true
			if mi, ok := readModInfo(m); ok && mi.ID != "" && mi.ID != "fabric-api" {
				continue
			}
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// FabricAPIMC returns the Minecraft version a fabric-api jar was built for.
//
// fabric-api sürümü "<api>+<minecraft>" biçimindedir ("0.141.6+1.21.11",
// "0.150.0+26.3"). Önce jar'ın içindeki fabric.mod.json'a, yoksa dosya adına
// bakılır. "" = anlaşılamadı.
func FabricAPIMC(path string) string {
	if mi, ok := readModInfo(path); ok {
		if i := strings.LastIndex(mi.Version, "+"); i >= 0 && i+1 < len(mi.Version) {
			return mi.Version[i+1:]
		}
	}
	return depNameMC(filepath.Base(path))
}

// depNameMC reads the MC part of "fabric-api-0.150.0+26.3.jar" -> "26.3".
func depNameMC(name string) string {
	name = strings.TrimSuffix(name, ".jar")
	for _, sep := range []string{"+", "_2B"} {
		if i := strings.LastIndex(name, sep); i >= 0 {
			if mc := name[i+len(sep):]; mc != "" {
				if _, ok := parseVersion(mc); ok {
					return mc
				}
			}
		}
	}
	return ""
}

// FabricAPIFor finds, in dirs, a fabric-api built for mc — whatever its build
// number.
//
// İndeksin adıyla (Result.DepPath) bulunamadığında devreye girer: çevrimdışı
// paket aynı Minecraft sürümü için BAŞKA bir yapı taşıyabilir. Sürümü
// TUTMAYAN bir kopya asla seçilmez — eskiden ilk bulunan fabric-api alınıyordu
// ve 26.3 sunucusuna 1.21.11'inki kopyalanırdı.
func FabricAPIFor(dirs []string, mc string) string {
	for _, dir := range dirs {
		for _, p := range FabricAPIsIn(dir) {
			if SameVersion(FabricAPIMC(p), mc) {
				return p
			}
		}
	}
	return ""
}

// CleanCacheName strips the "<16 hex>-" prefix the offline cache adds
// (providers.cachePath): mods/ içinde kullanıcı gerçek adı görmeli.
func CleanCacheName(name string) string {
	if len(name) > 17 && name[16] == '-' {
		if _, err := strconv.ParseUint(name[:16], 16, 64); err == nil {
			return name[17:]
		}
	}
	return name
}

// PrepareFabricAPI leaves exactly one FITTING fabric-api in modsDir.
//
// ── Neden bu kadar dikkat ───────────────────────────────────────────────────
// mcos-link fabric-api'yi SERT bağımlılık olarak bildiriyor; eksikse Fabric
// Loader modu atlamaz, SUNUCUYU HİÇ AÇMAZ ("requires any version of
// fabric-api, which is missing!" — gerçek bir 1.21.11 sunucusunda görüldü).
// Eskiden "mods/ içinde herhangi bir fabric-api var mı" diye bakılıyordu. Ama
// sürüm değiştirilen bir sunucuda (1.21.11 -> 26.3) mods/ içinde 1.21.11'in
// fabric-api'si kalır: "var" denip geçilirdi ve 26.3 sunucusu o jar yüzünden
// açılmazdı. Doğru olan, İNDEKSİN söylediği fabric-api'yi koymak ve yanlış
// sürümdekini kaldırmaktır. İki fabric-api'yi birlikte bırakmak da olmaz:
// yükleyici aynı kimlikli iki modu reddeder.
//
// Sıra (önce yerel — MCOS internetsiz çalışabilmeli):
//  1. mods/ içinde indeksteki adla ya da aynı Minecraft sürümü için bir
//     fabric-api varsa o kalır,
//  2. yoksa yerel kopya (r.DepPath) kopyalanır,
//  3. o da yoksa fetch (Modrinth) denenir,
//  4. hiçbiri olmazsa ve sürümü ANLAŞILAMAYAN bir fabric-api varsa o bırakılır
//     (eski davranış; yanlış olduğunu bilmiyoruz), yoksa HATA döner.
//
// Sağlanan fabric-api dışındaki bütün fabric-api jar'ları kaldırılır. Dönüş:
// günlüğe yazılacak kısa açıklama.
func PrepareFabricAPI(modsDir string, r Result, mc string, fetch func() (string, error)) (string, error) {
	want := depNameMC(r.Dep)
	if want == "" {
		want = strings.TrimSpace(mc)
	}
	have := FabricAPIsIn(modsDir)
	var keep string
	// İndeksin adını taşıyan kopya her zaman önce gelir: onu biz koyduk ve
	// sürümü indeksle aynı.
	if r.Dep != "" {
		for _, p := range have {
			if filepath.Base(p) == r.Dep {
				keep = p
				break
			}
		}
	}
	var wrong, unknown []string
	for _, p := range have {
		if p == keep {
			continue
		}
		got := FabricAPIMC(p)
		switch {
		case keep == "" && got != "" && (SameVersion(got, want) || SameVersion(got, mc)):
			// Aynı Minecraft sürümü için başka bir yapı (kullanıcı güncellemiş
			// olabilir): uyumlu, olduğu gibi kalır.
			keep = p
		case got == "":
			unknown = append(unknown, p)
		default:
			// Başka bir sürüm İÇİN ya da uygun olanın ikinci kopyası.
			wrong = append(wrong, p)
		}
	}

	note := ""
	switch {
	case keep != "":
		note = "fabric-api zaten uygun: " + filepath.Base(keep)
	case r.DepPath != "":
		dst := filepath.Join(modsDir, r.Dep)
		if err := copyAtomic(r.DepPath, dst); err != nil {
			return "", fmt.Errorf("fabric-api kopyalanamadı: %w", err)
		}
		keep = dst
		note = "fabric-api yerel depodan kuruldu: " + r.DepPath
	default:
		var ferr error
		if fetch != nil {
			var p string
			p, ferr = fetch()
			if ferr == nil && p != "" {
				keep = p
				note = "fabric-api indirildi: " + filepath.Base(p)
			} else if ferr == nil {
				ferr = errors.New("indirme dosya döndürmedi")
			}
		} else {
			ferr = errors.New("indirme kapalı")
		}
		if keep == "" {
			if len(unknown) > 0 {
				// Sürümü okunamayan bir fabric-api: yanlış olduğunu
				// BİLMİYORUZ; silmek çalışan bir kurulumu bozabilirdi.
				return "fabric-api sürümü anlaşılamadı, olduğu gibi bırakıldı: " +
					filepath.Base(unknown[0]), nil
			}
			name := r.Dep
			if name == "" {
				name = "Minecraft " + mc + " için fabric-api"
			}
			msg := fmt.Sprintf("%s bulunamadı ve indirilemedi (%v)", name, ferr)
			if len(wrong) > 0 {
				msg += fmt.Sprintf("; mods/ içindeki %s başka bir Minecraft sürümü (%s) için",
					filepath.Base(wrong[0]), FabricAPIMC(wrong[0]))
			}
			return "", errors.New(msg)
		}
	}

	// Sağlanan dışındakiler: yanlış sürüm, sürümü okunamayan ve kopyalar.
	var removed []string
	for _, p := range append(wrong, unknown...) {
		if p == keep {
			continue
		}
		if err := os.Remove(p); err == nil {
			removed = append(removed, filepath.Base(p))
		}
	}
	if len(removed) > 0 {
		note += "; uymayan fabric-api kaldırıldı: " + strings.Join(removed, ", ")
	}
	return note, nil
}

// copyAtomic copies src to dst via a temp file + rename.
//
// Doğrudan yazmak yarım bir jar bırakabilir; Minecraft onu yüklemeye çalışıp
// açılışta çöker ve hatanın nedeni hiç anlaşılmaz.
func copyAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
