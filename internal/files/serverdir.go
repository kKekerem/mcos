package files

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"mcos/internal/mcver"
)

// ════════════════════════════════════════════════════════════════════════════
// SUNUCU KLASÖRÜ TANIMA (USB'den sunucu aktarma)
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "Sunucu klasörü aktarma ekle: flaşı seçip bir klasör seçip onu
// aktarabilelim, içindeki sunucu, dünya, her şey, modlar." Başka bir
// bilgisayarda (Windows'ta, bir barındırıcıda) çalışmış bir sunucu klasörü
// USB'ye kopyalanıp MCOS'a getirilir. Burada klasörün NE olduğu anlaşılır:
// hangi yazılım (Paper, Fabric, Forge...), hangi Minecraft sürümü, dünya var
// mı, kaç mod/eklenti; ve server.properties'teki ayarlar (port, oyuncu sınırı,
// zorluk) — aktarılan sunucu AYNI ayarlarla açılsın.

// ServerDirInfo describes a folder that looks like a Minecraft server.
type ServerDirInfo struct {
	Software  string            // "paper", "fabric", ... (boş: tanınmadı)
	MCVersion string            // "1.21.1", "26.3" (boş: bulunamadı)
	HasWorld  bool              // dünya klasörü (level.dat) var
	World     string            // dünya klasörünün adı
	Mods      int               // mods/*.jar
	Plugins   int               // plugins/*.jar
	Props     map[string]string // server.properties
	Score     int               // ne kadar "sunucu gibi" (0 = değil)
}

// reHistory, Paper'ın version_history.json'undaki "(MC: 26.3)" kısmını bulur.
// Sürümün kendisini mcver.Find okur.
//
// ── Yakalanan gerçek hata: 26.x klasörleri tanınmıyordu ─────────────────────
// Buradaki iki ifade yalnızca "1\.\d+" arıyordu. Paper 26.3 klasörünün jar'ı
// "paper-26.3-49.jar", sürüm geçmişi "26.3-49-0fdc088 (MC: 26.3)" (gerçek bir
// Paper 26.3 sunucusunun yazdığı dosyadan). İkisi de eşleşmiyor, dünya yoksa
// aktarma "Minecraft sürümü anlaşılamadı" diye DÜŞÜYORDU. Artık sürüm
// mcver.Find ile okunuyor (1.x ve 26–39 takvim yılları).
var reHistory = regexp.MustCompile(`MC:\s*([^)]*)`)

