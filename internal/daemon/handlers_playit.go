package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/tunnel"
)

// Bu dosya playit.gg tünel ajanını daemon'a bağlar.
//
// ════════════════════════════════════════════════════════════════════════════
// KULLANICI AKIŞI — tam olarak istenen sıra
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı aynen şöyle dedi:
//
//	"ilk kurulumda kursun servisi, sonra giris yapalım agente, sonra siteden
//	 tünel acıp bağlayabilelim"
//
// Uygulanan akış tam olarak budur:
//
//	1. KURULUM (playit.install) — ilk kurulum sihirbazında çalışır. İkilileri
//	   doğrular; imajda gömülü değilse çevrimdışı paketten kurar. İnternet
//	   gerekmez (ikililer imajın içinde).
//
//	2. HESABA GİRİŞ (playit.claim → playit.poll) — panel bir onay kodu ve
//	   adres gösterir. Kullanıcı telefonundan playit.gg/claim/<kod> adresini
//	   açıp onaylar. Bu adım ATLANAMAZ: hesap sahipliğini doğrulamanın başka
//	   yolu yok ve playit'in tasarımı böyle.
//
//	3. AJAN (playit.start) — gizli anahtar kaydedildikten sonra ajan
//	   başlatılır ve playit bulutundan tünel yapılandırmasını KENDİSİ çeker.
//
//	4. TÜNEL (otomatik; playit.tunnel ile elle yenilenir) — ajan SELF-MANAGED
//	   kaydedildiği için playit bulutu ona kendiliğinden tünel açmaz. Eskiden
//	   ekranda "playit.gg sitesinden tünel oluşturun" yazıp bırakıyorduk;
//	   kullanıcı "tünel kendiliğinden açılsın" dedi. Artık aynı anahtarla
//	   API'den Minecraft Java tünelini BİZ açıyoruz, adres gelene kadar arka
//	   planda rundata okuyup panele ve sunucunun WAN.Hostname alanına
//	   yazıyoruz (bkz. handlers_playit_tunnel.go). Sitede elle açılan tüneller
//	   de yerel portlarından tanınır; ikinci bir tünel açılmaz.
//
// ── Neden 3. adımda yapılandırma dosyası yok? ───────────────────────────────
// playit 1.0 ajanının yapılandırması YALNIZCA gizli anahtarı tutar. Tüneller
// bulutta yaşar. İnternette dolaşan "[[mappings]]" örnekleri çok eski 0.15
// sürümüne aittir ve 1.0 ile çalışmaz.

// playitState holds the agent and any in-flight claim.
//
// Daemon yapısına gömmek yerine ayrı tutuluyor: playit isteğe bağlıdır ve
// imajda hiç bulunmayabilir; daemon'un çekirdeği ona bağımlı olmamalı.
type playitState struct {
	mu    sync.Mutex
	agent *tunnel.PlayitAgent
	claim *tunnel.Claim
	// claimErr, arka planda biten bir bağlama denemesinin sonucudur.
	claimErr error
	claimOK  bool
	// tun, API üzerinden tünel açan arka plan eşitleyicisinin durumu.
	tun playitTunnels
}

// playit returns the lazily-created playit state.
func (d *Daemon) playit() *playitState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.playitSt == nil {
		d.playitSt = &playitState{agent: &tunnel.PlayitAgent{}}
	}
	return d.playitSt
}

// playitStop shuts the agent down on daemon exit.
//
// Ajanı öldürmeden çıkmak, tünelin playit bulutunda "açık" görünmeye devam
// etmesine ve yeniden başlatıldığında çakışmaya yol açar.
func (d *Daemon) playitStop() {
	d.mu.Lock()
	st := d.playitSt
	d.mu.Unlock()
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.claim != nil {
		st.claim.Cancel()
		st.claim = nil
	}
	st.tun.shutdown()
	if st.agent != nil {
		_ = st.agent.Stop()
	}
}

