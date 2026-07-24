package panel

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mcos/internal/ipc"
	"mcos/internal/java"
	"mcos/internal/model"
)

// --- messages --------------------------------------------------------------

type tickMsg time.Time
type statusMsg struct{ status *model.SystemStatus }
type serversMsg struct{ servers []*model.Server }
type serverMsg struct{ server *model.Server }
type consoleMsg struct {
	id     string
	lines  []ipc.ConsoleLine
	cursor int64
}
type javaMsg struct{ runtimes []model.JavaRuntime }
type actionMsg struct {
	msg string
	err error
}
type createdMsg struct {
	server *model.Server
	err    error
}

// M6 async payloads
type backupsMsg struct {
	id      string
	backups []model.Backup
	err     error
}
type filesMsg struct {
	id      string
	entries []model.FileEntry
	err     error
}
type worldsMsg struct {
	id     string
	worlds []model.World
	err    error
}
type playersMsg struct {
	id     string
	result ipc.PlayersListResult
	err    error
}
type peersMsg struct {
	peers []model.Peer
	err   error
}
type tasksMsg struct {
	tasks []model.Task
	err   error
}
type disksMsg struct {
	disks []ipc.DiskTarget
	err   error
}
type configMsg struct {
	config *model.Config
	err    error
}

// setup wizard messages
type javaSetupMsg struct {
	ok       bool
	runtimes []model.JavaRuntime
}
type installDoneMsg struct {
	err error
}
type setupDoneMsg struct{}

type wifiScanMsg struct {
	networks []ipc.WiFiNetwork
	err      error
}

// versionsMsg carries the live Minecraft version list for the create wizard.
type versionsMsg struct {
	versions []string
	latest   string
	err      error
}

func doWiFiScan(cl *Client) tea.Cmd {
	return func() tea.Msg {
		n, err := cl.WiFiScan()
		return wifiScanMsg{networks: n, err: err}
	}
}

func doWiFiApply(cl *Client, ssid, pass string) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{msg: "wifi: " + ssid, err: cl.WiFiApply(ssid, pass)}
	}
}

// doWiredUp brings up wired interfaces + DHCP (daemon-side). Non-blocking.
func doWiredUp(cl *Client) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{msg: "kablolu arabirim getiriliyor (DHCP)", err: cl.WiredUp()}
	}
}

type javaProgressMsg struct {
	progress map[int]java.DownloadProgress
	err      error
}

func fetchJavaProgress(cl *Client) tea.Cmd {
	return func() tea.Msg {
		p, err := cl.JavaProgress()
		return javaProgressMsg{progress: p, err: err}
	}
}

// doJavaInstall installs a Java major asynchronously and begins progress polling.
func doJavaInstall(cl *Client, major int) tea.Cmd {
	return func() tea.Msg {
		go func() {
			_, _ = cl.JavaInstall(major)
		}()
		p, _ := cl.JavaProgress()
		return javaProgressMsg{progress: p, err: nil}
	}
}

// doPower asks the daemon to power off / reboot. The connection drops right
// after, which the panel treats as a normal shutdown.
func doPower(cl *Client, action string) tea.Cmd {
	return func() tea.Msg {
		label := "kapatılıyor…"
		if action == "reboot" {
			label = "yeniden başlatılıyor…"
		}
		return actionMsg{msg: label, err: cl.Power(action)}
	}
}

// doTurbo toggles global turbo mode and reports the resulting state.
func doTurbo(cl *Client, enabled bool) tea.Cmd {
	return func() tea.Msg {
		on, err := cl.Turbo(enabled)
		msg := "Turbo kapatıldı"
		if on {
			msg = "Turbo AÇIK — yeni/yeniden başlatılan sunucular tam hızlanır"
		}
		return actionMsg{msg: msg, err: err}
	}
}

// doPersist runs the "make this USB persistent" flow on the daemon.
func doPersist(cl *Client) tea.Cmd {
	return func() tea.Msg {
		m, err := cl.Persist("")
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: m}
	}
}

// doInstallOS executes the mcos-install script to copy and persist the OS.
// If dlJava or dlPlugins is true, it triggers those downloads via the daemon
// before copying the root filesystem to the target disk.
func doInstallOS(cl *Client, device string, dlJava, dlPlugins bool) tea.Cmd {
	return func() tea.Msg {
		if dlJava {
			// Pre-download common Java versions (17 and 21)
			_, _ = cl.JavaInstall(17)
			_, _ = cl.JavaInstall(21)
		}
		
		// Note: dlPlugins logic can be added here or in mcos-install script.
		// For now, we prioritize Java as it's the largest/most critical part.

		out, err := exec.Command("mcos-install", device).CombinedOutput()
		if err != nil {
			return installDoneMsg{err: fmt.Errorf("kurulum başarısız: %s", string(out))}
		}
		return installDoneMsg{err: nil}
	}
}

func doClusterPair(cl *Client, id string) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{msg: "eşleştirme istendi", err: cl.ClusterPair(id, false)}
	}
}

func doFetchVersions(cl *Client, sw model.Software) tea.Cmd {
	return func() tea.Msg {
		res, err := cl.ServerVersions(sw)
		return versionsMsg{versions: res.Versions, latest: res.Latest, err: err}
	}
}

type tunnelsMsg struct {
	tunnels []model.TunnelStatus
	err     error
}

