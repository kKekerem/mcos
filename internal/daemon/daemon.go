// Package daemon wires together the store, logger, supervisor, and subsystems,
// and exposes them as JSON-RPC handlers. It is the "brain" referenced in the
// architecture: every front-end is a thin client over the handlers registered
// here.
package daemon

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"mcos/internal/backup"
	"mcos/internal/catalog"
	"mcos/internal/cluster"
	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/players"
	"mcos/internal/server"
	"mcos/internal/sshd"
	"mcos/internal/store"
	"mcos/internal/supervisor"
	"mcos/internal/tunnel"
	"mcos/internal/version"
	"mcos/internal/worlds"
)

// Version is the MCOS release string surfaced in status/ping.
//
// Tek kaynak internal/version; burada yeniden yazmak, daemon ile panelin
// farkli surum bildirmesine yol aciyordu.
const Version = version.Version

// Daemon is the central coordinator.
type Daemon struct {
	cfgPath   string
	store     *store.Store
	log       *log.Logger
	sup       *supervisor.Supervisor
	java      *java.Manager
	servers   *server.Manager
	backup    *backup.Manager
	files     *files.Manager
	worlds    *worlds.Manager
	players   *players.Manager
	cluster   *cluster.Manager
	tunnelMgr *tunnel.Manager
	catalog   *catalog.Client

	// playitSt, tünel ajanının durumudur. TEMBEL kurulur: playit isteğe
	// bağlıdır ve imajda hiç bulunmayabilir (bkz. handlers_playit.go).
	playitSt *playitState

	// remoteSt, telefon uygulamasinin bagli oldugu HTTPS koprusu.
	remoteSt remoteState
	// wifiScan, canli kablosuz taramasinin oturumu (bkz. handlers_net.go).
	// TEMBEL kurulur: tarama hic istenmezse hicbir sey ayrilmaz.
	wifiScan *wifiScanSession
	// peerScan, canli LAN tarama oturumu (bkz. handlers_link.go).
	peerScan *peerScanSession
	// vncSt, ekran paylasimi (RFB). TEMBEL kurulur: kapaliyken hicbir sey
	// acilmaz (bkz. handlers_vnc.go).
	vncSt vncState
	// ssh, kabuk erisimi (dropbear).
	ssh *sshd.Manager
	// turboSt, donanim turbosu (frekans, fanlar, P-cekirdekleri); bkz.
	// handlers_turbo.go. TEMBEL kurulur.
	turboSt turboState
	// backupSt, otomatik yedek zamanlayıcısının süreç içi belleği (bkz.
	// backup_auto.go).
	backupSt autoBackupState
	// proxySt, ortak dünyanın tek adresli Velocity proxy'si (bkz. proxy.go).
	proxySt proxyState
	// rpc, kendi yontem tablomuz. Uzaktan kontrol koprusu AYNI tabloyu
	// kullanir; ayri bir tablo tutmak, iki yolun zamanla ayrismasi demekti.
	rpc *ipc.Server

	mu        sync.RWMutex
	cfg       *model.Config
	startedAt time.Time
}

// newLinkCoordinator wires the shared-world coordinator to this daemon.
//
// Ayrı bir yardımcı çünkü cluster paketi daemon'u ithal EDEMEZ (döngü
// olurdu); bağlantı burada, LinkHost arayüzü üzerinden kuruluyor.
func newLinkCoordinator(d *Daemon) *cluster.LinkCoordinator {
	c := cluster.NewLinkCoordinator(d.cluster, d)
	// Velocity eklentisi arka uç listesini buradan okur (/link/proxy):
	// liste değişince proxy'yi yeniden başlatıp herkesi düşürmemek için.
	c.SetProxySource(d.proxyList)
	return c
}

