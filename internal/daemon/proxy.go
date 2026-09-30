package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/model"
	"mcos/internal/portmgr"
	"mcos/internal/proxy"
	"mcos/internal/server"
)

// ════════════════════════════════════════════════════════════════════════════
// Ortak dünyanın TEK ADRESİ: kurucudaki Velocity proxy'si
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "çoklu PC bağlama çok yanlış, bildiğin yeni sunucuya
// aktarıyorsun. DonutSMP gibi modern sunucular böyle yapmıyor, sessizce
// geçiriyor. Tek bir IP'den çıkış versin hepsi, proxy olsun onları yöneten."
//
// DonutSMP'nin kurgusu: Paper sunucuları bir Velocity proxy'sinin arkasında.
// MCOS'ta da ortak dünya açıkken KURUCU makine Velocity'yi çalıştırır:
//   - proxy ana sunucunun GENEL portunu alır (ör. 25565),
//   - ana sunucu boş bir iç porta taşınır (25580+) ve kaydedilir; ortak
//     dünya kapanınca eski portuna döner,
//   - arka uçlar topolojinin düğümleridir (bu makine → 127.0.0.1:iç port,
//     kardeş kopyalar → 127.0.0.1:port, PC'ler → LAN IP:port),
//   - liste değişince Velocity YENİDEN BAŞLATILMAZ: mcos-link-velocity
//     eklentisi listeyi koordinatörden (/link/proxy) okuyup çalışan proxy'ye
//     kendisi ekler/çıkarır. Eskiden her yeni PC, IP/port değişimi ya da
//     kopya açılıp kapanması Velocity'yi yeniden başlatıyor ve bağlı herkes
//     düşüyordu ("velocity eklentisini de yap herkes düşmesin"). Eklenti
//     jar'ı yoksa eski yol (liste velocity.toml'da, değişince yeniden
//     başlatma) yedek olarak kalır.
//
// Oyuncu sınırı geçince mod, transfer paketi yerine proxy'ye "Connect
// <arka uç>" der; bağlantı kopmaz, yükleme ekranı görünmez.

// firstInternalPort, proxy açılınca ana sunucunun taşındığı ilk port.
//
// 25580: kardeşler ana portun hemen üstünden (25566…) devam eder; iç port
// onlarla çakışmasın diye biraz yukarıda başlar.
const firstInternalPort = 25580

// proxyReconcileEvery, uzlaştırma döngüsünün aralığı. Eklentisiz (yedek)
// yolda topoloji değişimi (eş eklendi, eşin portu değişti) en geç bu kadar
// sonra proxy'ye yansır; eklentili yolda eklenti 3 sn'de bir kendisi okur.
const proxyReconcileEvery = 10 * time.Second

// velocityPluginSource finds mcos-link-velocity.jar in the link jar folders.
//
// Diğer ortak dünya jar'larıyla AYNI yerlerde (linkJarDirs: imaj, kalıcı
// bölüm, dist/mods/link) ve sabit adla aranır; sürüm seçimi yok, çünkü proxy
// tek bir Velocity sürümüdür. Değişken: sınamalar sahte yol verir.
var velocityPluginSource = func() string {
	for _, dir := range linkJarDirs {
		p := filepath.Join(dir, proxy.PluginJar)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return p
		}
	}
	return ""
}

// proxyState is the daemon's proxy bookkeeping.
type proxyState struct {
	once   sync.Once
	runner *proxy.Runner
	// recMu: uzlaştırma aynı anda iki gorutinde koşmasın (döngü + başlatma
	// sonrası dürtme); ikisi birden Velocity'yi yeniden başlatırdı.
	recMu sync.Mutex

	mu      sync.Mutex
	problem string
	// plugin, son turda eklentili yolun seçilip seçilmediği (yalnızca kip
	// değişimini bir kez günlüğe yazmak için).
	plugin *bool
}

func (d *Daemon) proxyRunner() *proxy.Runner {
	d.proxySt.once.Do(func() {
		d.proxySt.runner = proxy.NewRunner(d.sup, d.proxyDir(), d.log)
	})
	return d.proxySt.runner
}

