package cluster

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// LINK KOORDİNATÖRÜ
// ════════════════════════════════════════════════════════════════════════════
//
// Minecraft sunucusunun içindeki mcos-link modu, dünyanın hangi parçasının
// kime ait olduğunu bilmek zorundadır. Bu bilgi MCOS tarafındadır (eşleştirme
// listesi, düğüm adresleri, zorluk). Aradaki köprü budur.
//
// ── Neden HTTP, neden mcosd'nin JSON-RPC soketi değil? ──────────────────────
// mcosd, Unix alan soketinde satır bazlı JSON-RPC konuşur. Java'dan Unix
// soketi açmak 16+ sürümlerde mümkün ama Fabric mod ortamında güvenilir
// değil; ayrıca JSON-RPC çerçevelemesini Java'da yeniden yazmak gereksiz iş.
// 127.0.0.1'de küçük bir HTTP uç noktası, Java tarafında TEK satırdır:
//
//	new URL("http://127.0.0.1:27892/link/topology").openStream()
//
// ── Neden yalnızca 127.0.0.1? ───────────────────────────────────────────────
// Topoloji, eş IP'lerini ve düğüm adlarını içerir. Modun buna erişmesi için
// ağa açmak gerekmiyor: mod aynı makinede çalışıyor. Dışarı açmak, ağdaki
// herkese altyapı haritası vermek olurdu.

// linkReadTimeout bounds a mod request.
const linkReadTimeout = 5 * time.Second

// LinkHost is what the coordinator needs from the daemon.
//
// Arayüz olarak tanımlı ki cluster paketi server/supervisor paketlerini
// İTHAL ETMESİN: cluster ↔ server arasında çift yönlü bağımlılık, Go'da
// derleme hatasıdır ve mimaride her zaman bir tasarım kokusudur.
type LinkHost interface {
	// LinkSpec returns the local shared-world setup, or ok=false when the
	// feature is off.
	LinkSpec() (model.LinkSpec, bool)
	// ApplyLinkSpec creates/updates the local server to match a spec a peer
	// pushed to us. Döndürdüğü metin panele yazılır.
	// Ikinci donus, esin GERCEKTEN kullandigi Minecraft portudur; spec.Port
	// orada dolu olabilir ve baska bir port secilmis olabilir.
	ApplyLinkSpec(spec model.LinkSpec) (string, int, error)
	// PlayersOnline reports how many players are on the local shared world.
	PlayersOnline() int
}

// LinkCoordinator serves topology to the local mod and syncs peers.
type LinkCoordinator struct {
	mu       sync.RWMutex
	mgr      *Manager
	host     LinkHost
	srv      *http.Server
	handoffs int
	events   []string
	// lastSpec, eşlerden gelen en son yapılandırmadır; panel bunu gösterir.
	lastSpec model.LinkSpec
	haveSpec bool
	// peerMCPort, her esin ortak dunya sunucusunu GERCEKTEN hangi portta
	// calistirdigidir (es kimligi -> port).
	//
	// NEDEN GEREKLI: topoloji eskiden her ese kendi portumuzu yaziyordu.
	// Es o portu kullanamamis olabilir (orada baska bir sunucu vardir) ve
	// baska bir port secer. Oyuncu sinir gectiginde mod transfer paketini
	// topolojideki porta gore kurar; yanlis port, oyuncuyu ILGISIZ bir
	// sunucuya (ya da hicbir yere) gonderir.
	peerMCPort map[string]int
}

// maxLinkEvents is how many recent link events are kept for the panel.
const maxLinkEvents = 50

// NewLinkCoordinator builds the coordinator.
func NewLinkCoordinator(mgr *Manager, host LinkHost) *LinkCoordinator {
	return &LinkCoordinator{mgr: mgr, host: host}
}

// Start begins serving on 127.0.0.1:CoordinatorPort.
//
// Bağlanamamak ÖLÜMCÜL DEĞİLDİR: ortak dünya çalışmaz ama sunucular ve panel
// normal şekilde çalışmaya devam eder. Port başkası tarafından tutuluyorsa
// kullanıcı bunu panelde görür.
func (c *LinkCoordinator) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/link/topology", c.handleTopology)
	mux.HandleFunc("/link/event", c.handleEvent)
	mux.HandleFunc("/link/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1",
		strconv.Itoa(model.CoordinatorPort)))
	if err != nil {
		return fmt.Errorf("link koordinatörü dinleyemedi: %w", err)
	}
	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  linkReadTimeout,
		WriteTimeout: linkReadTimeout,
	}
	c.mu.Lock()
	c.srv = srv
	c.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Stop shuts the coordinator down.
