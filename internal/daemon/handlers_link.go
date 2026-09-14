package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/model"
	"mcos/internal/portmgr"
	"mcos/internal/store"
)

// Bu dosya MCOS LINK'i (ortak dünya) daemon tarafında uygular.
//
// ── Sorumluluk sınırı ───────────────────────────────────────────────────────
// Daemon ŞU İŞLERİ yapar:
//   - hangi sunucunun ortak dünya olduğunu bilmek,
//   - eşlere aynı kurulumu göndermek,
//   - mcos-link modunu sunucunun mods/ klasörüne kurmak,
//   - moda topoloji sunmak (cluster.LinkCoordinator üzerinden).
//
// Daemon ŞUNU YAPMAZ: oyuncu aktarımı. O, Minecraft'ın içinde çalışan
// mcos-link modunun işidir — çünkü yalnızca mod oyuncunun konumunu ve
// envanterini görebilir ve yalnızca mod istemciye transfer paketi
// gönderebilir.

// linkModSearchPaths is where mcos-link.jar may live in the image.
//
// Birden çok yola bakıyoruz çünkü mod üç yoldan gelebilir: imaja gömülü
// (/usr/lib/mcos/mods), çevrimdışı paketten (/data/artifacts) veya
// geliştirme makinesinde derlenmiş (./dist/mods).
var linkModSearchPaths = []string{
	"/usr/lib/mcos/mods",
	"/data/mcos/mods",
	"/data/artifacts",
	"dist/mods",
}

// Ortak dunya eklentisinin iki yapisi vardir.
//
// NEDEN IKI DOSYA: Fabric ile Paper AYRI yukleyici platformlaridir. Fabric
// modlari mods/ altindan, Paper eklentileri plugins/ altindan yuklenir ve
// ikisi birbirinin dosyasini TANIMAZ. Tek bir jar ile ikisini birden
// beslemek mumkun degil.
const (
	linkModName    = "mcos-link.jar"       // Fabric -> mods/
	linkPluginName = "mcos-link-paper.jar" // Paper / Purpur / Spigot -> plugins/
)

// linkArtifact says which file a server needs and where it goes.
//
// ok=false: bu yazilim ortak dunya modunu YUKLEYEMEZ. Cagiran bunu
// kullaniciya soylemeli, sessizce gecmemeli.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// Burada `sw.SupportsMods()` yazıyordu ve o yordam Forge ile NeoForge'u da
// TRUE sayıyor. Oysa mcos-link bir FABRIC modudur: Forge ve NeoForge Fabric
// biçimli bir mod'u hiç tanımaz. Sonuç sinsiydi — jar sunucunun mods/
// klasörüne kopyalanıyor, panel "mod kurulu" diyordu, ama ortak dünya hiç
// çalışmıyordu ve kullanıcı nedenini göremiyordu.
//
// Quilt de listeden ÇIKARILDI. Quilt Loader Fabric modlarını çalıştırabilir
// ama fabric-api'nin kendisini DEĞİL: orada "Quilted Fabric API" (ayrı bir
// proje) gerekir. Doğrulanmamış bir kombinasyonu sessizce desteklemektense
// açıkça reddetmek doğru — kullanıcının isteği zaten "fabric ve paper için
// derle" idi ve ikisi de gerçek sunucularda denendi.
func linkArtifact(sw model.Software) (name, dir string, ok bool) {
	switch {
	case sw == model.SoftwareFabric:
		return linkModName, "mods", true
	case sw.SupportsPlugins():
		return linkPluginName, "plugins", true
	}
	return "", "", false
}

// ── cluster.LinkHost uygulaması ─────────────────────────────────────────────

// LinkSpec returns the local shared-world setup.
//
// Ortak dünya olarak işaretli İLK sunucu kazanır. Birden çok ortak dünya
// desteklenmiyor: iki farklı dünya aynı eş kümesine dağıtılırsa, dilim
// sahipliği iki kez hesaplanır ve oyuncular yanlış sunucuya gider. Panel
// bunu zaten engelliyor; burada da savunma amaçlı tek sunucu seçiyoruz.
func (d *Daemon) LinkSpec() (model.LinkSpec, bool) {
	srv := d.sharedWorldServer()
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
		Origin:     d.Config().Cluster.NodeName,
	}.Normalize(), true
}

