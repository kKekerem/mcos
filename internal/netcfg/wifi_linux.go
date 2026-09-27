//go:build linux

package netcfg

// Kablosuz bağlantının donanıma dokunan kısmı: rfkill, arayüz bulma,
// wpa_supplicant yönetimi, DHCP ve TANI raporu.
//
// Tanı raporu neden var: kullanıcı gerçek dizüstünde (Ventoy'dan) deniyor ve
// "kablosuza bağlanamıyorum" dışında bir şey göremiyordu. Bir sonraki
// denemede nedenin EKRANDA ve bir dosyada yazması gerekiyor: kart yok mu,
// firmware mı eksik, anahtar mı kapalı, parola mı yanlış, DHCP mi yanıtsız.

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	rfkillRoot = "/sys/class/rfkill"
	// wpaLog: supplicant'ın -f ile yazdığı günlük (CONFIG_DEBUG_FILE=y).
	// "pre-shared key may be incorrect" gibi nedenler yalnızca burada görünür.
	wpaLog = "/run/mcos/wpa_supplicant.log"
	// taniDosya: son kablosuz hatasının ayrıntılı raporu.
	taniDosya = "/run/mcos/wifi-tani.txt"
)

// wifiMu serializes scanning and connecting.
//
// ── Yakalanan gerçek hata: tarama bağlantının ortasında sürüyordu ───────────
//
// Panel taramayı arka planda başlatıyor ve ağlar göründükçe listeliyor;
// kullanıcı ilk ağ görünür görünmez ona tıklayabiliyor. Tarama ise 45
// saniyeye kadar sürüp arayüzde "iwlist scan", "wpa_cli scan" ve
// "iw dev scan" çalıştırmaya DEVAM ediyordu — tam kimlik doğrulama sırasında.
// Tarama isteği ilişkilendirmeyi kesebilir ("Device or resource busy") ya
// da el sıkışmayı zaman aşımına uğratır. Artık bağlanma, süren taramayı
// DURDURUP bekliyor (wifiAbort) ve ikisi hiç aynı anda çalışmıyor.
var (
	wifiMu    sync.Mutex
	wifiAbort atomic.Bool
)

// wirelessIface is one detected wireless network interface.
type wirelessIface struct {
	Name   string
	Driver string // iwlwifi, rtw89_8852be, ath11k_pci...
}

// findWireless lists wireless interfaces (phy80211 ya da wireless dizini olan).
//
// Eskiden hiç arayüz yoksa "wlan0" UYDURULUYORDU: sonraki her adım var olmayan
// bir arayüzle 10+ saniye uğraşıp "kablosuz sürücü yanıt vermiyor (wlan0)"
// diyordu. Asıl neden — kart yok ya da firmware eksik — hiç söylenmiyordu.
func findWireless() []wirelessIface {
	var out []wirelessIface
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		base := filepath.Join("/sys/class/net", name)
		_, e1 := os.Stat(filepath.Join(base, "phy80211"))
		_, e2 := os.Stat(filepath.Join(base, "wireless"))
		if e1 != nil && e2 != nil {
			continue
		}
		drv := ""
		if l, err := os.Readlink(filepath.Join(base, "device", "driver")); err == nil {
			drv = filepath.Base(l)
		}
		out = append(out, wirelessIface{Name: name, Driver: drv})
	}
	return out
}

// wirelessIfaces returns the names of detected wireless interfaces (may be empty).
func wirelessIfaces() []string {
	var out []string
	for _, w := range findWireless() {
		out = append(out, w.Name)
	}
	return out
}

// kernelLog returns the kernel ring buffer (dmesg) — root required.
func kernelLog() string {
	n, err := syscall.Klogctl(10, nil) // SYSLOG_ACTION_SIZE_BUFFER
	if err != nil || n <= 0 {
		n = 1 << 20
	}
	buf := make([]byte, n)
	m, err := syscall.Klogctl(3, buf) // SYSLOG_ACTION_READ_ALL
	if err != nil || m <= 0 {
		return ""
	}
	return string(buf[:m])
}

// pciWireless lists PCI network controllers of class 0x0280 (kablosuz).
func pciWireless() []string {
	var out []string
	devs, _ := filepath.Glob("/sys/bus/pci/devices/*")
	for _, d := range devs {
		b, err := os.ReadFile(filepath.Join(d, "class"))
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(b)), "0x0280") {
			continue
		}
		ven, _ := os.ReadFile(filepath.Join(d, "vendor"))
		dev, _ := os.ReadFile(filepath.Join(d, "device"))
		drv := "sürücü YOK"
		if l, err := os.Readlink(filepath.Join(d, "driver")); err == nil {
			drv = "sürücü " + filepath.Base(l)
		}
		out = append(out, fmt.Sprintf("%s [%s:%s] (%s)", filepath.Base(d),
			strings.TrimPrefix(strings.TrimSpace(string(ven)), "0x"),
			strings.TrimPrefix(strings.TrimSpace(string(dev)), "0x"), drv))
	}
	return out
}

