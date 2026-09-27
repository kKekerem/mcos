package netcfg

import (
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Kablosuz bağlantının saf mantığının sınamaları. Donanım yok: wpa_supplicant
// çıktıları, çekirdek günlüğü satırları ve sysfs rfkill dizini elle üretiliyor.

// IEEE 802.11i-2004 ek H.4 sınama vektörü: parola "password", SSID "IEEE".
func TestPSKStandartVektor(t *testing.T) {
	got, err := pskFor("IEEE", "password")
	if err != nil {
		t.Fatal(err)
	}
	const want = "f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e"
	if got != want {
		t.Fatalf("PSK = %s, beklenen %s", got, want)
	}
}

// Ölçülen hata: tırnak ve ters bölü parolalardan SESSİZCE siliniyordu.
func TestParoladakiOzelKarakterlerKorunur(t *testing.T) {
	const ssid, pass = `Ev "Ağı"`, `ab"c\d12345`
	conf, err := wpaConfig(ssid, pass, false)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := pbkdf2.Key(sha1.New, pass, []byte(ssid), 4096, 32)
	if !strings.Contains(conf, "psk="+hex.EncodeToString(k)) {
		t.Fatalf("PSK parolanın TAMAMINDAN türetilmemiş:\n%s", conf)
	}
	kesik, _ := pbkdf2.Key(sha1.New, "abcd12345", []byte("Ev Ağı"), 4096, 32)
	if strings.Contains(conf, hex.EncodeToString(kesik)) {
		t.Fatalf("parola eski hatadaki gibi kırpılmış")
	}
	if !strings.Contains(conf, "ssid="+hex.EncodeToString([]byte(ssid))+"\n") {
		t.Fatalf("SSID onaltılık ve eksiksiz yazılmamış:\n%s", conf)
	}
	if strings.Contains(conf, `"`+pass) || strings.Contains(conf, "Ağı") {
		t.Fatalf("değerler tırnak içinde düz metin olarak kalmış:\n%s", conf)
	}
}

func TestParolaDogrulama(t *testing.T) {
	for _, c := range []struct {
		pass string
		ok   bool
	}{
		{"", true},         // açık ağ
		{"1234567", false}, // 7 karakter
		{"12345678", true},
		{strings.Repeat("a", 63), true},
		{strings.Repeat("z", 64), false},                    // 64 ama onaltılık değil
		{strings.Repeat("ab", 32), true},                    // 64 haneli hazır PSK
		{"iyi parola\nikinci satir", false},                 // satır sonu
		{"çğıöşü12", true},                                  // 8 karakter, 14 bayt
		{strings.Repeat("ş", 32), false},                    // 64 bayt > 63
		{strings.Repeat("0123456789abcdef", 4)[:64], true},  // hazır PSK
		{strings.Repeat("0123456789abcdeg", 4)[:64], false}, // g onaltılık değil
	} {
		err := checkPassword(c.pass)
		if (err == nil) != c.ok {
			t.Errorf("checkPassword(%q) hata=%v, geçerli olmalı=%v", c.pass, err, c.ok)
		}
	}
}

func TestAcikAgVeWPA3Yapilandirmasi(t *testing.T) {
	acik, err := wpaConfig("Kafe", "", false)
	if err != nil || !strings.Contains(acik, "key_mgmt=NONE") || strings.Contains(acik, "psk=") {
		t.Fatalf("açık ağ yapılandırması yanlış (err=%v):\n%s", err, acik)
	}
	w3, err := wpaConfig("Ev", "parola123", true)
	if err != nil || !strings.Contains(w3, "SAE") || !strings.Contains(w3, `sae_password="parola123"`) ||
		!strings.Contains(w3, "ieee80211w=1") {
		t.Fatalf("WPA3 yapılandırması yanlış (err=%v):\n%s", err, w3)
	}
	w2, _ := wpaConfig("Ev", "parola123", false)
	if strings.Contains(w2, "SAE") || strings.Contains(w2, "sae_password") {
		t.Fatalf("SAE desteklenmezken SAE yazılmış (supplicant tüm dosyayı reddeder):\n%s", w2)
	}
	if _, err := wpaConfig(strings.Repeat("x", 33), "parola123", false); err == nil {
		t.Fatal("33 baytlık SSID kabul edildi")
	}
}

// ── İlişkilendirme hatalarının yorumu ───────────────────────────────────────

func izle(states ...string) *assocWatch {
	w := &assocWatch{}
	for _, s := range states {
		w.observe(s)
	}
	return w
}

func TestYanlisParolaAyirtEdilir(t *testing.T) {
	// Yanlış parolada supplicant'ın gerçek durum dizisi: el sıkışma iki kez
	// denenir, arada SCANNING/DISCONNECTED.
	w := izle("SCANNING", "ASSOCIATING", "ASSOCIATED", "4WAY_HANDSHAKE", "DISCONNECTED",
		"SCANNING", "ASSOCIATING", "ASSOCIATED", "4WAY_HANDSHAKE", "SCANNING")
	err := w.failure("Ev", assocFacts{Secured: true, SSIDVisible: true, Iface: "wlan0"})
	if err == nil || !strings.Contains(err.Error(), "parola yanlış") {
		t.Fatalf("yanlış parola söylenmedi: %v", err)
	}
	// Günlükteki WRONG_KEY tek başına yeter.
	w = izle("SCANNING", "4WAY_HANDSHAKE", "SCANNING")
	err = w.failure("Ev", assocFacts{Secured: true, SSIDVisible: true, WrongKey: true})
	if err == nil || !strings.Contains(err.Error(), "parola yanlış") {
		t.Fatalf("WRONG_KEY günlüğüne rağmen parola denmedi: %v", err)
	}
}

func TestAgBulunamadiAyirtEdilir(t *testing.T) {
	w := izle("SCANNING", "SCANNING", "DISCONNECTED", "SCANNING")
	err := w.failure("Komsu", assocFacts{Secured: true, SSIDVisible: false})
	if err == nil || !strings.Contains(err.Error(), "bulunamadı") {
		t.Fatalf("ağ yok denmedi: %v", err)
	}
}

func TestKartKapaliAyirtEdilir(t *testing.T) {
	w := izle("INTERFACE_DISABLED", "INTERFACE_DISABLED")
	err := w.failure("Ev", assocFacts{Secured: true, Iface: "wlp2s0"})
	if err == nil || !strings.Contains(err.Error(), "etkin değil") || !strings.Contains(err.Error(), "wlp2s0") {
		t.Fatalf("kart kapalı denmedi: %v", err)
	}
}

func TestYalnizcaWPA3AgAyirtEdilir(t *testing.T) {
	sr := "bssid / frequency / signal level / flags / ssid\n" +
		"aa:bb:cc:dd:ee:01\t2437\t-40\t[WPA2-SAE-CCMP][ESS]\tYeniModem\n" +
		"aa:bb:cc:dd:ee:02\t2412\t-50\t[WPA2-PSK+SAE-CCMP][ESS]\tKarma\n" +
		"aa:bb:cc:dd:ee:03\t5180\t-60\t[WPA2-PSK-CCMP][ESS]\tEski\n" +
		"aa:bb:cc:dd:ee:04\t5180\t-60\t[WPA2-SAE-GCMP-256][ESS]\tAltiGHz\n"
	for _, c := range []struct {
		ssid          string
		visible, only bool
	}{
		{"YeniModem", true, true},
		{"Karma", true, false},
		{"Eski", true, false},
		{"AltiGHz", true, true},
		{"Yok", false, false},
	} {
		v, o := saeOnly(sr, c.ssid)
		if v != c.visible || o != c.only {
			t.Errorf("%s: görünür=%v yalnızWPA3=%v, beklenen %v/%v", c.ssid, v, o, c.visible, c.only)
		}
	}
	w := izle("SCANNING", "ASSOCIATING", "SCANNING")
	err := w.failure("YeniModem", assocFacts{Secured: true, SSIDVisible: true, SAEOnly: true})
	if err == nil || !strings.Contains(err.Error(), "WPA3") {
		t.Fatalf("WPA3 nedeni söylenmedi: %v", err)
	}
}

// ── Firmware ───────────────────────────────────────────────────────────────

func TestEksikWiFiFirmwareBulunur(t *testing.T) {
	// Gerçek çekirdek satırları (6.6, iwlwifi/amdgpu/btrtl biçimleri).
	klog := `<3>[    2.101] iwlwifi 0000:00:14.3: Direct firmware load for iwlwifi-ty-a0-gf-a0-83.ucode failed with error -2
<3>[    2.102] iwlwifi 0000:00:14.3: Direct firmware load for iwlwifi-ty-a0-gf-a0-82.ucode failed with error -2
<3>[    2.140] iwlwifi 0000:00:14.3: no suitable firmware found!
<3>[    3.001] amdgpu 0000:03:00.0: Direct firmware load for amdgpu/navi10_sos.bin failed with error -2
<3>[    4.500] Bluetooth: hci0: Direct firmware load for rtl_bt/rtl8852bu_fw.bin failed with error -2
<3>[    5.000] mt7921e 0000:02:00.0: Direct firmware load for mediatek/WIFI_RAM_CODE_MT7961_1.bin failed with error -2
<3>[    5.100] iwlwifi 0000:00:14.3: Direct firmware load for iwlwifi-ty-a0-gf-a0-83.ucode failed with error -2`
	got := wifiFirmware(missingFirmware(klog))
	want := []string{"iwlwifi-ty-a0-gf-a0-83.ucode", "iwlwifi-ty-a0-gf-a0-82.ucode",
		"mediatek/WIFI_RAM_CODE_MT7961_1.bin"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("eksik Wi-Fi firmware'i = %v, beklenen %v", got, want)
	}
}

// ── rfkill (araçsız, sysfs) ─────────────────────────────────────────────────

func sahteRfkill(t *testing.T, root, ad, tur, isim, soft, hard string) string {
	t.Helper()
	d := filepath.Join(root, ad)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, v := range map[string]string{"type": tur, "name": isim, "soft": soft, "hard": hard} {
		if err := os.WriteFile(filepath.Join(d, f), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// Ölçülen hata: "rfkill" ikilisi imajda yok; yumuşak engel HİÇ kalkmıyordu.
func TestRfkillYumusakEngelAracsizKalkar(t *testing.T) {
	root := t.TempDir()
	wlan := sahteRfkill(t, root, "rfkill0", "wlan", "phy0", "1", "0")
	bt := sahteRfkill(t, root, "rfkill1", "bluetooth", "hci0", "1", "0")
	plat := sahteRfkill(t, root, "rfkill2", "wlan", "ideapad_wlan", "1", "0")

	if hard := unblockRfkill(root); len(hard) != 0 {
		t.Fatalf("sert engel yokken %d sert engel bildirildi", len(hard))
	}
	for _, d := range []string{wlan, plat} {
		b, _ := os.ReadFile(filepath.Join(d, "soft"))
		if strings.TrimSpace(string(b)) != "0" {
			t.Errorf("%s: yumuşak engel kalkmadı (soft=%q)", d, b)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(bt, "soft")); strings.TrimSpace(string(b)) != "1" {
		t.Errorf("bluetooth anahtarına dokunulmamalıydı (soft=%q)", b)
	}
}

func TestRfkillSertEngelBildirilir(t *testing.T) {
	root := t.TempDir()
	sahteRfkill(t, root, "rfkill0", "wlan", "hp-wifi", "0", "1")
	hard := unblockRfkill(root)
	if len(hard) != 1 || hard[0].Name != "hp-wifi" {
		t.Fatalf("sert engel bulunamadı: %+v", hard)
	}
	if err := hardBlockError(hard); !strings.Contains(err.Error(), "DONANIMSAL") ||
		!strings.Contains(err.Error(), "hp-wifi") {
		t.Fatalf("sert engel mesajı yetersiz: %v", err)
	}
}
