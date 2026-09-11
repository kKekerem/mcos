package model

// Tier classifies host capability and drives adaptive feature toggling.
type Tier string

const (
	TierLow    Tier = "low"
	TierMedium Tier = "medium"
	TierHigh   Tier = "high"
)

// FeatureMode is a tri-state toggle: auto (decided by tier), on, or off.
type FeatureMode string

const (
	FeatureAuto FeatureMode = "auto"
	FeatureOn   FeatureMode = "on"
	FeatureOff  FeatureMode = "off"
)

// TierConfig controls capability detection and manual override.
type TierConfig struct {
	Mode     string `json:"mode"`               // "auto" or a fixed Tier value
	Override string `json:"override,omitempty"` // forced Tier, empty = none
}

// ClusterConfig controls LAN peer discovery and work sharing.
type ClusterConfig struct {
	Enabled   bool   `json:"enabled"`
	NodeName  string `json:"nodeName"`
	Discovery string `json:"discovery"` // "mdns+udp"
	Port      int    `json:"port"`      // peer protocol TCP port
	Role      string `json:"role"`      // "auto" | "game-host" | "helper"

	// Secret is the pre-shared key that authorises task offloading between
	// nodes. İlk kullanımda üretilir ve config.json'a yazılır.
	//
	// Bu alan OLMADAN eşleştirme protokolünde hiçbir kimlik doğrulaması yoktu:
	// LAN'daki herkes eşleştirme portuna bağlanıp "assignTask" gönderebiliyor,
	// daemon da işi doğrudan çalıştırıyordu. İki MCOS cihazının birlikte
	// çalışması için bu anahtarın ikisinde de aynı olması gerekir.
	Secret string `json:"secret,omitempty"`
}

// WANGlobal holds daemon-wide tunnel settings.
type WANGlobal struct {
	Autostart bool   `json:"autostart"`
	BinPath   string `json:"binPath,omitempty"`
}

// Features is the manual override surface for adaptive behaviors. Each entry is
// "auto" unless the user pins it on/off.
type Features struct {
	GPUMonitor        FeatureMode `json:"gpuMonitor"`
	AdvancedAnalytics FeatureMode `json:"advancedAnalytics"`
	FullPerformance   FeatureMode `json:"fullPerformance"`
}

// ResourceBudget caps how much of this machine the server processes may use.
// Configured in the first-boot wizard's "resource budget" step. Zero means
// unlimited (the host decides).
type ResourceBudget struct {
	MaxServerRAMMB      int `json:"maxServerRamMB,omitempty"`      // hard cap on a single server's -Xmx
	MaxServerCPUPercent int `json:"maxServerCpuPercent,omitempty"` // cap on CPU quota (percent of one core *100)
}

// Config is the global daemon configuration, persisted as
// <config-root>/config.json.
type Config struct {
	Version          string         `json:"version"`
	Theme            string         `json:"theme"`
	Tier             TierConfig     `json:"tier"`
	AutostartServers bool           `json:"autostartServers"`
	Cluster          ClusterConfig  `json:"cluster"`
	WAN              WANGlobal      `json:"wan"`
	Features         Features       `json:"features"`
	Budget           ResourceBudget `json:"budget"`
	Timezone         string         `json:"timezone,omitempty"` // IANA tz, e.g. "Europe/Istanbul"

	// Turbo is a single global "use everything" switch. When true the daemon
	// ignores the resource budget, launches servers at high priority across all
	// cores, and uses the aggressive "turbo" JVM profile. Off by default.
	Turbo bool `json:"turbo,omitempty"`

	// First-boot / identity fields.
	SetupComplete bool   `json:"setupComplete"`
	NodeID        string `json:"nodeId,omitempty"`
	HardwareID    string `json:"hardwareId,omitempty"` // MAC fingerprint set at setup; mismatch ⇒ re-run setup
	WiFiSSID      string `json:"wifiSsid,omitempty"`
	WiFiPassword  string `json:"wifiPassword,omitempty"`
}

// DefaultConfig returns a config populated with sane first-boot defaults.
func DefaultConfig() *Config {
	return &Config{
		Version:          "0.1.0",
		Theme:            "graphite-teal",
		Tier:             TierConfig{Mode: "auto"},
		AutostartServers: true,
		Cluster: ClusterConfig{
			// KAPALI varsayılan: eşleştirme sunucusu ağa açık bir TCP portu
			// dinler, bu yüzden kullanıcı OOBE'de açıkça istemediyse
			// başlatılmaz. Eskiden bu alan hiç okunmuyordu ve cluster her zaman
			// çalışıyordu.
			Enabled:   false,
			NodeName:  "mcos-1",
			Discovery: "mdns+udp",
			Port:      27890,
			Role:      "auto",
		},
		WAN: WANGlobal{Autostart: false},
		Features: Features{
			GPUMonitor:        FeatureAuto,
			AdvancedAnalytics: FeatureAuto,
			FullPerformance:   FeatureAuto,
		},
	}
}
