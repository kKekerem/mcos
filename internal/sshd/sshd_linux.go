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
	"time"
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

// binDirs is where findBin looks before $PATH.
//
// Değişken, çünkü sınama bunu boşaltıp "hiçbir harici program yokken de
// parola konabiliyor mu" diye bakıyor. Geliştirme makinesinde
// /usr/sbin/chpasswd VAR; imajda YOK. Sabit liste, chpasswd'a geri dönen bir
// gerilemeyi geliştirme makinesinde görünmez kılıyordu (karşı-sınamada
// yakalandı).
var binDirs = []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"}

func findBin(name string) string {
	for _, dir := range binDirs {
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

// passwordSupported: parola yalnızca cihazda (Linux) ayarlanabilir.
const passwordSupported = true

// Available reports whether an SSH server binary exists.
func Available() bool { return findServer() != nil }

// Flavor names the implementation that will be used ("" when none).
func Flavor() string {
	if f := findServer(); f != nil {
		return f.kind
	}
	return ""
}

// earlyExitWindow is how long start waits to catch a server that dies at once.
//
// ── Neden bekliyoruz ────────────────────────────────────────────────────────
// sshd yapılandırma ya da port hatasında (ör. "Bind to port 22 ... Address
// already in use") başladıktan milisaniyeler sonra çıkar. Eskiden start()
// cmd.Start() başarılı diye nil dönüyordu: panel "SSH açıldı" diyor, bir
// sonraki bakışta "açık ama çalışmıyor" gösteriyor ve NEDENİ hiçbir yerde
// görünmüyordu (yalnızca /run/mcos/mcosd.log'da).
var earlyExitWindow = 700 * time.Millisecond

// start launches the server on the given port.
//
// pwAuth false ise parola girişi sunucu düzeyinde KAPATILIR (bkz. sshd.go:
// kalıcı parola yokken shadow'da imajın varsayılanı "root" durur).
func (m *Manager) start(port int, pwAuth bool) error {
	m.mu.Lock()
	already := m.running && m.port == port && m.pwAuth == pwAuth
	m.mu.Unlock()
	if already {
		return nil
	}
	// Port ya da parola ayarı değiştiyse önce eskisini durdur: iki sunucu
	// aynı porta bağlanamaz ve ikincisi sessizce ölür.
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
		cmd, err = m.opensshCommand(f, port, pwAuth)
	} else {
		cmd, err = m.dropbearCommand(f, port, pwAuth)
	}
	if err != nil {
		return err
	}

	// Kendi süreç grubunda: Stop() tüm grubu öldürebilsin ve daemon'a
	// gelen bir sinyal SSH'ı yanlışlıkla düşürmesin.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Çıktı yine mcosd günlüğüne gider; son satırları ayrıca tutulur ki
	// sunucu ölürse neden öldüğü panelde gösterilebilsin.
	tail := newTailBuffer(os.Stderr, 4096)
	cmd.Stdout = tail
	cmd.Stderr = tail

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("SSH sunucusu başlatılamadı: %w", err)
	}

	m.mu.Lock()
	m.proc = &process{cmd: cmd, flavor: f.kind}
	m.running = true
	m.port = port
	m.pwAuth = pwAuth
	m.lastErr = ""
	m.mu.Unlock()

	// Süreç kendiliğinden ölürse durumu düzelt; yoksa panel sonsuza dek
	// "çalışıyor" gösterirdi.
	// Kapanma nedeni kanaldan gelir: Stop() ile yarışta m.lastErr boş
	// kalabilir ama erken ölümün nedeni yine de kaybolmamalı.
	done := make(chan string, 1)
	go func() {
		err := cmd.Wait()
		why := exitReason(err, tail.LastLines(2))
		m.mu.Lock()
		unexpected := m.proc != nil && m.proc.cmd == cmd
		if unexpected {
			// Stop() proc'u önceden nil yapar; buraya yalnızca KENDİLİĞİNDEN
			// ölen bir sunucu için gelinir.
			m.running = false
			m.proc = nil
			m.lastErr = why
		}
		m.mu.Unlock()
		done <- why
		if m.log == nil {
			return
		}
		if unexpected {
			m.log.Warnf("sshd: SSH sunucusu durdu: %s", why)
		} else {
			// Bizim durdurmamız (kapatma, port/parola değişimi) bir uyarı
			// değil; günlükte "durdu" uyarısı gerçek çökmeleri gölgeliyordu.
			m.log.Infof("sshd: SSH sunucusu durduruldu")
		}
	}()

	select {
	case why := <-done:
		return fmt.Errorf("SSH sunucusu başlar başlamaz kapandı: %s", why)
	case <-time.After(earlyExitWindow):
	}

	if m.log != nil {
		auth := "yalnızca anahtar"
		if pwAuth {
			auth = "parola + anahtar"
		}
		m.log.Infof("sshd: SSH açık (%s, port %d, giriş: %s)", f.kind, port, auth)
	}
	return nil
}