type catalogMsg struct {
	id    string
	items []ipc.CatalogItem
	err   error
}

type errMsg struct{ err error }

// --- commands --------------------------------------------------------------

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func consoleTick(id string) tea.Cmd {
	return tea.Tick(700*time.Millisecond, func(t time.Time) tea.Msg { return consolePollMsg{id} })
}

type consolePollMsg struct{ id string }

func fetchStatus(cl *Client) tea.Cmd {
	return func() tea.Msg {
		st, err := cl.Status()
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{st}
	}
}

func fetchServers(cl *Client) tea.Cmd {
	return func() tea.Msg {
		s, err := cl.Servers()
		if err != nil {
			return errMsg{err}
		}
		return serversMsg{s}
	}
}

func fetchServer(cl *Client, id string) tea.Cmd {
	return func() tea.Msg {
		s, err := cl.Server(id)
		if err != nil {
			return errMsg{err}
		}
		return serverMsg{s}
	}
}

func fetchConsole(cl *Client, id string, cursor int64) tea.Cmd {
	return func() tea.Msg {
		res, err := cl.Console(id, cursor)
		if err != nil {
			return errMsg{err}
		}
		return consoleMsg{id: id, lines: res.Lines, cursor: res.Cursor}
	}
}

func fetchJava(cl *Client) tea.Cmd {
	return func() tea.Msg {
		rts, err := cl.JavaList()
		if err != nil {
			return errMsg{err}
		}
		return javaMsg{rts}
	}
}

// actionKind enumerates simple server actions.
type actionKind int

const (
	actStart actionKind = iota
	actStop
	actRestart
	actInstall
	actDelete
)

func doAction(cl *Client, kind actionKind, id string) tea.Cmd {
	return func() tea.Msg {
		var err error
		var label string
		switch kind {
		case actStart:
			err, label = cl.Start(id), "başlatılıyor"
		case actStop:
			err, label = cl.Stop(id), "durduruluyor"
		case actRestart:
			err, label = cl.Restart(id), "yeniden başlatılıyor"
			if err == nil {
				label = "kapatılıyor... (ardından başlayacak)"
			}
		case actInstall:
			err, label = cl.Install(id), "kuruluyor"
		case actDelete:
			err, label = cl.Delete(id), "silindi"
		}
		return actionMsg{msg: label, err: err}
	}
}

func doCommand(cl *Client, id, cmd string) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{msg: "komut gönderildi", err: cl.Command(id, cmd)}
	}
}

func doCreate(cl *Client, p ipc.ServerCreateParams) tea.Cmd {
	return func() tea.Msg {
		s, err := cl.Create(p)
		return createdMsg{server: s, err: err}
	}
}

func fetchBackups(cl *Client, id string) tea.Cmd {
	return func() tea.Msg {
		b, err := cl.BackupList(id)
		return backupsMsg{id: id, backups: b, err: err}
	}
}

func doBackupCreate(cl *Client, id string) tea.Cmd {
	return func() tea.Msg {
		if err := cl.BackupCreate(ipc.BackupCreateParams{ServerID: id}); err != nil {
			return backupsMsg{id: id, err: err}
		}
		b, err := cl.BackupList(id)
		return backupsMsg{id: id, backups: b, err: err}
	}
}

func fetchFiles(cl *Client, id, path string) tea.Cmd {
	return func() tea.Msg {
		e, err := cl.FilesList(id, path)
		return filesMsg{id: id, entries: e, err: err}
	}
}

func fetchWorlds(cl *Client, id string) tea.Cmd {
	return func() tea.Msg {
		w, err := cl.WorldsList(id)
		return worldsMsg{id: id, worlds: w, err: err}
	}
}

func fetchPlayers(cl *Client, id string) tea.Cmd {
	return func() tea.Msg {
		r, err := cl.PlayersList(id)
		return playersMsg{id: id, result: r, err: err}
	}
}

func doPlayerCmd(cl *Client, id, cmd string) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{msg: "oyuncu komutu: " + cmd, err: cl.PlayersCommand(id, cmd)}
	}
}

func doCatalogSearch(cl *Client, id, query string) tea.Cmd {
	return func() tea.Msg {
		items, err := cl.CatalogSearch(id, query)
		return catalogMsg{id: id, items: items, err: err}
	}
}

func doCatalogInstall(cl *Client, id, slug string) tea.Cmd {
	return func() tea.Msg {
		name, err := cl.CatalogInstall(id, slug)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: "kuruldu: " + name}
	}
}

func fetchConfig(cl *Client) tea.Cmd {
	return func() tea.Msg {
		cfg, err := cl.Config()
		return configMsg{config: cfg, err: err}
	}
}

func fetchTunnels(cl *Client) tea.Cmd {
	return func() tea.Msg {
		t, err := cl.TunnelList()
		return tunnelsMsg{tunnels: t, err: err}
	}
}

func fetchPeers(cl *Client) tea.Cmd {
	return func() tea.Msg {
		p, err := cl.ClusterPeers()
		return peersMsg{peers: p, err: err}
	}
}

func fetchTasks(cl *Client) tea.Cmd {
	return func() tea.Msg {
		t, err := cl.ClusterTasks()
		return tasksMsg{tasks: t, err: err}
	}
}

func doFetchDisks(cl *Client) tea.Cmd {
	return func() tea.Msg {
		d, err := cl.Disks()
		return disksMsg{disks: d, err: err}
	}
}
