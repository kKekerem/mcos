package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcos/internal/java"
	mlog "mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/portmgr"
	"mcos/internal/server"
	"mcos/internal/store"
)

// nodeHost is this machine's half of a shared world.
//
// ════════════════════════════════════════════════════════════════════════════
// NE İŞE YARAR
// ════════════════════════════════════════════════════════════════════════════
//
// MCOS kutusundaki kullanıcı "ortak dünya aç" dediğinde, MCOS bu makineye bir
// LinkSpec gönderir: hangi sürüm, hangi tohum, hangi zorluk, dünyanın kaç
// dilime bölüneceği. Bu tip o isteği alır ve BU BİLGİSAYARDA gerçek bir
// Minecraft sunucusu kurar ve başlatır.
//
// Yani kullanıcı açısından: programı açmak, makineyi MCOS'a "ikinci PC" olarak
// eklemeye yeter. Başka hiçbir şey yapması gerekmez.
//
// cluster.LinkHost arayüzünü uygular — MCOS kutusundaki mcosd ile TAM AYNI
// arayüz. Protokol de aynı olduğu için MCOS tarafında bu makineyi ayrı bir
// şey olarak tanıyan hiçbir kod yok: sıradan bir eş.
type nodeHost struct {
	st      *store.Store
	servers *server.Manager
	log     *mlog.Logger

	// budget, bu makinede bir sunucuya verilecek en fazla kaynak.
	//
	// NEDEN GEREKLİ: eş 16 GB'lık bir makine olabilir ve 12 GB öneren bir
	// spec gönderebilir. Burası kullanıcının günlük kullandığı bilgisayar;
	// belleğini tamamen yutmak kabul edilemez.
	budgetRAMMB int
	budgetCPU   int

	// installFn, kurulum+baslatma adimidir.
	//
	// NEDEN ALAN: gercek kurulum internetten JDK ve sunucu jar'i indirir.
	// Testlerde o adimi degistirebilmek, ApplyLinkSpec'in KARARLARINI
	// (port secimi, JavaMajor hesabi, alan atamalari) ag olmadan sinamayi
	// mumkun kiliyor. Uretimde her zaman installAndStart'tir.
	installFn func(*model.Server)

	mu       sync.Mutex
	lastNote string
	busy     bool
}

// install runs the install step, honouring a test override.
func (h *nodeHost) install(srv *model.Server) {
	if h.installFn != nil {
		h.installFn(srv)
		return
	}
	h.installAndStart(srv)
}

// LinkSpec reports the shared world running here, if any.
func (h *nodeHost) LinkSpec() (model.LinkSpec, bool) {
	srv := h.sharedWorldServer()
	if srv == nil {
		return model.LinkSpec{}, false
	}
	return model.LinkSpec{
		Mode:       srv.Link.Mode,
		ServerName: srv.Name,
		Software:   string(srv.Software),
		MCVersion:  srv.MCVersion,
		Seed:       srv.LevelSeed,
		Difficulty: srv.Link.Difficulty,
		RAMMB:      srv.RAMMB,
		Port:       srv.Port,
		LinkPort:   srv.Link.LinkPort,
		SlabChunks: srv.Link.SlabChunks,
		Origin:     nodeName(),
	}.Normalize(), true
}

// PlayersOnline reports how many players are on the local half.
func (h *nodeHost) PlayersOnline() int {
	srv := h.sharedWorldServer()
	if srv == nil {
		return 0
	}
	h.servers.FillRuntime(srv)
	return srv.Players
}

func (h *nodeHost) sharedWorldServer() *model.Server {
	list, err := h.st.ListServers()
	if err != nil {
		return nil
	}
	for _, s := range list {
		if s.Link.Mode == model.LinkSharedWorld {
			return s
		}
	}
	return nil
}

