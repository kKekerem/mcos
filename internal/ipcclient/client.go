// Package ipcclient is a typed convenience wrapper over the mcosd JSON-RPC API.
//
// Neden ayri bir paket: iki farkli arayuz (Bubble Tea tabanli panel ve yeni
// framebuffer paneli) ayni cagrilari kullaniyor. Istemci panel paketinde
// kalsaydi, framebuffer ikilisi de Bubble Tea ve Lip Gloss'u baglamak zorunda
// kalirdi - initramfs'te bu bosuna megabaytlar demek.
package ipcclient

import (
	"errors"

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

// ErrNoDaemon is returned when the client is not connected.
//
// ── Neden bu hata var ─────────────────────────────────────────────────
// Panel, daemon'a bağlanamadan da ÇALIŞABILMELİDİR: ekran görüntüsü kipinde,
// açılışta daemon'dan önce hazır olduğunda, ya da daemon çöktüğünde. O
// durumlarda istemci nil'dir.
//
// Eskiden her çağrı yerinde ayrı ayrı "offline mi?" denetimi vardı ve bir
// tanesi unutulduğunda sonuç NİL İŞARETÇİ ÇÖKMESİYDİ — üstelik arka plan
// goroutine'inde, yani paneli tamamen düşürerek. (Ölçüldü: playit hesap
// bağlama akışı böyle çökertti — SIGSEGV, ipcclient/link.go:142.)
//
// Artık koruma TEK YERDE: bağlantı yoksa çağrı hata döner. Çağrı yerlerindeki
// offline() denetimleri yalnızca gereksiz "işlem sürüyor" mesajlarını
// engellemek için duruyor; DOĞRULUK artık buna bağlı değil.
var ErrNoDaemon = errors.New("mcosd bağlantısı yok")

// call is the single entry point for every RPC.
//
// nil alıcı da güvenlidir: Go'da nil bir işaretçi üzerinden metot çağırmak
// geçerlidir; yalnızca alanına erişmek çökertir — o erişim burada, tek bir
// denetimin arkasında.
func (cl *Client) call(method string, params any, out any) error {
	if cl == nil || cl.c == nil {
		return ErrNoDaemon
	}
	return cl.c.Call(method, params, out)
}

// callLong runs an RPC on a DEDICATED connection.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// ipc.Client bütün çağrıları TEK bir kilitle sıraya dizer (bağlantı başına bir
// istek). Kısa çağrılar için bu doğru ve basit.
//
// Ama bazı çağrılar DAKİKALARCA sürer: Java çalışma zamanını indirmek
// (~200 MB), sunucu jar'ını kurmak, yedek almak. O çağrı kilidi tuttuğu sürece
// panelin yoklama döngüsü, tuşlar ve fare TAMAMEN DONAR — kullanıcı "panel
// kilitlendi" der.
//
// Uzun çağrılar bu yüzden kendi bağlantılarını açar ve bitince kapatır.
// Daemon eş zamanlı bağlantıları zaten destekliyor: ipc.Server her bağlantıyı
// ayrı bir goroutine'de servis eder (internal/ipc/server.go).
//
// Yeni bağlantı açılamazsa PAYLAŞILAN bağlantıya düşülür: yavaş olur ama
// çalışır — işlemi hiç yapmamaktan iyidir.
func (cl *Client) callLong(method string, params any, out any) error {
	if cl == nil || cl.c == nil {
		return ErrNoDaemon
	}
	if cl.endpoint == "" {
		return cl.call(method, params, out)
	}
	c, err := ipc.DialClient(cl.endpoint)
	if err != nil {
		return cl.call(method, params, out)
	}
	defer c.Close()
	return c.Call(method, params, out)
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
	err := cl.call(ipc.MethodSystemStatus, nil, &st)
	return &st, err
}

func (cl *Client) Servers() ([]*model.Server, error) {
	var res ipc.ServerListResult
	err := cl.call(ipc.MethodServerList, nil, &res)
	return res.Servers, err
}

func (cl *Client) Server(id string) (*model.Server, error) {
	var res ipc.ServerResult
	err := cl.call(ipc.MethodServerGet, ipc.IDParams{ID: id}, &res)
	return res.Server, err
}

func (cl *Client) Create(p ipc.ServerCreateParams) (*model.Server, error) {
	var res ipc.ServerResult
	err := cl.callLong(ipc.MethodServerCreate, p, &res)
	return res.Server, err
}

// UpdateServer applies changes to an existing server's configuration.
func (cl *Client) UpdateServer(p ipc.ServerUpdateParams) (*model.Server, error) {
	var res ipc.ServerResult
	err := cl.call(ipc.MethodServerUpdate, p, &res)
	return res.Server, err
}

func (cl *Client) action(method, id string) error {
	var res ipc.OKResult
	return cl.call(method, ipc.IDParams{ID: id}, &res)
}

func (cl *Client) Install(id string) error { return cl.action(ipc.MethodServerInstall, id) }
func (cl *Client) Start(id string) error   { return cl.action(ipc.MethodServerStart, id) }
func (cl *Client) Stop(id string) error    { return cl.action(ipc.MethodServerStop, id) }
func (cl *Client) Restart(id string) error { return cl.action(ipc.MethodServerRestart, id) }
func (cl *Client) Delete(id string) error  { return cl.action(ipc.MethodServerDelete, id) }

func (cl *Client) Command(id, cmd string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodServerCommand, ipc.CommandParams{ID: id, Command: cmd}, &res)
}