// sharedWorldServer returns the server running in shared-world mode, or nil.
func (d *Daemon) sharedWorldServer() *model.Server {
	list, err := d.store.ListServers()
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

// PlayersOnline reports how many players are on the shared world.
func (d *Daemon) PlayersOnline() int {
	srv := d.sharedWorldServer()
	if srv == nil {
		return 0
	}
	d.servers.FillRuntime(srv)
	return srv.Players
}

// ApplyLinkSpec creates or updates the local half of a shared world.
//
// Bu, EŞTEN gelen bir istektir: karşı makinedeki kullanıcı "ortak dünya aç"
// dediğinde bizim makinemizde de aynı sunucu kurulur. Kullanıcı hiçbir şey
// yapmaz — kullanıcının isteği aynen buydu ("sistemi adam akıllı kur").
//
// AYNI ADLI bir sunucu varsa YENİDEN OLUŞTURULMAZ, güncellenir: eş her
// açılışta yeniden gönderirse dünyayı silmek felaket olurdu.
// Donus: panele yazilacak metin, GERCEKTEN kullanilan Minecraft portu, hata.
//
// Portun donmesi SART: spec.Port bizde doluysa baska bir port seciyoruz ve
// karsi makine oyuncuyu transfer ederken o portu bilmek zorunda. Eskiden
// yalnizca metin donuyordu, secilen port hicbir yere ulasmiyordu ve oyuncular
// YANLIS sunucuya aktariliyordu.
func (d *Daemon) ApplyLinkSpec(spec model.LinkSpec) (string, int, error) {
	spec = spec.Normalize()
	if spec.Mode != model.LinkSharedWorld {
		// Eş ortak dünyayı KAPATTI: bizim sunucumuzu silmiyoruz (dünya
		// kullanıcının verisidir), yalnızca kipi düşürüyoruz.
		if srv := d.sharedWorldServer(); srv != nil {
			srv.Link.Mode = model.LinkOff
			srv.UpdatedAt = time.Now()
			if err := d.store.SaveServer(srv); err != nil {
				return "", 0, err
			}
			return srv.Name + ": ortak dünya kapatıldı", srv.Port, nil
		}
		return "ortak dünya zaten kapalı", 0, nil
	}

	existing, _ := d.store.ListServers()
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
			// Bizde o port doluysa BAŞKA bir port seç ve eşe bunu
			// bildir — sunucuyu hiç kurmamaktansa farklı portta kurmak
			// iyidir; transfer paketi zaten portu topolojiden okuyor.
			free, err := portmgr.FindFree(port, used)
			if err != nil {
				return "", 0, fmt.Errorf("boş port bulunamadı: %w", err)
			}
			port = free
		}
		srv = &model.Server{
			ID:        generateID(),
			Name:      spec.ServerName,
			Software:  model.Software(spec.Software),
			MCVersion: spec.MCVersion,
			// JavaMajor HESAPLANMALI. Eskiden hic atanmiyordu ve 0 kaliyordu;
			// sunucu kuruluyor ama baslatilamiyordu, cunku java.BindForServer
			// "java 0" diye bir surum arayip bulamiyordu. Es tarafindan
			// kurulan her ortak dunya sunucusu bu yuzden olu doguyordu.
			JavaMajor:      java.RequiredJavaMajor(spec.MCVersion),
			Port:           port,
			RestartOnCrash: true,
			Autostart:      true,
			JVMFlags:       "aikar",
			MaxPlayers:     20,
			Gamemode:       "survival",
			OnlineMode:     true,
			PVP:            true,
		}
		srv.CreatedAt = time.Now()
		created = true
	}

	// RAM eşin ÖNERİSİDİR; kendi bütçemizi aşamaz. Eş 16 GB'lık bir makine
	// olabilir, biz 4 GB'lık bir mini PC.
	ram, quota := d.clampToBudget(spec.RAMMB, srv.CPUQuota)
	srv.RAMMB = ram
	srv.CPUQuota = quota

	// Yazilim ya da surum DEGISTIYSE kurulum gecersizdir.
	//
	// Eskiden burada yalnizca alanlar degistiriliyordu. EnsureInstalled ise
	// veri klasorundeki .mcos-launch.json dosyasina bakip "zaten kurulu"
	// diyerek hemen donuyordu. Sonuc: panel "Fabric 1.21.1" yaziyor ama
	// makine hala eski paper-1.20.1.jar dosyasini calistiriyordu. Dogru yol
	// (handleServerChangeVersion) bunu MarkUninstalled ile yapiyor.
	if !created &&
		(srv.Software != model.Software(spec.Software) || srv.MCVersion != spec.MCVersion) {
		d.servers.MarkUninstalled(srv.ID)
	}

	srv.MCVersion = spec.MCVersion
	srv.Software = model.Software(spec.Software)
	// Turetilmis alanlar HER ZAMAN yeniden hesaplanir: yazilim degistiginde
	// eklenti/mod destegi de degisir, yalnizca olusturmada hesaplamak
	// guncellenen sunucuda yanlis deger birakirdi.
	srv.JavaMajor = java.RequiredJavaMajor(spec.MCVersion)
	srv.SupportsPlugins = srv.Software.SupportsPlugins()
	srv.SupportsMods = srv.Software.SupportsMods()
	srv.LevelSeed = spec.Seed
	srv.Difficulty = string(spec.Difficulty)
	srv.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: spec.Difficulty,
		SlabChunks: spec.SlabChunks,
		Seed:       spec.Seed,
		LinkPort:   spec.LinkPort,
	}
	srv.UpdatedAt = time.Now()

	if err := d.store.SaveServer(srv); err != nil {
		return "", 0, err
	}

	verb := "güncellendi"
	if created {
		verb = "oluşturuldu"
	}
	d.log.Infof("link: %s sunucusu %s (%s tarafından, tohum %s)",
		srv.Name, verb, spec.Origin, spec.Seed)

	// Kurulum ve mod yüklemesi ARKA PLANDA: eş bizden 60 saniyeden uzun
	// süren bir indirme beklememeli, yoksa zaman aşımına uğrar ve kurulumu
	// başarısız sanır.
	go func(s *model.Server) {
		if err := d.servers.EnsureInstalled(context.Background(), s); err != nil {
			d.log.Errorf("link: %q kurulamadı: %v", s.Name, err)
			return
		}
		if err := d.installLinkMod(s); err != nil {
			d.log.Errorf("link: mod kurulamadı (%s): %v", s.Name, err)
		}
	}(srv.Clone())

	return fmt.Sprintf("%s %s (port %d)", srv.Name, verb, srv.Port), srv.Port, nil
}

