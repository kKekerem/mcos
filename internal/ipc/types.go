package ipc

import (
	"mcos/internal/java"
	"mcos/internal/model"
)

// This file declares the typed parameter/result payloads for each RPC method.
// The Go front-ends import these directly; the Rust lite panel mirrors the same
// JSON shapes. Keeping them in one place makes the protocol the single contract.

// PingResult is returned by the ping method.
type PingResult struct {
	Pong    bool   `json:"pong"`
	Version string `json:"version"`
}

// IDParams carries a single resource id (server, backup, tunnel, ...).
type IDParams struct {
	ID string `json:"id"`
}

// ServerListResult lists servers with their current runtime state.
type ServerListResult struct {
	Servers []*model.Server `json:"servers"`
}

// ServerResult wraps a single server.
type ServerResult struct {
	Server *model.Server `json:"server"`
}

// ServerCreateParams is the install-wizard payload for a new server.
type ServerCreateParams struct {
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Software         model.Software `json:"software"`
	MCVersion        string         `json:"mcVersion"`
	JavaMajor        int            `json:"javaMajor,omitempty"` // 0 = auto-resolve
	RAMMB            int            `json:"ramMB"`
	Port             int            `json:"port,omitempty"` // 0 = auto-pick free
	CPUQuota         int            `json:"cpuQuota,omitempty"`
	CPUAffinity      []int          `json:"cpuAffinity,omitempty"`
	Priority         model.Priority `json:"priority,omitempty"`
	Autostart        bool           `json:"autostart"`
	AutoBackup       bool           `json:"autoBackup"`
	AllowOldVersions bool           `json:"allowOldVersions"`
	WAN              bool           `json:"wan"`
	JVMFlags         string         `json:"jvmFlags,omitempty"`

	// Gameplay + placement, plumbed from the install wizard.
	ViewDistance int    `json:"viewDistance,omitempty"`
	SimDistance  int    `json:"simDistance,omitempty"`
	MaxPlayers   int    `json:"maxPlayers,omitempty"`
	MOTD         string `json:"motd,omitempty"`
	Gamemode     string `json:"gamemode,omitempty"`
	Difficulty   string `json:"difficulty,omitempty"`
	// OnlineMode and PVP default to true (Minecraft's own default) when the
	// caller omits them, so they are pointers to distinguish "unset" from false.
	OnlineMode   *bool  `json:"onlineMode,omitempty"`
	PVP          *bool  `json:"pvp,omitempty"`
	Hardcore     bool   `json:"hardcore,omitempty"`
	Whitelist    bool   `json:"whitelist,omitempty"`
	ClusterShare bool   `json:"clusterShare,omitempty"`
	DataDir      string `json:"dataDir,omitempty"`
}

type ServerUpdateParams struct {
	ID           string         `json:"id"`
	RAMMB        int            `json:"ramMB,omitempty"`
	CPUQuota     int            `json:"cpuQuota,omitempty"`
	ViewDistance int            `json:"viewDistance,omitempty"`
	SimDistance  int            `json:"simDistance,omitempty"`
	MaxPlayers   int            `json:"maxPlayers,omitempty"`
	FullPerf     *bool          `json:"fullPerf,omitempty"`
	JVMFlags     string         `json:"jvmFlags,omitempty"`
	Autostart    *bool          `json:"autostart,omitempty"`
	WAN          *bool          `json:"wan,omitempty"`
}

// ServerChangeVersionParams re-installs a server at a new version/software,
// backing up first. Software empty = keep current flavour.
type ServerChangeVersionParams struct {
	ID        string         `json:"id"`
	MCVersion string         `json:"mcVersion"`
	Software  model.Software `json:"software,omitempty"`
}

// CatalogSearchParams searches the mod/plugin catalog for a server (the server's
// flavour and version constrain the results).
type CatalogSearchParams struct {
	ServerID string `json:"serverId"`
	Query    string `json:"query"`
}

// CatalogItem is one catalog search hit.
type CatalogItem struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int    `json:"downloads"`
	Type        string `json:"type"`
}

// CatalogSearchResult lists catalog hits.
type CatalogSearchResult struct {
	Items []CatalogItem `json:"items"`
}

