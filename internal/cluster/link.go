package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	// lastHash, en son UYGULADIĞIMIZ (eşten gelen) kurulumun özetidir.
	// "status" yanıtında kurucuya bildirilir (bkz. syncOnce).
	lastHash string
	// lastOrigin, uzaktan kurulumu gönderen eşin kaydı (Host düzeltmesi
	// için: kurucunun kendi bildirdiği adres yanlış arabirim olabilir).
	lastOrigin string
	// lastOriginIP, kurulumun GELDİĞİ adres. Kimlik göndermeyen eski bir
	// kurucuda dosyaları (linkfiles.go) nereden çekeceğimizi bundan biliriz.
	lastOriginIP string
	// fileMu: aynı anda iki eşitleme aynı mods/ klasörüne yazmasın (kurucu
	// kurulumu art arda iki kez gönderebilir).
	fileMu sync.Mutex
	// sharedName, en son yaydığımız ortak dünyanın adı; dünya kapatılınca
	// o sırada çevrimdışı olan eşlere "kapat" göndermek için.
	sharedName string

	kick chan struct{}
	stop chan struct{}

	// modHealth: düğüm adı -> mod portu son yoklamada yanıt verdi mi
	// (bkz. probeMods, model.LinkNode.ModReady).
	modHealth map[string]bool
}

// maxLinkEvents is how many recent link events are kept for the panel.
const maxLinkEvents = 50

// linkSyncInterval is how often the origin re-checks every peer's setup.
//
// 10 sn: eşin elindeki kurulumun özeti zaten 8 sn'de bir gelen yoklama
// yanıtında var (bkz. liveness.go); eşitleme yalnızca FARK varsa ağa çıkar,
// yani sık bakmanın bedeli neredeyse sıfır.
const linkSyncInterval = 10 * time.Second

// NewLinkCoordinator builds the coordinator.
func NewLinkCoordinator(mgr *Manager, host LinkHost) *LinkCoordinator {
	return &LinkCoordinator{mgr: mgr, host: host,
		kick: make(chan struct{}, 1)}
}

// Kick asks the sync loop to run now (e.g. right after a new pairing).
//
// Bloklamaz: bekleyen bir dürtme varsa ikincisi atılır, sonuç aynı.
func (c *LinkCoordinator) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
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
	c.stop = make(chan struct{})
	stop := c.stop
	c.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	go c.syncLoop(stop)
	return nil
}

// Stop shuts the coordinator down.
func (c *LinkCoordinator) Stop() {
	c.mu.Lock()
	srv := c.srv
	c.srv = nil
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
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
		Self:       c.mgr.name(),
		SlabChunks: model.DefaultSlabChunks,
		Hysteresis: model.HandoffHysteresisChunks,
	}

	spec, ok := c.host.LinkSpec()
	if !ok || spec.Mode != model.LinkSharedWorld {
		t.Note = "ortak dünya kapalı"
		return t
	}

	members := c.members(spec)
	nodes := c.linkNodesFrom(members)
	if len(nodes) < 2 {
		t.Note = "ortak dünya için en az iki eşleşmiş cihaz gerekir"
		t.Nodes = nodes
		return t
	}

	// Dilim sırası LİSTENİN SIRASIDIR ve BURADA YENİDEN SIRALANMAZ: kurucu
	// sıralamayı bir kez yapar, herkes aynı listeyi kullanır.
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
		if n.Self {
			t.Self = n.Name
		}
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
	// Mod portu yoklamasının sonucu (bkz. probeMods). Yanıt vermeyen düğüm
	// NOTA yazılır: panel ortak dünya özetinde bunu gösterir.
	c.mu.RLock()
	var down []string
	for i := range nodes {
		if ok, seen := c.modHealth[nodes[i].Name]; seen {
			v := ok
			nodes[i].ModReady = &v
			if !ok {
				down = append(down, fmt.Sprintf("%s (port %d)", nodes[i].Name, nodes[i].LinkPort))
			}
		}
	}
	c.mu.RUnlock()
	fillPublicAddrs(nodes, spec, c.mgr.name())
	if len(down) > 0 {
		t.Note = "ortak dünya modu yanıt vermiyor: " + strings.Join(down, ", ") +
			" — o makinede sunucu açık mı, mod kurulu mu, güvenlik duvarı izin veriyor mu?"
	}
	t.Nodes = nodes
	t.Areas = model.Territories(names, slab)
	return t
}

