// Package sshd manages the appliance's SSH server.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN VAR
// ════════════════════════════════════════════════════════════════════════════
//
// MCOS'un kendi paneli ve telefon uygulaması çoğu işi görür, ama bazı işler
// için kabuk gerekir: bir günlük dosyasına bakmak, elle bir dosya kopyalamak,
// bir sorunu ayıklamak. Kullanıcının isteği buydu: "ssh ile bağlanıp kontrol
// etme ve uzaktan kontrol ekle".
//
// ── Neden init betiği değil de daemon ───────────────────────────────────────
// SSH sunucusunu bir açılış betiğinden başlatmak, onu panelden AÇIP
// KAPATMAYI imkânsız kılardı: kullanıcı her seferinde yeniden başlatmak
// zorunda kalırdı. Daemon zaten süreç yönetiyor; SSH de öyle yönetiliyor.
//
// ── Neden dropbear ──────────────────────────────────────────────────────────
// OpenSSH bu imaj için çok büyük (birkaç MB + OpenSSL). dropbear tek bir
// ~250 KB ikilidir, gömülü sistemlerin standardıdır ve aynı protokolü
// konuşur: kullanıcı sıradan bir "ssh" istemcisiyle bağlanır.
package sshd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcos/internal/log"
	"mcos/internal/model"
)

// ErrUnavailable means no SSH server binary is present.
//
// Bu bir HATA DEĞİL, bir durum: geliştirme makinesinde ya da dropbear'sız
// derlenmiş bir imajda beklenen şey budur. Panel bunu "kullanılamıyor" diye
// gösterir, "çöktü" diye değil.
var ErrUnavailable = errors.New("SSH sunucusu bu imajda yok")