// proxyDir is Velocity's data folder: /data/proxy on the appliance.
func (d *Daemon) proxyDir() string {
	root := filepath.Clean(d.store.Paths.Root)
	if root == "/data/mcos" {
		return "/data/proxy"
	}
	return filepath.Join(root, "proxy")
}

// proxySecretPath: anahtar MCOS'un kalıcı klasöründe (/data/mcos).
func (d *Daemon) proxySecretPath() string {
	return filepath.Join(d.store.Paths.Root, "proxy.secret")
}

func (d *Daemon) setProxyProblem(s string) {
	d.proxySt.mu.Lock()
	changed := d.proxySt.problem != s
	d.proxySt.problem = s
	d.proxySt.mu.Unlock()
	if changed && s != "" {
		d.log.Warnf("proxy: %s", s)
	}
}

func (d *Daemon) proxyProblem() string {
	d.proxySt.mu.Lock()
	defer d.proxySt.mu.Unlock()
	return d.proxySt.problem
}

// pickInternalPort returns the first free port from firstInternalPort up.
func pickInternalPort(list []*model.Server, selfID string, bindable func(int) bool) int {
	used := portmgr.UsedPorts(list, selfID)
	for p := firstInternalPort; p < firstInternalPort+500; p++ {
		if !used[p] && bindable(p) {
			return p
		}
	}
	return 0
}

// moveBehindProxy gives the public port to the proxy and moves srv inside.
//
// Zaten taşınmışsa port DEĞİŞMEZ (her açılışta başka iç porta atlamak,
// eşlerin ve topolojinin sürekli değişmesi demekti); yalnızca anahtar tazelenir.
func moveBehindProxy(srv *model.Server, list []*model.Server, secret string, online bool,
	bindable func(int) bool) error {
	if px := srv.Link.Proxy; px != nil && px.PublicPort > 0 {
		px.Secret = secret
		return nil
	}
	port := pickInternalPort(list, srv.ID, bindable)
	if port == 0 {
		return fmt.Errorf("proxy için boş iç port bulunamadı (%d+)", firstInternalPort)
	}
	pub := srv.Port
	if pub <= 0 {
		pub = portmgr.DefaultPort
	}
	srv.Link.Proxy = &model.LinkProxy{Secret: secret, PublicPort: pub, OnlineMode: online}
	srv.Port = port
	return nil
}

// restoreFromProxy puts srv back on its public port; true if anything changed.
//
// server.properties ve arka uç ayarları bir sonraki açılışta işaret
// dosyasından geri alınır (bkz. server.WriteProxyBackend).
func restoreFromProxy(srv *model.Server) bool {
	px := srv.Link.Proxy
	if px == nil {
		return false
	}
	if px.PublicPort > 0 {
		srv.Port = px.PublicPort
	}
	srv.Link.Proxy = nil
	return true
}

// ensureOriginProxy puts the origin's shared world behind Velocity if it is
// not yet. true: kayıt değişti (çalışan sunucu yeniden başlamalı).
//
// Velocity jar'ı ya da Java'sı sağlanamazsa HATA döner ve hiçbir şey
// değişmez: proxy'siz ortak dünya (eski transfer yolu) çalışır, proxy'si
// olmayan bir "arka uç" ise kimseyi içeri almaz.
func (d *Daemon) ensureOriginProxy(ctx context.Context, srv *model.Server) (bool, error) {
	if srv == nil || srv.IsSibling() || srv.Link.Mode != model.LinkSharedWorld ||
		srv.Link.OriginID != "" || !srv.Software.VelocityBackend() {
		return false, nil
	}
	if srv.Link.BehindProxy() && srv.Link.Proxy.PublicPort > 0 {
		return false, d.ensureProxyMod(ctx, srv)
	}
	jar, err := resolveVelocity(ctx)
	if err != nil {
		return false, err
	}
	if _, err := d.java.Ensure(jar.Java); err != nil {
		return false, fmt.Errorf("Velocity %s için Java %d kurulamadı: %w", jar.Version, jar.Java, err)
	}
	secret, err := proxy.LoadOrCreateSecret(d.proxySecretPath())
	if err != nil {
		return false, err
	}
	// Gerçek kural proxy açılmadan ÖNCE okunur: sonra dosyada false yazar.
	rules := server.ReadLinkRules(d.serverDataDir(srv), srv)
	list, _ := d.store.ListServers()
	if err := moveBehindProxy(srv, list, secret, rules.OnlineMode, portmgr.IsBindable); err != nil {
		return false, err
	}
	if err := d.ensureProxyMod(ctx, srv); err != nil {
		restoreFromProxy(srv)
		return false, err
	}
	srv.UpdatedAt = time.Now()
	if err := d.store.SaveServer(srv); err != nil {
		return false, err
	}
	d.log.Infof("proxy: %s Velocity %s arkasına alındı (genel port %d, iç port %d)",
		srv.Name, jar.Version, srv.Link.Proxy.PublicPort, srv.Port)
	return true, nil
}