// New constructs a daemon: loads config, opens the store, prepares logging,
// the process supervisor, and the Java + server managers.
func New(cfgPath, dataRoot string, lg *log.Logger) (*Daemon, error) {
	st, err := store.New(dataRoot)
	if err != nil {
		return nil, err
	}
	cfg, err := store.LoadConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	sup := supervisor.New(lg)
	jm := java.NewManager(st, lg)
	sm := server.NewManager(st, jm, sup, lg)
	tm := tunnel.NewManager(st, lg)
	d := &Daemon{
		cfgPath:   cfgPath,
		store:     st,
		log:       lg,
		sup:       sup,
		java:      jm,
		servers:   sm,
		backup:    backup.NewManager(st, sm, lg),
		files:     files.NewManager(st, lg),
		worlds:    worlds.NewManager(st, lg),
		players:   players.NewManager(sm),
		tunnelMgr: tm,
		catalog:   catalog.New(),
		cfg:       cfg,
		startedAt: time.Now(),
	}
	// SSH yoneticisi: sunucu anahtarlari KALICI veri klasorunde durur,
	// yoksa her acilista degisir ve istemci 'anahtar degisti' diye reddeder.
	d.ssh = sshd.New(dataRoot, lg)
	// Cluster needs an Executor that runs real work via the daemon's
	// subsystems, so it is wired after d exists.
	d.cluster = cluster.NewManager(cfg.Cluster, Version, st, lg, taskExecutor{d})
	// Kardeş kopyaya hangi düğüm olduğunu söyleyen ortam (MCOS_LINK_SELF).
	sm.ExtraEnv = d.instanceEnv
	return d, nil
}

// Java exposes the Java runtime manager.
func (d *Daemon) Java() *java.Manager { return d.java }

// Servers exposes the server lifecycle manager.
func (d *Daemon) Servers() *server.Manager { return d.servers }

// Config returns a snapshot pointer to the current config (read-only use).
func (d *Daemon) Config() *model.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
}

// Store exposes the underlying store for subsystems that need direct access.
func (d *Daemon) Store() *store.Store { return d.store }

// Log returns the daemon logger.
func (d *Daemon) Log() *log.Logger { return d.log }

