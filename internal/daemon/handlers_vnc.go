package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"mcos/internal/fbdev"
	"mcos/internal/framebus"
	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/vnc"
)

// Bu dosya EKRAN PAYLAŞIMINI yönetir (RFB / VNC).
//
// ════════════════════════════════════════════════════════════════════════════
// KULLANICININ İSTEĞİ
// ════════════════════════════════════════════════════════════════════════════
//
//	"realvnc ile bağlanma bu da olsun"
//
// ── Üç uzaktan erişim yolunun farkı ─────────────────────────────────────────
//
//	HTTPS köprüsü → telefon uygulaması. Yapılandırılmış veriyle konuşur
//	                (sunucu listesi, konsol, durum). Şifreli, jetonlu.
//	SSH           → kabuk. Panelin yapamadığı işler.
//	VNC           → EKRANIN KENDİSİ. Panelin tamamı, olduğu gibi: sihirbaz,
//	                açılış animasyonu, kilit ekranı. Klavye ve fare de gider.
//
// Üçü de varsayılan KAPALIDIR ve panelden açılır.

// vncState holds the running screen-sharing server.
type vncState struct {
	mu    sync.Mutex
	srv   *vnc.Server
	fb    vncSource
	input vnc.Injector
	err   string
	// izle, kaynak değişimini (panel açıldı/kapandı, çözünürlük değişti)
	// gözleyen goroutine'i durdurur.
	izle chan struct{}
	// op, başlat/durdur dizilerini sıralar: izleyicinin yeniden başlatması
	// ile panelden gelen bir aç/kapa aynı anda çalışmasın.
	op sync.Mutex
}

// vncSource is where the shared screen comes from.
type vncSource interface {
	vnc.Framebuffer
	Close() error
}

// openVNCSource picks the screen to share.
//
// ── Neden artık önce framebus ───────────────────────────────────────────────
//
// Panel ekran kartına (DRM) doğrudan çiziyor; /dev/fb0'da yalnızca fbcon'un
// tamponu kalıyor. VNC oradan okumaya devam etseydi uzaktaki kullanıcı paneli
// değil BOŞ BİR KONSOLU görürdü. Panel karelerini framebus'a koyuyor; o
// canlıysa oradan okunur. Panel kapalıysa (kurtarma konsolu, eski panel) eski
// yol: /dev/fb0.
func openVNCSource() (vncSource, string, error) {
	if r, err := framebus.Open(framebus.Path); err == nil {
		return r, "panel", nil
	}
	fb, err := fbdev.Open("/dev/fb0")
	if err != nil {
		return nil, "", err
	}
	return fb, "fb0", nil
}

// VNCStatus is what the panel shows.
type VNCStatus struct {
	Enabled   bool     `json:"enabled"`
	Running   bool     `json:"running"`
	Port      int      `json:"port"`
	Password  string   `json:"password,omitempty"`
	ViewOnly  bool     `json:"viewOnly"`
	Clients   int      `json:"clients"`
	Addresses []string `json:"addresses,omitempty"`
	// Input, girdi enjeksiyonunun çalışıp çalışmadığını söyler.
	//
	// Kullanıcının "bağlandım ama tıklayamıyorum" sorusunun cevabı burada:
	// uinput yoksa (çekirdekte kapalıysa) VNC yalnızca İZLEME kipindedir ve
	// panel bunu açıkça yazar.
	Input bool   `json:"input"`
	Note  string `json:"note,omitempty"`
}

func (d *Daemon) handleVNCStatus(_ context.Context, _ json.RawMessage) (any, error) {
	return d.vncStatus(), nil
}

func (d *Daemon) vncStatus() VNCStatus {
	cfg := d.Config()
	vc := cfg.VNC.Normalize()

	st := VNCStatus{
		Enabled:   vc.Enabled,
		Port:      vc.Port,
		Password:  vc.Password,
		ViewOnly:  vc.ViewOnly,
		Addresses: hostAddresses(),
	}

	d.vncSt.mu.Lock()
	srv := d.vncSt.srv
	input := d.vncSt.input
	errText := d.vncSt.err
	d.vncSt.mu.Unlock()

	st.Running = srv != nil
	st.Input = input != nil
	if srv != nil {
		st.Clients = srv.Clients()
	}

	switch {
	case errText != "":
		st.Note = errText
	case !vc.Enabled:
		st.Note = "Ekran paylaşımı kapalı."
	case st.Running && !st.Input:
		st.Note = "Bağlanabilirsiniz — ama yalnızca İZLEME (girdi aygıtı yok)."
	case st.Running && vc.ViewOnly:
		st.Note = "Yalnızca izleme kipinde açık."
	case st.Running:
		st.Note = "RealVNC Viewer'a adresi ve parolayı girin."
	}
	return st
}

