// Package tunnel manages public tunnels so a server can be reached from the
// internet without port forwarding.
//
// ════════════════════════════════════════════════════════════════════════════
// SERVEO → PLAYIT GEÇİŞİ
// ════════════════════════════════════════════════════════════════════════════
//
// Eski uygulama Serveo kullanıyordu: `ssh -R 0:localhost:PORT serveo.net`.
// Sorunları: üçüncü tarafa SSH bağımlılığı, sık kesintiler, adresin log
// satırından ayrıştırılması, ve hesap/denetim yokluğu.
//
// playit.gg bunların hepsini çözer ama MİMARİSİ FARKLIDIR ve bu fark
// tasarımı belirler:
//
//  1. TÜNEL YAPILANDIRMA DOSYASI YOKTUR. 1.0.x ajanının yapılandırması
//     YALNIZCA gizli anahtarı tutar:
//
//     secret_key = "74f8...6509"
//
//     Tünellerin kendisi (hangi yerel porta gideceği dahil) playit'in
//     BULUTUNDA saklanır ve ajana oradan iletilir. İnternette görülen
//     "[[mappings]]" örnekleri çok eski 0.15 sürümüne aittir.
//
//  2. TARAYICI TAM BİR KEZ ZORUNLUDUR. Ajanı bir hesaba bağlamak için
//     kullanıcının playit.gg/claim/<kod> adresini açıp onaylaması gerekir.
//     Bu atlanamaz — hesap sahipliği doğrulaması. Ondan SONRA her şey
//     (tünel açma, adresi okuma) gizli anahtarla otomatiktir.
//
// Yani "tamamen otomatik yapılandırma" TEK bir onay adımı dışında mümkün.
// Kullanıcıya bunu dürüstçe söylüyoruz: ekranda kod ve adres gösterilir,
// telefondan onaylanır, gerisi kendiliğinden olur.
//
// ── İkili dosyalar ──────────────────────────────────────────────────────────
// playit 1.0 ikiye bölünmüştür:
//
//	playitd      arka plan servisi (tüneli yürüten)
//	playit-cli   komut satırı (claim akışı, durum sorgulama)
//
// İkisi de statik-pie musl derlemesidir: hiçbir libc bağımlılığı yoktur,
// Buildroot rootfs'ine olduğu gibi düşer. CA sertifikası da gerekmez —
// rustls kök sertifikaları ikiliye gömülü gelir.
package tunnel

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// playitd / playit-cli binary locations inside the image.
const (
	playitDaemonBin = "/usr/bin/playitd"
	playitCLIBin    = "/usr/bin/playit-cli"

	// SecretPath is where the agent secret lives.
	//
	// playitd çözümleme sırası: ./playit.toml → /etc/playit/playit.toml
	// (YALNIZCA zaten varsa) → ~/.config/playit_gg/playit.toml.
	//
	// /data altını seçiyoruz çünkü KALICI bölümdür: RAM'de çalışan canlı
	// sistemde bile yeniden başlatmayı atlatır. Yol açıkça --secret-path
	// ile veriliyor, böylece çözümleme sırasına hiç güvenmiyoruz.
	SecretPath = "/data/mcos/playit.toml"
)

// ErrNotInstalled is returned when the playit binaries are missing.
var ErrNotInstalled = errors.New("playit ajanı bu imajda yok (make offline-bundle ile gömülür)")

// ErrNotClaimed is returned when the agent has no secret yet.
var ErrNotClaimed = errors.New("playit hesabı henüz bağlanmadı")

// PlayitAvailable reports whether the agent binaries are present.
func PlayitAvailable() bool {
	for _, p := range []string{playitDaemonBin, playitCLIBin} {
		if st, err := os.Stat(p); err != nil || st.Mode()&0o111 == 0 {
			return false
		}
	}
	return true
}

// Claimed reports whether an agent secret has been stored.
func Claimed() bool {
	b, err := os.ReadFile(SecretPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "secret_key")
}

// Claim is an in-progress account binding.
type Claim struct {
	// Code is the 10-character hex claim code.
	Code string
	// URL is what the user must open in a browser.
	URL string

	cmd  *exec.Cmd
	mu   sync.Mutex
	done bool
	err  error
}

