package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/linkjar"
	"mcos/internal/model"
	"mcos/internal/portmgr"
	"mcos/internal/server"
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

// linkJarDirs are where the per-version link jars and index-*.tsv live.
//
// Yakalanan hata (kullanıcı, gerçek PC): "PC eşleştirmede ortak dünyayı
// açınca 'mcos link kurulu değil' diyor." Mod yalnızca 1.21.11 için vardı;
// çevrimiçi kurulan sunucu 26.3 alıyordu. Artık her sürümün jar'ı ve hangi
// sürüme hangisinin gittiğini söyleyen indeks bu klasörlerde (seçim kuralı
// internal/linkjar'da; mcos-node da aynısını kullanıyor):
//   - /usr/lib/mcos/mods/link: imaja gömülü (post-build; küçük, RAM'e girer),
//   - /data/mcos/mods/link: kalıcı bölüm (imajda olmayan bir sürüm sonradan
//     eklenebilsin),
//   - dist/mods/link: geliştirme makinesi (make mod).
//
// Değişken, çünkü sınamalar sahte klasör verir.
var linkJarDirs = []string{
	"/usr/lib/mcos/mods/link",
	"/data/mcos/mods/link",
	"dist/mods/link",
}

// linkModSearchPaths is the OLD, index-less layout (mcos-link.jar,
// mcos-link-paper.jar) and also where fabric-api may already sit.
//
// Eski jar'lar YALNIZCA 1.21.11 içindir (bkz. linkjar.LegacyMC); indeksli
// düzen bir sürümü kapsamıyorsa son çare olarak burada aranırlar. Birden
// çok yol: imaja gömülü, kalıcı bölüm, çevrimdışı paket (/data/artifacts)
// ve geliştirme makinesi (./dist/mods).
var linkModSearchPaths = []string{
	"/usr/lib/mcos/mods",
	"/data/mcos/mods",
	"/data/artifacts",
	"dist/mods",
}

// fabricAPIDirs are the local stores searched for fabric-api.
//
// fabric-api kök dosya sistemine GİRMEZ (her sürüm ~2 MB; hepsi RAM'den
// ~40 MB yerdi): post-build onu çevrimdışı paketle ISO'ya koyar, mcos-install
// AYNI adla /data/artifacts'a kopyalar. Önce yerel — MCOS internetsiz
// çalışabilmeli; Modrinth son çare (fetchFabricAPI).
func fabricAPIDirs() []string {
	dirs := append([]string{}, linkModSearchPaths...)
	return append(dirs, "/usr/lib/mcos/offline", "dist/offline")
}

// linkLocator is the jar selection used by every install path here.
func linkLocator() linkjar.Locator {
	return linkjar.Locator{
		Dirs:       linkJarDirs,
		LegacyDirs: linkModSearchPaths,
		DepDirs:    fabricAPIDirs(),
	}
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
//
// Aynı eşleme internal/linkjar.LoaderFor'da da var (mcos-node onu kullanıyor);
// TestLinkArtifactMatchesLinkjar ikisinin ayrışmadığını denetler. Burada
// yalnızca HEDEF dosya adı ve klasör seçilir: hangi jar'ın (hangi Minecraft
// sürümü için) kopyalanacağı linkjar'ın işi.
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
	var files []model.LinkFile
	var inst []model.LinkInstance
	if srv.Link.OriginID == "" {
		// Kurucuyuz: eşlerin kuracağı mod/eklenti listesi bizim sunucumuzdan
		// çıkar (bkz. cluster/linkfiles.go). Eşten gelmiş bir kopyada liste
		// YOK: kurulumu yalnızca kurucu yayar.
		files = cluster.ScanLinkFiles(d.serverDataDir(srv))
		// Aynı makinedeki kardeş kopyalar da düğümdür (bkz. instances.go).
		// Yalnızca kurucuda: eşin listesi kurucudan gelir.
		list, _ := d.store.ListServers()
		inst = d.localInstances(srv, list)
	}
	return model.LinkSpec{
		Instances:  inst,
		PublicAddr: srv.WAN.Hostname,
		Files:      files,
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
		// Eşten gelmiş bir kopya kurucusunu korur: koordinatör bu alana
		// bakarak kurulumu yalnızca kurucunun yaymasını sağlar.
		OriginID: srv.Link.OriginID,
		Rules:    linkRulesFor(d.serverDataDir(srv), srv),
	}.Normalize(), true
}

