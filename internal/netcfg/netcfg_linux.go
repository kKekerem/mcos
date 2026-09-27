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

func apply(ssid, pass string) error {
	if strings.TrimSpace(ssid) == "" {
		return nil
	}
	// Parola ve ad HİÇBİR ŞEYE dokunmadan önce doğrulanır: kısa parola
	// eskiden supplicant'ın tüm yapılandırmayı reddetmesine ve 20 saniyelik
	// anlamsız bir beklemeye yol açıyordu.
	if err := checkSSID(ssid); err != nil {
		return err
	}
	if err := checkPassword(pass); err != nil {
		return err
	}

	// Süren taramayı durdur ve bitmesini bekle (bkz. wifiMu).
	wifiAbort.Store(true)
	wifiMu.Lock()
	wifiAbort.Store(false)
	defer wifiMu.Unlock()

	return recordFailure(connect(ssid, pass))
}

// connect does the actual association + DHCP. Every error it returns is
// shown to the user verbatim, so each one names the cause.
//
// ── Düzeltilen gerçek hata: DHCP, İLİŞKİLENDİRMEDEN ÖNCE ÇALIŞIYORDU ──
//
// Kullanıcı: "wifiye baglaniyom hala internet yok dio". Eski sıra:
// "wpa_cli reconfigure" -> HEMEN udhcpc. "reconfigure" yalnızca "ayarları
// yeniden oku" der; tarama, kimlik doğrulama ve ilişkilendirme saniyeler
// sürer. Artık önce COMPLETED beklenir, sonra DHCP, sonra adres doğrulanır.
func connect(ssid, pass string) error {
	ifs := findWireless()
	if len(ifs) == 0 {
		return noWirelessError()
	}
	iface := ifs[0].Name

	// 1. rfkill (araçsız, sysfs) + arayüzü kaldır + düzenleyici alan.
	if err := prepareRadio(iface); err != nil {
		return err
	}
	ensureRegDomain()

	// 2. Supplicant: çalışmıyorsa başlat (tarama başlatmış olabilir).
	if err := ensureSupplicant(iface); err != nil {
		return err
	}
	sae := supplicantHasSAE(iface)
	conf, err := wpaConfig(ssid, pass, sae)
	if err != nil {
		return err
	}
	if err := os.WriteFile(wpaConf, []byte(conf), 0o600); err != nil {
		return fmt.Errorf("kablosuz ayarı yazılamadı: %w", err)
	}
	// Günlüğün bu denemeye ait kısmını ayırabilmek için boyutunu not et.
	logFrom := fileSize(wpaLog)
	out, err := wpaCLI(iface, "reconfigure")
	if err != nil || !strings.Contains(out, "OK") {
		return fmt.Errorf("wpa_supplicant yeni ayarı kabul etmedi (%s): %s",
			strings.TrimSpace(out), lastLines(readFrom(wpaLog, logFrom), 3))
	}
	_, _ = wpaCLI(iface, "reassociate")

	// 3. İLİŞKİLENDİRMEYİ BEKLE.
	if err := waitAssociated(iface, ssid, pass != "", sae, logFrom, assocTimeout); err != nil {
		return err
	}

	// 4. DHCP: tek kopya, arka planda kiralamayı yeniler; adres GERÇEKTEN
	//    gelene kadar beklenir.
	startDHCP(iface)
	if ip := waitIPv4(iface, dhcpTimeout); ip == "" {
		return fmt.Errorf("'%s' ağına bağlanıldı ama IP adresi alınamadı: yönlendiricinin "+
			"DHCP sunucusu yanıt vermiyor (arka planda denemeye devam ediliyor)", ssid)
	}
	return nil
}

// assocTimeout, ilişkilendirmenin tamamlanması için beklenen en uzun süre.
//
// 20 saniye: zayıf sinyalde tarama + kimlik doğrulama + 4'lü el sıkışma
// gerçekten bu kadar sürebiliyor. Daha kısa tutmak, çalışan bir bağlantıyı
// "başarısız" diye raporlamak demek olurdu.
const assocTimeout = 20 * time.Second

