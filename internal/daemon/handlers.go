package daemon

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/model"
	"mcos/internal/portmgr"
	"mcos/internal/store"
	"mcos/internal/sysmon"
	"mcos/internal/tier"
	"mcos/internal/timesync"
)

// decode unmarshals raw JSON params into v. An empty payload is treated as nil.
func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func validateCreate(p *ipc.ServerCreateParams) error {
	if strings.TrimSpace(p.Name) == "" {
		return &ipc.Error{Code: ipc.CodeInvalidParams, Message: "name is required"}
	}
	if !p.Software.Valid() {
		return &ipc.Error{Code: ipc.CodeInvalidParams, Message: fmt.Sprintf("unknown software %q", p.Software)}
	}
	if strings.TrimSpace(p.MCVersion) == "" {
		return &ipc.Error{Code: ipc.CodeInvalidParams, Message: "mcVersion is required"}
	}
	if p.RAMMB <= 0 {
		return &ipc.Error{Code: ipc.CodeInvalidParams, Message: "ramMB must be > 0"}
	}
	return nil
}

func (d *Daemon) handlePing(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.PingResult{Pong: true, Version: Version}, nil
}

func (d *Daemon) handleSystemStatus(_ context.Context, _ json.RawMessage) (any, error) {
	cpu := sysmon.CPU()
	mem := sysmon.Memory()
	net := sysmon.Net()
	disks := sysmon.Disks()
	gpus := sysmon.GPUs()

	cfg := d.Config()
	decision := tier.Decide(cfg, mem, cpu)
	javas, _ := d.java.List()

	st := &model.SystemStatus{
		SystemName:     cfg.Cluster.NodeName,
		Version:        Version,
		Uptime:         int64(time.Since(d.startedAt).Seconds()),
		Tier:           decision.Effective,
		DetectedTier:   decision.Detected,
		Panel:          decision.Panel,
		PollIntervalMS: decision.PollIntervalMS,
		CPU:            cpu,
		Memory:         mem,
		Disks:          disks,
		GPUs:           gpus,
		Net:            net,
		JavaVersions:   javaMajors(javas),
		WAN:            "stopped",
		ClusterOn:      cfg.Cluster.Enabled,
	}

	servers, _ := d.store.ListServers()
	up := 0
	for _, srv := range servers {
		if d.servers.State(srv.ID) == model.StateRunning {
			up++
		}
	}
	st.ServersTotal = len(servers)
	st.ServersUp = up

	st.ClockSynced = timesync.Synced()
	st.TurboOn = cfg.Turbo
	// Turbo forces the effective tier to HIGH: full polling + every adaptive
	// feature on, so the box throws all of itself at the running servers.
	if cfg.Turbo {
		st.Tier = model.TierHigh
	}

	tier.Apply(cfg, st, 0)
	return st, nil
}

func javaMajors(rts []model.JavaRuntime) []int {
	out := make([]int, 0, len(rts))
	for _, rt := range rts {
		out = append(out, rt.Major)
	}
	return out
}

func (d *Daemon) handleConfigGet(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.ConfigResult{Config: d.Config()}, nil
}

func (d *Daemon) handleConfigSet(_ context.Context, raw json.RawMessage) (any, error) {
	var cfg model.Config
	if err := decode(raw, &cfg); err != nil {
		return nil, err
	}
	if err := store.SaveConfig(d.cfgPath, &cfg); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cfg = &cfg
	d.mu.Unlock()
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleServerList(_ context.Context, _ json.RawMessage) (any, error) {
	servers, err := d.store.ListServers()
	if err != nil {
		return nil, err
	}
	for _, srv := range servers {
		d.servers.FillRuntime(srv)
	}
	return ipc.ServerListResult{Servers: servers}, nil
}

func (d *Daemon) handleServerGet(_ context.Context, raw json.RawMessage) (any, error) {
	srv, err := d.loadServer(raw)
	if err != nil {
		return nil, err
	}
	d.servers.FillRuntime(srv)
	return ipc.ServerResult{Server: srv}, nil
}

// loadServer decodes an IDParams and loads the server, mapping not-found to an
// RPC error.
func (d *Daemon) loadServer(raw json.RawMessage) (*model.Server, error) {
	var p ipc.IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "id is required"}
	}
	srv, err := d.store.GetServer(p.ID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
		}
		return nil, err
	}
	return srv, nil
}