// handleVNCEnable turns screen sharing on, generating a password if needed.
func (d *Daemon) handleVNCEnable(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Port     int   `json:"port,omitempty"`
		ViewOnly *bool `json:"viewOnly,omitempty"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}

	cfg := d.Config()
	cp := *cfg
	cp.VNC = cp.VNC.Normalize()
	cp.VNC.Enabled = true
	if p.Port > 0 {
		cp.VNC.Port = p.Port
	}
	if p.ViewOnly != nil {
		cp.VNC.ViewOnly = *p.ViewOnly
	}
	if strings.TrimSpace(cp.VNC.Password) == "" {
		cp.VNC.Password = newVNCPassword()
	}

	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	if err := d.startVNC(); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return d.vncStatus(), nil
}

func (d *Daemon) handleVNCDisable(_ context.Context, _ json.RawMessage) (any, error) {
	cfg := d.Config()
	cp := *cfg
	cp.VNC.Enabled = false
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	d.stopVNC()
	return d.vncStatus(), nil
}

// handleVNCRotate replaces the password, cutting off existing viewers.
func (d *Daemon) handleVNCRotate(_ context.Context, _ json.RawMessage) (any, error) {
	cfg := d.Config()
	cp := *cfg
	cp.VNC = cp.VNC.Normalize()
	cp.VNC.Password = newVNCPassword()
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	// Çalışan sunucu ESKİ parolayı bellekte tutuyor; yenilemenin amacı tam
	// olarak eski istemcileri kesmek.
	if cp.VNC.Enabled {
		d.stopVNC()
		if err := d.startVNC(); err != nil {
			return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
		}
	}
	return d.vncStatus(), nil
}

// handleVNCViewOnly toggles input injection without restarting the server.
func (d *Daemon) handleVNCViewOnly(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ViewOnly bool `json:"viewOnly"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	cfg := d.Config()
	cp := *cfg
	cp.VNC = cp.VNC.Normalize()
	cp.VNC.ViewOnly = p.ViewOnly
	if err := d.saveConfigCopy(&cp); err != nil {
		return nil, err
	}
	// Girdi enjeksiyonu sunucu kurulurken bağlanıyor; değişikliğin etkili
	// olması için yeniden başlatmak gerekiyor. Bağlı istemciler düşer —
	// "artık kontrol edemezsiniz" demenin en dürüst yolu da budur.
	if cp.VNC.Enabled {
		d.stopVNC()
		if err := d.startVNC(); err != nil {
			return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
		}
	}
	return d.vncStatus(), nil
}

// ── Yaşam döngüsü ───────────────────────────────────────────────────────────

// startVNC opens the framebuffer, the virtual input device and the listener.
func (d *Daemon) startVNC() error {
	cfg := d.Config()
	vc := cfg.VNC.Normalize()
	if !vc.Enabled {
		return nil
	}

	d.vncSt.op.Lock()
	defer d.vncSt.op.Unlock()
	d.stopVNC() // yeniden başlatma: eski dinleyici kapansın

	fb, kaynak, err := openVNCSource()
	if err != nil {
		d.setVNCErr(fmt.Sprintf("ekran okunamadı: %v", err))
		return fmt.Errorf("ekran paylaşımı açılamadı: %w", err)
	}
	w, h := fb.Size()
	d.log.Infof("vnc: kaynak=%s %dx%d", kaynak, w, h)

	var input vnc.Injector
	if !vc.ViewOnly {
		input, err = vnc.NewInjector(w, h)
		if err != nil {
			// İZLEME kipine düş: ekranı görebilmek, hiç bağlanamamaktan
			// iyidir. Panel bunu "yalnızca izleme" diye yazıyor.
			d.log.Warnf("vnc: girdi aygıtı açılamadı (%v) — izleme kipi", err)
			input = nil
		}
	}

	srv, err := vnc.New(vnc.Options{
		Addr:     fmt.Sprintf(":%d", vc.Port),
		Password: vc.Password,
		Name:     "MCOS " + cfg.Cluster.NodeName,
		FB:       fb,
		Input:    input,
		Log:      d.log,
	})
	if err != nil {
		fb.Close()
		if input != nil {
			_ = input.Close()
		}
		d.setVNCErr(err.Error())
		return err
	}

	izle := make(chan struct{})
	d.vncSt.mu.Lock()
	d.vncSt.srv, d.vncSt.fb, d.vncSt.input, d.vncSt.err = srv, fb, input, ""
	d.vncSt.izle = izle
	d.vncSt.mu.Unlock()
	go d.watchVNCSource(fb, izle)

	go func() {
		if err := srv.Serve(); err != nil {
			d.log.Warnf("vnc: sunucu durdu: %v", err)
			d.setVNCErr(err.Error())
		}
	}()
	return nil
}