// playitStatusResult is what the panel renders.
type playitStatusResult struct {
	// Installed reports whether the agent binaries exist in the image.
	Installed bool `json:"installed"`
	// Claimed reports whether an account secret is stored.
	Claimed bool `json:"claimed"`
	// Running reports whether playitd is alive.
	Running bool `json:"running"`
	// Address is the public hostname, once the agent reports one.
	Address string `json:"address,omitempty"`
	// ClaimCode/ClaimURL are set while an account binding is pending.
	ClaimCode string `json:"claimCode,omitempty"`
	ClaimURL  string `json:"claimUrl,omitempty"`
	// Log holds the last agent output lines.
	Log []string `json:"log,omitempty"`
	// Note is a human-readable explanation of the current step.
	Note string `json:"note,omitempty"`

	// Tunnels: playit hesabındaki tüneller (API'den), MCOS sunucusuyla
	// eşlenmiş; oluşturulmakta olanlar ve açılamayanlar da burada.
	Tunnels []ipc.PlayitTunnel `json:"tunnels,omitempty"`
	// Syncing: arka planda rundata okunuyor ya da tünel adres bekliyor.
	Syncing bool `json:"syncing,omitempty"`
	// TunnelError: son eşitleme turunun Türkçe hatası.
	TunnelError string `json:"tunnelError,omitempty"`
	// Notices: misafir/doğrulanmamış hesap ve playit'in kritik duyuruları.
	Notices []string `json:"notices,omitempty"`
	// KeyInvalid: kayıtlı anahtar reddedildi; hesap YENİDEN bağlanmalı.
	KeyInvalid bool `json:"keyInvalid,omitempty"`
}

func (d *Daemon) handlePlayitStatus(_ context.Context, _ json.RawMessage) (any, error) {
	st := d.playit()
	res := playitStatusResult{
		Installed: tunnel.PlayitAvailable(),
		Claimed:   tunnel.Claimed(),
	}
	if !res.Installed {
		res.Note = "playit ajanı bu imajda yok — çevrimdışı paketle gelir " +
			"(make offline-bundle)"
		return res, nil
	}

	st.mu.Lock()
	agent := st.agent
	claim := st.claim
	claimOK := st.claimOK
	claimErr := st.claimErr
	st.mu.Unlock()

	logAddr := ""
	if agent != nil {
		running, addr, lg := agent.Status()
		res.Running = running
		logAddr = addr
		res.KeyInvalid = agent.KeyRejected()
		// Günlüğün tamamını göndermek paneli boğar; son 12 satır yeter.
		if n := len(lg); n > 12 {
			lg = lg[n-12:]
		}
		res.Log = lg
	}

	// API'den okunan tüneller. Adres ÖNCE buradan: günlük ayrıştırması
	// (parsePlayitAddr) yalnızca yedek — playitd 1.0.10 adresi günlüğe her
	// zaman yazmıyor.
	d.fillPlayitTunnels(&res)
	if res.Address == "" {
		res.Address = logAddr
	}
	if res.Running && res.Claimed {
		d.kickPlayitSyncIfTargetsChanged()
	}

	switch {
	case claim != nil && !claimOK:
		res.ClaimCode = claim.Code
		res.ClaimURL = claim.URL
		res.Note = "Telefonunuzdan " + claim.URL + " adresini açıp onaylayın."
	case claimErr != nil:
		res.Note = "Hesap bağlama başarısız: " + claimErr.Error()
	case !res.Claimed:
		res.Note = "Hesap bağlı değil. 'Hesabı bağla' ile başlayın."
	case res.KeyInvalid:
		res.Note = "playit anahtarı geçersiz; 'Hesabı bağla' ile yeniden bağlayın."
	case !res.Running:
		res.Note = "Hesap bağlı. Ajanı başlatın."
	case res.TunnelError != "" && res.Address == "":
		res.Note = "Tünel açılamadı: " + res.TunnelError
	case res.Address == "":
		res.Note = "Tünel hazırlanıyor — playit adres atıyor…"
	default:
		res.Note = "Tünel açık: " + res.Address
	}
	return res, nil
}

// handlePlayitInstall verifies (and reports) the agent installation.
//
// İkililer imaja GÖMÜLÜ gelir; bu çağrı onları indirmez, yalnızca varlığını
// ve çalıştırılabilirliğini doğrular. İlk kurulum sihirbazı bunu çağırır ki
// kullanıcı daha o anda "playit hazır" ya da "playit yok" bilgisini görsün —
// aylar sonra tünel açmaya çalışırken değil.
func (d *Daemon) handlePlayitInstall(_ context.Context, _ json.RawMessage) (any, error) {
	if !tunnel.PlayitAvailable() {
		return nil, &ipc.Error{
			Code: ipc.CodeUnavailable,
			Message: "playit ajanı bulunamadı (/usr/bin/playitd, /usr/bin/playit-cli). " +
				"İmaj 'make offline-bundle' ile derlenmemiş olabilir.",
		}
	}
	return map[string]any{
		"installed": true,
		"message":   "playit ajanı hazır.",
	}, nil
}

