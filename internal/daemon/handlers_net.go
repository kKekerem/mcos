package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/netcfg"
	"mcos/internal/store"
)

// --- Wi-Fi ----------------------------------------------------------------

// wifiScanSession holds the state of the running (or last finished) scan.
//
// Tek oturum bilerek: aynı anda iki tarama çalıştırmak kablosuz kartı iki kez
// "scan" moduna sokar, ikisi de boş döner ve üstelik varolan bağlantıyı
// kesebilir. İkinci "başlat" isteği bu yüzden yeni bir tarama AÇMAZ, süren
// oturuma katılır.
type wifiScanSession struct {
	mu       sync.Mutex
	running  bool
	gen      int
	networks []netcfg.Network
	err      string
	started  time.Time
}

// scanSession is created lazily so a daemon built in tests needs no setup.
func (d *Daemon) scanSession() *wifiScanSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.wifiScan == nil {
		d.wifiScan = &wifiScanSession{}
	}
	return d.wifiScan
}

// handleNetWiFiScanStart starts a scan if one is not already running and
// returns immediately. The panel then polls net.wifiScanStatus.
func (d *Daemon) handleNetWiFiScanStart(_ context.Context, _ json.RawMessage) (any, error) {
	s := d.scanSession()

	s.mu.Lock()
	if s.running {
		// Süren taramaya katıl: yeni oturum açma, aynı gen'i döndür.
		out := progressOf(s)
		s.mu.Unlock()
		return out, nil
	}
	s.running = true
	s.gen++
	s.networks = nil
	s.err = ""
	s.started = time.Now()
	gen := s.gen
	out := progressOf(s)
	s.mu.Unlock()

	go func() {
		nets, err := netcfg.ScanLive(func(partial []netcfg.Network) {
			s.mu.Lock()
			// Aradan yeni bir oturum başladıysa (gen değiştiyse) bu geri
			// çağrı ESKİ taramaya aittir; sonucu yazmak yeni listeyi
			// bozardı.
			if s.gen == gen {
				s.networks = partial
			}
			s.mu.Unlock()
		})
		s.mu.Lock()
		if s.gen == gen {
			if err != nil {
				s.err = err.Error()
			} else if nets != nil {
				s.networks = nets
			}
			s.running = false
		}
		s.mu.Unlock()
	}()

	return out, nil
}

// handleNetWiFiScanStatus returns what the current scan has found so far.
func (d *Daemon) handleNetWiFiScanStatus(_ context.Context, _ json.RawMessage) (any, error) {
	s := d.scanSession()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Emniyet supabı: bir sebeple geri çağrı hiç dönmezse (tarama aracı
	// asılı kaldı) panel sonsuza kadar "aranıyor" yazmasın.
	if s.running && !s.started.IsZero() && time.Since(s.started) > wifiScanMaxAge {
		s.running = false
		if s.err == "" {
			s.err = "tarama zaman aşımına uğradı"
		}
	}
	return progressOf(s), nil
}

// wifiScanMaxAge, taramanın en geç ne zaman "bitti" sayılacağı.
//
// netcfg.ScanLive en kötü durumda arabirim başına ~3 deneme × (2+1 sn uyku) +
// hazırlık ≈ 15 sn harcar. İki kablosuz arabirimli bir makinede bu 30 sn'ye
// çıkar. 45 sn, gerçek bir taramayı erken kesmeyecek kadar uzun, asılı kalmış
// bir aracı yakalayacak kadar kısa.
const wifiScanMaxAge = 45 * time.Second

// progressOf snapshots the session. Caller must hold s.mu.
func progressOf(s *wifiScanSession) ipc.WiFiScanProgress {
	out := ipc.WiFiScanProgress{Scanning: s.running, Error: s.err, Gen: s.gen}
	out.Networks = make([]ipc.WiFiNetwork, 0, len(s.networks))
	for _, n := range s.networks {
		out.Networks = append(out.Networks,
			ipc.WiFiNetwork{SSID: n.SSID, Signal: n.Signal, Secured: n.Secured})
	}
	return out
}

func (d *Daemon) handleNetWiFiScan(_ context.Context, _ json.RawMessage) (any, error) {
	nets, err := netcfg.Scan()
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	out := make([]ipc.WiFiNetwork, 0, len(nets))
	for _, n := range nets {
		out = append(out, ipc.WiFiNetwork{SSID: n.SSID, Signal: n.Signal, Secured: n.Secured})
	}
	return ipc.WiFiScanResult{Networks: out}, nil
}

func (d *Daemon) handleNetWiFiApply(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.WiFiApplyParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.SSID) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "ssid is required"}
	}
	if err := netcfg.Apply(p.SSID, p.Password); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	// Persist so the link is restored on the next boot.
	cfg := d.Config()
	cp := *cfg
	cp.WiFiSSID = p.SSID
	cp.WiFiPassword = p.Password
	if err := store.SaveConfig(d.cfgPath, &cp); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cfg = &cp
	d.mu.Unlock()
	// Now that we (likely) have a link, fix the clock so TLS downloads work.
	d.syncClockAsync()
	return ipc.OKResult{OK: true, Message: "wifi uygulandı"}, nil
}

