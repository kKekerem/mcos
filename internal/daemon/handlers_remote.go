package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/remote"
	"mcos/internal/sshd"
	"mcos/internal/store"
)

// Bu dosya UZAKTAN KONTROLÜ yönetir: telefon uygulamasının bağlandığı HTTPS
// köprüsü ve SSH sunucusu.
//
// ════════════════════════════════════════════════════════════════════════════
// KULLANICININ İSTEĞİ
// ════════════════════════════════════════════════════════════════════════════
//
//	"ssh ile bağlanıp kontrol etme ve uzaktan kontrol ekle ... ordan ip girip
//	 bağlanabilelim kontrol edebilelim"
//
// İki ayrı yol, iki ayrı amaç:
//
//	HTTPS köprüsü → telefon uygulaması. Panelin yaptığı her şeyi yapar
//	                (sunucu başlat/durdur, konsol, dosya), ama grafik bir
//	                arayüzle.
//	SSH           → kabuk. Panelin YAPAMADIĞI şeyler için: bir dosyaya elle
//	                bakmak, bir sorunu ayıklamak.
//
// İkisi de varsayılan olarak KAPALIDIR ve panelden açılır.

// remoteState holds the running bridge.
type remoteState struct {
	mu     sync.Mutex
	srv    *remote.Server
	cancel context.CancelFunc
}

// ── RPC yüzeyi ──────────────────────────────────────────────────────────────

// RemoteStatus is what the panel and the phone app show.
type RemoteStatus struct {
	Enabled     bool     `json:"enabled"`
	Running     bool     `json:"running"`
	Port        int      `json:"port"`
	Token       string   `json:"token,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Addresses   []string `json:"addresses,omitempty"`
	URL         string   `json:"url,omitempty"`
	Note        string   `json:"note,omitempty"`
}

func (d *Daemon) handleRemoteStatus(_ context.Context, _ json.RawMessage) (any, error) {
	return d.remoteStatus(), nil
}

func (d *Daemon) remoteStatus() RemoteStatus {
	cfg := d.Config()
	rc := cfg.Remote.Normalize()

	st := RemoteStatus{
		Enabled:   rc.Enabled,
		Port:      rc.Port,
		Token:     rc.Token,
		Addresses: hostAddresses(),
	}

	d.remoteSt.mu.Lock()
	srv := d.remoteSt.srv
	d.remoteSt.mu.Unlock()

	if srv != nil {
		st.Running = true
		st.Fingerprint = srv.Fingerprint()
	}
	if len(st.Addresses) > 0 {
		st.URL = fmt.Sprintf("https://%s:%d", st.Addresses[0], rc.Port)
	}
	switch {
	case !rc.Enabled:
		st.Note = "kapalı"
	case !st.Running:
		st.Note = "açık ama dinlemiyor — günlüğe bakın"
	case len(st.Addresses) == 0:
		st.Note = "ağ adresi yok — kablo veya Wi-Fi bağlı mı?"
	}
	return st
}

func (d *Daemon) handleRemoteEnable(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Port int `json:"port,omitempty"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}

	cfg := d.Config()
	cp := *cfg
	cp.Remote = cp.Remote.Normalize()
	cp.Remote.Enabled = true
	if p.Port > 0 {
		cp.Remote.Port = p.Port
	}
	// Jeton yoksa ÜRET. Kullanıcıdan jeton yazmasını istemek, zayıf bir
	// jeton seçmesine yol açardı; 128 bit rastgele değer tahmin edilemez.
	if strings.TrimSpace(cp.Remote.Token) == "" {
		tok, err := newToken()
		if err != nil {
			return nil, err
		}
		cp.Remote.Token = tok
	}

	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	if err := d.startRemote(); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return d.remoteStatus(), nil
}

func (d *Daemon) handleRemoteDisable(_ context.Context, _ json.RawMessage) (any, error) {
	cfg := d.Config()
	cp := *cfg
	cp.Remote.Enabled = false
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	d.stopRemote()
	return d.remoteStatus(), nil
}

// handleRemoteRotate replaces the token, cutting off every existing client.
func (d *Daemon) handleRemoteRotate(_ context.Context, _ json.RawMessage) (any, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	cfg := d.Config()
	cp := *cfg
	cp.Remote = cp.Remote.Normalize()
	cp.Remote.Token = tok
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	// Yeniden başlat: çalışan köprü ESKİ jetonu bellekte tutuyor ve
	// yenilemenin amacı tam olarak eski istemcileri kesmek.
	if cp.Remote.Enabled {
		d.stopRemote()
		if err := d.startRemote(); err != nil {
			return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
		}
	}
	return d.remoteStatus(), nil
}

// ── Köprünün yaşam döngüsü ──────────────────────────────────────────────────

