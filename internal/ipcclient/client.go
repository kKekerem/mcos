// Package ipcclient is a typed convenience wrapper over the mcosd JSON-RPC API.
//
// Neden ayri bir paket: iki farkli arayuz (Bubble Tea tabanli panel ve yeni
// framebuffer paneli) ayni cagrilari kullaniyor. Istemci panel paketinde
// kalsaydi, framebuffer ikilisi de Bubble Tea ve Lip Gloss'u baglamak zorunda
// kalirdi - initramfs'te bu bosuna megabaytlar demek.
package ipcclient

import (
	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/model"
)

// Client is a typed convenience wrapper over the JSON-RPC client.
type Client struct {
	endpoint string
	c        *ipc.Client
}

// Dial connects to mcosd at endpoint.
func Dial(endpoint string) (*Client, error) {
	c, err := ipc.DialClient(endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{endpoint: endpoint, c: c}, nil
}

// Reconnect re-establishes the connection (used after a drop).
func (cl *Client) Reconnect() error {
	c, err := ipc.DialClient(cl.endpoint)
	if err != nil {
		return err
	}
	cl.c = c
	return nil
}

func (cl *Client) Status() (*model.SystemStatus, error) {
	var st model.SystemStatus
	err := cl.c.Call(ipc.MethodSystemStatus, nil, &st)
	return &st, err
}

func (cl *Client) Servers() ([]*model.Server, error) {
	var res ipc.ServerListResult
	err := cl.c.Call(ipc.MethodServerList, nil, &res)
	return res.Servers, err
}

func (cl *Client) Server(id string) (*model.Server, error) {
	var res ipc.ServerResult
	err := cl.c.Call(ipc.MethodServerGet, ipc.IDParams{ID: id}, &res)
	return res.Server, err
}

func (cl *Client) Create(p ipc.ServerCreateParams) (*model.Server, error) {
	var res ipc.ServerResult
	err := cl.c.Call(ipc.MethodServerCreate, p, &res)
	return res.Server, err
}

// UpdateServer applies changes to an existing server's configuration.
func (cl *Client) UpdateServer(p ipc.ServerUpdateParams) (*model.Server, error) {
	var res ipc.ServerResult
	err := cl.c.Call(ipc.MethodServerUpdate, p, &res)
	return res.Server, err
}

func (cl *Client) action(method, id string) error {
	var res ipc.OKResult
	return cl.c.Call(method, ipc.IDParams{ID: id}, &res)
}

func (cl *Client) Install(id string) error { return cl.action(ipc.MethodServerInstall, id) }
func (cl *Client) Start(id string) error   { return cl.action(ipc.MethodServerStart, id) }
func (cl *Client) Stop(id string) error    { return cl.action(ipc.MethodServerStop, id) }
func (cl *Client) Restart(id string) error { return cl.action(ipc.MethodServerRestart, id) }
func (cl *Client) Delete(id string) error  { return cl.action(ipc.MethodServerDelete, id) }

func (cl *Client) Command(id, cmd string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodServerCommand, ipc.CommandParams{ID: id, Command: cmd}, &res)
}

func (cl *Client) ChangeVersion(id, mcVersion string, software model.Software) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodServerChangeVersion, ipc.ServerChangeVersionParams{ID: id, MCVersion: mcVersion, Software: software}, &res)
}

// ServerVersions lists selectable Minecraft versions for a flavour (live from
// Mojang, with an offline fallback baked into the daemon).
func (cl *Client) ServerVersions(software model.Software) (ipc.ServerVersionsResult, error) {
	var res ipc.ServerVersionsResult
	err := cl.c.Call(ipc.MethodServerVersions, ipc.ServerVersionsParams{Software: software}, &res)
	return res, err
}

// ── Network (Wi-Fi) ───────────────────────────────────────────────────────

func (cl *Client) WiFiScan() ([]ipc.WiFiNetwork, error) {
	var res ipc.WiFiScanResult
	err := cl.c.Call(ipc.MethodNetWiFiScan, nil, &res)
	return res.Networks, err
}

func (cl *Client) WiFiApply(ssid, pass string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodNetWiFiApply, ipc.WiFiApplyParams{SSID: ssid, Password: pass}, &res)
}