// installLinkMod copies mcos-link.jar into the server's mods directory.
//
// Mod OLMADAN ortak dünya çalışmaz: oyuncu sınırı geçtiğinde hiçbir şey
// olmaz ve dünyanın öbür yarısı boş görünür. Bu yüzden eksikliği sessizce
// geçmiyoruz — panel "mod kurulu değil" diye açıkça yazıyor.
func (d *Daemon) installLinkMod(srv *model.Server) error {
	name, sub, ok := linkArtifact(srv.Software)
	if !ok {
		return fmt.Errorf("%s ne mod ne eklenti yükler; ortak dünya için "+
			"Fabric veya Paper gerekir", srv.Software)
	}
	src := findLinkArtifact(name)
	if src == "" {
		return fmt.Errorf("%s bulunamadı (make mod ile derlenir)", name)
	}
	dataDir := srv.DataDir
	if dataDir == "" {
		dataDir = d.store.Paths.ServerData(srv.ID)
	}
	dir := filepath.Join(dataDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// ── Fabric: API ÖNCE, mod SONRA ─────────────────────────────────────
	//
	// Bu sıra zorunlu ve sebebi ölçüldü. mcos-link'in fabric.mod.json'ı
	// fabric-api'yi SERT BAĞIMLILIK olarak bildiriyor. Fabric Loader eksik
	// bir sert bağımlılıkta modu atlamaz — SUNUCUYU HİÇ AÇMAZ:
	//
	//   Incompatible mods found!
	//   - Mod 'MCOS Link' (mcos-link) 1.0.1 requires any version of
	//     fabric-api, which is missing!
	//   net.fabricmc.loader.impl.FormattedException: Some of your mods are
	//   incompatible with the game or each other!
	//
	// (Gerçek bir Fabric 1.21.11 sunucusunda görüldü.) Bu, ensureLinkArtifact
	// modu VARSAYILAN OLARAK her sunucuya kurduğu için çok ağır bir sonuç
	// doğuruyordu: MCOS'un kurduğu her Fabric sunucusu açılmayı reddederdi.
	//
	// Bu yüzden API sağlanamıyorsa mod da KURULMAZ ve hata döndürülür.
	// Ortak dünyasız çalışan bir sunucu, hiç açılmayan bir sunucudan iyidir.
	if sub == "mods" {
		if err := d.ensureFabricAPI(srv, dir); err != nil {
			return err
		}
	}

	return copyFile(src, filepath.Join(dir, name))
}

// fabricAPIGlobs are the file-name shapes Fabric API ships under.
//
// Modrinth "fabric-api-0.141.6+1.21.11.jar" verir; çevrimdışı paket aynı adı
// korur (bkz. offline-manifest.txt). Yine de tek bir ada bağlanmıyoruz:
// önbellek dosya adının başına URL özeti ekliyor
// (providers.cachePath -> "<16 hane>-fabric-api-….jar").
var fabricAPIGlobs = []string{
	"fabric-api-*.jar",
	"*-fabric-api-*.jar",
	"fabric_api*.jar",
}

// fabricAPIPresent reports whether mods/ already carries Fabric API.
func fabricAPIPresent(modsDir string) bool {
	for _, g := range fabricAPIGlobs {
		if m, _ := filepath.Glob(filepath.Join(modsDir, g)); len(m) > 0 {
			return true
		}
	}
	return false
}

// findBundledFabricAPI looks for Fabric API in MCOS's local artifact stores.
//
// ÖNCE YEREL: MCOS internetsiz çalışabilmeli. Çevrimdışı paket zaten
// fabric-api'yi içeriyor (offline-manifest.txt) ve mcos-install onu
// /data/artifacts'a tohumluyor; ağa çıkmak son çaredir.
func findBundledFabricAPI() string {
	dirs := append([]string{}, linkModSearchPaths...)
	dirs = append(dirs, "/usr/lib/mcos/offline", "dist/offline")
	for _, dir := range dirs {
		for _, g := range fabricAPIGlobs {
			matches, _ := filepath.Glob(filepath.Join(dir, g))
			for _, m := range matches {
				if st, err := os.Stat(m); err == nil && st.Size() > 0 {
					return m
				}
			}
		}
	}
	return ""
}

// ensureFabricAPI guarantees mods/ has Fabric API before mcos-link lands.
//
// Sıra: zaten var mı → yerel depolarda var mı → Modrinth'ten indir. Üçü de
// olmazsa HATA döner ve çağıran mod'u kurmaz (gerekçe installLinkMod'da).
func (d *Daemon) ensureFabricAPI(srv *model.Server, modsDir string) error {
	if fabricAPIPresent(modsDir) {
		return nil
	}

	if src := findBundledFabricAPI(); src != "" {
		dst := filepath.Join(modsDir, "fabric-api.jar")
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("fabric-api kopyalanamadı: %w", err)
		}
		d.log.Infof("link: fabric-api yerel depodan kuruldu (%s)", src)
		return nil
	}

	// Son çare: internet. Zaman aşımı KISA tutuldu — sunucu kurulumu bu
	// çağrıda süresiz asılı kalmamalı.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	path, err := d.catalog.InstallByID(ctx, "fabric-api", "fabric",
		srv.MCVersion, modsDir)
	if err != nil {
		return fmt.Errorf("fabric-api bulunamadı ve indirilemedi (%w); "+
			"ortak dünya modu kurulmadı — modu fabric-api olmadan koymak "+
			"sunucunun HİÇ açılmamasına yol açardı", err)
	}
	d.log.Infof("link: fabric-api indirildi -> %s", path)
	return nil
}

