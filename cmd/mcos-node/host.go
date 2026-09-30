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

	"mcos/internal/catalog"
	"mcos/internal/cluster"
	"mcos/internal/java"
	"mcos/internal/linkjar"
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
	// fabricAPI, Fabric API'yi internetten mods/ klasörüne indiren SON ÇARE
	// adımıdır (nil = downloadFabricAPI; dönüş indirilen dosyanın yolu).
	// Testler ağa çıkmasın diye değiştirilebilir.
	fabricAPI func(srv *model.Server, modsDir string) (string, error)

	// coord, mod/eklenti eşitlemesi için (nil: sınamalarda eşitleme yok).
	coord *cluster.LinkCoordinator
	// pair, kodla eşleştirme istekleri ve kararları (pairing.go; nil:
	// sınamalarda yok).
	pair *pairTracker

	installMu sync.Mutex

	mu       sync.Mutex
	lastNote string
	busy     bool
	files    map[string][]model.LinkFile
}

// dataDir is where a server's files live on this machine.
func (h *nodeHost) dataDir(srv *model.Server) string {
	if srv.DataDir != "" {
		return srv.DataDir
	}
	return h.st.Paths.ServerData(srv.ID)
}

// LinkDataDir implements cluster.LinkFileHost.
func (h *nodeHost) LinkDataDir() (string, bool) {
	srv := h.sharedWorldServer()
	if srv == nil {
		return "", false
	}
	return h.dataDir(srv), true
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
		// Kurucu MCOS'tur: bu alan dolu olduğu sürece koordinatör kurulumu
		// geri yaymaz (bkz. cluster.LinkCoordinator.isOrigin).
		OriginID: srv.Link.OriginID,
		// Kurucunun Velocity proxy'si: doluysa bu PC'nin topolojisi de
		// "proxy": true der ve mod oyuncuyu proxy içinden geçirir.
		ProxySecret: nodeProxySecret(srv),
	}.Normalize(), true
}