// Register installs all RPC handlers onto the IPC server.
func (d *Daemon) Register(s *ipc.Server) {
	// Uzaktan kontrol koprusu bu tabloyu yeniden kullanir.
	d.rpc = s

	s.Handle(ipc.MethodRemoteStatus, d.handleRemoteStatus)
	s.Handle(ipc.MethodRemoteEnable, d.handleRemoteEnable)
	s.Handle(ipc.MethodRemoteDisable, d.handleRemoteDisable)
	s.Handle(ipc.MethodRemoteRotate, d.handleRemoteRotate)

	s.Handle(ipc.MethodVNCStatus, d.handleVNCStatus)
	s.Handle(ipc.MethodVNCEnable, d.handleVNCEnable)
	s.Handle(ipc.MethodVNCDisable, d.handleVNCDisable)
	s.Handle(ipc.MethodVNCRotate, d.handleVNCRotate)
	s.Handle(ipc.MethodVNCViewOnly, d.handleVNCViewOnly)

	s.Handle(ipc.MethodSSHStatus, d.handleSSHStatus)
	s.Handle(ipc.MethodSSHEnable, d.handleSSHEnable)
	s.Handle(ipc.MethodSSHDisable, d.handleSSHDisable)
	s.Handle(ipc.MethodSSHPassword, d.handleSSHPassword)
	s.Handle(ipc.MethodSSHAddKey, d.handleSSHAddKey)

	s.Handle(ipc.MethodPing, d.handlePing)
	s.Handle(ipc.MethodSystemStatus, d.handleSystemStatus)
	s.Handle(ipc.MethodSystemPower, d.handleSystemPower)
	s.Handle(ipc.MethodSystemTurbo, d.handleSystemTurbo)
	s.Handle(ipc.MethodSystemDisks, d.handleSystemDisks)
	s.Handle(ipc.MethodSystemPersist, d.handleSystemPersist)
	s.Handle(ipc.MethodSystemUpdateScan, d.handleSystemUpdateScan)
	s.Handle(ipc.MethodSystemUpdate, d.handleSystemUpdate)
	s.Handle(ipc.MethodSystemUpdateStatus, d.handleSystemUpdateStatus)
	s.Handle(ipc.MethodConfigGet, d.handleConfigGet)
	s.Handle(ipc.MethodConfigSet, d.handleConfigSet)

	s.Handle(ipc.MethodServerList, d.handleServerList)
	s.Handle(ipc.MethodServerGet, d.handleServerGet)
	s.Handle(ipc.MethodServerCreate, d.handleServerCreate)
	s.Handle(ipc.MethodServerPerfPack, d.handleServerPerfPack)
	s.Handle(ipc.MethodServerUpdate, d.handleServerUpdate)
	s.Handle(ipc.MethodServerDelete, d.handleServerDelete)
	s.Handle(ipc.MethodServerInstall, d.handleServerInstall)
	s.Handle(ipc.MethodServerStart, d.handleServerStart)
	s.Handle(ipc.MethodServerStop, d.handleServerStop)
	s.Handle(ipc.MethodServerRestart, d.handleServerRestart)
	s.Handle(ipc.MethodServerConsole, d.handleServerConsole)
	s.Handle(ipc.MethodServerCommand, d.handleServerCommand)
	s.Handle(ipc.MethodServerChangeVersion, d.handleServerChangeVersion)

	s.Handle(ipc.MethodCatalogSearch, d.handleCatalogSearch)
	s.Handle(ipc.MethodCatalogInstall, d.handleCatalogInstall)
	s.Handle(ipc.MethodServerScanUSBMods, d.handleServerScanUSBMods)
	s.Handle(ipc.MethodServerInstallUSBMods, d.handleServerInstallUSBMods)
	s.Handle(ipc.MethodServerScanUSBFolders, d.handleServerScanUSBFolders)
	s.Handle(ipc.MethodServerImportUSB, d.handleServerImportUSB)
	s.Handle(ipc.MethodServerImportFolder, d.handleServerImportFolder)

	s.Handle(ipc.MethodJavaList, d.handleJavaList)
	s.Handle(ipc.MethodJavaResolve, d.handleJavaResolve)
	s.Handle(ipc.MethodJavaInstall, d.handleJavaInstall)
	s.Handle(ipc.MethodJavaRemove, d.handleJavaRemove)
	s.Handle(ipc.MethodJavaDetect, d.handleJavaDetect)
	s.Handle(ipc.MethodJavaProgress, d.handleJavaProgress)

	s.Handle(ipc.MethodBackupList, d.handleBackupList)
	s.Handle(ipc.MethodBackupCreate, d.handleBackupCreate)
	s.Handle(ipc.MethodBackupRestore, d.handleBackupRestore)
	s.Handle(ipc.MethodBackupDelete, d.handleBackupDelete)
	s.Handle(ipc.MethodBackupPolicy, d.handleBackupPolicy)
	s.Handle(ipc.MethodBackupSetPolicy, d.handleBackupSetPolicy)

	s.Handle(ipc.MethodFilesList, d.handleFilesList)
	s.Handle(ipc.MethodFilesRead, d.handleFilesRead)
	s.Handle(ipc.MethodFilesWrite, d.handleFilesWrite)
	s.Handle(ipc.MethodFilesDelete, d.handleFilesDelete)

	s.Handle(ipc.MethodPlayersList, d.handlePlayersList)
	s.Handle(ipc.MethodPlayersCommand, d.handlePlayersCommand)

	s.Handle(ipc.MethodWorldsList, d.handleWorldsList)
	s.Handle(ipc.MethodWorldsRename, d.handleWorldsRename)
	s.Handle(ipc.MethodWorldsDelete, d.handleWorldsDelete)

	s.Handle(ipc.MethodClusterPeers, d.handleClusterPeers)
	s.Handle(ipc.MethodClusterPair, d.handleClusterPair)
	s.Handle(ipc.MethodClusterPairOffer, d.handleClusterPairOffer)
	s.Handle(ipc.MethodClusterPairConfirm, d.handleClusterPairConfirm)
	s.Handle(ipc.MethodClusterPairCancel, d.handleClusterPairCancel)
	s.Handle(ipc.MethodClusterTasks, d.handleClusterTasks)
	s.Handle(ipc.MethodClusterScan, d.handleClusterScan)
	s.Handle(ipc.MethodClusterScanStart, d.handleClusterScanStart)
	s.Handle(ipc.MethodClusterScanStatus, d.handleClusterScanStatus)
	s.Handle(ipc.MethodClusterPairManual, d.handleClusterPairManual)
	s.Handle(ipc.MethodClusterSecret, d.handleClusterSecret)

	s.Handle(ipc.MethodLinkStatus, d.handleLinkStatus)
	s.Handle(ipc.MethodLinkEnable, d.handleLinkEnable)
	s.Handle(ipc.MethodLinkDisable, d.handleLinkDisable)
	s.Handle(ipc.MethodLinkEvents, d.handleLinkEvents)

	s.Handle(ipc.MethodPlayitStatus, d.handlePlayitStatus)
	s.Handle(ipc.MethodPlayitClaim, d.handlePlayitClaim)
	s.Handle(ipc.MethodPlayitPoll, d.handlePlayitPoll)
	s.Handle(ipc.MethodPlayitStart, d.handlePlayitStart)
	s.Handle(ipc.MethodPlayitStop, d.handlePlayitStop)
	s.Handle(ipc.MethodPlayitInstall, d.handlePlayitInstall)
	s.Handle(ipc.MethodPlayitTunnel, d.handlePlayitTunnel)

	s.Handle(ipc.MethodServerVersions, d.handleServerVersions)
	s.Handle(ipc.MethodNetWiFiScan, d.handleNetWiFiScan)
	s.Handle(ipc.MethodNetWiFiScanStart, d.handleNetWiFiScanStart)
	s.Handle(ipc.MethodNetWiFiScanStatus, d.handleNetWiFiScanStatus)
	s.Handle(ipc.MethodNetWiFiApply, d.handleNetWiFiApply)
	s.Handle(ipc.MethodNetWiredUp, d.handleNetWiredUp)

	s.Handle(ipc.MethodTunnelList, d.handleTunnelList)
	s.Handle(ipc.MethodTunnelCreate, d.handleTunnelCreate)
	s.Handle(ipc.MethodTunnelResolve, d.handleTunnelResolve)
	s.Handle(ipc.MethodTunnelStart, d.handleTunnelStart)
	s.Handle(ipc.MethodTunnelStop, d.handleTunnelStop)
}

