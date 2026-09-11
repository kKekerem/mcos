// Package cluster implements LAN peer discovery (UDP multicast beacon), pairing,
// and a simple task-distribution protocol over TCP. It is designed to work
// gracefully even when there are no other peers on the network.
package cluster

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/store"
	"mcos/internal/sysmon"
	"mcos/internal/tier"
)

const (
	multicastAddr = "239.255.0.1:27891"
	beaconPeriod  = 5 * time.Second
	peerTimeout   = 20 * time.Second

	// peerConnTimeout, bir eş bağlantısının toplam ömrü. Kimlik doğrulanmamış
	// bir bağlantının süresiz açık kalmasını engeller.
	peerConnTimeout = 3 * time.Minute
	// maxPeerMessageBytes, tek bir eş bağlantısından okunacak en fazla veri.
	// Görev parametreleri (log metni) buraya sığar; sınırsız JSON beslemesiyle
	// bellek tüketimini engeller.
	maxPeerMessageBytes = 8 << 20 // 8 MiB
)

// beacon is the UDP multicast payload. Yalnızca genel bilgi taşır; hiçbir
// gizli değer (cluster anahtarı dahil) yayınlanmaz.
type beacon struct {
	NodeName   string `json:"nodeName"`
	Version    string `json:"version"`
	ListenAddr string `json:"listenAddr"`
	Tier       string `json:"tier"`
	Cores      int    `json:"cores"`
	RAMMB      int    `json:"ramMB"`
	Role       string `json:"role"` // effective role
}

// peerRequest is the wire format for peer-to-peer requests.
type peerRequest struct {
	Method string      `json:"method"`
	Token  string      `json:"token,omitempty"` // cluster anahtarı (assignTask için zorunlu)
	Task   *model.Task `json:"task,omitempty"`
}

// Executor performs the real work behind a task. The daemon implements it so
// the cluster package stays focused on discovery and transport. Execute runs to
// completion and returns a short human-readable result (or an error). It may run
// on the node that created the task (local) or on a paired helper that received
// the task over the wire — so everything Execute needs must travel in the task's
// Params (e.g. log text for analysis); tasks that require local data (backups)
// are only ever run on the node that owns that data.
type Executor interface {
	Execute(t model.Task) (result string, err error)
}

// Manager runs discovery, pairing, and the local task queue.
type Manager struct {
	cfg        model.ClusterConfig
	nodeName   string
	version    string
	listenPort int
	log        *log.Logger
	store      *store.Store
	exec       Executor

	mu      sync.RWMutex
	secret  string // önceden paylaşılmış cluster anahtarı
	peers   map[string]*model.Peer
	tasks   []model.Task
	running bool
	conn    *net.UDPConn
	ln      net.Listener
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewManager creates a cluster manager. exec may be nil (tasks then no-op).
func NewManager(cfg model.ClusterConfig, version string, st *store.Store, lg *log.Logger, exec Executor) *Manager {
	return &Manager{
		cfg:        cfg,
		nodeName:   cfg.NodeName,
		version:    version,
		listenPort: cfg.Port,
		log:        lg,
		store:      st,
		exec:       exec,
		secret:     cfg.Secret,
		peers:      map[string]*model.Peer{},
	}
}

// Secret returns the local cluster key so the panel can show it for pairing.
func (m *Manager) Secret() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.secret
}

// SetSecret installs the pre-shared cluster key.
func (m *Manager) SetSecret(s string) {
	m.mu.Lock()
	m.secret = s
	m.mu.Unlock()
}