// fillPublicAddrs copies this machine's playit addresses into its nodes.
//
// Yalnızca yerel düğümler: eşin tünel adresini bilmiyoruz ve mod onu
// zaten kullanmaz (eşe aktarımda LAN adresi geçerlidir).
func fillPublicAddrs(nodes []model.LinkNode, spec model.LinkSpec, machine string) {
	byName := make(map[string]string, len(spec.Instances))
	for _, in := range spec.Instances {
		byName[model.InstanceNodeName(machine, in.Index)] = in.PublicAddr
	}
	for i := range nodes {
		switch {
		case nodes[i].Self:
			nodes[i].PublicAddr = spec.PublicAddr
		case nodes[i].Local:
			nodes[i].PublicAddr = byName[nodes[i].Name]
		}
	}
}

// isOrigin reports whether this machine created the shared world.
func (c *LinkCoordinator) isOrigin(spec model.LinkSpec) bool {
	return spec.OriginID == "" || spec.OriginID == c.mgr.nodeID
}

// members returns the participant list for a spec.
//
// KURUCU listeyi kendi eşleştirmelerinden hesaplar. EŞ ise kurucunun
// gönderdiği listeyi AYNEN kullanır — kendi eşleştirmelerinden kurmak,
// üç makinede her düğümün farklı bir liste görmesi demekti (bkz.
// model.LinkSpec.Members).
func (c *LinkCoordinator) members(spec model.LinkSpec) []model.LinkMember {
	if !c.isOrigin(spec) {
		c.mu.RLock()
		last, have, originKey := c.lastSpec, c.haveSpec, c.lastOrigin
		c.mu.RUnlock()
		if have && len(last.Members) > 0 && strings.EqualFold(last.ServerName, spec.ServerName) {
			out := append([]model.LinkMember(nil), last.Members...)
			// Kurucunun adresini BİZİM gördüğümüz adresle değiştir: kurucu
			// kendi adresini ilk bulduğu arabirimden okur ve o, bizim
			// ulaşabildiğimiz adres olmayabilir.
			if originKey != "" {
				for _, p := range c.mgr.Peers() {
					if p.ID != originKey || p.IP == "" {
						continue
					}
					for i := range out {
						key := "id:" + out[i].ID
						switch {
						case key == originKey:
							out[i].Host = p.IP
							out[i].Online = p.State == model.PeerAvailable
						case strings.HasPrefix(key, originKey+"#"):
							// Kurucunun aynı makinedeki kardeş kopyası:
							// adresi kurucununkiyle aynıdır; kurucu
							// kapalıysa o da kapalıdır.
							out[i].Host = p.IP
							out[i].Online = out[i].Online && p.State == model.PeerAvailable
						}
					}
				}
			}
			return out
		}
		// Kurucunun listesi henüz gelmedi (ör. bu makine yeniden başladı):
		// iki makinelik bir dünyada kendi eşleştirmelerimiz aynı listeyi
		// verir; kurucu birkaç saniye içinde listeyi yeniden gönderir.
	}
	return c.originMembers(spec)
}