func (cl *Client) ChangeVersion(id, mcVersion string, software model.Software) error {
	var res ipc.OKResult
	return cl.callLong(ipc.MethodServerChangeVersion, ipc.ServerChangeVersionParams{ID: id, MCVersion: mcVersion, Software: software}, &res)
}

// ServerVersions lists selectable Minecraft versions for a flavour (live from
// Mojang, with an offline fallback baked into the daemon).
func (cl *Client) ServerVersions(software model.Software) (ipc.ServerVersionsResult, error) {
	var res ipc.ServerVersionsResult
	err := cl.call(ipc.MethodServerVersions, ipc.ServerVersionsParams{Software: software}, &res)
	return res, err
}

// ── Network (Wi-Fi) ───────────────────────────────────────────────────────

func (cl *Client) WiFiScan() ([]ipc.WiFiNetwork, error) {
	var res ipc.WiFiScanResult
	err := cl.call(ipc.MethodNetWiFiScan, nil, &res)
	return res.Networks, err
}

func (cl *Client) WiFiApply(ssid, pass string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodNetWiFiApply, ipc.WiFiApplyParams{SSID: ssid, Password: pass}, &res)
}

// WiredUp brings up wired interfaces and requests DHCP (daemon-side, best-effort).
func (cl *Client) WiredUp() error {
	var res ipc.OKResult
	return cl.call(ipc.MethodNetWiredUp, nil, &res)
}

// ── Catalog (mods / plugins) ──────────────────────────────────────────────

func (cl *Client) CatalogSearch(serverID, query string) ([]ipc.CatalogItem, error) {
	var res ipc.CatalogSearchResult
	err := cl.call(ipc.MethodCatalogSearch, ipc.CatalogSearchParams{ServerID: serverID, Query: query}, &res)
	return res.Items, err
}

func (cl *Client) CatalogInstall(serverID, slug string) (string, error) {
	var res ipc.OKResult
	err := cl.callLong(ipc.MethodCatalogInstall, ipc.CatalogInstallParams{ServerID: serverID, Slug: slug}, &res)
	return res.Message, err
}

func (cl *Client) ScanUSBMods() ([]model.USBJar, error) {
	var res ipc.USBScanResult
	err := cl.call(ipc.MethodServerScanUSBMods, nil, &res)
	return res.Items, err
}

func (cl *Client) InstallUSBMods(serverID string, items []model.USBJar) (string, error) {
	var res ipc.OKResult
	err := cl.callLong(ipc.MethodServerInstallUSBMods,
		ipc.USBInstallParams{ServerID: serverID, Items: items}, &res)
	return res.Message, err
}

func (cl *Client) Console(id string, cursor int64) (ipc.ConsoleResult, error) {
	var res ipc.ConsoleResult
	err := cl.call(ipc.MethodServerConsole, ipc.ConsoleParams{ID: id, Cursor: cursor}, &res)
	return res, err
}

func (cl *Client) JavaList() ([]model.JavaRuntime, error) {
	var res ipc.JavaListResult
	err := cl.call(ipc.MethodJavaList, nil, &res)
	return res.Runtimes, err
}