// exitReason turns a Wait error plus the last stderr line into one sentence.
func exitReason(err error, last string) string {
	switch {
	case last != "" && err != nil:
		return fmt.Sprintf("%s (%v)", last, err)
	case last != "":
		return last
	case err != nil:
		return err.Error()
	}
	return "çıkış kodu 0"
}

// ── OpenSSH ─────────────────────────────────────────────────────────────────

// opensshHostKeys are the key files OpenSSH uses.
var opensshHostKeys = []struct{ name, typ string }{
	{"ssh_host_ed25519_key", "ed25519"},
	{"ssh_host_rsa_key", "rsa"},
}

func (m *Manager) opensshCommand(f *serverFlavor, port int, pwAuth bool) (*exec.Cmd, error) {
	keys, err := m.ensureOpenSSHKeys(f.keygen)
	if err != nil {
		return nil, err
	}
	cfgPath, err := m.writeSSHDConfig(port, keys, pwAuth)
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
func (m *Manager) writeSSHDConfig(port int, keys []string, pwAuth bool) (string, error) {
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
	// Parola girişi YALNIZCA kullanıcının koyduğu kalıcı bir parola varken
	// açık. Yoksa /etc/shadow'da imajın varsayılanı ("root") durur ve
	// yalnızca anahtar ekleyip SSH'ı açan kullanıcının makinesine aynı
	// ağdaki herkes root/root ile girebiliyordu (QEMU'da ölçüldü).
	if pwAuth {
		b.WriteString("PasswordAuthentication yes\n")
	} else {
		b.WriteString("PasswordAuthentication no\n")
	}
	// Boş parolayla giriş ASLA: /etc/shadow'da parola kurulmamışsa
	// SSH kapısı herkese açık olurdu.
	b.WriteString("PermitEmptyPasswords no\n")
	b.WriteString("ChallengeResponseAuthentication no\n")
	b.WriteString("KbdInteractiveAuthentication no\n")
	// "UsePAM no" YAZILMIYOR: imajdaki sshd PAM'siz derlenmiş ve her
	// başlangıçta "Unsupported option UsePAM" uyarısı basıyordu (QEMU'da
	// görüldü). OpenSSH'ın derleme varsayılanı zaten "no".
	b.WriteString("PrintMotd no\n")
	fmt.Fprintf(&b, "PidFile %s\n", filepath.Join(m.hostKeyDir(), "sshd.pid"))
	// Yavaş bir mini PC'de ters DNS araması girişleri 30 saniye
	// geciktirebilir; kapalı.
	b.WriteString("UseDNS no\n")
	if sftp := findSFTPServer(); sftp != "" {
		// sftp: telefondan/masaüstünden dosya kopyalamayı mümkün kılar.
		// -d: WinSCP doğrudan sunucular klasöründe açılsın (kullanıcının
		// isteği: "bizi sunucular klasörünü göstersin"). Kök kilitlenmez
		// (chroot yok): klasördeki adlı bağlar /data/servers'a gider ve
		// chroot'ta çözülemezdi.
		fmt.Fprintf(&b, "Subsystem sftp %s -d %s\n", sftp, filepath.Join(m.dataDir, SFTPDirName))
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

func (m *Manager) dropbearCommand(f *serverFlavor, port int, pwAuth bool) (*exec.Cmd, error) {
	dir := m.hostKeyDir()
	args := []string{"-F", "-E", "-p", strconv.Itoa(port)}
	if !pwAuth {
		// -s: parola girişi kapalı (OpenSSH'taki PasswordAuthentication no
		// ile aynı gerekçe; bkz. writeSSHDConfig).
		args = append(args, "-s")
	}

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

// dataVolatile reports whether dir is on a RAM filesystem (lost at reboot).
func dataVolatile(dir string) bool {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	return volatileMount(string(b), dir)
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
