package netcfg

// Kablosuz bağlantının platformdan BAĞIMSIZ mantığı: wpa_supplicant yapılandırma
// metni, parola doğrulama, ilişkilendirme durumunun yorumlanması ve çekirdek
// günlüğünden eksik firmware'in bulunması. Hepsi saf işlevlerdir; donanım
// gerektirmeden sınanırlar (wifi_test.go). Donanıma dokunan kısım
// wifi_linux.go'da.

import (
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// wpaHeader is the global part of /etc/wpa_supplicant.conf.
//
// update_config=0: supplicant'ın dosyamızı kendi kafasına göre yeniden
// yazmasını istemiyoruz — dosyayı her bağlanışta biz üretiyoruz.
const wpaHeader = "ctrl_interface=/var/run/wpa_supplicant\nupdate_config=0\ncountry=TR\n"

// wpaConfig builds the complete wpa_supplicant.conf for one network.
//
// ── Yakalanan gerçek hata: parolanın bazı karakterleri SİLİNİYORDU ──────────
//
// Eski kod SSID'yi ve parolayı çift tırnak içine koyabilmek için içlerindeki
// `"` ve `\` karakterlerini SESSİZCE atıyordu. `abc"123` parolası
// supplicant'a `abc123` olarak gidiyor, yönlendirici haklı olarak reddediyor
// ve kullanıcı "parolayı doğru yazdım, bağlanmıyor" diyordu. Aynı şey adında
// tırnak geçen ağlar için de geçerliydi.
//
// Artık hiçbir değer tırnak içine YAZILMIYOR:
//   - SSID onaltılık yazılır (ssid=6d79616761) — her bayt olduğu gibi gider.
//   - WPA2 parolası (8-63 karakter) PBKDF2 ile 64 haneli PSK'ya çevrilir;
//     wpa_passphrase'in yaptığının aynısı (IEEE 802.11i ek H.4).
//
// WPA3 (SAE) PSK kabul etmez, parolanın kendisini ister; sae=true iken
// sae_password tırnaklı yazılır. wpa_supplicant tırnaklı dizgiyi SON tırnağa
// kadar okur (config.c wpa_config_parse_string, os_strrchr), yani içerideki
// tırnak zararsızdır; yalnızca satır sonu yasaktır ve baştan reddedilir.
func wpaConfig(ssid, pass string, sae bool) (string, error) {
	if err := checkSSID(ssid); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(wpaHeader)
	b.WriteString("\nnetwork={\n")
	fmt.Fprintf(&b, "\tssid=%s\n", hex.EncodeToString([]byte(ssid)))
	// Gizli (yayın yapmayan) ağlar da bulunabilsin: adı bilinen ağ için
	// doğrudan sorgu gönderilir. Görünür ağlarda zararsızdır.
	b.WriteString("\tscan_ssid=1\n")

	if pass == "" {
		b.WriteString("\tkey_mgmt=NONE\n}\n")
		return b.String(), nil
	}
	psk, err := pskFor(ssid, pass)
	if err != nil {
		return "", err
	}
	if sae {
		// WPA2 + WPA3 karma: yönlendirici hangisini sunarsa. ieee80211w=1
		// (isteğe bağlı PMF) WPA3 için şart, WPA2 yönlendiricilerini bozmaz.
		b.WriteString("\tkey_mgmt=WPA-PSK WPA-PSK-SHA256 SAE\n")
		b.WriteString("\tieee80211w=1\n")
		fmt.Fprintf(&b, "\tpsk=%s\n", psk)
		if len(pass) != 64 || !isHex(pass) {
			fmt.Fprintf(&b, "\tsae_password=\"%s\"\n", pass)
		}
	} else {
		b.WriteString("\tkey_mgmt=WPA-PSK WPA-PSK-SHA256\n")
		fmt.Fprintf(&b, "\tpsk=%s\n", psk)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// checkSSID validates a network name.
func checkSSID(ssid string) error {
	if strings.TrimSpace(ssid) == "" {
		return fmt.Errorf("ağ adı boş")
	}
	if len(ssid) > 32 {
		return fmt.Errorf("ağ adı çok uzun (%d bayt; en çok 32 olabilir)", len(ssid))
	}
	return nil
}

// checkPassword validates a WPA passphrase before anything is touched.
//
// Eskiden kısa parola doğrudan dosyaya yazılıyordu; wpa_supplicant dosyanın
// TAMAMINI reddediyor, "reconfigure" FAIL dönüyor ve kullanıcı 20 saniye
// bekledikten sonra anlamsız bir "sürücü yanıt vermiyor" görüyordu.
func checkPassword(pass string) error {
	if pass == "" {
		return nil // açık ağ
	}
	if strings.ContainsAny(pass, "\r\n\x00") {
		return fmt.Errorf("Wi-Fi parolası satır sonu içeremez")
	}
	if len(pass) == 64 && isHex(pass) {
		return nil // hazır PSK
	}
	n := utf8.RuneCountInString(pass)
	if len(pass) < 8 || len(pass) > 63 {
		return fmt.Errorf("Wi-Fi parolası 8 ile 63 karakter arasında olmalı (girilen: %d karakter)", n)
	}
	return nil
}

// pskFor returns the 64-hex-digit WPA PSK for a passphrase.
func pskFor(ssid, pass string) (string, error) {
	if err := checkPassword(pass); err != nil {
		return "", err
	}
	if len(pass) == 64 && isHex(pass) {
		return strings.ToLower(pass), nil
	}
	k, err := pbkdf2.Key(sha1.New, pass, []byte(ssid), 4096, 32)
	if err != nil {
		return "", fmt.Errorf("parola anahtara çevrilemedi: %w", err)
	}
	return hex.EncodeToString(k), nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return s != ""
}

// ── İlişkilendirmenin yorumlanması ──────────────────────────────────────────

// assocWatch records the wpa_state values seen while waiting for a link.
//
// ── Yakalanan gerçek hata: "parola yanlış" HİÇ söylenemiyordu ───────────────
//
// Eski bekleme döngüsü "4WAY_HANDSHAKE_FAILED" ve "HANDSHAKE_FAILED"
// durumlarını arıyordu. wpa_supplicant'ta böyle durumlar YOK (wpa_states:
// DISCONNECTED, INTERFACE_DISABLED, INACTIVE, SCANNING, AUTHENTICATING,
// ASSOCIATING, ASSOCIATED, 4WAY_HANDSHAKE, GROUP_HANDSHAKE, COMPLETED).
// Yanlış parolada supplicant 4WAY_HANDSHAKE'e gelir, düşer ve yeniden
// SCANNING'e döner; eski kod 20 saniye sonra "durum: SCANNING — parola yanlış
// olabilir ya da sinyal zayıf" diyordu. Kullanıcı hangisi olduğunu
// bilemiyordu. Artık iz tutuluyor ve neden AYRI AYRI söyleniyor.
type assocWatch struct {
	seen      map[string]bool
	last      string
	handshake int // 4WAY_HANDSHAKE'e kaç kez girildi
}

func (w *assocWatch) observe(state string) {
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	if state == "4WAY_HANDSHAKE" && w.last != "4WAY_HANDSHAKE" {
		w.handshake++
	}
	if state != "" {
		w.seen[state] = true
		w.last = state
	}
}

// assocFacts are the extra observations used to explain a failure.
type assocFacts struct {
	WrongKey     bool   // supplicant günlüğünde WRONG_KEY / "pre-shared key may be incorrect"
	TempDisabled bool   // list_networks: [TEMP-DISABLED]
	SSIDVisible  bool   // ağ taramada görünüyor mu
	SAEOnly      bool   // ağ yalnızca WPA3 (SAE) sunuyor
	SAESupported bool   // bu wpa_supplicant SAE biliyor mu
	Secured      bool   // parola girildi mi
	Iface        string // arayüz adı
}

// failure turns what was seen into ONE user-facing reason (Turkish).
func (w *assocWatch) failure(ssid string, f assocFacts) error {
	switch {
	case w.last == "INTERFACE_DISABLED" || (w.last == "" && !w.seen["SCANNING"]):
		return fmt.Errorf("kablosuz arayüz (%s) etkin değil: sürücü kartı başlatamadı ya da "+
			"kart kapalı (uçak kipi / Fn tuşu)", f.Iface)
	case f.SAEOnly && !f.SAESupported:
		return fmt.Errorf("'%s' yalnızca WPA3 kullanıyor ve bu sürüm WPA3 desteklemiyor: "+
			"yönlendiricide güvenliği \"WPA2/WPA3\" (karma) ya da \"WPA2\" yapın", ssid)
	case f.Secured && (f.WrongKey || w.handshake >= 2 ||
		(w.handshake >= 1 && f.TempDisabled)):
		return fmt.Errorf("'%s' ağı parolayı kabul etmedi: parola yanlış", ssid)
	case !f.SSIDVisible && !w.seen["ASSOCIATING"] && !w.seen["AUTHENTICATING"]:
		return fmt.Errorf("'%s' ağı bulunamadı: menzil dışında olabilir, adı yanlış olabilir "+
			"ya da yalnızca bu kartın desteklemediği bir bantta (5/6 GHz) yayın yapıyor", ssid)
	case w.seen["ASSOCIATING"] && !w.seen["ASSOCIATED"]:
		return fmt.Errorf("'%s' ağı bağlantıyı reddetti (sinyal çok zayıf ya da yönlendiricide "+
			"MAC filtresi var)", ssid)
	case w.handshake == 1:
		return fmt.Errorf("'%s' ağında kimlik doğrulama tamamlanamadı: parola yanlış olabilir", ssid)
	}
	son := w.last
	if son == "" {
		son = "bilinmiyor"
	}
	return fmt.Errorf("'%s' ağına bağlanılamadı (son durum: %s)", ssid, son)
}

// ── Taramadan güvenlik türü ─────────────────────────────────────────────────

// saeOnly reports whether `wpa_cli scan_results` shows ssid, and whether
// EVERY access point of it offers only WPA3 (SAE) and no WPA2 (PSK).
//
// Bayrak biçimi (wpa_supplicant 2.10, wpa_supplicant_ie_txt): köşeli ayraç
// içinde "PROTO-ANAHTAR1+ANAHTAR2-ŞİFRE", ör. [WPA2-PSK-CCMP],
// [WPA2-PSK+SAE-CCMP] (karma), [WPA2-SAE-CCMP] (yalnızca WPA3),
// [WPA2-PSK-SHA256-CCMP]. Anahtar adlarının kendisinde de "-" olabildiği için
// (PSK-SHA256) ayrıştırma son "-" (şifre) atılarak yapılır.
func saeOnly(scanResults, ssid string) (visible, onlySAE bool) {
	for i, line := range strings.Split(scanResults, "\n") {
		if i == 0 {
			continue
		}
		cols := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 5)
		if len(cols) < 5 || cols[4] != ssid {
			continue
		}
		visible = true
		psk, sae := keyMgmt(cols[3])
		if psk || !sae {
			return true, false // en az bir erişim noktası WPA2 (ya da açık/WEP)
		}
		onlySAE = true
	}
	return visible, onlySAE
}

// keyMgmt extracts whether scan flags offer PSK and/or SAE authentication.
func keyMgmt(flags string) (psk, sae bool) {
	for _, grp := range strings.Split(flags, "[") {
		grp = strings.TrimSuffix(strings.TrimSpace(grp), "]")
		var rest string
		switch {
		case strings.HasPrefix(grp, "WPA2-"):
			rest = grp[5:]
		case strings.HasPrefix(grp, "RSN-"):
			rest = grp[4:]
		case strings.HasPrefix(grp, "WPA-"):
			rest = grp[4:]
		default:
			continue
		}
		if k := strings.LastIndex(rest, "-"); k > 0 {
			rest = rest[:k] // şifreyi (CCMP, TKIP, GCMP-256'nın sonu) at
		}
		for _, km := range strings.Split(rest, "+") {
			km = strings.TrimPrefix(km, "FT/")
			switch {
			case strings.HasPrefix(km, "PSK"):
				psk = true
			case strings.HasPrefix(km, "SAE"):
				sae = true
			}
		}
	}
	return psk, sae
}

// ── Çekirdek günlüğünden eksik firmware ─────────────────────────────────────

var (
	reFwDirect = regexp.MustCompile(`Direct firmware load for (\S+) failed`)
	reFwFailed = regexp.MustCompile(`(?:failed to load|could not load|request firmware failed|firmware: failed to load) (?:firmware )?(\S+\.(?:bin|ucode|fw|pnvm))`)
)

// missingFirmware lists firmware files the kernel failed to load, in order.
func missingFirmware(klog string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(klog, "\n") {
		for _, re := range []*regexp.Regexp{reFwDirect, reFwFailed} {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				f := strings.Trim(m[1], "'\"(),:")
				if f != "" && !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// wifiFirmware keeps only firmware names that belong to wireless drivers.
//
// Ekran kartı ya da bluetooth firmware'inin eksikliği Wi-Fi'nin nedeni
// değildir; kullanıcıya onları göstermek yanlış yere baktırır.
func wifiFirmware(names []string) []string {
	var out []string
	for _, n := range names {
		l := strings.ToLower(n)
		if strings.Contains(l, "bt") && !strings.Contains(l, "iwlwifi") {
			continue // rtl8723bu_bt.bin gibi bluetooth parçaları
		}
		for _, p := range []string{"iwlwifi", "ath9k", "ath10k", "ath11k", "ath12k", "rtw88",
			"rtw89", "rtlwifi", "rtl_bt", "mediatek/mt7", "mt76", "mt7", "brcm/brcmfmac",
			"cypress/", "rt2", "rt3", "rt5", "carl9170", "htc_", "ar9", "mrvl/", "ti-connectivity",
			"wil6210", "qca/"} {
			if strings.Contains(l, p) {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// ── rfkill ─────────────────────────────────────────────────────────────────

// rfkillDev is one /sys/class/rfkill entry.
type rfkillDev struct {
	Dir  string // /sys/class/rfkill/rfkill0
	Name string // phy0, ideapad_wlan, hp-wifi...
	Type string // wlan, bluetooth, wwan, all...
	Soft bool
	Hard bool
}

// readRfkill lists rfkill switches under root (normally /sys/class/rfkill).
func readRfkill(root string) []rfkillDev {
	dirs, _ := filepath.Glob(filepath.Join(root, "rfkill*"))
	sort.Strings(dirs)
	var out []rfkillDev
	for _, d := range dirs {
		oku := func(ad string) string {
			b, _ := os.ReadFile(filepath.Join(d, ad))
			return strings.TrimSpace(string(b))
		}
		out = append(out, rfkillDev{
			Dir: d, Name: oku("name"), Type: oku("type"),
			Soft: oku("soft") == "1", Hard: oku("hard") == "1",
		})
	}
	return out
}

// wifiSwitch reports whether an rfkill switch can block Wi-Fi.
func (r rfkillDev) wifiSwitch() bool { return r.Type == "wlan" || r.Type == "all" }

// unblockRfkill clears the SOFT block on every Wi-Fi switch under root and
// returns the switches that are still HARD blocked.
//
// ── Yakalanan gerçek hata: rfkill ARACI İMAJDA YOK ──────────────────────────
//
// Kod her yerde "rfkill unblock all" çalıştırıyordu ve hatayı yutuyordu.
// Ölçüldü: dist/mcos-x86_64.iso'nun initrd'sinde rfkill ikilisi YOK.
// mcos_defconfig BR2_PACKAGE_UTIL_LINUX_RFKILL=y diyor ama derleme ağacındaki
// util-linux 4 Haziran'da "--disable-rfkill" ile yapılandırılmış ve bir daha
// yeniden derlenmemiş (Buildroot seçenek değişince paketi kendiliğinden
// yeniden derlemez). Sonuç: Windows'ta uçak kipinde kapatılmış ya da
// üreticinin (ideapad/asus/hp-wmi) yumuşak engelle başlattığı bir dizüstünde
// kart ENGELLİ kalıyordu; "ip link set wlan0 up" "RF-kill" hatası veriyor,
// tarama boş dönüyor, bağlantı kurulamıyordu.
//
// Artık araca hiç ihtiyaç yok: çekirdeğin sysfs arayüzü (/sys/class/rfkill/
// rfkillN/soft, 2.6.31'den beri yazılabilir) doğrudan kullanılıyor. Sert
// engel (donanım anahtarı, BIOS) yazılımla kaldırılamaz; onu kullanıcıya
// söylemek için döndürüyoruz.
func unblockRfkill(root string) (hard []rfkillDev) {
	for _, r := range readRfkill(root) {
		if !r.wifiSwitch() {
			continue
		}
		if r.Soft {
			_ = os.WriteFile(filepath.Join(r.Dir, "soft"), []byte("0"), 0o644)
		}
		if r.Hard {
			hard = append(hard, r)
		}
	}
	return hard
}

// hardBlockError explains a hardware Wi-Fi block.
func hardBlockError(hard []rfkillDev) error {
	var ad []string
	for _, r := range hard {
		ad = append(ad, r.Name)
	}
	return fmt.Errorf("kablosuz kart DONANIMSAL olarak kapalı (%s): dizüstündeki Wi-Fi/uçak "+
		"kipi tuşuna ya da anahtarına basın (çoğunda Fn+F2, Fn+F12 ya da yanda bir sürgü); "+
		"BIOS'ta kablosuz kapalıysa oradan açın", strings.Join(ad, ", "))
}
