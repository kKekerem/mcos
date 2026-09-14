//go:build linux

package sshd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Bu dosya SSH sunucusunu gerçekten çalıştırır.
//
// ════════════════════════════════════════════════════════════════════════════
// İKİ SUNUCU DESTEKLENİYOR
// ════════════════════════════════════════════════════════════════════════════
//
// MCOS imajında OpenSSH var (BR2_PACKAGE_OPENSSH=y), ama gömülü sistemlerde
// sık kullanılan dropbear da desteklenebilir. Hangisi varsa o kullanılır ve
// OpenSSH tercih edilir — imajda zaten bulunan budur.
//
// ── Neden kendi yapılandırma dosyamızı yazıyoruz ────────────────────────────
// /etc/ssh/sshd_config sistemin dosyasıdır ve imaj güncellemesinde değişebilir.
// Panelin açıp kapattığı bir hizmetin ayarını oraya yazmak, kullanıcının elle
// yaptığı değişiklikleri sessizce ezmek olurdu. Bizim dosyamız veri klasöründe
// durur ve yalnızca bizim başlattığımız süreç onu kullanır.

// process wraps the running server.
type process struct {
	cmd    *exec.Cmd
	flavor string
}

// serverFlavor is which SSH implementation was found.
type serverFlavor struct {
	bin    string
	kind   string // "openssh" | "dropbear"
	keygen string
}

// findServer locates an SSH server binary, preferring OpenSSH.
func findServer() *serverFlavor {
	for _, p := range []string{"/usr/sbin/sshd", "/usr/bin/sshd", "/sbin/sshd"} {
		if isFile(p) {
			return &serverFlavor{bin: p, kind: "openssh", keygen: findBin("ssh-keygen")}
		}
	}
	if p := findBin("sshd"); p != "" {
		return &serverFlavor{bin: p, kind: "openssh", keygen: findBin("ssh-keygen")}
	}
	for _, p := range []string{"/usr/sbin/dropbear", "/usr/bin/dropbear", "/sbin/dropbear"} {
		if isFile(p) {
			return &serverFlavor{bin: p, kind: "dropbear", keygen: findBin("dropbearkey")}
		}
	}
	if p := findBin("dropbear"); p != "" {
		return &serverFlavor{bin: p, kind: "dropbear", keygen: findBin("dropbearkey")}
	}
	return nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func findBin(name string) string {
	for _, dir := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		p := filepath.Join(dir, name)
		if isFile(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// Available reports whether an SSH server binary exists.
func Available() bool { return findServer() != nil }

// Flavor names the implementation that will be used ("" when none).
func Flavor() string {
	if f := findServer(); f != nil {
		return f.kind
	}
	return ""
}

// start launches the server on the given port.
func (m *Manager) start(port int) error {
	m.mu.Lock()
	already := m.running && m.port == port
	m.mu.Unlock()
	if already {
		return nil
	}
	// Port değiştiyse önce eskisini durdur: iki sunucu aynı porta
	// bağlanamaz ve ikincisi sessizce ölür.
	m.Stop()

	f := findServer()
	if f == nil {
		return ErrUnavailable
	}
	if err := os.MkdirAll(m.hostKeyDir(), 0o700); err != nil {
		return fmt.Errorf("anahtar klasörü oluşturulamadı: %w", err)
	}

	var cmd *exec.Cmd
	var err error
	if f.kind == "openssh" {
		cmd, err = m.opensshCommand(f, port)
	} else {
		cmd, err = m.dropbearCommand(f, port)
	}
	if err != nil {
		return err
	}

	// Kendi süreç grubunda: Stop() tüm grubu öldürebilsin ve daemon'a
	// gelen bir sinyal SSH'ı yanlışlıkla düşürmesin.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("SSH sunucusu başlatılamadı: %w", err)
	}

	m.mu.Lock()
	m.proc = &process{cmd: cmd, flavor: f.kind}
	m.running = true
	m.port = port
	m.mu.Unlock()

	if m.log != nil {
		m.log.Infof("sshd: SSH açık (%s, port %d)", f.kind, port)
	}

	// Süreç kendiliğinden ölürse durumu düzelt; yoksa panel sonsuza dek
	// "çalışıyor" gösterirdi.
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if m.proc != nil && m.proc.cmd == cmd {
			m.running = false
			m.proc = nil
		}
		m.mu.Unlock()
		if m.log != nil {
			if err != nil {
				m.log.Warnf("sshd: SSH sunucusu durdu: %v", err)
			} else {
				m.log.Infof("sshd: SSH sunucusu durdu")
			}
		}
	}()
	return nil
}

// ── OpenSSH ─────────────────────────────────────────────────────────────────

// opensshHostKeys are the key files OpenSSH uses.
var opensshHostKeys = []struct{ name, typ string }{
	{"ssh_host_ed25519_key", "ed25519"},
	{"ssh_host_rsa_key", "rsa"},
}

func (m *Manager) opensshCommand(f *serverFlavor, port int) (*exec.Cmd, error) {
	keys, err := m.ensureOpenSSHKeys(f.keygen)
	if err != nil {
		return nil, err
	}
	cfgPath, err := m.writeSSHDConfig(port, keys)
	if err != nil {
		return nil, err
	}
	// -D: ön planda (süreci biz yönetiyoruz). -e: günlükler stderr'e.
	// -f: KENDİ yapılandırmamız, sistemin /etc/ssh/sshd_config'i değil.
	return exec.Command(f.bin, "-D", "-e", "-f", cfgPath), nil
}

func (m *Manager) ensureOpenSSHKeys(keygen string) ([]string, error) {
	dir := m.hostKeyDir()
	var out []string
	for _, k := range opensshHostKeys {
		path := filepath.Join(dir, k.name)
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			out = append(out, path)
			continue
		}
		if keygen == "" {
			continue
		}
		// -N "": parolasız anahtar. Sunucu anahtarına parola koymak,
		// açılışta kimsenin yazamayacağı bir parola sormak demektir.
		cmd := exec.Command(keygen, "-q", "-t", k.typ, "-f", path, "-N", "")
		if b, err := cmd.CombinedOutput(); err != nil {
			if m.log != nil {
				m.log.Warnf("sshd: %s anahtarı üretilemedi: %v (%s)",
					k.typ, err, strings.TrimSpace(string(b)))
			}
			continue
		}
		out = append(out, path)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sunucu anahtarı üretilemedi (ssh-keygen yok mu?)")
	}
	return out, nil
}