// Running reports whether the cluster manager is active.
func (m *Manager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// Start launches the UDP beacon listener, the TCP peer server, and the
// background janitor. It is safe to call multiple times (idempotent).
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(context.Background())

	// UDP multicast discovery. This needs a multicast-capable interface to be up;
	// on a fresh/offline boot there may be none yet. That must NOT block the
	// daemon, so failure here is non-fatal: we skip LAN discovery (beacons) but
	// still run the TCP peer server and task loops. Discovery comes up on a later
	// refresh/boot once a network interface exists.
	if addr, err := net.ResolveUDPAddr("udp4", multicastAddr); err != nil {
		if m.log != nil {
			m.log.Infof("cluster: discovery off (bad multicast addr): %v", err)
		}
	} else if conn, err := net.ListenMulticastUDP("udp4", nil, addr); err != nil {
		if m.log != nil {
			// Don't echo the raw "setsockopt: no such device" error — it scares
			// users and only means there's no multicast NIC yet.
			m.log.Infof("cluster: LAN keşfi kapalı (uygun ağ arabirimi yok); eşleştirme elle yapılabilir")
		}
	} else {
		m.conn = conn
	}

	// TCP peer server. Binding the wildcard address succeeds even with only
	// loopback up, so manual pairing/offload works regardless of discovery.
	// Still non-fatal: run without it rather than failing daemon startup.
	if m.listenPort == 0 {
		m.listenPort = 27890
	}
	if ln, err := net.Listen("tcp", fmt.Sprintf(":%d", m.listenPort)); err != nil {
		if m.log != nil {
			m.log.Warnf("cluster: peer server disabled (tcp listen failed): %v", err)
		}
	} else {
		m.ln = ln
		go m.tcpAcceptLoop()
	}

	// Beacon loops require the multicast socket; only run them when we have it.
	if m.conn != nil {
		go m.beaconLoop()
		go m.readBeaconLoop()
	}
	go m.janitorLoop()
	go m.taskConsumerLoop()

	if m.log != nil {
		if m.conn != nil {
			m.log.Infof("cluster: started discovery on %s, peer port %d", multicastAddr, m.listenPort)
		} else {
			m.log.Infof("cluster: started without LAN discovery, peer port %d", m.listenPort)
		}
	}
	return nil
}

// Stop shuts down the cluster manager.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	// Kapatılan tanıtıcıları nil'e çek: aksi halde yeniden Start() çağrıldığında
	// (config.set ile cluster tekrar açıldığında) multicast bağlanması başarısız
	// olursa beaconLoop kapatılmış eski soketi kullanmaya çalışır.
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	if m.ln != nil {
		m.ln.Close()
		m.ln = nil
	}
}

// Peers returns a snapshot of known peers (paired or discovered).
func (m *Manager) Peers() []model.Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Peer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, *p)
	}
	return out
}

// Pair marks a peer as trusted/paired.
func (m *Manager) Pair(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.peers[id]; ok {
		p.Paired = true
	}
}

// Unpair removes a peer.
func (m *Manager) Unpair(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, id)
}

// Tasks returns the local task queue snapshot.
func (m *Manager) Tasks() []model.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Task, len(m.tasks))
	copy(out, m.tasks)
	return out
}

// SubmitTask adds a task to the queue.
func (m *Manager) SubmitTask(t model.Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, t)
}

func (m *Manager) beaconLoop() {
	ticker := time.NewTicker(beaconPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.sendBeacon()
		}
	}
}

func (m *Manager) effectiveRole() string {
	if m.cfg.Role != "" && m.cfg.Role != "auto" {
		return m.cfg.Role
	}
	cpu := sysmon.CPU()
	mem := sysmon.Memory()
	t := tier.Classify(mem, cpu)
	if t == model.TierHigh || t == model.TierMedium {
		return "game-host"
	}
	return "helper"
}

func (m *Manager) sendBeacon() {
	cpu := sysmon.CPU()
	mem := sysmon.Memory()
	ramMiB := int(mem.TotalBytes / (1 << 20))
	t := tier.Classify(mem, cpu)

	b := beacon{
		NodeName:   m.nodeName,
		Version:    m.version,
		ListenAddr: fmt.Sprintf(":%d", m.listenPort),
		Tier:       string(t),
		Cores:      cpu.Cores,
		RAMMB:      ramMiB,
		Role:       m.effectiveRole(),
	}
	data, _ := json.Marshal(b)

	addr, _ := net.ResolveUDPAddr("udp4", multicastAddr)
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	conn.Write(data)
}

func (m *Manager) readBeaconLoop() {
	buf := make([]byte, 1024)
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		m.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, src, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var b beacon
		if err := json.Unmarshal(buf[:n], &b); err != nil {
			continue
		}
		if b.NodeName == m.nodeName {
			continue // ignore self
		}
		m.upsertPeer(b, src.IP.String())
	}
}

func (m *Manager) upsertPeer(b beacon, ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := b.NodeName + "@" + ip
	if p, ok := m.peers[id]; ok {
		p.LastSeen = time.Now()
		p.State = model.PeerAvailable
		p.Cores = b.Cores
		p.RAMMB = b.RAMMB
		p.ActiveJobs = 0 // will be refreshed via status RPC
		return
	}
	m.peers[id] = &model.Peer{
		ID:    id,
		Name:  b.NodeName,
		IP:    ip,
		Port:  m.listenPort,
		Cores: b.Cores,
		RAMMB: b.RAMMB,
		State: model.PeerAvailable,
		// GÜVENLİK: otomatik eşleştirme KALDIRILDI. Eskiden burada
		// "Paired: true" vardı; keşfedilen her düğüm anında güvenilir kabul
		// ediliyordu, bu da cluster.pair RPC'sini ve README'deki eşleştirme
		// güvenlik hikâyesini anlamsız kılıyordu. Eşleştirme artık yalnızca
		// kullanıcının açık eylemiyle (cluster.pair) olur.
		Paired:   false,
		LastSeen: time.Now(),
	}
}

