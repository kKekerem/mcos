// Package model defines the core data types shared across mcosd, the panels,
// and the CLI. These types are the on-disk schema (JSON manifests) as well as
// the wire schema for the IPC protocol, so they must stay stable and explicit.
package model

import "time"

// SchemaVersion is bumped when the on-disk JSON layout changes incompatibly.
const SchemaVersion = 1

// Software identifies a Minecraft server software flavor.
type Software string

const (
	SoftwareVanilla     Software = "vanilla"
	SoftwarePaper       Software = "paper"
	SoftwarePurpur      Software = "purpur"
	SoftwareSpigot      Software = "spigot"
	SoftwareCraftBukkit Software = "craftbukkit"
	SoftwareFabric      Software = "fabric"
	SoftwareForge       Software = "forge"
	SoftwareNeoForge    Software = "neoforge"
	SoftwareFolia       Software = "folia"
	SoftwareQuilt       Software = "quilt"
)

// AllSoftware lists every supported server flavor in display order.
var AllSoftware = []Software{
	SoftwareVanilla, SoftwarePaper, SoftwarePurpur, SoftwareSpigot,
	SoftwareCraftBukkit, SoftwareFabric, SoftwareForge, SoftwareNeoForge,
	SoftwareFolia, SoftwareQuilt,
}

// SupportsPlugins reports whether the flavor loads Bukkit-style plugins.
func (s Software) SupportsPlugins() bool {
	switch s {
	case SoftwarePaper, SoftwarePurpur, SoftwareSpigot, SoftwareCraftBukkit, SoftwareFolia:
		return true
	default:
		return false
	}
}

// SupportsMods reports whether the flavor loads a modloader's mods.
func (s Software) SupportsMods() bool {
	switch s {
	case SoftwareFabric, SoftwareForge, SoftwareNeoForge, SoftwareQuilt:
		return true
	default:
		return false
	}
}

// ModrinthLoader maps a flavour to the Modrinth "loader" facet used for mod /
// plugin searches and installs.
func (s Software) ModrinthLoader() string {
	switch s {
	case SoftwarePaper:
		return "paper"
	case SoftwarePurpur:
		return "purpur"
	case SoftwareSpigot:
		return "spigot"
	case SoftwareCraftBukkit:
		return "bukkit"
	case SoftwareFolia:
		return "folia"
	case SoftwareFabric:
		return "fabric"
	case SoftwareForge:
		return "forge"
	case SoftwareNeoForge:
		return "neoforge"
	case SoftwareQuilt:
		return "quilt"
	}
	return ""
}

// Valid reports whether s is a known software flavor.
func (s Software) Valid() bool {
	for _, k := range AllSoftware {
		if k == s {
			return true
		}
	}
	return false
}

// ServerState is the lifecycle state of a managed Minecraft server.
type ServerState string

const (
	StateStopped  ServerState = "stopped"
	StateStarting ServerState = "starting"
	StateRunning  ServerState = "running"
	StateStopping ServerState = "stopping"
	StateError    ServerState = "error"
)

// Priority controls OS-level scheduling for the server process.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

// BackupPolicy describes automatic backup behavior for a server.
type BackupPolicy struct {
	Auto     bool   `json:"auto"`
	Schedule string `json:"schedule"` // cron expression, empty = disabled
	Keep     int    `json:"keep"`     // number of backups to retain (0 = unlimited)
}

// WANConfig holds per-server tunnel settings (Serveo).
type WANConfig struct {
	Enabled  bool   `json:"enabled"`
	TunnelID string `json:"tunnelId,omitempty"`
	Code     string `json:"code,omitempty"`     // 5-char share code
	Hostname string `json:"hostname,omitempty"` // external address
}

