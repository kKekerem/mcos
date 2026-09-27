package deskgui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Arayüz ayar değiştiren (eşleştirme anahtarı) ve mcos-flash'ta DİSK SİLEN
// uçlar sunuyor. Bu sınamalar, belirteçsiz ya da yanlış belirteçli hiçbir
// isteğin bir işleyiciye ULAŞMADIĞINI doğrular: işleyici çağrılırsa sayaç
// artar ve sınama düşer.

func newTestServer(t *testing.T) (*Server, *int) {
	t.Helper()
	s, err := New(Page{Title: "Sınama", Body: "<p id=govde>merhaba</p>", Script: "var x=1;"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.ln.Close() })
	calls := 0
	s.Get("/api/oku", func(*http.Request) (any, error) {
		calls++
		return map[string]any{"deger": 42}, nil
	})
	s.Post("/api/yaz", func(r *http.Request) (any, error) {
		calls++
		var req struct {
			A string `json:"a"`
		}
		if err := ReadJSON(r, &req); err != nil {
			return nil, err
		}
		if req.A == "" {
			return nil, Fail(0, "a boş")
		}
		return nil, nil
	})
	return s, &calls
}

func do(s *Server, method, target, token, body, host string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, rd)
	r.Host = host
	if host == "" {
		r.Host = s.host
	}
	if token != "" {
		r.Header.Set(TokenHeader, token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestAPIBelirtecsizReddedilir(t *testing.T) {
	s, calls := newTestServer(t)
	for _, c := range []struct {
		ad, method, yol, belirtec string
	}{
		{"belirteçsiz okuma", http.MethodGet, "/api/oku", ""},
		{"yanlış belirteçle okuma", http.MethodGet, "/api/oku", strings.Repeat("0", 64)},
		{"belirteçsiz yazma", http.MethodPost, "/api/yaz", ""},
		{"yanlış belirteçle yazma", http.MethodPost, "/api/yaz", s.token[:63] + "x"},
		// Belirteç sorgu dizesinde gelirse de KABUL EDİLMEZ: API yalnızca
		// başlığa bakar (başka sitenin <img src> ile tetikleyebileceği tek
		// yol sorgu dizesidir).
		{"sorgu dizesinde belirteç", http.MethodGet, "/api/oku?t=" + s.token, ""},
	} {
		w := do(s, c.method, c.yol, c.belirtec, `{"a":"b"}`, "")
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: kod %d, 403 bekleniyordu", c.ad, w.Code)
		}
	}
	if *calls != 0 {
		t.Fatalf("belirteçsiz istekler işleyiciye ulaştı (%d çağrı)", *calls)
	}
}

func TestAPIDogruBelirtecleCalisir(t *testing.T) {
	s, calls := newTestServer(t)
	w := do(s, http.MethodGet, "/api/oku", s.token, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("kod %d: %s", w.Code, w.Body)
	}
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v["deger"] != float64(42) {
		t.Fatalf("yanıt beklenmedik: %s", w.Body)
	}
	w = do(s, http.MethodPost, "/api/yaz", s.token, `{"a":"b"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("yazma kodu %d: %s", w.Code, w.Body)
	}
	if *calls != 2 {
		t.Fatalf("2 çağrı bekleniyordu, %d", *calls)
	}
	if s.LastSeen().IsZero() {
		t.Fatal("doğrulanmış istek LastSeen'i güncellemedi (tarayıcı kipi hiç kapanmazdı)")
	}
}

func TestAPIYontemVeHataKodlari(t *testing.T) {
	s, _ := newTestServer(t)
	// Değiştiren uç GET ile çağrılamaz.
	if w := do(s, http.MethodGet, "/api/yaz", s.token, "", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/yaz: kod %d, 405 bekleniyordu", w.Code)
	}
	// İşleyicinin Fail'i 400 ve Türkçe ileti olarak döner.
	w := do(s, http.MethodPost, "/api/yaz", s.token, `{"a":""}`, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "a boş") {
		t.Errorf("Fail: kod %d gövde %s", w.Code, w.Body)
	}
	// Bozuk JSON 400.
	if w := do(s, http.MethodPost, "/api/yaz", s.token, `{bozuk`, ""); w.Code != http.StatusBadRequest {
		t.Errorf("bozuk JSON: kod %d", w.Code)
	}
}

func TestSayfaBelirtecIster(t *testing.T) {
	s, _ := newTestServer(t)
	if w := do(s, http.MethodGet, "/", "", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("belirteçsiz sayfa: kod %d, 403 bekleniyordu", w.Code)
	}
	if w := do(s, http.MethodGet, "/?t=yanlis", "", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("yanlış belirteçli sayfa: kod %d", w.Code)
	}
	w := do(s, http.MethodGet, "/?t="+s.token, "", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("sayfa: kod %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`content="` + s.token + `"`, "<p id=govde>merhaba</p>", "var x=1;", "--accent: #23A99C"} {
		if !strings.Contains(body, want) {
			t.Errorf("sayfada %q yok", want)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP eksik: %q", csp)
	}
}

// DNS yeniden bağlama: saldırganın alan adı 127.0.0.1'e çözülse bile Host
// başlığı onun adını taşır ve reddedilmelidir — belirteç doğru olsa bile.
func TestYabanciHostReddedilir(t *testing.T) {
	s, calls := newTestServer(t)
	for _, h := range []string{"kotu.example:" + itoa(s.port), "127.0.0.1:1", "192.168.1.5:" + itoa(s.port)} {
		if w := do(s, http.MethodGet, "/api/oku", s.token, "", h); w.Code != http.StatusForbidden {
			t.Errorf("Host %q: kod %d, 403 bekleniyordu", h, w.Code)
		}
	}
	if *calls != 0 {
		t.Fatal("yabancı Host işleyiciye ulaştı")
	}
	if w := do(s, http.MethodGet, "/api/oku", s.token, "", "localhost:"+itoa(s.port)); w.Code != http.StatusOK {
		t.Errorf("localhost:port kabul edilmedi: %d", w.Code)
	}
}

// Dinleyici YALNIZCA geri döngüde: ağdaki başka makine ulaşamamalı.
func TestYalnizcaGeriDongu(t *testing.T) {
	s, _ := newTestServer(t)
	if a := s.ln.Addr().String(); !strings.HasPrefix(a, "127.0.0.1:") {
		t.Fatalf("dinleyici %s adresinde; yalnızca 127.0.0.1 olmalı", a)
	}
	if !strings.HasPrefix(s.URL(), "http://127.0.0.1:") || !strings.HasSuffix(s.URL(), "?t="+s.token) {
		t.Fatalf("adres beklenmedik: %s", s.URL())
	}
	s2, _ := newTestServer(t)
	if s2.token == s.token {
		t.Fatal("iki sunucu aynı belirteci üretti")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
