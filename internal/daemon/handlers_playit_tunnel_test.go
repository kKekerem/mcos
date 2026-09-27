package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/tunnel"
)

// playitFake is a scripted api.playit.gg: yol → sırayla yanıtlar (sonuncu
// tekrar eder); her çağrı sayılır.
type playitFake struct {
	mu     sync.Mutex
	routes map[string][]string
	status map[string]int
	header map[string]map[string]string
	hits   map[string]int
	bodies map[string][]map[string]any
}

func (f *playitFake) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	f.mu.Lock()
	f.hits[r.URL.Path]++
	f.bodies[r.URL.Path] = append(f.bodies[r.URL.Path], body)
	rs := f.routes[r.URL.Path]
	resp := `{"status":"error","data":{"type":"path-not-found","message":{"path":"` + r.URL.Path + `"}}}`
	code := 404
	if len(rs) > 0 {
		resp, code = rs[0], 200
		if len(rs) > 1 {
			f.routes[r.URL.Path] = rs[1:]
		}
		if c, ok := f.status[r.URL.Path]; ok {
			code = c
		}
	}
	for k, v := range f.header[r.URL.Path] {
		w.Header().Set(k, v)
	}
	f.mu.Unlock()
	w.WriteHeader(code)
	_, _ = io.WriteString(w, resp)
}

