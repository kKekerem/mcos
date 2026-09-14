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

// wirelessIfaces returns all detected wireless interface names.
func wirelessIfaces() []string {
	var ifaces []string
	entries, err := os.ReadDir("/sys/class/net")
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if name == "lo" {
				continue
			}
			isWireless := false
			if _, err := os.Stat(filepath.Join("/sys/class/net", name, "phy80211")); err == nil {
				isWireless = true
			} else if _, err := os.Stat(filepath.Join("/sys/class/net", name, "wireless")); err == nil {
				isWireless = true
			} else if strings.HasPrefix(name, "wlan") || strings.HasPrefix(name, "wlp") || strings.HasPrefix(name, "wls") || strings.HasPrefix(name, "wl") {
				isWireless = true
			}
			if isWireless {
				ifaces = append(ifaces, name)
			}
		}
	}
	if len(ifaces) == 0 {
		ifaces = append(ifaces, "wlan0")
	}
	return ifaces
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
	ifaces := wirelessIfaces()
	iface := ifaces[0]

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

// defaultRegDomain is the regulatory domain applied when none is active.
//
// TR seçildi çünkü cihaz Türkiye'de kullanılıyor. Yanlış bir alan seçmek
// yasal bir sorun değil pratik bir sorundur: fazla kısıtlı bir alan kanalları
// kapatır, fazla geniş bir alan kartın kullanamayacağı kanalları açar.
const defaultRegDomain = "TR"

// SetRegulatoryDomain applies a wireless regulatory domain.
//
// ── NEDEN GEREKLİ ───────────────────────────────────────────────────────────
// Kullanıcı şikâyeti: "wifi çalışıyor ama ağları görmüyor."
//
// Çekirdek CONFIG_CFG80211_REQUIRE_SIGNED_REGDB=y ile derleniyor, yani
// cfg80211 /lib/firmware/regulatory.db (+ .p7s imzası) yüklemek ZORUNDA.
// O dosyalar imajda YOKTU; sonuçta hiçbir alan yüklenemiyor ve çekirdek
// gömülü "00" (dünya dolaşımı) alanına düşüyordu.
//
// "00" alanında kanalların çoğu NO-IR işaretlidir: kart o kanallarda AKTİF
// tarama yapamaz, yalnızca pasif dinler. 5 GHz'in tamamı bu durumdadır.
// Belirti tam olarak buydu — kart çalışıyor, arayüz UP oluyor, tarama boş.
//
// Veritabanı artık imajda (BR2_PACKAGE_WIRELESS_REGDB). Ama tek başına
// yetmez: alanın UYGULANMASI da gerekir, yoksa çekirdek yine "00" ile
// başlar. wpa_supplicant.conf'taki "country=TR" satırı yalnızca supplicant
// çalışırken etkilidir; taramadan önce burada açıkça ayarlıyoruz.
func SetRegulatoryDomain(code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return fmt.Errorf("netcfg: geçersiz ülke kodu %q", code)
	}
	// iw tercih edilir (nl80211); wireless-tools yedeği eski kartlar için.
	if err := run("iw", "reg", "set", code); err == nil {
		return nil
	}
	return run("iwconfig", "reg", code)
}

// regDomainActive reports the currently active domain, or "" if unknown.
//
// "00" dönmesi, veritabanının yüklenemediği anlamına gelir — arayüz bunu
// kullanıcıya söyleyebilmeli, çünkü "ağ göremiyorum" şikâyetinin en olası
// sebebi budur.
func regDomainActive() string {
	out, err := output("iw", "reg", "get")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "country ") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				return strings.TrimSuffix(f[1], ":")
			}
		}
	}
	return ""
}

