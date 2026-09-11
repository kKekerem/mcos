package cluster

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mcos/internal/model"
)

// recordingExecutor records every task it is asked to run so a test can prove
// that an unauthorised request never reached the executor.
type recordingExecutor struct{ ran []model.Task }

func (e *recordingExecutor) Execute(t model.Task) (string, error) {
	e.ran = append(e.ran, t)
	return "tamam", nil
}

func newTestManager(secret string) (*Manager, *recordingExecutor) {
	exec := &recordingExecutor{}
	m := NewManager(
		model.ClusterConfig{Enabled: true, NodeName: "test", Port: 0, Secret: secret},
		"test", nil, nil, exec,
	)
	return m, exec
}

// addPeer registers a peer as the discovery path would, with explicit pairing
// state.
func (m *Manager) addPeerForTest(ip string, paired bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers["peer@"+ip] = &model.Peer{
		ID: "peer@" + ip, Name: "peer", IP: ip, Paired: paired,
		State: model.PeerAvailable, LastSeen: time.Now(),
	}
}

func TestAuthorizeTask(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name     string
		secret   string
		peerIP   string
		paired   bool
		reqIP    string
		reqToken string
		wantErr  bool
	}{
		{"dogru anahtar + eslesmis eş", secret, "10.0.0.5", true, "10.0.0.5", secret, false},
		{"dogru anahtar ama eslesmemis eş", secret, "10.0.0.5", false, "10.0.0.5", secret, true},
		{"eslesmis eş ama yanlis anahtar", secret, "10.0.0.5", true, "10.0.0.5", "yanlis", true},
		{"eslesmis eş ama bos anahtar", secret, "10.0.0.5", true, "10.0.0.5", "", true},
		{"bilinmeyen IP, dogru anahtar", secret, "10.0.0.5", true, "10.0.0.99", secret, true},
		{"yerel anahtar yapilandirilmamis", "", "10.0.0.5", true, "10.0.0.5", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestManager(tc.secret)
			m.addPeerForTest(tc.peerIP, tc.paired)

			err := m.authorizeTask(tc.reqIP, tc.reqToken)
			if tc.wantErr && err == nil {
				t.Errorf("authorizeTask = nil, reddedilmeliydi")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("authorizeTask = %v, kabul edilmeliydi", err)
			}
		})
	}
}

// TestUpsertPeerDoesNotAutoPair, D1'in regresyon testidir: keşfedilen düğümler
// eskiden "Paired: true" ile ekleniyordu, yani LAN'daki herkes anında güvenilir
// kabul ediliyordu.
func TestUpsertPeerDoesNotAutoPair(t *testing.T) {
	m, _ := newTestManager("secret")
	m.upsertPeer(beacon{NodeName: "komsu", Cores: 4, RAMMB: 8192}, "192.168.1.50")

	peers := m.Peers()
	if len(peers) != 1 {
		t.Fatalf("eş sayısı = %d, 1 beklenmişti", len(peers))
	}
	if peers[0].Paired {
		t.Error("keşfedilen eş otomatik eşleştirildi — açık kullanıcı eylemi gerekmeli")
	}
	if m.HasHelper() {
		t.Error("eşleştirilmemiş eş yardımcı olarak seçilebiliyor")
	}

	// Açık eşleştirmeden sonra kullanılabilir olmalı.
	m.Pair(peers[0].ID)
	if !m.HasHelper() {
		t.Error("açıkça eşleştirilen eş yardımcı olarak seçilemedi")
	}
}

// dialHandler serves handlePeerConn over a REAL loopback TCP socket.
//
// net.Pipe() kullanılamaz: onun RemoteAddr()'ı "pipe" döndürür, IP çıkarımı
// başarısız olur ve yetkilendirme (doğru şekilde) kapalı tarafa düşer. Kaynak
// IP kontrolünü sınamak için gerçek bir soket şart.
func dialHandler(t *testing.T, m *Manager) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		m.handlePeerConn(conn)
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	return client
}

func roundTrip(t *testing.T, conn net.Conn, req peerRequest, out any) {
	t.Helper()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("istek gönderilemedi: %v", err)
	}
	if err := json.NewDecoder(conn).Decode(out); err != nil {
		t.Fatalf("yanıt okunamadı: %v", err)
	}
}

