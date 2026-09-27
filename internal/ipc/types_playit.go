package ipc

// playit tünel listesi ve playit.tunnel çağrısı. Ayrı dosyada: types.go ve
// protocol.go başka özelliklerle paylaşılıyor; playit'in sözleşmesi burada
// kendi başına okunabilsin.

// MethodPlayitTunnel asks the daemon to create (or refresh) the Minecraft
// tunnel of one server. HEMEN döner; iş arka planda yürür ve ilerlemesi
// playit.status'taki Tunnels/Syncing alanlarından okunur (panel 10 sn'de
// zaman aşımına uğrar, tünel ayırma ise dakikalar sürebilir).
const MethodPlayitTunnel = "playit.tunnel"

// PlayitTunnelParams selects the server; boş ServerID = varsayılan seçim
// (WAN açık çalışan sunucular, yoksa ilk sunucu, yoksa 25565).
type PlayitTunnelParams struct {
	ServerID string `json:"serverId,omitempty"`
}

// Playit tunnel states shown by the panel.
const (
	PlayitTunnelOpen     = "open"     // adres hazır, oyuncular bağlanabilir
	PlayitTunnelPending  = "pending"  // playit port ayırıyor
	PlayitTunnelCreating = "creating" // istek gönderildi, listede henüz yok
	PlayitTunnelDisabled = "disabled" // playit devre dışı bıraktı (Detail: neden)
	PlayitTunnelError    = "error"    // açılamadı (Detail: Türkçe neden)
)

// PlayitTunnel is one row of the tunnel list.
type PlayitTunnel struct {
	ID string `json:"id,omitempty"`
	// Name, playit panelindeki tünel adı.
	Name string `json:"name,omitempty"`
	// Address, oyuncunun yazacağı genel adres (display_address). Bekleyen
	// tünelde boştur.
	Address   string `json:"address,omitempty"`
	LocalPort int    `json:"localPort,omitempty"`
	// Type, playit tünel türü ("minecraft-java"; boş = genel TCP/UDP).
	Type  string `json:"type,omitempty"`
	State string `json:"state"`
	// Detail: bekleyen tünelin status_msg'i, devre dışı nedeni ya da hata.
	Detail string `json:"detail,omitempty"`
	// ServerID/ServerName: yerel portu bu tüneli kullanan MCOS sunucusu.
	ServerID   string `json:"serverId,omitempty"`
	ServerName string `json:"serverName,omitempty"`
}