// linkRulesFor: kurucu kurallarını kendi server.properties'inden okur; eş
// kopyası kurucudan aldığını aynen iletir (bkz. model.LinkRules).
func linkRulesFor(dataDir string, srv *model.Server) *model.LinkRules {
	if srv.Link.OriginID != "" {
		return srv.Link.Rules
	}
	return server.ReadLinkRules(dataDir, srv)
}

// serverDataDir is where a server's files live.
func (d *Daemon) serverDataDir(srv *model.Server) string {
	if srv.DataDir != "" {
		return srv.DataDir
	}
	return d.store.Paths.ServerData(srv.ID)
}

// LinkDataDir implements cluster.LinkFileHost: eşler mod/eklenti jar'larını
// buradan çeker.
func (d *Daemon) LinkDataDir() (string, bool) {
	srv := d.sharedWorldServer()
	if srv == nil {
		return "", false
	}
	return d.serverDataDir(srv), true
}

// sharedWorldServer returns the server running in shared-world mode, or nil.
func (d *Daemon) sharedWorldServer() *model.Server {
	list, err := d.store.ListServers()
	if err != nil {
		return nil
	}
	for _, s := range list {
		// Kardeş kopyalar da ortak dünya kipinde (server.properties için)
		// ama dünyanın sahibi ana sunucudur; kardeş seçilseydi eşlere
		// "<ad> #2" adıyla ikinci bir dünya yayılırdı.
		if s.Link.Mode == model.LinkSharedWorld && !s.IsSibling() {
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
		// kullanıcının verisidir), kipi düşürüp DURDURUYORUZ. Masaüstü düğüm
		// de durduruyor; burada durdurmamak, dünyanın yalnızca yarısını
		// tutan bir sunucuyu tek başına açık bırakıyordu (panel ise "eşlerdeki
		// kopyalar durdurulmaz" diyordu — iki taraf tutarsızdı).
		if srv := d.sharedWorldServer(); srv != nil {
			if srv.Link.OriginID != "" {
				_ = d.servers.Stop(srv.ID)
			}
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
	changed := !created &&
		(srv.Software != model.Software(spec.Software) || srv.MCVersion != spec.MCVersion)
	if changed {
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
	prevRules := srv.Link.Rules
	srv.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: spec.Difficulty,
		SlabChunks: spec.SlabChunks,
		Seed:       spec.Seed,
		LinkPort:   spec.LinkPort,
		// Kurucu BAŞKASI: bu makine kurulumu geri yaymamalı.
		OriginID: spec.OriginID,
		Rules:    spec.Rules,
	}
	spec.Rules.ApplyTo(srv)
	// Kural değiştiyse (ör. kurucu online-mode'u kapattı) çalışan sunucu
	// yeniden başlamalı: server.properties yalnızca açılışta okunur.
	rulesChanged := !created && spec.Rules != nil && !prevRules.Equal(spec.Rules)
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

	// Mod bu makinede kurulabilecek mi — kurulumla AYNI seçim (linkjar).
	// Kurulum arka planda koşuyor ve hatası yalnızca bu makinenin günlüğüne
	// düşüyordu; kurucu "oluşturuldu" görüp sınırın neden çalışmadığını
	// bilemiyordu. Neden yanıt metnine eklenir (eşleştirme iletisinde
	// görünür); sunucu yine kurulur, çünkü dünyanın bu yarısı kullanıcının
	// verisidir ve jar'lar eklenince bir sonraki eşitlemede mod da gelir.
	modWarn := ""
	if _, err := linkLocator().Find(srv.Software, srv.MCVersion); err != nil {
		modWarn = " — UYARI: " + err.Error()
	}

	// ── Zaten çalışıyorsa ve hiçbir şey değişmediyse DOKUNMA ──────────────
	// Kurucu kurulumu eşitleme döngüsüyle yeniden gönderir (katılımcı listesi
	// ya da port değişince). Eskiden burada ne "çalışıyor mu" ne de "ne
	// değişti" soruluyordu: yazılım değişse bile çalışan sunucu eski jar'la
	// sürüyordu, çünkü yalnızca durmuş sunucular başlatılıyordu.
	dataDir := d.serverDataDir(srv)
	st := d.servers.State(srv.ID)
	running := st == model.StateRunning || st == model.StateStarting
	filesPending := cluster.LinkFilesPending(dataDir, spec.Files)
	// Eldeki dünya BAŞKA bir tohumla üretilmişse (daha önce aynı adla farklı
	// bir ortak dünya kurulmuştu) Minecraft level.dat'taki tohumu kullanır ve
	// bu yarı yanlış araziyi üretir. Kenara alınmalı; bu da durdurmayı ister.
	seedChanged := false
	if have, err := cluster.WorldSeed(dataDir); err == nil && !cluster.SameSeed(have, spec.Seed) {
		seedChanged = true
	}
	if running && !changed && !seedChanged && !filesPending && !rulesChanged {
		return fmt.Sprintf("%s %s (port %d, çalışıyor)%s", srv.Name, verb, srv.Port, modWarn), srv.Port, nil
	}

	// Kurulum ve mod yüklemesi ARKA PLANDA: eş bizden 60 saniyeden uzun
	// süren bir indirme beklememeli, yoksa zaman aşımına uğrar ve kurulumu
	// başarısız sanır.
	files := append([]model.LinkFile(nil), spec.Files...)
	go func(s *model.Server) {
		if running {
			d.log.Infof("link: %s yeniden başlatılıyor (kurulum değişti)", s.Name)
			d.stopAndWait(s.ID)
		}
		if seedChanged {
			moved, err := cluster.RetireMismatchedWorld(dataDir, s.LevelSeed)
			if err != nil {
				d.log.Warnf("link: %s dünyası kenara alınamadı: %v", s.Name, err)
			} else if len(moved) > 0 {
				d.log.Warnf("link: %s tohumu değişti; eski dünya kenara alındı: %s",
					s.Name, strings.Join(moved, ", "))
			}
		}
		if err := d.servers.EnsureInstalled(context.Background(), s); err != nil {
			d.log.Errorf("link: %q kurulamadı: %v", s.Name, err)
			return
		}
		// Kurucunun mod/eklentileri: sunucu BAŞLAMADAN önce yerinde olmalı,
		// yoksa ilk açılışta dünyanın bu yarısı modsuz üretilir.
		if c := d.cluster.LinkCoord(); c != nil {
			if _, err := c.SyncLinkFiles(context.Background(), dataDir, files); err != nil {
				d.log.Warnf("link: %s mod/eklenti eşitlemesi eksik: %v", s.Name, err)
			}
		}
		if err := d.installLinkMod(s); err != nil {
			d.log.Errorf("link: mod kurulamadı (%s): %v", s.Name, err)
		}
		// ── Düzeltilen eksik: sunucu KURULUYOR ama BAŞLATILMIYORDU ──────
		// Kullanıcının isteği "o da başlasın" idi. Eşten gelen kurulum
		// sunucuyu oluşturup indiriyordu, sonra orada bırakıyordu: dünyanın
		// bu yarısı biri panelden elle "Başlat" diyene kadar kapalıydı ve
		// sınırı geçen oyuncu boşluğa aktarılıyordu.
		if st := d.servers.State(s.ID); st == model.StateRunning || st == model.StateStarting {
			return
		}
		s.RAMMB, s.CPUQuota = d.clampToBudget(s.RAMMB, s.CPUQuota)
		if err := d.servers.Start(context.Background(), s); err != nil {
			d.log.Errorf("link: %q başlatılamadı: %v", s.Name, err)
			return
		}
		d.log.Infof("link: %s başlatıldı (port %d)", s.Name, s.Port)
	}(srv.Clone())

	return fmt.Sprintf("%s %s (port %d)%s", srv.Name, verb, srv.Port, modWarn), srv.Port, nil
}

// liveWorldSeed reads the world's seed, forcing a save if the server runs.
//
// ── Yakalanan gerçek hata (uçtan uca sınamada ölçüldü) ──────────────────────
// Minecraft level.dat'ı İLK KAYITTA yazar (5 dakikalık otomatik kayıt ya da
// kapanış). Yeni açılmış bir sunucuda dünya BELLEKTE rastgele bir tohumla
// üretilmiş ama level.dat henüz YOK: WorldSeed "dünya yok" der, ortak dünya
// yeni bir tohum üretip eşlere onu gönderirdi — kurucu ise rastgele tohumla
// üretmeye devam ederdi. Çalışan sunucuya "save-all flush" gönderip level.dat
// diske düşene kadar bekliyoruz (ölçüldü: Paper 1.21.11'de < 1 sn).
func (d *Daemon) liveWorldSeed(srv *model.Server, dataDir string) (string, error) {
	have, err := cluster.WorldSeed(dataDir)
	if !errors.Is(err, cluster.ErrNoWorld) {
		return have, err
	}
	st := d.servers.State(srv.ID)
	if st == model.StateStarting {
		// Açılış sürüyor: dünya şu an üretiliyor olabilir. Açılmasını bekle,
		// yoksa kaydetme komutu boşa gider.
		deadline := time.Now().Add(3 * time.Minute)
		for st == model.StateStarting && time.Now().Before(deadline) {
			time.Sleep(time.Second)
			st = d.servers.State(srv.ID)
		}
	}
	if st != model.StateRunning {
		return "", err // çalışmıyor ve dünya yok: tohum serbestçe seçilebilir
	}
	if cerr := d.servers.Command(srv.ID, "save-all flush"); cerr != nil {
		return "", fmt.Errorf("dünya kaydettirilemedi: %w", cerr)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if have, err = cluster.WorldSeed(dataDir); !errors.Is(err, cluster.ErrNoWorld) {
			return have, err
		}
		time.Sleep(250 * time.Millisecond)
	}
	// Sunucu çalışıyor ama level.dat gelmedi: tohumu BİLMİYORUZ. "Dünya yok"
	// demek yeni bir tohum ürettirirdi; hata olarak bildir.
	return "", fmt.Errorf("sunucu çalışıyor ama level.dat yazılmadı")
}

// stopAndWait stops a server and waits (bounded) until it has exited.
//
// Dünya klasörünü taşımadan ya da jar'ı değiştirmeden önce süreç GERÇEKTEN
// bitmiş olmalı: Minecraft kapanırken dünyayı diske yazar.
func (d *Daemon) stopAndWait(id string) {
	_ = d.servers.Stop(id)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		st := d.servers.State(id)
		if st != model.StateRunning && st != model.StateStarting && st != model.StateStopping {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// installLinkMod copies the right mcos-link jar into the server.
//
// Mod OLMADAN ortak dünya çalışmaz: oyuncu sınırı geçtiğinde hiçbir şey
// olmaz ve dünyanın öbür yarısı boş görünür. Bu yüzden eksikliği sessizce
// geçmiyoruz: hata NEDENİYLE döner, link.status'ta ModProblem olarak görünür
// ve link.enable onu kullanıcıya hata olarak verir.
//
// Hangi jar: sunucunun Minecraft sürümüne göre linkjar seçer (indeks). Hedef
// ad SABİT (mods/mcos-link.jar, plugins/mcos-link-paper.jar): linkModInstalled
// ve eski sunucuların temizliği bu adlara bakıyor.
//
// ── Uyumsuz sürümde eski kopya KALDIRILIR ───────────────────────────────────
// Link modu VARSAYILAN OLARAK her sunucuya kuruluyor. Eskiden fabric.mod.json
// kendini ">=1.20.5" ile uyumlu ilan ediyordu ve 1.21.11 jar'ı 1.21.1'de
// açılışta NoSuchFieldError ile düşüyordu (uçtan uca sınamada görüldü).
// Sürümü değişen bir sunucuda önceki sürümün jar'ı kalırsa aynısı olur:
// kaldırılır ki sunucu en azından ortak dünyasız açılsın.
//
// ── Fabric: API ÖNCE, mod SONRA ─────────────────────────────────────────────
// mcos-link fabric-api'yi SERT bağımlılık olarak bildiriyor; Fabric Loader
// eksik sert bağımlılıkta modu atlamaz, SUNUCUYU HİÇ AÇMAZ:
//
//	Incompatible mods found!
//	- Mod 'MCOS Link' (mcos-link) 1.0.1 requires any version of
//	  fabric-api, which is missing!
//
// (Gerçek bir Fabric 1.21.11 sunucusunda görüldü.) API sağlanamıyorsa mod da
// KURULMAZ — varsa eski kopyası da kaldırılır. Ortak dünyasız çalışan bir
// sunucu, hiç açılmayan bir sunucudan iyidir.
func (d *Daemon) installLinkMod(srv *model.Server) (err error) {
	// Aynı sunucuya iki kurulum aynı anda koşmasın (bkz. lockLinkInstall).
	unlock := lockLinkInstall(srv.ID)
	defer unlock()
	defer func() { recordLinkProblem(srv.ID, err) }()
	name, sub, ok := linkArtifact(srv.Software)
	if !ok {
		if _, lerr := linkjar.LoaderFor(srv.Software); lerr != nil {
			return lerr
		}
		return fmt.Errorf("%s ortak dünyayı desteklemiyor", srv.Software)
	}
	dir := filepath.Join(d.serverDataDir(srv), sub)
	dst := filepath.Join(dir, name)
	res, err := linkLocator().Find(srv.Software, srv.MCVersion)
	if err != nil {
		if linkjar.KindOf(err) == linkjar.KindVersion {
			_ = os.Remove(dst)
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if res.Loader == linkjar.Fabric && res.Dep != "" {
		if err := d.ensureFabricAPI(srv, dir, res); err != nil {
			_ = os.Remove(dst)
			return err
		}
	}
	if err := copyFile(res.Jar, dst); err != nil {
		return err
	}
	d.log.Infof("link: %s: %s -> %s/%s (Minecraft %s)", srv.Name,
		filepath.Base(res.Jar), sub, name, res.MC)
	return nil
}

// linkProblems remembers the last install failure per server (server id ->
// reason). linkjar.Find'ın söylemediği nedenler (fabric-api indirilemedi,
// disk dolu) de panele ulaşsın diye: kurulum arka planda koşuyor ve hata
// yalnızca günlükte kalıyordu.
var linkProblems sync.Map

// linkInstallLocks holds one mutex per server id.
var linkInstallLocks sync.Map

// lockLinkInstall serializes installLinkMod for one server; dönüş kilidi açar.
//
// Aynı sunucuya iki kurulum AYNI ANDA koşabiliyor: link.enable, sunucu
// oluşturma/sürüm değiştirme gorutinindeki ensureLinkArtifact ile ya da eşin
// art arda iki ApplyLinkSpec gorutini. İkisi de "fabric-api yok" görüp
// indirir, ikisi de aynı "<ad>.tmp" dosyasına yazar ve ikincinin os.Rename'i
// "no such file" ile düşer — link.enable anlamsız bir hatayla reddedilirdi.
// Sınamada ölçüldü (TestConcurrentInstallLinkModFetchesOnce): kilitsiz iki
// eş zamanlı kurulum fabric-api'yi İKİ kez indirdi ve biri "rename
// …/mods/mcos-link.jar.tmp …: no such file or directory" ile düştü.
func lockLinkInstall(id string) func() {
	v, _ := linkInstallLocks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func recordLinkProblem(id string, err error) {
	if err == nil {
		linkProblems.Delete(id)
		return
	}
	linkProblems.Store(id, err.Error())
}

// linkModProblem says why the mod is not on this server ("" = bilinmiyor).
//
// Önce BUGÜNKÜ gerçek (Find): son kurulumdan sonra jar eklenmiş ya da sürüm
// değişmiş olabilir. Seçim başarılıysa son kurulum hatası anlatılır.
func (d *Daemon) linkModProblem(srv *model.Server) string {
	if _, err := linkLocator().Find(srv.Software, srv.MCVersion); err != nil {
		return err.Error()
	}
	if v, ok := linkProblems.Load(srv.ID); ok {
		return v.(string)
	}
	return ""
}

// fetchFabricAPI downloads fabric-api from Modrinth (son çare).
//
// Değişken, çünkü sınamalar ağa çıkmamalı. Zaman aşımı KISA: sunucu kurulumu
// bu çağrıda süresiz asılı kalmamalı.
var fetchFabricAPI = func(d *Daemon, mc, modsDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return d.catalog.InstallByID(ctx, "fabric-api", "fabric", mc, modsDir)
}

// findBundledFabricAPI looks for a fabric-api built for mc in MCOS's local
// artifact stores, whatever its exact build number (kural:
// linkjar.FabricAPIFor — sürümü tutmayan kopya asla seçilmez).
func findBundledFabricAPI(mc string) string {
	return linkjar.FabricAPIFor(fabricAPIDirs(), mc)
}

// ensureFabricAPI guarantees mods/ has the RIGHT fabric-api before mcos-link
// lands (kural ve sıra: linkjar.PrepareFabricAPI).
//
// Sağlanamazsa HATA döner ve çağıran mod'u kurmaz (gerekçe installLinkMod'da).
func (d *Daemon) ensureFabricAPI(srv *model.Server, modsDir string, res linkjar.Result) error {
	if res.DepPath == "" {
		if p := findBundledFabricAPI(srv.MCVersion); p != "" {
			res.DepPath = p
			res.Dep = linkjar.CleanCacheName(filepath.Base(p))
		}
	}
	note, err := linkjar.PrepareFabricAPI(modsDir, res, srv.MCVersion, func() (string, error) {
		return fetchFabricAPI(d, srv.MCVersion, modsDir)
	})
	if err != nil {
		return fmt.Errorf("%w; ortak dünya modu kurulmadı — modu fabric-api "+
			"olmadan koymak sunucunun HİÇ açılmamasına yol açardı", err)
	}
	d.log.Infof("link: %s: %s", srv.Name, note)
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
	name, sub, ok := linkArtifact(srv.Software)
	if !ok {
		return false
	}
	st, err := os.Stat(filepath.Join(d.serverDataDir(srv), sub, name))
	return err == nil && st.Size() > 0
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
		if !st.ModInstalled {
			st.ModProblem = d.linkModProblem(srv)
		}
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
		// NEDEN de yazılır: eskiden yalnızca "kurulmadı" deniyordu ve
		// kullanıcı 26.3 sunucusunun neden ortak dünya olamadığını göremedi.
		st.Note = "ortak dünya modu sunucuya kurulmadı — ortak dünya çalışmaz"
		if st.ModProblem != "" {
			st.Note = st.ModProblem + " — ortak dünya çalışmaz"
		}
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
	// linkArtifact, SupportsMods DEĞİL: eskiden burada SupportsMods vardı ve
	// Paper'ı REDDEDİYORDU — oysa Paper için ayrı bir eklenti
	// (mcos-link-paper.jar) var ve installLinkMod onu kuruyor. Forge ise
	// SupportsMods'ta "evet" diyor ama Fabric modunu hiç yüklemiyor.
	if _, _, ok := linkArtifact(srv.Software); !ok {
		msg := "ortak dünya yalnızca Fabric ve Paper/Purpur sunucularında çalışır"
		if _, err := linkjar.LoaderFor(srv.Software); err != nil {
			msg = err.Error()
		}
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: msg}
	}

	// En az iki eşleşmiş cihaz gerekir — yoksa "ortak" bir dünya yok.
	paired := 0
	for _, peer := range d.cluster.Peers() {
		if peer.Paired {
			paired++
		}
	}
	// Aynı makinede kopyalara bölünmüş bir sunucu PC olmadan da ortak
	// dünyadır: kopyalar birbirinin düğümüdür.
	local := d.instanceCount(srv) - 1
	if paired == 0 && local == 0 {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "önce en az bir PC eşleştirin (MCOS Paylaşım ekranı) ya da sunucu sayısını 2+ yapın"}
	}

	// ── Mod ÖNCE: kurulamıyorsa ortak dünya AÇILMAZ ─────────────────────
	// Kullanıcının gerçek raporu: "ortak dünyayı açınca 'mcos link kurulu
	// değil' diyor." Eskiden burada mod hatası yalnızca günlüğe yazılıyor,
	// sunucu yine ortak dünya olarak işaretleniyor ve kullanıcıya "ortak
	// dünya oldu" deniyordu: eşlere kurulum gidiyor, sınır hiç çalışmıyordu.
	// Şimdi NEDEN (ör. "Fabric 1.20.1 için ortak dünya modu yok
	// (desteklenen: 1.20.5–26.3)") hata olarak döner ve hiçbir şey
	// işaretlenmez.
	if err := d.installLinkMod(srv); err != nil {
		d.log.Warnf("link: %s ortak dünya açılamadı, mod kurulamadı: %v", srv.Name, err)
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}

	diff := model.LinkDifficulty(strings.TrimSpace(p.Difficulty))
	if diff == "" {
		diff = model.DifficultyNormal
	}
	seed := strings.TrimSpace(p.Seed)
	// ── Dünya ZATEN VARSA tohum oradan gelir ─────────────────────────────
	// Uçtan uca sınamada ölçüldü: sunucu önce kurulup açılmıştı; burada
	// üretilen tohum yalnızca kayda yazılıyor, kurucunun level.dat'ında ise
	// rastgele bir tohum (1347023201105565743) duruyordu. Düğüm 424242 ile
	// kuruldu — iki ayrı dünya. Var olan bir dünyanın tohumu değiştirilemez;
	// doğru olan, eşlere GERÇEK tohumu göndermektir.
	seedNote := ""
	dataDir := d.serverDataDir(srv)
	switch have, err := d.liveWorldSeed(srv, dataDir); {
	case err == nil:
		if seed != "" && !cluster.SameSeed(seed, have) {
			seedNote = fmt.Sprintf(" — bu dünya zaten %s tohumuyla oluşmuş; "+
				"eşlere o gönderildi (girilen tohum yalnızca yeni dünyada geçerli olur)", have)
		}
		seed = have
	case !errors.Is(err, cluster.ErrNoWorld):
		// level.dat okunamıyor: tohumu bilmeden devam etmek iki ayrı dünya
		// riskidir. Kullanıcıya açıkça söyle.
		d.log.Warnf("link: %s dünyasının tohumu okunamadı: %v", srv.Name, err)
		seedNote = " — UYARI: dünyanın tohumu okunamadı (" + err.Error() +
			"); iki makinede arazi farklı olabilir"
	}
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
		// OriginID BOŞ: kurucu bu makine.
	}
	srv.LevelSeed = seed
	srv.Difficulty = string(diff)
	srv.UpdatedAt = time.Now()
	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}

	// Tohum/zorluk server.properties'e ŞİMDİ yazılır; yoksa ilk açılışa
	// kadar diskte eski değerler durur (Start da yeniden yazar, bkz.
	// server.WriteLinkProperties). Çalışan sunucuda zorluk komutla
	// hemen uygulanır — yeniden başlatmadan.
	if err := server.WriteLinkProperties(dataDir, srv); err != nil {
		d.log.Warnf("link: server.properties yazılamadı: %v", err)
	}
	if st := d.servers.State(srv.ID); st == model.StateRunning {
		_ = d.servers.Command(srv.ID, "difficulty "+string(diff))
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
		srv.Name, paired+1, model.DifficultyLabel(diff)) + seedNote
	if local > 0 {
		msg += fmt.Sprintf(" — bu makinede %d sunucuya bölünecek", local+1)
	}
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

// ── RPC: cluster.scanStart / cluster.scanStatus ─────────────────────────────
//
// CANLI eş taraması. Kablosuz taramasıyla AYNI desen (bkz. handlers_net.go):
// başlat + yokla. Kullanıcının isteği her iki liste için de aynıydı —
// "ağı tararken dönen animasyon, listeye seçenek gelince aynı animasyon".
//
// Neden bloklayan cluster.scan duruyor: telefon uygulaması ve betikler onu
// kullanıyor; kaldırmak onları kırardı.

// peerScanSession holds the running LAN scan.
type peerScanSession struct {
	mu      sync.Mutex
	running bool
	gen     int
	peers   []model.Peer
	total   int
	done    int
	err     string
	started time.Time
}

func (d *Daemon) peerScanSess() *peerScanSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.peerScan == nil {
		d.peerScan = &peerScanSession{}
	}
	return d.peerScan
}

// PeerScanProgress is what the panel polls.
type PeerScanProgress struct {
	Scanning bool         `json:"scanning"`
	Peers    []model.Peer `json:"peers"`
	Total    int          `json:"total"`
	Done     int          `json:"done"`
	Error    string       `json:"error,omitempty"`
	Gen      int          `json:"gen"`
}

func peerProgressOf(s *peerScanSession) PeerScanProgress {
	out := PeerScanProgress{
		Scanning: s.running, Total: s.total, Done: s.done,
		Error: s.err, Gen: s.gen,
	}
	out.Peers = append([]model.Peer(nil), s.peers...)
	return out
}

func (d *Daemon) handleClusterScanStart(_ context.Context, _ json.RawMessage) (any, error) {
	if !d.Config().Cluster.Enabled {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "PC paylaşımı kapalı — Ayarlar'dan açın"}
	}
	s := d.peerScanSess()

	s.mu.Lock()
	if s.running {
		out := peerProgressOf(s)
		s.mu.Unlock()
		return out, nil
	}
	s.running = true
	s.gen++
	s.peers = nil
	s.total, s.done, s.err = 0, 0, ""
	s.started = time.Now()
	gen := s.gen
	out := peerProgressOf(s)
	s.mu.Unlock()

	go func() {
		// Bağlamı İSTEKTEN AYIRIYORUZ: istek yanıtlandığı an ctx iptal olur;
		// ona bağlanmak taramayı başlar başlamaz öldürürdü.
		sctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		peers, err := d.cluster.ScanLAN(sctx, func(p cluster.ScanProgress) {
			s.mu.Lock()
			if s.gen == gen {
				s.total, s.done = p.Total, p.Done
				// Bulunanları da taşı: ilerleme raporu yalnızca SAYI
				// veriyor, panel ise satırları istiyor.
				s.peers = append([]model.Peer(nil), p.Peers...)
			}
			s.mu.Unlock()
		})

		s.mu.Lock()
		if s.gen == gen {
			if err != nil {
				s.err = err.Error()
			} else {
				s.peers = peers
			}
			s.running = false
		}
		s.mu.Unlock()
	}()

	return out, nil
}

func (d *Daemon) handleClusterScanStatus(_ context.Context, _ json.RawMessage) (any, error) {
	s := d.peerScanSess()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Emniyet supabı: tarama asılı kalırsa panel sonsuza kadar beklemesin.
	if s.running && !s.started.IsZero() && time.Since(s.started) > 40*time.Second {
		s.running = false
		if s.err == "" {
			s.err = "tarama zaman aşımına uğradı"
		}
	}
	return peerProgressOf(s), nil
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
	if errors.Is(err, cluster.ErrNeedsCodePairing) {
		return map[string]any{"peer": peer, "needsCode": true,
			"message": peer.Name + " bulundu — kodla eşleştirme başlatılıyor"}, nil
	}
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	}
	msg := peer.Name + " eşleştirildi (" + peer.IP + ")"
	if peer.Problem != "" {
		msg += " — " + peer.Problem
	}

	// Ortak dünya açıksa YENİ EŞE HEMEN KUR. Eskiden kurulum yalnızca
	// "ortak dünyayı aç" anında gönderiliyordu; sonradan eşleşen PC hiçbir
	// şey almıyordu. Kullanıcının isteği aynen: "eşleyince ona da sunucu
	// kurulacak, alan belirlenecek, mod kurulacak, o da başlayacak".
	if coord := d.cluster.LinkCoord(); coord != nil && peer.Problem == "" {
		res, ok, perr := coord.SyncPeer(peer)
		switch {
		case perr != nil:
			msg += " — ortak dünya kurulamadı: " + perr.Error()
		case ok:
			msg += " — ortak dünya oraya kuruluyor: " + res
		}
	}
	d.log.Infof("cluster: %s", msg)
	return map[string]any{
		"peer":    peer,
		"message": msg,
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
		// enabled: panel kapalıyken "Ağı tara" yerine "PC paylaşımını aç"
		// gösterebilsin. Eskiden kullanıcı taramaya basıyor, "kapalı —
		// Ayarlar'dan açın" hatası alıp ekranı terk etmek zorunda kalıyordu.
		"enabled": d.Config().Cluster.Enabled,
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
