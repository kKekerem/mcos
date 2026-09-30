package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/tunnel"
)

// ════════════════════════════════════════════════════════════════════════════
// playit TÜNEL EŞİTLEYİCİSİ — tüneli kullanıcı yerine MCOS açar
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "gerçek playit'i kur ve aç; gerçek hesabımı gireceğim".
// Hesap bağlandıktan sonra panel "playit.gg sitesinden tünel oluşturun"
// deyip bırakıyordu: "claim exchange" ajanı SELF-MANAGED kaydeder ve playit
// bulutu böyle bir ajana kendiliğinden tünel açmaz.
//
// Bu dosya o boşluğu kapatır. Ajan çalıştığı sürece ARKA PLANDA:
//
//  1. /v1/agents/rundata okunur (tüneller + bekleyenler + hesap durumu),
//  2. hedef sunucuların (bkz. playitPickTargets) yerel portuna giden bir
//     Minecraft Java tüneli yoksa /v1/tunnels/create ile açılır,
//  3. display_address gelene kadar rundata yeniden okunur; adres panele
//     (playit.status → Tunnels) ve sunucunun WAN.Hostname alanına yazılır.
//
// ── Neden RPC içinde değil ──────────────────────────────────────────────────
// Panel her çağrıyı 10 sn'de keser; playit'in port ayırması dakikalar
// sürebilir ve API isteği yavaş hatta tek başına 15 sn'yi bulabilir. RPC'ler
// yalnızca eşitleyiciyi DÜRTER ve anında döner; ilerleme durum çağrısından
// okunur.
//
// ── 429'dan kaçınma ─────────────────────────────────────────────────────────
// Her tur rundata'yı BİR KEZ okur (sunucu başına değil). Adres beklerken
// 5 sn → 15 sn → 30 sn aralıkla, adres gelince 2 dakikada bir okunur. 429'da
// Retry-After (en az 30 sn) katlanarak beklenir ve elle yenileme bile bu
// beklemeyi DELMEZ.

const (
	// playitDefaultPort, hiç sunucu yokken açılan tünelin yerel portu.
	playitDefaultPort = 25565
	// playitRecreateAfter: açma isteğinden sonra tünel listede bu süre
	// görünmezse yeniden açılır. Kısa tutulursa playit'in listesi gecikince
	// aynı port için İKİ tünel açılır; ücretsiz hesabın sınırı düşüktür.
	playitRecreateAfter = 3 * time.Minute
	// playitIdlePoll: bütün adresler hazırken okuma aralığı (tünel sitede
	// silinir/devre dışı kalırsa ya da yeni bir sunucu WAN açarsa fark etmek
	// için). playitd de aynı ucu kendi okuyor; dakikada birin altı gereksiz.
	playitIdlePoll = 2 * time.Minute
	// playitMaxBackoff, hata ve 429 beklemesinin tavanı.
	playitMaxBackoff = 5 * time.Minute
	// playitMinRateWait: Retry-After gelmezse ya da çok kısaysa en az bu kadar.
	playitMinRateWait = 30 * time.Second
	// playitMaxAutoCreates: aynı port için kendiliğinden en fazla bu kadar
	// açma isteği. playit isteği kabul edip tüneli listede HİÇ göstermezse
	// (beklenmeyen bir biçim farkı), 3 dakikada bir yeni tünel açıp hesabı
	// doldurmak yerine durup kullanıcıya söylüyoruz. Elle "Tünel oluştur/
	// yenile" sayacı sıfırlar.
	playitMaxAutoCreates = 2
)

// playitTargetCheck: eşitleyici beklerken hedef kümesine (hangi portlar
// tünel istiyor) bu aralıkla YEREL olarak bakar; ağ isteği yoktur, yalnızca
// manifestler okunur. Değişken: sınama kısaltır.
var playitTargetCheck = 15 * time.Second

// playitCreated remembers a create request that playit accepted.
type playitCreated struct {
	id    string
	at    time.Time
	count int // bu port için kabul edilen açma isteği sayısı
}

