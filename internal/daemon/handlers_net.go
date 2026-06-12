package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/netcfg"
	"mcos/internal/store"
)

// --- Wi-Fi ----------------------------------------------------------------

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

// handleNetWiredUp brings up wired interfaces and requests DHCP. Triggered on
// demand from the OOBE / "Donanım" tab; never at boot, so it can't block.
func (d *Daemon) handleNetWiredUp(_ context.Context, _ json.RawMessage) (any, error) {
	if err := netcfg.BringUpWired(); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	d.syncClockAsync()
	return ipc.OKResult{OK: true, Message: "kablolu arabirimler getiriliyor (DHCP)"}, nil
}

// applyNetworkFromConfig (re)applies persisted Wi-Fi + timezone at startup. It
// is best-effort: failures are logged, never fatal (graceful degradation).
func (d *Daemon) applyNetworkFromConfig() {
	cfg := d.Config()
	if ssid := strings.TrimSpace(cfg.WiFiSSID); ssid != "" {
		if err := netcfg.Apply(ssid, cfg.WiFiPassword); err != nil {
			d.log.Warnf("netcfg: wifi apply: %v", err)
		}
	}
	if tz := strings.TrimSpace(cfg.Timezone); tz != "" {
		if err := netcfg.ApplyTimezone(tz); err != nil {
			d.log.Warnf("netcfg: timezone: %v", err)
		}
	}
}

// --- Minecraft version catalog --------------------------------------------

const mojangManifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"

// fallbackVersions are offered when the Mojang manifest can't be reached.
var fallbackVersions = []string{
	"1.21.4", "1.21.1", "1.20.6", "1.20.4", "1.20.1",
	"1.19.4", "1.18.2", "1.16.5", "1.12.2", "1.8.8",
}

type versionsCache struct {
	mu  sync.Mutex
	at  time.Time
	res ipc.ServerVersionsResult
}

var verCache versionsCache

func (d *Daemon) handleServerVersions(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ServerVersionsParams
	_ = decode(raw, &p)

	verCache.mu.Lock()
	fresh := time.Since(verCache.at) < time.Hour && len(verCache.res.Versions) > 0
	cached := verCache.res
	verCache.mu.Unlock()
	if fresh {
		return cached, nil
	}

	res, err := fetchMojangVersions(ctx)
	if err != nil {
		d.log.Warnf("versions: Mojang manifest unavailable (%v); using fallback", err)
		return ipc.ServerVersionsResult{Versions: fallbackVersions, Latest: fallbackVersions[0]}, nil
	}
	verCache.mu.Lock()
	verCache.res = res
	verCache.at = time.Now()
	verCache.mu.Unlock()
	return res, nil
}

func fetchMojangVersions(ctx context.Context) (ipc.ServerVersionsResult, error) {
	var out ipc.ServerVersionsResult
	var manifest struct {
		Latest struct {
			Release  string `json:"release"`
			Snapshot string `json:"snapshot"`
		} `json:"latest"`
		Versions []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"versions"`
	}
	cl := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mojangManifestURL, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := cl.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, &ipc.Error{Code: ipc.CodeUnavailable, Message: resp.Status}
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return out, err
	}
	for _, v := range manifest.Versions { // manifest is already newest-first
		if v.Type == "release" {
			out.Versions = append(out.Versions, v.ID)
		}
	}
	out.Latest = manifest.Latest.Release
	out.LatestSnapshot = manifest.Latest.Snapshot
	if len(out.Versions) == 0 {
		out.Versions = fallbackVersions
	}
	return out, nil
}