func (d *Daemon) handlePlayitClaim(_ context.Context, _ json.RawMessage) (any, error) {
	st := d.playit()
	st.mu.Lock()
	defer st.mu.Unlock()

	// Zaten süren bir bağlama varsa AYNI kodu döndür: ikinci bir kod üretmek,
	// kullanıcının telefonunda açtığı sayfayı geçersiz kılardı.
	if st.claim != nil && !st.claimOK {
		if done, err := st.claim.Done(); !done {
			return map[string]any{"code": st.claim.Code, "url": st.claim.URL}, nil
		} else if err == nil {
			st.claimOK = true
			return map[string]any{"code": st.claim.Code, "url": st.claim.URL,
				"done": true}, nil
		}
	}

	c, err := tunnel.StartClaim()
	if err != nil {
		code := ipc.CodeInternalError
		if errors.Is(err, tunnel.ErrNotInstalled) {
			code = ipc.CodeUnavailable
		}
		return nil, &ipc.Error{Code: code, Message: err.Error()}
	}
	st.claim = c
	st.claimOK = false
	st.claimErr = nil
	d.log.Infof("playit: hesap bağlama başlatıldı, kod %s", c.Code)

	return map[string]any{
		"code": c.Code,
		"url":  c.URL,
		"message": "Telefonunuzdan " + c.URL +
			" adresini açın ve onaylayın.",
	}, nil
}

// handlePlayitPoll reports whether the pending claim finished.
//
// Panel bunu saniyede bir çağırır. Bittiğinde ajan KENDİLİĞİNDEN başlatılır:
// kullanıcı onayladıktan sonra bir düğmeye daha basmak zorunda kalmamalı.
func (d *Daemon) handlePlayitPoll(_ context.Context, _ json.RawMessage) (any, error) {
	st := d.playit()
	st.mu.Lock()
	claim := st.claim
	st.mu.Unlock()

	if claim == nil {
		return map[string]any{
			"pending": false,
			"claimed": tunnel.Claimed(),
		}, nil
	}

	done, err := claim.Done()
	if !done {
		return map[string]any{"pending": true, "code": claim.Code,
			"url": claim.URL}, nil
	}

	st.mu.Lock()
	st.claim = nil
	st.claimErr = err
	st.claimOK = err == nil
	st.mu.Unlock()

	if err != nil {
		d.log.Warnf("playit: hesap bağlama başarısız: %v", err)
		return map[string]any{"pending": false, "claimed": false,
			"error": err.Error()}, nil
	}
	if !tunnel.Claimed() {
		return map[string]any{"pending": false, "claimed": false,
			"error": "onay tamamlandı ama gizli anahtar yazılmadı"}, nil
	}

	d.log.Infof("playit: hesap bağlandı, ajan başlatılıyor")
	d.playitEnableAutostart()
	// Yeni anahtar: eski turun "anahtar geçersiz" hatası ve beklemesi silinir.
	st.mu.Lock()
	st.tun.resetErrors()
	st.mu.Unlock()
	// YENİDEN başlat: hesap yeniden bağlandıysa playitd hâlâ eski (reddedilmiş)
	// anahtarla bekliyordur ve anahtarı yalnızca açılışta okur.
	if err := d.restartPlayitAgent(); err != nil {
		return map[string]any{"pending": false, "claimed": true,
			"error": err.Error()}, nil
	}
	return map[string]any{"pending": false, "claimed": true, "running": true,
		"message": "Hesap bağlandı, ajan çalışıyor."}, nil
}

func (d *Daemon) handlePlayitStart(_ context.Context, _ json.RawMessage) (any, error) {
	if err := d.startPlayitAgent(); err != nil {
		code := ipc.CodeInternalError
		if errors.Is(err, tunnel.ErrNotInstalled) || errors.Is(err, tunnel.ErrNotClaimed) {
			code = ipc.CodeUnavailable
		}
		return nil, &ipc.Error{Code: code, Message: err.Error()}
	}
	return map[string]any{"running": true, "message": "playit ajanı başlatıldı."}, nil
}

func (d *Daemon) handlePlayitStop(_ context.Context, _ json.RawMessage) (any, error) {
	st := d.playit()
	st.mu.Lock()
	agent := st.agent
	st.mu.Unlock()
	if agent == nil {
		return map[string]any{"running": false}, nil
	}
	if err := agent.Stop(); err != nil {
		return nil, err
	}
	d.log.Infof("playit: ajan durduruldu")
	return map[string]any{"running": false, "message": "playit ajanı durduruldu."}, nil
}

