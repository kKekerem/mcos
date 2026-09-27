// Package ipc implements the newline-delimited JSON-RPC 2.0 protocol spoken
// between the daemon (mcosd) and its front-ends (mcos-panel, mcos-panel-lite,
// mcosctl). It is deliberately transport-agnostic and language-neutral so the
// Rust lite panel can speak it with a few lines of code.
package ipc

import "encoding/json"

// Version is the JSON-RPC protocol version string.
const Version = "2.0"

// Request is a single JSON-RPC request. Params is left raw so each handler
// decodes its own typed parameters.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a single JSON-RPC response. Exactly one of Result/Error is set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Standard JSON-RPC error codes plus MCOS-specific ranges.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeNotFound       = -32000 // resource not found
	CodeConflict       = -32001 // e.g. port in use, name taken
	CodeUnavailable    = -32002 // feature disabled / dependency missing
)

// Method names. Grouped by subsystem; only a subset is implemented per
// milestone, but the full surface is declared here as the protocol contract.
const (
	MethodPing = "ping"

	// Uzaktan kontrol: telefon uygulamasinin bagli oldugu HTTPS koprusu.
	MethodRemoteStatus  = "remote.status"
	MethodRemoteEnable  = "remote.enable"
	MethodRemoteDisable = "remote.disable"
	MethodRemoteRotate  = "remote.rotate"

	// Ekran paylaşımı (RFB/VNC): RealVNC Viewer gibi istemcilerle bağlanma.
	MethodVNCStatus   = "vnc.status"
	MethodVNCEnable   = "vnc.enable"
	MethodVNCDisable  = "vnc.disable"
	MethodVNCRotate   = "vnc.rotate"   // parolayı yenile (bağlıları keser)
	MethodVNCViewOnly = "vnc.viewOnly" // girdiyi aç/kapat

	// SSH: kabuk erisimi.
	MethodSSHStatus   = "ssh.status"
	MethodSSHEnable   = "ssh.enable"
	MethodSSHDisable  = "ssh.disable"
	MethodSSHPassword = "ssh.password"
	MethodSSHAddKey   = "ssh.addKey"

	MethodSystemStatus  = "system.status"
	MethodSystemPower   = "system.power"   // poweroff | reboot
	MethodSystemTurbo   = "system.turbo"   // toggle global turbo mode
	MethodSystemDisks   = "system.disks"   // list block devices (persist targets)
	MethodSystemPersist = "system.persist" // make the booted USB persistent
	MethodTierGet       = "tier.get"
	MethodTierSet       = "tier.set"
	MethodConfigGet     = "config.get"
	MethodConfigSet     = "config.set"

	// Sistem güncellemesi: USB'deki yeni ISO ile, veri kaybı olmadan
	// (daemon/handlers_update.go, rootfs-overlay/usr/bin/mcos-update).
	MethodSystemUpdateScan   = "system.updateScan"   // USB'deki MCOS ISO'larını listele
	MethodSystemUpdate       = "system.update"       // güncellemeyi arka planda başlat
	MethodSystemUpdateStatus = "system.updateStatus" // güncellemenin ilerlemesi/sonucu

	MethodServerList    = "server.list"
	MethodServerGet     = "server.get"
	MethodServerCreate  = "server.create"
	MethodServerDelete  = "server.delete"
	MethodServerUpdate  = "server.update"
	MethodServerInstall = "server.install"
	MethodServerStart   = "server.start"
	MethodServerStop    = "server.stop"
	MethodServerRestart = "server.restart"
	MethodServerCommand = "server.command"
	MethodServerConsole = "server.console" // tail console via cursor

	MethodServerChangeVersion = "server.changeVersion" // re-install at a new version/software
	MethodServerVersions      = "server.versions"      // list selectable MC versions for a flavour

	MethodCatalogSearch  = "catalog.search"  // Modrinth mod/plugin search
	MethodCatalogInstall = "catalog.install" // one-click install into plugins/mods

	MethodServerScanUSBMods    = "server.scanUSBMods"    // scan USB drives for .jar files
	MethodServerInstallUSBMods = "server.installUSBMods" // copy selected USB .jar files to mods/plugins
	// USB'den sunucu klasörü aktarma (dünya, modlar, eklentiler, ayarlar).
	MethodServerScanUSBFolders = "server.scanUSBFolders"
	MethodServerImportUSB      = "server.importUSB"

	MethodJavaList     = "java.list"
	MethodJavaInstall  = "java.install"
	MethodJavaResolve  = "java.resolve" // mcVersion -> required major
	MethodJavaRemove   = "java.remove"
	MethodJavaDetect   = "java.detect"   // scan host for installed JDKs
	MethodJavaProgress = "java.progress" // live download progress map

	MethodBackupList    = "backup.list"
	MethodBackupCreate  = "backup.create"
	MethodBackupRestore = "backup.restore"
	MethodBackupDelete  = "backup.delete"

	MethodFilesList   = "files.list"
	MethodFilesRead   = "files.read"
	MethodFilesWrite  = "files.write"
	MethodFilesDelete = "files.delete"

	MethodPlayersList    = "players.list"
	MethodPlayersCommand = "players.command"

	MethodTunnelList    = "tunnel.list"
	MethodTunnelCreate  = "tunnel.create"
	MethodTunnelResolve = "tunnel.resolve" // 5-char code -> command
	MethodTunnelStart   = "tunnel.start"
	MethodTunnelStop    = "tunnel.stop"

	MethodWorldsList   = "worlds.list"
	MethodWorldsRename = "worlds.rename"
	MethodWorldsDelete = "worlds.delete"

	MethodClusterPeers = "cluster.peers"
	MethodClusterPair  = "cluster.pair"
	MethodClusterTasks = "cluster.tasks"

	// Etkin tarama ve elle eşleştirme.
	//
	// NEDEN GEREKLİ: pasif multicast keşfi ev modemlerinin çoğunda
	// (istemci yalıtımı) çalışmaz. Kullanıcı "otomatik ağda tarasın,
	// bulamazsak IP girelim" dedi; bu iki metot tam olarak odur.
	MethodClusterScan       = "cluster.scan"
	MethodClusterScanStart  = "cluster.scanStart"  // canlı tarama: başlat
	MethodClusterScanStatus = "cluster.scanStatus" // canlı tarama: yokla
	MethodClusterPairManual = "cluster.pairManual"
	MethodClusterSecret     = "cluster.secret"

	// Anahtarsız (kodla) eşleştirme: iki ekranda aynı 6 haneli kod, düğümde
	// "Kabul et", MCOS'ta onay. Bkz. internal/cluster/pairoffer.go.
	MethodClusterPairOffer   = "cluster.pairOffer"
	MethodClusterPairConfirm = "cluster.pairConfirm"
	MethodClusterPairCancel  = "cluster.pairCancel"

	// MCOS Link — birden çok PC'nin aynı dünyayı çalıştırması.
	MethodLinkStatus  = "link.status"
	MethodLinkEnable  = "link.enable"
	MethodLinkDisable = "link.disable"
	MethodLinkEvents  = "link.events"

	// playit tünel ajanı.
	MethodPlayitStatus  = "playit.status"
	MethodPlayitClaim   = "playit.claim"   // hesap bağlama akışını başlat
	MethodPlayitPoll    = "playit.poll"    // bağlama tamamlandı mı?
	MethodPlayitStart   = "playit.start"   // ajanı başlat
	MethodPlayitStop    = "playit.stop"    // ajanı durdur
	MethodPlayitInstall = "playit.install" // ikilileri kur/doğrula

	MethodNetWiFiScan  = "net.wifiScan"  // scan for nearby access points
	MethodNetWiFiApply = "net.wifiApply" // connect + persist a wifi network
	MethodNetWiredUp   = "net.wiredUp"   // bring up wired interfaces + DHCP

	// Canlı tarama: başlat + durumu yokla. net.wifiScan (bloklayan) uyumluluk
	// için duruyor; telefon uygulaması ve kurulum sihirbazı onu kullanıyor.
	MethodNetWiFiScanStart  = "net.wifiScanStart"  // non-blocking: start a scan
	MethodNetWiFiScanStatus = "net.wifiScanStatus" // poll partial results
)
