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

	proc    *process
	running bool
	port    int
}

// New creates a manager. dataDir must be persistent: host keys live there and
// must survive reboots, otherwise every connection warns about a changed key.
func New(dataDir string, lg *log.Logger) *Manager {
	return &Manager{dataDir: dataDir, log: lg}
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
	m.mu.Unlock()

	st := Status{
		Available:   Available(),
		Running:     running,
		Enabled:     cfg.Enabled,
		Port:        cfg.Port,
		PasswordSet: cfg.PasswordSet,
		Keys:        len(cfg.AuthorizedKeys),
		User:        "root",
		Addresses:   localAddresses(),
		Flavor:      Flavor(),
	}
	switch {
	case !st.Available:
		st.Note = "SSH sunucusu bu imajda yok (openssh/dropbear)"
	case cfg.Enabled && !running:
		st.Note = "açık ama çalışmıyor"
	case !cfg.PasswordSet && len(cfg.AuthorizedKeys) == 0 && cfg.Enabled:
		// Bu önemli: parolasız ve anahtarsız bir SSH sunucusu ya hiç kimseyi
		// içeri almaz ya da (yanlış yapılandırılırsa) herkesi alır. Kullanıcı
		// durumu bilmeli.
		st.Note = "parola da anahtar da yok — giriş yapılamaz"
	}
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
	// Anahtarsız ve parolasız açmak anlamsız: kullanıcı giremez ve nedenini
	// anlayamaz. Açıkça söylüyoruz.
	if !cfg.PasswordSet && len(cfg.AuthorizedKeys) == 0 {
		return errors.New("önce bir parola koyun ya da bir açık anahtar ekleyin")
	}

	if err := m.writeAuthorizedKeys(cfg.AuthorizedKeys); err != nil {
		return err
	}
	return m.start(cfg.Port)
}

// writeAuthorizedKeys installs the user's public keys.
func (m *Manager) writeAuthorizedKeys(keys []string) error {
	if len(keys) == 0 {
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
