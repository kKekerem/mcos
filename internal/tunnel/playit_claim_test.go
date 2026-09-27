package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sahteAnahtar = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"

// Kullanıcı: "siteye giriyorum, ajanın çevrimiçi olmasını bekliyor". Onay
// verildiğinde "claim exchange" anahtarı stdout'a basar; eskiden bu çıktı
// hiçbir yere bağlı değildi, anahtar kayboluyor ve ajan hiç başlamıyordu.
func TestOnaySonrasiAnahtarKaydedilir(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "playit-cli")
	betik := "#!/bin/sh\ncase \"$1 $2\" in\n" +
		"'claim generate') echo 0123456789 ;;\n" +
		"'claim url') echo https://playit.gg/claim/$3?type=self-managed ;;\n" +
		"'claim exchange') echo 'bilgi: onay bekleniyor' >&2; echo " + sahteAnahtar + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(cli, []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "playitd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	eskiCLI, eskiD, eskiS := playitCLIBin, playitDaemonBin, SecretPath
	playitCLIBin, playitDaemonBin = cli, filepath.Join(dir, "playitd")
	SecretPath = filepath.Join(dir, "veri", "playit.toml")
	defer func() { playitCLIBin, playitDaemonBin, SecretPath = eskiCLI, eskiD, eskiS }()

	c, err := StartClaim()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.URL, "type=self-managed") {
		t.Fatalf("claim adresi CLI'den alınmadı: %s", c.URL)
	}
	son := time.Now().Add(5 * time.Second)
	for time.Now().Before(son) {
		if done, err := c.Done(); done {
			if err != nil {
				t.Fatalf("onay hatası: %v", err)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !Claimed() {
		t.Fatal("onay bitti ama gizli anahtar kaydedilmedi (ajan başlatılamaz)")
	}
	b, _ := os.ReadFile(SecretPath)
	if !strings.Contains(string(b), sahteAnahtar) {
		t.Fatalf("kaydedilen dosyada anahtar yok: %q", b)
	}
}

// gercekBekleme, playit-cli 1.0.10 "claim exchange 31944a6585 --wait 5"
// komutunun GERÇEK stdout'u (2026-09-27, onaysız). Ölçülenler: kod 10
// onaltılık hane, "claim url" yalın "https://playit.gg/claim/<kod>" basar,
// exchange her ~10 sn'de bu iki satırı yineler, stderr boştur ve --wait 5
// YOK SAYILIR (150 sn sonra hâlâ bekliyordu; bu yüzden zaman aşımına
// güvenmiyoruz, --wait 0 ile başlatıp iptali Cancel'a bırakıyoruz).
const gercekBekleme = "Open this link to finish setting up playit:\n" +
	"https://playit.gg/claim/31944a6585\n" +
	"Open this link to finish setting up playit:\n" +
	"https://playit.gg/claim/31944a6585\n"

func TestAnahtarAyiklama(t *testing.T) {
	for girdi, beklenen := range map[string]string{
		sahteAnahtar + "\n":                       sahteAnahtar,
		"günlük satırı\n" + sahteAnahtar + "\n":   sahteAnahtar,
		"secret_key = \"" + sahteAnahtar + "\"\n": sahteAnahtar,
		"hata: kısa abc\n":                        "",
		// Onay beklerken basılanlar anahtar SANILMAMALI (URL, 10 haneli kod).
		gercekBekleme: "",
		// Onaydan sonra anahtar bekleme satırlarının ARDINDAN gelir.
		gercekBekleme + sahteAnahtar + "\n": sahteAnahtar,
	} {
		if got := parseSecret(girdi); got != beklenen {
			t.Errorf("parseSecret(%q) = %q, beklenen %q", girdi, got, beklenen)
		}
	}
}