// ApplyLinkSpec creates, updates and starts the local half of a shared world.
//
// Dönüş: kullanıcıya gösterilecek metin, GERÇEKTEN kullanılan Minecraft portu,
// hata. Portun dönmesi şart: istenen port bu makinede doluysa başkasını
// seçiyoruz ve karşı taraf oyuncuyu aktarırken o portu bilmek zorunda.
func (h *nodeHost) ApplyLinkSpec(spec model.LinkSpec) (string, int, error) {
	spec = spec.Normalize()

	if spec.Mode != model.LinkSharedWorld {
		// Karşı taraf ortak dünyayı KAPATTI. Sunucuyu silmiyoruz: dünya
		// kullanıcının verisidir ve bir daha açtığında yerinde durmalı.
		srv := h.sharedWorldServer()
		if srv == nil {
			return "ortak dünya zaten kapalı", 0, nil
		}
		_ = h.servers.Stop(srv.ID)
		srv.Link.Mode = model.LinkOff
		srv.UpdatedAt = time.Now()
		if err := h.st.SaveServer(srv); err != nil {
			return "", 0, err
		}
		h.note(srv.Name + ": ortak dünya kapatıldı")
		return srv.Name + ": ortak dünya kapatıldı", srv.Port, nil
	}

	existing, _ := h.st.ListServers()
	var srv *model.Server
	for _, s := range existing {
		if strings.EqualFold(s.Name, spec.ServerName) ||
			s.Link.Mode == model.LinkSharedWorld {
			srv = s
			break
		}
	}

	created := false
	if srv == nil {
		port := spec.Port
		used := portmgr.UsedPorts(existing, "")
		if used[port] {
			free, err := portmgr.FindFree(port, used)
			if err != nil {
				return "", 0, fmt.Errorf("boş port bulunamadı: %w", err)
			}
			port = free
		}
		srv = &model.Server{
			ID:             newID(),
			Name:           spec.ServerName,
			Software:       model.Software(spec.Software),
			MCVersion:      spec.MCVersion,
			Port:           port,
			RestartOnCrash: true,
			Autostart:      true,
			JVMFlags:       "aikar",
			MaxPlayers:     20,
			Gamemode:       "survival",
			OnlineMode:     true,
			PVP:            true,
			CreatedAt:      time.Now(),
		}
		created = true
	}

	// Yazılım ya da sürüm değiştiyse kurulum geçersizdir; yoksa eski jar
	// çalışmaya devam eder ve panel yanlış sürümü gösterir.
	if !created &&
		(srv.Software != model.Software(spec.Software) || srv.MCVersion != spec.MCVersion) {
		h.servers.MarkUninstalled(srv.ID)
	}

	srv.Software = model.Software(spec.Software)
	srv.MCVersion = spec.MCVersion
	srv.JavaMajor = java.RequiredJavaMajor(spec.MCVersion)
	srv.SupportsPlugins = srv.Software.SupportsPlugins()
	srv.SupportsMods = srv.Software.SupportsMods()
	srv.LevelSeed = spec.Seed
	srv.Difficulty = string(spec.Difficulty)
	srv.RAMMB, srv.CPUQuota = h.clamp(spec.RAMMB, srv.CPUQuota)
	srv.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: spec.Difficulty,
		SlabChunks: spec.SlabChunks,
		Seed:       spec.Seed,
		LinkPort:   spec.LinkPort,
	}
	srv.UpdatedAt = time.Now()

	if err := h.st.SaveServer(srv); err != nil {
		return "", 0, err
	}

	verb := "güncellendi"
	if created {
		verb = "oluşturuldu"
	}

	// Kurulum ve başlatma ARKA PLANDA: karşı taraf 60 saniyeden uzun süren
	// bir indirmeyi bekleyemez, zaman aşımına uğrar ve kurulumu başarısız
	// sanar.
	go h.install(srv.Clone())

	h.note(fmt.Sprintf("%s %s (port %d) — kuruluyor", srv.Name, verb, srv.Port))
	return fmt.Sprintf("%s %s (port %d)", srv.Name, verb, srv.Port), srv.Port, nil
}

// installAndStart downloads the server, installs the mod and starts it.
func (h *nodeHost) installAndStart(srv *model.Server) {
	h.setBusy(true)
	defer h.setBusy(false)

	ctx := context.Background()

	h.note(fmt.Sprintf("%s %s indiriliyor…", srv.Software, srv.MCVersion))
	if err := h.servers.EnsureInstalled(ctx, srv); err != nil {
		h.note("kurulum başarısız: " + err.Error())
		h.log.Errorf("node: %q kurulamadı: %v", srv.Name, err)
		return
	}

	if err := h.installLinkMod(srv); err != nil {
		// Mod olmadan sunucu ÇALIŞIR ama ortak dünya devri olmaz. Bunu
		// sessizce geçmek, kullanıcının sınırda takılıp kalmasına yol
		// açardı; açıkça söylüyoruz.
		h.note("UYARI: ortak dünya modu kurulamadı — " + err.Error())
		h.log.Warnf("node: mod kurulamadı (%s): %v", srv.Name, err)
	}

	h.note("sunucu başlatılıyor…")
	if err := h.servers.Start(ctx, srv); err != nil {
		h.note("başlatılamadı: " + err.Error())
		h.log.Errorf("node: %q başlatılamadı: %v", srv.Name, err)
		return
	}
	h.note(fmt.Sprintf("%s çalışıyor (port %d)", srv.Name, srv.Port))
}

