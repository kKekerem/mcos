package main

import (
	"testing"

	"mcos/internal/model"
)

// Kurucunun oyun kuralları PC'deki kopyaya geçmeli. İki sanal makineli
// sınamada kurucu online-mode=false iken kopya true doğdu; sınırı geçen
// oyuncu "unverified_username" ile atıldı.
func TestApplyLinkSpecCarriesRules(t *testing.T) {
	h, _ := newTestHost(t)
	spec := baseSpec()
	spec.Rules = &model.LinkRules{OnlineMode: false, PVP: false, Gamemode: "creative", MaxPlayers: 500}
	if _, _, err := h.ApplyLinkSpec(spec); err != nil {
		t.Fatal(err)
	}
	list, _ := h.st.ListServers()
	if len(list) != 1 {
		t.Fatalf("%d sunucu", len(list))
	}
	srv := list[0]
	if srv.OnlineMode || srv.PVP || srv.Gamemode != "creative" || srv.MaxPlayers != 500 {
		t.Fatalf("kurallar kayda geçmedi: online=%v pvp=%v kip=%q oyuncu=%d",
			srv.OnlineMode, srv.PVP, srv.Gamemode, srv.MaxPlayers)
	}
	if !srv.Link.Rules.Equal(spec.Rules) {
		t.Fatalf("Link.Rules saklanmadı (her açılışta yazılamaz): %+v", srv.Link.Rules)
	}
}

// KARŞI-SINAMA: kural göndermeyen (eski) bir kurucu, kopyanın mevcut
// değerlerini bozmamalı.
func TestApplyLinkSpecWithoutRulesKeepsDefaults(t *testing.T) {
	h, _ := newTestHost(t)
	if _, _, err := h.ApplyLinkSpec(baseSpec()); err != nil {
		t.Fatal(err)
	}
	list, _ := h.st.ListServers()
	if !list[0].OnlineMode || !list[0].PVP || list[0].Link.Rules != nil {
		t.Fatalf("kural yokken öntanımlılar bozuldu: %+v", list[0])
	}
}