// ensureLinkArtifact installs the shared-world plugin on ANY server.
//
// -- Kullanicinin istegi -----------------------------------------------------
// "mod varsayilan olarak her sunucuya gelmeli". Yani ortak dunya sonradan
// acildiginda sunucuyu yeniden kurmak gerekmesin; dosya zaten orada olsun.
//
// SESSIZCE BASARISIZ OLUR: mod olmadan da sunucu calisir, yalnizca ortak
// dunya devri olmaz. Siradan bir sunucu kurulumunu bu yuzden engellemek
// yanlis olurdu. Durum panelde ayrica gosteriliyor (bkz. handleLinkStatus).
func (d *Daemon) ensureLinkArtifact(srv *model.Server) {
	name, _, ok := linkArtifact(srv.Software)
	if !ok {
		return // vanilla: yuklenecek bir yer yok
	}
	if err := d.installLinkMod(srv); err != nil {
		d.log.Infof("link: %q sunucusuna %s kurulmadi: %v", srv.Name, name, err)
	}
}

// linkModInstalled reports whether the mod is present for a server.
func (d *Daemon) linkModInstalled(srv *model.Server) bool {
	dataDir := srv.DataDir
	if dataDir == "" {
		dataDir = d.store.Paths.ServerData(srv.ID)
	}
	name, sub, ok := linkArtifact(srv.Software)
	if !ok {
		return false
	}
	st, err := os.Stat(filepath.Join(dataDir, sub, name))
	return err == nil && st.Size() > 0
}

