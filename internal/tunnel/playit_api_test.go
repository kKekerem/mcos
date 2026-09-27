package tunnel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── GERÇEK api.playit.gg yanıtları ──────────────────────────────────────────
// 2026-09-27'de hesapsız ölçüldü: anahtarsız ve rastgele (kimseye ait
// olmayan) 64 haneli uydurma anahtarla POST. /v1/agents/rundata,
// /agents/rundata, /v1/tunnels/create ve /tunnels/create AYNI yanıtları
// verdi. Hiçbir gerçek hesap ya da başkasının anahtarı kullanılmadı.
const (
	realNoAuth    = `{"status":"error","data":{"type":"auth","message":"AuthRequired"}}`    // HTTP 401
	realWrongKey  = `{"status":"error","data":{"type":"auth","message":"InvalidAgentKey"}}` // HTTP 401
	realBadHeader = `{"status":"error","data":{"type":"auth","message":"InvalidHeader"}}`   // HTTP 401 ("Authorization: Bearer xyz")
	realNoPath    = `{"status":"error","data":{"type":"path-not-found","message":{"path":"/v1/does/not/exist"}}}`
)

const testSecret = "4b1d0c0ffee0000000000000000000000000000000000000000000000000beef"
const testAgent = "9f5a3c2e-1111-4222-8333-944455556666"

// fakePlayit is an httptest playit API that records every call.
type fakePlayit struct {
	t     *testing.T
	mu    sync.Mutex
	calls []fakeCall
	// routes: yol → sırayla verilecek yanıtlar (son yanıt tekrar eder).
	routes map[string][]fakeResp
}

type fakeCall struct {
	Path string
	Auth string
	Body map[string]any
}

type fakeResp struct {
	Status int
	Body   string
	Header map[string]string
}

func newFakePlayit(t *testing.T) (*fakePlayit, *PlayitAPI) {
	f := &fakePlayit{t: t, routes: map[string][]fakeResp{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &PlayitAPI{Base: srv.URL, Secret: testSecret + "\n",
		HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func (f *fakePlayit) on(path string, rs ...fakeResp) {
	f.mu.Lock()
	f.routes[path] = rs
	f.mu.Unlock()
}

func ok(data string) fakeResp {
	return fakeResp{Status: 200, Body: `{"status":"success","data":` + data + `}`}
}

func (f *fakePlayit) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	rs := f.routes[r.URL.Path]
	var resp fakeResp
	switch {
	case len(rs) == 0:
		resp = fakeResp{Status: 404, Body: `{"status":"error","data":{"type":"path-not-found","message":{"path":"` + r.URL.Path + `"}}}`}
	case len(rs) == 1:
		resp = rs[0]
	default:
		resp = rs[0]
		f.routes[r.URL.Path] = rs[1:]
	}
	f.mu.Unlock()
	if r.Method != http.MethodPost {
		f.t.Errorf("%s: yöntem %s, POST bekleniyordu", r.URL.Path, r.Method)
	}
	for k, v := range resp.Header {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	_, _ = io.WriteString(w, resp.Body)
}

func (f *fakePlayit) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Path == path {
			n++
		}
	}
	return n
}

func (f *fakePlayit) last(path string) fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Path == path {
			return f.calls[i]
		}
	}
	f.t.Fatalf("%s hiç çağrılmadı", path)
	return fakeCall{}
}

// v1RunData builds a /v1/agents/rundata payload.
func v1RunData(tunnels, pending string) string {
	return `{"agent_id":"` + testAgent + `","tunnels":[` + tunnels + `],"pending":[` + pending +
		`],"notices":[],"permissions":{"is_self_managed":true,"has_premium":false,"account_status":"verified"}}`
}