// originMembers builds the participant list: this node plus paired peers.
//
// SIRA SABİT olmalı: dilim sahipliği sıradan hesaplanıyor. Ada, eşitlikte
// kimliğe göre sıralanır. AYNI ADLI iki makine (iki taze kurulumun ikisi de
// "mcos-1"dir) kimliklerinin ilk dört hanesiyle ayrılır — eskiden aynı adlı
// iki dilim oluşuyor ve mod ikisini de "kendim" sanıyordu.
func (c *LinkCoordinator) originMembers(spec model.LinkSpec) []model.LinkMember {
	self := model.LinkMember{
		ID:       c.mgr.nodeID,
		Name:     c.mgr.name(),
		Host:     "127.0.0.1",
		MCPort:   spec.Port,
		LinkPort: linkPortOr(spec.LinkPort),
		Online:   true,
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

	out := []model.LinkMember{self}
	for _, p := range c.mgr.Peers() {
		if !p.Paired {
			continue
		}
		// Esin bize bildirdigi port varsa ONU kullan; yoksa spec portu.
		mcPort := spec.Port
		if got := ports[p.ID]; got > 0 {
			mcPort = got
		}
		out = append(out, model.LinkMember{
			ID:       strings.TrimPrefix(p.ID, "id:"),
			Name:     p.Name,
			Host:     p.IP,
			MCPort:   mcPort,
			LinkPort: linkPortOr(spec.LinkPort),
			Online:   p.State == model.PeerAvailable,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	count := map[string]int{}
	for _, m := range out {
		count[m.Name]++
	}
	for i := range out {
		if count[out[i].Name] > 1 {
			id := out[i].ID
			if len(id) > 4 {
				id = id[:4]
			}
			out[i].Name = out[i].Name + "-" + id
		}
	}
	return withInstances(out, self, spec.Instances, c.mgr.name())
}

// withInstances puts this machine's sibling copies into the member list.
//
// SIRA: bu makine, hemen ardından kardeşleri, sonra eşleşmiş PC'ler (kendi
// sıralarıyla). Kardeşler bitişik dilimleri alır; böylece aynı makinedeki
// kopyalar arasındaki geçişler (en sık olanı) ağ üzerinden değil, makine
// içinde olur. Kardeş yoksa liste HİÇ DEĞİŞMEZ: var olan iki PC'lik
// dünyalarda dilim sahipliği güncellemeyle kaymamalı.
//
// Kimlik "<düğüm>#<sıra>": eşler kardeşi kurucuya bu önekle bağlar (adres
// düzeltmesi, bkz. members) ve hiçbir makinenin kimliğiyle çakışmaz.
func withInstances(out []model.LinkMember, self model.LinkMember,
	inst []model.LinkInstance, machine string) []model.LinkMember {
	if len(inst) == 0 {
		return out
	}
	res := make([]model.LinkMember, 0, len(out)+len(inst))
	var rest []model.LinkMember
	for _, m := range out {
		if m.ID == self.ID {
			res = append(res, m)
		} else {
			rest = append(rest, m)
		}
	}
	for _, in := range inst {
		res = append(res, model.LinkMember{
			ID:       fmt.Sprintf("%s#%d", self.ID, in.Index),
			Name:     model.InstanceNodeName(machine, in.Index),
			Host:     self.Host,
			MCPort:   in.MCPort,
			LinkPort: linkPortOr(in.LinkPort),
			Online:   in.Online,
		})
	}
	return append(res, rest...)
}

// linkNodesFrom turns members into what the mod and the panel read.
func (c *LinkCoordinator) linkNodesFrom(members []model.LinkMember) []model.LinkNode {
	out := make([]model.LinkNode, 0, len(members))
	for _, m := range members {
		n := model.LinkNode{
			Name: m.Name, Host: m.Host, MCPort: m.MCPort,
			LinkPort: m.LinkPort, Online: m.Online,
			// Okuyan makinenin kendi kardeşi mi (bkz. model.LinkNode.Local).
			// Ana sunucu da (kimliği düğüm kimliğinin kendisi) kardeşin
			// gözünden yereldir.
			Local: m.ID == c.mgr.nodeID || strings.HasPrefix(m.ID, c.mgr.nodeID+"#"),
		}
		if m.ID != "" && m.ID == c.mgr.nodeID {
			n.Self = true
			n.Online = true
			n.Players = c.host.PlayersOnline()
			n.LastSeen = time.Now()
		}
		out = append(out, n)
	}
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
//
// ── Yakalanan gerçek hata (Windows'ta ölçüldü) ──────────────────────────────
// Eskiden "açık ilk arabirimin ilk adresi" dönüyordu. VirtualBox kurulu bir
// Windows'ta bu 192.168.56.1 idi — sanal "Host-Only" bağdaştırıcısı; düğüm
// kullanıcıya "MCOS'ta IP gir → 192.168.56.1" diyordu ve eşleşme hiç
// olamazdı. Hyper-V/WSL (vEthernet), VMware ve Docker aynı tuzağı kurar.
//
// Artık önce VARSAYILAN YOLUN kaynak adresi alınır (UDP "bağlantısı" paket
// göndermez, yalnızca yönlendirme tablosuna sorar). İnternet yolu yoksa
// (yalnızca yerel ağ) sanal görünen arabirimler atlanarak özel ağ adresi
// seçilir.
func PrimaryIPv4() string {
	if ip := defaultRouteIPv4(); ip != "" {
		return ip
	}
	return pickLANIPv4(localIPv4s())
}

func defaultRouteIPv4() string {
	c, err := net.Dial("udp4", "1.1.1.1:53")
	if err != nil {
		return ""
	}
	defer c.Close()
	ua, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || ua.IP == nil || ua.IP.IsLoopback() || ua.IP.IsLinkLocalUnicast() {
		return ""
	}
	return ua.IP.To4().String()
}

// ifaceIPv4 is one candidate address with the name of its interface.
type ifaceIPv4 struct {
	name string
	ip   net.IP
}

func localIPv4s() []ifaceIPv4 {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifaceIPv4
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil &&
					!ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
					out = append(out, ifaceIPv4{name: ifi.Name, ip: ip4})
				}
			}
		}
	}
	return out
}

// virtualIfacePrefixes are adapters that are never the LAN a user means.
var virtualIfacePrefixes = []string{"virtualbox", "vboxnet", "vethernet", "vmware", "vmnet",
	"docker", "br-", "veth", "virbr", "hyper-v", "tailscale", "zt", "wg", "tun", "tap"}

func isVirtualIface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(n, p) || strings.Contains(n, "virtual") {
			return true
		}
	}
	return false
}