type taskResponse struct {
	Accepted bool   `json:"accepted"`
	Result   string `json:"result"`
	Error    string `json:"error"`
}

// TestHandlePeerConnRejectsUnpairedPeer: keşfedilmiş ama eşleştirilmemiş bir eş
// doğru anahtarla bile görev çalıştıramaz.
func TestHandlePeerConnRejectsUnpairedPeer(t *testing.T) {
	m, exec := newTestManager("dogru-anahtar")
	m.addPeerForTest("127.0.0.1", false) // eşleştirilmemiş

	conn := dialHandler(t, m)
	var resp taskResponse
	roundTrip(t, conn, peerRequest{
		Method: "assignTask",
		Token:  "dogru-anahtar",
		Task:   &model.Task{ID: "t1", Kind: model.TaskBackup, ServerID: "srv_x"},
	}, &resp)

	if resp.Accepted {
		t.Error("eşleştirilmemiş eşin görevi kabul edildi")
	}
	if len(exec.ran) != 0 {
		t.Errorf("yetkisiz görev Executor'a ulaştı: %+v", exec.ran)
	}
}

// TestHandlePeerConnRejectsBadToken: eşleştirilmiş eş bile yanlış anahtarla
// görev çalıştıramaz.
func TestHandlePeerConnRejectsBadToken(t *testing.T) {
	m, exec := newTestManager("dogru-anahtar")
	m.addPeerForTest("127.0.0.1", true)

	conn := dialHandler(t, m)
	var resp taskResponse
	roundTrip(t, conn, peerRequest{
		Method: "assignTask",
		Token:  "yanlis-anahtar",
		Task:   &model.Task{ID: "t1", Kind: model.TaskBackup, ServerID: "srv_x"},
	}, &resp)

	if resp.Accepted {
		t.Error("yanlış anahtarlı görev kabul edildi")
	}
	if len(exec.ran) != 0 {
		t.Errorf("yetkisiz görev Executor'a ulaştı: %+v", exec.ran)
	}
}

// TestHandlePeerConnRejectsNoToken: eski istemci davranışı (token yok) —
// D1 açığının tam senaryosu.
func TestHandlePeerConnRejectsNoToken(t *testing.T) {
	m, exec := newTestManager("dogru-anahtar")
	m.addPeerForTest("127.0.0.1", true)

	conn := dialHandler(t, m)
	var resp taskResponse
	roundTrip(t, conn, peerRequest{
		Method: "assignTask",
		Task:   &model.Task{ID: "t1", Kind: model.TaskBackup, ServerID: "srv_x"},
	}, &resp)

	if resp.Accepted {
		t.Error("anahtarsız görev kabul edildi (D1 açığı geri döndü)")
	}
	if len(exec.ran) != 0 {
		t.Errorf("anahtarsız görev Executor'a ulaştı: %+v", exec.ran)
	}
}

// TestHandlePeerConnAcceptsAuthorizedTask, doğru yolun bozulmadığını gösterir.
func TestHandlePeerConnAcceptsAuthorizedTask(t *testing.T) {
	m, exec := newTestManager("dogru-anahtar")
	m.addPeerForTest("127.0.0.1", true)

	conn := dialHandler(t, m)
	var resp taskResponse
	roundTrip(t, conn, peerRequest{
		Method: "assignTask",
		Token:  "dogru-anahtar",
		Task:   &model.Task{ID: "t1", Kind: model.TaskLogAnalysis, ServerID: "srv_x"},
	}, &resp)

	if !resp.Accepted {
		t.Errorf("yetkili görev reddedildi: %s", resp.Error)
	}
	if len(exec.ran) != 1 || exec.ran[0].ID != "t1" {
		t.Errorf("Executor görevi almadı: %+v", exec.ran)
	}
}

// TestPingNeedsNoAuth: keşif arayüzü kimlik doğrulaması olmadan çalışmaya
// devam etmeli (salt-okunur ve eş listesi için gerekli).
func TestPingNeedsNoAuth(t *testing.T) {
	m, _ := newTestManager("anahtar")

	conn := dialHandler(t, m)
	var resp struct {
		Pong bool `json:"pong"`
	}
	roundTrip(t, conn, peerRequest{Method: "ping"}, &resp)
	if !resp.Pong {
		t.Error("ping yanıtı alınamadı")
	}
}