// findLinkMod locates the mod jar in the image.
func findLinkMod() string { return findLinkArtifact(linkModName) }

// findLinkArtifact locates one of the two jars in the image.
func findLinkArtifact(name string) string {
	if name == "" {
		return ""
	}
	for _, dir := range linkModSearchPaths {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	return ""
}

// copyFile copies src to dst atomically (temp + rename).
//
// Doğrudan yazmak, yarım kopyalanmış bir jar bırakabilir; Minecraft onu
// yüklemeye çalışıp açılışta çöker ve hatanın nedeni hiç anlaşılmaz.
func copyFile(src, dst string) error {
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

// ── RPC: link.status ────────────────────────────────────────────────────────

func (d *Daemon) handleLinkStatus(_ context.Context, _ json.RawMessage) (any, error) {
	st := model.LinkStatus{Mode: model.LinkOff}

	srv := d.sharedWorldServer()
	if srv != nil {
		st.Mode = srv.Link.Mode
		st.ServerID = srv.ID
		st.ServerName = srv.Name
		st.Difficulty = srv.Link.Difficulty
		st.SlabChunks = srv.Link.SlabChunks
		st.ModInstalled = d.linkModInstalled(srv)
	}

	coord := d.cluster.LinkCoord()
	if coord == nil {
		st.Note = "ortak dünya koordinatörü çalışmıyor"
		return st, nil
	}

	top := coord.Topology()
	st.Nodes = top.Nodes
	st.Territories = top.Areas
	st.Handoffs = coord.Handoffs()
	if top.Note != "" {
		st.Note = top.Note
	}
	if srv != nil && !st.ModInstalled {
		st.Note = linkModName + " sunucuya kurulmadı — ortak dünya çalışmaz"
	}
	return st, nil
}

// ── RPC: link.enable ────────────────────────────────────────────────────────

// linkEnableParams turns one server into a shared world.
type linkEnableParams struct {
	ServerID   string `json:"serverId"`
	Difficulty string `json:"difficulty,omitempty"`
	SlabChunks int    `json:"slabChunks,omitempty"`
	Seed       string `json:"seed,omitempty"`
}

func (d *Daemon) handleLinkEnable(_ context.Context, raw json.RawMessage) (any, error) {
	var p linkEnableParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	srv, err := d.store.GetServer(strings.TrimSpace(p.ServerID))
	if err != nil {
		if err == store.ErrNotFound {
			return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "sunucu bulunamadı"}
		}
		return nil, err
	}
	if !srv.Software.SupportsMods() {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "ortak dünya yalnızca mod yükleyen sürümlerde çalışır (Fabric önerilir)"}
	}

	// En az iki eşleşmiş cihaz gerekir — yoksa "ortak" bir dünya yok.
	paired := 0
	for _, peer := range d.cluster.Peers() {
		if peer.Paired {
			paired++
		}
	}
	if paired == 0 {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "önce en az bir PC eşleştirin (MCOS Paylaşım ekranı)"}
	}

	diff := model.LinkDifficulty(strings.TrimSpace(p.Difficulty))
	if diff == "" {
		diff = model.DifficultyNormal
	}
	seed := strings.TrimSpace(p.Seed)
	if seed == "" {
		// Tohum YOKSA üret: Minecraft'ın rastgele tohumu her düğümde
		// FARKLI olurdu ve iki ayrı dünya oluşurdu. Bu, ortak dünyada
		// yapılabilecek en sessiz ve en yıkıcı hatadır.
		seed = strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	srv.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: diff,
		SlabChunks: p.SlabChunks,
		Seed:       seed,
		LinkPort:   model.DefaultLinkPort,
	}
	srv.LevelSeed = seed
	srv.Difficulty = string(diff)
	srv.UpdatedAt = time.Now()
	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}

	if err := d.installLinkMod(srv); err != nil {
		d.log.Warnf("link: mod kurulamadı: %v", err)
	}

	// Eşlere gönder. Hatalar TOPLANIR ve kullanıcıya döner: bir eşin
	// ulaşılamaması diğerlerini engellememeli.
	spec, _ := d.LinkSpec()
	var problems []string
	if coord := d.cluster.LinkCoord(); coord != nil {
		for _, e := range coord.PushSpec(spec) {
			problems = append(problems, e.Error())
		}
	}

	msg := fmt.Sprintf("%s ortak dünya oldu (%d cihaz, zorluk %s)",
		srv.Name, paired+1, model.DifficultyLabel(diff))
	if len(problems) > 0 {
		msg += " — ulaşılamayan: " + strings.Join(problems, ", ")
	}
	d.log.Infof("link: %s", msg)
	return map[string]any{"message": msg, "seed": seed}, nil
}

