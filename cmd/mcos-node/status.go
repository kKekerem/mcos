package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/model"
	"mcos/internal/store"
	"mcos/internal/version"
)

// Bu dosya çalışan düğümün DURUMUNU diske yazar ve başka bir pencereden
// okunmasını sağlar.
//
// ── Neden ───────────────────────────────────────────────────────────────────
// Düğüm artık bir SERVİS gibi çalışıyor: Windows'ta oturum açılınca penceresiz,
// Linux'ta systemd kullanıcı birimiyle. Kullanıcı programa yeniden çift
// tıkladığında ikinci bir düğüm AÇILMAMALI (2222 ve 27892 portları dolu olur,
// ikinci kopya hiçbir şey yapamaz ama "çalışıyor" görünür). Bunun yerine
// arka plandaki düğümün durumu gösterilir. İki süreç arasında en basit ve
// en sağlam köprü küçük bir JSON dosyasıdır.

const statusFileName = "durum.json"

// nodeStatus is what the status screen shows.
type nodeStatusFile struct {
	PID     int       `json:"pid"`
	Name    string    `json:"name"`
	Addr    string    `json:"addr"`
	Port    int       `json:"port"`
	Note    string    `json:"note"`
	Busy    bool      `json:"busy"`
	Paired  int       `json:"paired"`
	Online  int       `json:"online"`
	Peers   []string  `json:"peers,omitempty"`
	Version string    `json:"version"`
	LogPath string    `json:"logPath"`
	Updated time.Time `json:"updated"`

	// PeerList ve Servers pencereli arayüz içindir (gui.go). Peers
	// konsol ekranı için önceden biçimlenmiş satırlardır; arayüz ise
	// çevrimiçi/çevrimdışı durumunu renkle, sorunu ayrı satırda gösteriyor
	// ve bunun için metni geri ayrıştırmak zorunda kalmamalı.
	PeerList []peerInfo   `json:"peerList,omitempty"`
	Servers  []serverInfo `json:"servers,omitempty"`

	// Offers: MCOS'tan gelen, kabul bekleyen eşleştirme istekleri (kodla
	// eşleştirme, bkz. pairing.go). Pencere ve "--kabul" buradan okur;
	// teklifler arka plandaki düğüm sürecinin belleğindedir.
	Offers []offerInfo `json:"offers,omitempty"`
	// PairEvent: son eşleştirme sonucu ("mcos-kutu ile eşleşildi").
	PairEvent *pairEvent `json:"pairEvent,omitempty"`
	// KeySet: düğümün elinde bir küme anahtarı var mı (elle girilmiş ya da
	// kodla alınmış). Anahtarın KENDİSİ bu dosyaya yazılmaz.
	KeySet bool `json:"keySet"`
}

// peerInfo is one paired MCOS box, as the window shows it.
type peerInfo struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Online  bool   `json:"online"`
	Problem string `json:"problem,omitempty"`
	Version string `json:"version,omitempty"`
}

// serverInfo is one Minecraft server hosted on this PC.
type serverInfo struct {
	Name       string `json:"name"`
	Software   string `json:"software"`
	MCVersion  string `json:"mcVersion"`
	Port       int    `json:"port"`
	State      string `json:"state"`
	Players    int    `json:"players"`
	MaxPlayers int    `json:"maxPlayers"`
	Shared     bool   `json:"shared"`
	// Region, ortak dünyada bu PC'nin tuttuğu dilimdir ("x < 0").
	Region string `json:"region,omitempty"`
}

func statusPath(dataRoot string) string { return filepath.Join(dataRoot, statusFileName) }

// snapshot builds the current status of this process.
func snapshot(h *nodeHost, mgr *cluster.Manager, s nodeSettings, port int, logPath string) nodeStatusFile {
	note, busy := h.status()
	st := nodeStatusFile{
		PID: os.Getpid(), Name: s.Name, Port: port, Note: note, Busy: busy,
		Version: version.Display(), LogPath: logPath, Updated: time.Now(),
		Addr: cluster.PrimaryIPv4(), KeySet: mgr.Secret() != "",
	}
	if h.pair != nil {
		st.Offers, st.PairEvent = h.pair.offers(), h.pair.lastEvent()
	}
	for _, p := range mgr.Peers() {
		if !p.Paired {
			continue
		}
		st.Paired++
		line := p.Name + " (" + p.IP + ")"
		if p.State == model.PeerAvailable {
			st.Online++
		} else {
			line += " — çevrimdışı"
		}
		if p.Problem != "" {
			line += " — " + p.Problem
		}
		st.Peers = append(st.Peers, line)
		st.PeerList = append(st.PeerList, peerInfo{
			Name: p.Name, IP: p.IP, Online: p.State == model.PeerAvailable,
			Problem: p.Problem, Version: p.Version,
		})
	}
	st.Servers = hostedServers(h)
	return st
}

