package model

import (
	"mcos/internal/version"
	"strings"
)

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

// UIConfig holds interface behaviour the user can change from Settings.
//
// NEDEN YAPILANDIRMADA: bunlar zevk meselesi değil, DONANIM meselesidir.
// Dizüstünde touchpad iyi çalışır; masaüstünde touchpad yoktur ve "touchpad"
// ayarını görmek kafa karıştırır. Zayıf bir makinede geçiş animasyonları
// akıcı olmayabilir. Kullanıcı her birini ayrı ayrı kapatabilmeli.
type UIConfig struct {
	// Mouse enables pointer support for relative devices (mice, trackballs).
	Mouse bool `json:"mouse"`
	// Touchpad enables pointer support for absolute devices (laptop pads).
	//
	// Fareden AYRI: bazı kullanıcılar yazarken avuç içi teması yüzünden
	// touchpad'i kapatıp yalnızca fare kullanmak ister.
	Touchpad bool `json:"touchpad"`
	// TapToClick turns a short touchpad tap into a left click.
	TapToClick bool `json:"tapToClick"`
	// PointerSpeed scales pointer motion. 100 = normal, 50 = yarı hız.
	PointerSpeed int `json:"pointerSpeed"`
	// Animations enables screen transitions and the boot intro.
	Animations bool `json:"animations"`
	// BootAnimation shows the animated splash before the panel appears.
	BootAnimation bool `json:"bootAnimation"`
}

// DefaultUI returns the interface defaults.
func DefaultUI() UIConfig {
	return UIConfig{
		Mouse:         true,
		Touchpad:      true,
		TapToClick:    true,
		PointerSpeed:  100,
		Animations:    true,
		BootAnimation: true,
	}
}

// Normalize clamps out-of-range values loaded from an old or hand-edited file.
//
// Sıfır bir hız DEĞİLDİR, imleci dondurur; elle düzenlenmiş veya eski
// sürümden gelen bir yapılandırma dosyası fareyi tamamen bozabilirdi.
func (u UIConfig) Normalize() UIConfig {
	if u.PointerSpeed <= 0 {
		u.PointerSpeed = 100
	}
	if u.PointerSpeed < 20 {
		u.PointerSpeed = 20
	}
	if u.PointerSpeed > 300 {
		u.PointerSpeed = 300
	}
	return u
}

// SecurityConfig holds the OPTIONAL panel password.
//
// Kullanıcının isteği: "isteğe bağlı şifre olsun". İsteğe bağlı olması
// kritik: MCOS çoğu zaman evdeki bir makinede, klavyesiz bir köşede durur.
// Zorunlu bir parola, kullanıcıyı kendi sunucusundan kilitler.
//
// Parolanın KENDİSİ saklanmaz; yalnızca PBKDF2-SHA256 özeti ve tuzu. Bu
// dosya /data altındadır ve USB'den okunabilir — düz metin parola, aynı
// parolayı başka yerde kullanan kullanıcıyı da riske atardı.
type SecurityConfig struct {
	// PasswordHash is the hex PBKDF2-SHA256 digest. Boş = parola yok.
	PasswordHash string `json:"passwordHash,omitempty"`
	// PasswordSalt is the hex random salt.
	PasswordSalt string `json:"passwordSalt,omitempty"`
	// Iterations is the PBKDF2 round count actually used for this hash.
	//
	// Özetle BİRLİKTE saklanır: ileride tur sayısını artırdığımızda eski
	// parolalar doğrulanabilir kalmalı.
	Iterations int `json:"passwordIterations,omitempty"`
	// LockOnSleep asks for the password again when waking from sleep.
	LockOnSleep bool `json:"lockOnSleep,omitempty"`
}