// pickLANIPv4 chooses the most likely LAN address among candidates.
//
// Sıra: sanal olmayan + özel ağ > sanal olmayan > herhangi biri. Sanal
// arabirim tek seçenekse yine döner: boş bir adres, yanlış bir adresten
// daha az yardımcıdır ama hiç adres göstermemek de kullanıcıyı çıkmaza sokar.
func pickLANIPv4(c []ifaceIPv4) string {
	best, bestScore := "", -1
	for _, x := range c {
		score := 0
		if !isVirtualIface(x.name) {
			score += 2
		}
		if x.ip.IsPrivate() {
			score++
		}
		if score > bestScore {
			best, bestScore = x.ip.String(), score
		}
	}
	return best
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
//
// Hatalar KULLANICININ DİLİNDEDİR ("anahtar yanlış", "güvenlik duvarı…"):
// panel bunları olduğu gibi gösterir.
func (c *LinkCoordinator) PushSpec(spec model.LinkSpec) []error {
	var errs []error
	for _, p := range c.mgr.Peers() {
		if !p.Paired {
			continue
		}
		if err := c.pushRecorded(p, spec); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
		}
	}
	// Eşlerin seçtiği portlar ilk turda öğrenilir; liste değiştiyse
	// eşitleme döngüsü güncel listeyi hemen yeniden yollasın.
	c.Kick()
	return errs
}

// SyncPeer pushes the current shared world to one peer right away.
//
// Eşleşmenin hemen ardından çağrılır: kullanıcının isteği "eşleyince ona da
// sunucu kurulacak" idi — ortak dünyayı yeniden açmayı beklemeden.
// ok=false: yayılacak bir ortak dünya yok (ya da kurucu biz değiliz).
func (c *LinkCoordinator) SyncPeer(p model.Peer) (msg string, ok bool, err error) {
	spec, have := c.host.LinkSpec()
	if !have || spec.Mode != model.LinkSharedWorld || !c.isOrigin(spec) {
		return "", false, nil
	}
	res, err := c.pushResult(p, c.outgoing(spec))
	c.recordPush(p, c.outgoing(spec), err)
	return res, true, err
}

// retract tells a peer we no longer share a world with it.
//
// Yalnızca kurucu gönderir ve yalnızca eşte gerçekten bizim kurduğumuz bir
// dünya varsa: eşten gelmiş bir kopya başkasının dünyasını kapatmamalı.
func (c *LinkCoordinator) retract(p model.Peer) {
	spec, have := c.host.LinkSpec()
	if !have || spec.Mode != model.LinkSharedWorld || !c.isOrigin(spec) {
		return
	}
	off := model.LinkSpec{Mode: model.LinkOff, ServerName: spec.ServerName,
		OriginID: c.mgr.nodeID}.Normalize()
	if _, err := c.pushResult(p, off); err != nil {
		if c.mgr.log != nil {
			c.mgr.log.Warnf("link: %s eşleşmesi kaldırıldı ama ortak dünyası "+
				"kapatılamadı: %v", p.Name, err)
		}
		return
	}
	if c.mgr.log != nil {
		c.mgr.log.Infof("link: %s eşleşmesi kaldırıldı, oradaki ortak dünya kapatıldı", p.Name)
	}
}

