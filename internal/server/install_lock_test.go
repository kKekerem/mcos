package server

import (
	"context"
	"testing"
	"time"

	"mcos/internal/model"
)

// server.install, server.create'in arka plan kurulumuyla AYNI kilidi almalı.
// Almazsa iki kurulumcu aynı klasöre aynı jar'ı aynı anda yazar (uçtan uca
// sınamada ölçüldü: "indiriliyor" satırı 2 ms arayla iki kez).
func TestAcikKurulumArkaPlanKurulumunuBekler(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)
	srv := &model.Server{ID: "kilit-sinama", Software: "boyle-bir-yazilim-yok"}

	r := m.rt(srv.ID)
	r.installMu.Lock() // EnsureInstalled'ın kurulum sırasında tuttuğu kilit

	bitti := make(chan error, 1)
	go func() { bitti <- m.InstallExclusive(context.Background(), srv) }()

	select {
	case <-bitti:
		t.Fatal("arka plan kurulumu sürerken açık kurulum başladı (kilit alınmıyor)")
	case <-time.After(150 * time.Millisecond):
	}

	r.installMu.Unlock()
	select {
	case err := <-bitti:
		// Sağlayıcı yok: kurulum kilidi aldıktan sonra hemen hata döner.
		if err == nil {
			t.Fatal("olmayan yazılım için hata beklenirdi")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kilit bırakıldıktan sonra açık kurulum çalışmadı")
	}
}