// CatalogInstallParams installs a catalog project into a server.
type CatalogInstallParams struct {
	ServerID string `json:"serverId"`
	Slug     string `json:"slug"`
}

// ServerVersionsParams asks for available Minecraft versions for a flavour.
// Software empty = vanilla/release list.
type ServerVersionsParams struct {
	Software model.Software `json:"software,omitempty"`
}

// ServerVersionsResult lists selectable versions (newest first) plus the
// convenience "latest" pointers.
type ServerVersionsResult struct {
	Versions       []string `json:"versions"`
	Latest         string   `json:"latest,omitempty"`
	LatestSnapshot string   `json:"latestSnapshot,omitempty"`
}

// WiFiNetwork is one scanned access point.
type WiFiNetwork struct {
	SSID    string `json:"ssid"`
	Signal  int    `json:"signal"` // percent 0-100
	Secured bool   `json:"secured"`
}

// WiFiScanResult lists nearby networks (best signal first).
type WiFiScanResult struct {
	Networks []WiFiNetwork `json:"networks"`
}

// WiFiApplyParams connects to a network and persists it to the config.
type WiFiApplyParams struct {
	SSID     string `json:"ssid"`
	Password string `json:"password,omitempty"`
}

// ConsoleParams requests console lines for a server since a cursor.
type ConsoleParams struct {
	ID     string `json:"id"`
	Cursor int64  `json:"cursor"`
}

// ConsoleLine is one captured output line with a timestamp (unix ms).
type ConsoleLine struct {
	TimeMS int64  `json:"t"`
	Text   string `json:"text"`
}

// ConsoleResult returns new lines plus the advanced cursor.
type ConsoleResult struct {
	Lines  []ConsoleLine `json:"lines"`
	Cursor int64         `json:"cursor"`
}

// CommandParams sends a raw console command to a running server.
type CommandParams struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

// OKResult is a generic acknowledgement.
type OKResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// ConfigResult wraps the global config.
type ConfigResult struct {
	Config *model.Config `json:"config"`
}

// ── System: power / turbo / persist ───────────────────────────────────────

// PowerParams selects a power action: "poweroff" or "reboot".
type PowerParams struct {
	Action string `json:"action"`
}

// TurboParams toggles the global turbo mode.
type TurboParams struct {
	Enabled bool `json:"enabled"`
}

// TurboResult reports the new turbo state after a toggle.
type TurboResult struct {
	Enabled bool `json:"enabled"`
}

// DiskTarget is one candidate block device for the "make USB persistent" flow.
type DiskTarget struct {
	Device      string `json:"device"`      // e.g. /dev/sda
	Model       string `json:"model"`       // e.g. "SanDisk Ultra"
	SizeBytes   uint64 `json:"sizeBytes"`   // total device size
	Removable   bool   `json:"removable"`   // USB / removable media
	IsBootDisk  bool   `json:"isBootDisk"`  // the device MCOS booted from
	HasPersist  bool   `json:"hasPersist"`  // already carries an MCOS-DATA partition
	FreeBytes   uint64 `json:"freeBytes"`   // unallocated space available
}

// DisksResult lists candidate devices for persistence.
type DisksResult struct {
	Disks []DiskTarget `json:"disks"`
}

// PersistParams requests making a device persistent. Empty Device = the booted
// USB (the common case).
type PersistParams struct {
	Device string `json:"device,omitempty"`
}

// JavaListResult lists installed Java runtimes.
type JavaListResult struct {
	Runtimes []model.JavaRuntime `json:"runtimes"`
}

// JavaResolveParams asks which Java major a Minecraft version needs.
type JavaResolveParams struct {
	MCVersion string `json:"mcVersion"`
}

// JavaResolveResult is the required major and whether it is installed.
type JavaResolveResult struct {
	Major     int  `json:"major"`
	Installed bool `json:"installed"`
}

// JavaInstallParams requests installation of a specific Java major version.
type JavaInstallParams struct {
	Major int `json:"major"`
}

// JavaRuntimeResult wraps a single installed runtime.
type JavaRuntimeResult struct {
	Runtime model.JavaRuntime `json:"runtime"`
}

// JavaProgressResult carries a map of active and finished Java download progress objects.
type JavaProgressResult struct {
	Progresses map[int]java.DownloadProgress `json:"progresses"`
}