// playitTunnels is the sync worker's state (playitState.mu altında).
type playitTunnels struct {
	looping bool
	wake    chan struct{}
	quit    chan struct{}
	closed  bool

	// requested: kullanıcının panelden "Tünel oluştur/yenile" dediği
	// sunucular (WAN kapalı olsa da hedeftir).
	requested map[string]bool
	created   map[int]playitCreated
	// targetErr: bu turda açılamayan hedeflerin Türkçe nedeni (port → ileti).
	targetErr map[int]string

	view       []ipc.PlayitTunnel
	messages   []string
	err        string
	keyInvalid bool
	working    bool
	waiting    bool
	lastSync   time.Time
	notBefore  time.Time
	failures   int
	waitRounds int
	targetsKey string
}

func (t *playitTunnels) init() {
	if t.wake == nil {
		t.wake = make(chan struct{}, 1)
		t.quit = make(chan struct{})
	}
	if t.requested == nil {
		t.requested = map[string]bool{}
		t.created = map[int]playitCreated{}
		t.targetErr = map[int]string{}
	}
}

// shutdown stops the worker for good (daemon çıkışı).
func (t *playitTunnels) shutdown() {
	t.init()
	if !t.closed {
		t.closed = true
		close(t.quit)
	}
}

// resetErrors forgets failures after the account is linked again.
//
// 429 beklemesi (notBefore) SİLİNMEZ: yeni anahtar playit'in hız sınırını
// sıfırlamaz; hemen yeniden denemek yalnızca bir 429 daha getirir.
func (t *playitTunnels) resetErrors() {
	t.init()
	t.err = ""
	t.keyInvalid = false
	t.failures = 0
	t.waitRounds = 0
	t.targetErr = map[int]string{}
}

// ── Hedef seçimi ────────────────────────────────────────────────────────────

// playitTarget is a local port that should have a tunnel.
type playitTarget struct {
	ServerID string
	Name     string // playit'teki tünel adı (TunnelName'den geçmiş)
	Port     int
	TunnelID string // sunucunun kayıtlı tünel kimliği
}

func serverPort(s *model.Server) int {
	// Ortak dünya proxy'si açıkken sunucu iç porttadır; dışarıya açılacak
	// TEK adres proxy'nin (sunucunun eski, genel) portudur.
	if px := s.Link.Proxy; px != nil && px.PublicPort > 0 && s.Link.BehindProxy() {
		return px.PublicPort
	}
	if s.Port > 0 {
		return s.Port
	}
	return playitDefaultPort
}

// proxyOnlyBackend: sunucu başka bir makinenin/ana sunucunun proxy'sinin
// arkasında bir arka uç. Ona tünel açılmaz: proxy'yi atlayan bağlantıyı
// zaten reddeder ve kullanıcının isteği "tek bir IP'den çıkış" idi.
func proxyOnlyBackend(s *model.Server) bool {
	return s.Link.BehindProxy() && s.Link.Proxy.PublicPort <= 0
}

func isLive(st model.ServerState) bool {
	return st == model.StateRunning || st == model.StateStarting
}