func nodeProxySecret(srv *model.Server) string {
	if srv.Link.BehindProxy() {
		return srv.Link.Proxy.Secret
	}
	return ""
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
	changed := !created &&
		(srv.Software != model.Software(spec.Software) || srv.MCVersion != spec.MCVersion)
	if changed {
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
	// Kural değiştiyse çalışan sunucu yeniden başlamalı (properties açılışta okunur).
	rulesChanged := !created && spec.Rules != nil && !srv.Link.Rules.Equal(spec.Rules)
	// Tek adres: kurucu (MCOS) Velocity proxy'si çalıştırıyorsa bu PC'nin
	// sunucusu onun arka ucudur — oyuncular bu PC'ye doğrudan değil, proxy
	// üzerinden gelir (DonutSMP gibi; bkz. internal/proxy). Anahtar değişince
	// sunucu yeniden başlamalı: paper-global.yml / FabricProxy-Lite açılışta
	// okunur.
	px := spec.BackendProxy(srv.Software)
	if !created && !srv.Link.Proxy.Equal(px) {
		rulesChanged = true
	}
	srv.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: spec.Difficulty,
		SlabChunks: spec.SlabChunks,
		Seed:       spec.Seed,
		LinkPort:   spec.LinkPort,
		OriginID:   spec.OriginID,
		Rules:      spec.Rules,
		Proxy:      px,
	}
	// Kurucunun oyun kuralları (online-mode, kip, PvP...): uyuşmazlıkta
	// aktarılan oyuncu bu PC'nin sunucusundan atılıyordu (bkz. model.LinkRules).
	spec.Rules.ApplyTo(srv)
	srv.UpdatedAt = time.Now()

	if err := h.st.SaveServer(srv); err != nil {
		return "", 0, err
	}

	verb := "güncellendi"
	if created {
		verb = "oluşturuldu"
	}
	// Mod bu PC'de kurulabilecek mi — kurulumla AYNI seçim (linkjar). Kurulum
	// arka planda koşuyor; paket eskiyse (yalnızca 1.21.11 jar'ı) neden
	// MCOS'a dönen metinde görünsün, sunucu yine kurulur (MCOS kutusuyla aynı
	// davranış, bkz. internal/daemon ApplyLinkSpec).
	modWarn := ""
	if _, err := linkLocator().Find(srv.Software, srv.MCVersion); err != nil {
		modWarn = " — UYARI: " + err.Error()
	}

	// ── Zaten çalışıyorsa DOKUNMA ──────────────────────────────────────
	// MCOS kurulumu eşitleme döngüsüyle yeniden gönderebilir (katılımcı
	// listesi ya da port değişince). Eskiden her gönderim installAndStart'ı
	// yeniden çalıştırıyordu: çalışan sunucuya ikinci kez "başlat" deniyor,
	// "server already running" hatası durum satırına "başlatılamadı" diye
	// düşüyordu. Yalnızca yazılım/sürüm değiştiyse yeniden başlatılır.
	//
	// Mod/eklenti listesi değiştiyse ya da eldeki dünya başka bir tohumla
	// üretilmişse de yeniden başlatılır: ikisi de ancak açılışta etkili olur.
	dir := h.dataDir(srv)
	filesPending := cluster.LinkFilesPending(dir, spec.Files)
	seedChanged := false
	if have, err := cluster.WorldSeed(dir); err == nil && !cluster.SameSeed(have, spec.Seed) {
		seedChanged = true
	}
	running := h.isRunning(srv.ID)
	if running && !changed && !filesPending && !seedChanged && !rulesChanged {
		h.note(fmt.Sprintf("%s çalışıyor (port %d) — kurulum eşitlendi", srv.Name, srv.Port))
		return fmt.Sprintf("%s %s (port %d, çalışıyor)%s", srv.Name, verb, srv.Port, modWarn), srv.Port, nil
	}

	// Kurulum ve başlatma ARKA PLANDA: karşı taraf 60 saniyeden uzun süren
	// bir indirmeyi bekleyemez, zaman aşımına uğrar ve kurulumu başarısız
	// sanar.
	clone := srv.Clone()
	files := append([]model.LinkFile(nil), spec.Files...)
	go func() {
		if running {
			h.note(clone.Name + " yeniden başlatılıyor (kurulum değişti)")
			h.stopAndWait(clone.ID)
		}
		if seedChanged {
			// Düğüm daha önce aynı adla BAŞKA bir tohumdan kurulmuş bir
			// ortak dünyayı yeniden kullanıyor: eski dünya silinmez,
			// kenara alınır (bkz. cluster.RetireMismatchedWorld).
			if moved, err := cluster.RetireMismatchedWorld(dir, clone.LevelSeed); err != nil {
				h.log.Warnf("node: dünya kenara alınamadı: %v", err)
			} else if len(moved) > 0 {
				h.log.Warnf("node: tohum değişti; eski dünya kenara alındı: %s",
					strings.Join(moved, ", "))
			}
		}
		h.pendingFiles(clone.ID, files)
		h.install(clone)
	}()

	h.note(fmt.Sprintf("%s %s (port %d) — kuruluyor%s", srv.Name, verb, srv.Port, modWarn))
	return fmt.Sprintf("%s %s (port %d)%s", srv.Name, verb, srv.Port, modWarn), srv.Port, nil
}

// isRunning reports whether the server is up or coming up.
func (h *nodeHost) isRunning(id string) bool {
	if h.servers == nil {
		return false
	}
	st := h.servers.State(id)
	return st == model.StateRunning || st == model.StateStarting
}

// stopAndWait stops a server and waits (bounded) for it to exit.
func (h *nodeHost) stopAndWait(id string) {
	_ = h.servers.Stop(id)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && h.isRunning(id) {
		time.Sleep(500 * time.Millisecond)
	}
}

// fetchNodeFabricProxy installs FabricProxy-Lite (değişken: sınamalar ağa
// çıkmamalı).
var fetchNodeFabricProxy = func(mc, modsDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return catalog.New().InstallByID(ctx, "fabricproxy-lite", "fabric", mc, modsDir)
}

