package remote

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mcos/internal/ipc"
)

// ════════════════════════════════════════════════════════════════════════════
// UZAKTAN ERİŞİM: JETONSUZ HİÇBİR ŞEY
// ════════════════════════════════════════════════════════════════════════════
//
// Bu köprü, sunucunun TAM DENETİMİNİ ağa açar: sunucu silme, dosya yazma,
// makineyi kapatma. Kimlik denetiminde tek bir boşluk, aynı Wi-Fi'deki
// herkesin (ya da tünel açıksa internetin) sistemi ele geçirmesi demektir.
//
// Testler o yüzden "çalışıyor mu" değil, "İZİNSİZ geçilebiliyor mu" diye
// soruyor.

// fakeDispatcher counts how many requests actually reached the RPC layer.
type fakeDispatcher struct{ calls atomic.Int64 }

func (f *fakeDispatcher) Dispatch(_ context.Context, line []byte) ipc.Response {
	f.calls.Add(1)
	var req ipc.Request
	_ = json.Unmarshal(line, &req)
	res, _ := json.Marshal(map[string]string{"echo": req.Method})
	return ipc.Response{JSONRPC: ipc.Version, ID: req.ID, Result: res}
}

// rpcBody, yikici bir komut tasiyan ornek bir istek.
const rpcBody = `{"jsonrpc":"2.0","id":1,"method":"system.power"}`

const testToken = "0123456789abcdef0123456789abcdef"

func newTestServer(t *testing.T) (*Server, *fakeDispatcher, string) {
	t.Helper()
	d := &fakeDispatcher{}
	s, err := New(d, Options{
		Addr:    "127.0.0.1:0", // çekirdek boş port seçsin
		Token:   testToken,
		CertDir: t.TempDir(),
		Name:    "test-mcos",
		Version: "1.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Dinleyiciyi elle açıp Serve'e veriyoruz ki bağlandığımız portu bilelim.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	go func() { _ = s.http.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = s.Close() })

	return s, d, "https://" + ln.Addr().String()
}

// testClient trusts any certificate: sertifika doğrulaması ayrı bir testin
// konusu, burada kimlik denetimini sınıyoruz.
func testClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test
		},
	}
}