// resolveVelocity finds or downloads the Velocity jar. Değişken: sınamalar
// ağa çıkmamalı.
var resolveVelocity = proxy.Resolve

// fetchFabricProxy installs FabricProxy-Lite from Modrinth. Değişken:
// sınamalar ağa çıkmamalı.
var fetchFabricProxy = func(d *Daemon, ctx context.Context, mc, modsDir string) (string, error) {
	return d.catalog.InstallByID(ctx, "fabricproxy-lite", "fabric", mc, modsDir)
}

// ensureProxyMod installs FabricProxy-Lite on a Fabric backend.
//
// Fabric'in kendisi Velocity yönlendirmesini bilmez; mod olmadan proxy'den
// gelen oyuncu "bu sunucu çevrimdışı kipte" diye yanlış UUID ile girer ya
// da hiç giremez.
func (d *Daemon) ensureProxyMod(ctx context.Context, srv *model.Server) error {
	if srv.Software != model.SoftwareFabric || !srv.Link.BehindProxy() {
		return nil
	}
	mods := filepath.Join(d.serverDataDir(srv), "mods")
	if server.HasFabricProxy(mods) {
		return nil
	}
	if err := os.MkdirAll(mods, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if _, err := fetchFabricProxy(d, ctx, srv.MCVersion, mods); err != nil {
		return fmt.Errorf("FabricProxy-Lite (Fabric %s) kurulamadı: %w", srv.MCVersion, err)
	}
	d.log.Infof("proxy: %s: FabricProxy-Lite kuruldu", srv.Name)
	return nil
}

// restartWorld stops the main and its copies, then starts them again.
//
// Port ve online-mode yalnızca açılışta okunur: proxy açılıp kapanınca
// çalışan sunucu yeniden başlamazsa eski porttan eski kurallarla sürerdi.
func (d *Daemon) restartWorld(id string) {
	list, _ := d.store.ListServers()
	for _, sib := range siblingsOf(id, list) {
		if isLive(d.servers.State(sib.ID)) {
			d.stopAndWait(sib.ID)
		}
	}
	d.stopAndWait(id)
	fresh, err := d.store.GetServer(id)
	if err != nil {
		return
	}
	if err := d.startWithInstances(context.Background(), fresh); err != nil {
		d.log.Errorf("proxy: %s yeniden başlatılamadı: %v", fresh.Name, err)
	}
}

// enableProxyInBackground turns the proxy on for a freshly shared world.
//
// ARKA PLANDA: Velocity jar'ı (~18 MB) ve gerekirse Java indirilebilir; panel
// RPC'si bunu beklememeli.
func (d *Daemon) enableProxyInBackground(id string) {
	go func() {
		srv, err := d.store.GetServer(id)
		if err != nil {
			return
		}
		changed, err := d.ensureOriginProxy(context.Background(), srv)
		if err != nil {
			d.setProxyProblem("tek adres (proxy) açılamadı, eski aktarım kullanılıyor: " + err.Error())
			return
		}
		if c := d.cluster.LinkCoord(); c != nil {
			c.Kick() // eşler anahtarı hemen alsın
		}
		if changed && isLive(d.servers.State(id)) {
			d.log.Infof("proxy: %s tek adres için yeniden başlatılıyor", srv.Name)
			d.restartWorld(id)
		}
		d.reconcileProxy()
	}()
}

// proxyLoop keeps Velocity in line with the shared world.
func (d *Daemon) proxyLoop(ctx context.Context) {
	tk := time.NewTicker(proxyReconcileEvery)
	defer tk.Stop()
	d.reconcileProxy()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			d.reconcileProxy()
		}
	}
}

