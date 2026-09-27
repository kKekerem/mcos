package ipcclient

import "mcos/internal/ipc"

// PlayitSync is the tunnel part of playit.status, embedded in PlayitStatus.
//
// Ayrı bir tip olarak burada: link.go başka özelliklerle paylaşılıyor; playit
// tünel alanları eklenirken o dosyaya yalnızca tek satırlık gömme girdi.
type PlayitSync struct {
	Tunnels     []ipc.PlayitTunnel `json:"tunnels"`
	Syncing     bool               `json:"syncing"`
	TunnelError string             `json:"tunnelError"`
	Notices     []string           `json:"notices"`
	KeyInvalid  bool               `json:"keyInvalid"`
}

// PlayitTunnel asks the daemon to create/refresh a server's tunnel.
//
// Boş serverID = varsayılan seçim. HEMEN döner; ilerleme Playit()'ten okunur.
func (cl *Client) PlayitTunnel(serverID string) (string, error) {
	var res struct {
		Message string `json:"message"`
	}
	err := cl.call(ipc.MethodPlayitTunnel, ipc.PlayitTunnelParams{ServerID: serverID}, &res)
	return res.Message, err
}