// ── Backup ──────────────────────────────────────────────────────────────

// BackupListResult lists backups for a server.
type BackupListResult struct {
	Backups []model.Backup `json:"backups"`
}

// BackupCreateParams requests a new backup.
type BackupCreateParams struct {
	ServerID    string `json:"serverId"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	WorldOnly   bool   `json:"worldOnly,omitempty"`
}

// BackupRestoreParams selects a backup to restore.
type BackupRestoreParams struct {
	ServerID string `json:"serverId"`
	BackupID string `json:"backupId"`
}

// BackupDeleteParams selects a backup to delete.
type BackupDeleteParams struct {
	ServerID string `json:"serverId"`
	BackupID string `json:"backupId"`
}

// ── Files ────────────────────────────────────────────────────────────────

// FilesListParams requests a directory listing inside a server data tree.
type FilesListParams struct {
	ServerID string `json:"serverId"`
	Path     string `json:"path"` // relative to server data root
}

// FilesListResult returns file entries.
type FilesListResult struct {
	Entries []model.FileEntry `json:"entries"`
}

// FilesReadParams requests a file's content.
type FilesReadParams struct {
	ServerID string `json:"serverId"`
	Path     string `json:"path"`
}

// FilesReadResult returns base64-encoded file content.
type FilesReadResult struct {
	ContentBase64 string `json:"contentBase64"`
	Size          int64  `json:"size"`
}

// FilesWriteParams writes a file (base64 content) into the server tree.
type FilesWriteParams struct {
	ServerID      string `json:"serverId"`
	Path          string `json:"path"`
	ContentBase64 string `json:"contentBase64"`
}

// FilesDeleteParams removes a file or empty directory.
type FilesDeleteParams struct {
	ServerID string `json:"serverId"`
	Path     string `json:"path"`
}

// ── Players ─────────────────────────────────────────────────────────────

// PlayersListResult returns parsed online player data.
type PlayersListResult struct {
	Players []model.PlayerInfo `json:"players"`
	Max     int                `json:"max"`
	Online  int                `json:"online"`
}

// PlayersCommandParams sends a players-related command through the server console.
type PlayersCommandParams struct {
	ServerID string `json:"serverId"`
	Command  string `json:"command"` // e.g. "kick Steve", "op Alice"
}

// WorldsListResult returns the worlds for a server.
type WorldsListResult struct {
	Worlds []model.World `json:"worlds"`
}

// WorldsRenameParams renames a world directory.
type WorldsRenameParams struct {
	ServerID string `json:"serverId"`
	OldName  string `json:"oldName"`
	NewName  string `json:"newName"`
}

// WorldsDeleteParams removes a world directory.
type WorldsDeleteParams struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
}

// ── Cluster ────────────────────────────────────────────────────────────

// ClusterPeersResult returns the known peer list.
type ClusterPeersResult struct {
	Peers []model.Peer `json:"peers"`
}

// ClusterPairParams pairs or unpairs a peer.
type ClusterPairParams struct {
	ID     string `json:"id"`
	Unpair bool   `json:"unpair,omitempty"`
}

// ClusterTasksResult returns the local task queue.
type ClusterTasksResult struct {
	Tasks []model.Task `json:"tasks"`
}

// ── Tunnel ───────────────────────────────────────────────────────────────

// TunnelCompressParams carries the raw tunnel command.
type TunnelCompressParams struct {
	Command string `json:"command"`
}

// TunnelCompressResult returns the 5-char short code.
type TunnelCompressResult struct {
	Code string `json:"code"`
}

// TunnelResolveParams carries a 5-char code.
type TunnelResolveParams struct {
	Code string `json:"code"`
}

// TunnelResolveResult returns the original command.
type TunnelResolveResult struct {
	Command string `json:"command"`
}

// TunnelListResult returns all known tunnel statuses.
type TunnelListResult struct {
	Tunnels []model.TunnelStatus `json:"tunnels"`
}

// TunnelStatusParams identifies a tunnel by code.
type TunnelStatusParams struct {
	Code string `json:"code"`
}

// TunnelStatusResult returns a single tunnel status.
type TunnelStatusResult struct {
	Status *model.TunnelStatus `json:"status"`
}