// proxyWorldLive: dünyanın bu makinedeki herhangi bir kopyası açık mı.
//
// Yalnızca ana sunucuya bakılsaydı ana sunucu çöküp yeniden başlarken
// proxy de kapanır ve EŞLERDEKİ oyuncular da düşerdi.
func (d *Daemon) proxyWorldLive(main *model.Server) bool {
	if isLive(d.servers.State(main.ID)) {
		return true
	}
	list, _ := d.store.ListServers()
	for _, sib := range siblingsOf(main.ID, list) {
		if isLive(d.servers.State(sib.ID)) {
			return true
		}
	}
	return false
}

// reconcileProxy starts, updates or stops Velocity.
func (d *Daemon) reconcileProxy() {
	if d.sup == nil || d.store == nil || d.cluster == nil {
		return // sınamalardaki yarım kurulmuş daemon
	}
	d.proxySt.recMu.Lock()
	defer d.proxySt.recMu.Unlock()
	r := d.proxyRunner()

	srv := d.sharedWorldServer()
	if srv == nil || srv.Link.OriginID != "" || !srv.Link.BehindProxy() ||
		srv.Link.Proxy.PublicPort <= 0 || !d.proxyWorldLive(srv) {
		r.Stop()
		d.setProxyProblem("")
		return
	}
	px := srv.Link.Proxy
	jar, ok := proxy.Cached()
	if !ok {
		d.setProxyProblem("Velocity jar'ı bulunamadı")
		return
	}
	rt, ok, _ := d.java.Get(jar.Java)
	if !ok || rt == nil {
		d.setProxyProblem(fmt.Sprintf("Velocity için Java %d kurulu değil", jar.Java))
		return
	}
	// Anahtar dosyası kayıttakiyle aynı olmalı: silinmiş ya da elle
	// değiştirilmişse Velocity başka bir anahtarla imzalar ve HİÇBİR arka uç
	// oyuncuyu kabul etmez.
	secretFile := d.proxySecretPath()
	if b, err := os.ReadFile(secretFile); err != nil || strings.TrimSpace(string(b)) != px.Secret {
		if err := os.WriteFile(secretFile, []byte(px.Secret), 0o600); err != nil {
			d.setProxyProblem("proxy anahtarı yazılamadı: " + err.Error())
			return
		}
	}

	backends, try := d.proxyBackendsFor(srv)
	legacy := jar.Major() > 0 && jar.Major() < 4
	// Eklenti yalnızca Velocity 4.x için derlenir (velocity-api 4.2.0, Java
	// 25); 3.x'te yüklenip yüklenmediği denenmedi, orada eski yol kalır.
	withPlugin := false
	if !legacy {
		ok, err := proxy.InstallPlugin(r.Dir(), velocityPluginSource())
		if err != nil {
			d.log.Warnf("proxy: Velocity eklentisi kopyalanamadı: %v", err)
		}
		withPlugin = ok
	}
	d.noteProxyPluginMode(withPlugin)
	motd := strings.TrimSpace(srv.MOTD)
	if motd == "" {
		motd = srv.Name
	}
	listed, maxPlayers := proxyConfBackends(backends, try, srv.MaxPlayers, withPlugin)
	conf := proxy.Render(proxy.Config{
		Bind:       net.JoinHostPort("0.0.0.0", strconv.Itoa(px.PublicPort)),
		OnlineMode: px.OnlineMode,
		SecretFile: secretFile,
		MOTD:       motd,
		MaxPlayers: maxPlayers,
		Backends:   listed,
		Try:        try,
		Legacy:     legacy,
	})
	// Port hâlâ ana sunucunun eski sürecindeyse (yeniden başlatma sürüyor)
	// Velocity bağlanamayıp çöker; bir sonraki turu beklemek daha temiz.
	if !r.Running() && !portmgr.IsBindable(px.PublicPort) {
		d.setProxyProblem(fmt.Sprintf("port %d dolu; proxy bekliyor", px.PublicPort))
		return
	}
	restarted, err := r.Apply(rt.JavaBin, jar, conf)
	if err != nil {
		d.setProxyProblem("Velocity başlatılamadı: " + err.Error())
		return
	}
	d.setProxyProblem("")
	if restarted {
		names := make([]string, len(backends))
		for i, b := range backends {
			names[i] = b.Name + "=" + b.Addr
		}
		how := ""
		if withPlugin {
			how = "; liste eklentiyle canlı güncelleniyor"
		}
		d.log.Infof("proxy: Velocity %s port %d'de (arka uçlar: %s%s)",
			jar.Version, px.PublicPort, strings.Join(names, ", "), how)
	}
}

