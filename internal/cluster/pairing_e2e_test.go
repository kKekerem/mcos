package cluster

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/model"
	"mcos/internal/store"
)

// Bu dosya İKİ GERÇEK Manager'ı loopback TCP üzerinden konuşturur: biri MCOS
// kutusu (eşleştirme kapalı, kullanıcı panelden eşleştirir), öbürü masaüstü
// düğüm (anahtarı bilen çağıranı kabul eder). Tel biçimi, kimlik doğrulama,
// kayıt ve dosya eşitlemesi taklit edilmeden sınanır.

// fakeLinkHost is a LinkHost that remembers what it was told.
type fakeLinkHost struct {
	mu      sync.Mutex
	spec    model.LinkSpec
	have    bool
	dir     string
	origin  bool
	applied []model.LinkSpec
}

func (h *fakeLinkHost) LinkSpec() (model.LinkSpec, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.spec
	if h.origin && h.dir != "" {
		s.Files = ScanLinkFiles(h.dir)
	}
	return s, h.have
}

func (h *fakeLinkHost) ApplyLinkSpec(spec model.LinkSpec) (string, int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.applied = append(h.applied, spec)
	h.spec, h.have = spec, spec.Mode == model.LinkSharedWorld
	return "tamam", spec.Port, nil
}

func (h *fakeLinkHost) PlayersOnline() int { return 0 }

func (h *fakeLinkHost) LinkDataDir() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dir, h.have && h.dir != ""
}

func (h *fakeLinkHost) lastApplied() (model.LinkSpec, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.applied) == 0 {
		return model.LinkSpec{}, 0
	}
	return h.applied[len(h.applied)-1], len(h.applied)
}

// testNode is one machine: manager + coordinator + listening port.
type testNode struct {
	m    *Manager
	c    *LinkCoordinator
	host *fakeLinkHost
	port int
}

func newTestNode(t *testing.T, name, secret string, open bool) *testNode {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(model.ClusterConfig{Enabled: true, NodeName: name, Secret: secret},
		"test", st, nil, nil)
	m.SetOpenPairing(open)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.ln = ln
	m.listenPort = ln.Addr().(*net.TCPAddr).Port
	go m.tcpAcceptLoop()
	t.Cleanup(func() { m.cancel(); ln.Close() })

	h := &fakeLinkHost{}
	c := NewLinkCoordinator(m, h)
	m.AttachLink(c)
	return &testNode{m: m, c: c, host: h, port: m.listenPort}
}

// discover makes `from` list `to` the way a LAN scan would (unpaired).
func discover(t *testing.T, from, to *testNode) string {
	t.Helper()
	st, err := probeNode(context.Background(), "127.0.0.1", to.port)
	if err != nil {
		t.Fatalf("yoklama: %v", err)
	}
	return from.m.upsertProbedPort(st, "127.0.0.1", to.port).ID
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", what)
}

func peerByID(m *Manager, id string) (model.Peer, bool) {
	for _, p := range m.Peers() {
		if p.ID == id {
			return p, true
		}
	}
	return model.Peer{}, false
}

const testKey = "0123456789abcdef0123456789abcdef"

// Panelden "Eşleştir": anahtar KARŞIDA sınanır, düğüm MCOS'u GERÇEK
// kimliği, adı ve portuyla kaydeder (eskiden "host" / 2222 tahmini).
func TestPairRegistersCallerIdentityOnNode(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", testKey, true)

	id := discover(t, kutu, pc)
	kutu.m.Pair(id)

	want := "id:" + kutu.m.NodeID()
	waitFor(t, "düğüm MCOS'u kaydetsin", func() bool {
		p, ok := peerByID(pc.m, want)
		return ok && p.Paired
	})
	p, _ := peerByID(pc.m, want)
	if p.Name != "mcos-kutu" {
		t.Errorf("düğüm MCOS'u %q adıyla kaydetti, mcos-kutu bekleniyordu", p.Name)
	}
	if p.Port != kutu.port {
		t.Errorf("düğüm MCOS portunu %d kaydetti, gerçek port %d", p.Port, kutu.port)
	}
	waitFor(t, "MCOS tarafında sorun kalmasın", func() bool {
		q, _ := peerByID(kutu.m, id)
		return q.Paired && q.Problem == ""
	})
}