// Run performs startup work (autostart) and blocks until ctx is cancelled, then
// gracefully stops all supervised processes.
func (d *Daemon) Run(ctx context.Context) {
	d.applyNetworkFromConfig()
	go d.timeSyncLoop(ctx)
	d.applyClusterFromConfig()
	d.startLinkCoordinator()

	// Uzaktan kontrol ve SSH, yeniden baslatmayi ATLATMALI: kullanici
	// telefondan acip makineyi yeniden baslatinca erisimini kaybederse,
	// makinenin basina gitmek zorunda kalir -- ki uzaktan erisimin varlik
	// sebebi tam olarak bunu onlemekti.
	if err := d.startRemote(); err != nil {
		d.log.Warnf("remote: acilista baslatilamadi: %v", err)
	}
	if sc := d.Config().SSH; sc.Enabled {
		if err := d.ssh.Apply(sc); err != nil {
			d.log.Warnf("sshd: acilista baslatilamadi: %v", err)
		}
	}
	// Ekran paylasimi da yeniden baslatmayi atlatmali: uzaktan baglanip
	// makineyi yeniden baslatan kullanici, ekrani bir daha goremezse
	// makinenin basina gitmek zorunda kalir.
	if vncConfigured(d.Config()) {
		if err := d.startVNC(); err != nil {
			d.log.Warnf("vnc: acilista baslatilamadi: %v", err)
		}
	}

	// Turbo, autostart'tan ONCE: acilista baslayan sunucular baslatma
	// kancasiyla dogrudan P-cekirdeklerinde dogsun (bkz. handlers_turbo.go).
	d.turboStartup(ctx)
	d.autostart(ctx)
	go d.backupScheduler(ctx)
	// WinSCP: /data/sunucular altında adlı bağlar ve yüklenen klasörlerin içe
	// aktarımı (bkz. handlers_sftp.go).
	go d.sftpLoop(ctx)
	go d.clusterWorkLoop(ctx)
	// Ortak dünya proxy'si: sunucular açıldıktan sonra (autostart) Velocity
	// onların önüne geçer.
	go d.proxyLoop(ctx)
	<-ctx.Done()
	d.log.Infof("daemon: shutting down, stopping all servers")
	if c := d.cluster.LinkCoord(); c != nil {
		c.Stop()
	}
	d.stopRemote()
	d.stopVNC()
	d.ssh.Stop()
	d.playitStop()
	d.cluster.Stop()
	d.sup.StopAll()
	// Sunucular dunyayi tam hizda kaydettikten SONRA: fanlar otomatige,
	// frekans ayarlari ozgun haline doner.
	d.turboShutdown()
}

