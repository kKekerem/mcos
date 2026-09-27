//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Linux/macOS: konsol zaten vardır, kurulum install.sh + systemd kullanıcı
// birimiyle yapılır (bkz. packaging/linux). Bu yüzden buradaki işlevler
// ya hiçbir şey yapmaz ya da kullanıcıyı doğru araca yönlendirir.

func platformConsole(background bool) {}

// platformJob: Linux'ta gerekmez. systemd birimi durdurulunca bütün denetim
// grubunu (Java dahil) sonlandırır; terminalde Ctrl+C ise ön plandaki süreç
// grubunun TAMAMINA gider.
func platformJob() {}

func setupNeeded(s nodeSettings) bool { return false }

var errSetupUnsupported = errors.New("bu sistemde otomatik kurulum yok — install.sh kullanın")

func runSetup(dataRoot string, port int) (string, error) { return "", errSetupUnsupported }

func removeSetup(port int) error {
	return errors.New("Linux'ta kaldırmak için: sh ~/.local/lib/mcos-node/install.sh --kaldir")
}

func firewallElevated(ports string) int { return 1 }

// installedExe: Linux'ta arka plan systemd'dir; yol kullanılmaz.
func installedExe() string {
	exe, _ := os.Executable()
	return exe
}

// startBackground starts the systemd user service if it is installed.
func startBackground(exe string, args []string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "cat", "mcos-node.service").Run(); err != nil {
		return errors.New("mcos-node.service kurulu değil")
	}
	return exec.Command("systemctl", "--user", "start", "mcos-node.service").Run()
}

// systemdUnit is the user unit install.sh installs (packaging/linux).
const systemdUnit = "mcos-node.service"

func unitInstalled() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.Command("systemctl", "--user", "cat", systemdUnit).Run() == nil
}

// autostartStatus: Linux'ta oturum açılışı systemd kullanıcı birimidir.
func autostartStatus() autostartState {
	if !unitInstalled() {
		return autostartState{Detail: "systemd kullanıcı birimi kurulu değil — install.sh ile kurun."}
	}
	out, _ := exec.Command("systemctl", "--user", "is-enabled", systemdUnit).Output()
	return autostartState{Supported: true, Enabled: strings.TrimSpace(string(out)) == "enabled"}
}

func setAutostart(dataRoot string, port int, on bool) error {
	verb := "disable"
	if on {
		verb = "enable"
	}
	if out, err := exec.Command("systemctl", "--user", verb, systemdUnit).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user %s: %v (%s)", verb, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// openFolder opens the folder in the desktop's file manager.
func openFolder(path string) error {
	p, err := exec.LookPath("xdg-open")
	if err != nil {
		return errors.New("xdg-open yok — klasör: " + path)
	}
	c := exec.Command(p, path)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// guiFatal: Linux'ta arayüz bir terminalden açılır; hata oraya yazılır.
func guiFatal(err error) {
	fmt.Fprintln(os.Stderr, "HATA:", err)
}