// ErrJavaInstallUnverified means java.install answered, but the answer did not
// contain an installed runtime — so nothing may claim the JDK is ready.
//
// Neden ayrı bir hata: çağıran taraflar (sihirbaz ve Yazılım bölümü) bu hatayı
// "kurulum başarısız" diye gösterir; sessizce sıfır bir sürüm döndürmekten
// farkı, kullanıcının YANLIŞ değil DOĞRU bilgi görmesidir.
var ErrJavaInstallUnverified = errors.New("mcosd, Java kurulumunu doğrulayan bir yanıt döndürmedi")

// JavaInstall installs a Java major version and returns the runtime that is now
// on disk. It BLOCKS until the download and extraction have finished — call it
// from a background goroutine.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Daemon bu çağrıya ipc.OKResult ({"ok":true,"message":...}) ile yanıt
// veriyordu, burada ise yanıt ipc.JavaRuntimeResult'a çözülüyordu. İki şekil
// uyuşmadığı için JSON çözme sessizce başarılı oluyor, geriye HATASIZ bir
// SIFIR runtime kalıyordu. Çağıranlar err == nil'i "kuruldu" sayınca panel,
// indirme daha başlamadan "Java 0 kuruldu" yazıyordu; indirme sonradan
// çökse bile bunu düzelten hiçbir şey yoktu.
//
// Şekil artık iki tarafta da ipc.JavaRuntimeResult. Aşağıdaki denetim ise
// aynı sessiz uyuşmazlık BİR DAHA olmasın diye duruyor: eski bir mcosd (ya da
// ileride başka bir yanıt tipi) "runtime" alanı olmayan bir nesne dönerse,
// çağıran hatasız bir sıfır yerine açık bir hata alır — böylece panel ancak
// runtime gerçekten kuruluyken "kuruldu" der.
//
// Çağrı, diğer her RPC gibi tek girişli call() üzerinden gider: nil istemci
// denetimi orada, tek bir yerde.
func (cl *Client) JavaInstall(major int) (model.JavaRuntime, error) {
	var res ipc.JavaRuntimeResult
	if err := cl.callLong(ipc.MethodJavaInstall, ipc.JavaInstallParams{Major: major}, &res); err != nil {
		return model.JavaRuntime{}, err
	}
	if res.Runtime.Major <= 0 || res.Runtime.JavaBin == "" {
		return model.JavaRuntime{}, ErrJavaInstallUnverified
	}
	return res.Runtime, nil
}

func (cl *Client) JavaDetect() ([]model.JavaRuntime, error) {
	var res ipc.JavaListResult
	err := cl.call(ipc.MethodJavaDetect, nil, &res)
	return res.Runtimes, err
}

func (cl *Client) JavaProgress() (map[int]java.DownloadProgress, error) {
	var res ipc.JavaProgressResult
	err := cl.call(ipc.MethodJavaProgress, nil, &res)
	return res.Progresses, err
}

func (cl *Client) Config() (*model.Config, error) {
	var res ipc.ConfigResult
	err := cl.call(ipc.MethodConfigGet, nil, &res)
	return res.Config, err
}

func (cl *Client) UpdateConfig(cfg *model.Config) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodConfigSet, cfg, &res)
}

// ── System: power / turbo / persistence ───────────────────────────────────

// Power requests a poweroff or reboot. The daemon defers it briefly so this
// reply reaches the panel before the box goes down.
func (cl *Client) Power(action string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodSystemPower, ipc.PowerParams{Action: action}, &res)
}

// Turbo toggles the global turbo mode; returns the new state.
func (cl *Client) Turbo(enabled bool) (bool, error) {
	var res ipc.TurboResult
	err := cl.call(ipc.MethodSystemTurbo, ipc.TurboParams{Enabled: enabled}, &res)
	return res.Enabled, err
}

// Disks lists candidate block devices for the "make USB persistent" flow.
func (cl *Client) Disks() ([]ipc.DiskTarget, error) {
	var res ipc.DisksResult
	err := cl.call(ipc.MethodSystemDisks, nil, &res)
	return res.Disks, err
}

// Persist makes the booted USB (or the given device) persistent by adding an
// MCOS-DATA partition. Returns a human-readable result message.
func (cl *Client) Persist(device string) (string, error) {
	var res ipc.OKResult
	err := cl.call(ipc.MethodSystemPersist, ipc.PersistParams{Device: device}, &res)
	return res.Message, err
}

// ── Backup ──────────────────────────────────────────────────────────────