// writeSSHDConfig writes our own sshd_config and returns its path.
func (m *Manager) writeSSHDConfig(port int, keys []string) (string, error) {
	var b strings.Builder
	b.WriteString("# MCOS tarafından üretildi — elle düzenlemeyin.\n")
	b.WriteString("# Panelden SSH'ı kapatıp açmak bu dosyayı yeniden yazar.\n")
	fmt.Fprintf(&b, "Port %d\n", port)
	for _, k := range keys {
		fmt.Fprintf(&b, "HostKey %s\n", k)
	}
	// Bu cihazda tek kullanıcı var: root. Başka bir kullanıcı yaratmak,
	// sunucu dosyalarına erişemeyen ve hiçbir işe yaramayan bir hesap
	// üretmek olurdu.
	b.WriteString("PermitRootLogin yes\n")
	b.WriteString("AuthorizedKeysFile " + authorizedKeysPath + "\n")
	b.WriteString("PubkeyAuthentication yes\n")
	b.WriteString("PasswordAuthentication yes\n")
	// Boş parolayla giriş ASLA: /etc/shadow'da parola kurulmamışsa
	// SSH kapısı herkese açık olurdu.
	b.WriteString("PermitEmptyPasswords no\n")
	b.WriteString("ChallengeResponseAuthentication no\n")
	b.WriteString("KbdInteractiveAuthentication no\n")
	b.WriteString("UsePAM no\n")
	b.WriteString("PrintMotd no\n")
	fmt.Fprintf(&b, "PidFile %s\n", filepath.Join(m.hostKeyDir(), "sshd.pid"))
	// Yavaş bir mini PC'de ters DNS araması girişleri 30 saniye
	// geciktirebilir; kapalı.
	b.WriteString("UseDNS no\n")
	if sftp := findSFTPServer(); sftp != "" {
		// sftp: telefondan/masaüstünden dosya kopyalamayı mümkün kılar.
		fmt.Fprintf(&b, "Subsystem sftp %s\n", sftp)
	}

	path := filepath.Join(m.hostKeyDir(), "sshd_config")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return "", fmt.Errorf("sshd yapılandırması yazılamadı: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("sshd yapılandırması yazılamadı: %w", err)
	}
	return path, nil
}