// ensureNodeProxyMod puts FabricProxy-Lite on a Fabric backend behind the
// MCOS proxy (MCOS kutusundaki daemon.ensureProxyMod'un aynısı). Ayar dosyası
// her açılışta server.WriteProxyBackend ile yazılır.
func ensureNodeProxyMod(srv *model.Server, dir string) error {
	if srv.Software != model.SoftwareFabric || !srv.Link.BehindProxy() {
		return nil
	}
	mods := filepath.Join(dir, "mods")
	if server.HasFabricProxy(mods) {
		return nil
	}
	if err := os.MkdirAll(mods, 0o755); err != nil {
		return err
	}
	if _, err := fetchNodeFabricProxy(srv.MCVersion, mods); err != nil {
		return fmt.Errorf("FabricProxy-Lite (Fabric %s) kurulamadı: %w", srv.MCVersion, err)
	}
	return nil
}

// installAndStart downloads the server, installs the mod and starts it.
func (h *nodeHost) installAndStart(srv *model.Server) {
	// Kurulumlar SIRAYLA: açılışta restorePrevious sunucuyu başlatırken
	// kurucu kurulumu yeniden gönderebilir (düğümün elindeki özet boş
	// olduğu için). İkisi aynı anda koşarsa ikinci Start "server already
	// starting" ile düşer ve durum satırı çalışan bir sunucu için
	// "başlatılamadı" yazar.
	h.installMu.Lock()
	defer h.installMu.Unlock()
	h.setBusy(true)
	defer h.setBusy(false)

	ctx := context.Background()

	h.note(fmt.Sprintf("%s %s indiriliyor…", srv.Software, srv.MCVersion))
	if err := h.servers.EnsureInstalled(ctx, srv); err != nil {
		h.note("kurulum başarısız: " + err.Error())
		h.log.Errorf("node: %q kurulamadı: %v", srv.Name, err)
		return
	}

	// Kurucunun mod/eklentileri BAŞLATMADAN önce: yoksa bu yarı ilk
	// açılışta modsuz üretilir ve modlu bloklar sınırda kaybolur.
	if files, ok := h.takeFiles(srv.ID); ok && h.coord != nil {
		h.note("modlar/eklentiler eşitleniyor…")
		if _, err := h.coord.SyncLinkFiles(ctx, h.dataDir(srv), files); err != nil {
			h.note("UYARI: bazı modlar/eklentiler alınamadı — " + err.Error())
			h.log.Warnf("node: mod/eklenti eşitlemesi eksik (%s): %v", srv.Name, err)
		}
	}

	if err := h.installLinkMod(srv); err != nil {
		// Mod olmadan sunucu ÇALIŞIR ama ortak dünya devri olmaz. Bunu
		// sessizce geçmek, kullanıcının sınırda takılıp kalmasına yol
		// açardı; açıkça söylüyoruz.
		h.note("UYARI: ortak dünya modu kurulamadı — " + err.Error())
		h.log.Warnf("node: mod kurulamadı (%s): %v", srv.Name, err)
	}
	if err := ensureNodeProxyMod(srv, h.dataDir(srv)); err != nil {
		// FabricProxy-Lite olmadan Fabric sunucusu proxy'den gelen oyuncuyu
		// tanımaz: bu yarıya kimse giremez. Açıkça söylüyoruz.
		h.note("UYARI: " + err.Error())
		h.log.Warnf("node: %v", err)
	}

	if h.isRunning(srv.ID) {
		h.note(fmt.Sprintf("%s çalışıyor (port %d)", srv.Name, srv.Port))
		return
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
	linkModName    = "mcos-link.jar"       // yalnızca Fabric
	linkPluginName = "mcos-link-paper.jar" // Paper / Purpur / Spigot
)

// linkArtifact says which file this server needs and where it goes.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// Burada sw.SupportsMods() yazıyordu: Forge, NeoForge ve Quilt'e de Fabric
// modu kopyalanıyordu. Forge Fabric biçimli bir modu TANIMAZ; Quilt ise
// fabric-api yerine ayrı bir "Quilted Fabric API" ister. MCOS kutusu bu
// hatayı çoktan düzeltmişti (internal/daemon/handlers_link.go) ama düğüm
// eski kuralı taşıyordu — aynı kurulum iki makinede farklı davranıyordu.
func linkArtifact(sw model.Software) (name, dir string, ok bool) {
	switch {
	case sw == model.SoftwareFabric:
		return linkModName, "mods", true
	case sw.SupportsPlugins():
		return linkPluginName, "plugins", true
	}
	return "", "", false
}

// linkBaseDir is the program folder the shared-world jars ship in.
//
// Değişken, çünkü sınamada os.Executable sınama ikilisini gösterir; sınama
// sahte bir program klasörü verir. Üretimde her zaman exeDir.
var linkBaseDir = exeDir

// linkLocator says where this machine looks for the link jars.
//
// ── Yakalanan hata ──────────────────────────────────────────────────────────
// Düğüm paketi yalnızca 1.21.11 için derlenmiş mcos-link.jar'ı taşıyordu ve
// düğüm sürüm bile sormadan onu HER Fabric sunucusuna kopyalıyordu: MCOS
// kutusu 26.3 ortak dünyası gönderdiğinde bu PC'deki yarı ya modsuz kalır ya
// da açılışta düşerdi. Artık paket, her sürümün jar'ını ve indeksini
// programın yanındaki mods/link klasöründe taşır (make node-jars) ve seçim
// MCOS kutusundakiyle AYNI kuralla (internal/linkjar) yapılır — iki yarı aynı
// jar'ı seçmeli.
//
// Arama sırası: MCOS_NODE_LINK_DIR (elle verilen klasör), <program>/mods/link,
// <program> (klasör düzleştirilmişse), ./dist/mods/link (depodan go run).
// Eski, indekssiz düzen (programın yanında mcos-link.jar; yalnızca 1.21.11)
// son çare olarak tanınır.
func linkLocator() linkjar.Locator {
	base := linkBaseDir()
	link := filepath.Join(base, "mods", "link")
	dirs := []string{link, base, filepath.Join(".", "dist", "mods", "link")}
	if d := os.Getenv("MCOS_NODE_LINK_DIR"); d != "" {
		dirs = append([]string{d}, dirs...)
	}
	return linkjar.Locator{
		Dirs: dirs,
		LegacyDirs: []string{base, filepath.Join(base, "mods"), ".",
			filepath.Join(".", "mods"), filepath.Join(".", "dist", "mods")},
		// fabric-api paketle mods/link içinde gelir; eski paketlerde
		// programın yanında ya da mods/ altındaydı.
		DepDirs: []string{link, base, filepath.Join(base, "mods")},
	}
}

// installLinkMod copies the shared-world mod into the server.
//
// Fabric mods/ klasörüne, Paper ise plugins/ klasörüne koyar. Hangi jar:
// sunucunun Minecraft sürümüne göre linkjar seçer; hedef ad sabittir
// (mods/mcos-link.jar, plugins/mcos-link-paper.jar) — MCOS kutusuyla aynı.
// Uyumsuz sürümde önceki kurulumun jar'ı KALDIRILIR: yanlış sürümün Fabric
// modu sunucuyu açılışta düşürür (1.21.11 jar'ı 1.21.1'de NoSuchFieldError).
func (h *nodeHost) installLinkMod(srv *model.Server) error {
	name, sub, ok := linkArtifact(srv.Software)
	if !ok {
		if _, err := linkjar.LoaderFor(srv.Software); err != nil {
			return err
		}
		return fmt.Errorf("%s ortak dünyayı desteklemiyor", srv.Software)
	}
	dir := filepath.Join(h.dataDir(srv), sub)
	dst := filepath.Join(dir, name)
	l := linkLocator()
	res, err := l.Find(srv.Software, srv.MCVersion)
	if err != nil {
		if linkjar.KindOf(err) == linkjar.KindVersion {
			_ = os.Remove(dst)
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// ── Fabric: API ÖNCE ────────────────────────────────────────────────
	// mcos-link, fabric-api'yi SERT bağımlılık olarak bildirir. Fabric
	// Loader eksik sert bağımlılıkta modu atlamaz, SUNUCUYU HİÇ AÇMAZ
	// ("requires any version of fabric-api, which is missing!"). Düğüm
	// fabric-api'yi hiç kurmuyordu: MCOS'un buraya kurduğu her Fabric ortak
	// dünyası açılışta çöküyordu. API sağlanamıyorsa mod da kurulmaz (varsa
	// eski kopyası da kaldırılır). Doğru sürümün fabric-api'si ve yanlış
	// sürümdekinin temizliği: linkjar.PrepareFabricAPI.
	if res.Loader == linkjar.Fabric && res.Dep != "" {
		if res.DepPath == "" {
			if p := linkjar.FabricAPIFor(l.DepDirs, srv.MCVersion); p != "" {
				res.DepPath, res.Dep = p, linkjar.CleanCacheName(filepath.Base(p))
			}
		}
		fetch := h.fabricAPI
		if fetch == nil {
			fetch = downloadFabricAPI
		}
		note, err := linkjar.PrepareFabricAPI(dir, res, srv.MCVersion, func() (string, error) {
			return fetch(srv, dir)
		})
		if err != nil {
			_ = os.Remove(dst)
			return fmt.Errorf("%w; ortak dünya modu kurulmadı — onsuz Fabric "+
				"sunucusu HİÇ açılmaz", err)
		}
		if h.log != nil {
			h.log.Infof("node: %s: %s", srv.Name, note)
		}
	}
	return copyFile(res.Jar, dst)
}

// copyLinkJars copies the shipped link folder (index-*.tsv + jars) to
// <base>/mods/link.
//
// Windows'ta program ilk açılışta kendini %LOCALAPPDATA%\MCOS-Node\program
// altına kopyalar; eskiden yalnızca iki sabit adlı jar yanına alınıyordu.
// İndeks ve sürüme özel jar'lar alınmazsa kurulu program hiçbir sürümde
// ortak dünya modu bulamazdı. Dönüş: kopyalanan dosya sayısı.
func copyLinkJars(base string) (int, error) {
	dst := filepath.Join(base, "mods", "link")
	for _, src := range linkLocator().Dirs {
		idx, _ := filepath.Glob(filepath.Join(src, "index-*.tsv"))
		if len(idx) == 0 {
			continue
		}
		if a, b := absPath(src), absPath(dst); a == b {
			return 0, nil // zaten kurulu yerden çalışıyoruz
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return 0, err
		}
		names, err := os.ReadDir(src)
		if err != nil {
			return 0, err
		}
		n := 0
		for _, e := range names {
			nm := e.Name()
			if !e.Type().IsRegular() || !(strings.HasSuffix(nm, ".jar") ||
				(strings.HasPrefix(nm, "index-") && strings.HasSuffix(nm, ".tsv"))) {
				continue
			}
			if err := copyFile(filepath.Join(src, nm), filepath.Join(dst, nm)); err != nil {
				return n, err
			}
			n++
		}
		return n, nil
	}
	return 0, fmt.Errorf("ortak dünya jar'ları bulunamadı: index-*.tsv yok (aranan: %s)",
		strings.Join(linkLocator().Dirs, ", "))
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return filepath.Clean(a)
	}
	return filepath.Clean(p)
}

// downloadFabricAPI fetches fabric-api from Modrinth (son çare: paket
// fabric-api'yi zaten taşıyor, internetsiz kurulum çalışmalı).
func downloadFabricAPI(srv *model.Server, modsDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return catalog.New().InstallByID(ctx, "fabric-api", "fabric", srv.MCVersion, modsDir)
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

// pendingFiles remembers the origin's jar list for the next install.
//
// install() test sahtesiyle değiştirilebildiği için liste parametre olarak
// değil, sunucu kimliğiyle saklanır; restorePrevious (kurucudan liste
// gelmeden açılış) için kayıt yoktur ve eşitleme atlanır — kurucu birkaç
// saniye içinde kurulumu zaten yeniden gönderir.
func (h *nodeHost) pendingFiles(id string, files []model.LinkFile) {
	h.mu.Lock()
	if h.files == nil {
		h.files = map[string][]model.LinkFile{}
	}
	h.files[id] = files
	h.mu.Unlock()
}

func (h *nodeHost) takeFiles(id string) ([]model.LinkFile, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.files[id]
	delete(h.files, id)
	return f, ok
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