// waitAssociated blocks until wpa_supplicant reports COMPLETED; on failure it
// explains WHY (parola yanlış / ağ yok / WPA3 / kart kapalı), see assocWatch.
//
// Yanlış parola erken yakalanır: supplicant günlüğüne WRONG_KEY düştüğünde ya
// da el sıkışma iki kez düştüğünde 20 saniyenin dolması beklenmez.
func waitAssociated(iface, ssid string, secured, sae bool, logFrom int64, limit time.Duration) error {
	var w assocWatch
	son := time.Now().Add(limit)
	for time.Now().Before(son) {
		st := ""
		if out, err := wpaCLI(iface, "status"); err == nil {
			for _, satir := range strings.Split(out, "\n") {
				if strings.HasPrefix(satir, "wpa_state=") {
					st = strings.TrimSpace(strings.TrimPrefix(satir, "wpa_state="))
				}
			}
		}
		w.observe(st)
		if st == "COMPLETED" {
			return nil
		}
		if secured && (wrongKeyLogged(readFrom(wpaLog, logFrom)) || w.handshake >= 2) {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	sr, _ := wpaCLI(iface, "scan_results")
	visible, onlySAE := saeOnly(sr, ssid)
	ln, _ := wpaCLI(iface, "list_networks")
	return w.failure(ssid, assocFacts{
		WrongKey:     wrongKeyLogged(readFrom(wpaLog, logFrom)),
		TempDisabled: strings.Contains(ln, "TEMP-DISABLED"),
		SSIDVisible:  visible,
		SAEOnly:      onlySAE,
		SAESupported: sae,
		Secured:      secured,
		Iface:        iface,
	})
}

// wrongKeyLogged reports whether the supplicant log says the key was wrong.
func wrongKeyLogged(log string) bool {
	return strings.Contains(log, "reason=WRONG_KEY") ||
		strings.Contains(log, "pre-shared key may be incorrect")
}

// fileSize returns the size of path, or 0.
func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

// readFrom returns path's content starting at byte offset from.
func readFrom(path string, from int64) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if from < 0 || from > int64(len(b)) {
		from = 0 // günlük kesilmiş/yeniden başlamış
	}
	return string(b[from:])
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

func scan() ([]Network, error) { return scanLive(nil) }

// scanLive does the real work; onBatch (may be nil) receives the accumulated
// set as soon as any source yields something new, so the panel can list
// networks WHILE the scan is still running.
func scanLive(onBatch func([]Network)) ([]Network, error) {
	// Bağlanma sürüyorsa bekle; bağlanma başlarsa bu tarama durur (wifiAbort).
	wifiMu.Lock()
	defer wifiMu.Unlock()

	_ = os.MkdirAll("/var/run/wpa_supplicant", 0755)
	_ = os.MkdirAll("/run/wpa_supplicant", 0755)

	// rfkill engelini ARAÇSIZ kaldır (sysfs). "rfkill" ikilisi imajda yok;
	// eskiden buradaki üç "rfkill unblock" çağrısı sessizce başarısız
	// oluyordu (bkz. unblockRfkill). Sert engel kullanıcıya söylenir: kart
	// kapalıyken boş bir liste "yakında ağ yok" gibi görünür.
	if hard := unblockRfkill(rfkillRoot); len(hard) > 0 {
		err := recordFailure(hardBlockError(hard))
		if onBatch != nil {
			onBatch(nil)
		}
		return nil, err
	}
	time.Sleep(300 * time.Millisecond)

	// DÜZENLEYİCİ ALANI TARAMADAN ÖNCE AYARLA.
	//
	// Aktif alan "00" ise (veya hiç yoksa) kanalların çoğu aktif taramaya
	// kapalıdır ve tarama boş döner.
	ensureRegDomain()

	ifaces := wirelessIfaces()
	if len(ifaces) == 0 {
		// Eskiden burada "wlan0" uydurulup 10+ saniye boşuna taranıyordu ve
		// sonuç sessizce boş liste oluyordu. Neden (kart yok / firmware
		// eksik) artık panelde yazıyor.
		err := recordFailure(noWirelessError())
		if onBatch != nil {
			onBatch(nil)
		}
		return nil, err
	}

	var allNets []Network
	seen := map[string]bool{}

	// emit, yeni bulunan ağları birikimli kümeye ekler ve DEĞİŞİKLİK VARSA
	// çağırana haber verir. Kopya gönderilir: çağıran (panel) dilimi kendi
	// kilidinin altında saklıyor ve biz taramaya devam ederken allNets'i
	// yeniden tahsis ediyoruz — aynı arka belleği paylaşmak veri yarışıdır.
	emit := func(res []Network) {
		added := false
		for _, n := range res {
			if !seen[n.SSID] && strings.TrimSpace(n.SSID) != "" {
				seen[n.SSID] = true
				allNets = append(allNets, n)
				added = true
			}
		}
		if added && onBatch != nil {
			onBatch(append([]Network(nil), allNets...))
		}
	}

	var ilkHata error
	for _, iface := range ifaces {
		if wifiAbort.Load() {
			break
		}
		// Arayüzü kaldır; RF-kill gibi bir neden varsa sakla (hiç ağ
		// bulunamazsa kullanıcıya o söylenir).
		if err := prepareRadio(iface); err != nil && ilkHata == nil {
			ilkHata = err
		}
		time.Sleep(1 * time.Second)

		var res []Network

		// Try up to 3 scan attempts for slower WiFi cards
		for attempt := 1; attempt <= 3 && !wifiAbort.Load(); attempt++ {
			// 1. Try iwlist scan (Direct ioctl scan across all Linux wireless extensions)
			iwlistOut, err := output("iwlist", iface, "scan")
			if err == nil && strings.TrimSpace(iwlistOut) != "" {
				res = parseIwlistScanResults(iwlistOut)
				if len(res) > 0 {
					emit(res)
					break
				}
			}

			// 2. wpa_supplicant üzerinden tarama (gerekirse başlatılır; -f
			//    günlüğüyle, bağlanmadaki ile AYNI biçimde).
			_ = ensureSupplicant(iface)
			_ = run("wpa_cli", "-i", iface, "scan")
			time.Sleep(2 * time.Second)
			wpaOut, _ := output("wpa_cli", "-i", iface, "scan_results")
			res = parseScanResults(wpaOut)
			if len(res) > 0 {
				emit(res)
				break
			}

			// 3. Try iw dev scan
			iwOut, err := output("iw", "dev", iface, "scan")
			if err == nil && strings.TrimSpace(iwOut) != "" {
				res = parseIwScanResults(iwOut)
				if len(res) > 0 {
					emit(res)
					break
				}
			}

			time.Sleep(1 * time.Second)
		}

		// Denemeler bittiğinde son sonuç zaten emit edilmiş olabilir; emit
		// yinelenenleri eler, bu yüzden ikinci çağrı zararsızdır ve
		// "son deneme kısmen döndü" durumunu da kapsar.
		emit(res)
	}

	// Son bildirim: hiç ağ bulunamasa bile çağıran "tarama bitti, liste bu"
	// bilgisini almalı. Yoksa panel boş listeyi sonsuza kadar "aranıyor" diye
	// gösterirdi.
	if onBatch != nil {
		onBatch(append([]Network(nil), allNets...))
	}
	if len(allNets) == 0 && ilkHata != nil {
		return allNets, recordFailure(ilkHata)
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

// bringUpWired brings up every wired interface and keeps a DHCP lease on it.
//
// ── Yakalanan gerçek hatalar ────────────────────────────────────────────────
//
//  1. Bu işlev AÇILIŞTA HİÇ ÇAĞRILMIYORDU (yalnızca kurulum sihirbazından ve
//     Donanım sekmesinden). Kablolu bir sunucu yeniden başladığında AĞSIZ
//     açılıyordu: PC eşleme keşfi, uzaktan ekran, tünel, indirmeler — hiçbiri
//     çalışmıyordu. QEMU'da virtio-net ile ölçüldü: panel "Ağ: yok" dedi, VNC
//     dışarıdan erişilemedi.
//  2. udhcpc "-n -q" ile çalışıyordu: -q adresi alınca ÇIKAR ve kiralamayı
//     YENİLEMEZ (kira süresi dolunca yönlendirici adresi başkasına verebilir,
//     ağ bir gün sonra kopar); -n ilk denemeler başarısızsa vazgeçer (kablo
//     sonradan takılırsa hiç denenmez).
//
// Artık her kablolu arayüz için TEK bir arka plan udhcpc'si (-b) çalışır:
// adres alamazsa beklemeye devam eder, alınca kiralamayı yeniler. Aynı
// arayüz için ikinci bir kopya başlatılmaz (pid dosyası), bu yüzden işlev
// istenildiği kadar çağrılabilir — daemon onu periyodik çağırıyor ki sonradan
// takılan USB Ethernet de bağlansın.
func bringUpWired() error {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil // best-effort
	}
	for _, e := range entries {
		name := e.Name()
		if !wiredCandidate(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "wireless")); err == nil {
			continue // wireless is associated via wpa_supplicant in apply()
		}
		if err := run("ip", "link", "set", name, "up"); err != nil {
			_ = run("ifconfig", name, "up")
		}
		pid := "/run/udhcpc." + name + ".pid"
		if dhcpRunning(pid) {
			continue
		}
		// -b: kiralama alınamazsa arka plana geç ve denemeye devam et.
		// -t 3 -T 3 -A 10: 3 deneme x 3 sn, sonra 10 sn bekle, sürekli.
		// -S: syslog; -p: tek kopya denetimi için pid dosyası.
		cmd := exec.Command("udhcpc", "-i", name, "-b", "-S", "-p", pid,
			"-t", "3", "-T", "3", "-A", "10")
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
		}
	}
	return nil
}

// wiredCandidate filters virtual interfaces that must not get DHCP.
//
// Köprüler, tüneller ve konteyner arayüzleri (docker0, veth*, tun*, wg*)
// kendi yapılandırmalarına sahip; onlara DHCP istemcisi bağlamak ağı bozar.
func wiredCandidate(name string) bool {
	if name == "lo" {
		return false
	}
	for _, p := range []string{"docker", "veth", "br-", "virbr", "tun", "tap", "wg", "zt", "tailscale", "sit", "dummy"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	// Fiziksel aygıtın /sys/class/net/<ad>/device bağlantısı vardır; sanal
	// arayüzlerin yoktur.
	_, err := os.Stat(filepath.Join("/sys/class/net", name, "device"))
	return err == nil
}

// dhcpRunning reports whether the udhcpc that owns pidfile is alive.
func dhcpRunning(pidfile string) bool {
	b, err := os.ReadFile(pidfile)
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(b))
	if pid == "" {
		return false
	}
	cmdline, err := os.ReadFile("/proc/" + pid + "/cmdline")
	return err == nil && strings.Contains(string(cmdline), "udhcpc")
}