func findSFTPServer() string {
	for _, p := range []string{
		"/usr/libexec/sftp-server",
		"/usr/lib/openssh/sftp-server",
		"/usr/libexec/openssh/sftp-server",
		"/usr/lib/ssh/sftp-server",
	} {
		if isFile(p) {
			return p
		}
	}
	return ""
}

// ── dropbear ────────────────────────────────────────────────────────────────

var dropbearKeys = []struct{ name, typ string }{
	{"dropbear_ed25519_host_key", "ed25519"},
	{"dropbear_rsa_host_key", "rsa"},
}

func (m *Manager) dropbearCommand(f *serverFlavor, port int) (*exec.Cmd, error) {
	dir := m.hostKeyDir()
	args := []string{"-F", "-E", "-p", strconv.Itoa(port)}

	found := 0
	for _, k := range dropbearKeys {
		path := filepath.Join(dir, k.name)
		if st, err := os.Stat(path); err != nil || st.Size() == 0 {
			if f.keygen == "" {
				continue
			}
			cmd := exec.Command(f.keygen, "-t", k.typ, "-f", path)
			if b, err := cmd.CombinedOutput(); err != nil {
				if m.log != nil {
					m.log.Warnf("sshd: %s anahtarı üretilemedi: %v (%s)",
						k.typ, err, strings.TrimSpace(string(b)))
				}
				continue
			}
		}
		args = append(args, "-r", path)
		found++
	}
	if found == 0 {
		// -R: dropbear anahtarı kendi üretsin. Kalıcı olmaz ama hiç
		// çalışmamaktan iyidir; kullanıcı uyarıyı günlükte görür.
		args = append(args, "-R")
		if m.log != nil {
			m.log.Warnf("sshd: kalıcı anahtar üretilemedi — her açılışta " +
				"yeni anahtar kullanılacak")
		}
	}
	return exec.Command(f.bin, args...), nil
}

// ── Ortak ───────────────────────────────────────────────────────────────────

// Stop terminates the server if it is running.
func (m *Manager) Stop() {
	m.mu.Lock()
	p := m.proc
	m.proc = nil
	m.running = false
	m.mu.Unlock()

	if p == nil || p.cmd.Process == nil {
		return
	}
	// Süreç GRUBUNU öldür (negatif pid): SSH sunucusu her bağlantı için
	// çocuk süreç oluşturur; yalnızca ana süreci öldürmek açık oturumları
	// arkada bırakırdı.
	if pgid, err := syscall.Getpgid(p.cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = p.cmd.Process.Kill()
	}
}

// SetPassword sets the root login password.
//
// ── Neden chpasswd ──────────────────────────────────────────────────────────
// Parolayı /etc/shadow'a doğru biçimde (tuzlanmış, doğru algoritma, doğru
// alan sayısı) yazmak elle yapılacak bir iş değil. chpasswd BusyBox'ta da
// vardır ve bunu doğru yapar.
//
// Parola STDIN'den verilir, komut satırından DEĞİL: komut satırı argümanları
// /proc üzerinden makinedeki her sürece görünür.
func SetPassword(user, password string) error {
	if user == "" {
		user = "root"
	}
	if strings.ContainsAny(password, "\n:") {
		// chpasswd girdisi "kullanıcı:parola" satırlarıdır; iki nokta ya da
		// satır sonu o biçimi bozar.
		return fmt.Errorf("parola iki nokta veya satır sonu içeremez")
	}
	bin := findBin("chpasswd")
	if bin == "" {
		return fmt.Errorf("chpasswd bulunamadı")
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(user + ":" + password + "\n")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("parola ayarlanamadı: %v (%s)", err,
			strings.TrimSpace(string(b)))
	}
	return nil
}

// localAddresses lists the addresses a user can ssh to.
func localAddresses() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		if ip4 := ipn.IP.To4(); ip4 != nil {
			out = append(out, ip4.String())
		}
	}
	return out
}