func post(t *testing.T, base, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/rpc", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := testClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestNoTokenNoAccess — ASIL DEĞİŞMEZ.
func TestNoTokenNoAccess(t *testing.T) {
	_, d, base := newTestServer(t)

	cases := []struct {
		name  string
		token string
	}{
		{"jeton yok", ""},
		{"boş jeton", " "},
		{"yanlış jeton", "yanlis-jeton"},
		{"doğru jetonun öneki", testToken[:16]},
		{"doğru jeton + fazladan karakter", testToken + "x"},
		{"büyük harfli", strings.ToUpper(testToken)},
	}
	for _, c := range cases {
		status, _ := post(t, base, c.token, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		if status != http.StatusUnauthorized {
			t.Errorf("%s: durum %d, 401 bekleniyordu — YETKİSİZ ERİŞİM",
				c.name, status)
		}
	}

	if n := d.calls.Load(); n != 0 {
		t.Fatalf("yetkisiz istekler RPC katmanına ULAŞTI (%d kez) — "+
			"kimlik denetimi dağıtımdan sonra yapılıyor olmalı", n)
	}
}

// Doğru jetonla istek geçmeli ve gerçekten dağıtılmalı.
func TestValidTokenReachesRPC(t *testing.T) {
	_, d, base := newTestServer(t)

	status, body := post(t, base, testToken,
		`{"jsonrpc":"2.0","id":7,"method":"system.status"}`)
	if status != http.StatusOK {
		t.Fatalf("durum %d: %s", status, body)
	}
	if d.calls.Load() != 1 {
		t.Fatalf("RPC katmanına ulaşmadı")
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("yanıt JSON değil: %v (%s)", err, body)
	}
	if resp.ID != 7 {
		t.Errorf("istek kimliği korunmadı: %d", resp.ID)
	}
	if !strings.Contains(string(resp.Result), "system.status") {
		t.Errorf("yanlış yöntem dağıtıldı: %s", resp.Result)
	}
}

// X-MCOS-Token başlığı da kabul edilmeli (kabuk betiklerinden çağırmak için).
func TestAlternateHeaderWorks(t *testing.T) {
	_, d, base := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost, base+"/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("X-MCOS-Token", testToken)
	resp, err := testClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("durum %d", resp.StatusCode)
	}
	if d.calls.Load() != 1 {
		t.Error("istek dağıtılmadı")
	}
}

// Jetonsuz bir sunucu HİÇ kurulmamalı.
//
// "Kapalı" ile "herkese açık" arasında kaza eseri geçiş olmamalı: boş jeton
// sessizce "kimlik denetimi yok" anlamına gelseydi, bir yapılandırma hatası
// sistemi ağa açardı.
func TestEmptyTokenRefusesToStart(t *testing.T) {
	for _, tok := range []string{"", "   ", "\t"} {
		if _, err := New(&fakeDispatcher{}, Options{
			Token: tok, CertDir: t.TempDir(),
		}); err == nil {
			t.Errorf("%q jetonuyla sunucu kuruldu — ağa açık kalırdı", tok)
		}
	}
}

// /health kimlik istemez ama HİÇBİR hassas bilgi vermemeli.
func TestHealthLeaksNothing(t *testing.T) {
	s, _, base := newTestServer(t)

	resp, err := testClient().Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("durum %d", resp.StatusCode)
	}
	body := string(b)
	if strings.Contains(body, testToken) {
		t.Fatal("JETON /health yanıtında sızdırıldı")
	}

	var h healthReply
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("yanıt JSON değil: %v", err)
	}
	if h.Service != "mcos" {
		t.Errorf("service = %q", h.Service)
	}
	if h.Fingerprint != s.Fingerprint() {
		t.Errorf("parmak izi uyuşmuyor:\n  %s\n  %s", h.Fingerprint, s.Fingerprint())
	}
	// Parmak izi biçimi kullanıcıya gösterilecek: iki nokta ile ayrılmış,
	// 32 bayt.
	if n := strings.Count(h.Fingerprint, ":"); n != 31 {
		t.Errorf("parmak izi biçimi bozuk (%d iki nokta): %s", n, h.Fingerprint)
	}
}

// Çok büyük gövde reddedilmeli.
func TestOversizedBodyRejected(t *testing.T) {
	_, d, base := newTestServer(t)

	huge := `{"jsonrpc":"2.0","id":1,"method":"x","params":"` +
		strings.Repeat("A", maxBodyBytes+100) + `"}`
	status, _ := post(t, base, testToken, huge)
	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("durum %d, 413 bekleniyordu", status)
	}
	if d.calls.Load() != 0 {
		t.Error("aşırı büyük gövde RPC katmanına ulaştı")
	}
}

// GET /rpc olmamalı: yalnızca POST.
func TestRPCRejectsGet(t *testing.T) {
	_, _, base := newTestServer(t)
	resp, err := testClient().Get(base + "/rpc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("durum %d, 405 bekleniyordu", resp.StatusCode)
	}
}

// ── Sertifika ───────────────────────────────────────────────────────────────

// Sertifika KALICI olmalı: her açılışta yenisi üretilirse telefondaki
// parmak izi geçersizleşir ve kullanıcı her seferinde "araya giren var"
// uyarısı alır.
func TestCertificateIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.fingerprint != second.fingerprint {
		t.Errorf("sertifika yeniden üretildi:\n  %s\n  %s",
			first.fingerprint, second.fingerprint)
	}
	if first.fingerprint == "" {
		t.Error("parmak izi boş")
	}
}

// Farklı klasörler farklı sertifika üretmeli (aynı anahtar iki cihazda
// kullanılmamalı).
func TestCertificatesAreUnique(t *testing.T) {
	a, err := loadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a.fingerprint == b.fingerprint {
		t.Error("iki ayrı cihaz aynı sertifikayı aldı")
	}
}