func (c *LinkCoordinator) Stop() {
	c.mu.Lock()
	srv := c.srv
	c.srv = nil
	c.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// topology is the JSON the mod consumes.
//
// Alan adları KISA ve SABİT: Java tarafı bunları elle ayrıştırıyor
// (mcos-link'te JSON kütüphanesi bağımlılığı yok), bu yüzden bu yapı
// değiştiğinde mod da güncellenmelidir. Sürüm alanı tam bunun içindir.
type topology struct {
	// Version, mod ile koordinatör arasındaki sözleşme sürümüdür.
	// Mod, tanımadığı bir sürümde ortak dünyayı KAPATIR — yanlış
	// yorumlanmış bir topoloji, oyuncuyu var olmayan bir sunucuya
	// göndermekten daha kötüsünü yapamaz ama yine de yapmasın.
	Version int `json:"version"`

	Enabled    bool              `json:"enabled"`
	Self       string            `json:"self"`
	Difficulty string            `json:"difficulty"`
	SlabChunks int               `json:"slabChunks"`
	Hysteresis int               `json:"hysteresisChunks"`
	Nodes      []model.LinkNode  `json:"nodes"`
	Areas      []model.Territory `json:"territories"`
	Note       string            `json:"note,omitempty"`

	// Token, düğümler arası kimlik doğrulama anahtarıdır.
	//
	// Mod bunu eşlere gönderdiği her istekte taşır. Anahtar OLMADAN, ağdaki
	// herhangi biri 27893'e bağlanıp bir oyuncunun kayıt dosyasını
	// değiştirebilirdi — handoff, gelen dosyayı olduğu gibi diske yazar.
	//
	// Bu uç nokta YALNIZCA 127.0.0.1'e bağlıdır; anahtarı burada vermek,
	// aynı makinedeki moda vermek demektir.
	Token string `json:"token,omitempty"`
}

// TopologyVersion is the current contract version.
const TopologyVersion = 1

// handleTopology answers the mod's topology poll.
func (c *LinkCoordinator) handleTopology(w http.ResponseWriter, r *http.Request) {
	t := c.Topology()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(t)
}

// Topology computes the current world split.
func (c *LinkCoordinator) Topology() topology {
	t := topology{
		Version:    TopologyVersion,
		Self:       c.mgr.nodeName,
		SlabChunks: model.DefaultSlabChunks,
		Hysteresis: model.HandoffHysteresisChunks,
	}

	spec, ok := c.host.LinkSpec()
	if !ok || spec.Mode != model.LinkSharedWorld {
		t.Note = "ortak dünya kapalı"
		return t
	}

	nodes := c.linkNodes(spec)
	if len(nodes) < 2 {
		t.Note = "ortak dünya için en az iki eşleşmiş cihaz gerekir"
		t.Nodes = nodes
		return t
	}

	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	slab := spec.SlabChunks
	if slab <= 0 {
		slab = model.DefaultSlabChunks
	}

	t.Enabled = true
	t.Token = c.mgr.Secret()
	t.Difficulty = string(spec.Difficulty)
	if t.Difficulty == "" {
		t.Difficulty = string(model.DifficultyNormal)
	}
	t.SlabChunks = slab
	t.Nodes = nodes
	t.Areas = model.Territories(names, slab)
	return t
}

// linkNodes builds the participant list: this node first, then paired peers.
//
// SIRA SABİT olmalı: dilim sahipliği sıradan hesaplanıyor. Adlara göre
// sıralıyoruz, böylece HER düğüm aynı sırayı bağımsız olarak hesaplar ve
// hiçbir düğümün "listeyi kim yayınlıyor" sorusunu sorması gerekmez.
func (c *LinkCoordinator) linkNodes(spec model.LinkSpec) []model.LinkNode {
	self := model.LinkNode{
		Name:     c.mgr.nodeName,
		Host:     "127.0.0.1",
		MCPort:   spec.Port,
		LinkPort: linkPortOr(spec.LinkPort),
		Self:     true,
		Online:   true,
		Players:  c.host.PlayersOnline(),
		LastSeen: time.Now(),
	}
	// Kendi dış adresimizi bulmaya çalış: oyuncu bize aktarılırken
	// 127.0.0.1 işe yaramaz (istemci başka makinede).
	if ip := PrimaryIPv4(); ip != "" {
		self.Host = ip
	}

	c.mu.RLock()
	ports := make(map[string]int, len(c.peerMCPort))
	for k, v := range c.peerMCPort {
		ports[k] = v
	}
	c.mu.RUnlock()

	out := []model.LinkNode{self}
	for _, p := range c.mgr.Peers() {
		if !p.Paired {
			continue
		}
		// Esin bize bildirdigi port varsa ONU kullan; yoksa spec portu.
		mcPort := spec.Port
		if got := ports[p.ID]; got > 0 {
			mcPort = got
		}
		out = append(out, model.LinkNode{
			Name:     p.Name,
			Host:     p.IP,
			MCPort:   mcPort,
			LinkPort: linkPortOr(spec.LinkPort),
			Online:   p.State == model.PeerAvailable,
			LastSeen: p.LastSeen,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func linkPortOr(p int) int {
	if p <= 0 {
		return model.DefaultLinkPort
	}
	return p
}

// PrimaryIPv4 returns this machine's main LAN address.
//
// Dışa açık: panel eşleştirme ekranında "bu makinenin adresi" olarak
// gösteriyor — kullanıcı öbür makineye elle girecekse bunu görmeli.
func PrimaryIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil &&
					!ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
					return ip4.String()
				}
			}
		}
	}
	return ""
}

