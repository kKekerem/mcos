package netprobe

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// VirtualBox'ın NAT'ı gibi bağlantıyı KABUL edip dışarıya ulaşmayan bir
// katman: TCP el sıkışması başarılı, sonra hiçbir şey. Eskiden bu "internet
// var" sayılıyordu (kullanıcı: "internete bağlı olmamasına rağmen bağlı diyor").
func TestSahteKabulEdenKatmanInternetSayilmaz(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("dinleyici açılamadı")
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Hiç TLS konuşmadan kapat (NAT'ın dışarıya ulaşamadığı an).
			c.Close()
		}
	}()
	if tlsUlasiyor(ln.Addr().String()) {
		t.Fatal("TLS konuşmayan (sahte kabul eden) katman internet sayıldı")
	}
}

func TestGercekTLSSunucusuUlasilabilir(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	adres := strings.TrimPrefix(srv.URL, "https://")
	if !tlsUlasiyor(adres) {
		t.Fatal("gerçek bir TLS sunucusu ulaşılamaz sayıldı")
	}
}