// Ortak dunya eklentisinin iki yapisi.
//
// Fabric modlari mods/ altindan, Paper eklentileri plugins/ altindan yuklenir
// ve ikisi birbirinin dosyasini TANIMAZ. MCOS kutusundaki adlandirmanin AYNISI
// kullaniliyor (internal/daemon/handlers_link.go): kullanici ayni klasoru iki
// makineye kopyalayabilmeli.
const (
	linkModName    = "mcos-link.jar"       // Fabric / Quilt / Forge
	linkPluginName = "mcos-link-paper.jar" // Paper / Purpur / Spigot
)

// linkArtifact says which file this server needs and where it goes.
func linkArtifact(sw model.Software) (name, dir string, ok bool) {
	switch {
	case sw.SupportsMods():
		return linkModName, "mods", true
	case sw.SupportsPlugins():
		return linkPluginName, "plugins", true
	}
	return "", "", false
}

// installLinkMod copies the shared-world mod into the server.
//
// Fabric mods/ klasörüne, Paper ise plugins/ klasörüne koyar. İkisi de
// desteklenir, çünkü kullanıcı her iki yazılımı da seçebilmeli.
func (h *nodeHost) installLinkMod(srv *model.Server) error {
	name, sub, ok := linkArtifact(srv.Software)
	if !ok {
		return fmt.Errorf("%s ne mod ne eklenti yükler; ortak dünya için "+
			"Fabric veya Paper gerekir", srv.Software)
	}
	src := findLinkArtifact(name)
	if src == "" {
		return fmt.Errorf("%s bulunamadı (programın yanında olmalı)", name)
	}

	dataDir := srv.DataDir
	if dataDir == "" {
		dataDir = h.st.Paths.ServerData(srv.ID)
	}
	dir := filepath.Join(dataDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return copyFile(src, filepath.Join(dir, name))
}

// findLinkArtifact looks for one of the two jars next to the program.
func findLinkArtifact(name string) string {
	if name == "" {
		return ""
	}
	// Acik yol: kullanici dosyayi baska bir yerde tutuyorsa gosterebilmeli.
	// MCOS_NODE_MOD Fabric modunu, MCOS_NODE_PLUGIN Paper eklentisini
	// gosterir.
	env := "MCOS_NODE_MOD"
	if name == linkPluginName {
		env = "MCOS_NODE_PLUGIN"
	}
	if p := os.Getenv(env); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}

	base := exeDir()
	for _, p := range []string{
		filepath.Join(base, name),
		filepath.Join(base, "mods", name),
		filepath.Join(".", name),
		filepath.Join(".", "mods", name),
		filepath.Join(".", "dist", "mods", name),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return p
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Geçici dosya + yeniden adlandırma: kopyalama yarıda kesilirse yarım
	// bir jar kalmasın (sunucu onu yüklemeye çalışıp çökerdi).
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
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

// clamp keeps a peer's suggestion inside this machine's budget.
func (h *nodeHost) clamp(ramMB, cpu int) (int, int) {
	if ramMB <= 0 || ramMB > h.budgetRAMMB {
		ramMB = h.budgetRAMMB
	}
	if cpu <= 0 || cpu > h.budgetCPU {
		cpu = h.budgetCPU
	}
	return ramMB, cpu
}

func (h *nodeHost) note(s string) {
	h.mu.Lock()
	h.lastNote = s
	h.mu.Unlock()
}

func (h *nodeHost) status() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastNote, h.busy
}

func (h *nodeHost) setBusy(b bool) {
	h.mu.Lock()
	h.busy = b
	h.mu.Unlock()
}

// newID returns a random server id.
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("srv%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