// Server is a single managed Minecraft server. It is persisted as
// <data-root>/servers/<id>/manifest.json.
type Server struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	Software         Software `json:"software"`
	MCVersion        string   `json:"mcVersion"`
	JavaMajor        int      `json:"javaMajor"`
	JavaPath         string   `json:"javaPath,omitempty"` // manual override, empty = auto
	AllowOldVersions bool     `json:"allowOldVersions"`

	RAMMB       int      `json:"ramMB"`
	Port        int      `json:"port"`
	CPUQuota    int      `json:"cpuQuota,omitempty"` // percent of one core *100, 0 = unlimited
	CPUAffinity []int    `json:"cpuAffinity,omitempty"`
	Priority    Priority `json:"priority"`
	FullPerf    bool     `json:"fullPerf"`
	JVMFlags    string   `json:"jvmFlags"` // profile name: "aikar" | "default" | custom

	// Gameplay properties written into server.properties on install. These are
	// configured (persisted) values — distinct from the runtime player count.
	ViewDistance int    `json:"viewDistance,omitempty"`
	SimDistance  int    `json:"simDistance,omitempty"`
	MaxPlayers   int    `json:"maxPlayers,omitempty"`
	MOTD         string `json:"motd,omitempty"`
	Gamemode     string `json:"gamemode,omitempty"`   // survival|creative|adventure|spectator
	Difficulty   string `json:"difficulty,omitempty"` // peaceful|easy|normal|hard
	OnlineMode   bool   `json:"onlineMode"`           // verify players against Mojang auth
	PVP          bool   `json:"pvp"`                  // player-versus-player combat
	Hardcore     bool   `json:"hardcore,omitempty"`
	Whitelist    bool   `json:"whitelist,omitempty"`

	// LevelSeed pins the world seed.
	//
	// ORTAK DÜNYA İÇİN ŞART: iki düğüm aynı tohumu kullanmazsa aynı araziyi
	// üretmez ve sınırı geçen oyuncu bambaşka bir dünyaya düşer. Tek makinede
	// de yararlı: kullanıcı sevdiği bir dünyayı yeniden kurabilir.
	LevelSeed string `json:"levelSeed,omitempty"`

	// Link holds the shared-world (MCOS Link) setup for this server.
	Link LinkConfig `json:"link,omitempty"`

	// ClusterShare opts this server's heavy side-work (backups, log analysis)
	// into LAN work-sharing when a paired helper node is available.
	ClusterShare bool `json:"clusterShare,omitempty"`
	// DataDir overrides where the server's data lives (empty = default tree).
	DataDir string `json:"dataDir,omitempty"`

	Autostart       bool `json:"autostart"`
	RestartOnCrash  bool `json:"restartOnCrash"`
	SupportsPlugins bool `json:"supportsPlugins"`
	SupportsMods    bool `json:"supportsMods"`

	Backup BackupPolicy `json:"backup"`
	WAN    WANConfig    `json:"wan"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Runtime fields — not persisted, filled by the daemon on demand.
	State     ServerState `json:"state"`
	PID       int         `json:"pid,omitempty"`
	Players   int         `json:"players,omitempty"`
	LastLog   string      `json:"lastLog,omitempty"`
	UptimeSec int64       `json:"uptimeSec,omitempty"`
}

// Clone returns a deep-ish copy safe to mutate without touching the original's
// slices/maps. Time and scalar fields are value-copied.
func (s *Server) Clone() *Server {
	cp := *s
	if s.CPUAffinity != nil {
		cp.CPUAffinity = append([]int(nil), s.CPUAffinity...)
	}
	return &cp
}

// Backup describes a single server backup (full data snapshot).
type Backup struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"serverId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	SizeBytes   int64     `json:"sizeBytes"`
	Type        string    `json:"type"` // manual | auto | restore-point
	WorldOnly   bool      `json:"worldOnly,omitempty"`
}

// FileEntry describes a single file or directory inside a server data tree.
type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"isDir"`
	ModTime time.Time `json:"modTime,omitempty"`
	Mode    string    `json:"mode,omitempty"` // e.g. "0644"
}

// PlayerInfo is a single online/offline player record.
type PlayerInfo struct {
	Name   string `json:"name"`
	Online bool   `json:"online"`
	IP     string `json:"ip,omitempty"`
	Ping   int    `json:"ping,omitempty"`
}

// World describes a discovered Minecraft world folder.
type World struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

// TunnelStatus mirrors tunnel manager state for IPC.
type TunnelStatus struct {
	Running   bool      `json:"running"`
	Command   string    `json:"command,omitempty"`
	Code      string    `json:"code,omitempty"`
	Hostname  string    `json:"hostname,omitempty"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Log       []string  `json:"log,omitempty"`
	AutoStart bool      `json:"autoStart,omitempty"`
}