func (f *playitFake) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// newPlayitTestDaemon: gerçek Daemon, geçici veri kökü, sahte playit API'si
// ve kayıtlı (uydurma) anahtar.
func newPlayitTestDaemon(t *testing.T) (*Daemon, *playitFake) {
	t.Helper()
	root := t.TempDir()
	d, err := New(filepath.Join(root, "config.json"), root, log.New(io.Discard, log.LevelError, 16))
	if err != nil {
		t.Fatal(err)
	}
	f := &playitFake{routes: map[string][]string{}, status: map[string]int{},
		header: map[string]map[string]string{}, hits: map[string]int{},
		bodies: map[string][]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	oldBase, oldSecret := tunnel.PlayitAPIBase, tunnel.SecretPath
	tunnel.PlayitAPIBase = srv.URL
	tunnel.SecretPath = filepath.Join(root, "playit.toml")
	t.Cleanup(func() { tunnel.PlayitAPIBase, tunnel.SecretPath = oldBase, oldSecret })
	if err := tunnel.SaveSecret(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	return d, f
}

const testAgentID = "0d3f5b8a-2222-4333-8444-955566667777"

func rundata(tunnels string) string {
	return `{"status":"success","data":{"agent_id":"` + testAgentID + `","tunnels":[` + tunnels +
		`],"pending":[],"notices":[],"permissions":{"is_self_managed":true,"has_premium":false,"account_status":"verified"}}}`
}

func javaTunnel(id, addr string, port int) string {
	return `{"id":"` + id + `","name":"MCOS Survival","display_address":"` + addr +
		`","port_type":"tcp","port_count":1,"tunnel_type":"minecraft-java",` +
		`"agent_config":{"fields":[{"name":"local_port","value":"` + itoaT(port) + `"}]},"disabled_reason":null}`
}

func itoaT(n int) string { b, _ := json.Marshal(n); return string(b) }

func saveTestServer(t *testing.T, d *Daemon, id, name string, port int) {
	t.Helper()
	if err := d.store.SaveServer(&model.Server{ID: id, Name: name, Port: port}); err != nil {
		t.Fatal(err)
	}
}

// Hesap bağlandı, sunucunun tüneli yok: eşitleyici açar, playit listesine
// düşene kadar İKİNCİ bir tünel açmaz, adres gelince panele ve sunucunun
// WAN.Hostname alanına yazar.
func TestPlayitSyncCreatesThenAddress(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Survival", 25565)
	f.routes["/v1/agents/rundata"] = []string{rundata(""), rundata(""),
		rundata(javaTunnel("t-yeni", "mavi-kedi.gl.joinmc.link", 25565))}
	f.routes["/v1/tunnels/create"] = []string{`{"status":"success","data":{"id":"t-yeni"}}`}

	// 1. tur: tünel açılır, adres yok.
	if delay := d.playitSyncOnce(); delay != 5*time.Second {
		t.Fatalf("adres beklerken hızlı okuma bekleniyordu, %v", delay)
	}
	if n := f.count("/v1/tunnels/create"); n != 1 {
		t.Fatalf("tünel açılmadı (%d istek)", n)
	}
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	if len(res.Tunnels) == 0 || res.Tunnels[0].State != ipc.PlayitTunnelCreating ||
		res.Tunnels[0].ServerID != "s1" || !res.Syncing || res.Address != "" {
		t.Fatalf("ilerleme görünmüyor: %+v", res)
	}
	if srv, _ := d.store.GetServer("s1"); srv.WAN.TunnelID != "t-yeni" {
		t.Fatalf("tünel kimliği sunucuya yazılmadı: %+v", srv.WAN)
	}

	// 2. tur: playit listesi gecikti (tünel henüz yok) — yenisi AÇILMAMALI.
	d.playitSyncOnce()
	if n := f.count("/v1/tunnels/create"); n != 1 {
		t.Fatalf("liste gecikince aynı port için ikinci tünel açıldı (%d)", n)
	}

	// 3. tur: adres geldi.
	if delay := d.playitSyncOnce(); delay != playitIdlePoll {
		t.Fatalf("adres hazırken boşta okuma bekleniyordu, %v", delay)
	}
	res = playitStatusResult{}
	d.fillPlayitTunnels(&res)
	if res.Address != "mavi-kedi.gl.joinmc.link" || res.Tunnels[0].State != ipc.PlayitTunnelOpen ||
		res.Syncing {
		t.Fatalf("adres panele gelmedi: %+v", res)
	}
	srv, _ := d.store.GetServer("s1")
	if srv.WAN.Hostname != "mavi-kedi.gl.joinmc.link" {
		t.Fatalf("adres sunucuya yazılmadı: %+v", srv.WAN)
	}
	if n := f.count("/v1/tunnels/create"); n != 1 {
		t.Fatalf("tünel varken yeniden açıldı (%d)", n)
	}
	// Ad Türkçe karakter taşımıyor; yerel port sunucunun portu.
	b := f.bodies["/v1/tunnels/create"][0]
	if b["name"] != "MCOS Survival" {
		t.Fatalf("tünel adı: %v", b["name"])
	}
}

// Sitede elle açılmış tünel (aynı yerel port) kullanılır; yenisi açılmaz.
func TestPlayitSyncUsesExistingTunnel(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Yaratıcı", 25570)
	f.routes["/v1/agents/rundata"] = []string{rundata(javaTunnel("t-site", "yesil.gl.joinmc.link", 25570))}

	d.playitSyncOnce()
	if n := f.count("/v1/tunnels/create"); n != 0 {
		t.Fatalf("var olan tünel yerine yenisi açıldı (%d)", n)
	}
	srv, _ := d.store.GetServer("s1")
	if srv.WAN.Hostname != "yesil.gl.joinmc.link" || srv.WAN.TunnelID != "t-site" {
		t.Fatalf("adres/kimlik yazılmadı: %+v", srv.WAN)
	}
}

// 429: Retry-After'a (en az 30 sn) uyulur; bekleme bitmeden gelen tur (ör.
// panelden "yenile") API'ye İSTEK ATMAZ.
func TestPlayitSyncRateLimit(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	f.routes["/v1/agents/rundata"] = []string{`too many`}
	f.status["/v1/agents/rundata"] = 429
	f.header["/v1/agents/rundata"] = map[string]string{"Retry-After": "2"}

	delay := d.playitSyncOnce()
	if delay < playitMinRateWait {
		t.Fatalf("429'da en az %v beklenmeli, %v", playitMinRateWait, delay)
	}
	d.kickPlayitSyncIfTargetsChanged() // döngü yok (ajan kapalı); yalnızca etkisiz olmalı
	if again := d.playitSyncOnce(); again <= 0 || again > delay {
		t.Fatalf("bekleme sürerken tur yeniden istek atmaya çalıştı: %v", again)
	}
	if n := f.count("/v1/agents/rundata"); n != 1 {
		t.Fatalf("429'dan sonra %d istek atıldı (spam)", n)
	}
	if f.count("/agents/rundata") != 0 {
		t.Fatal("429'da eski uca da gidildi")
	}
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	if !strings.Contains(res.TunnelError, "yeniden denenecek") {
		t.Fatalf("Türkçe 429 iletisi yok: %q", res.TunnelError)
	}
}

// Hesap e-postası doğrulanmamış: tünel satırı "error", Türkçe neden görünür.
func TestPlayitSyncVerifiedAccountError(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	f.routes["/v1/agents/rundata"] = []string{rundata("")}
	f.routes["/v1/tunnels/create"] = []string{`{"status":"fail","data":"RequiresVerifiedAccount"}`}

	d.playitSyncOnce()
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	want := "playit hesabınızın e-postasını doğrulayın (playit.gg → hesap)"
	if res.TunnelError != want || len(res.Tunnels) != 1 ||
		res.Tunnels[0].State != ipc.PlayitTunnelError || res.Tunnels[0].Detail != want {
		t.Fatalf("doğrulama hatası kullanıcıya ulaşmadı: %+v", res)
	}
	// Hiç sunucu yok → 25565 açılmaya çalışıldı.
	if res.Tunnels[0].LocalPort != 25565 {
		t.Fatalf("varsayılan port: %d", res.Tunnels[0].LocalPort)
	}
}

// Gerçek 401 (InvalidAgentKey): anahtar geçersiz → yeniden bağlama iste,
// 5 dakikadan sık deneme.
func TestPlayitSyncKeyInvalid(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	real401 := `{"status":"error","data":{"type":"auth","message":"InvalidAgentKey"}}`
	f.routes["/v1/agents/rundata"] = []string{real401}
	f.status["/v1/agents/rundata"] = 401
	f.routes["/agents/rundata"] = []string{real401}
	f.status["/agents/rundata"] = 401

	if delay := d.playitSyncOnce(); delay != playitMaxBackoff {
		t.Fatalf("ölü anahtarla sık deneme: %v", delay)
	}
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	if !res.KeyInvalid || res.TunnelError != "anahtar geçersiz; hesabı yeniden bağlayın" {
		t.Fatalf("anahtar hatası bildirilmedi: %+v", res)
	}
}

func TestPlayitPickTargets(t *testing.T) {
	servers := []*model.Server{
		{ID: "a", Name: "Ana", Port: 25565},
		{ID: "b", Name: "Şövalye", Port: 25566, WAN: model.WANConfig{Enabled: true}},
		{ID: "c", Name: "Ç", Port: 25567},
		{ID: "d", Name: "Aynı port", Port: 25566, WAN: model.WANConfig{Enabled: true}},
	}
	states := map[string]model.ServerState{}
	state := func(id string) model.ServerState {
		if s, ok := states[id]; ok {
			return s
		}
		return model.StateStopped
	}
	ports := func(ts []playitTarget) string {
		var p []string
		for _, t := range ts {
			p = append(p, t.ServerID+":"+itoaT(t.Port))
		}
		return strings.Join(p, ",")
	}

	// Hiçbiri çalışmıyor → ilk sunucu.
	if got := ports(playitPickTargets(servers, state, nil)); got != "a:25565" {
		t.Errorf("hiçbiri çalışmıyorken: %s", got)
	}
	// WAN kapalı c çalışıyor → ilk ÇALIŞAN sunucu.
	states["c"] = model.StateRunning
	if got := ports(playitPickTargets(servers, state, nil)); got != "c:25567" {
		t.Errorf("WAN işaretli yokken: %s", got)
	}
	// WAN açık b ve d çalışıyor (aynı port) → tek tünel; yalnız işaretliler.
	states["b"], states["d"] = model.StateRunning, model.StateStarting
	if got := ports(playitPickTargets(servers, state, nil)); got != "b:25566" {
		t.Errorf("WAN açıkken: %s", got)
	}
	// Kullanıcı a'yı istedi → eklenir.
	if got := ports(playitPickTargets(servers, state, map[string]bool{"a": true})); got != "b:25566,a:25565" {
		t.Errorf("istenen sunucu: %s", got)
	}
	// Hiç sunucu yok → 25565.
	ts := playitPickTargets(nil, state, nil)
	if len(ts) != 1 || ts[0].Port != 25565 || ts[0].ServerID != "" {
		t.Errorf("sunucusuz: %+v", ts)
	}
	if n := playitPickTargets(servers, state, nil)[0].Name; n != "MCOS Sovalye" {
		t.Errorf("tünel adı ASCII değil: %q", n)
	}
}

// playit.tunnel ANINDA döner; ajan/hesap yoksa anlaşılır biçimde reddeder
// (bu makinede /usr/bin/playitd yok → "ajan bu imajda yok").
func TestPlayitTunnelRPCReturnsAtOnce(t *testing.T) {
	d, _ := newPlayitTestDaemon(t)
	_ = os.Remove(tunnel.SecretPath)
	start := time.Now()
	_, err := d.handlePlayitTunnel(t.Context(), json.RawMessage(`{"serverId":"yok"}`))
	ie, ok := err.(*ipc.Error)
	if !ok || ie.Code != ipc.CodeUnavailable {
		t.Fatalf("ajansız/hesapsız istek yanlış reddedildi: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("RPC bekletti")
	}
}

// UÇTAN UCA: playit.start panelin 10 sn sınırının içinde döner; tünel ARKA
// PLANDA açılır ve adres playit.status'a kendiliğinden düşer. Kullanıcının
// şikâyeti tam olarak buydu: "ajan çalışıyor; playit.gg sitesinden tünel
// oluşturun" yazıp bırakıyordu.
func TestPlayitStartOpensTunnelInBackground(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Survival", 25565)
	f.routes["/v1/agents/rundata"] = []string{rundata(""),
		rundata(javaTunnel("t-yeni", "mavi-kedi.gl.joinmc.link", 25565))}
	f.routes["/v1/tunnels/create"] = []string{`{"status":"success","data":{"id":"t-yeni"}}`}

	// Sahte playitd: hiçbir şey yazmadan bekler (gerçeği de adresi günlüğe
	// yazmayabilir — adres API'den gelmeli).
	bin := t.TempDir()
	fakeD := filepath.Join(bin, "playitd")
	fakeC := filepath.Join(bin, "playit-cli")
	if err := os.WriteFile(fakeD, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeC, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunnel.SetPlayitBinsForTest(fakeD, fakeC))
	t.Cleanup(d.playitStop)

	start := time.Now()
	if _, err := d.handlePlayitStart(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("playit.start %v bekletti (panel 10 sn'de keser)", el)
	}
	// Tünel, kimse playit.status'u YOKLAMADAN açılmalı: açılışta otomatik
	// başlatmada ve hesap bağlandığında panel tünel ekranında olmayabilir.
	for wait := time.Now().Add(5 * time.Second); f.count("/v1/tunnels/create") == 0; {
		if time.Now().After(wait) {
			t.Fatal("ajan başladı ama tünel açılmadı (eşitleyici yalnızca durum yoklamasıyla uyanıyor)")
		}
		time.Sleep(50 * time.Millisecond)
	}

	deadline := time.Now().Add(20 * time.Second)
	var res playitStatusResult
	for time.Now().Before(deadline) {
		r, err := d.handlePlayitStatus(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		res = r.(playitStatusResult)
		if res.Address != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if res.Address != "mavi-kedi.gl.joinmc.link" {
		t.Fatalf("adres kendiliğinden gelmedi: %+v", res)
	}
	if res.Note != "Tünel açık: mavi-kedi.gl.joinmc.link" || !res.Running {
		t.Fatalf("not/durum yanlış: %q running=%v", res.Note, res.Running)
	}
	if n := f.count("/v1/tunnels/create"); n != 1 {
		t.Fatalf("%d tünel açıldı, 1 bekleniyordu", n)
	}
	if srv, _ := d.store.GetServer("s1"); srv.WAN.Hostname != "mavi-kedi.gl.joinmc.link" {
		t.Fatalf("adres sunucuya yazılmadı: %+v", srv.WAN)
	}
}

// playit açma isteğini kabul edip tüneli listede HİÇ göstermezse eşitleyici
// her 3 dakikada bir yeni tünel açıp hesabı doldurmamalı: 2 denemeden sonra
// durur ve nedenini söyler; elle istek sayacı sıfırlar.
func TestPlayitSyncStopsRecreatingInvisibleTunnel(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Survival", 25565)
	f.routes["/v1/agents/rundata"] = []string{rundata("")}
	f.routes["/v1/tunnels/create"] = []string{`{"status":"success","data":{"id":"hayalet"}}`}

	age := func() { // 3 dakikalık bekleme dolmuş gibi
		st := d.playit()
		st.mu.Lock()
		c := st.tun.created[25565]
		c.at = c.at.Add(-playitRecreateAfter - time.Second)
		st.tun.created[25565] = c
		st.mu.Unlock()
	}
	for i := 0; i < 4; i++ {
		d.playitSyncOnce()
		age()
	}
	if n := f.count("/v1/tunnels/create"); n != playitMaxAutoCreates {
		t.Fatalf("görünmeyen tünel için %d kez açıldı, en fazla %d", n, playitMaxAutoCreates)
	}
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	if !strings.Contains(res.TunnelError, "listede göstermiyor") {
		t.Fatalf("kullanıcıya neden söylenmedi: %q", res.TunnelError)
	}
	// Elle "Tünel oluştur/yenile" (burada doğrudan sayaç sıfırlama yolu).
	st := d.playit()
	st.mu.Lock()
	c := st.tun.created[25565]
	c.count = 0
	st.tun.created[25565] = c
	st.mu.Unlock()
	d.playitSyncOnce()
	if n := f.count("/v1/tunnels/create"); n != playitMaxAutoCreates+1 {
		t.Fatalf("sayaç sıfırlanınca yeniden denenmedi (%d)", n)
	}
}

// startFakePlayitd runs playit.start with a fake playitd (bekler, hiçbir şey
// yazmaz) so the sync loop really runs in the background.
func startFakePlayitd(t *testing.T, d *Daemon) {
	t.Helper()
	bin := t.TempDir()
	fakeD := filepath.Join(bin, "playitd")
	fakeC := filepath.Join(bin, "playit-cli")
	if err := os.WriteFile(fakeD, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeC, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunnel.SetPlayitBinsForTest(fakeD, fakeC))
	t.Cleanup(d.playitStop)
	if _, err := d.handlePlayitStart(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}

// Hatalı turdan sonra panelin durum yoklaması (watchTunnel: 2 sn'de bir,
// 5 dk boyunca) eşitleyiciyi HER SEFERİNDE dürtmemeli. Yakalanan hata:
// hatalı turda hedefler hesaplanmadığı için targetsKey "" kalıyor, her
// playit.status "hedefler değişti" sanıp beklemeyi deliyor ve playit'e 2
// sn'de bir istek gidiyordu (bir iç hatada ya da ağ kesintisinde; 429'a
// kadar).
func TestPlayitStatusPollDoesNotHammerOnError(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Survival", 25565)
	f.routes["/v1/agents/rundata"] = []string{`{"status":"error","data":{"type":"internal","message":{"trace_id":"x"}}}`}
	f.status["/v1/agents/rundata"] = 500

	startFakePlayitd(t, d)
	for wait := time.Now().Add(5 * time.Second); f.count("/v1/agents/rundata") == 0; {
		if time.Now().After(wait) {
			t.Fatal("eşitleyici hiç çalışmadı")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for wait := time.Now().Add(2 * time.Second); ; {
		var res playitStatusResult
		d.fillPlayitTunnels(&res)
		if res.TunnelError != "" {
			break
		}
		if time.Now().After(wait) {
			t.Fatal("ilk turun hatası durumda görünmedi")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := 0; i < 8; i++ {
		if _, err := d.handlePlayitStatus(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n := f.count("/v1/agents/rundata"); n != 1 {
		t.Fatalf("hata beklemesi durum yoklamasıyla delindi: %d istek (1 bekleniyordu)", n)
	}
}

// Sunucusuz açılan varsayılan tünel ("MCOS Minecraft", 25565) BEKLERKEN
// eklenen 25570'teki sunucu o tünele bağlanmamalı: kimliği kapıp 25565'in
// adresini kendi genel adresi diye göstermek ve kendisi için tünel
// açılmaması demekti.
func TestPlayitSyncDoesNotAdoptOtherPortTunnel(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Yaratıcı", 25570)
	// s2'nin manifesti başka bir sunucudan kopyalanmış: tünel kimliği
	// 25565'e giden varsayılan tünelin.
	if err := d.store.SaveServer(&model.Server{ID: "s2", Name: "Kopya", Port: 25571,
		WAN: model.WANConfig{TunnelID: "p-def"}}); err != nil {
		t.Fatal(err)
	}
	pendingDef := `{"id":"p-def","name":"MCOS Minecraft","tunnel_type":"minecraft-java",` +
		`"tunnel_type_display":"Minecraft Java","port_type":"tcp","port_count":1,"status_msg":"allocating"}`
	withPending := strings.Replace(rundata(""), `"pending":[]`, `"pending":[`+pendingDef+`]`, 1)
	f.routes["/v1/agents/rundata"] = []string{withPending,
		rundata(javaTunnel("p-def", "varsayilan.gl.joinmc.link", 25565))}
	f.routes["/v1/tunnels/create"] = []string{`{"status":"success","data":{"id":"t-25570"}}`}

	d.playitSyncOnce()
	if srv, _ := d.store.GetServer("s1"); srv.WAN.TunnelID != "t-25570" {
		t.Fatalf("sunucu başka porta giden bekleyen tünele bağlandı: %+v", srv.WAN)
	}
	d.playitSyncOnce()
	srv, _ := d.store.GetServer("s1")
	if srv.WAN.Hostname == "varsayilan.gl.joinmc.link" {
		t.Fatalf("25570'teki sunucu 25565'in adresini gösteriyor: %+v", srv.WAN)
	}
	if s2, _ := d.store.GetServer("s2"); s2.WAN.Hostname != "" || s2.WAN.TunnelID != "" {
		t.Fatalf("kopyalanmış kimlik 25571'deki sunucuya 25565'in adresini yazdı: %+v", s2.WAN)
	}
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	if len(res.Tunnels) == 0 || res.Tunnels[0].ServerID != "s1" ||
		res.Tunnels[0].State != ipc.PlayitTunnelCreating || res.Tunnels[0].LocalPort != 25570 {
		t.Fatalf("s1 satırı yanlış: %+v", res.Tunnels)
	}
}

// Elle açma ipucu hedef sunucunun portunu söyler.
func TestPlayitManualHintUsesServerPort(t *testing.T) {
	d, f := newPlayitTestDaemon(t)
	saveTestServer(t, d, "s1", "Yaratıcı", 25570)
	scope := `{"status":"error","data":{"type":"auth","message":"ScopeNotAllowed"}}`
	f.routes["/v1/agents/rundata"] = []string{rundata("")}
	f.routes["/v1/tunnels/create"] = []string{scope}
	f.status["/v1/tunnels/create"] = 401
	f.routes["/tunnels/create"] = []string{scope}
	f.status["/tunnels/create"] = 401

	d.playitSyncOnce()
	var res playitStatusResult
	d.fillPlayitTunnels(&res)
	if !strings.Contains(res.TunnelError, "127.0.0.1:25570") ||
		len(res.Tunnels) != 1 || !strings.Contains(res.Tunnels[0].Detail, "127.0.0.1:25570") {
		t.Fatalf("ipucu sunucunun portunu söylemiyor: %q / %+v", res.TunnelError, res.Tunnels)
	}
}

// playit.tunnel uzaktan köprüden de çağrılabilir: sunucu kimliği yol
// parçası olarak kullanılmadan ÖNCE doğrulanır.
func TestPlayitTunnelRejectsBadServerID(t *testing.T) {
	d, _ := newPlayitTestDaemon(t)
	bin := t.TempDir()
	for _, n := range []string{"playitd", "playit-cli"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(tunnel.SetPlayitBinsForTest(filepath.Join(bin, "playitd"), filepath.Join(bin, "playit-cli")))
	// servers/ dizininin DIŞINDA bir manifest: doğrulamasız GetServer("..")
	// servers/../manifest.json'u okuyup onu sunucu sanardı.
	outside := filepath.Join(d.store.Paths.Root, "manifest.json")
	_ = os.WriteFile(outside, []byte(`{"id":"x","name":"dışarıda","port":25599}`), 0o644)
	t.Cleanup(func() { _ = os.Remove(outside) })

	_, err := d.handlePlayitTunnel(t.Context(), json.RawMessage(`{"serverId":".."}`))
	ie, ok := err.(*ipc.Error)
	if !ok || ie.Code != ipc.CodeInvalidParams {
		t.Fatalf("geçersiz kimlik reddedilmedi: %v", err)
	}
	st := d.playit()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.tun.requested[".."] {
		t.Fatal("geçersiz kimlik hedef listesine yazıldı")
	}
}

// Boşta beklerken (bütün adresler hazır, 2 dk'lık okuma aralığı) yeni bir
// hedef çıkarsa — ilk sunucu oluşturuldu, WAN'ı açık sunucu başladı — tünel
// kimse panelin tünel ekranını açmadan açılmalı. Eskiden hedef değişimi
// yalnızca playit.status çağrısıyla fark ediliyordu; panel başka ekrandayken
// yeni sunucu 2 dakikaya kadar tünelsiz ve "Genel adres: —" kalıyordu.
func TestPlayitIdleLoopNoticesNewTarget(t *testing.T) {
	old := playitTargetCheck
	playitTargetCheck = 50 * time.Millisecond
	t.Cleanup(func() { playitTargetCheck = old })

	d, f := newPlayitTestDaemon(t)
	// Sunucu yok: varsayılan 25565 hedefi zaten açık → eşitleyici boşta.
	f.routes["/v1/agents/rundata"] = []string{rundata(javaTunnel("t-def", "def.gl.joinmc.link", 25565))}
	f.routes["/v1/tunnels/create"] = []string{`{"status":"success","data":{"id":"t-25570"}}`}
	startFakePlayitd(t, d)
	for wait := time.Now().Add(5 * time.Second); ; {
		var res playitStatusResult
		d.fillPlayitTunnels(&res)
		if res.Address == "def.gl.joinmc.link" && !res.Syncing {
			break
		}
		if time.Now().After(wait) {
			t.Fatalf("eşitleyici boşa geçmedi: %+v", res)
		}
		time.Sleep(20 * time.Millisecond)
	}

	saveTestServer(t, d, "s1", "Yaratıcı", 25570)
	for wait := time.Now().Add(3 * time.Second); f.count("/v1/tunnels/create") == 0; {
		if time.Now().After(wait) {
			t.Fatal("yeni sunucunun tüneli boşta beklerken açılmadı (yalnızca durum yoklaması fark ediyor)")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := f.count("/v1/agents/rundata"); n > 3 {
		t.Fatalf("yerel hedef denetimi playit'e istek attı: %d rundata", n)
	}
}

// Hesap BAĞLANINCA ajan açılışta da kendiliğinden başlamalı. Yakalanan
// boşluk: otomatik başlatma yalnızca sihirbazdaki "playit tünel servisi"
// anahtarına (cfg.WAN.Autostart; DefaultConfig'te KAPALI) bakıyordu.
// Sihirbazda kapatıp sonra hesabını bağlayan kullanıcının tüneli her yeniden
// başlatmada kapalı kalıyor, "tünel kendiliğinden açılsın" isteği yalnızca
// ilk oturumda tutuyordu.
func TestPlayitClaimEnablesAutostart(t *testing.T) {
	d, _ := newPlayitTestDaemon(t)
	_ = os.Remove(tunnel.SecretPath)
	cp := *d.Config()
	cp.WAN.Autostart = false
	if err := d.saveConfigCopy(&cp); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	fakeD := filepath.Join(bin, "playitd")
	fakeC := filepath.Join(bin, "playit-cli")
	if err := os.WriteFile(fakeD, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cli := "#!/bin/sh\ncase \"$1 $2\" in\n" +
		"'claim generate') echo 0123456789 ;;\n" +
		"'claim url') echo https://playit.gg/claim/$3 ;;\n" +
		"'claim exchange') echo " + strings.Repeat("cd", 32) + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(fakeC, []byte(cli), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunnel.SetPlayitBinsForTest(fakeD, fakeC))
	t.Cleanup(d.playitStop)

	if _, err := d.handlePlayitClaim(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	var res map[string]any
	for wait := time.Now().Add(5 * time.Second); ; {
		r, err := d.handlePlayitPoll(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		res = r.(map[string]any)
		if res["pending"] == false {
			break
		}
		if time.Now().After(wait) {
			t.Fatal("onay bitmedi")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if res["claimed"] != true {
		t.Fatalf("hesap bağlanmadı: %v", res)
	}
	if !d.Config().WAN.Autostart {
		t.Fatal("hesap bağlandı ama ajan açılışta başlamayacak (WAN.Autostart kapalı kaldı)")
	}
	// Diske de yazılmış olmalı: yeniden başlatmayı atlatan budur.
	b, err := os.ReadFile(d.cfgPath)
	var disk model.Config
	if err == nil {
		err = json.Unmarshal(b, &disk)
	}
	if err != nil || !disk.WAN.Autostart {
		t.Fatalf("yapılandırma dosyasına yazılmadı: %v %+v", err, disk.WAN)
	}
}