// linkEvent is what the mod reports back.
type linkEvent struct {
	Kind   string `json:"kind"` // "handoff" | "arrive" | "error" | "info"
	Player string `json:"player,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Text   string `json:"text,omitempty"`
}

// handleEvent records what the mod did, so the panel can show it.
//
// Bu uç nokta OLMADAN, ortak dünya "çalışıyor mu?" sorusunun yanıtı yok:
// kullanıcı sunucu günlüğüne bakmak zorunda kalırdı. Artık panel "Ahmet,
// mcos-1'den mcos-2'ye geçti" diye yazabiliyor.
func (c *LinkCoordinator) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST bekleniyor", http.StatusMethodNotAllowed)
		return
	}
	var ev linkEvent
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).
		Decode(&ev); err != nil {
		http.Error(w, "geçersiz JSON", http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	var line string
	switch ev.Kind {
	case "handoff":
		c.handoffs++
		line = fmt.Sprintf("%s → %s (%s)", ev.From, ev.To, ev.Player)
	case "arrive":
		line = fmt.Sprintf("%s geldi (%s)", ev.Player, ev.From)
	case "error":
		line = "hata: " + ev.Text
	default:
		line = ev.Text
	}
	if line != "" {
		c.events = append(c.events, time.Now().Format("15:04:05")+"  "+line)
		if len(c.events) > maxLinkEvents {
			c.events = c.events[len(c.events)-maxLinkEvents:]
		}
	}
	c.mu.Unlock()

	if c.mgr.log != nil && line != "" {
		c.mgr.log.Infof("link: %s", line)
	}
	w.WriteHeader(http.StatusNoContent)
}

// Events returns recent link activity, newest last.
func (c *LinkCoordinator) Events() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.events))
	copy(out, c.events)
	return out
}

// Handoffs returns how many player transfers happened.
func (c *LinkCoordinator) Handoffs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.handoffs
}

// ── Eşlere yayma ────────────────────────────────────────────────────────────

// PushSpec sends the shared-world setup to every paired peer.
//
// Her eş, aynı tohum/sürüm/zorlukla kendi sunucusunu kurar. Bunu ELLE
// yapmak kullanıcıdan her makinede aynı 8 alanı doldurmasını istemek
// demektir — ve bir tanesini yanlış yazarsa iki ayrı dünya oluşur, hata
// da ancak oyuncu sınırı geçtiğinde ortaya çıkar.
func (c *LinkCoordinator) PushSpec(spec model.LinkSpec) []error {
	var errs []error
	for _, p := range c.mgr.Peers() {
		if !p.Paired {
			continue
		}
		if err := c.pushOne(p, spec); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
		}
	}
	return errs
}

// pushOne sends the spec to one peer over the cluster protocol.
func (c *LinkCoordinator) pushOne(p model.Peer, spec model.LinkSpec) error {
	addr := net.JoinHostPort(p.IP, strconv.Itoa(p.Port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(peerRequest{
		Method: "linkSpec",
		Token:  c.mgr.Secret(),
		Link:   &spec,
	}); err != nil {
		return err
	}
	var res struct {
		Accepted bool   `json:"accepted"`
		Result   string `json:"result"`
		Port     int    `json:"port"`
		Error    string `json:"error"`
	}
	if err := dec.Decode(&res); err != nil {
		return err
	}
	if !res.Accepted {
		if res.Error == "" {
			res.Error = "eş reddetti"
		}
		return fmt.Errorf("%s", res.Error)
	}
	// Esin sectigi portu SAKLA: topoloji bunu kullanacak.
	if res.Port > 0 {
		c.mu.Lock()
		if c.peerMCPort == nil {
			c.peerMCPort = map[string]int{}
		}
		c.peerMCPort[p.ID] = res.Port
		c.mu.Unlock()
	}
	return nil
}

// applyRemoteSpec handles an incoming linkSpec from a peer.
func (c *LinkCoordinator) applyRemoteSpec(spec model.LinkSpec) (string, int, error) {
	c.mu.Lock()
	c.lastSpec = spec
	c.haveSpec = true
	c.mu.Unlock()
	return c.host.ApplyLinkSpec(spec)
}

// RemoteSpec returns the last spec a peer pushed to us.
func (c *LinkCoordinator) RemoteSpec() (model.LinkSpec, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSpec, c.haveSpec
}