// noWirelessError explains why there is no wireless interface at all.
func noWirelessError() error {
	fw := wifiFirmware(missingFirmware(kernelLog()))
	pci := pciWireless()
	switch {
	case len(fw) > 0:
		if len(fw) > 3 {
			fw = append(fw[:3], "…")
		}
		return fmt.Errorf("kablosuz kart bulundu ama başlatılamadı: firmware eksik (%s). "+
			"Bu kart bu MCOS sürümünde çalışmıyor; USB Wi-Fi çubuğu ya da kablo kullanın",
			strings.Join(fw, ", "))
	case len(pci) > 0:
		return fmt.Errorf("kablosuz kart var (%s) ama ağ arayüzü oluşmadı: sürücüsü bu "+
			"sürümde yok ya da kart BIOS'ta kapalı", strings.Join(pci, "; "))
	}
	return fmt.Errorf("kablosuz kart bulunamadı: bu makinede (ya da takılı USB'de) " +
		"tanınan bir Wi-Fi aygıtı yok")
}

// prepareRadio unblocks rfkill and brings iface up; returns a clear error.
func prepareRadio(iface string) error {
	if hard := unblockRfkill(rfkillRoot); len(hard) > 0 {
		return hardBlockError(hard)
	}
	var out string
	var err error
	// Yumuşak engel kaldırıldıktan sonra çekirdeğin olayı işlemesi birkaç
	// yüz ms sürebiliyor; ilk "up" RF-kill ile dönerse kısa bir yeniden deneme.
	for i := 0; i < 3; i++ {
		out, err = combined("ip", "link", "set", iface, "up")
		if err == nil || !strings.Contains(out, "RF-kill") {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	if err != nil {
		if strings.Contains(out, "RF-kill") {
			return fmt.Errorf("kablosuz kart kapalı (rfkill) ve açılamadı: uçak kipi/Wi-Fi tuşuna basın")
		}
		return fmt.Errorf("kablosuz arayüz (%s) açılamadı: %s", iface, strings.TrimSpace(out))
	}
	return nil
}

// ensureRegDomain applies the default regulatory domain if none is active.
func ensureRegDomain() {
	if d := regDomainActive(); d == "" || d == "00" {
		if err := SetRegulatoryDomain(defaultRegDomain); err == nil {
			time.Sleep(300 * time.Millisecond)
		}
	}
}

// wpaCLI runs wpa_cli against iface and returns its trimmed output.
func wpaCLI(iface string, args ...string) (string, error) {
	out, err := output("wpa_cli", append([]string{"-i", iface}, args...)...)
	return strings.TrimSpace(out), err
}

// ensureSupplicant starts wpa_supplicant on iface unless one already answers.
func ensureSupplicant(iface string) error {
	if out, err := wpaCLI(iface, "ping"); err == nil && strings.Contains(out, "PONG") {
		return nil
	}
	if _, err := os.Stat(wpaConf); err != nil {
		_ = os.WriteFile(wpaConf, []byte(wpaHeader), 0o600)
	}
	_ = os.MkdirAll("/var/run/wpa_supplicant", 0o755)
	_ = os.MkdirAll(filepath.Dir(wpaLog), 0o755)
	// -D nl80211: bu imajdaki wpa_supplicant YALNIZCA nl80211 ile derlendi
	// (CONFIG_DRIVER_WEXT yok); eski "nl80211,wext" listesindeki wext hiç
	// yoktu. -f: neden günlüğü (yanlış parola vb.) için.
	out, err := combined("wpa_supplicant", "-B", "-i", iface, "-c", wpaConf,
		"-D", "nl80211", "-f", wpaLog)
	if err != nil {
		return fmt.Errorf("wpa_supplicant başlatılamadı (%s): %s", iface, lastLines(out, 3))
	}
	// Denetim soketi oluşana kadar bekle; yoksa ilk wpa_cli komutu boşa gider.
	for i := 0; i < 20; i++ {
		if out, err := wpaCLI(iface, "ping"); err == nil && strings.Contains(out, "PONG") {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("wpa_supplicant yanıt vermiyor (%s)", iface)
}

// supplicantHasSAE reports whether the running supplicant can do WPA3.
func supplicantHasSAE(iface string) bool {
	out, err := wpaCLI(iface, "get_capability", "key_mgmt")
	return err == nil && strings.Contains(" "+out+" ", " SAE ")
}

// ── DHCP ───────────────────────────────────────────────────────────────────

// startDHCP (re)starts ONE background udhcpc for iface.
//
// ── Yakalanan gerçek hatalar ────────────────────────────────────────────────
//  1. udhcpc pid dosyasız başlatılıyordu: her "Bağlan" yeni bir kopya
//     bırakıyordu ve aynı arayüzde birden çok istemci kiralamayı birbirine
//     yeniletip adresi oynatıyordu.
//  2. "-b" ile başlatılan udhcpc adres ALAMASA da arka plana geçip 0 ile
//     çıkıyor. Yani "adres alınamadı" hatası hiçbir zaman üretilmiyordu;
//     panel "bağlanıldı" diyor, internet yoktu. Artık adres gerçekten
//     arayüzde görünene kadar bekleniyor (waitIPv4).
func startDHCP(iface string) {
	pid := "/run/udhcpc." + iface + ".pid"
	if b, err := os.ReadFile(pid); err == nil {
		if n := strings.TrimSpace(string(b)); n != "" && dhcpRunning(pid) {
			var p int
			if _, err := fmt.Sscan(n, &p); err == nil && p > 1 {
				_ = syscall.Kill(p, syscall.SIGTERM)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	cmd := exec.Command("udhcpc", "-i", iface, "-b", "-S", "-p", pid,
		"-t", "5", "-T", "2", "-A", "10")
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// ipv4Of returns iface's first IPv4 address, or "".
func ipv4Of(iface string) string {
	ifc, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
			return n.IP.String()
		}
	}
	return ""
}

// waitIPv4 polls until iface has an IPv4 address or the limit passes.
func waitIPv4(iface string, limit time.Duration) string {
	son := time.Now().Add(limit)
	for time.Now().Before(son) {
		if ip := ipv4Of(iface); ip != "" {
			return ip
		}
		time.Sleep(300 * time.Millisecond)
	}
	return ipv4Of(iface)
}

// dhcpTimeout: 5 deneme × 2 sn + pay. Yönlendiricilerin çoğu 1-3 sn'de yanıtlar.
const dhcpTimeout = 15 * time.Second

// ── Tanı ───────────────────────────────────────────────────────────────────

// Diagnose returns a Turkish multi-line report of the wireless state.
func Diagnose() string {
	var b strings.Builder
	fmt.Fprintf(&b, "MCOS kablosuz tanı raporu — %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	ifs := findWireless()
	if len(ifs) == 0 {
		b.WriteString("Kablosuz arayüz: YOK\n")
		fmt.Fprintf(&b, "Neden: %v\n", noWirelessError())
	}
	for _, w := range ifs {
		st, _ := os.ReadFile("/sys/class/net/" + w.Name + "/operstate")
		fmt.Fprintf(&b, "Arayüz: %s  sürücü=%s  durum=%s  IPv4=%s\n", w.Name, w.Driver,
			strings.TrimSpace(string(st)), orYok(ipv4Of(w.Name)))
		if out, err := wpaCLI(w.Name, "status"); err == nil {
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, "wpa_state=") || strings.HasPrefix(l, "ssid=") ||
					strings.HasPrefix(l, "key_mgmt=") || strings.HasPrefix(l, "freq=") {
					b.WriteString("  " + l + "\n")
				}
			}
		} else {
			b.WriteString("  wpa_supplicant: çalışmıyor\n")
		}
	}
	for _, p := range pciWireless() {
		b.WriteString("PCI kablosuz aygıt: " + p + "\n")
	}
	for _, r := range readRfkill(rfkillRoot) {
		fmt.Fprintf(&b, "rfkill %s (%s, %s): yumuşak=%v sert=%v\n",
			filepath.Base(r.Dir), r.Name, r.Type, r.Soft, r.Hard)
	}
	fmt.Fprintf(&b, "Düzenleyici alan: %s\n", orYok(regDomainActive()))
	if fw := wifiFirmware(missingFirmware(kernelLog())); len(fw) > 0 {
		b.WriteString("Yüklenemeyen Wi-Fi firmware'i: " + strings.Join(fw, ", ") + "\n")
	}
	if rc, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		b.WriteString("resolv.conf: " + strings.Join(strings.Fields(string(rc)), " ") + "\n")
	}
	if lg, err := os.ReadFile(wpaLog); err == nil {
		b.WriteString("\nwpa_supplicant günlüğünün sonu:\n" + lastLines(string(lg), 25) + "\n")
	}
	return b.String()
}

// recordFailure writes the diagnosis next to the error so the user (or
// "cat /run/mcos/wifi-tani.txt" from the recovery shell) can see WHY.
func recordFailure(err error) error {
	if err == nil {
		return nil
	}
	rapor := "HATA: " + err.Error() + "\n\n" + Diagnose()
	_ = os.MkdirAll(filepath.Dir(taniDosya), 0o755)
	_ = os.WriteFile(taniDosya, []byte(rapor), 0o644)
	// Kalıcı kopya: /run RAM'de ve yeniden başlatınca kaybolur; kullanıcı
	// raporu Ventoy USB'sinden bilgisayarına alabilsin diye /data'ya da.
	if st, e := os.Stat("/data"); e == nil && st.IsDir() {
		_ = os.MkdirAll("/data/log", 0o755)
		_ = os.WriteFile("/data/log/wifi-tani.txt", []byte(rapor), 0o644)
	}
	fmt.Fprintf(os.Stderr, "netcfg: kablosuz: %v (ayrıntı: %s)\n", err, taniDosya)
	return err
}

func orYok(s string) string {
	if s == "" {
		return "yok"
	}
	return s
}

// lastLines returns the last n non-empty lines of s.
func lastLines(s string, n int) string {
	var ls []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(l) != "" {
			ls = append(ls, l)
		}
	}
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

// combined runs a command and returns stdout+stderr.
func combined(name string, args ...string) (string, error) {
	b, err := exec.Command(name, args...).CombinedOutput()
	return string(b), err
}