func (m *Manager) janitorLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			for id, p := range m.peers {
				if time.Since(p.LastSeen) > peerTimeout {
					p.State = model.PeerOffline
					if !p.Paired {
						delete(m.peers, id)
					}
				}
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) tcpAcceptLoop() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			select {
			case <-m.ctx.Done():
				return
			default:
				continue
			}
		}
		go m.handlePeerConn(conn)
	}
}

// authorizeTask decides whether an incoming assignTask may run.
//
// GÜVENLİK: eskiden hiçbir kontrol yoktu — LAN'daki herhangi biri eşleştirme
// portuna bağlanıp "assignTask" gönderebiliyor, daemon işi doğrudan
// Executor.Execute ile çalıştırıyordu. Tekrarlanan backup görevleriyle diski
// doldurmak veya CPU'yu yüklemek mümkündü. Artık iki koşul birlikte gerekli:
//
//  1. İstek, yerel cluster anahtarıyla eşleşen bir token taşımalı
//     (sabit süreli karşılaştırma).
//  2. Kaynak IP, KULLANICI TARAFINDAN eşleştirilmiş bir eşe ait olmalı.
//     Otomatik eşleştirme kaldırıldı (bkz. upsertPeer).
//
// ping/status salt-okunur olduğu ve keşif arayüzü için gerekli olduğundan
// kimlik doğrulaması istemez.
func (m *Manager) authorizeTask(remoteIP, token string) error {
	m.mu.RLock()
	secret := m.secret
	m.mu.RUnlock()

	if secret == "" {
		return fmt.Errorf("cluster anahtarı yapılandırılmamış")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return fmt.Errorf("geçersiz cluster anahtarı")
	}
	if !m.isPairedIP(remoteIP) {
		return fmt.Errorf("eş eşleştirilmemiş: %s", remoteIP)
	}
	return nil
}

// isPairedIP reports whether any user-paired peer is reachable at ip.
func (m *Manager) isPairedIP(ip string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.peers {
		if p.IP == ip && p.Paired {
			return true
		}
	}
	return false
}

// handlePeerConn handles an incoming TCP peer connection.
func (m *Manager) handlePeerConn(conn net.Conn) {
	defer conn.Close()

	remoteIP := ""
	if host, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
		remoteIP = host
	}

	// Kaynak sınırları: eskiden ne okuma zaman aşımı ne de boyut sınırı vardı,
	// yani bir eş bağlantıyı süresiz tutabilir veya sınırsız JSON besleyerek
	// belleği tüketebilirdi.
	_ = conn.SetDeadline(time.Now().Add(peerConnTimeout))
	dec := json.NewDecoder(io.LimitReader(conn, maxPeerMessageBytes))
	enc := json.NewEncoder(conn)

	for {
		var msg peerRequest
		if err := dec.Decode(&msg); err != nil {
			return
		}
		switch msg.Method {
		case "status":
			_ = enc.Encode(m.localStatus())
		case "ping":
			_ = enc.Encode(map[string]any{"pong": true})
		case "assignTask":
			if err := m.authorizeTask(remoteIP, msg.Token); err != nil {
				if m.log != nil {
					m.log.Warnf("cluster: %s adresinden yetkisiz görev reddedildi: %v", remoteIP, err)
				}
				_ = enc.Encode(map[string]any{"accepted": false, "error": "yetkisiz"})
				return // yetkisiz eşle konuşmayı sürdürme
			}
			// Eş (game-host) bize CPU'muzla çalıştırılacak iş veriyor. İşi yerinde
			// yürütüp sonucu döndürüyoruz; LAN güç paylaşımının çekirdeği budur.
			if msg.Task == nil {
				_ = enc.Encode(map[string]any{"accepted": false, "error": "görev yok"})
				continue
			}
			t := *msg.Task
			result, execErr := "", error(nil)
			if m.exec != nil {
				result, execErr = m.exec.Execute(t)
			}
			resp := map[string]any{"accepted": true, "result": result}
			if execErr != nil {
				resp["state"] = string(model.TaskFailed)
				resp["error"] = execErr.Error()
			} else {
				resp["state"] = string(model.TaskDone)
			}
			if m.log != nil {
				m.log.Infof("cluster: %s eşinden gelen görev %s (%s) çalıştırıldı", remoteIP, t.ID, t.Kind)
			}
			_ = enc.Encode(resp)
		default:
			_ = enc.Encode(map[string]any{"error": "bilinmeyen metot"})
		}
	}
}

