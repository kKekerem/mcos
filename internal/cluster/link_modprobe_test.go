package cluster

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"mcos/internal/model"
)

// İki VM'li sınamada eşte mod kurulamamış, sunucu modsuz açılmıştı; kurucu
// yine "2 düğüm çevrimiçi" diyordu. Mod portu yanıt vermeyen düğüm topolojide
// ModReady=false ile ve notta ADIYLA görünmeli; yanıt veren true görünmeli.
func TestTopologyReportsModPortHealth(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", testKey, true)
	id := discover(t, kutu, pc)
	kutu.m.Pair(id)
	waitFor(t, "eşleşme", func() bool {
		_, ok := peerByID(pc.m, "id:"+kutu.m.NodeID())
		return ok
	})
	kutu.host.origin = true
	kutu.host.spec = model.LinkSpec{Mode: model.LinkSharedWorld, ServerName: "Ortak",
		Software: "paper", MCVersion: "1.21.11", Seed: "1", Port: 25565}.Normalize()
	kutu.host.have = true

	var mu sync.Mutex
	var dialed []string
	old := dialMod
	dialMod = func(name, addr string) error {
		mu.Lock()
		dialed = append(dialed, name+"@"+addr)
		mu.Unlock()
		if name == "mcos-kutu" {
			return nil // kendi modumuz yanıt veriyor
		}
		return errors.New("bağlantı reddedildi")
	}
	t.Cleanup(func() { dialMod = old })

	// Yoklama yapılmadan: ModReady bilinmiyor (nil), not temiz.
	if top := kutu.c.Topology(); strings.Contains(top.Note, "yanıt vermiyor") {
		t.Fatalf("yoklamadan önce uyarı çıktı: %q", top.Note)
	}
	kutu.c.probeMods()
	top := kutu.c.Topology()
	var self, peer *model.LinkNode
	for i := range top.Nodes {
		if top.Nodes[i].Self {
			self = &top.Nodes[i]
		} else {
			peer = &top.Nodes[i]
		}
	}
	if self == nil || peer == nil {
		t.Fatalf("düğümler eksik: %+v", top.Nodes)
	}
	if self.ModReady == nil || !*self.ModReady {
		t.Errorf("kendi modumuz hazır görünmedi: %+v", self.ModReady)
	}
	if peer.ModReady == nil || *peer.ModReady {
		t.Errorf("yanıt vermeyen eş hazır göründü: %+v", peer.ModReady)
	}
	if !strings.Contains(top.Note, peer.Name) || !strings.Contains(top.Note, "yanıt vermiyor") {
		t.Errorf("not yanıt vermeyen düğümü adlandırmıyor: %q", top.Note)
	}
	if !top.Enabled {
		t.Error("uyarı ortak dünyayı kapattı; yalnızca bildirmeli")
	}
	mu.Lock()
	n := len(dialed)
	mu.Unlock()
	if n != 2 {
		t.Errorf("%d yoklama, 2 bekleniyordu: %v", n, dialed)
	}
}
