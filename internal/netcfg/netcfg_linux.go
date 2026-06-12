//go:build linux

package netcfg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const wpaConf = "/etc/wpa_supplicant.conf"

// wirelessIface returns the first wireless interface name, defaulting to wlan0.
func wirelessIface() string {
	entries, err := os.ReadDir("/sys/class/net")
	if err == nil {
		for _, e := range entries {
			if _, err := os.Stat(filepath.Join("/sys/class/net", e.Name(), "wireless")); err == nil {
				return e.Name()
			}
		}
	}
	return "wlan0"
}

// quote sanitises a value for inclusion in a quoted wpa_supplicant string by
// dropping embedded double quotes and backslashes.
func quote(s string) string {
	r := strings.NewReplacer("\"", "", "\\", "")
	return r.Replace(s)
}

func apply(ssid, pass string) error {
	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		return nil
	}
	iface := wirelessIface()

	var block string
	if pass == "" {
		block = fmt.Sprintf("network={\n\tssid=\"%s\"\n\tkey_mgmt=NONE\n}\n", quote(ssid))
	} else {
		block = fmt.Sprintf("network={\n\tssid=\"%s\"\n\tpsk=\"%s\"\n}\n", quote(ssid), quote(pass))
	}
	conf := "ctrl_interface=/var/run/wpa_supplicant\nupdate_config=1\ncountry=TR\n\n" + block
	if err := os.WriteFile(wpaConf, []byte(conf), 0o600); err != nil {
		return fmt.Errorf("netcfg: write %s: %w", wpaConf, err)
	}

	// Reconfigure a running supplicant; otherwise start one.
	if err := run("wpa_cli", "-i", iface, "reconfigure"); err != nil {
		_ = run("wpa_supplicant", "-B", "-i", iface, "-c", wpaConf)
	}
	// (Re)acquire a lease. busybox udhcpc is what Buildroot ships.
	if err := run("udhcpc", "-i", iface, "-n", "-q"); err != nil {
		_ = run("dhclient", iface)
	}
	return nil
}

func scan() ([]Network, error) {
	iface := wirelessIface()
	// Ensure a supplicant is up so wpa_cli can talk to it.
	if err := run("wpa_cli", "-i", iface, "status"); err != nil {
		_ = run("wpa_supplicant", "-B", "-i", iface, "-c", wpaConf)
		time.Sleep(500 * time.Millisecond)
	}
	_ = run("wpa_cli", "-i", iface, "scan")
	time.Sleep(2 * time.Second)
	out, err := output("wpa_cli", "-i", iface, "scan_results")
	if err != nil {
		return nil, nil // best-effort: no wireless tooling/iface
	}
	return parseScanResults(out), nil
}

// parseScanResults turns `wpa_cli scan_results` output into Network records.
// Columns: bssid / frequency / signal(dBm) / flags / ssid
func parseScanResults(out string) []Network {
	var nets []Network
	seen := map[string]bool{}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header / blank
		}
		cols := strings.SplitN(line, "\t", 5)
		if len(cols) < 5 {
			continue
		}
		ssid := strings.TrimSpace(cols[4])
		if ssid == "" || seen[ssid] {
			continue
		}
		seen[ssid] = true
		dbm, _ := strconv.Atoi(strings.TrimSpace(cols[2]))
		flags := cols[3]
		nets = append(nets, Network{
			SSID:    ssid,
			Signal:  dbmToPercent(dbm),
			Secured: strings.Contains(flags, "WPA") || strings.Contains(flags, "WEP"),
		})
	}
	// Sort best signal first (simple insertion is fine for a short list).
	for i := 1; i < len(nets); i++ {
		for j := i; j > 0 && nets[j].Signal > nets[j-1].Signal; j-- {
			nets[j], nets[j-1] = nets[j-1], nets[j]
		}
	}
	return nets
}

// dbmToPercent maps a typical -100..-30 dBm range onto 0..100.
func dbmToPercent(dbm int) int {
	if dbm == 0 {
		return 0
	}
	p := 2 * (dbm + 100)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

func applyTimezone(tz string) error {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return nil
	}
	zone := filepath.Join("/usr/share/zoneinfo", tz)
	if _, err := os.Stat(zone); err != nil {
		return fmt.Errorf("netcfg: unknown timezone %q", tz)
	}
	_ = os.Remove("/etc/localtime")
	if err := os.Symlink(zone, "/etc/localtime"); err != nil {
		return err
	}
	return os.WriteFile("/etc/timezone", []byte(tz+"\n"), 0o644)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	b, err := cmd.Output()
	return string(b), err
}

// bringUpWired brings up every wired (non-loopback, non-wireless) interface and
// leases a DHCP address in the background. The link is set up synchronously
// (instant; makes it multicast-capable for the cluster); the DHCP client runs
// detached with a bounded retry so this never blocks the caller / boot.
func bringUpWired() error {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil // best-effort
	}
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "wireless")); err == nil {
			continue // wireless is associated via wpa_supplicant in apply()
		}
		if err := run("ip", "link", "set", name, "up"); err != nil {
			_ = run("ifconfig", name, "up")
		}
		iface := name
		go func() { _ = run("udhcpc", "-i", iface, "-n", "-q", "-t", "5") }()
	}
	return nil
}