// StartClaim begins the account-binding flow.
//
// AKIŞ:
//  1. playit-cli claim generate  → 10 haneli onay kodu
//  2. Kullanıcı https://playit.gg/claim/<kod> adresini açar ve onaylar
//  3. playit-cli claim exchange  → gizli anahtar döner, dosyaya yazılır
//
// 2. adım atlanamaz: ajanı bir hesaba bağlamak için hesap sahibinin
// onayı gerekir. Panel kodu ve adresi ekranda gösterir.
func StartClaim() (*Claim, error) {
	if !PlayitAvailable() {
		return nil, ErrNotInstalled
	}

	out, err := exec.Command(playitCLIBin, "claim", "generate").Output()
	if err != nil {
		return nil, fmt.Errorf("onay kodu üretilemedi: %w", err)
	}
	code := strings.TrimSpace(string(out))
	// Kod tam olarak 10 onaltılık hanedir; başka bir şey geldiyse ajan
	// beklenmedik bir sürümdür ve devam etmek yanıltıcı olur.
	if len(code) != 10 {
		return nil, fmt.Errorf("beklenmeyen onay kodu biçimi: %q", code)
	}

	c := &Claim{
		Code: code,
		URL:  "https://playit.gg/claim/" + code,
	}

	// Değişimi arka planda bekle. --wait 0 = süresiz: kod, ajan beklediği
	// sürece geçerli kalır (sunucu tarafı her yoklamada tazeler).
	c.cmd = exec.Command(playitCLIBin, "claim", "exchange", code, "--wait", "0")
	if err := c.cmd.Start(); err != nil {
		return nil, fmt.Errorf("onay beklenemedi: %w", err)
	}
	go c.wait()
	return c, nil
}

// wait collects the exchange result.
func (c *Claim) wait() {
	err := c.cmd.Wait()
	c.mu.Lock()
	c.done = true
	c.err = err
	c.mu.Unlock()
}

// Done reports whether the claim finished, and with what error.
func (c *Claim) Done() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done, c.err
}

// Cancel aborts a pending claim.
func (c *Claim) Cancel() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// SaveSecret writes the agent secret to the persistent path.
//
// Dosya 0600 ile yazılır: bu anahtar hesabın tamamına erişim verir.
func SaveSecret(secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return errors.New("boş gizli anahtar")
	}
	if err := os.MkdirAll(filepath.Dir(SecretPath), 0o700); err != nil {
		return err
	}
	// Önce geçici dosyaya yaz, sonra taşı: yarım yazılmış bir anahtar
	// ajanı her açılışta hata verdirirdi.
	tmp := SecretPath + ".tmp"
	body := fmt.Sprintf("secret_key = %q\n", secret)
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, SecretPath)
}

// PlayitAgent supervises the playitd daemon.
type PlayitAgent struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
	lastLog []string
	addr    string
}

// maxAgentLog is how many daemon log lines are kept for the UI.
const maxAgentLog = 200

// Start launches playitd with the stored secret.
func (a *PlayitAgent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil
	}
	if !PlayitAvailable() {
		return ErrNotInstalled
	}
	if !Claimed() {
		return ErrNotClaimed
	}

	// --secret-path ile açık yol veriyoruz: playitd'nin kendi çözümleme
	// sırasına (CWD → /etc → ~/.config) güvenmek, çalışma dizini değişince
	// sessizce başka bir anahtar kullanmak demektir.
	cmd := exec.Command(playitDaemonBin, "--secret-path", SecretPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("playitd başlatılamadı: %w", err)
	}
	a.cmd = cmd
	a.running = true
	a.lastLog = nil

	go a.readLog(stdout)
	go func() {
		_ = cmd.Wait()
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()
	return nil
}

// readLog captures daemon output and extracts the public address.
func (a *PlayitAgent) readLog(r interface{ Read([]byte) (int, error) }) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		a.mu.Lock()
		a.lastLog = append(a.lastLog, line)
		if len(a.lastLog) > maxAgentLog {
			a.lastLog = a.lastLog[len(a.lastLog)-maxAgentLog:]
		}
		// Genel adres günlükte görünür. API'den okumak daha güvenilir
		// olurdu ama bu, ajan çalışır çalışmaz bir şey göstermeyi sağlar.
		if h := parsePlayitAddr(line); h != "" {
			a.addr = h
		}
		a.mu.Unlock()
	}
}

// parsePlayitAddr extracts a "something.playit.gg:port" style address.
func parsePlayitAddr(line string) string {
	for _, f := range strings.Fields(line) {
		f = strings.Trim(f, "\"',()[]")
		if strings.Contains(f, ".playit.gg") ||
			strings.Contains(f, ".craft.ply.gg") ||
			strings.Contains(f, ".ply.gg") {
			return f
		}
	}
	return ""
}

// Stop terminates the agent.
func (a *PlayitAgent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.cmd == nil || a.cmd.Process == nil {
		return nil
	}
	// Önce nazikçe: playitd SIGTERM'de tünelleri düzgünce kapatır.
	_ = a.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = a.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = a.cmd.Process.Kill()
	}
	a.running = false
	return nil
}

// Status reports the agent state for the UI.
func (a *PlayitAgent) Status() (running bool, addr string, log []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.lastLog))
	copy(out, a.lastLog)
	return a.running, a.addr, out
}