// DetectServerDir inspects dir (yalnızca okur).
func DetectServerDir(dir string) ServerDirInfo {
	var info ServerDirInfo
	info.Props = readProps(filepath.Join(dir, "server.properties"))
	if len(info.Props) > 0 {
		info.Score += 3
	}
	if fileExists(filepath.Join(dir, "eula.txt")) {
		info.Score++
	}
	world := info.Props["level-name"]
	if world == "" {
		world = "world"
	}
	if fileExists(filepath.Join(dir, world, "level.dat")) {
		info.HasWorld, info.World = true, world
		info.Score += 3
	}
	info.Mods = countJars(filepath.Join(dir, "mods"))
	info.Plugins = countJars(filepath.Join(dir, "plugins"))

	jars, _ := filepath.Glob(filepath.Join(dir, "*.jar"))
	var names []string
	for _, j := range jars {
		names = append(names, strings.ToLower(filepath.Base(j)))
	}
	info.Software, info.MCVersion = softwareFromJars(names)
	if info.Software != "" {
		info.Score += 2
	}

	// Forge / NeoForge (1.17+) jar değil, libraries altındaki argüman
	// dosyasıyla başlar; sürüm klasör adındadır. Forge: "1.20.1-47.2.0",
	// "26.3-66.0.4". NeoForge kendi numarasını taşır: "21.1.77" (1.21.1),
	// "26.3.0.23-beta" (26.3) — eskiden NeoForge için sürüm hiç okunmuyordu.
	for _, c := range []struct{ sw, path string }{
		{"neoforge", "libraries/net/neoforged/neoforge"},
		{"forge", "libraries/net/minecraftforge/forge"},
	} {
		if ents, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(c.path))); err == nil && len(ents) > 0 {
			info.Software = c.sw
			// Birden çok kurulum kalmış olabilir: EN YENİ Minecraft sürümü
			// seçilir. Eskiden ReadDir'in metin sırasındaki SON klasör
			// alınıyordu; "1.21.9-58.0.0" metin olarak "1.21.11-61.2.0"den
			// sonra gelir, yani eski kurulum seçilirdi.
			for _, e := range ents {
				v := mcver.Find(e.Name())
				if c.sw == "neoforge" {
					v = mcver.FromNeoForge(e.Name())
				}
				if v != "" && (info.MCVersion == "" || mcver.Newer(v, info.MCVersion)) {
					info.MCVersion = v
				}
			}
			info.Score += 2
			break
		}
	}
	// ── Yakalanan gerçek hata: "server.jar" adlı Paper/Purpur/Fabric ────────
	// Jar'ı "server.jar" olan bir klasör "vanilla" sayılıyordu. MCOS'un
	// kendisi (ve çoğu barındırıcı paneli) jar'ı hep bu adla kaydediyor:
	// MCOS'ta kurulup gerçekten açılan Paper, Purpur ve Fabric 26.3
	// klasörlerinin ÜÇÜ DE "vanilla" algılandı; aktarılan sunucu eklentisiz /
	// modsuz vanilla olarak kurulurdu. Oysa her yazılım kendi izini bırakıyor
	// (aynı klasörlerde görüldü): Purpur purpur.yml, Paper .paper/ ve
	// bukkit.yml, Fabric başlatıcısı .fabric/. ".fabric" denetimi de vardı ama
	// fileExists ile yapılıyordu — .fabric bir KLASÖR, denetim hiç tutmuyordu.
	if info.Software == "" || info.Software == "vanilla" {
		if sw := softwareFromMarkers(dir); sw != "" {
			info.Software = sw
		}
	}
	if info.Software == "" {
		switch {
		case info.Plugins > 0:
			info.Software = "paper" // eklentiler Bukkit tabanlı bir sunucu ister
		case info.Mods > 0:
			info.Software = "fabric"
		case info.Score > 0:
			info.Software = "vanilla"
		}
	}

	// Sürüm: jar adında yoksa dünyanın son açıldığı sürüm (level.dat),
	// sonra Paper'ın sürüm geçmişi, en son kurulu sunucu dosyaları.
	//
	// Dünya jar'dan YENİ bir tam sürümde kaydedilmişse dünyanınki alınır:
	// dünyayı daha eski sürümle açmak onu bozar (bkz. worldVersion). Klasörde
	// kalmış "paper-1.21.11-132.jar" ile 26.3'te kaydedilmiş bir dünya, 26.3
	// sunucusu olarak aktarılmalı. Ön sürüm/anlık görüntü adı ("26.4-snapshot-1")
	// jar'daki tam sürümün önüne geçmez: onun için sunucu yazılımı yok.
	if info.HasWorld {
		wv := worldVersion(filepath.Join(dir, info.World, "level.dat"))
		if info.MCVersion == "" || (mcver.IsRelease(wv) && mcver.Newer(wv, info.MCVersion)) {
			info.MCVersion = wv
		}
	}
	if info.MCVersion == "" {
		info.MCVersion = historyVersion(dir)
	}
	if info.MCVersion == "" {
		info.MCVersion = bundleVersion(dir)
	}
	return info
}

// softwareFromMarkers recognises Paper/Purpur/Fabric by the files they write.
//
// Sıra önemli: Purpur bir Paper çatalıdır ve .paper/ ile bukkit.yml'yi de
// yazar; önce purpur.yml aranır. Yalnızca bukkit.yml/spigot.yml olan bir
// Spigot klasörü "paper" sayılır: Paper, Spigot eklentilerini çalıştırır ve
// dakikalar süren BuildTools derlemesi gerektirmez.
func softwareFromMarkers(dir string) string {
	switch {
	case fileExists(filepath.Join(dir, "purpur.yml")):
		return "purpur"
	case dirExists(filepath.Join(dir, ".paper")), fileExists(filepath.Join(dir, "version_history.json")),
		fileExists(filepath.Join(dir, "bukkit.yml")), fileExists(filepath.Join(dir, "spigot.yml")):
		return "paper"
	case dirExists(filepath.Join(dir, ".fabric")):
		return "fabric"
	}
	return ""
}