// PasswordSet reports whether a panel password is configured.
func (s SecurityConfig) PasswordSet() bool {
	return s.PasswordHash != "" && s.PasswordSalt != ""
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

	// UI holds pointer and animation preferences (Ayarlar ekranı).
	UI UIConfig `json:"ui"`
	// Security holds the optional panel password.
	Security SecurityConfig `json:"security,omitempty"`

	// Remote, telefon/masaüstü uygulamasının bağlandığı HTTPS köprüsü.
	Remote RemoteConfig `json:"remote,omitempty"`
	// SSH, kabuk erişimi.
	SSH SSHConfig `json:"ssh,omitempty"`

	// Turbo is a single global "use everything" switch. When true the daemon
	// ignores the resource budget, launches servers at high priority across all
	// cores, and uses the aggressive "turbo" JVM profile. Off by default.
	Turbo bool `json:"turbo,omitempty"`

	// First-boot / identity fields.
	SetupComplete bool `json:"setupComplete"`

	// Hostname, kurulum sihirbazında girilen PC adıdır.
	//
	// NEDEN EKLENDİ: sihirbaz bu adı ZORUNLU tutuyor ve özet ekranında
	// gösteriyordu ama HİÇBİR YERE yazmıyordu — modelde böyle bir alan yoktu.
	// Kullanıcı her kurulumda yeniden giriyor, her seferinde kayboluyordu.
	Hostname string `json:"hostname,omitempty"`

	NodeID       string `json:"nodeId,omitempty"`
	HardwareID   string `json:"hardwareId,omitempty"` // MAC fingerprint set at setup; mismatch ⇒ re-run setup
	WiFiSSID     string `json:"wifiSsid,omitempty"`
	WiFiPassword string `json:"wifiPassword,omitempty"`
}

// RemoteConfig controls the HTTPS bridge the phone app talks to.
//
// ── Neden varsayılan KAPALI ─────────────────────────────────────────────────
// Bu köprü sunucunun TAM DENETİMİNİ ağa açar: sunucu silme, dosya yazma,
// makineyi kapatma. Kullanıcı açıkça istemeden açılmamalı. Açıldığında
// panel jetonu ve sertifika parmak izini gösterir.
type RemoteConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// Port, dinlenen TCP portu. 0 ise varsayılan (2223) kullanılır.
	Port int `json:"port,omitempty"`
	// Token, her istekte beklenen gizli değer.
	//
	// DİKKAT: bu gerçek bir sırdır ve config.json içinde düz durur. Dosya
	// yalnızca kök tarafından okunabilir (0600). Paneldeki "jetonu yenile"
	// eylemi bunu değiştirir ve eski telefonların erişimini keser.
	Token string `json:"token,omitempty"`
}

// Normalize fills in defaults without changing a deliberate choice.
func (r RemoteConfig) Normalize() RemoteConfig {
	if r.Port <= 0 || r.Port > 65535 {
		r.Port = DefaultRemotePort
	}
	return r
}

// DefaultRemotePort is where the remote bridge listens.
//
// 2223: eşleştirme portunun (2222) hemen yanı; kullanıcı ikisini birlikte
// hatırlar ve ikisi de ayrıcalıklı aralığın dışındadır.
const DefaultRemotePort = 2223

// SSHConfig controls shell access to the appliance.
//
// ── Neden parola burada DEĞİL ───────────────────────────────────────────────
// Parola /etc/shadow'a yazılır, yapılandırmaya değil. config.json yedeklenip
// kopyalanabilen bir dosya; içine giriş parolası koymak, o yedeği alan
// herkese kabuk erişimi vermek olurdu. Burada yalnızca "parola kuruldu mu"
// bilgisi durur.
type SSHConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	Port    int  `json:"port,omitempty"`
	// PasswordSet, /etc/shadow'da bir parola olup olmadığını yansıtır.
	PasswordSet bool `json:"passwordSet,omitempty"`
	// AuthorizedKeys, parolasız giriş için açık anahtarlar.
	AuthorizedKeys []string `json:"authorizedKeys,omitempty"`
}

// Normalize fills in defaults.
func (c SSHConfig) Normalize() SSHConfig {
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = DefaultSSHPort
	}
	// Boş satırları at: kullanıcı anahtarı yapıştırırken fazladan satır
	// kalabilir ve boş bir satır dropbear tarafında hata üretir.
	var keys []string
	for _, k := range c.AuthorizedKeys {
		if t := strings.TrimSpace(k); t != "" {
			keys = append(keys, t)
		}
	}
	c.AuthorizedKeys = keys
	return c
}

// DefaultSSHPort is the standard SSH port.
const DefaultSSHPort = 22

// DefaultConfig returns a config populated with sane first-boot defaults.
func DefaultConfig() *Config {
	return &Config{
		Version:          version.Version,
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
			Port:      PairingPort,
			Role:      "auto",
		},
		UI:  DefaultUI(),
		WAN: WANGlobal{Autostart: false},
		Features: Features{
			GPUMonitor:        FeatureAuto,
			AdvancedAnalytics: FeatureAuto,
			FullPerformance:   FeatureAuto,
		},
	}
}