// hostedServers lists the servers on this PC with live state and region.
func hostedServers(h *nodeHost) []serverInfo {
	if h == nil || h.st == nil {
		return nil
	}
	list, err := h.st.ListServers()
	if err != nil {
		return nil
	}
	var out []serverInfo
	for _, srv := range list {
		if h.servers != nil {
			h.servers.FillRuntime(srv)
		}
		si := serverInfo{
			Name: srv.Name, Software: string(srv.Software), MCVersion: srv.MCVersion,
			Port: srv.Port, State: string(srv.State), Players: srv.Players,
			MaxPlayers: srv.MaxPlayers, Shared: srv.Link.Mode == model.LinkSharedWorld,
		}
		if si.State == "" {
			si.State = string(model.StateStopped)
		}
		if si.Shared && h.coord != nil {
			t := h.coord.Topology()
			si.Region = selfRegion(t.Enabled, t.Self, t.Areas, t.Note)
		}
		out = append(out, si)
	}
	return out
}

// selfRegion says which slice of the shared world this PC holds.
//
// Topolojiyi mod da okur (127.0.0.1:27892); burada aynı hesaptan bu
// makinenin dilimi alınır ki pencere ile oyun AYNI şeyi söylesin.
func selfRegion(enabled bool, self string, areas []model.Territory, note string) string {
	if enabled {
		for _, a := range areas {
			if a.Node == self {
				return regionLabel(a)
			}
		}
	}
	// Dilim yoksa koordinatörün nedeni gösterilir ("en az iki eşleşmiş
	// cihaz gerekir"): boş bir hücre, kullanıcıya neden bölge atanmadığını
	// söylemez.
	return note
}

// regionLabel renders a territory in BLOCK coordinates ("x < 0").
//
// Panelin dilim çubuğuyla (fbpanel territoryLabel) aynı biçim: oyuncu F3
// ekranında blok koordinatı görür, chunk değil.
func regionLabel(t model.Territory) string {
	switch {
	case t.UnboundedMin && t.UnboundedMax:
		return "tüm dünya"
	case t.UnboundedMin:
		return fmt.Sprintf("x < %d", t.MaxChunkX*16)
	case t.UnboundedMax:
		return fmt.Sprintf("x ≥ %d", t.MinChunkX*16)
	default:
		return fmt.Sprintf("%d ≤ x < %d", t.MinChunkX*16, t.MaxChunkX*16)
	}
}

func writeStatus(dataRoot string, st nodeStatusFile) {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	tmp := statusPath(dataRoot) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, statusPath(dataRoot))
	}
}

func readStatus(dataRoot string) (nodeStatusFile, bool) {
	var st nodeStatusFile
	b, err := os.ReadFile(statusPath(dataRoot))
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, false
	}
	return st, true
}

// statusLoop keeps durum.json fresh until ctx ends, then removes it.
//
// kick: hemen yaz. Eşleştirme isteği geldiğinde pencere 2 saniyelik turu
// beklemesin — MCOS ekranında kod o an görünüyor ve kullanıcı PC'ye
// baktığında kartın orada olmasını bekliyor. Tek yazıcı bu döngüdür: iki
// goroutine aynı .tmp dosyasına yazsaydı Windows'ta yeniden adlandırma
// "erişim engellendi" ile düşebilirdi.
func statusLoop(ctx context.Context, dataRoot string, f func() nodeStatusFile, kick <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	writeStatus(dataRoot, f())
	for {
		select {
		case <-ctx.Done():
			// Kapanırken silinir: eski bir dosya, görüntüleyiciye "çalışıyor"
			// dedirtmesin.
			_ = os.Remove(statusPath(dataRoot))
			return
		case <-t.C:
			writeStatus(dataRoot, f())
		case <-kick:
			writeStatus(dataRoot, f())
		}
	}
}

// runningInstance reports whether a node for THIS data folder already serves.
//
// Yalnızca port doluluğuna bakmak yetmez: 2222'yi başka bir program (ya da
// başka veri klasörüyle çalışan başka bir düğüm) tutuyor olabilir. Yanıt
// veren düğümün kimliği bizim kimlik dosyamızla aynıysa o BİZİZ.
func runningInstance(dataRoot, bind string, port int) bool {
	st, err := store.New(dataRoot)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(st.Paths.NodeIDFile())
	if err != nil {
		return false
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	host := "127.0.0.1"
	if ip := net.ParseIP(bind); ip != nil && !ip.IsUnspecified() {
		host = bind // --dinle ile tek adrese bağlıysa geri döngüden yanıt vermez
	}
	got, err := cluster.ProbeNodeID(ctx, host, port)
	return err == nil && got == id
}

// fmtAddr renders "ip:port" for the status screen.
func fmtAddr(addr string, port int) string {
	if addr == "" {
		addr = "adres yok"
	}
	return addr + ":" + strconv.Itoa(port)
}