// noteProxyPluginMode logs once when the plugin path turns on or off.
func (d *Daemon) noteProxyPluginMode(on bool) {
	d.proxySt.mu.Lock()
	prev := d.proxySt.plugin
	d.proxySt.plugin = &on
	d.proxySt.mu.Unlock()
	if prev != nil && *prev == on {
		return
	}
	if on {
		d.log.Infof("proxy: Velocity eklentisi yerinde; arka uç listesi değişince proxy yeniden başlatılmayacak")
	} else {
		d.log.Warnf("proxy: Velocity eklentisi (%s) yok; liste değişince proxy yeniden başlatılacak (oyuncular düşer)", proxy.PluginJar)
	}
}

// proxyConfBackends picks what velocity.toml lists, and its player cap.
//
// Eklentiyle yalnızca "try" sunucusu yazılır (Velocity'nin açılışta en az bir
// sunucuya ihtiyacı var; oyuncunun ilk durağı). Geri kalanını eklenti
// çalışırken ekler. Böylece velocity.toml arka uç listesi değişince
// DEĞİŞMEZ ve Runner.Apply yeniden başlatmaz — tüm oyuncuların düşmesinin
// sebebi buydu. Üst sınır da arka uç sayısına bağlı olmamalı (aynı sebep);
// asıl değeri eklenti ping yanıtına yazar (/link/proxy maxPlayers).
func proxyConfBackends(backends []proxy.Backend, try []string, maxPlayers int,
	withPlugin bool) ([]proxy.Backend, int) {
	if !withPlugin {
		return backends, maxPlayers * max(1, len(backends))
	}
	var out []proxy.Backend
	for _, b := range backends {
		for _, t := range try {
			if b.Name == t {
				out = append(out, b)
			}
		}
	}
	if len(out) == 0 {
		return backends, maxPlayers * max(1, len(backends))
	}
	return out, maxPlayers
}

// proxyBackendsFor is the live backend list for the origin's shared world.
//
// velocity.toml (yedek yol) ve /link/proxy (eklenti) AYNI listeyi buradan
// alır: iki yol farklı liste üretseydi eklenti açılıp kapandıkça oyuncular
// başka arka uçlara gönderilirdi.
func (d *Daemon) proxyBackendsFor(srv *model.Server) ([]proxy.Backend, []string) {
	var nodes []model.LinkNode
	if c := d.cluster.LinkCoord(); c != nil {
		nodes = c.Topology().Nodes
	}
	return proxyBackends(nodes, srv.Port, d.cluster.NodeName())
}

