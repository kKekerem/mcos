package fbpanel

import (
	"testing"

	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
)

// Tünel rozeti playit'i göstermeli: ajan çalışıyor VE bir genel adres var.
// Eskiden kaldırılmış Serveo listesine bakıyor, playit açıkken boş kalıyordu.
func TestSidebarTunnelBadgeFollowsPlayit(t *testing.T) {
	a := peersShotAppSized(t, 1280, 800)
	set := func(pl ipcclient.PlayitStatus) string {
		a.mu.Lock()
		a.playit = pl
		a.tunnels = nil
		a.mu.Unlock()
		s, _ := a.sidebarBadge(SecTunnel)
		return s
	}
	if got := set(ipcclient.PlayitStatus{Running: true}); got != "" {
		t.Errorf("adres yokken rozet %q", got)
	}
	var pl ipcclient.PlayitStatus
	pl.Running = true
	pl.Tunnels = []ipc.PlayitTunnel{{Address: "ornek.gl.joinmc.link", LocalPort: 25565}}
	if got := set(pl); got != "açık" {
		t.Errorf("playit tüneli açıkken rozet %q", got)
	}
	pl.Running = false
	if got := set(pl); got != "" {
		t.Errorf("ajan kapalıyken rozet %q (arkadaşın bağlanamayacağı adres)", got)
	}
}
