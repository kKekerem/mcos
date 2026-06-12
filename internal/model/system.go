package model

// CPUInfo describes the host processor.
type CPUInfo struct {
	Model    string  `json:"model"`
	Cores    int     `json:"cores"`   // physical cores
	Threads  int     `json:"threads"` // logical processors
	UsagePct float64 `json:"usagePct"`
	MHz      int     `json:"mhz,omitempty"`
	TempC    float64 `json:"tempC,omitempty"`
}

// MemInfo describes system memory in bytes.
type MemInfo struct {
	TotalBytes     uint64  `json:"totalBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	UsagePct       float64 `json:"usagePct"`
}

// DiskInfo describes a single mounted filesystem.
type DiskInfo struct {
	Mount      string  `json:"mount"`
	Filesystem string  `json:"filesystem,omitempty"`
	TotalBytes uint64  `json:"totalBytes"`
	UsedBytes  uint64  `json:"usedBytes"`
	FreeBytes  uint64  `json:"freeBytes"`
	UsagePct   float64 `json:"usagePct"`
}

// GPUInfo describes a detected graphics adapter (optional).
type GPUInfo struct {
	Vendor   string  `json:"vendor"`
	Model    string  `json:"model"`
	MemMB    int     `json:"memMB,omitempty"`
	UsagePct float64 `json:"usagePct,omitempty"`
	TempC    float64 `json:"tempC,omitempty"`
}

// NICInfo describes a network interface.
type NICInfo struct {
	Name    string `json:"name"`
	MAC     string `json:"mac,omitempty"`
	IPv4    string `json:"ipv4,omitempty"`
	Up      bool   `json:"up"`
	Kind    string `json:"kind,omitempty"`   // "wired" | "wireless" | "other"
	Driver  string `json:"driver,omitempty"` // kernel driver bound to the device
	Link    bool   `json:"link,omitempty"`   // carrier present (cable in / associated)
	RxBytes uint64 `json:"rxBytes,omitempty"`
	TxBytes uint64 `json:"txBytes,omitempty"`
}

// NetStatus aggregates network state.
type NetStatus struct {
	NICs     []NICInfo `json:"nics"`
	LocalIP  string    `json:"localIP"`
	Internet bool      `json:"internet"`
	Hostname string    `json:"hostname"`
}

// SystemStatus is the snapshot shown on the main dashboard. It is computed by
// the daemon on request (system.status), never persisted.
type SystemStatus struct {
	SystemName     string     `json:"systemName"`
	Version        string     `json:"version"`
	Uptime         int64      `json:"uptimeSec"`
	Tier           Tier       `json:"tier"`
	DetectedTier   Tier       `json:"detectedTier,omitempty"`
	Panel          string     `json:"panel,omitempty"`
	PollIntervalMS int        `json:"pollIntervalMs,omitempty"`
	CPU            CPUInfo    `json:"cpu"`
	Memory         MemInfo    `json:"memory"`
	Disks          []DiskInfo `json:"disks"`
	GPUs           []GPUInfo  `json:"gpus"`
	Net            NetStatus  `json:"net"`
	JavaVersions   []int      `json:"javaVersions"` // installed Java majors
	ServersTotal   int        `json:"serversTotal"`
	ServersUp      int        `json:"serversUp"`
	WAN            string     `json:"wan"` // "active" | "stopped" | "disabled"
	PeersOnline    int        `json:"peersOnline"`
	ActiveTasks    int        `json:"activeTasks"`

	// Effective adaptive flags after applying tier + manual overrides.
	GPUMonitorOn        bool `json:"gpuMonitorOn"`
	AdvancedAnalyticsOn bool `json:"advancedAnalyticsOn"`
	FullPerformanceOn   bool `json:"fullPerformanceOn"`
	ClusterOn           bool `json:"clusterOn"`
	WANOn               bool `json:"wanOn"`

	// ClockSynced is true once the daemon has set the system clock from NTP
	// (RTC-less PCs boot with a wrong date → TLS fails until this flips). The
	// OOBE waits on it before downloading Java.
	ClockSynced bool `json:"clockSynced"`
	// TurboOn mirrors config.Turbo: when on, servers run unclamped with high
	// priority + an aggressive JVM profile.
	TurboOn bool `json:"turboOn"`
}
