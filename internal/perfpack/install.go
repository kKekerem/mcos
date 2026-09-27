package perfpack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"mcos/internal/catalog"
	"mcos/internal/model"
	"mcos/internal/netprobe"
)

// Catalog is the part of the Modrinth client the installer needs.
//
// Arayüz, sınamalar httptest üzerindeki sahte Modrinth'le gerçek
// catalog.Client'ı kullanabilsin diye; üretimde daemon'un d.catalog'u geçer.
type Catalog interface {
	Versions(ctx context.Context, idOrSlug, loader, gameVersion string) ([]catalog.Version, error)
	DownloadTo(ctx context.Context, file catalog.File, destDir string) (string, error)
}

// Installer applies packs to server folders.
type Installer struct {
	Catalog Catalog
	// Online, internet olup olmadığını söyler. nil = netprobe.Check (HTTPS
	// çıkışını ölçer, 10 sn önbellekli).
	Online func() bool
	// Logf, ayrıntı günlüğüdür (nil = sessiz).
	Logf func(format string, args ...any)
	// Now, sınamalarda zamanı sabitlemek için (nil = time.Now).
	Now func() time.Time
}

// Request says which server to apply the pack to.
type Request struct {
	Software model.Software
	MC       string
	// DataDir, sunucunun veri klasörüdür (mods/, config/ burada).
	DataDir string
	// Prev, sunucu kaydındaki önceki paket durumudur (nil = ilk kurulum).
	Prev *model.PerfPackState
}

// Skip is one pack item that was not installed, with the reason.
type Skip struct {
	Name   string
	Reason string
}

// Result is what one Apply did.
type Result struct {
	Pack Pack
	// State, sunucu kaydına yazılacak yeni durumdur.
	State model.PerfPackState
	// Installed, bu çalışmada indirilenlerdir; Kept zaten güncel olanlar.
	Installed []model.PerfPackItem
	Kept      []model.PerfPackItem
	// Removed, kaldırılan bayat paket dosyalarıdır (yalnızca paketin kendi
	// koyduğu dosyalar; kullanıcınınkine dokunulmaz).
	Removed []string
	Skipped []Skip
	// Changed, bu çalışmada değiştirilen ayarlardır.
	Changed []string
	// Pending, dosyası henüz oluşmadığı için ilk açılıştan sonra yazılacak
	// ayarlardır.
	Pending []string
	// Offline: internet yoktu, modlar atlandı.
	Offline bool
}

// OfflineNote is the log/panel line when mods are skipped for lack of
// internet (kullanıcının istediği ifade).
const OfflineNote = "internet yok, performans paketi atlandı"

// Summary is the one-line Turkish outcome for the panel.
func (r Result) Summary() string {
	if r.Pack.Empty() {
		if r.Pack.Note != "" {
			return r.Pack.Note
		}
		return "bu sunucu için performans paketi yok"
	}
	var parts []string
	if r.Offline {
		parts = append(parts, OfflineNote)
	}
	if n := len(r.Installed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d mod kuruldu (%s)", n, itemNames(r.Installed)))
	}
	if n := len(r.Kept); n > 0 && len(r.Installed) == 0 {
		parts = append(parts, fmt.Sprintf("%d mod zaten güncel", n))
	}
	if n := len(r.Changed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d ayar yapıldı", n))
	} else if len(r.Pack.Settings) > 0 && len(r.State.Settings) > 0 {
		parts = append(parts, fmt.Sprintf("%d ayar zaten uygulanmış", len(r.State.Settings)))
	}
	if n := len(r.Pending); n > 0 {
		parts = append(parts, fmt.Sprintf("%d ayar ilk açılıştan sonra yazılacak", n))
	}
	if n := len(r.Removed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d eski dosya kaldırıldı", n))
	}
	if n := len(r.Skipped); n > 0 && !r.Offline {
		names := make([]string, 0, n)
		for _, s := range r.Skipped {
			names = append(names, s.Name)
		}
		parts = append(parts, fmt.Sprintf("%d atlandı (%s)", n, strings.Join(names, ", ")))
	}
	if len(parts) == 0 {
		return "performans paketinde uygulanacak bir şey yok"
	}
	return strings.Join(parts, "; ")
}