const v1JavaTunnel = `{"id":"t-java","internal_id":7,"name":"MCOS Survival",
	"display_address":"mavi-kedi.gl.joinmc.link","port_type":"tcp","port_count":1,
	"tunnel_type":"minecraft-java","tunnel_type_display":"Minecraft Java",
	"agent_config":{"fields":[{"name":"local_ip","value":""},{"name":"local_port","value":"25565"}]},
	"disabled_reason":null}`

// Var olan tünel (aynı yerel port) YENİDEN AÇILMAZ; adres rundata'dan gelir.
func TestEnsureReusesExistingTunnel(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/agents/rundata", ok(v1RunData(v1JavaTunnel, "")))
	f.on("/v1/tunnels/create", ok(`{"id":"yeni"}`))

	res, err := api.EnsureMinecraftTunnel(25565, "Survival")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || f.count("/v1/tunnels/create") != 0 {
		t.Fatal("tünel zaten vardı ama yenisi açıldı (ücretsiz hesabın sınırı boşa gider)")
	}
	if res.Tunnel.Address != "mavi-kedi.gl.joinmc.link" || res.Tunnel.ID != "t-java" {
		t.Fatalf("yanlış tünel: %+v", res.Tunnel)
	}
	if res.Tunnel.LocalIP != "127.0.0.1" {
		t.Fatalf("boş local_ip playitd gibi 127.0.0.1 sayılmalı: %q", res.Tunnel.LocalIP)
	}
	// Anahtar KIRPILARAK gönderilmeli (dosyadaki satır sonu başlığa girmesin).
	if a := f.last("/v1/agents/rundata").Auth; a != "Agent-Key "+testSecret {
		t.Fatalf("Authorization başlığı yanlış: %q", a)
	}
}

// Tünel yoksa v1 ucuyla, doğru gövdeyle açılır.
func TestEnsureCreatesNewTunnel(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/agents/rundata", ok(v1RunData("", "")))
	f.on("/v1/tunnels/create", ok(`{"id":"t-yeni"}`))

	res, err := api.EnsureMinecraftTunnel(25570, "Şövalye Dünyası")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Tunnel.ID != "t-yeni" || !res.Tunnel.Pending {
		t.Fatalf("yeni tünel bildirilmedi: %+v", res)
	}
	b := f.last("/v1/tunnels/create").Body
	ports := b["ports"].(map[string]any)
	if ports["type"] != "tunnel-type" || ports["details"] != "minecraft-java" {
		t.Fatalf("ports yanlış: %v", ports)
	}
	data := b["origin"].(map[string]any)["data"].(map[string]any)
	if data["agent_id"] != testAgent {
		t.Fatalf("agent_id yanlış: %v", data["agent_id"])
	}
	fields := map[string]string{}
	for _, x := range data["config"].(map[string]any)["fields"].([]any) {
		m := x.(map[string]any)
		fields[m["name"].(string)] = m["value"].(string)
	}
	if fields["local_port"] != "25570" || fields["local_ip"] != "127.0.0.1" {
		t.Fatalf("yerel adres alanları yanlış: %v", fields)
	}
	if b["name"] != "Sovalye Dunyasi" {
		t.Fatalf("ad ASCII'ye çevrilmedi (TunnelNameIsNotAscii): %q", b["name"])
	}
	if b["enabled"] != true {
		t.Fatal("tünel kapalı açılıyor")
	}
	if f.count("/tunnels/create") != 0 {
		t.Fatal("v1 başarılıyken eski uca da gidildi")
	}
}

// v1 ucu "yol yok" derse (gerçek 404 zarfı) eski /tunnels/create denenir.
func TestCreateFallsBackToOldEndpoint(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/tunnels/create", fakeResp{Status: 404,
		Body: strings.Replace(realNoPath, "/v1/does/not/exist", "/v1/tunnels/create", 1)})
	f.on("/tunnels/create", ok(`{"id":"t-eski"}`))

	id, err := api.CreateMinecraftTunnel(testAgent, 25565, "Survival")
	if err != nil {
		t.Fatal(err)
	}
	if id != "t-eski" {
		t.Fatalf("eski uçtan kimlik alınmadı: %q", id)
	}
	b := f.last("/tunnels/create").Body
	data := b["origin"].(map[string]any)["data"].(map[string]any)
	if b["tunnel_type"] != "minecraft-java" || b["port_type"] != "tcp" ||
		data["local_port"] != float64(25565) || data["agent_id"] != testAgent {
		t.Fatalf("eski uç gövdesi yanlış: %v", b)
	}
}