func (cl *Client) BackupList(serverID string) ([]model.Backup, error) {
	var res ipc.BackupListResult
	err := cl.call(ipc.MethodBackupList, ipc.IDParams{ID: serverID}, &res)
	return res.Backups, err
}

func (cl *Client) BackupCreate(p ipc.BackupCreateParams) error {
	var res ipc.OKResult
	return cl.callLong(ipc.MethodBackupCreate, p, &res)
}

func (cl *Client) BackupRestore(serverID, backupID string) error {
	var res ipc.OKResult
	return cl.callLong(ipc.MethodBackupRestore, ipc.BackupRestoreParams{ServerID: serverID, BackupID: backupID}, &res)
}

func (cl *Client) BackupDelete(serverID, backupID string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodBackupDelete, ipc.BackupDeleteParams{ServerID: serverID, BackupID: backupID}, &res)
}

// ── Files ─────────────────────────────────────────────────────────────────

func (cl *Client) FilesList(serverID, path string) ([]model.FileEntry, error) {
	var res ipc.FilesListResult
	err := cl.call(ipc.MethodFilesList, ipc.FilesListParams{ServerID: serverID, Path: path}, &res)
	return res.Entries, err
}

func (cl *Client) FilesRead(serverID, path string) (ipc.FilesReadResult, error) {
	var res ipc.FilesReadResult
	err := cl.call(ipc.MethodFilesRead, ipc.FilesReadParams{ServerID: serverID, Path: path}, &res)
	return res, err
}

func (cl *Client) FilesWrite(serverID, path, contentBase64 string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodFilesWrite, ipc.FilesWriteParams{ServerID: serverID, Path: path, ContentBase64: contentBase64}, &res)
}

func (cl *Client) FilesDelete(serverID, path string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodFilesDelete, ipc.FilesDeleteParams{ServerID: serverID, Path: path}, &res)
}

// ── Players ─────────────────────────────────────────────────────────────

func (cl *Client) PlayersList(serverID string) (ipc.PlayersListResult, error) {
	var res ipc.PlayersListResult
	err := cl.call(ipc.MethodPlayersList, ipc.IDParams{ID: serverID}, &res)
	return res, err
}

func (cl *Client) PlayersCommand(serverID, command string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodPlayersCommand, ipc.PlayersCommandParams{ServerID: serverID, Command: command}, &res)
}

// ── Worlds ───────────────────────────────────────────────────────────────

func (cl *Client) WorldsList(serverID string) ([]model.World, error) {
	var res ipc.WorldsListResult
	err := cl.call(ipc.MethodWorldsList, ipc.IDParams{ID: serverID}, &res)
	return res.Worlds, err
}

// ── Cluster ──────────────────────────────────────────────────────────────

func (cl *Client) ClusterPeers() ([]model.Peer, error) {
	var res ipc.ClusterPeersResult
	err := cl.call(ipc.MethodClusterPeers, nil, &res)
	return res.Peers, err
}

func (cl *Client) ClusterPair(id string, unpair bool) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodClusterPair, ipc.ClusterPairParams{ID: id, Unpair: unpair}, &res)
}

func (cl *Client) ClusterTasks() ([]model.Task, error) {
	var res ipc.ClusterTasksResult
	err := cl.call(ipc.MethodClusterTasks, nil, &res)
	return res.Tasks, err
}

// ── Tunnel / WAN ────────────────────────────────────────────────

func (cl *Client) TunnelList() ([]model.TunnelStatus, error) {
	var res ipc.TunnelListResult
	err := cl.call(ipc.MethodTunnelList, nil, &res)
	return res.Tunnels, err
}

func (cl *Client) TunnelCompress(command string) (string, error) {
	var res ipc.TunnelCompressResult
	err := cl.call(ipc.MethodTunnelCreate, ipc.TunnelCompressParams{Command: command}, &res)
	return res.Code, err
}

func (cl *Client) TunnelResolve(code string) (string, error) {
	var res ipc.TunnelResolveResult
	err := cl.call(ipc.MethodTunnelResolve, ipc.TunnelResolveParams{Code: code}, &res)
	return res.Command, err
}

func (cl *Client) TunnelStart(code string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodTunnelStart, ipc.TunnelStatusParams{Code: code}, &res)
}

func (cl *Client) TunnelStop(code string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodTunnelStop, ipc.TunnelStatusParams{Code: code}, &res)
}
