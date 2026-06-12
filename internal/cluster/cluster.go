// Package cluster implements LAN peer discovery (UDP multicast beacon), pairing,
// and a simple task-distribution protocol over TCP. It is designed to work
// gracefully even when there are no other peers on the network.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
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
)

// beacon is the UDP multicast payload.
type beacon struct {
	NodeName   string `json:"nodeName"`
	Version    string `json:"version"`
	ListenAddr string `json:"listenAddr"`
	Tier       string `json:"tier"`
	Cores      int    `json:"cores"`
	RAMMB      int    `json:"ramMB"`
	Role       string `json:"role"` // effective role
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
		peers:      map[string]*model.Peer{},
	}
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
	if m.conn != nil {
		m.conn.Close()
	}
	if m.ln != nil {
		m.ln.Close()
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
		ID:       id,
		Name:     b.NodeName,
		IP:       ip,
		Port:     m.listenPort,
		Cores:    b.Cores,
		RAMMB:    b.RAMMB,
		State:    model.PeerAvailable,
		Paired:   true, // Auto-pair on LAN to enable seamless resource sharing
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

// handlePeerConn handles an incoming TCP peer connection.
func (m *Manager) handlePeerConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return
		}
		method, _ := msg["method"].(string)
		switch method {
		case "status":
			enc.Encode(m.localStatus())
		case "ping":
			enc.Encode(map[string]any{"pong": true})
		case "assignTask":
			// A peer (the game-host) is handing us work to run with our CPU.
			// We execute it inline and return the result so the host can track
			// it. This is the core of LAN power-sharing: the helper does the
			// heavy side-work (log analysis / optimization) for the host.
			taskData, _ := json.Marshal(msg["task"])
			var t model.Task
			if err := json.Unmarshal(taskData, &t); err != nil {
				enc.Encode(map[string]any{"accepted": false, "error": err.Error()})
				continue
			}
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
				m.log.Infof("cluster: ran offloaded task %s (%s) from peer", t.ID, t.Kind)
			}
			enc.Encode(resp)
		default:
			enc.Encode(map[string]any{"error": "unknown method"})
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
	if err := enc.Encode(map[string]any{"method": "assignTask", "task": t}); err != nil {
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