func itemNames(its []model.PerfPackItem) string {
	names := make([]string, 0, len(its))
	for _, it := range its {
		n := it.Name
		if n == "" {
			n = it.Slug
		}
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func (in *Installer) logf(format string, args ...any) {
	if in.Logf != nil {
		in.Logf(format, args...)
	}
}

func (in *Installer) online() bool {
	if in.Online != nil {
		return in.Online()
	}
	return netprobe.Check().Online
}

func (in *Installer) now() time.Time {
	if in.Now != nil {
		return in.Now()
	}
	return time.Now()
}

// Apply installs (or updates) the pack for req.
//
// Ayarlar internet GEREKTİRMEZ ve her durumda uygulanır; modlar yalnızca
// internet varsa indirilir. İnternet yoksa hiçbir ağ isteği yapılmaz (panel
// "kuruluyor" diye dakikalarca beklemesin) ve OfflineNote döner.
//
// Hata yalnızca klasöre hiç yazılamadığında döner; tek tek modların
// başarısızlığı Skipped'e yazılır — bir modun indirilememesi ötekileri
// engellememeli.
func (in *Installer) Apply(ctx context.Context, req Request) (Result, error) {
	p := For(req.Software, req.MC)
	res := Result{Pack: p}
	st := model.PerfPackState{MC: p.MC, At: in.now()}

	var prev []model.PerfPackItem
	if req.Prev != nil {
		prev = req.Prev.Items
	}

	if len(p.Mods) > 0 || len(prev) > 0 {
		dir := filepath.Join(req.DataDir, orDefault(p.Dir, "mods"))
		items, err := in.applyMods(ctx, p, dir, prev, &res)
		if err != nil {
			return res, err
		}
		st.Items = items
	}

	in.applySettings(p, req.DataDir, &res, &st)

	res.State = st
	res.State.Note = res.Summary()
	return res, nil
}

// applySettings writes the pack's settings into dataDir.
//
// Dosyası HENÜZ OLMAYAN ayar yazılmaz, "bekleyen" olarak kaydedilir:
// spigot.yml ve paper-world-defaults.yml'ı sunucu İLK açılışında kendisi
// oluşturur. Onlardan önce yarım bir dosya koymak, Paper'ın sürüm geçiş
// (_version) mantığını ve açıklama satırlı varsayılan dosyayı atlatırdı.
// Bekleyenler sunucunun sonraki başlatılışından hemen önce yazılır (bkz.
// ApplySettings, daemon'un başlatma kancası).
func (in *Installer) applySettings(p Pack, dataDir string, res *Result, st *model.PerfPackState) {
	for _, s := range p.Settings {
		changed, applied, pending, why := applySetting(dataDir, s)
		switch {
		case pending:
			st.Pending = append(st.Pending, s.Key())
			res.Pending = append(res.Pending, s.Key())
		case why != "":
			res.Skipped = append(res.Skipped, Skip{Name: s.Key(), Reason: why})
			in.logf("perfpack: %s atlandı: %s", s.Key(), why)
		case applied:
			st.Settings = append(st.Settings, s.Key()+" = "+s.Value)
			if changed {
				res.Changed = append(res.Changed, s.Key())
				in.logf("perfpack: %s = %s (%s)", s.Key(), s.Value, s.Why)
			}
		}
	}
}

// ApplySettings applies only the pack's settings (ağ isteği YOK, hızlı).
//
// Daemon bunu sunucuyu başlatmadan hemen önce, kayıtta bekleyen ayar varsa
// çağırır: ilk açılış dosyaları oluşturmuştur, ayarlar bu açılıştan itibaren
// geçerli olur. Mod kayıtları (Items) olduğu gibi korunur.
func (in *Installer) ApplySettings(req Request) Result {
	p := For(req.Software, req.MC)
	res := Result{Pack: p}
	st := model.PerfPackState{MC: p.MC, At: in.now()}
	if req.Prev != nil {
		st.Items = req.Prev.Items
	}
	in.applySettings(p, req.DataDir, &res, &st)
	res.State = st
	res.State.Note = res.Summary()
	return res
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// applyMods installs the pack's mods into dir and returns the pack's items.
func (in *Installer) applyMods(ctx context.Context, p Pack, dir string, prev []model.PerfPackItem, res *Result) ([]model.PerfPackItem, error) {
	ours := map[string]model.PerfPackItem{} // dosya adı -> paketin kaydı
	prevBySlug := map[string]model.PerfPackItem{}
	for _, it := range prev {
		ours[it.File] = it
		prevBySlug[it.Slug] = it
	}
	var keep []model.PerfPackItem // yeni durumdaki öğeler

	online := len(p.Mods) > 0 && in.online()
	if len(p.Mods) > 0 && !online {
		res.Offline = true
		in.logf("perfpack: %s", OfflineNote)
		// İnternetsiz de olsa AYNI Minecraft sürümüne kurulmuş dosyalar
		// yerinde kalır; başka sürümünkiler aşağıda kaldırılır (sürüm
		// değiştirildiyse eski modlar açılışı düşürürdü).
		for _, it := range prev {
			if it.MC == p.MC && fileExists(filepath.Join(dir, it.File)) && packHas(p, it.Slug) {
				keep = append(keep, it)
			}
		}
	}

	if online {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		installed := scanMods(dir)
		packIDs := map[string]bool{}
		for _, m := range p.Mods {
			packIDs[m.ProjectID] = true
		}
		for _, m := range p.Mods {
			it, skip := in.installMod(ctx, p, m, dir, installed, ours, prevBySlug[m.Slug], packIDs, res)
			if skip != "" {
				res.Skipped = append(res.Skipped, Skip{Name: m.Name, Reason: skip})
				in.logf("perfpack: %s atlandı: %s", m.Name, skip)
				continue
			}
			keep = append(keep, it)
		}
	}

	// Bayat paket dosyalarını kaldır: önceki kayıtta olup yenisinde olmayan.
	// Yalnızca PAKETİN kaydettiği dosyalar; kullanıcı dosyası kayıtta yoktur.
	now := map[string]bool{}
	for _, it := range keep {
		now[it.File] = true
	}
	for _, it := range prev {
		if now[it.File] {
			continue
		}
		path := filepath.Join(dir, it.File)
		if !fileExists(path) {
			continue
		}
		if err := os.Remove(path); err != nil {
			in.logf("perfpack: %s kaldırılamadı: %v", it.File, err)
			keep = append(keep, it) // hâlâ diskte; kayıttan düşürme
			continue
		}
		res.Removed = append(res.Removed, it.File)
		in.logf("perfpack: eski dosya kaldırıldı: %s", it.File)
	}
	return keep, nil
}

func packHas(p Pack, slug string) bool {
	for _, m := range p.Mods {
		if m.Slug == slug {
			return true
		}
	}
	return false
}

// installMod installs one mod. Dönüş: kayıt ya da atlama nedeni.
func (in *Installer) installMod(ctx context.Context, p Pack, m Mod, dir string,
	installed map[string][]string, ours map[string]model.PerfPackItem,
	prev model.PerfPackItem, packIDs map[string]bool, res *Result) (model.PerfPackItem, string) {

	for _, c := range m.Conflicts {
		if fs := installed[c]; len(fs) > 0 {
			return model.PerfPackItem{}, fmt.Sprintf("çakışan mod kurulu: %s (%s)", c, fs[0])
		}
	}
	for _, id := range m.IDs {
		for _, f := range installed[id] {
			if _, mine := ours[f]; !mine {
				// Kullanıcının kendi kopyası: ikinci kez kurulmaz, ona da
				// dokunulmaz (sürümünü bilerek seçmiş olabilir). Paketin
				// eski kopyası da varsa kayıttan düşer ve aşağıdaki süpürme
				// onu kaldırır — iki kopya "duplicate mod" ile düşürürdü.
				return model.PerfPackItem{}, "zaten kurulu: " + f
			}
		}
	}

	v, f, loader, err := in.resolve(ctx, p, m)
	if err != nil {
		// Ağ hatası: aynı sürüme önceden kurulmuş dosya varsa korunur;
		// geçici bir Modrinth kesintisi çalışan paketi söküp atmamalı.
		if prev.File != "" && prev.MC == p.MC && fileExists(filepath.Join(dir, prev.File)) {
			return prev, ""
		}
		return model.PerfPackItem{}, err.Error()
	}
	for _, dep := range v.Required() {
		if packIDs[dep] {
			continue
		}
		// fabric-api'yi MCOS'un ortak dünya modu kurar (handlers_link.go);
		// klasörde varsa bağımlılık karşılanmıştır. İkinci kopya KURULMAZ:
		// Fabric iki kopyayı görünce "duplicate mod" ile açılmaz.
		if dep == fabricAPIProjectID && len(installed["fabric-api"]) > 0 {
			continue
		}
		return model.PerfPackItem{}, "zorunlu bağımlılık istiyor (" + dep + ")"
	}

	item := model.PerfPackItem{Slug: m.Slug, Name: m.Name, File: filepath.Base(f.Filename),
		Version: v.VersionNumber, MC: p.MC}
	target := filepath.Join(dir, item.File)
	if fileExists(target) {
		if _, mine := ours[item.File]; mine || prev.File == item.File {
			res.Kept = append(res.Kept, item)
			return item, ""
		}
		// Aynı adlı dosya var ama kimliği okunamadı ve paketin değil:
		// kullanıcınındır.
		return model.PerfPackItem{}, "zaten kurulu: " + item.File
	}
	path, err := in.Catalog.DownloadTo(ctx, f, dir)
	if err != nil {
		if prev.File != "" && prev.MC == p.MC && fileExists(filepath.Join(dir, prev.File)) {
			return prev, ""
		}
		return model.PerfPackItem{}, "indirilemedi: " + err.Error()
	}
	item.File = filepath.Base(path)
	res.Installed = append(res.Installed, item)
	in.logf("perfpack: %s %s kuruldu (%s, %s)", m.Name, v.VersionNumber, loader, item.File)
	return item, ""
}

// fabricAPIProjectID is Fabric API's Modrinth project id.
const fabricAPIProjectID = "P7dR8mSH"

// errNoBuild is the reason when Modrinth has no fitting build.
var errNoBuild = errors.New("yapısı yok")

// resolve picks the build of m for the pack's loaders and exact MC version.
//
// Seçim kuralı catalog.Resolve'dan BİLEREK daha sıkı:
//   - Minecraft sürümü AÇIKÇA etiketli olmalı (aynı çizgideki başka sürüm
//     kabul edilmez: 1.21.10 yapısı 1.21.11'de mixin hatasıyla düşebilir).
//   - Yalnızca "release"; Unstable işaretli projelerde alpha/beta da olur.
//   - Zincirde önce en özgül yükleyici (quilt sunucusunda önce quilt).
func (in *Installer) resolve(ctx context.Context, p Pack, m Mod) (catalog.Version, catalog.File, string, error) {
	var lastErr error
	for _, loader := range p.Loaders {
		vs, err := in.Catalog.Versions(ctx, m.Slug, loader, p.MC)
		if err != nil {
			lastErr = err
			continue
		}
		var best *catalog.Version
		for i := range vs {
			v := &vs[i]
			if !slices.Contains(v.GameVersions, p.MC) || !containsFold(v.Loaders, loader) {
				continue
			}
			if !m.Unstable && !strings.EqualFold(v.VersionType, "release") {
				continue
			}
			if best == nil || v.DatePublished.After(best.DatePublished) {
				best = v
			}
		}
		if best == nil {
			continue
		}
		if f, ok := primary(*best); ok {
			return *best, f, loader, nil
		}
	}
	if lastErr != nil {
		return catalog.Version{}, catalog.File{}, "", fmt.Errorf("Modrinth'e ulaşılamadı: %w", lastErr)
	}
	kind := "kararlı "
	if m.Unstable {
		kind = ""
	}
	return catalog.Version{}, catalog.File{}, "", fmt.Errorf("Minecraft %s için %s%s %w", p.MC,
		kind, strings.Join(p.Loaders, "/"), errNoBuild)
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func primary(v catalog.Version) (catalog.File, bool) {
	for _, f := range v.Files {
		if f.Primary {
			return f, true
		}
	}
	if len(v.Files) > 0 {
		return v.Files[0], true
	}
	return catalog.File{}, false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// applySetting writes one setting. changed: dosya değişti; applied: değer
// şu an paketinki; pending: dosya henüz yok (ilk açılış oluşturacak); why:
// neden uygulanmadı ("" = uygulandı ya da zaten öyle).
func applySetting(dataDir string, s Setting) (changed, applied, pending bool, why string) {
	path := filepath.Join(dataDir, filepath.FromSlash(s.File))
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, false, true, ""
	}
	if err != nil {
		return false, false, false, "okunamadı: " + err.Error()
	}
	doc := string(raw)
	cur, present, isMap := yamlGet(doc, s.Path)
	if isMap && cur == "" && present {
		return false, false, false, "beklenmeyen yapı (eşlem)"
	}
	if present && cur == s.Value {
		return false, true, false, ""
	}
	if present && !slices.Contains(s.Defaults, cur) {
		// Kullanıcı bilerek değiştirmiş: onun kararı paketinkinden önce gelir.
		return false, false, false, "kullanıcı değiştirmiş (" + cur + ")"
	}
	out, err := yamlSet(doc, s.Path, s.Value)
	if err != nil {
		return false, false, false, err.Error()
	}
	if err := writeAtomic(path, []byte(out)); err != nil {
		return false, false, false, "yazılamadı: " + err.Error()
	}
	return true, true, false, ""
}

// writeAtomic writes via a temp file + rename so the server never reads a
// half-written config.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".mcos-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
