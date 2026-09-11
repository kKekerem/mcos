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
	"mcos/internal/store"
	"mcos/internal/supervisor"
	"mcos/internal/tunnel"
	"mcos/internal/worlds"
)

// Version is the MCOS release string surfaced in status/ping.
const Version = "0.1.0"

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

	mu        sync.RWMutex
	cfg       *model.Config
	startedAt time.Time
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
	// Cluster needs an Executor that runs real work via the daemon's
	// subsystems, so it is wired after d exists.
	d.cluster = cluster.NewManager(cfg.Cluster, Version, st, lg, taskExecutor{d})
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
	s.Handle(ipc.MethodPing, d.handlePing)
	s.Handle(ipc.MethodSystemStatus, d.handleSystemStatus)
	s.Handle(ipc.MethodSystemPower, d.handleSystemPower)
	s.Handle(ipc.MethodSystemTurbo, d.handleSystemTurbo)
	s.Handle(ipc.MethodSystemDisks, d.handleSystemDisks)
	s.Handle(ipc.MethodSystemPersist, d.handleSystemPersist)
	s.Handle(ipc.MethodConfigGet, d.handleConfigGet)
	s.Handle(ipc.MethodConfigSet, d.handleConfigSet)

	s.Handle(ipc.MethodServerList, d.handleServerList)
	s.Handle(ipc.MethodServerGet, d.handleServerGet)
	s.Handle(ipc.MethodServerCreate, d.handleServerCreate)
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
	s.Handle(ipc.MethodClusterTasks, d.handleClusterTasks)

	s.Handle(ipc.MethodServerVersions, d.handleServerVersions)
	s.Handle(ipc.MethodNetWiFiScan, d.handleNetWiFiScan)
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
	d.autostart(ctx)
	go d.backupScheduler(ctx)
	go d.clusterWorkLoop(ctx)
	<-ctx.Done()
	d.log.Infof("daemon: shutting down, stopping all servers")
	d.cluster.Stop()
	d.sup.StopAll()
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

// backupScheduler runs auto-backups on each server's configured interval and
// enforces the retention count. Schedule is a Go duration string ("30m", "6h",
// "24h"); empty disables it. Backups are submitted as cluster tasks so they go
// through the same executor path (and stay local — they need the server data).
func (d *Daemon) backupScheduler(ctx context.Context) {
	tk := time.NewTicker(1 * time.Minute)
	defer tk.Stop()
	// last, yalnızca bu süreçte alınan yedekleri izler. Zamanlama kararı DİSKTEN
	// okunan en son yedek zamanına dayanır (lastBackupTime): eskiden bu harita
	// tek gerçek kaynaktı ve her daemon yeniden başlatmasında sıfırlanıyordu;
	// "seed" dalı bir aralık daha beklettiği için sık yeniden başlayan bir
	// cihazda otomatik yedek HİÇ alınmıyordu.
	last := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tk.C:
			servers, err := d.store.ListServers()
			if err != nil {
				continue
			}
			for _, srv := range servers {
				if !srv.Backup.Auto || strings.TrimSpace(srv.Backup.Schedule) == "" {
					continue
				}
				every, perr := time.ParseDuration(strings.TrimSpace(srv.Backup.Schedule))
				if perr != nil || every <= 0 {
					continue
				}

				ref, ok := last[srv.ID]
				if !ok {
					// Süreç içi kayıt yok — diskteki en son yedeğe bak.
					ref, ok = d.lastBackupTime(srv.ID)
				}
				if ok && now.Sub(ref) < every {
					continue
				}
				if !ok {
					// Hiç yedek yok: hemen bir tane al, böylece ilk yedek için
					// bir tam aralık beklenmez.
					d.log.Infof("backup: %s için ilk otomatik yedek alınıyor", srv.ID)
				}

				last[srv.ID] = now
				d.cluster.SubmitTask(model.Task{
					ID: generateTaskID(), Kind: model.TaskBackup, State: model.TaskQueued,
					ServerID: srv.ID, Params: map[string]string{"name": ""}, CreatedAt: now,
				})
				// NOT: budama artık BURADA yapılmıyor. Görev henüz kuyruğa
				// alındı, yedek üretilmedi; hemen budamak bir tur gecikmeli
				// çalışıyordu. Budama, yedek gerçekten oluştuktan sonra
				// aşağıdaki turda yapılır.
			}

			// Retention'ı her turda uygula: bu noktada önceki turların yedekleri
			// diskte hazırdır.
			for _, srv := range servers {
				if srv.Backup.Keep > 0 {
					d.pruneBackups(srv)
				}
			}
		}
	}
}

// lastBackupTime returns the creation time of the newest backup on disk.
// Daemon yeniden başlatmalarına dayanıklı zamanlama için tek gerçek kaynak.
func (d *Daemon) lastBackupTime(serverID string) (time.Time, bool) {
	list, err := d.backup.List(serverID) // en yeni ilk
	if err != nil || len(list) == 0 {
		return time.Time{}, false
	}
	return list[0].CreatedAt, true
}

// pruneBackups deletes the oldest backups beyond the server's Keep count.
func (d *Daemon) pruneBackups(srv *model.Server) {
	if srv.Backup.Keep <= 0 {
		return
	}
	list, err := d.backup.List(srv.ID) // newest-first
	if err != nil {
		return
	}
	for i := srv.Backup.Keep; i < len(list); i++ {
		_ = d.backup.Delete(srv.ID, list[i].ID)
	}
}

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
	d.servers.StartAutostart(ctx, servers)
}