// WiredUp brings up wired interfaces and requests DHCP (daemon-side, best-effort).
func (cl *Client) WiredUp() error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodNetWiredUp, nil, &res)
}

// ── Catalog (mods / plugins) ──────────────────────────────────────────────

func (cl *Client) CatalogSearch(serverID, query string) ([]ipc.CatalogItem, error) {
	var res ipc.CatalogSearchResult
	err := cl.c.Call(ipc.MethodCatalogSearch, ipc.CatalogSearchParams{ServerID: serverID, Query: query}, &res)
	return res.Items, err
}

func (cl *Client) CatalogInstall(serverID, slug string) (string, error) {
	var res ipc.OKResult
	err := cl.c.Call(ipc.MethodCatalogInstall, ipc.CatalogInstallParams{ServerID: serverID, Slug: slug}, &res)
	return res.Message, err
}

func (cl *Client) ScanUSBMods() ([]model.USBJar, error) {
	var res ipc.USBScanResult
	err := cl.c.Call(ipc.MethodServerScanUSBMods, nil, &res)
	return res.Items, err
}

func (cl *Client) InstallUSBMods(serverID string, items []model.USBJar) (string, error) {
	var res ipc.OKResult
	err := cl.c.Call(ipc.MethodServerInstallUSBMods,
		ipc.USBInstallParams{ServerID: serverID, Items: items}, &res)
	return res.Message, err
}

func (cl *Client) Console(id string, cursor int64) (ipc.ConsoleResult, error) {
	var res ipc.ConsoleResult
	err := cl.c.Call(ipc.MethodServerConsole, ipc.ConsoleParams{ID: id, Cursor: cursor}, &res)
	return res, err
}

func (cl *Client) JavaList() ([]model.JavaRuntime, error) {
	var res ipc.JavaListResult
	err := cl.c.Call(ipc.MethodJavaList, nil, &res)
	return res.Runtimes, err
}

func (cl *Client) JavaInstall(major int) (model.JavaRuntime, error) {
	var res ipc.JavaRuntimeResult
	err := cl.c.Call(ipc.MethodJavaInstall, ipc.JavaInstallParams{Major: major}, &res)
	return res.Runtime, err
}

func (cl *Client) JavaDetect() ([]model.JavaRuntime, error) {
	var res ipc.JavaListResult
	err := cl.c.Call(ipc.MethodJavaDetect, nil, &res)
	return res.Runtimes, err
}

func (cl *Client) JavaProgress() (map[int]java.DownloadProgress, error) {
	var res ipc.JavaProgressResult
	err := cl.c.Call(ipc.MethodJavaProgress, nil, &res)
	return res.Progresses, err
}

func (cl *Client) Config() (*model.Config, error) {
	var res ipc.ConfigResult
	err := cl.c.Call(ipc.MethodConfigGet, nil, &res)
	return res.Config, err
}

func (cl *Client) UpdateConfig(cfg *model.Config) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodConfigSet, cfg, &res)
}

// ── System: power / turbo / persistence ───────────────────────────────────

// Power requests a poweroff or reboot. The daemon defers it briefly so this
// reply reaches the panel before the box goes down.
func (cl *Client) Power(action string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodSystemPower, ipc.PowerParams{Action: action}, &res)
}

// Turbo toggles the global turbo mode; returns the new state.
func (cl *Client) Turbo(enabled bool) (bool, error) {
	var res ipc.TurboResult
	err := cl.c.Call(ipc.MethodSystemTurbo, ipc.TurboParams{Enabled: enabled}, &res)
	return res.Enabled, err
}

// Disks lists candidate block devices for the "make USB persistent" flow.
func (cl *Client) Disks() ([]ipc.DiskTarget, error) {
	var res ipc.DisksResult
	err := cl.c.Call(ipc.MethodSystemDisks, nil, &res)
	return res.Disks, err
}

// Persist makes the booted USB (or the given device) persistent by adding an
// MCOS-DATA partition. Returns a human-readable result message.
func (cl *Client) Persist(device string) (string, error) {
	var res ipc.OKResult
	err := cl.c.Call(ipc.MethodSystemPersist, ipc.PersistParams{Device: device}, &res)
	return res.Message, err
}

// ── Backup ──────────────────────────────────────────────────────────────