// historyVersion reads Paper's version history ("… (MC: 26.3)").
//
// ── Yakalanan gerçek hata: 26.x'te dosyanın YERİ değişti ──────────────────
// Yalnızca kökteki version_history.json okunuyordu. Gerçek Paper 26.3 ve
// Purpur 26.3 sunucuları dosyayı .paper/version_history.json'a yazdı
// (içerik: {"currentVersion":"26.3-49-0fdc088 (MC: 26.3)"}); kökte yoktu.
// Eski Paper klasörleri için kök de okunmaya devam eder.
func historyVersion(dir string) string {
	for _, p := range []string{".paper/version_history.json", "version_history.json"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		var vh struct {
			Current string `json:"currentVersion"`
		}
		if json.Unmarshal(b, &vh) != nil {
			continue
		}
		if m := reHistory.FindStringSubmatch(vh.Current); m != nil {
			if v := mcver.Find(m[1]); v != "" {
				return v
			}
		}
	}
	return ""
}

// bundleVersion reads the version from unpacked server files: the 1.18+
// bundler's versions/<mc>/ (vanilla, Paper, Purpur) and the Fabric
// launcher's .fabric/server/<mc>-server.jar. Newest wins.
//
// Dünya da sürüm geçmişi de yoksa (kullanıcı world/ klasörünü silmiş,
// jar'ın adı "server.jar") sürüm bu dosyalarda duruyor; gerçek 26.3
// klasörlerinde versions/26.3/ ve .fabric/server/26.3-server.jar görüldü.
func bundleVersion(dir string) string {
	best := ""
	consider := func(name string) {
		if v := mcver.Find(name); v != "" && (best == "" || mcver.Newer(v, best)) {
			best = v
		}
	}
	if ents, err := os.ReadDir(filepath.Join(dir, "versions")); err == nil {
		for _, e := range ents {
			if e.IsDir() && mcver.IsRelease(e.Name()) {
				consider(e.Name())
			}
		}
	}
	if ents, err := os.ReadDir(filepath.Join(dir, ".fabric", "server")); err == nil {
		for _, e := range ents {
			if n, ok := strings.CutSuffix(e.Name(), "-server.jar"); ok && mcver.IsRelease(n) {
				consider(n)
			}
		}
	}
	return best
}

// softwareFromJars recognises a server flavour from top-level jar names.
//
// ── Yakalanan gerçek hata: yükseltilmiş klasörde ESKİ jar seçiliyordu ──────
// Aynı yazılımın birden çok jar'ı olabilir: kullanıcı 1.21.11'den 26.3'e
// geçerken yeni jar'ı bırakıp eskisini silmemiştir. İlk eşleşen ad
// alınıyordu ve Glob metin sırası verir: "paper-1.21.11-132.jar",
// "paper-26.3-49.jar"dan ÖNCE gelir. Klasör 1.21.11 diye aktarılıyor, 26.3'te
// kaydedilmiş dünya eski sürümle açılmaya çalışılıyordu. Artık aynı yazılımın
// jar'ları arasından EN YENİ sürüm seçilir (Forge kütüphane klasörlerindeki
// gibi).
func softwareFromJars(names []string) (sw, ver string) {
	order := []struct{ prefix, sw string }{
		{"fabric-server-launch", "fabric"}, {"fabric-server", "fabric"},
		{"quilt-server-launch", "quilt"},
		{"purpur", "purpur"}, {"folia", "folia"}, {"paper", "paper"},
		{"spigot", "spigot"}, {"craftbukkit", "craftbukkit"},
		{"neoforge", "neoforge"}, {"forge", "forge"},
		{"minecraft_server", "vanilla"}, {"server", "vanilla"},
	}
	for _, o := range order {
		found := false
		for _, n := range names {
			if !strings.HasPrefix(n, o.prefix) {
				continue
			}
			found = true
			if v := mcver.Find(strings.TrimSuffix(n, ".jar")); v != "" && (ver == "" || mcver.Newer(v, ver)) {
				ver = v
			}
		}
		if found {
			return o.sw, ver
		}
	}
	return "", ""
}

func readProps(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func countJars(dir string) int {
	m, _ := filepath.Glob(filepath.Join(dir, "*.jar"))
	return len(m)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// PropInt reads an integer property (0 = yok ya da geçersiz).
func (i ServerDirInfo) PropInt(k string) int {
	n, _ := strconv.Atoi(i.Props[k])
	return n
}

// PropBool reads a boolean property; def yoksa döner.
func (i ServerDirInfo) PropBool(k string, def bool) bool {
	switch strings.ToLower(i.Props[k]) {
	case "true":
		return true
	case "false":
		return false
	}
	return def
}