// applyClusterFromConfig starts or stops the LAN cluster to match
// config.cluster.enabled, generating the pre-shared key on first enable.
//
// GÜVENLİK: eskiden Run() koşulsuz d.cluster.Start() çağırıyordu ve
// ClusterConfig.Enabled alanı kod tabanında HİÇ okunmuyordu. Sonuç: kullanıcı
// OOBE'de "PC eşleştirme"yi kapatsa bile kimliksiz eşleştirme sunucusu her
// arabirimde :27890 portunu dinliyordu. Artık bayrak gerçekten uygulanıyor ve
// config.set ile çalışma zamanında da değiştirilebiliyor.
func (d *Daemon) applyClusterFromConfig() {
	cfg := d.Config()
	if !cfg.Cluster.Enabled {
		if d.cluster.Running() {
			d.log.Infof("cluster: yapılandırma gereği durduruluyor")
			d.cluster.Stop()
		}
		return
	}

	// Anahtar yoksa üret ve kalıcı hale getir. Anahtar olmadan hiçbir görev
	// kabul edilmez (bkz. cluster.authorizeTask).
	if strings.TrimSpace(cfg.Cluster.Secret) == "" {
		secret, err := randomSecret()
		if err != nil {
			d.log.Errorf("cluster: anahtar üretilemedi, cluster başlatılmıyor: %v", err)
			return
		}
		d.mu.Lock()
		d.cfg.Cluster.Secret = secret
		snapshot := d.cfg
		d.mu.Unlock()
		if err := store.SaveConfig(d.cfgPath, snapshot); err != nil {
			d.log.Errorf("cluster: anahtar kaydedilemedi: %v", err)
			return
		}
		d.log.Infof("cluster: yeni eşleştirme anahtarı üretildi ve kaydedildi")
		cfg = snapshot
	}

	d.cluster.SetSecret(cfg.Cluster.Secret)
	// Ad ÇALIŞIRKEN de uygulanır: eskiden yalnızca açılışta okunuyordu ve
	// sihirbazda verilen ad bir sonraki yeniden başlatmaya kadar eşlere
	// "mcos-1" olarak gidiyordu (bkz. cluster.SetNodeName).
	d.cluster.SetNodeName(cfg.Cluster.NodeName)
	if d.cluster.Running() {
		return
	}
	if err := d.cluster.Start(); err != nil {
		d.log.Warnf("cluster: başlatılamadı: %v", err)
		return
	}
	d.log.Infof("cluster: etkin (port %d) — görevler yalnızca eşleştirilmiş ve doğru anahtarı sunan eşlerden kabul edilir", cfg.Cluster.Port)
}

// randomSecret returns a 32-hex-character pre-shared cluster key.
func randomSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// backupScheduler, lastBackupTime ve pruneBackups backup_auto.go'ya taşındı
// (otomatik yedek planı saat/gün aralığı ve günün saatiyle genişledi).

// clusterWorkLoop periodically generates data-light optimization work
// (log analysis) for running cluster-share servers. When a paired helper is
// reachable, the cluster offloads it so the game-host's CPU stays free.
func (d *Daemon) clusterWorkLoop(ctx context.Context) {
	tk := time.NewTicker(2 * time.Minute)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			if !d.cluster.HasHelper() {
				continue
			}
			servers, err := d.store.ListServers()
			if err != nil {
				continue
			}
			for _, srv := range servers {
				if !srv.ClusterShare || d.servers.State(srv.ID) != model.StateRunning {
					continue
				}
				var sb strings.Builder
				for _, e := range d.servers.TailConsole(srv.ID, 200) {
					sb.WriteString(e.Message)
					sb.WriteByte('\n')
				}
				d.cluster.SubmitTask(model.Task{
					ID: generateTaskID(), Kind: model.TaskLogAnalysis, State: model.TaskQueued,
					ServerID: srv.ID, Params: map[string]string{"log": sb.String()}, CreatedAt: time.Now(),
				})
			}
		}
	}
}

// autostart launches servers flagged for automatic startup.
func (d *Daemon) autostart(ctx context.Context) {
	if !d.Config().AutostartServers {
		return
	}
	servers, err := d.store.ListServers()
	if err != nil {
		d.log.Errorf("daemon: autostart list: %v", err)
		return
	}
	// Kardeşler kendi başına açılmaz; bölünmüş ana sunucunun kopyaları ana
	// AÇILMADAN hazırlanır (ortak dünya kipi ve tohum ana sunucunun
	// server.properties'ine açılışta yazılıyor), ana açıldıktan sonra açılır.
	servers = model.HideSiblings(servers)
	pending := map[string][]*model.Server{}
	for _, s := range servers {
		if !s.Autostart || d.instanceCount(s) <= 1 {
			continue
		}
		sibs, err := d.ensureInstances(s)
		if err != nil {
			d.log.Warnf("instances: %s bölünemedi, tek sunucu açılıyor: %v", s.Name, err)
			continue
		}
		pending[s.ID] = sibs
	}
	d.servers.StartAutostart(ctx, servers)
	for _, sibs := range pending {
		go d.startSiblings(ctx, sibs)
	}
}