// ── RPC: link.disable ───────────────────────────────────────────────────────

func (d *Daemon) handleLinkDisable(_ context.Context, _ json.RawMessage) (any, error) {
	srv := d.sharedWorldServer()
	if srv == nil {
		return map[string]any{"message": "ortak dünya zaten kapalı"}, nil
	}
	srv.Link.Mode = model.LinkOff
	srv.UpdatedAt = time.Now()
	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}
	// Eşlere de kapat: aksi halde onlar oyuncuyu bize göndermeye devam eder
	// ve oyuncular kapalı bir dilime düşer.
	if coord := d.cluster.LinkCoord(); coord != nil {
		spec, _ := d.LinkSpec()
		spec.Mode = model.LinkOff
		spec.ServerName = srv.Name
		coord.PushSpec(spec)
	}
	d.log.Infof("link: %s ortak dünya kapatıldı", srv.Name)
	return map[string]any{"message": srv.Name + ": ortak dünya kapatıldı"}, nil
}

// ── RPC: link.events ────────────────────────────────────────────────────────

func (d *Daemon) handleLinkEvents(_ context.Context, _ json.RawMessage) (any, error) {
	coord := d.cluster.LinkCoord()
	if coord == nil {
		return []string{}, nil
	}
	return coord.Events(), nil
}

// ── RPC: cluster.scan ───────────────────────────────────────────────────────

// handleClusterScan actively probes the LAN for other MCOS machines.
//
// Pasif keşif (multicast) çoğu ev modeminde engellidir; bu yüzden panelin
// "tara" düğmesi bunu çağırır. Tarama birkaç saniye sürer ve bu süre
// boyunca panel radar animasyonunu gösterir.
func (d *Daemon) handleClusterScan(ctx context.Context, _ json.RawMessage) (any, error) {
	if !d.Config().Cluster.Enabled {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "PC paylaşımı kapalı — Ayarlar'dan açın"}
	}
	// Üst sınır: taramanın paneli süresiz bekletmesi kabul edilemez.
	sctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	peers, err := d.cluster.ScanLAN(sctx, nil)
	if err != nil {
		return nil, err
	}
	d.log.Infof("cluster: etkin tarama bitti, %d cihaz bulundu", len(peers))
	return map[string]any{"peers": peers}, nil
}

// ── RPC: cluster.pairManual ─────────────────────────────────────────────────

type pairManualParams struct {
	Address string `json:"address"`
}

func (d *Daemon) handleClusterPairManual(ctx context.Context, raw json.RawMessage) (any, error) {
	var p pairManualParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	addr := strings.TrimSpace(p.Address)
	if addr == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "adres gerekli"}
	}
	if !d.Config().Cluster.Enabled {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "PC paylaşımı kapalı — Ayarlar'dan açın"}
	}

	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	peer, err := d.cluster.AddManual(pctx, addr)
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	}
	return map[string]any{
		"peer":    peer,
		"message": peer.Name + " eşleştirildi (" + peer.IP + ")",
	}, nil
}

// ── RPC: cluster.secret ─────────────────────────────────────────────────────

// handleClusterSecret returns the pairing key so the user can copy it to the
// other machine.
//
// İKİ MAKİNEDE AYNI olmak zorundadır: anahtar eşleşmezse görevler ve ortak
// dünya kurulumu reddedilir (bkz. authorizeTask). Panel bunu ekranda
// gösterebilmeli, yoksa kullanıcı config.json'u elle açmak zorunda kalır.
func (d *Daemon) handleClusterSecret(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{
		"secret":   d.cluster.Secret(),
		"nodeName": d.Config().Cluster.NodeName,
		"port":     d.Config().Cluster.Port,
		"address":  clusterAddress(d.Config().Cluster.Port),
	}, nil
}

// clusterAddress formats this machine's pairing address for display.
func clusterAddress(port int) string {
	ip := cluster.PrimaryIPv4()
	if ip == "" {
		return ""
	}
	return ip + ":" + strconv.Itoa(port)
}