// KARŞI-SINAMA: yanlış anahtar eşleşme ANINDA ve Türkçe nedenle söylenir;
// düğüm hiçbir şeyi eşleştirmiş saymaz.
func TestPairWithWrongKeyExplainsWhy(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", "ffffffffffffffffffffffffffffffff", true)

	id := discover(t, kutu, pc)
	kutu.m.Pair(id)
	waitFor(t, "sorun görünsün", func() bool {
		q, _ := peerByID(kutu.m, id)
		return q.Problem != ""
	})
	q, _ := peerByID(kutu.m, id)
	if !strings.Contains(q.Problem, "anahtar yanlış") {
		t.Errorf("sorun metni = %q, 'anahtar yanlış' bekleniyordu", q.Problem)
	}
	for _, p := range pc.m.Peers() {
		if p.Paired {
			t.Errorf("yanlış anahtarla düğüm %s'yi eşleştirdi", p.Name)
		}
	}
}

// KARŞI-SINAMA: MCOS kutusu (açık eşleştirme KAPALI) doğru anahtarı bilen
// ama panelden eşleştirilmemiş birini kabul ETMEZ ve nedeni "unpaired" olur.
func TestClosedPairingRejectsUnpairedCaller(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", testKey, true)

	rep, err := pc.m.sayHello(context.Background(), "127.0.0.1", kutu.port)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK || rep.Reason != reasonUnpaired {
		t.Fatalf("yanıt = %+v, unpaired reddi bekleniyordu", rep)
	}
	msg, pairable := pc.m.helloProblem(rep, nil, kutu.port)
	if !pairable || !strings.Contains(msg, "henüz eşleştirmedi") {
		t.Errorf("açıklama = %q (yerel eşleşme %v)", msg, pairable)
	}
}

// ASIL HATA 2'nin sınaması: iki taraf dilimleri AYNI hesaplamalı ve her
// makine FARKLI bir dilimin sahibi olmalı. Eskiden düğüm MCOS'u "host"
// adıyla kaydettiği için iki taraf farklı sıralar görüyordu ve ikisi de
// x ≥ 0 yarısını sahipleniyordu.
func TestBothSidesAgreeOnTerritories(t *testing.T) {
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
		Software: "paper", MCVersion: "1.21.11", Seed: "424242", Port: 25565}.Normalize()
	kutu.host.have = true
	if errs := kutu.c.PushSpec(kutu.host.spec); len(errs) > 0 {
		t.Fatalf("kurulum gönderilemedi: %v", errs)
	}
	got, n := pc.host.lastApplied()
	if n == 0 || got.OriginID != kutu.m.NodeID() || got.Seed != "424242" {
		t.Fatalf("düğüme giden kurulum = %+v", got)
	}

	a, b := kutu.c.Topology(), pc.c.Topology()
	if !a.Enabled || !b.Enabled {
		t.Fatalf("ortak dünya açık değil: kutu=%q pc=%q", a.Note, b.Note)
	}
	if len(a.Areas) != 2 || len(b.Areas) != 2 {
		t.Fatalf("dilim sayısı kutu=%d pc=%d", len(a.Areas), len(b.Areas))
	}
	for i := range a.Areas {
		if a.Areas[i] != b.Areas[i] {
			t.Errorf("dilim %d farklı: kutu %+v, pc %+v", i, a.Areas[i], b.Areas[i])
		}
	}
	if a.Self == b.Self {
		t.Fatalf("iki makine de kendini %q sanıyor — dünyanın aynı yarısı", a.Self)
	}
	if a.Self != "mcos-kutu" || b.Self != "ikinci-pc" {
		t.Errorf("self: kutu=%q pc=%q", a.Self, b.Self)
	}
}

// PC adı çalışırken değişince eşler YENİ adı görmeli (uçtan uca sınamada
// bulunan hata: "mcos-kutu" yapılan ad eşlere "mcos-1" gidiyordu).
func TestSetNodeNameIsAnnounced(t *testing.T) {
	kutu := newTestNode(t, "mcos-1", testKey, false)
	if !kutu.m.SetNodeName("mcos-kutu") {
		t.Fatal("ad değişmedi")
	}
	st, err := probeNode(context.Background(), "127.0.0.1", kutu.port)
	if err != nil {
		t.Fatal(err)
	}
	if st.NodeName != "mcos-kutu" {
		t.Errorf("yoklama adı = %q", st.NodeName)
	}
	if got := kutu.m.selfHello().NodeName; got != "mcos-kutu" {
		t.Errorf("el sıkışma adı = %q", got)
	}
	// KARŞI-SINAMA: boş ya da aynı ad bir değişiklik sayılmaz.
	if kutu.m.SetNodeName("  ") || kutu.m.SetNodeName("mcos-kutu") {
		t.Error("boş/aynı ad değişiklik sayıldı")
	}
}

// Eşleşme kaldırılınca düğümdeki ortak dünya KAPATILMALI.
func TestUnpairRetractsSharedWorld(t *testing.T) {
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
		Software: "paper", MCVersion: "1.21.11", Seed: "1"}.Normalize()
	kutu.host.have = true
	kutu.c.PushSpec(kutu.host.spec)

	kutu.m.Unpair(id)
	waitFor(t, "düğüme kapat gitsin", func() bool {
		s, _ := pc.host.lastApplied()
		return s.Mode == model.LinkOff
	})
	if _, ok := peerByID(kutu.m, id); ok {
		t.Error("eş listeden silinmedi")
	}
}

