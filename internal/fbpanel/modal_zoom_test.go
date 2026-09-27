package fbpanel

import (
	"testing"
	"time"

	"mcos/internal/model"
)

// Açılır pencerenin ölçeklenerek gelmesinin testi.
//
// NEDEN: bu hareket YALNIZCA 180 ms sürüyor ve kimse ona bakarken hata
// aramıyor. Sessizce kaybolduğunda (ör. geçiş türü değişince) hiçbir test
// kırılmazdı — tıpkı bir önceki oturumda AÇILIŞ geçişinin hiç oynamadığının
// haftalarca fark edilmemesi gibi.

func TestModalZoomScaleStartsSmallAndSettles(t *testing.T) {
	a, _ := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = true
	a.SetConfig(cfg)

	a.OpenModal(NewListModal("Test", "", []ListItem{{Label: "Bir"}}, nil))

	scale, active := a.modalZoomScale()
	if !active {
		t.Fatal("pencere açılırken ölçekleme etkin değil — hareket yok")
	}
	if scale >= 1 {
		t.Errorf("pencere tam boyutta başlıyor (%.3f) — büyüme görünmez", scale)
	}
	if scale < 0.8 {
		t.Errorf("başlangıç ölçeği çok küçük (%.3f) — pencere fırlıyor gibi görünür", scale)
	}

	// Geçiş bitince ölçekleme KAPANMALI: her karede boşuna kopyalama
	// yapmak, 1080p'de kare başına megabaytlarca iş demektir.
	time.Sleep(transDuration + 60*time.Millisecond)
	if _, active := a.modalZoomScale(); active {
		t.Error("geçiş bittiği hâlde ölçekleme sürüyor")
	}
}

// Animasyonlar KAPALIYKEN hiçbir ek iş yapılmamalı.
func TestModalZoomOffWhenAnimationsDisabled(t *testing.T) {
	a, _ := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = false
	a.SetConfig(cfg)

	a.OpenModal(NewListModal("Test", "", []ListItem{{Label: "Bir"}}, nil))
	if _, active := a.modalZoomScale(); active {
		t.Error("animasyon kapalıyken ölçekleme çalışıyor")
	}
}

// Ölçekleme sırasında çizilen kare, oturmuş kareden FARKLI olmalı.
func TestModalZoomActuallyChangesPixels(t *testing.T) {
	a, img := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = true
	a.SetConfig(cfg)

	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.Draw()

	a.OpenModal(NewListModal("Ölçek", "Deneme", []ListItem{
		{Label: "Bir"}, {Label: "İki"},
	}, nil))

	// ── Neden saate GÜVENİLMİYOR ────────────────────────────────────────
	//
	// Burada eskiden "çiz, 180 ms uyu, tekrar çiz" vardı. -race altında
	// OpenModal ile ilk Draw arasında geçen süre 180 ms'lik geçişi zaten
	// bitiriyordu: iki kare de OTURMUŞ hâli çiziyor, fark 0 çıkıyor ve test
	// kod bozulmadığı hâlde kırılıyordu (ölçüldü: "yalnızca 0 bayt değişti").
	//
	// Artık saat okunmuyor, KURULUYOR: geçiş yapay olarak uzatılıp başı
	// çizdiriliyor, sonra geçiş silinip oturmuş hâl çizdiriliyor. Test
	// tamamen belirlenimli ve iddia hiç zayıflamadı.
	a.mu.Lock()
	if a.trans != nil {
		a.trans.start = time.Now()
		a.trans.dur = time.Hour
	}
	a.mu.Unlock()

	// Geçişin BAŞI.
	a.Draw()
	early := make([]byte, len(img.Pix))
	copy(early, img.Pix)

	// Geçişin SONU.
	a.mu.Lock()
	a.trans = nil
	a.mu.Unlock()
	a.Draw()

	diff := 0
	for i := range img.Pix {
		if img.Pix[i] != early[i] {
			diff++
		}
	}
	if diff < 2000 {
		t.Errorf("ölçekleme sırasında yalnızca %d bayt değişti — pencere büyümüyor", diff)
	}
}