func (d *Daemon) handleServerCreate(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ServerCreateParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := validateCreate(&p); err != nil {
		return nil, err
	}
	existing, _ := d.store.ListServers()
	for _, s := range existing {
		if strings.EqualFold(s.Name, p.Name) {
			return nil, &ipc.Error{Code: ipc.CodeConflict, Message: "name already in use"}
		}
	}

	javaMajor := p.JavaMajor
	if javaMajor == 0 {
		javaMajor = java.RequiredJavaMajor(p.MCVersion)
	}

	used := portmgr.UsedPorts(existing, "")
	port := p.Port
	if port == 0 {
		var err error
		port, err = portmgr.FindFree(portmgr.DefaultPort, used)
		if err != nil {
			return nil, err
		}
	} else if used[port] {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: fmt.Sprintf("port %d already used by another server", port)}
	}

	priority := p.Priority
	if priority == "" {
		priority = model.PriorityNormal
	}

	// Gameplay defaults: empty string fields fall back to Minecraft's own
	// defaults; OnlineMode/PVP default true unless the caller pinned them false.
	gamemode := strings.TrimSpace(p.Gamemode)
	if gamemode == "" {
		gamemode = "survival"
	}
	difficulty := strings.TrimSpace(p.Difficulty)
	if difficulty == "" {
		difficulty = "easy"
	}
	maxPlayers := p.MaxPlayers
	if maxPlayers <= 0 {
		maxPlayers = 20
	}
	onlineMode := true
	if p.OnlineMode != nil {
		onlineMode = *p.OnlineMode
	}
	pvp := true
	if p.PVP != nil {
		pvp = *p.PVP
	}

	// Honor the host's resource budget (set in the first-boot wizard).
	ramMB, cpuQuota := d.clampToBudget(p.RAMMB, p.CPUQuota)
	if ramMB != p.RAMMB {
		d.log.Infof("daemon: clamping %q RAM %d→%d MB (budget)", p.Name, p.RAMMB, ramMB)
	}

	id := generateID()
	srv := &model.Server{
		ID:               id,
		Name:             strings.TrimSpace(p.Name),
		Description:      p.Description,
		Software:         p.Software,
		MCVersion:        p.MCVersion,
		JavaMajor:        javaMajor,
		RAMMB:            ramMB,
		Port:             port,
		CPUQuota:         cpuQuota,
		CPUAffinity:      p.CPUAffinity,
		Priority:         priority,
		Autostart:        p.Autostart,
		RestartOnCrash:   true,
		SupportsPlugins:  p.Software.SupportsPlugins(),
		SupportsMods:     p.Software.SupportsMods(),
		Backup:           backupPolicyFor(p.AutoBackup),
		WAN:              model.WANConfig{Enabled: p.WAN},
		JVMFlags:         p.JVMFlags,
		AllowOldVersions: p.AllowOldVersions,
		ViewDistance:     p.ViewDistance,
		SimDistance:      p.SimDistance,
		MaxPlayers:       maxPlayers,
		MOTD:             p.MOTD,
		Gamemode:         gamemode,
		Difficulty:       difficulty,
		OnlineMode:       onlineMode,
		PVP:              pvp,
		Hardcore:         p.Hardcore,
		Whitelist:        p.Whitelist,
		ClusterShare:     p.ClusterShare,
		DataDir:          strings.TrimSpace(p.DataDir),
	}
	if srv.JVMFlags == "" {
		srv.JVMFlags = "aikar"
	}
	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}
	d.log.Infof("daemon: created server %s (%s %s, java %d, port %d)", srv.Name, srv.Software, srv.MCVersion, srv.JavaMajor, srv.Port)

	// Download + prepare the software right away so the server is ready to
	// start. Runs in the background (downloads can be large); a later Start
	// will reuse the result, and EnsureInstalled is safe against a concurrent
	// install triggered by the user pressing Start.
	go func(s *model.Server) {
		if err := d.servers.EnsureInstalled(context.Background(), s); err != nil {
			d.log.Errorf("daemon: auto-install %q failed: %v", s.Name, err)
		}
	}(srv.Clone())

	return ipc.ServerResult{Server: srv}, nil
}