// playitPickTargets decides which local ports get a tunnel.
//
// Sıra:
//  1. WAN'ı açık ve ÇALIŞAN (ya da açılan) her sunucu,
//  2. kullanıcının panelden istediği sunucular (durumu ne olursa olsun),
//  3. bunlar yoksa: ilk çalışan sunucu; o da yoksa ilk sunucu; hiç sunucu
//     yoksa 25565. Hesabı yeni bağlayan kullanıcı henüz WAN anahtarını
//     bilmiyor olabilir — "tünel kendiliğinden açılsın" isteğini boş bir
//     ekranla karşılamak yerine en olası portu açıyoruz.
//
// Aynı porttaki iki sunucu TEK tünel paylaşır (aynı anda yalnızca biri
// çalışabilir).
func playitPickTargets(servers []*model.Server, state func(string) model.ServerState,
	requested map[string]bool) []playitTarget {
	var out []playitTarget
	seen := map[int]bool{}
	add := func(s *model.Server) {
		p := serverPort(s)
		if seen[p] || proxyOnlyBackend(s) {
			return
		}
		seen[p] = true
		out = append(out, playitTarget{ServerID: s.ID, Name: tunnel.TunnelName("MCOS " + s.Name),
			Port: p, TunnelID: s.WAN.TunnelID})
	}
	for _, s := range servers {
		if s.WAN.Enabled && isLive(state(s.ID)) {
			add(s)
		}
	}
	for _, s := range servers {
		if requested[s.ID] {
			add(s)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, s := range servers {
		if isLive(state(s.ID)) {
			add(s)
			return out
		}
	}
	if len(servers) > 0 {
		add(servers[0])
		return out
	}
	return []playitTarget{{Name: tunnel.TunnelName("MCOS Minecraft"), Port: playitDefaultPort}}
}

func targetsKey(ts []playitTarget) string {
	var b strings.Builder
	for _, t := range ts {
		b.WriteString(t.ServerID)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(t.Port))
		b.WriteByte(';')
	}
	return b.String()
}

func (d *Daemon) serverState(id string) model.ServerState {
	if d.servers == nil {
		return model.StateStopped
	}
	return d.servers.State(id)
}

func (d *Daemon) listServersSorted() []*model.Server {
	if d.store == nil {
		return nil
	}
	servers, _ := d.store.ListServers()
	// Kararlı sıra: "ilk sunucu" her turda aynı sunucu olmalı, yoksa tünel
	// sunucudan sunucuya atlar.
	sort.SliceStable(servers, func(i, j int) bool {
		if !servers[i].CreatedAt.Equal(servers[j].CreatedAt) {
			return servers[i].CreatedAt.Before(servers[j].CreatedAt)
		}
		return servers[i].ID < servers[j].ID
	})
	return servers
}

// ── Eşitleyici döngüsü ──────────────────────────────────────────────────────

// kickPlayitSync wakes (or starts) the background worker.
func (d *Daemon) kickPlayitSync() {
	st := d.playit()
	st.mu.Lock()
	t := &st.tun
	t.init()
	if t.closed {
		st.mu.Unlock()
		return
	}
	select {
	case t.wake <- struct{}{}:
	default:
	}
	start := !t.looping
	t.looping = true
	wake, quit := t.wake, t.quit
	st.mu.Unlock()
	if start {
		go d.playitSyncLoop(wake, quit)
	}
}

// kickPlayitSyncIfTargetsChanged reacts to a server starting/stopping.
//
// handlers.go'daki sunucu başlatma yoluna dokunmadan: panel playit.status'u
// çağırdıkça hedef kümesi yeniden hesaplanır; değiştiyse (ör. WAN'ı açık bir
// sunucu yeni başladı) 2 dakikalık boşta beklemeyi beklemeden tur atılır.
func (d *Daemon) kickPlayitSyncIfTargetsChanged() {
	if d.playitTargetsChanged() {
		d.kickPlayitSync()
	}
}

// playitTargetsChanged reports whether the target set differs from the one
// the last round worked on. Yalnızca yerel: manifestler okunur, ağ yok.
func (d *Daemon) playitTargetsChanged() bool {
	st := d.playit()
	st.mu.Lock()
	st.tun.init()
	req := copyReq(st.tun.requested)
	prev := st.tun.targetsKey
	st.mu.Unlock()
	return targetsKey(playitPickTargets(d.listServersSorted(), d.serverState, req)) != prev
}

func copyReq(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (d *Daemon) playitAgentRunning() bool {
	st := d.playit()
	st.mu.Lock()
	agent := st.agent
	st.mu.Unlock()
	if agent == nil {
		return false
	}
	running, _, _ := agent.Status()
	return running
}

func (d *Daemon) playitSyncLoop(wake, quit chan struct{}) {
	st := d.playit()
	// Aralık döngü başında BİR KEZ okunur: sınama değişkeni geri yüklerken
	// döngü hâlâ dönüyor olabilir (-race bunu yakaladı).
	checkEvery := playitTargetCheck
	for {
		// Ajan durduysa döngü biter (kapalı ajana tünel açmanın anlamı yok,
		// API'yi boşuna yormayalım); yeniden başlatılınca kickPlayitSync
		// yenisini açar.
		if !d.playitAgentRunning() {
			st.mu.Lock()
			st.tun.looping = false
			closed := st.tun.closed
			st.mu.Unlock()
			// Yarış: ajan tam bu arada başladıysa onun dürtmesi "döngü zaten
			// var" deyip düşmüş olabilir. looping'i bıraktıktan SONRA bir kez
			// daha bakıp gerekirse döngüyü kendimiz yeniden açıyoruz.
			if !closed && d.playitAgentRunning() {
				d.kickPlayitSync()
			}
			return
		}
		// Bu turdan ÖNCE gelen dürtmeler bu turla karşılanır.
		select {
		case <-wake:
		default:
		}
		delay := d.playitSyncOnce()

		// Beklerken hedef kümesine YEREL olarak da bakılır: WAN'ı açık bir
		// sunucu başladığında ya da ilk sunucu oluşturulduğunda tünel, panel
		// tünel ekranında olmasa da ~15 sn içinde açılsın (eskiden 2 dk'lık
		// boşta okumayı bekliyordu; TestPlayitIdleLoopNoticesNewTarget).
		timer := time.NewTimer(delay)
		check := time.NewTicker(checkEvery)
	wait:
		for {
			select {
			case <-quit:
				timer.Stop()
				check.Stop()
				st.mu.Lock()
				st.tun.looping = false
				st.mu.Unlock()
				return
			case <-wake:
				// Dürtme 429 beklemesini delmez: playitSyncOnce notBefore'a bakar.
				break wait
			case <-timer.C:
				break wait
			case <-check.C:
				if d.playitTargetsChanged() {
					break wait
				}
			}
		}
		timer.Stop()
		check.Stop()
	}
}

// playitRound is the outcome of one sync pass (kilit dışında hesaplanır).
type playitRound struct {
	rd        *tunnel.PlayitRunData
	err       error
	targets   []playitTarget
	created   map[int]playitCreated // bu turda açılanlar
	targetErr map[int]string
	servers   []*model.Server
}

// playitSyncOnce runs one pass and returns how long to wait before the next.
func (d *Daemon) playitSyncOnce() time.Duration {
	st := d.playit()
	now := time.Now()
	st.mu.Lock()
	t := &st.tun
	t.init()
	if wait := t.notBefore.Sub(now); wait > 0 {
		st.mu.Unlock()
		return wait
	}
	t.working = true
	req := copyReq(t.requested)
	created := make(map[int]playitCreated, len(t.created))
	for k, v := range t.created {
		created[k] = v
	}
	st.mu.Unlock()

	r := d.playitRoundRun(req, created, now)

	st.mu.Lock()
	defer st.mu.Unlock()
	t.working = false
	t.lastSync = time.Now()
	t.targetsKey = targetsKey(r.targets)
	for p, c := range r.created {
		t.created[p] = c
	}
	t.targetErr = r.targetErr
	return d.playitApplyRound(t, r, now)
}

// playitApplyRound folds a round into the state and picks the next delay.
func (d *Daemon) playitApplyRound(t *playitTunnels, r playitRound, now time.Time) time.Duration {
	if r.err != nil {
		t.err = r.err.Error()
		if wait, limited := tunnel.RateLimited(r.err); limited {
			if wait < playitMinRateWait {
				wait = playitMinRateWait
			}
			wait <<= uint(min(t.failures, 3))
			if wait > playitMaxBackoff {
				wait = playitMaxBackoff
			}
			t.failures++
			t.notBefore = now.Add(wait)
			d.logf("playit: 429, %s bekleniyor", wait)
			return wait
		}
		if tunnel.KeyInvalid(r.err) || errors.Is(r.err, tunnel.ErrNotClaimed) {
			t.keyInvalid = tunnel.KeyInvalid(r.err)
			return playitMaxBackoff
		}
		t.failures++
		wait := 15 * time.Second << uint(min(t.failures-1, 5))
		if wait > playitMaxBackoff {
			wait = playitMaxBackoff
		}
		d.logf("playit: eşitleme hatası (%d. kez): %v", t.failures, r.err)
		// Eski görünüm KORUNUR: bir ağ kesintisi, ekrandaki çalışan adresi
		// silmemeli.
		return wait
	}

	t.err = ""
	t.keyInvalid = false
	t.failures = 0
	t.messages = r.rd.Messages()
	t.view = playitBuildView(r.rd, r.servers, d.serverState, r.targets, t.created, r.targetErr, now)
	// Hedef hatası da tur hatası sayılır (ekranda görünsün) ama bekleme
	// uzamaz: rundata okunabiliyor, bir sonraki tur yeniden dener.
	for _, tg := range r.targets {
		if m := r.targetErr[tg.Port]; m != "" {
			t.err = m
			break
		}
	}
	t.waiting = false
	for _, tg := range r.targets {
		if _, ok := openFor(r.rd, tg); !ok {
			t.waiting = true
		}
	}
	if !t.waiting {
		t.waitRounds = 0
		return playitIdlePoll
	}
	t.waitRounds++
	switch {
	case t.waitRounds <= 6: // ilk ~30 sn
		return 5 * time.Second
	case t.waitRounds <= 18: // sonraki ~3 dk
		return 15 * time.Second
	default:
		return 30 * time.Second
	}
}

func (d *Daemon) logf(format string, args ...any) {
	if d.log != nil {
		d.log.Infof(format, args...)
	}
}

// openFor finds the target's tunnel that already has a public address.
func openFor(rd *tunnel.PlayitRunData, tg playitTarget) (tunnel.PlayitTunnel, bool) {
	t, ok := tunnel.FindMinecraftTunnel(rd, tg.Port, tg.Name, tg.TunnelID)
	if !ok || t.Pending || t.Address == "" {
		return tunnel.PlayitTunnel{}, false
	}
	return t, true
}

// playitRoundRun does the network work of one pass. Kilit TUTULMAZ.
func (d *Daemon) playitRoundRun(req map[string]bool, created map[int]playitCreated, now time.Time) playitRound {
	r := playitRound{created: map[int]playitCreated{}, targetErr: map[int]string{}}
	// Hedefler ağdan ÖNCE hesaplanır: tur hata verse de targetsKey doğru
	// kalmalı. Eskiden hatalı turda hedefler boş kalıyor, targetsKey "" oluyor
	// ve panelin her playit.status yoklaması (watchTunnel, 2 sn'de bir)
	// "hedefler değişti" sanıp eşitleyiciyi dürtüyordu: iç hata ya da ağ
	// kesintisinde 15 sn → 5 dk'lık bekleme deliniyor, playit'e 2 sn'de bir
	// istek gidiyordu (TestPlayitStatusPollDoesNotHammerOnError: 0,8 sn'de
	// 9 istek).
	r.servers = d.listServersSorted()
	r.targets = playitPickTargets(r.servers, d.serverState, req)
	api, err := tunnel.NewPlayitAPI()
	if err != nil {
		r.err = err
		return r
	}
	rd, err := api.RunData()
	if err != nil {
		r.err = err
		return r
	}
	r.rd = rd

	for _, tg := range r.targets {
		known := tg.TunnelID
		if c, ok := created[tg.Port]; ok && known == "" {
			known = c.id
		}
		if _, ok := tunnel.FindMinecraftTunnel(rd, tg.Port, tg.Name, known); ok {
			continue
		}
		// Az önce açtık ama playit listesine henüz düşmedi: yenisini AÇMA.
		if c, ok := created[tg.Port]; ok && now.Sub(c.at) < playitRecreateAfter {
			continue
		}
		if c, ok := created[tg.Port]; ok && c.count >= playitMaxAutoCreates {
			r.targetErr[tg.Port] = "playit tüneli kabul etti ama listede göstermiyor; " +
				"playit.gg → Tunnels sayfasını denetleyip burada yeniden deneyin"
			continue
		}
		res, err := api.EnsureMinecraftTunnelIn(rd, tg.Port, tg.Name, known)
		if err != nil {
			// Elle açma ipucu BU sunucunun portunu söylesin (bkz. MessageForPort).
			r.targetErr[tg.Port] = tunnel.MessageForPort(err, tg.Port)
			d.logf("playit: %d için tünel açılamadı: %v", tg.Port, err)
			if _, limited := tunnel.RateLimited(err); limited {
				r.err = err
				break
			}
			continue
		}
		if res.Created {
			d.logf("playit: %d portu için Minecraft tüneli açıldı (%s)", tg.Port, res.Tunnel.ID)
			r.created[tg.Port] = playitCreated{id: res.Tunnel.ID, at: now,
				count: created[tg.Port].count + 1}
		}
	}
	// Önceki turlarda açılıp listede henüz görünmeyenler de sayılır; yoksa
	// kayıtlı tünel kimliği "sitede silinmiş" sanılıp silinirdi.
	recent := map[int]playitCreated{}
	for p, c := range created {
		if now.Sub(c.at) < playitRecreateAfter {
			recent[p] = c
		}
	}
	for p, c := range r.created {
		recent[p] = c
	}
	d.playitSaveHostnames(rd, r.servers, recent)
	return r
}

// playitSaveHostnames writes each server's public address into its manifest.
// recent: yakın zamanda açma isteği kabul edilmiş portlar.
//
// Sunucu ayrıntı ekranı "Genel adres" satırını WAN.Hostname'den gösterir;
// adres yalnızca playit ekranında kalsaydı kullanıcı arkadaşına vereceği
// adresi sunucunun kendi ekranında bulamazdı. Yalnızca DEĞİŞİNCE yazılır ve
// yazmadan hemen önce manifest yeniden okunur (başka bir güncellemeyi
// ezmemek için).
func (d *Daemon) playitSaveHostnames(rd *tunnel.PlayitRunData, servers []*model.Server,
	recent map[int]playitCreated) {
	if d.store == nil || rd == nil {
		return
	}
	for _, s := range servers {
		known := s.WAN.TunnelID
		if c, ok := recent[serverPort(s)]; ok && known == "" {
			known = c.id
		}
		host, id := "", ""
		if t, ok := tunnel.FindMinecraftTunnel(rd, serverPort(s), "", known); ok {
			id = t.ID
			if !t.Pending {
				host = t.Address
			}
		} else if c, ok := recent[serverPort(s)]; ok {
			id = c.id
		} else if known != "" {
			// Kayıtlı tünel artık yok (sitede silinmiş): eski adres yanıltır.
			id, host = "", ""
		} else {
			continue
		}
		if host == s.WAN.Hostname && id == s.WAN.TunnelID {
			continue
		}
		fresh, err := d.store.GetServer(s.ID)
		if err != nil || fresh == nil {
			continue
		}
		if host == fresh.WAN.Hostname && id == fresh.WAN.TunnelID {
			continue
		}
		fresh.WAN.Hostname = host
		fresh.WAN.TunnelID = id
		if err := d.store.SaveServer(fresh); err != nil {
			d.logf("playit: %s adresi kaydedilemedi: %v", s.ID, err)
			continue
		}
		if host != "" {
			d.logf("playit: %s genel adresi %s", s.Name, host)
		}
	}
}

// playitBuildView turns rundata into the panel's tunnel list.
//
// Önce hedefler (sunucu sırasıyla), sonra hesaptaki diğer tüneller. Açma
// isteği kabul edilip listede henüz görünmeyenler "creating", açılamayanlar
// "error" satırı olarak eklenir — kullanıcı boş bir ekrana bakıp beklemesin.
func playitBuildView(rd *tunnel.PlayitRunData, servers []*model.Server,
	state func(string) model.ServerState, targets []playitTarget,
	created map[int]playitCreated, targetErr map[int]string, now time.Time) []ipc.PlayitTunnel {
	byPort := func(port int) *model.Server {
		var first *model.Server
		for _, s := range servers {
			if serverPort(s) != port {
				continue
			}
			if isLive(state(s.ID)) {
				return s
			}
			if first == nil {
				first = s
			}
		}
		return first
	}
	row := func(t tunnel.PlayitTunnel) ipc.PlayitTunnel {
		v := ipc.PlayitTunnel{ID: t.ID, Name: t.Name, Address: t.Address,
			LocalPort: t.LocalPort, Type: t.TunnelType, State: ipc.PlayitTunnelOpen}
		switch {
		case t.Disabled != "":
			v.State, v.Detail = ipc.PlayitTunnelDisabled, t.Disabled
		case t.Pending || t.Address == "":
			v.State, v.Detail = ipc.PlayitTunnelPending, t.StatusMsg
		}
		return v
	}
	annotate := func(v *ipc.PlayitTunnel, serverID string) {
		var s *model.Server
		if serverID != "" {
			for _, x := range servers {
				if x.ID == serverID {
					s = x
				}
			}
		}
		if s == nil && v.LocalPort > 0 {
			s = byPort(v.LocalPort)
		}
		if s != nil {
			v.ServerID, v.ServerName = s.ID, s.Name
		}
	}

	var out []ipc.PlayitTunnel
	used := map[string]bool{}
	for _, tg := range targets {
		known := tg.TunnelID
		if c, ok := created[tg.Port]; ok && known == "" {
			known = c.id
		}
		t, ok := tunnel.FindMinecraftTunnel(rd, tg.Port, tg.Name, known)
		var v ipc.PlayitTunnel
		switch {
		case ok:
			v = row(t)
			used[t.ID] = true
			if v.LocalPort == 0 {
				v.LocalPort = tg.Port
			}
		case targetErr[tg.Port] != "":
			v = ipc.PlayitTunnel{Name: tg.Name, LocalPort: tg.Port,
				Type: tunnel.TunnelTypeMinecraftJava, State: ipc.PlayitTunnelError,
				Detail: targetErr[tg.Port]}
		default:
			v = ipc.PlayitTunnel{Name: tg.Name, LocalPort: tg.Port,
				Type: tunnel.TunnelTypeMinecraftJava, State: ipc.PlayitTunnelCreating}
			if c, ok := created[tg.Port]; ok {
				v.ID = c.id
				used[c.id] = true
			}
		}
		annotate(&v, tg.ServerID)
		out = append(out, v)
	}
	if rd == nil {
		return out
	}
	for _, t := range rd.Tunnels {
		if !used[t.ID] {
			v := row(t)
			annotate(&v, "")
			out = append(out, v)
		}
	}
	for _, t := range rd.Pending {
		if !used[t.ID] {
			out = append(out, row(t))
		}
	}
	return out
}

// fillPlayitTunnels copies the worker's view into a status result.
func (d *Daemon) fillPlayitTunnels(res *playitStatusResult) {
	st := d.playit()
	st.mu.Lock()
	defer st.mu.Unlock()
	t := &st.tun
	res.Tunnels = append([]ipc.PlayitTunnel(nil), t.view...)
	res.Notices = append([]string(nil), t.messages...)
	res.TunnelError = t.err
	res.Syncing = t.working || t.waiting
	if t.keyInvalid {
		res.KeyInvalid = true
	}
	// Genel adres: sunucuya bağlı ilk AÇIK tünel; yoksa herhangi bir açık tünel.
	for pass := 0; pass < 2 && res.Address == ""; pass++ {
		for _, v := range t.view {
			if v.State == ipc.PlayitTunnelOpen && v.Address != "" &&
				(pass == 1 || v.ServerID != "") {
				res.Address = v.Address
				break
			}
		}
	}
}

// handlePlayitTunnel creates/refreshes a server's tunnel in the background.
//
// ANINDA döner. Ajan kapalıysa önce onu açar (en fazla ~3 sn; panelin 10 sn
// sınırının içinde). Tünel zaten varsa yenisi açılmaz — yalnızca rundata
// yeniden okunur ve adres tazelenir.
func (d *Daemon) handlePlayitTunnel(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.PlayitTunnelParams
	if len(raw) > 0 && string(raw) != "null" {
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
	}
	if !tunnel.PlayitAvailable() {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: tunnel.ErrNotInstalled.Error()}
	}
	if !tunnel.Claimed() {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable,
			Message: "Önce playit hesabını bağlayın (Tünel → 2. adım)."}
	}
	name, port := "", 0
	if p.ServerID != "" {
		// Kimlik yol parçasıdır (servers/<id>/manifest.json) ve bu çağrı
		// telefon köprüsünden de gelir: ".." servers/ dışındaki bir
		// manifest.json'u sunucu diye okutuyordu. Dosya işlemlerinin
		// kullandığı doğrulamanın aynısı (requireServer).
		if e := d.requireServer(p.ServerID); e != nil {
			return nil, e
		}
		srv, err := d.store.GetServer(p.ServerID)
		if err != nil || srv == nil {
			return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "sunucu bulunamadı"}
		}
		name, port = srv.Name, serverPort(srv)
	}

	st := d.playit()
	st.mu.Lock()
	st.tun.init()
	if p.ServerID != "" {
		st.tun.requested[p.ServerID] = true
	}
	// Elle istek, kendiliğinden açma sınırını (playitMaxAutoCreates) açar.
	if c, ok := st.tun.created[port]; ok {
		c.count = 0
		st.tun.created[port] = c
	}
	// Elle yenileme: önceki hata beklemesi sıfırlanır (429 hariç).
	st.tun.failures = 0
	st.tun.waitRounds = 0
	st.mu.Unlock()

	if !d.playitAgentRunning() {
		if err := d.startPlayitAgent(); err != nil {
			code := ipc.CodeInternalError
			if errors.Is(err, tunnel.ErrNotInstalled) || errors.Is(err, tunnel.ErrNotClaimed) {
				code = ipc.CodeUnavailable
			}
			return nil, &ipc.Error{Code: code, Message: err.Error()}
		}
	} else {
		d.kickPlayitSync()
	}

	msg := "Tünel hazırlanıyor; adres birkaç saniye içinde görünür."
	if name != "" {
		msg = name + ": " + msg
	}
	return map[string]any{"started": true, "message": msg}, nil
}