func (m *Manager) localStatus() map[string]any {
	cpu := sysmon.CPU()
	mem := sysmon.Memory()
	return map[string]any{
		"nodeName": m.nodeName,
		"version":  m.version,
		"tier":     string(tier.Classify(mem, cpu)),
		"cores":    cpu.Cores,
		"ramMB":    int(mem.TotalBytes / (1 << 20)),
		"role":     m.effectiveRole(),
	}
}

// taskConsumerLoop processes the local task queue based on role.
func (m *Manager) taskConsumerLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.consumeTasks()
		}
	}
}

func (m *Manager) consumeTasks() {
	m.mu.Lock()
	role := m.effectiveRole()
	var pending []model.Task
	for _, t := range m.tasks {
		if t.State == model.TaskQueued {
			pending = append(pending, t)
		}
	}
	m.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	helper := m.pickHelper()
	for _, t := range pending {
		switch t.Kind {
		case model.TaskLogAnalysis, model.TaskFileOp:
			// Data-light optimization work: hand to a paired helper when we are
			// the game-host so our CPU stays free for the server. The helper
			// runs it and returns the result.
			if role == "game-host" && helper != nil {
				m.offloadToPeer(t, helper)
			} else {
				m.runTaskLocal(t)
			}
		default:
			// Backups and game-host tasks need local data; always run here.
			m.runTaskLocal(t)
		}
	}
}

// pickHelper returns an available paired peer, or nil if we are alone.
func (m *Manager) pickHelper() *model.Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.peers {
		if p.Paired && p.State == model.PeerAvailable {
			cp := *p
			return &cp
		}
	}
	return nil
}

// HasHelper reports whether a paired helper is currently reachable.
func (m *Manager) HasHelper() bool { return m.pickHelper() != nil }

func (m *Manager) setTaskOutcome(id string, state model.TaskState, assignedTo, result, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks[i].State = state
			m.tasks[i].AssignedTo = assignedTo
			m.tasks[i].Result = result
			m.tasks[i].Error = errMsg
			m.tasks[i].UpdatedAt = time.Now()
			return
		}
	}
}

func (m *Manager) runTaskLocal(t model.Task) {
	m.setTaskOutcome(t.ID, model.TaskRunning, m.nodeName, "", "")
	if m.log != nil {
		m.log.Infof("cluster: running task %s (%s) locally", t.ID, t.Kind)
	}
	var (
		result string
		err    error
	)
	if m.exec != nil {
		result, err = m.exec.Execute(t)
	}
	if err != nil {
		m.setTaskOutcome(t.ID, model.TaskFailed, m.nodeName, "", err.Error())
		if m.log != nil {
			m.log.Warnf("cluster: task %s failed: %v", t.ID, err)
		}
		return
	}
	m.setTaskOutcome(t.ID, model.TaskDone, m.nodeName, result, "")
}

// offloadToPeer hands a task to a peer, waits for the result, and records it.
// Any transport failure falls back to running the task locally.
func (m *Manager) offloadToPeer(t model.Task, p *model.Peer) {
	m.setTaskOutcome(t.ID, model.TaskMigrating, p.ID, "", "")
	addr := net.JoinHostPort(p.IP, strconv.Itoa(p.Port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		m.runTaskLocal(t)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	// Yerel cluster anahtarını gönder; eş bunu kendi anahtarıyla karşılaştırır.
	// İki cihazın birlikte çalışması için anahtarların aynı olması gerekir.
	if err := enc.Encode(peerRequest{Method: "assignTask", Token: m.Secret(), Task: &t}); err != nil {
		m.runTaskLocal(t)
		return
	}
	var res struct {
		Accepted bool   `json:"accepted"`
		State    string `json:"state"`
		Result   string `json:"result"`
		Error    string `json:"error"`
	}
	if err := dec.Decode(&res); err != nil || !res.Accepted {
		m.runTaskLocal(t)
		return
	}
	state := model.TaskState(res.State)
	if state == "" {
		state = model.TaskDone
	}
	m.setTaskOutcome(t.ID, state, p.ID, res.Result, res.Error)
	if m.log != nil {
		m.log.Infof("cluster: task %s (%s) offloaded to peer %s -> %s", t.ID, t.Kind, p.Name, state)
	}
}
