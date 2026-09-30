package daemon

import (
	"context"
	"errors"
	"os"
	"testing"

	"mcos/internal/model"
	"mcos/internal/proxy"
)

// TestMain: paketin hiçbir sınaması Velocity indirmek için ağa çıkmasın
// (ortak dünya kuran sınamalar ensureOriginProxy'ye uğrar).
func TestMain(m *testing.M) {
	resolveVelocity = func(context.Context) (proxy.Jar, error) {
		return proxy.Jar{}, errors.New("sınamada Velocity yok")
	}
	fetchFabricProxy = func(*Daemon, context.Context, string, string) (string, error) {
		return "", errors.New("sınamada ağ yok")
	}
	os.Exit(m.Run())
}

// Proxy açılınca ana sunucu genel portu Velocity'ye bırakıp 25580+'e geçer;
// kapanınca eski portuna döner.
func TestMoveBehindProxyAndRestore(t *testing.T) {
	main := &model.Server{ID: "m", Port: 25565}
	sib := &model.Server{ID: "s", ParentID: "m", Port: 25580} // 25580 dolu
	list := []*model.Server{main, sib}
	all := func(int) bool { return true }

	if err := moveBehindProxy(main, list, "k", true, all); err != nil {
		t.Fatal(err)
	}
	if main.Port != 25581 || main.Link.Proxy == nil || main.Link.Proxy.PublicPort != 25565 ||
		main.Link.Proxy.Secret != "k" || !main.Link.Proxy.OnlineMode {
		t.Fatalf("taşıma yanlış: port %d, proxy %+v", main.Port, main.Link.Proxy)
	}
	// İkinci kez: port SABİT kalır, yalnızca anahtar tazelenir.
	if err := moveBehindProxy(main, list, "k2", true, all); err != nil {
		t.Fatal(err)
	}
	if main.Port != 25581 || main.Link.Proxy.Secret != "k2" {
		t.Fatalf("yeniden uygulama portu değiştirmemeli: %d %+v", main.Port, main.Link.Proxy)
	}
	if !restoreFromProxy(main) || main.Port != 25565 || main.Link.Proxy != nil {
		t.Fatalf("geri alma yanlış: port %d, proxy %+v", main.Port, main.Link.Proxy)
	}
	if restoreFromProxy(main) {
		t.Fatal("proxy yokken geri alma bir şey değiştirmemeli")
	}
}

func TestProxyBackendsFromTopology(t *testing.T) {
	nodes := []model.LinkNode{
		{Name: "MCOS", Backend: "mcos", Self: true, Local: true, Host: "192.168.1.5", MCPort: 25581},
		{Name: "MCOS-2", Backend: "mcos-2", Local: true, Host: "192.168.1.5", MCPort: 25566},
		{Name: "PC B", Backend: "pc-b", Host: "192.168.1.20", MCPort: 25565},
		{Name: "ölü", Backend: "-l-", Host: "", MCPort: 25565},
	}
	got, try := proxyBackends(nodes, 25581, "MCOS")
	want := []string{"mcos=127.0.0.1:25581", "mcos-2=127.0.0.1:25566", "pc-b=192.168.1.20:25565"}
	if len(got) != len(want) || len(try) != 1 || try[0] != "mcos" {
		t.Fatalf("arka uçlar %v, try %v", got, try)
	}
	for i, b := range got {
		if b.Name+"="+b.Addr != want[i] {
			t.Errorf("%d: %s=%s, beklenen %s", i, b.Name, b.Addr, want[i])
		}
	}
	// Topoloji yoksa en azından bu makine.
	got, try = proxyBackends(nil, 25581, "Mcos Kutu")
	if len(got) != 1 || got[0].Name != "mcos-kutu" || try[0] != "mcos-kutu" {
		t.Fatalf("koordinatörsüz: %v %v", got, try)
	}
}

// /link/proxy listesi velocity.toml'unkiyle AYNI adları ve adresleri taşır;
// eklenti host/port'u ayrı alır (IPv6 ayraçları Java'da ayrıştırılmasın).
func TestProxyListOf(t *testing.T) {
	backends := []proxy.Backend{
		{Name: "mcos-kutu", Addr: "127.0.0.1:25581"},
		{Name: "pc-b", Addr: "192.168.1.57:25565"},
		{Name: "v6", Addr: "[fe80::1]:25566"},
		{Name: "bozuk", Addr: "adres-yok"},
	}
	pl := proxyListOf(backends, []string{"mcos-kutu"}, 10)
	if pl.Try != "mcos-kutu" || len(pl.Backends) != 3 || pl.MaxPlayers != 30 {
		t.Fatalf("liste %+v", pl)
	}
	if b := pl.Backends[1]; b.Name != "pc-b" || b.Host != "192.168.1.57" || b.Port != 25565 {
		t.Errorf("pc-b: %+v", b)
	}
	if b := pl.Backends[2]; b.Host != "fe80::1" || b.Port != 25566 {
		t.Errorf("ipv6: %+v", b)
	}
	if pl := proxyListOf(nil, nil, 0); pl.Backends == nil || pl.Try != "" {
		t.Errorf("boş liste null olmamalı: %+v", pl)
	}
}

// Eklentiyle velocity.toml yalnızca "try" sunucusunu taşır ve yeni bir PC
// eklenince DEĞİŞMEZ: değişseydi Runner.Apply Velocity'yi yeniden başlatır,
// bağlı herkes düşerdi.
func TestProxyConfStableWithPlugin(t *testing.T) {
	self := proxy.Backend{Name: "mcos", Addr: "127.0.0.1:25581"}
	pcb := proxy.Backend{Name: "pc-b", Addr: "192.168.1.20:25565"}
	try := []string{"mcos"}
	render := func(bs []proxy.Backend, plugin bool) string {
		listed, maxp := proxyConfBackends(bs, try, 20, plugin)
		return proxy.Render(proxy.Config{Bind: "0.0.0.0:25565", MaxPlayers: maxp, Backends: listed, Try: try})
	}
	if render([]proxy.Backend{self}, true) != render([]proxy.Backend{self, pcb}, true) {
		t.Error("eklentiyle yeni PC velocity.toml'u değiştirdi (yeniden başlatma = herkes düşer)")
	}
	// Eklentisiz (yedek yol) liste dosyada olmalı.
	if render([]proxy.Backend{self}, false) == render([]proxy.Backend{self, pcb}, false) {
		t.Error("eklentisiz yolda yeni PC velocity.toml'a yazılmadı")
	}
	// try listede yoksa (olmamalı) tam liste yazılır: Velocity sunucusuz açılamaz.
	if listed, _ := proxyConfBackends([]proxy.Backend{pcb}, try, 20, true); len(listed) != 1 || listed[0].Name != "pc-b" {
		t.Errorf("try yokken liste %v", listed)
	}
}