// Eski rundata ucu: v1 yoksa tüneller oradan okunur.
func TestRunDataFallsBackToOld(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/agents/rundata", ok(`{"agent_id":"`+testAgent+`","agent_type":"self-managed",
		"account_status":"guest","tunnels":[{"id":"t1","internal_id":1,"name":"mc","ip_num":1,
		"region_num":2,"port":{"from":41234,"to":41235},"proto":"tcp","local_ip":"127.0.0.1",
		"local_port":25565,"tunnel_type":"minecraft-java","assigned_domain":"yesil.joinmc.link",
		"custom_domain":null,"disabled":null,"proxy_protocol":null,"agent_config":null}],
		"pending":[],"account_features":{}}`))

	rd, err := api.RunData()
	if err != nil {
		t.Fatal(err)
	}
	if f.count("/v1/agents/rundata") != 1 || !rd.Legacy {
		t.Fatal("önce v1 denenip sonra eski uca düşülmeli")
	}
	if len(rd.Tunnels) != 1 || rd.Tunnels[0].Address != "yesil.joinmc.link" ||
		rd.Tunnels[0].LocalPort != 25565 {
		t.Fatalf("eski biçim çözülmedi: %+v", rd.Tunnels)
	}
	if m := rd.Messages(); len(m) == 0 || m[0] != msgGuest {
		t.Fatalf("misafir hesap uyarısı yok: %v", m)
	}
}

// "RequiresVerifiedAccount" kullanıcıya Türkçe, yapılacak işle söylenir; bu
// GERÇEK bir yanıt olduğu için eski uca düşülmez.
func TestRequiresVerifiedAccountTurkish(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/agents/rundata", ok(v1RunData("", "")))
	f.on("/v1/tunnels/create", fakeResp{Status: 200,
		Body: `{"status":"fail","data":"RequiresVerifiedAccount"}`})
	f.on("/tunnels/create", ok(`{"id":"olmamali"}`))

	_, err := api.EnsureMinecraftTunnel(25565, "Survival")
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if err.Error() != "playit hesabınızın e-postasını doğrulayın (playit.gg → hesap)" {
		t.Fatalf("Türkçe ileti yanlış: %q", err.Error())
	}
	if f.count("/tunnels/create") != 0 {
		t.Fatal("iş kuralı hatasında eski uca düşülmemeli")
	}
}

// Gerçek 401 yanıtları: anahtar geçersiz → "hesabı yeniden bağlayın".
func TestAuthErrorRealFixtures(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{realWrongKey, "InvalidAgentKey"},
		{realNoAuth, "AuthRequired"},
	} {
		f, api := newFakePlayit(t)
		f.on("/v1/agents/rundata", fakeResp{Status: 401, Body: tc.body})
		f.on("/agents/rundata", fakeResp{Status: 401, Body: tc.body})
		_, err := api.RunData()
		if err == nil {
			t.Fatalf("%s: hata bekleniyordu", tc.code)
		}
		ae, _ := err.(*APIError)
		if ae == nil || ae.Kind != KindAuth || ae.Code != tc.code || ae.HTTPStatus != 401 {
			t.Fatalf("%s: zarf çözülmedi: %#v", tc.code, err)
		}
		if !KeyInvalid(err) || err.Error() != "anahtar geçersiz; hesabı yeniden bağlayın" {
			t.Fatalf("%s: kullanıcı iletisi yanlış: %q", tc.code, err.Error())
		}
	}
	// Bozuk başlık bizim hatamızdır, "yeniden bağlayın" DEĞİL.
	f, api := newFakePlayit(t)
	f.on("/v1/agents/rundata", fakeResp{Status: 401, Body: realBadHeader})
	f.on("/agents/rundata", fakeResp{Status: 401, Body: realBadHeader})
	if _, err := api.RunData(); err == nil || KeyInvalid(err) {
		t.Fatalf("InvalidHeader anahtar hatası sayıldı: %v", err)
	}
}

// Gerçek path-not-found iletisi NESNEDİR; dizge sanılırsa zarf düşer.
func TestPathNotFoundRealFixture(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/does/not/exist", fakeResp{Status: 404, Body: realNoPath})
	err := api.call("/v1/does/not/exist", struct{}{}, nil)
	ae, _ := err.(*APIError)
	if ae == nil || ae.Kind != KindPathNotFound || ae.Detail != "/v1/does/not/exist" {
		t.Fatalf("path-not-found çözülmedi: %#v", err)
	}
}

// 429: bekleme süresi okunur, eski uca İKİNCİ istek atılmaz.
func TestRateLimit429(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/agents/rundata", fakeResp{Status: 429, Body: `slow down`,
		Header: map[string]string{"Retry-After": "7"}})
	f.on("/agents/rundata", ok(`{"agent_id":"x"}`))

	_, err := api.RunData()
	wait, limited := RateLimited(err)
	if !limited || wait != 7*time.Second {
		t.Fatalf("429 tanınmadı: %v (%v, %v)", err, wait, limited)
	}
	if f.count("/agents/rundata") != 0 {
		t.Fatal("429'dan sonra eski uca gidildi — sınırı büyütür")
	}
	if !strings.Contains(err.Error(), "yeniden denenecek") {
		t.Fatalf("Türkçe ileti yok: %q", err.Error())
	}
}

// Bekleyen tünel: yenisi açılmaz; sonraki okumada adres gelir.
func TestPendingThenAddress(t *testing.T) {
	f, api := newFakePlayit(t)
	pending := `{"id":"t-bek","name":"MCOS Survival","tunnel_type":"minecraft-java",
		"tunnel_type_display":"Minecraft Java","port_type":"tcp","port_count":1,
		"status_msg":"allocating port"}`
	f.on("/v1/agents/rundata",
		ok(v1RunData("", pending)),
		ok(v1RunData(strings.Replace(v1JavaTunnel, `"t-java"`, `"t-bek"`, 1), "")))
	f.on("/v1/tunnels/create", ok(`{"id":"ikinci"}`))

	res, err := api.EnsureMinecraftTunnel(25565, "MCOS Survival")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !res.Tunnel.Pending || res.Tunnel.StatusMsg != "allocating port" {
		t.Fatalf("bekleyen tünel kullanılmadı: %+v", res)
	}
	res, err = api.EnsureMinecraftTunnel(25565, "MCOS Survival")
	if err != nil {
		t.Fatal(err)
	}
	if res.Tunnel.Address != "mavi-kedi.gl.joinmc.link" || res.Tunnel.Pending {
		t.Fatalf("adres gelmedi: %+v", res.Tunnel)
	}
	if f.count("/v1/tunnels/create") != 0 {
		t.Fatal("tünel beklerken ikincisi açıldı")
	}
}

// v1 rundata ayrıntıları: yerel port alan yoksa genel adresten, devre dışı
// nedeni, kritik duyuru.
func TestRunDataV1Details(t *testing.T) {
	f, api := newFakePlayit(t)
	f.on("/v1/agents/rundata", ok(`{"agent_id":"`+testAgent+`","tunnels":[
		{"id":"a","name":"tcp","display_address":"147.185.221.1:40001","port_type":"tcp",
		 "port_count":1,"tunnel_type":null,"agent_config":{"fields":[]},"disabled_reason":"AccountOverLimit"}],
		"pending":[],"notices":[
		{"priority":"Low","message":"bilgi","resolve_link":null},
		{"priority":"Critical","message":"Verify your email","resolve_link":"https://playit.gg/account"}],
		"permissions":{"is_self_managed":true,"has_premium":false,"account_status":"email-not-verified"}}`))
	rd, err := api.RunData()
	if err != nil {
		t.Fatal(err)
	}
	tn := rd.Tunnels[0]
	if tn.LocalPort != 40001 || tn.Disabled != "AccountOverLimit" || tn.TunnelType != "" {
		t.Fatalf("v1 tünel çözülmedi: %+v", tn)
	}
	m := rd.Messages()
	if len(m) != 2 || m[0] != msgVerifyEmail ||
		m[1] != "Verify your email → https://playit.gg/account" {
		t.Fatalf("duyurular yanlış: %q", m)
	}
	// Elle açılmış genel TCP tüneli aynı yerel portu tutuyorsa yeniden kullanılır.
	if _, ok := FindMinecraftTunnel(rd, 40001, "x", ""); !ok {
		t.Fatal("aynı portu tutan TCP tüneli bulunamadı")
	}
}

func TestTunnelName(t *testing.T) {
	for in, want := range map[string]string{
		"Şövalye Dünyası":                    "Sovalye Dunyasi",
		"  ":                                 "MCOS Minecraft",
		"çok  uzun bir sunucu adı ki sığmaz": "cok uzun bir sunucu adi ki sig",
		"MCOS 🎮 Yaratıcı":                    "MCOS Yaratici",
	} {
		got := TunnelName(in)
		if got != want {
			t.Errorf("TunnelName(%q) = %q, beklenen %q", in, got, want)
		}
		if len(got) > maxTunnelName {
			t.Errorf("TunnelName(%q) çok uzun: %d", in, len(got))
		}
	}
}

// playitd 1.0.10'un GERÇEK günlük satırı (ölçüldü): ANSI kodları temizlenir,
// anahtar reddi tanınır.
func TestAgentLogANSIAndKeyRejected(t *testing.T) {
	line := "\x1b[2m2026-09-27T04:08:24.728711Z\x1b[0m \x1b[33m WARN\x1b[0m \x1b[2mplayitd::daemon\x1b[0m\x1b[2m:\x1b[0m " +
		"configured agent secret is no longer valid \x1b[3merror\x1b[0m\x1b[2m=\x1b[0mApiError(Auth(InvalidAgentKey))"
	got := stripANSI(line)
	want := "2026-09-27T04:08:24.728711Z  WARN playitd::daemon: configured agent secret is no longer valid error=ApiError(Auth(InvalidAgentKey))"
	if got != want {
		t.Fatalf("ANSI temizlenmedi:\n%q\n%q", got, want)
	}
	a := &PlayitAgent{running: true}
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() { a.readLog(pr); close(done) }()
	_, _ = io.WriteString(pw, line+"\n\x1b[32mtunnel\x1b[0m mavi-kedi.gl.joinmc.link\x1b[0m ready\n")
	_ = pw.Close()
	<-done
	if !a.KeyRejected() {
		t.Fatal("anahtar reddi tanınmadı (ölü ajan çalışıyor görünür)")
	}
	if _, addr, lg := a.Status(); addr != "mavi-kedi.gl.joinmc.link" || strings.Contains(strings.Join(lg, ""), "\x1b") {
		t.Fatalf("günlük/adres temizlenmedi: %q %q", addr, lg)
	}
	if parsePlayitAddr("see https://playit.gg/account/upgrade api.playit.gg") != "" {
		t.Fatal("bağlantı/API adresi oyuncu adresi sanıldı")
	}
}

// Kayıtlı tünel kimliği BAŞKA bir yerel porta gidiyorsa (sunucu kopyalanıp
// başka porta taşındı, ya da kimlik adsız eşleşmeyle yanlış sunucuya yazıldı)
// o tünel bu sunucunun tüneli SAYILMAZ: saymak, 25570'teki sunucunun
// adresi diye 25565'e giden adresi göstermek ve 25570 için hiç tünel
// açmamak demekti. Adsız arama da "MCOS Minecraft" adlı bekleyen varsayılan
// tüneli kapmamalı (TunnelName("") = "MCOS Minecraft").
func TestFindKnownIDOnOtherPortAndNamelessPending(t *testing.T) {
	rd := &PlayitRunData{AgentID: testAgent,
		Tunnels: []PlayitTunnel{{ID: "t-25565", TunnelType: TunnelTypeMinecraftJava,
			Address: "eski.gl.joinmc.link", LocalIP: "127.0.0.1", LocalPort: 25565}},
		Pending: []PlayitTunnel{{ID: "p-def", Name: "MCOS Minecraft",
			TunnelType: TunnelTypeMinecraftJava, Pending: true}}}

	if tn, ok := FindMinecraftTunnel(rd, 25570, "MCOS Yaratici", "t-25565"); ok {
		t.Fatalf("başka porta giden kayıtlı tünel yeniden kullanıldı: %+v", tn)
	}
	// Aynı portta kimlikle eşleşme sürmeli.
	if tn, ok := FindMinecraftTunnel(rd, 25565, "", "t-25565"); !ok || tn.ID != "t-25565" {
		t.Fatalf("aynı porttaki kayıtlı tünel bulunamadı: %+v %v", tn, ok)
	}
	// Bekleyen tünelin yerel portu bilinmez: kimlikle eşleşme sürmeli.
	if tn, ok := FindMinecraftTunnel(rd, 25570, "", "p-def"); !ok || tn.ID != "p-def" {
		t.Fatalf("bekleyen tünel kimliğiyle bulunamadı: %+v %v", tn, ok)
	}
	if tn, ok := FindMinecraftTunnel(rd, 25570, "", ""); ok {
		t.Fatalf("adsız arama bekleyen varsayılan tüneli kaptı: %+v", tn)
	}
	// Adıyla istenen bekleyen tünel yine bulunur (TestPendingThenAddress'in yolu).
	if _, ok := FindMinecraftTunnel(rd, 25565, "MCOS Minecraft", ""); !ok {
		t.Fatal("adıyla istenen bekleyen tünel bulunamadı")
	}

	f, api := newFakePlayit(t)
	f.on("/v1/tunnels/create", ok(`{"id":"t-25570"}`))
	res, err := api.EnsureMinecraftTunnelIn(rd, 25570, "MCOS Yaratici", "t-25565")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Tunnel.ID != "t-25570" {
		t.Fatalf("25570 için tünel açılmadı: %+v", res)
	}
}

// Elle açma ipucu SUNUCUNUN portunu söyler: 25570'teki sunucu için
// "127.0.0.1:25565" yazmak, kullanıcıya yanlış tüneli açtırırdı.
func TestManualHintUsesPort(t *testing.T) {
	err := &APIError{Kind: KindAuth, Code: "ScopeNotAllowed"}
	if m := MessageForPort(err, 25570); !strings.Contains(m, "127.0.0.1:25570") ||
		strings.Contains(m, "25565") {
		t.Fatalf("ipucu sunucunun portunu söylemiyor: %q", m)
	}
	if m := MessageForPort(err, 0); !strings.Contains(m, "127.0.0.1:25565") {
		t.Fatalf("port bilinmezken varsayılan yazılmalı: %q", m)
	}
	if m := MessageForPort(&APIError{Kind: KindFail, Code: "RequiresVerifiedAccount"}, 25570); m != msgVerifyEmail {
		t.Fatalf("ipucu olmayan ileti değişti: %q", m)
	}
	if MessageForPort(nil, 25570) != "" {
		t.Fatal("hatasız ileti boş olmalı")
	}
}