func (d *Daemon) handleServerUpdate(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ServerUpdateParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "id is required"}
	}
	srv, err := d.store.GetServer(p.ID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
		}
		return nil, err
	}

	if p.RAMMB > 0 {
		srv.RAMMB = p.RAMMB
	}
	if p.CPUQuota > 0 {
		srv.CPUQuota = p.CPUQuota
	}
	if p.ViewDistance > 0 {
		srv.ViewDistance = p.ViewDistance
	}
	if p.SimDistance > 0 {
		srv.SimDistance = p.SimDistance
	}
	if p.MaxPlayers > 0 {
		srv.MaxPlayers = p.MaxPlayers
	}
	if p.FullPerf != nil {
		srv.FullPerf = *p.FullPerf
	}
	if p.JVMFlags != "" {
		srv.JVMFlags = p.JVMFlags
	}
	if p.Autostart != nil {
		srv.Autostart = *p.Autostart
	}
	if p.WAN != nil {
		srv.WAN.Enabled = *p.WAN
	}

	if err := d.store.SaveServer(srv); err != nil {
		return nil, err
	}
	d.servers.FillRuntime(srv)
	return ipc.ServerResult{Server: srv}, nil
}

func (d *Daemon) handleServerDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if d.servers.State(p.ID) == model.StateRunning {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: "stop the server before deleting"}
	}
	if err := d.store.DeleteServer(p.ID); err == store.ErrNotFound {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
	} else if err != nil {
		return nil, err
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleServerInstall(ctx context.Context, raw json.RawMessage) (any, error) {
	srv, err := d.loadServer(raw)
	if err != nil {
		return nil, err
	}
	if err := d.servers.Install(ctx, srv); err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}
	return ipc.OKResult{OK: true, Message: "installed"}, nil
}

func (d *Daemon) handleServerStart(ctx context.Context, raw json.RawMessage) (any, error) {
	srv, err := d.loadServer(raw)
	if err != nil {
		return nil, err
	}
	// Apply the host resource budget at launch (covers servers created before a
	// budget change). Clamp in-memory only — the manifest keeps the user's value.
	srv.RAMMB, srv.CPUQuota = d.clampToBudget(srv.RAMMB, srv.CPUQuota)

	// Ensure the required Java version is installed.
	rts, _ := d.java.List()
	installed := false
	for _, rt := range rts {
		if rt.Major == srv.JavaMajor {
			installed = true
			break
		}
	}
	if !installed {
		d.log.Infof("daemon: java %d missing for %s, installing...", srv.JavaMajor, srv.Name)
		if _, err := d.java.Install(srv.JavaMajor); err != nil {
			return nil, &ipc.Error{Code: ipc.CodeConflict, Message: fmt.Sprintf("java %d indirme hatası: %v", srv.JavaMajor, err)}
		}
	}
	// Turbo mode boosts this launch (in-memory only, not persisted): high
	// scheduling priority via FullPerf, no CPU quota cap, and the aggressive
	// "turbo" JVM profile.
	if d.Config().Turbo {
		srv.FullPerf = true
		srv.CPUQuota = 0
		srv.JVMFlags = java.ProfileTurbo
	}
	if err := d.servers.Start(ctx, srv); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}

	// Deep integration: auto-open a Serveo tunnel when the server enables it.
	if srv.WAN.Enabled {
		go func() {
			cmd := srv.WAN.Code
			if cmd == "" {
				cmd = serveoCommand(srv.Port)
			}
			d.tunnelMgr.Start(cmd)
		}()
	}

	return ipc.OKResult{OK: true, Message: "starting"}, nil
}