func (cl *Client) BackupList(serverID string) ([]model.Backup, error) {
	var res ipc.BackupListResult
	err := cl.c.Call(ipc.MethodBackupList, ipc.IDParams{ID: serverID}, &res)
	return res.Backups, err
}

func (cl *Client) BackupCreate(p ipc.BackupCreateParams) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodBackupCreate, p, &res)
}

func (cl *Client) BackupRestore(serverID, backupID string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodBackupRestore, ipc.BackupRestoreParams{ServerID: serverID, BackupID: backupID}, &res)
}

func (cl *Client) BackupDelete(serverID, backupID string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodBackupDelete, ipc.BackupDeleteParams{ServerID: serverID, BackupID: backupID}, &res)
}

// ── Files ─────────────────────────────────────────────────────────────────

func (cl *Client) FilesList(serverID, path string) ([]model.FileEntry, error) {
	var res ipc.FilesListResult
	err := cl.c.Call(ipc.MethodFilesList, ipc.FilesListParams{ServerID: serverID, Path: path}, &res)
	return res.Entries, err
}

func (cl *Client) FilesRead(serverID, path string) (ipc.FilesReadResult, error) {
	var res ipc.FilesReadResult
	err := cl.c.Call(ipc.MethodFilesRead, ipc.FilesReadParams{ServerID: serverID, Path: path}, &res)
	return res, err
}

func (cl *Client) FilesWrite(serverID, path, contentBase64 string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodFilesWrite, ipc.FilesWriteParams{ServerID: serverID, Path: path, ContentBase64: contentBase64}, &res)
}

func (cl *Client) FilesDelete(serverID, path string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodFilesDelete, ipc.FilesDeleteParams{ServerID: serverID, Path: path}, &res)
}

// ── Players ─────────────────────────────────────────────────────────────

func (cl *Client) PlayersList(serverID string) (ipc.PlayersListResult, error) {
	var res ipc.PlayersListResult
	err := cl.c.Call(ipc.MethodPlayersList, ipc.IDParams{ID: serverID}, &res)
	return res, err
}

func (cl *Client) PlayersCommand(serverID, command string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodPlayersCommand, ipc.PlayersCommandParams{ServerID: serverID, Command: command}, &res)
}

// ── Worlds ───────────────────────────────────────────────────────────────

func (cl *Client) WorldsList(serverID string) ([]model.World, error) {
	var res ipc.WorldsListResult
	err := cl.c.Call(ipc.MethodWorldsList, ipc.IDParams{ID: serverID}, &res)
	return res.Worlds, err
}

// ── Cluster ──────────────────────────────────────────────────────────────

func (cl *Client) ClusterPeers() ([]model.Peer, error) {
	var res ipc.ClusterPeersResult
	err := cl.c.Call(ipc.MethodClusterPeers, nil, &res)
	return res.Peers, err
}

func (cl *Client) ClusterPair(id string, unpair bool) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodClusterPair, ipc.ClusterPairParams{ID: id, Unpair: unpair}, &res)
}

func (cl *Client) ClusterTasks() ([]model.Task, error) {
	var res ipc.ClusterTasksResult
	err := cl.c.Call(ipc.MethodClusterTasks, nil, &res)
	return res.Tasks, err
}

// ── Tunnel / WAN ────────────────────────────────────────────────

func (cl *Client) TunnelList() ([]model.TunnelStatus, error) {
	var res ipc.TunnelListResult
	err := cl.c.Call(ipc.MethodTunnelList, nil, &res)
	return res.Tunnels, err
}

func (cl *Client) TunnelCompress(command string) (string, error) {
	var res ipc.TunnelCompressResult
	err := cl.c.Call(ipc.MethodTunnelCreate, ipc.TunnelCompressParams{Command: command}, &res)
	return res.Code, err
}

func (cl *Client) TunnelResolve(code string) (string, error) {
	var res ipc.TunnelResolveResult
	err := cl.c.Call(ipc.MethodTunnelResolve, ipc.TunnelResolveParams{Code: code}, &res)
	return res.Command, err
}

func (cl *Client) TunnelStart(code string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodTunnelStart, ipc.TunnelStatusParams{Code: code}, &res)
}

func (cl *Client) TunnelStop(code string) error {
	var res ipc.OKResult
	return cl.c.Call(ipc.MethodTunnelStop, ipc.TunnelStatusParams{Code: code}, &res)
}