// proxyList answers the Velocity plugin's poll (/link/proxy).
//
// ok=false: bu makine şu an proxy'li bir ortak dünyanın kurucusu değil;
// eklenti o turu atlar ve elindeki listeyi korur.
func (d *Daemon) proxyList() (cluster.ProxyList, bool) {
	if d.store == nil || d.cluster == nil {
		return cluster.ProxyList{}, false
	}
	srv := d.sharedWorldServer()
	if srv == nil || srv.Link.OriginID != "" || !srv.Link.BehindProxy() ||
		srv.Link.Proxy.PublicPort <= 0 {
		return cluster.ProxyList{}, false
	}
	backends, try := d.proxyBackendsFor(srv)
	return proxyListOf(backends, try, srv.MaxPlayers), true
}

// proxyListOf turns proxy backends into the plugin's JSON shape.
//
// Adres "host:port" dizgesi olarak üretiliyor (velocity.toml onu ister);
// eklentiye ayrı host/port verilir ki Java tarafı IPv6 köşeli ayraçlarını
// ayrıştırmak zorunda kalmasın. Ayrıştırılamayan bir adres atlanır: eklenti
// onu kaydedemez, listeyi bütünüyle reddetmesi daha kötü olurdu.
func proxyListOf(backends []proxy.Backend, try []string, maxPlayers int) cluster.ProxyList {
	out := cluster.ProxyList{Backends: []cluster.ProxyServer{}}
	for _, b := range backends {
		host, ps, err := net.SplitHostPort(b.Addr)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(ps)
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		out.Backends = append(out.Backends, cluster.ProxyServer{Name: b.Name, Host: host, Port: port})
	}
	if len(try) > 0 {
		out.Try = try[0]
	}
	if maxPlayers > 0 {
		out.MaxPlayers = maxPlayers * max(1, len(out.Backends))
	}
	return out
}

// proxyBackends turns topology nodes into Velocity servers.
//
// Bu makine 127.0.0.1:iç port, kardeş kopyalar 127.0.0.1:port (aynı
// makinede LAN IP'sine gitmek gereksiz bir dolambaç), PC'ler LAN IP:port.
// Çevrimdışı düğümler de listede kalır: Velocity listesi durağandır ve her
// "çevrimiçi/çevrimdışı" değişiminde yeniden başlatmak tüm oyuncuları
// düşürürdü. try = bu makinenin sunucusu (oyuncu kurucuya girer, mod onu
// kendi dilimine geçirir).
func proxyBackends(nodes []model.LinkNode, selfPort int, machine string) ([]proxy.Backend, []string) {
	var out []proxy.Backend
	var try []string
	for _, n := range nodes {
		if n.Backend == "" {
			continue
		}
		var addr string
		switch {
		case n.Self:
			addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(selfPort))
			try = []string{n.Backend}
		case n.Local:
			if n.MCPort <= 0 {
				continue
			}
			addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(n.MCPort))
		default:
			if n.Host == "" || n.MCPort <= 0 {
				continue
			}
			addr = net.JoinHostPort(n.Host, strconv.Itoa(n.MCPort))
		}
		out = append(out, proxy.Backend{Name: n.Backend, Addr: addr})
	}
	if len(try) == 0 {
		// Koordinatör yok ya da topoloji boş: en azından bu makinenin
		// sunucusu. Ad topolojininkiyle aynı kuraldan çıkar.
		if machine == "" {
			machine = "mcos"
		}
		self := model.ProxyBackendNames([]string{machine})[0]
		out = append([]proxy.Backend{{Name: self,
			Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(selfPort))}}, out...)
		try = []string{self}
	}
	return out, try
}

// proxyAddr is the single address players use ("" = proxy kapalı).
func (d *Daemon) proxyAddr(srv *model.Server) string {
	if srv == nil || d.sup == nil || srv.Link.OriginID != "" || !srv.Link.BehindProxy() ||
		srv.Link.Proxy.PublicPort <= 0 || !d.proxyRunner().Running() {
		return ""
	}
	ip := cluster.PrimaryIPv4()
	if ip == "" {
		ip = "127.0.0.1"
	}
	return net.JoinHostPort(ip, strconv.Itoa(srv.Link.Proxy.PublicPort))
}
