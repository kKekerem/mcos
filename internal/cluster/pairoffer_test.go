package cluster

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Kullanıcının istediği akış: düğümde ANAHTAR YOK; MCOS ağda bulduğu düğümü
// seçer, iki tarafta AYNI kod çıkar, düğümde "Kabul et", MCOS'ta onay ->
// anahtar düğüme gider ve eşleşme gerçek anahtarla doğrulanır.
func TestKodlaEslestirmeAnahtarsizDugum(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", "", true) // anahtar YOK
	saklanan := ""
	pc.m.EnablePairOffers(func(key string, _ InOffer) error { saklanan = key; return nil })

	id := discover(t, kutu, pc)
	ctx := context.Background()
	kod, err := kutu.m.PairOffer(ctx, id)
	if err != nil {
		t.Fatalf("teklif: %v", err)
	}
	offers := pc.m.Offers()
	if len(offers) != 1 {
		t.Fatalf("düğümde %d teklif var, 1 bekleniyordu", len(offers))
	}
	if offers[0].Code != kod {
		t.Fatalf("iki ekranın kodu farklı: MCOS %q, düğüm %q", kod, offers[0].Code)
	}
	if offers[0].Name != "mcos-kutu" {
		t.Fatalf("teklifte MCOS'un adı %q", offers[0].Name)
	}

	// Düğümde henüz kabul yok: anahtar GÖNDERİLSE bile saklanmamalı.
	if st, _ := kutu.m.PairConfirm(ctx, id); st != PairWaiting {
		t.Fatalf("kabul öncesi durum %q, %q bekleniyordu", st, PairWaiting)
	}
	if saklanan != "" || pc.m.Secret() != "" {
		t.Fatal("kabul edilmeden anahtar saklandı")
	}

	if err := pc.m.DecideOffer(offers[0].ID, true); err != nil {
		t.Fatal(err)
	}
	st, err := kutu.m.PairConfirm(ctx, id)
	if err != nil || st != PairDone {
		t.Fatalf("onay sonrası durum %q (%v)", st, err)
	}
	if saklanan != testKey || pc.m.Secret() != testKey {
		t.Fatalf("düğüm anahtarı almadı: saklanan=%q secret=%q", saklanan, pc.m.Secret())
	}
	// Eşleşme gerçek anahtarla doğrulandı (hello): düğüm MCOS'u kaydetmeli.
	waitFor(t, "düğüm MCOS'u eşleşmiş kaydetsin", func() bool {
		p, ok := peerByID(pc.m, "id:"+kutu.m.NodeID())
		return ok && p.Paired
	})
	waitFor(t, "MCOS tarafında sorun kalmasın", func() bool {
		q, _ := peerByID(kutu.m, id)
		return q.Paired && q.Problem == ""
	})
}

// Düğümde "Reddet": anahtar asla gitmez, MCOS reddi öğrenir.
func TestKodlaEslestirmeReddedilir(t *testing.T) {
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	pc := newTestNode(t, "ikinci-pc", "", true)
	pc.m.EnablePairOffers(func(string, InOffer) error { return nil })
	id := discover(t, kutu, pc)
	if _, err := kutu.m.PairOffer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	_ = pc.m.DecideOffer(pc.m.Offers()[0].ID, false)
	st, _ := kutu.m.PairConfirm(context.Background(), id)
	if st != PairRejected || pc.m.Secret() != "" {
		t.Fatalf("red sonrası durum %q, düğüm anahtarı %q", st, pc.m.Secret())
	}
}

// Kod eşleştirmesini desteklemeyen karşı taraf (başka bir MCOS: teklif kabul
// etmez) açıkça bildirilir; panel eski yola düşebilsin.
func TestKodlaEslestirmeDesteklenmiyor(t *testing.T) {
	a := newTestNode(t, "mcos-a", testKey, false)
	b := newTestNode(t, "mcos-b", testKey, false) // EnablePairOffers YOK
	id := discover(t, a, b)
	if _, err := a.m.PairOffer(context.Background(), id); !errors.Is(err, ErrPairUnsupported) {
		t.Fatalf("desteklenmiyor hatası beklenirdi: %v", err)
	}
}

// Aradaki biri: düğümün gördüğü teklif başka bir sırla kurulmuşsa (MITM),
// MCOS'un mühürlediği anahtar düğümde AÇILMAMALI.
func TestYanlisSirlaMuhurAcilmaz(t *testing.T) {
	s1, s2 := []byte("birinci-ortak-sir-32-bayt-uzunlk"), []byte("ikinci-ortak-sir-32-bayt-uzunluk")
	m, err := seal(s1, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unseal(s2, m); err == nil {
		t.Fatal("başka sırla mühür açıldı")
	}
	if k, err := unseal(s1, m); err != nil || k != testKey {
		t.Fatalf("doğru sırla açılmadı: %q %v", k, err)
	}
	if sasCode(s1, []byte("a"), []byte("b")) == sasCode(s2, []byte("a"), []byte("b")) {
		t.Fatal("farklı sırlar aynı kodu üretti")
	}
}

func TestTeklifSuresiDolar(t *testing.T) {
	pc := newTestNode(t, "ikinci-pc", "", true)
	pc.m.EnablePairOffers(func(string, InOffer) error { return nil })
	kutu := newTestNode(t, "mcos-kutu", testKey, false)
	id := discover(t, kutu, pc)
	if _, err := kutu.m.PairOffer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	ps := pc.m.pairSt()
	ps.mu.Lock()
	for _, o := range ps.in {
		o.At = time.Now().Add(-pairOfferTTL - time.Second)
	}
	ps.mu.Unlock()
	if n := len(pc.m.Offers()); n != 0 {
		t.Fatalf("süresi dolan teklif listede kaldı (%d)", n)
	}
}