// outgoing prepares a spec for the wire: origin id and member list.
func (c *LinkCoordinator) outgoing(spec model.LinkSpec) model.LinkSpec {
	if spec.OriginID == "" {
		spec.OriginID = c.mgr.nodeID
	}
	if spec.Mode == model.LinkSharedWorld {
		spec.Members = c.members(spec)
	} else {
		spec.Members = nil
	}
	return spec
}

// pushRecorded sends a spec and records the outcome on the peer.
func (c *LinkCoordinator) pushRecorded(p model.Peer, spec model.LinkSpec) error {
	out := c.outgoing(spec)
	_, err := c.pushResult(p, out)
	c.recordPush(p, out, err)
	return err
}

// recordPush stores what the peer now has (or why it failed).
func (c *LinkCoordinator) recordPush(p model.Peer, spec model.LinkSpec, err error) {
	if err != nil {
		c.mgr.setLinkProblem(p.ID, "ortak dünya kurulamadı: "+err.Error())
		return
	}
	c.mgr.setLinkProblem(p.ID, "")
	c.mgr.mu.Lock()
	c.mgr.remoteLinkHash[p.ID] = specHash(spec)
	c.mgr.mu.Unlock()
	if spec.Mode == model.LinkSharedWorld {
		c.mu.Lock()
		c.sharedName = spec.ServerName
		c.mu.Unlock()
	}
}

// pushResult sends the spec to one peer over the cluster protocol.
func (c *LinkCoordinator) pushResult(p model.Peer, spec model.LinkSpec) (string, error) {
	port := p.Port
	if port == 0 {
		port = model.PairingPort
	}
	addr := net.JoinHostPort(p.IP, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("%s", describeNetErr(err, port))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(peerRequest{
		Method: "linkSpec",
		Token:  c.mgr.Secret(),
		Link:   &spec,
		From:   c.mgr.selfHello(),
	}); err != nil {
		return "", fmt.Errorf("%s", describeNetErr(err, port))
	}
	var res struct {
		Accepted bool   `json:"accepted"`
		Result   string `json:"result"`
		Port     int    `json:"port"`
		Error    string `json:"error"`
		Reason   string `json:"reason"`
	}
	if err := dec.Decode(&res); err != nil {
		return "", fmt.Errorf("%s", describeNetErr(err, port))
	}
	if !res.Accepted {
		return "", fmt.Errorf("%s", rejectProblem(res.Reason, res.Error))
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
	return res.Result, nil
}

// applyRemoteSpec handles an incoming linkSpec from a peer.
//
// callerKey, gönderenin eş kaydıdır (boş olabilir: eski sürüm). Kurucunun
// Minecraft portu buraya yazılır; bu makine oyuncuyu kurucuya aktarırken o
// portu kullanır.
func (c *LinkCoordinator) applyRemoteSpec(spec model.LinkSpec, callerKey, remoteIP string) (string, int, error) {
	// Özet, NORMALİZE EDİLMEDEN ÖNCE alınır: kurucu da gönderdiğini
	// olduğu gibi özetliyor; iki tarafın aynı sayıyı bulması şart.
	h := specHash(spec)
	spec = spec.Normalize()
	c.mu.Lock()
	c.lastSpec = spec
	c.haveSpec = true
	c.lastOrigin = callerKey
	c.lastOriginIP = remoteIP
	if callerKey != "" && spec.Port > 0 {
		if c.peerMCPort == nil {
			c.peerMCPort = map[string]int{}
		}
		c.peerMCPort[callerKey] = spec.Port
	}
	c.mu.Unlock()
	res, port, err := c.host.ApplyLinkSpec(spec)
	if err == nil {
		c.mu.Lock()
		c.lastHash = h
		c.mu.Unlock()
	}
	return res, port, err
}

// receivedHash is the digest of the last spec applied from a peer.
func (c *LinkCoordinator) receivedHash() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastHash
}

// RemoteSpec returns the last spec a peer pushed to us.
func (c *LinkCoordinator) RemoteSpec() (model.LinkSpec, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSpec, c.haveSpec
}

// specHash digests a spec for "does the peer already have this?" checks.
func specHash(spec model.LinkSpec) string {
	b, _ := json.Marshal(spec)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// ── Sürekli eşitleme ────────────────────────────────────────────────────────
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Kurulum eşlere YALNIZCA "ortak dünyayı aç" anında gönderiliyordu. O anda
// kapalı olan, sonradan eşleştirilen ya da yeniden başlayan bir eş hiçbir
// zaman kurulumu almıyordu: dünya iki makinede açık görünüyor, eş tarafta
// sunucu hiç yoktu. Ayrıca eşin seçtiği Minecraft portu yalnızca bellekte
// tutuluyordu; MCOS yeniden başlayınca topoloji yanlış portu gösteriyordu.
//
// Artık kurucu, her eşin elindeki kurulumun özetini (yoklama yanıtındaki
// linkHash) kendi özetiyle karşılaştırır ve fark varsa yeniden gönderir.
// Kurulum eşte zaten aynıysa ağa hiç çıkılmaz.

func (c *LinkCoordinator) syncLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(linkSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		case <-c.kick:
		}
		c.syncOnce()
		c.probeMods()
	}
}

