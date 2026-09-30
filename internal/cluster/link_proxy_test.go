package cluster

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /link/proxy: Velocity eklentisinin okuduğu sözleşme. Alan adları değişirse
// eklenti listeyi okuyamaz ve yeni PC'ler proxy'ye hiç eklenmez.
func TestHandleProxyContract(t *testing.T) {
	c := NewLinkCoordinator(nil, nil)
	c.SetProxySource(func() (ProxyList, bool) {
		return ProxyList{Backends: []ProxyServer{{Name: "pc-b", Host: "192.168.1.57", Port: 25565}},
			Try: "mcos-kutu"}, true
	})
	rec := httptest.NewRecorder()
	c.handleProxy(rec, httptest.NewRequest(http.MethodGet, "/link/proxy", nil))
	want := `{"backends":[{"name":"pc-b","host":"192.168.1.57","port":25565}],"try":"mcos-kutu"}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("yanıt %d %s\nbeklenen %s", rec.Code, rec.Body.String(), want)
	}
}

// Proxy kapalıyken 503 ve "[]": eklenti o turu atlar, elindeki listeyi
// silmez (boş liste herkesin arka ucunu silerdi).
func TestHandleProxyUnavailable(t *testing.T) {
	for _, c := range []*LinkCoordinator{NewLinkCoordinator(nil, nil), func() *LinkCoordinator {
		c := NewLinkCoordinator(nil, nil)
		c.SetProxySource(func() (ProxyList, bool) { return ProxyList{}, false })
		return c
	}()} {
		rec := httptest.NewRecorder()
		c.handleProxy(rec, httptest.NewRequest(http.MethodGet, "/link/proxy", nil))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"backends":[]`) {
			t.Fatalf("kapalı proxy: %d %s", rec.Code, rec.Body.String())
		}
	}
}