// handleNetWiredUp brings up wired interfaces and requests DHCP (kurulum
// sihirbazı ve Donanım sekmesi). Açılışta da çağrılıyor: applyNetworkFromConfig.
func (d *Daemon) handleNetWiredUp(_ context.Context, _ json.RawMessage) (any, error) {
	if err := netcfg.BringUpWired(); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	d.syncClockAsync()
	return ipc.OKResult{OK: true, Message: "kablolu arabirimler getiriliyor (DHCP)"}, nil
}

// applyNetworkFromConfig (re)applies persisted Wi-Fi + timezone at startup. It
// is best-effort: failures are logged, never fatal (graceful degradation).
//
// Kablolu ağ da BURADA getiriliyor ve sonra periyodik olarak denetleniyor
// (bkz. netcfg.bringUpWired): eskiden açılışta hiç getirilmiyordu ve kablolu
// sunucu yeniden başlayınca ağsız kalıyordu. İşlem bloklamıyor (DHCP arka
// planda), yani açılışı yavaşlatmaz.
func (d *Daemon) applyNetworkFromConfig() {
	if err := netcfg.BringUpWired(); err != nil {
		d.log.Warnf("netcfg: kablolu ağ: %v", err)
	}
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for range t.C {
			// Sonradan takılan USB Ethernet ya da kablo: tek kopya
			// denetimi sayesinde çalışan istemciler yeniden başlatılmaz.
			_ = netcfg.BringUpWired()
		}
	}()
	cfg := d.Config()
	if ssid := strings.TrimSpace(cfg.WiFiSSID); ssid != "" {
		// ARKA PLANDA: Apply ilişkilendirmeyi (20 sn'ye kadar) ve DHCP'yi
		// (15 sn'ye kadar) bekliyor. Run() bunu senkron çağırıyordu; kayıtlı
		// ağ menzilde değilse sunucuların kendiliğinden başlatılması, uzaktan
		// erişim ve SSH yarım dakikadan fazla gecikiyordu.
		ssid, pass := cfg.WiFiSSID, cfg.WiFiPassword
		go func() {
			if err := netcfg.Apply(ssid, pass); err != nil {
				d.log.Warnf("netcfg: wifi apply: %v", err)
			}
		}()
	}
	if tz := strings.TrimSpace(cfg.Timezone); tz != "" {
		if err := netcfg.ApplyTimezone(tz); err != nil {
			d.log.Warnf("netcfg: timezone: %v", err)
		}
	}
}

// --- Minecraft version catalog --------------------------------------------
//
// İşleyici ve yazılım başına önbellek handlers_versions.go'da. Sürüm listesi
// artık her yazılımın KENDİ kaynağından geliyor (providers.ListVersions);
// buradaki liste yalnızca en son yedek.

// fallbackVersions are offered when neither the software's own list nor the
// Mojang manifest can be reached. The reply is then marked Fallback.
//
// ── Neden bu liste ──────────────────────────────────────────────────────────
// Eski liste 1.21.4'te duruyordu. 2026-09-27'de Mojang manifestindeki en yeni
// sürüm 26.3 ve arada 1.21.5–1.21.11 ile 26.1–26.3 var; Mojang'a
// ulaşılamayan bir makinede sihirbaz varsayılan olarak 1.21.4 öneriyor, en
// yeni sürümleri hiç göstermiyordu. Sıra manifestle aynı (en yeni önce) ve
// İLK öğe "Latest" olarak döner: çevrimiçi ile çevrimdışı varsayılan aynı
// kalsın.
//
//   - 26.1 yerine 26.1.2: Paper, Purpur ve Folia düz "26.1"i YAYIMLAMIYOR
//     (fill.papermc.io ve api.purpurmc.org 404 veriyor); 26.1.2 hepsinde var
//     ve 26.1 çizgisinin en yeni düzeltmesi.
//   - 1.21.11: eski düzenin son sürümü; çevrimdışı Fabric paketi
//     (intermediary-1.21.11.jar) bu sürüm için.
//   - Ortak dünya modu bu listeyi artık SINIRLAMIYOR: mcos-link eskiden
//     yalnızca 1.21.11 için derleniyordu (1.21.1'de açılışta
//     NoSuchFieldError), şimdi 1.20.5'ten 26.3'e her sürüm grubu için ayrı
//     jar üretiliyor (make mod-fabric -> dist/mods/link/index-fabric.tsv) ve
//     daemon sunucunun sürümüne uyan jar'ı seçiyor. Listedeki 1.20.5 ve
//     sonrası sürümlerin hepsi ortak dünyaya açılabilir; 1.20.4 ve öncesi
//     açılamaz ("desteklenen: 1.20.5–26.3" hatası).
//   - 26.x Java 25 ister; çevrimdışı pakette (java25-jre) geliyor.
var fallbackVersions = []string{
	"26.3", "26.2", "26.1.2",
	"1.21.11", "1.21.10", "1.21.8", "1.21.4", "1.21.1",
	"1.20.6", "1.20.4", "1.20.1",
	"1.19.4", "1.18.2", "1.16.5", "1.12.2", "1.8.8",
}