func writeJar(t *testing.T, dir, sub, name, body string) {
	t.Helper()
	p := filepath.Join(dir, sub)
	os.MkdirAll(p, 0o755)
	if err := os.WriteFile(filepath.Join(p, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Mod/eklenti eşitlemesi: kurucudaki jar'lar düğüme gelir, kurucudan
// kalkan jar düğümden silinir, kullanıcının kendi jar'ına dokunulmaz.
func TestLinkFilesSyncFromOrigin(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", testKey, true)
	id := discover(t, kutu, pc)
	kutu.m.Pair(id)
	waitFor(t, "eşleşme", func() bool {
		_, ok := peerByID(pc.m, "id:"+kutu.m.NodeID())
		return ok
	})

	src, dst := t.TempDir(), t.TempDir()
	writeJar(t, src, "plugins", "WorldGuard.jar", strings.Repeat("W", 70000))
	writeJar(t, src, "plugins", "mcos-link-paper.jar", "her düğüm kendisi kurar")
	writeJar(t, src, "mods", "fabric-api-0.141.6+1.21.11.jar", "her düğüm kendisi kurar")
	writeJar(t, dst, "plugins", "Benim.jar", "kullanıcının kendi eklentisi")
	kutu.host.origin, kutu.host.dir = true, src
	kutu.host.spec = model.LinkSpec{Mode: model.LinkSharedWorld, ServerName: "Ortak",
		Software: "paper", MCVersion: "1.21.11", Seed: "1"}.Normalize()
	kutu.host.have = true

	spec, _ := kutu.host.LinkSpec()
	if len(spec.Files) != 1 || spec.Files[0].Name != "WorldGuard.jar" {
		t.Fatalf("liste = %+v; yalnızca WorldGuard.jar bekleniyordu", spec.Files)
	}
	// daemon da PushSpec'e LinkSpec()'in sonucunu verir (handleLinkEnable).
	kutu.c.PushSpec(spec)
	sent, _ := pc.host.lastApplied()
	if len(sent.Files) != 1 {
		t.Fatalf("kurulumla giden dosya listesi = %+v", sent.Files)
	}
	if !LinkFilesPending(dst, sent.Files) {
		t.Fatal("eksik dosya varken bekleyen iş yok sayıldı")
	}
	changed, err := pc.c.SyncLinkFiles(context.Background(), dst, sent.Files)
	if err != nil || !changed {
		t.Fatalf("eşitleme: değişti=%v hata=%v", changed, err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "plugins", "WorldGuard.jar"))
	if len(b) != 70000 {
		t.Fatalf("gelen dosya %d bayt", len(b))
	}
	if LinkFilesPending(dst, sent.Files) {
		t.Error("eşitlemeden sonra hâlâ bekleyen iş var — sunucu boşuna yeniden başlar")
	}

	// Kurucuda eklenti değişti: eskisi kalktı, yenisi geldi.
	os.Remove(filepath.Join(src, "plugins", "WorldGuard.jar"))
	writeJar(t, src, "plugins", "Essentials.jar", "E")
	spec2, _ := kutu.host.LinkSpec()
	if _, err := pc.c.SyncLinkFiles(context.Background(), dst, spec2.Files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "plugins", "WorldGuard.jar")); !os.IsNotExist(err) {
		t.Error("kurucuda kaldırılan eklenti düğümde kaldı")
	}
	if _, err := os.Stat(filepath.Join(dst, "plugins", "Essentials.jar")); err != nil {
		t.Error("yeni eklenti gelmedi")
	}
	if _, err := os.Stat(filepath.Join(dst, "plugins", "Benim.jar")); err != nil {
		t.Error("kullanıcının kendi eklentisi SİLİNDİ")
	}
}

// KARŞI-SINAMA: kurucu listede olmayan hiçbir dosyayı vermez; yol dışına
// çıkmaya çalışan ad ve yanlış anahtar reddedilir.
func TestLinkFileServesOnlyListedFiles(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", testKey, true)
	id := discover(t, kutu, pc)
	kutu.m.Pair(id)
	waitFor(t, "eşleşme", func() bool {
		_, ok := peerByID(pc.m, "id:"+kutu.m.NodeID())
		return ok
	})
	src := t.TempDir()
	writeJar(t, src, "plugins", "A.jar", "AAAA")
	os.WriteFile(filepath.Join(src, "server.properties"), []byte("rcon.password=gizli\n"), 0o644)
	kutu.host.origin, kutu.host.dir, kutu.host.have = true, src, true
	kutu.host.spec = model.LinkSpec{Mode: model.LinkSharedWorld, ServerName: "Ortak",
		Software: "paper", MCVersion: "1.21.11"}.Normalize()
	kutu.c.PushSpec(kutu.host.spec)
	good := ScanLinkFiles(src)[0]

	dst := t.TempDir()
	cases := []struct {
		name string
		f    model.LinkFile
	}{
		{"listede yok", model.LinkFile{Dir: "plugins", Name: "B.jar", Size: 4, SHA256: good.SHA256}},
		{"özet farklı", model.LinkFile{Dir: "plugins", Name: "A.jar", Size: 4, SHA256: strings.Repeat("0", 64)}},
		{"yol dışı", model.LinkFile{Dir: "plugins", Name: "../server.properties", Size: 4, SHA256: good.SHA256}},
		{"klasör dışı", model.LinkFile{Dir: ".", Name: "server.properties", Size: 4, SHA256: good.SHA256}},
	}
	for _, tc := range cases {
		if err := pc.c.fetchLinkFile(context.Background(), dst, tc.f); err == nil {
			t.Errorf("%s: dosya VERİLDİ", tc.name)
		}
	}
	// Doğru istek geçer (karşı-sınamanın karşısı).
	if err := pc.c.fetchLinkFile(context.Background(), dst, good); err != nil {
		t.Fatalf("listedeki dosya verilmedi: %v", err)
	}

	// Yanlış anahtar: reddedilir ve nedeni söylenir.
	pc.m.SetSecret("ffffffffffffffffffffffffffffffff")
	err := pc.c.fetchLinkFile(context.Background(), t.TempDir(), good)
	if err == nil || !strings.Contains(err.Error(), "anahtar yanlış") {
		t.Errorf("yanlış anahtarla hata = %v", err)
	}
}

// describeNetErr: üç ayrı durum, üç ayrı çözüm.
func TestDescribeNetErr(t *testing.T) {
	// Gerçek bir "reddedildi": açılıp kapatılmış bir port.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	_, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		t.Skip("kapalı port bağlantı kabul etti")
	}
	if got := describeNetErr(err, 2222); !strings.Contains(got, "reddedildi") {
		t.Errorf("reddedilen bağlantı = %q", got)
	}
	if got := describeNetErr(timeoutErr{}, 2222); !strings.Contains(got, "güvenlik duvarı") {
		t.Errorf("zaman aşımı = %q", got)
	}
	if got := describeNetErr(errors.New("connect: no route to host"), 2222); !strings.Contains(got, "ulaşılamıyor") {
		t.Errorf("yol yok = %q", got)
	}
	if describeNetErr(nil, 2222) != "" {
		t.Error("hata yokken metin döndü")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// Bind=127.0.0.1: yalnızca geri döngü dinlenir, çoklu yayın AÇILMAZ
// (Windows'ta güvenlik duvarı penceresini tetiklememek için; bkz.
// model.ClusterConfig.Bind).
func TestBindLoopbackOnly(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	m := NewManager(model.ClusterConfig{Enabled: true, NodeName: "yerel", Port: port,
		Secret: testKey, Bind: "127.0.0.1"}, "test", nil, nil, nil)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if m.conn != nil {
		t.Error("geri döngü kipinde çoklu yayın soketi açıldı")
	}
	if got := m.ln.Addr().(*net.TCPAddr).IP; !got.IsLoopback() {
		t.Errorf("dinlenen adres %v, 127.0.0.1 bekleniyordu", got)
	}
	if _, err := ProbeNodeID(context.Background(), "127.0.0.1", port); err != nil {
		t.Errorf("geri döngüden yanıt yok: %v", err)
	}
}

// Windows'ta ölçülen hata: ilk arabirim VirtualBox'ınkiydi (192.168.56.1).
func TestPickLANIPv4SkipsVirtualAdapters(t *testing.T) {
	c := []ifaceIPv4{
		{name: "VirtualBox Host-Only Network", ip: net.ParseIP("192.168.56.1").To4()},
		{name: "vEthernet (WSL)", ip: net.ParseIP("172.25.160.1").To4()},
		{name: "Wi-Fi", ip: net.ParseIP("192.168.1.34").To4()},
	}
	if got := pickLANIPv4(c); got != "192.168.1.34" {
		t.Errorf("seçilen %s, Wi-Fi adresi 192.168.1.34 bekleniyordu", got)
	}
	// KARŞI-SINAMA: yalnızca sanal arabirim varsa boş değil, o döner.
	if got := pickLANIPv4(c[:1]); got != "192.168.56.1" {
		t.Errorf("tek aday yok sayıldı: %q", got)
	}
}