func scan() ([]Network, error) {
	_ = os.MkdirAll("/var/run/wpa_supplicant", 0755)
	_ = os.MkdirAll("/run/wpa_supplicant", 0755)

	// Unblock wireless devices via rfkill BEFORE checking /sys/class/net
	_ = run("rfkill", "unblock", "all")
	_ = run("rfkill", "unblock", "wifi")
	_ = run("rfkill", "unblock", "wlan")
	time.Sleep(500 * time.Millisecond)

	// DÜZENLEYİCİ ALANI TARAMADAN ÖNCE AYARLA.
	//
	// Aktif alan "00" ise (veya hiç yoksa) kanalların çoğu aktif taramaya
	// kapalıdır ve tarama boş döner. Bu satır o durumu düzeltir.
	if d := regDomainActive(); d == "" || d == "00" {
		if err := SetRegulatoryDomain(defaultRegDomain); err == nil {
			// Çekirdeğin kanal listesini yeniden hesaplaması için kısa bir an.
			time.Sleep(300 * time.Millisecond)
		}
	}

	ifaces := wirelessIfaces()

	// Ensure /etc/wpa_supplicant.conf exists so supplicant starts clean
	if _, err := os.Stat(wpaConf); err != nil {
		_ = os.WriteFile(wpaConf, []byte("ctrl_interface=/var/run/wpa_supplicant\nupdate_config=1\ncountry=TR\n"), 0o600)
	}

	var allNets []Network
	seen := map[string]bool{}

	for _, iface := range ifaces {
		// Bring interface UP and wait for driver/firmware readiness
		_ = run("ip", "link", "set", iface, "up")
		_ = run("ifconfig", iface, "up")
		time.Sleep(1 * time.Second)

		var res []Network

		// Try up to 3 scan attempts for slower WiFi cards
		for attempt := 1; attempt <= 3; attempt++ {
			// 1. Try iwlist scan (Direct ioctl scan across all Linux wireless extensions)
			iwlistOut, err := output("iwlist", iface, "scan")
			if err == nil && strings.TrimSpace(iwlistOut) != "" {
				res = parseIwlistScanResults(iwlistOut)
				if len(res) > 0 {
					break
				}
			}

			// 2. Try wpa_cli scan with nl80211,wext driver fallback
			if err := run("wpa_cli", "-i", iface, "status"); err != nil {
				_ = run("wpa_supplicant", "-B", "-i", iface, "-c", wpaConf, "-D", "nl80211,wext")
				time.Sleep(1 * time.Second)
			}
			_ = run("wpa_cli", "-i", iface, "scan")
			time.Sleep(2 * time.Second)
			wpaOut, _ := output("wpa_cli", "-i", iface, "scan_results")
			res = parseScanResults(wpaOut)
			if len(res) > 0 {
				break
			}

			// 3. Try iw dev scan
			iwOut, err := output("iw", "dev", iface, "scan")
			if err == nil && strings.TrimSpace(iwOut) != "" {
				res = parseIwScanResults(iwOut)
				if len(res) > 0 {
					break
				}
			}

			time.Sleep(1 * time.Second)
		}

		for _, n := range res {
			if !seen[n.SSID] && strings.TrimSpace(n.SSID) != "" {
				seen[n.SSID] = true
				allNets = append(allNets, n)
			}
		}
	}
	return allNets, nil
}

func parseIwlistScanResults(out string) []Network {
	var nets []Network
	seen := map[string]bool{}
	var currentSSID string
	var currentSignal int
	var currentSecured bool

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Cell ") {
			if currentSSID != "" && !seen[currentSSID] {
				seen[currentSSID] = true
				nets = append(nets, Network{SSID: currentSSID, Signal: currentSignal, Secured: currentSecured})
			}
			currentSSID = ""
			currentSignal = 0
			currentSecured = false
		} else if idx := strings.Index(line, "ESSID:\""); idx != -1 {
			s := line[idx+len("ESSID:\""):]
			s = strings.TrimSuffix(s, "\"")
			currentSSID = strings.TrimSpace(s)
		} else if strings.Contains(line, "Signal level=") {
			if idx := strings.Index(line, "Signal level="); idx != -1 {
				sub := line[idx+len("Signal level="):]
				fields := strings.Fields(sub)
				if len(fields) > 0 {
					dbm, _ := strconv.Atoi(strings.TrimSuffix(fields[0], "dBm"))
					currentSignal = dbmToPercent(dbm)
				}
			}
		} else if strings.Contains(line, "Encryption key:on") || strings.Contains(line, "WPA") || strings.Contains(line, "IEEE 802.11i") {
			currentSecured = true
		}
	}
	if currentSSID != "" && !seen[currentSSID] {
		nets = append(nets, Network{SSID: currentSSID, Signal: currentSignal, Secured: currentSecured})
	}
	return nets
}

func parseIwScanResults(out string) []Network {
	var nets []Network
	seen := map[string]bool{}
	var currentSSID string
	var currentSignal int
	var currentSecured bool

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "BSS ") {
			if currentSSID != "" && !seen[currentSSID] {
				seen[currentSSID] = true
				nets = append(nets, Network{SSID: currentSSID, Signal: currentSignal, Secured: currentSecured})
			}
			currentSSID = ""
			currentSignal = 0
			currentSecured = false
		} else if strings.HasPrefix(line, "SSID: ") {
			currentSSID = strings.TrimPrefix(line, "SSID: ")
		} else if strings.HasPrefix(line, "signal: ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				dbm, _ := strconv.ParseFloat(fields[1], 64)
				currentSignal = dbmToPercent(int(dbm))
			}
		} else if strings.Contains(line, "WPA") || strings.Contains(line, "RSN") || strings.Contains(line, "WEP") {
			currentSecured = true
		}
	}
	if currentSSID != "" && !seen[currentSSID] {
		nets = append(nets, Network{SSID: currentSSID, Signal: currentSignal, Secured: currentSecured})
	}
	return nets
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
