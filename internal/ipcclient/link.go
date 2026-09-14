package ipcclient

import (
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// Bu dosya PC EŞLEŞTİRME (etkin tarama, elle IP) ve ORTAK DÜNYA (MCOS Link)
// ile PLAYIT çağrılarını sarmalar.
//
// Ayrı bir dosyada, çünkü client.go zaten 400 satırın üzerinde ve bu üç
// konu birlikte, tek bir kullanıcı akışını oluşturuyor: eşleştir → ortak
// dünya kur → internete aç.

// ── Etkin tarama ve elle eşleştirme ─────────────────────────────────────────

// ClusterScan actively probes the LAN and returns the nodes found.
//
// Pasif keşiften (multicast) FARKLI: her adrese tek tek TCP ile bağlanmayı
// dener. Ev modemlerinin çoğu istemciler arası multicast'i engellediği için
// pasif keşif çoğu zaman hiçbir şey bulmaz.
//
// Çağrı birkaç saniye sürer; panel bu sırada radar animasyonu gösterir.
func (cl *Client) ClusterScan() ([]model.Peer, error) {
	var res struct {
		Peers []model.Peer `json:"peers"`
	}
	err := cl.call(ipc.MethodClusterScan, nil, &res)
	return res.Peers, err
}

// ClusterPairManual pairs with a typed address ("192.168.1.50" or with :port).
func (cl *Client) ClusterPairManual(addr string) (model.Peer, string, error) {
	var res struct {
		Peer    model.Peer `json:"peer"`
		Message string     `json:"message"`
	}
	err := cl.call(ipc.MethodClusterPairManual,
		map[string]string{"address": addr}, &res)
	return res.Peer, res.Message, err
}

// ClusterIdentity returns this machine's pairing key and address.
//
// Kullanıcı öbür makinede elle eşleştirme yaparken bu adresi yazar; anahtar
// ise iki makinede AYNI olmak zorundadır.
type ClusterIdentity struct {
	Secret   string `json:"secret"`
	NodeName string `json:"nodeName"`
	Port     int    `json:"port"`
	Address  string `json:"address"`
}

// ClusterSecret fetches this node's pairing identity.
func (cl *Client) ClusterSecret() (ClusterIdentity, error) {
	var res ClusterIdentity
	err := cl.call(ipc.MethodClusterSecret, nil, &res)
	return res, err
}

// ── Ortak dünya ─────────────────────────────────────────────────────────────

// LinkStatus reports the shared-world state.
func (cl *Client) LinkStatus() (model.LinkStatus, error) {
	var res model.LinkStatus
	err := cl.call(ipc.MethodLinkStatus, nil, &res)
	return res, err
}

// LinkEnable turns a server into a shared world across every paired machine.
//
// seed boş bırakılabilir: daemon üretir. Üretilen tohum SONUÇTA döner, çünkü
// kullanıcı onu görmek isteyebilir (aynı dünyayı ileride yeniden kurmak için).
func (cl *Client) LinkEnable(serverID string, diff model.LinkDifficulty,
	slabChunks int, seed string) (string, string, error) {

	var res struct {
		Message string `json:"message"`
		Seed    string `json:"seed"`
	}
	err := cl.call(ipc.MethodLinkEnable, map[string]any{
		"serverId":   serverID,
		"difficulty": string(diff),
		"slabChunks": slabChunks,
		"seed":       seed,
	}, &res)
	return res.Message, res.Seed, err
}

// LinkDisable turns the shared world off everywhere.
func (cl *Client) LinkDisable() (string, error) {
	var res struct {
		Message string `json:"message"`
	}
	err := cl.call(ipc.MethodLinkDisable, nil, &res)
	return res.Message, err
}

// LinkEvents returns recent handoff/arrival lines for the panel.
func (cl *Client) LinkEvents() ([]string, error) {
	var res []string
	err := cl.call(ipc.MethodLinkEvents, nil, &res)
	return res, err
}

// ── playit ──────────────────────────────────────────────────────────────────

// PlayitStatus is the tunnel agent state shown on the Tünel screen.
type PlayitStatus struct {
	Installed bool     `json:"installed"`
	Claimed   bool     `json:"claimed"`
	Running   bool     `json:"running"`
	Address   string   `json:"address"`
	ClaimCode string   `json:"claimCode"`
	ClaimURL  string   `json:"claimUrl"`
	Log       []string `json:"log"`
	Note      string   `json:"note"`
}

// Playit fetches the agent status.
func (cl *Client) Playit() (PlayitStatus, error) {
	var res PlayitStatus
	err := cl.call(ipc.MethodPlayitStatus, nil, &res)
	return res, err
}

// PlayitInstall verifies the agent binaries exist in the image.
func (cl *Client) PlayitInstall() (string, error) {
	var res struct {
		Message string `json:"message"`
	}
	err := cl.call(ipc.MethodPlayitInstall, nil, &res)
	return res.Message, err
}

// PlayitClaim starts account binding and returns the code and URL.
func (cl *Client) PlayitClaim() (code, url string, err error) {
	var res struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	err = cl.call(ipc.MethodPlayitClaim, nil, &res)
	return res.Code, res.URL, err
}

// PlayitPollResult reports how the pending claim is going.
type PlayitPollResult struct {
	Pending bool   `json:"pending"`
	Claimed bool   `json:"claimed"`
	Running bool   `json:"running"`
	Code    string `json:"code"`
	URL     string `json:"url"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

// PlayitPoll checks whether the user approved the claim yet.
func (cl *Client) PlayitPoll() (PlayitPollResult, error) {
	var res PlayitPollResult
	err := cl.call(ipc.MethodPlayitPoll, nil, &res)
	return res, err
}

// PlayitStart launches the agent.
func (cl *Client) PlayitStart() (string, error) {
	var res struct {
		Message string `json:"message"`
	}
	err := cl.call(ipc.MethodPlayitStart, nil, &res)
	return res.Message, err
}

// PlayitStop terminates the agent.
func (cl *Client) PlayitStop() (string, error) {
	var res struct {
		Message string `json:"message"`
	}
	err := cl.call(ipc.MethodPlayitStop, nil, &res)
	return res.Message, err
}