// stopVNC closes everything the server owns.
func (d *Daemon) stopVNC() {
	d.vncSt.mu.Lock()
	srv, fb, input := d.vncSt.srv, d.vncSt.fb, d.vncSt.input
	d.vncSt.srv, d.vncSt.fb, d.vncSt.input = nil, nil, nil
	if d.vncSt.izle != nil {
		close(d.vncSt.izle)
		d.vncSt.izle = nil
	}
	d.vncSt.mu.Unlock()

	if srv != nil {
		_ = srv.Close()
	}
	if input != nil {
		_ = input.Close()
	}
	if fb != nil {
		_ = fb.Close()
	}
}

// watchVNCSource restarts sharing when the source changes.
//
// Üç durum yeniden başlatma ister:
//   - panel çözünürlüğü çalışırken değiştirdi (VNC oturumu boyutu açılışta
//     sabitliyor; eski boyutta devam etmek ekranın bir köşesini gösterirdi),
//   - panel kapandı (framebus bayatladı) -> /dev/fb0'a dön,
//   - panel açıldı (framebus canlandı) -> /dev/fb0'dan panele geç.
//
// İstemciler kopar ve yeniden bağlanır; VNC görüntüleyicilerinin çoğu bunu
// kendiliğinden yapar.
func (d *Daemon) watchVNCSource(src vncSource, dur <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	w0, h0 := src.Size()
	for {
		select {
		case <-dur:
			return
		case <-t.C:
		}
		yenile := false
		if r, ok := src.(*framebus.Reader); ok {
			w, h := r.CurrentSize()
			yenile = !r.Alive() || w != w0 || h != h0
		} else if r, err := framebus.Open(framebus.Path); err == nil {
			r.Close()
			yenile = true
		}
		if yenile {
			go func() {
				if err := d.startVNC(); err != nil {
					d.log.Warnf("vnc: kaynak değişti, yeniden başlatılamadı: %v", err)
				}
			}()
			return
		}
	}
}

func (d *Daemon) setVNCErr(s string) {
	d.vncSt.mu.Lock()
	d.vncSt.err = s
	d.vncSt.mu.Unlock()
}

// ── Parola üretimi ──────────────────────────────────────────────────────────

// vncPasswordAlphabet excludes characters that are easy to misread.
//
// Parola kullanıcı tarafından EKRANDAN OKUNUP telefona/bilgisayara elle
// yazılıyor. 0/O ve 1/l/I ayrımı bir hata kaynağıdır; küme buna göre seçildi.
const vncPasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newVNCPassword returns an 8-character password.
//
// 8 KARAKTER TAM OLARAK: RFB protokolü parolayı 8 bayta kırpar. Daha uzunu
// üretmek, kullanıcının ekranda gördüğü parolanın bir kısmının hiç
// kullanılmaması demekti — ve "parolayı doğru yazdım ama kabul etmiyor"
// şikâyeti.
//
// Entropi: 32^8 ≈ 2^40. Şifresiz bir protokol için mükemmel değil ama
// yerel ağda kaba kuvvete karşı yeterli; panel zaten "internete açmayın,
// SSH tüneli kullanın" diye yazıyor.
func newVNCPassword() string {
	b := make([]byte, 8)
	max := big.NewInt(int64(len(vncPasswordAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand başarısız olursa parola ÜRETMİYORUZ: zayıf bir
			// yedek (zaman tabanlı vb.) tahmin edilebilir olurdu.
			return ""
		}
		b[i] = vncPasswordAlphabet[n.Int64()]
	}
	return string(b)
}

// vncConfigured reports whether the config asks for screen sharing.
func vncConfigured(cfg *model.Config) bool {
	return cfg != nil && cfg.VNC.Enabled
}