// restartPlayitAgent (re)starts playitd so it reads a new secret.
//
// Adres beklemesi YOK: Stop en kötü 7 sn sürebilir ve panel 10 sn'de zaman
// aşımına uğrar; adresi arka plan eşitleyicisi getirir.
func (d *Daemon) restartPlayitAgent() error {
	st := d.playit()
	st.mu.Lock()
	agent := st.agent
	st.mu.Unlock()
	if agent == nil {
		return errors.New("playit ajanı hazırlanmadı")
	}
	if err := agent.Restart(); err != nil {
		return err
	}
	d.kickPlayitSync()
	return nil
}

// startPlayitAgent launches playitd and waits briefly for a first address.
func (d *Daemon) startPlayitAgent() error {
	st := d.playit()
	st.mu.Lock()
	agent := st.agent
	st.mu.Unlock()
	if agent == nil {
		return errors.New("playit ajanı hazırlanmadı")
	}
	if err := agent.Start(); err != nil {
		return err
	}
	// Tüneli API'den açan eşitleyici: ajan her başladığında (hesap bağlama
	// sonrası, açılışta, elle) sunucuların tüneli olduğundan emin olur.
	d.kickPlayitSync()

	// Adresin görünmesi birkaç saniye sürer. Kısa bir süre bekleyip
	// yakalayabilirsek panel ilk yanıtta adresi gösterir; yakalayamazsak
	// yoklama zaten gösterecek. Uzun beklemiyoruz: RPC'yi 30 saniye
	// kilitlemek paneli dondurur.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if running, addr, _ := agent.Status(); running && addr != "" {
			d.log.Infof("playit: tünel adresi %s", addr)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// playitEnableAutostart turns on cfg.WAN.Autostart after a successful link.
//
// Hesabı bağlamak playit'i kullanma kararının kendisidir. Otomatik başlatma
// yalnızca sihirbazdaki "playit tünel servisi" anahtarına bakıyordu
// (DefaultConfig'te KAPALI); sihirbazda kapatıp sonradan hesabını bağlayan
// kullanıcının tüneli her yeniden başlatmada kapalı kalıyordu
// (TestPlayitClaimEnablesAutostart). Kaydedilemezse bağlama yine başarılıdır:
// yalnızca bir sonraki açılışta ajan elle başlatılır.
func (d *Daemon) playitEnableAutostart() {
	cfg := d.Config()
	if cfg == nil || cfg.WAN.Autostart {
		return
	}
	cp := *cfg
	cp.WAN.Autostart = true
	if err := d.saveConfigCopy(&cp); err != nil {
		d.log.Warnf("playit: otomatik başlatma kaydedilemedi: %v", err)
		return
	}
	d.log.Infof("playit: hesap bağlandı; ajan açılışta kendiliğinden başlayacak")
}

// autostartPlayit brings the agent up at boot when an account is bound.
//
// Kullanıcı bir kez hesabını bağladıysa, her açılışta panele girip "başlat"
// demesi anlamsızdır: tünelin amacı sunucunun İNTERNETTEN erişilebilir
// olmasıdır ve sunucu zaten otomatik başlıyor.
func (d *Daemon) autostartPlayit() {
	if !tunnel.PlayitAvailable() || !tunnel.Claimed() {
		return
	}
	if !d.Config().WAN.Autostart {
		return
	}
	if err := d.startPlayitAgent(); err != nil {
		d.log.Warnf("playit: otomatik başlatma başarısız: %v", err)
		return
	}
	d.log.Infof("playit: ajan otomatik başlatıldı")
}

// startLinkCoordinator brings up the shared-world coordinator.
//
// Başarısızlık ÖLÜMCÜL DEĞİLDİR: yalnızca ortak dünya çalışmaz. Port
// başkasındaysa (ör. kullanıcı 27892'yi başka bir şey için kullanıyorsa)
// panel bunu "koordinatör çalışmıyor" olarak gösterir.
func (d *Daemon) startLinkCoordinator() {
	coord := newLinkCoordinator(d)
	d.cluster.AttachLink(coord)
	if err := coord.Start(); err != nil {
		d.log.Warnf("link: %v", err)
		return
	}
	d.log.Infof("link: koordinatör 127.0.0.1 üzerinde dinliyor")

	// playit otomatik başlatması buraya bağlı değil ama aynı açılış
	// aşamasına ait; ayrı bir goroutine, ajan yavaş açılırsa daemon'u
	// bekletmesin.
	go d.autostartPlayit()
}