// Status is what the panel shows.
type Status struct {
	Available   bool   `json:"available"`
	Running     bool   `json:"running"`
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	PasswordSet bool   `json:"passwordSet"`
	Keys        int    `json:"keys"`
	User        string `json:"user"`
	// Flavor: hangi SSH sunucusu kullaniliyor ("openssh" / "dropbear").
	Flavor    string   `json:"flavor,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// Manager owns the SSH server process.
type Manager struct {
	mu sync.Mutex

	dataDir string
	log     *log.Logger
	// shadowPath, parolanın yazıldığı shadow dosyası. Cihazda /etc/shadow;
	// sınamalar geçici bir dosya verir ki geliştirme makinesinin gerçek
	// shadow'una asla dokunulmasın.
	shadowPath string

	proc    *process
	running bool
	port    int
	// pwAuth, çalışan sunucunun parola girişine izin verip vermediği.
	// Parola sonradan konursa sunucu bu yüzden yeniden başlatılır.
	pwAuth bool
	// lastErr, sunucunun kendiliğinden kapanma nedeni (stderr'inin son
	// satırı). Panel "açık ama çalışmıyor" yerine NEDENİ gösterebilsin.
	lastErr string
}

// New creates a manager. dataDir must be persistent: host keys live there and
// must survive reboots, otherwise every connection warns about a changed key.
func New(dataDir string, lg *log.Logger) *Manager {
	return &Manager{dataDir: dataDir, log: lg, shadowPath: "/etc/shadow"}
}

// ════════════════════════════════════════════════════════════════════════════
// PAROLA — ve neden /etc/shadow'a yazmak YETMİYOR
// ════════════════════════════════════════════════════════════════════════════
//
// MCOS'un kök dosya sistemi RAM'deki initramfs'tir: /etc/shadow her açılışta
// imajdaki hâline döner. İmajdaki kök parolası Buildroot'un varsayılanı olan
// "root"tur (BR2_TARGET_GENERIC_ROOT_PASSWD). Yani parolayı yalnızca
// /etc/shadow'a yazmak, yeniden başlatınca kullanıcının parolasını SİLİP
// herkesin bildiği "root" parolasını geri getirmek demekti.
//
// Bu yüzden parolanın ÖZETİ (asla kendisi değil) kalıcı veri klasöründe,
// sunucu anahtarlarının yanında 0600 izinle saklanır ve SSH her
// başlatıldığında /etc/shadow'a geri yazılır. config.json'a DEĞİL: o dosya
// yedeklenir, kopyalanır ve RPC ile okunabilir.
//
// Kalıcı bir parola yoksa sunucu PAROLA GİRİŞİNİ KAPATIR. Ölçülen açık:
// kullanıcı yalnızca anahtar ekleyip SSH'ı açtığında, aynı ağdaki herkes
// "root"/"root" ile kök kabuğu alabiliyordu (QEMU'da gerçek imajla denendi).

// passwordFile is the persisted root hash, relative to hostKeyDir.
const passwordFile = "root-password.hash"

func (m *Manager) passwordPath() string {
	return filepath.Join(m.hostKeyDir(), passwordFile)
}

// storedHash returns the persisted root hash, or "" when none is stored.
func (m *Manager) storedHash() (string, error) {
	b, err := os.ReadFile(m.passwordPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("kayıtlı parola okunamadı: %w", err)
	}
	h := strings.TrimSpace(string(b))
	if !validShadowHash(h) {
		return "", fmt.Errorf("kayıtlı parola dosyası bozuk (%s) — parolayı yeniden koyun",
			m.passwordPath())
	}
	return h, nil
}

// HasPassword reports whether a usable root password is persisted.
func (m *Manager) HasPassword() bool {
	h, err := m.storedHash()
	return err == nil && h != ""
}

// SetPassword sets the root login password and persists its hash.
//
// Özet saf Go ile hesaplanır (shacrypt.go): harici chpasswd'a bağımlılık
// kalmadı. Önce /etc/shadow yazılır, sonra kalıcı kopya: shadow yazılamazsa
// kalıcı dosya hiç değişmez ve eski parola geçerli kalır.
func (m *Manager) SetPassword(user, password string) error {
	if !passwordSupported {
		return errors.New("parola yalnızca MCOS cihazında ayarlanabilir")
	}
	if user == "" {
		user = "root"
	}
	if user != "root" {
		// Kalıcı özet yalnızca kök için tutuluyor; başka bir kullanıcının
		// parolası yeniden başlatınca sessizce kaybolurdu.
		return fmt.Errorf("yalnızca root kullanıcısının parolası ayarlanabilir")
	}
	if password == "" {
		return errors.New("parola boş olamaz")
	}
	if strings.ContainsAny(password, "\n\r\x00") {
		// SSH istemcisi parolayı bir satır olarak okur: satır sonu içeren bir
		// parola hiçbir zaman yazılamaz, yani giriş imkânsız olurdu.
		return errors.New("parola satır sonu içeremez")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if err := writeShadowHash(m.shadowPath, user, hash, time.Now()); err != nil {
		return fmt.Errorf("parola ayarlanamadı: %w", err)
	}
	if err := os.MkdirAll(m.hostKeyDir(), 0o700); err != nil {
		return fmt.Errorf("parola şimdilik geçerli ama kalıcı kaydedilemedi "+
			"(yeniden başlatınca silinir): %w", err)
	}
	if err := writeFileAtomic(m.passwordPath(), []byte(hash+"\n"), 0o600); err != nil {
		return fmt.Errorf("parola şimdilik geçerli ama kalıcı kaydedilemedi "+
			"(yeniden başlatınca silinir): %w", err)
	}
	if m.log != nil {
		m.log.Infof("sshd: root parolası ayarlandı (SHA-512, kalıcı: %s)", m.passwordPath())
	}
	return nil
}

// restorePassword writes the persisted hash back into the shadow file.
//
// Dönüş: kalıcı bir parola var ve UYGULANDI mı. Uygulanamadıysa shadow'da
// imajın varsayılanı duruyor demektir ve parola girişi AÇILMAMALIDIR.
func (m *Manager) restorePassword() (bool, error) {
	h, err := m.storedHash()
	if err != nil || h == "" {
		return false, err
	}
	if err := writeShadowHash(m.shadowPath, "root", h, time.Now()); err != nil {
		return false, fmt.Errorf("kayıtlı parola uygulanamadı: %w", err)
	}
	return true, nil
}

// hostKeyDir is where dropbear's host keys are kept.
func (m *Manager) hostKeyDir() string { return filepath.Join(m.dataDir, "ssh") }

// authorizedKeysPath is where the user's public keys go.
//
// Kök kullanıcının kendi ~/.ssh dizini kullanılıyor: dropbear oraya bakar ve
// başka bir yol vermek için yeniden derlemek gerekir.
const authorizedKeysPath = "/root/.ssh/authorized_keys"

// Status reports the current state.
func (m *Manager) Status(cfg model.SSHConfig) Status {
	cfg = cfg.Normalize()
	m.mu.Lock()
	running := m.running
	lastErr := m.lastErr
	m.mu.Unlock()

	h, pwErr := m.storedHash()
	hasPw := pwErr == nil && h != ""
	st := Status{
		Available: Available(),
		Running:   running,
		Enabled:   cfg.Enabled,
		Port:      cfg.Port,
		// Gerçeğin kaynağı KALICI özet dosyası, config'teki bayrak değil:
		// bayrak "bir zamanlar kondu" der, dosya "şu an geçerli" der.
		PasswordSet: hasPw,
		Keys:        len(cfg.AuthorizedKeys),
		User:        "root",
		Addresses:   localAddresses(),
		Flavor:      Flavor(),
	}
	var notes []string
	switch {
	case !st.Available:
		notes = append(notes, "SSH sunucusu bu imajda yok (openssh/dropbear)")
	case cfg.Enabled && !running:
		if lastErr != "" {
			// "açık ama çalışmıyor" zaten Enabled/Running'den okunuyor;
			// notun işi NEDENİ söylemek.
			notes = append(notes, "sunucu durdu: "+lastErr)
		} else {
			notes = append(notes, "açık ama çalışmıyor")
		}
	}
	// Parola notları sunucunun durumundan BAĞIMSIZ: sunucu çalışmıyorken de
	// kullanıcı parolanın neden geçmeyeceğini bilmeli.
	switch {
	case pwErr != nil:
		notes = append(notes, pwErr.Error())
	case cfg.PasswordSet && !hasPw:
		// Eski sürüm parolayı yalnızca RAM'deki /etc/shadow'a yazıyordu;
		// bayrak kalmış ama parola yeniden başlatmada silinmiş olabilir.
		notes = append(notes, "kayıtlı parola yok — parolayı yeniden koyun")
	case !hasPw && len(cfg.AuthorizedKeys) == 0 && cfg.Enabled:
		// Bu önemli: parolasız ve anahtarsız bir SSH sunucusu ya hiç kimseyi
		// içeri almaz ya da (yanlış yapılandırılırsa) herkesi alır. Kullanıcı
		// durumu bilmeli.
		notes = append(notes, "parola da anahtar da yok — giriş yapılamaz")
	}
	if st.Available && dataVolatile(m.dataDir) {
		notes = append(notes, "kalıcı depolama yok: parola ve anahtarlar "+
			"yeniden başlatınca silinir")
	}
	st.Note = strings.Join(notes, "; ")
	return st
}

// Apply starts or stops the server to match cfg.
func (m *Manager) Apply(cfg model.SSHConfig) error {
	cfg = cfg.Normalize()

	if !cfg.Enabled {
		m.Stop()
		return nil
	}
	if !Available() {
		return ErrUnavailable
	}
	// Kalıcı parolayı shadow'a geri yaz: kök RAM'de ve her açılışta imajın
	// varsayılanı ("root") geri geliyor. Parola girişi YALNIZCA bu başarılıysa
	// açılır; aksi hâlde kapı herkesin bildiği parolayla açık kalırdı.
	pwAuth, pwErr := m.restorePassword()
	if pwErr != nil && m.log != nil {
		m.log.Warnf("sshd: %v — parola girişi kapalı", pwErr)
	}
	// Anahtarsız ve parolasız açmak anlamsız: kullanıcı giremez ve nedenini
	// anlayamaz. Açıkça söylüyoruz.
	if !pwAuth && len(cfg.AuthorizedKeys) == 0 {
		if pwErr != nil {
			return pwErr
		}
		return errors.New("önce bir parola koyun ya da bir açık anahtar ekleyin")
	}

	if err := m.writeAuthorizedKeys(cfg.AuthorizedKeys); err != nil {
		return err
	}
	return m.start(cfg.Port, pwAuth)
}

// writeAuthorizedKeys installs the user's public keys.
func (m *Manager) writeAuthorizedKeys(keys []string) error {
	if len(keys) == 0 {
		// Anahtar listesi boşaldıysa eski dosya da gitmeli: yoksa silinmiş
		// sanılan bir anahtar içeri girmeye devam ederdi.
		if err := os.Remove(authorizedKeysPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("eski anahtarlar silinemedi: %w", err)
		}
		return nil
	}
	dir := filepath.Dir(authorizedKeysPath)
	// 0700 / 0600: dropbear (ve OpenSSH) gevşek izinli bir authorized_keys
	// dosyasını GÖRMEZDEN GELİR. Yanlış izin, "anahtarım çalışmıyor" diye
	// saatler harcatan sessiz bir arızadır.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%s oluşturulamadı: %w", dir, err)
	}
	body := strings.Join(keys, "\n") + "\n"
	tmp := authorizedKeysPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return fmt.Errorf("anahtarlar yazılamadı: %w", err)
	}
	if err := os.Rename(tmp, authorizedKeysPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("anahtarlar yazılamadı: %w", err)
	}
	return nil
}

// Running reports whether the server process is up.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Port reports the port the running server bound.
func (m *Manager) Port() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.port
}

// SFTPDirName, WinSCP/SFTP oturumunun açıldığı sunucular klasörüdür (veri
// kökü altında). daemon.SFTPDirName ile AYNI olmalı; daemon oraya her sunucu
// için adlı bir bağ koyar.
const SFTPDirName = "sunucular"