// startRemote launches the HTTPS bridge if it is enabled.
func (d *Daemon) startRemote() error {
	cfg := d.Config()
	rc := cfg.Remote.Normalize()
	if !rc.Enabled {
		return nil
	}
	if strings.TrimSpace(rc.Token) == "" {
		return fmt.Errorf("jeton yok — uzaktan erişim açılamaz")
	}

	d.remoteSt.mu.Lock()
	if d.remoteSt.srv != nil {
		d.remoteSt.mu.Unlock()
		return nil // zaten çalışıyor
	}
	d.remoteSt.mu.Unlock()

	name := cfg.Hostname
	if name == "" {
		name = cfg.Cluster.NodeName
	}

	srv, err := remote.New(d.rpc, remote.Options{
		Addr:    fmt.Sprintf(":%d", rc.Port),
		Token:   rc.Token,
		CertDir: filepath.Join(d.store.Paths.Root, "remote"),
		Name:    name,
		Version: Version,
		Log:     d.log,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.remoteSt.mu.Lock()
	d.remoteSt.srv = srv
	d.remoteSt.cancel = cancel
	d.remoteSt.mu.Unlock()

	go func() {
		if err := srv.Serve(ctx); err != nil {
			d.log.Errorf("remote: köprü durdu: %v", err)
		}
		// Durum temizlensin, yoksa panel sonsuza dek "çalışıyor" gösterir.
		d.remoteSt.mu.Lock()
		if d.remoteSt.srv == srv {
			d.remoteSt.srv = nil
			d.remoteSt.cancel = nil
		}
		d.remoteSt.mu.Unlock()
	}()
	return nil
}

// stopRemote shuts the bridge down.
func (d *Daemon) stopRemote() {
	d.remoteSt.mu.Lock()
	cancel := d.remoteSt.cancel
	srv := d.remoteSt.srv
	d.remoteSt.srv = nil
	d.remoteSt.cancel = nil
	d.remoteSt.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		_ = srv.Close()
	}
}

// ── SSH ─────────────────────────────────────────────────────────────────────

func (d *Daemon) handleSSHStatus(_ context.Context, _ json.RawMessage) (any, error) {
	return d.ssh.Status(d.Config().SSH), nil
}

func (d *Daemon) handleSSHEnable(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Port int `json:"port,omitempty"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	cfg := d.Config()
	cp := *cfg
	cp.SSH = cp.SSH.Normalize()
	cp.SSH.Enabled = true
	if p.Port > 0 {
		cp.SSH.Port = p.Port
	}
	if err := d.ssh.Apply(cp.SSH); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	return d.ssh.Status(cp.SSH), nil
}

func (d *Daemon) handleSSHDisable(_ context.Context, _ json.RawMessage) (any, error) {
	cfg := d.Config()
	cp := *cfg
	cp.SSH.Enabled = false
	d.ssh.Stop()
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	return d.ssh.Status(cp.SSH), nil
}

// handleSSHPassword sets the shell login password.
func (d *Daemon) handleSSHPassword(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "geçersiz parametre"}
	}
	// En az 8: SSH portu ağa açık olacak ve parola denemesi ucuzdur. Panel
	// parolasının alt sınırı (4) burada yetersiz olurdu.
	const minSSHPassword = 8
	if len([]rune(strings.TrimSpace(p.Password))) < minSSHPassword {
		return nil, &ipc.Error{
			Code:    ipc.CodeInvalidParams,
			Message: fmt.Sprintf("SSH parolası en az %d karakter olmalı", minSSHPassword),
		}
	}
	if err := sshd.SetPassword("root", p.Password); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}

	cfg := d.Config()
	cp := *cfg
	cp.SSH = cp.SSH.Normalize()
	cp.SSH.PasswordSet = true
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	return d.ssh.Status(cp.SSH), nil
}

// handleSSHAddKey appends a public key for password-less login.
func (d *Daemon) handleSSHAddKey(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "geçersiz parametre"}
	}
	key := strings.TrimSpace(p.Key)
	if !looksLikePublicKey(key) {
		return nil, &ipc.Error{
			Code: ipc.CodeInvalidParams,
			Message: "bu bir SSH açık anahtarı gibi görünmüyor " +
				"(ssh-ed25519 ... ya da ssh-rsa ... bekleniyor)",
		}
	}

	cfg := d.Config()
	cp := *cfg
	cp.SSH = cp.SSH.Normalize()
	for _, existing := range cp.SSH.AuthorizedKeys {
		if existing == key {
			return d.ssh.Status(cp.SSH), nil // zaten var
		}
	}
	cp.SSH.AuthorizedKeys = append(cp.SSH.AuthorizedKeys, key)
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	// Açıksa hemen uygula, yoksa anahtar yalnızca bir sonraki açılışta
	// geçerli olurdu.
	if cp.SSH.Enabled {
		_ = d.ssh.Apply(cp.SSH)
	}
	return d.ssh.Status(cp.SSH), nil
}

// looksLikePublicKey rejects obvious mistakes before writing the file.
//
// ── Neden denetliyoruz ──────────────────────────────────────────────────────
// En sık yapılan hata, ÖZEL anahtarı yapıştırmaktır. Onu authorized_keys'e
// yazmak hem işe yaramaz hem de özel anahtarı diske kopyalar. İkincisi
// gerçek bir güvenlik sorunudur.
func looksLikePublicKey(s string) bool {
	if s == "" || strings.Contains(s, "PRIVATE KEY") {
		return false
	}
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ssh-ed25519@openssh.com":
		return len(fields[1]) > 20
	}
	return false
}

// ── Yardımcılar ─────────────────────────────────────────────────────────────

// saveConfigCopy persists a modified config and swaps it in.
func (d *Daemon) saveConfigCopy(cp *model.Config) error {
	if err := store.SaveConfig(d.cfgPath, cp); err != nil {
		return err
	}
	d.mu.Lock()
	d.cfg = cp
	d.mu.Unlock()
	return nil
}

// newToken returns a fresh 128-bit secret.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("jeton üretilemedi: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hostAddresses lists the LAN addresses a phone can reach.
func hostAddresses() []string {
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