// serveoCommand builds the SSH reverse-tunnel command that exposes a local TCP
// port through serveo.net. Uses the ssh client shipped in the image.
func serveoCommand(port int) string {
	return fmt.Sprintf("ssh -N -T -o StrictHostKeyChecking=no -o ServerAliveInterval=30 "+
		"-o ExitOnForwardFailure=yes -R 0:localhost:%d serveo.net", port)
}

func (d *Daemon) handleServerStop(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.servers.Stop(p.ID); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}

	// Stop any quick tunnel associated with this server port or code.
	// Since we don't have the server instance here easily, let's load it to get the port/code.
	srv, _ := d.store.GetServer(p.ID)
	if srv != nil && srv.WAN.Enabled {
		cmd := srv.WAN.Code
		if cmd == "" {
			cmd = serveoCommand(srv.Port)
		}
		_ = d.tunnelMgr.Stop(cmd)
	}

	return ipc.OKResult{OK: true, Message: "stopping"}, nil
}

func (d *Daemon) handleServerRestart(ctx context.Context, raw json.RawMessage) (any, error) {
	srv, err := d.loadServer(raw)
	if err != nil {
		return nil, err
	}
	if err := d.servers.Restart(ctx, srv); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true, Message: "restarting"}, nil
}

func (d *Daemon) handleServerConsole(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ConsoleParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	entries, cursor := d.servers.Console(p.ID, p.Cursor)
	lines := make([]ipc.ConsoleLine, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, ipc.ConsoleLine{TimeMS: e.Time.UnixMilli(), Text: e.Message})
	}
	return ipc.ConsoleResult{Lines: lines, Cursor: cursor}, nil
}

func (d *Daemon) handleServerCommand(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.CommandParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Command) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "command is empty"}
	}
	if err := d.servers.Command(p.ID, p.Command); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}

func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "srv_" + hex.EncodeToString(b)
}

// clampToBudget caps a server's RAM and CPU quota to the host-wide resource
// budget configured in the first-boot wizard. Zero budget = unlimited. Turbo
// mode bypasses the budget entirely (use everything).
func (d *Daemon) clampToBudget(ramMB, cpuQuota int) (int, int) {
	cfg := d.Config()
	if cfg.Turbo {
		return ramMB, cpuQuota
	}
	if b := cfg.Budget.MaxServerRAMMB; b > 0 && ramMB > b {
		ramMB = b
	}
	if b := cfg.Budget.MaxServerCPUPercent; b > 0 && (cpuQuota == 0 || cpuQuota > b) {
		cpuQuota = b
	}
	return ramMB, cpuQuota
}

// backupPolicyFor returns a sensible auto-backup policy: when enabled it backs
// up every 6 hours and keeps the 5 most recent snapshots.
func backupPolicyFor(auto bool) model.BackupPolicy {
	if !auto {
		return model.BackupPolicy{}
	}
	return model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}
}

// ── Backup ────────────────────────────────────────────────────────────

func (d *Daemon) handleBackupList(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	list, err := d.backup.List(p.ID)
	if err != nil {
		return nil, err
	}
	return ipc.BackupListResult{Backups: list}, nil
}

func (d *Daemon) handleBackupCreate(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.BackupCreateParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	b, err := d.backup.Create(p.ServerID, p.Name, p.Description, p.WorldOnly)
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return b, nil
}

func (d *Daemon) handleBackupRestore(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.BackupRestoreParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.backup.Restore(p.ServerID, p.BackupID); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true, Message: "restored"}, nil
}