// TLS gerçekten konuşuluyor olmalı: düz HTTP ile HİÇBİR ŞEY yapılamamalı.
//
// ── Neden "hata döner" diye yazılmıyor ──────────────────────────────────────
// Go'nun HTTPS sunucusu, düz metin bir isteği sessizce düşürmek yerine
// nazikçe 400 ve "client sent an HTTP request to an HTTPS server" yanıtı
// verir. Yani istemci tarafında bağlantı hatası OLMAYABİLİR.
//
// Asıl değişmez o değil: düz metin bir istek SERVİS EDİLMEMELİ. Yani ne 2xx
// dönmeli ne de RPC katmanına ulaşmalı — jeton hiçbir koşulda açık metin bir
// bağlantı üzerinden işlenmemeli.
func TestPlainHTTPIsNotServed(t *testing.T) {
	_, d, base := newTestServer(t)
	plain := strings.Replace(base, "https://", "http://", 1)

	req, _ := http.NewRequest(http.MethodPost, plain+"/rpc",
		strings.NewReader(rpcBody))
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err == nil {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 == 2 {
			t.Fatalf("düz HTTP isteği SERVİS EDİLDİ (durum %d): %s — jeton "+
				"açık metin gidiyor olurdu", resp.StatusCode, string(b))
		}
		if strings.Contains(string(b), "jsonrpc") {
			t.Fatalf("düz HTTP üzerinden JSON-RPC yanıtı döndü: %s", string(b))
		}
	}

	// Asıl denetim: komut RPC katmanına ULAŞMAMIŞ olmalı.
	if n := d.calls.Load(); n != 0 {
		t.Fatalf("düz metin istek RPC katmanına ulaştı (%d kez) — "+
			"şifresiz bağlantıda komut çalıştırılıyor", n)
	}
}

// Yavaşlatma, art arda başarısız denemelerde devreye girmeli.
func TestRepeatedFailuresAreSlowedDown(t *testing.T) {
	l := newAuthLimiter()
	const ip = "10.0.0.9"

	// İlk birkaç hata cezasız: kullanıcı jetonu yanlış yapıştırmış olabilir.
	for i := 0; i < 3; i++ {
		if d := l.penalty(ip); d != 0 {
			t.Errorf("%d. denemede ceza var (%v) — kullanıcıyı cezalandırır", i+1, d)
		}
		l.fail(ip)
	}
	if d := l.penalty(ip); d <= 0 {
		t.Error("art arda hatalardan sonra yavaşlatma yok")
	}

	// Ceza sınırlı olmalı: unutulmuş bir saldırgan gerçek kullanıcıyı
	// dakikalarca dışarıda bırakmamalı.
	for i := 0; i < 200; i++ {
		l.fail(ip)
	}
	if d := l.penalty(ip); d > maxPenalty {
		t.Errorf("ceza %v — üst sınır %v olmalıydı", d, maxPenalty)
	}

	// Başarılı giriş sayacı sıfırlamalı.
	l.ok(ip)
	if d := l.penalty(ip); d != 0 {
		t.Errorf("başarılı girişten sonra ceza sürüyor: %v", d)
	}
}

// Yavaşlatma haritası sonsuza dek büyümemeli.
func TestLimiterForgetsOldEntries(t *testing.T) {
	l := newAuthLimiter()
	for i := 0; i < 100; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		l.fail(ip)
		// Eski gibi görünmesi için zamanı geri al.
		l.mu.Lock()
		l.seen[ip] = time.Now().Add(-time.Hour)
		l.mu.Unlock()
	}
	l.fail("10.0.1.1") // expire() burada çalışır

	l.mu.Lock()
	n := len(l.fails)
	l.mu.Unlock()
	if n > 5 {
		t.Errorf("%d giriş bellekte kaldı — harita yalnızca büyüyor", n)
	}
}