// modProbeTimeout: bir düğümün mod portuna bağlanma üst sınırı. Yerel ağda
// bağlantı milisaniyeler sürer; kapalı port hemen RST döner. Süre yalnızca
// paketi sessizce düşüren bir güvenlik duvarı için sınırdır.
const modProbeTimeout = 1500 * time.Millisecond

// dialMod: sınamalar değiştirir (ad, sınamada iki düğüm de 127.0.0.1'de
// olduğu için ayırt etmeye yarar).
var dialMod = func(_ /* düğüm adı */, addr string) error {
	conn, err := net.DialTimeout("tcp", addr, modProbeTimeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

// probeMods checks every participant's mod-to-mod port.
//
// Ortak dünyada oyuncu verisi (envanter, konum) sınırı geçerken bu porttan
// öbür düğüme gider. Port yanıt vermiyorsa (mod kurulamadı, sunucu kapalı,
// güvenlik duvarı) aktarım çalışmaz; panel bunu "çevrimiçi" altında
// gizlememeli.
func (c *LinkCoordinator) probeMods() {
	spec, have := c.host.LinkSpec()
	if !have || spec.Mode != model.LinkSharedWorld {
		c.mu.Lock()
		c.modHealth = nil
		c.mu.Unlock()
		return
	}
	nodes := c.linkNodesFrom(c.members(spec))
	health := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		host := n.Host
		if n.Self {
			host = "127.0.0.1"
		}
		if host == "" || n.LinkPort <= 0 {
			continue
		}
		health[n.Name] = dialMod(n.Name, net.JoinHostPort(host, strconv.Itoa(n.LinkPort))) == nil
	}
	c.mu.Lock()
	c.modHealth = health
	c.mu.Unlock()
}

// syncOnce pushes the current setup to every paired peer that lacks it.
func (c *LinkCoordinator) syncOnce() {
	spec, have := c.host.LinkSpec()
	shared := have && spec.Mode == model.LinkSharedWorld
	if shared && !c.isOrigin(spec) {
		return // eşiz: kurulumu yalnızca kurucu yayar
	}

	c.mu.RLock()
	lastName := c.sharedName
	c.mu.RUnlock()

	var want model.LinkSpec
	switch {
	case shared:
		want = c.outgoing(spec)
	case lastName != "":
		// Dünya kapatıldı: o sırada çevrimdışı olan eşlere de söyle,
		// yoksa oyuncuları kapalı bir dilime göndermeye devam ederler.
		want = c.outgoing(model.LinkSpec{Mode: model.LinkOff, ServerName: lastName}.Normalize())
	default:
		return
	}
	h := specHash(want)

	for _, p := range c.mgr.Peers() {
		if !p.Paired || p.State != model.PeerAvailable {
			continue
		}
		c.mgr.mu.RLock()
		got := c.mgr.remoteLinkHash[p.ID]
		c.mgr.mu.RUnlock()
		if got == h {
			continue
		}
		if !shared && got == "" {
			continue // eşte hiç kurulum yok; kapatacak bir şey de yok
		}
		_, err := c.pushResult(p, want)
		c.recordPush(p, want, err)
		if c.mgr.log != nil {
			if err != nil {
				c.mgr.log.Warnf("link: %s eşitlenemedi: %v", p.Name, err)
			} else {
				c.mgr.log.Infof("link: %s eşitlendi (%s, %s)", p.Name, want.ServerName, want.Mode)
			}
		}
	}
}