func (d *Daemon) handleBackupDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.BackupDeleteParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.backup.Delete(p.ServerID, p.BackupID); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}

// ── Files ───────────────────────────────────────────────────────────────

func (d *Daemon) handleFilesList(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.FilesListParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	entries, err := d.files.List(p.ServerID, p.Path)
	if err != nil {
		return nil, err
	}
	return ipc.FilesListResult{Entries: entries}, nil
}

func (d *Daemon) handleFilesRead(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.FilesReadParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	content, size, err := d.files.Read(p.ServerID, p.Path)
	if err != nil {
		return nil, err
	}
	return ipc.FilesReadResult{ContentBase64: content, Size: size}, nil
}

func (d *Daemon) handleFilesWrite(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.FilesWriteParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.files.Write(p.ServerID, p.Path, p.ContentBase64); err != nil {
		return nil, err
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleFilesDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.FilesDeleteParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.files.Delete(p.ServerID, p.Path); err != nil {
		return nil, err
	}
	return ipc.OKResult{OK: true}, nil
}

// ── Players ─────────────────────────────────────────────────────────────

func (d *Daemon) handlePlayersList(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	result, err := d.players.List(p.ID)
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return result, nil
}

func (d *Daemon) handlePlayersCommand(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.PlayersCommandParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Command) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "command is empty"}
	}
	if err := d.players.Command(p.ServerID, p.Command); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}

// ── Worlds ──────────────────────────────────────────────────────────────

func (d *Daemon) handleWorldsList(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	list, err := d.worlds.List(p.ID)
	if err != nil {
		return nil, err
	}
	return ipc.WorldsListResult{Worlds: list}, nil
}

func (d *Daemon) handleWorldsRename(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.WorldsRenameParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.worlds.Rename(p.ServerID, p.OldName, p.NewName); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleWorldsDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.WorldsDeleteParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.worlds.Delete(p.ServerID, p.Name); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}

// ── Cluster ─────────────────────────────────────────────────────────────

func (d *Daemon) handleClusterPeers(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.ClusterPeersResult{Peers: d.cluster.Peers()}, nil
}

func (d *Daemon) handleClusterPair(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ClusterPairParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Unpair {
		d.cluster.Unpair(p.ID)
	} else {
		d.cluster.Pair(p.ID)
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleClusterTasks(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.ClusterTasksResult{Tasks: d.cluster.Tasks()}, nil
}

// ── Tunnel / Serveo ────────────────────────────────────────────────

func (d *Daemon) handleTunnelList(_ context.Context, _ json.RawMessage) (any, error) {
	statuses := d.tunnelMgr.List()
	var out []model.TunnelStatus
	for _, s := range statuses {
		out = append(out, model.TunnelStatus{
			Running:   s.Running,
			Command:   s.Command,
			Code:      s.Code,
			Hostname:  s.Hostname,
			PID:       s.PID,
			StartedAt: s.StartedAt,
			Log:       s.Log,
			AutoStart: s.AutoStart,
		})
	}
	return ipc.TunnelListResult{Tunnels: out}, nil
}

func (d *Daemon) handleTunnelCreate(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.TunnelCompressParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	code := d.tunnelMgr.Compress(p.Command)
	if code == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "empty command"}
	}
	return ipc.TunnelCompressResult{Code: code}, nil
}

func (d *Daemon) handleTunnelResolve(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.TunnelResolveParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	cmd, ok := d.tunnelMgr.Resolve(p.Code)
	if !ok {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "code not found"}
	}
	return ipc.TunnelResolveResult{Command: cmd}, nil
}

func (d *Daemon) handleTunnelStart(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.TunnelStatusParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.tunnelMgr.Start(p.Code); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleTunnelStop(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.TunnelStatusParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.tunnelMgr.Stop(p.Code); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeConflict, Message: err.Error()}
	}
	return ipc.OKResult{OK: true}, nil
}
