package fbpanel

import "testing"

// Bir pencerenin geri çağrısı YENİ bir pencere açtığında, o yeni pencere
// AÇIK KALMALI.
//
// ── Neden bu test var ───────────────────────────────────────────────────────
//
// QEMU'da gerçek bir arıza gözlendi: Ayarlar → SSH → "Parola koy / değiştir"
// seçilince pencere kapanıyor ve HİÇBİR ŞEY açılmıyordu. Yani SSH parolası
// kurulamıyor, dolayısıyla SSH hiç açılamıyordu.
//
// Sebep: tuş yolu "pencere işini bitirdi" dediğinde AÇIK OLAN pencereyi
// kapatıyordu — ama geri çağrı araya yeni bir pencere açmıştı ve kapatılan o
// oldu. Aynı hata üç yerde vardı (SSH parolası, eş eşleştirme onayı, korumalı
// Wi-Fi parolası).
//
// Kural artık merkezî (App.closeModalIf) ve bu test onu kilitliyor.

func TestCallbackOpenedModalSurvives(t *testing.T) {
	a, _ := newTestApp(t)

	inner := NewInfoModal("İç pencere", []string{"kalmalı"})
	outer := NewListModal("Dış pencere", "", []ListItem{
		{Label: "Yeni pencere aç", Value: "open"},
	}, func(app *App, _ int, _ ListItem) bool {
		app.OpenModal(inner) // geri çağrı YENİ pencere açıyor
		return true          // ve "işim bitti" diyor
	})

	a.OpenModal(outer)
	a.Key("enter")

	got := a.ActiveModal()
	if got == nil {
		t.Fatal("geri çağrının açtığı pencere kapatıldı — SSH parolası hatası")
	}
	if got != Modal(inner) {
		t.Errorf("açık pencere yanlış: %T", got)
	}
}

// Geri çağrı YENİ pencere AÇMADIYSA pencere normal şekilde kapanmalı.
func TestPlainPickStillCloses(t *testing.T) {
	a, _ := newTestApp(t)

	m := NewListModal("Liste", "", []ListItem{{Label: "Bir"}},
		func(*App, int, ListItem) bool { return true })
	a.OpenModal(m)
	a.Key("enter")

	if a.ActiveModal() != nil {
		t.Error("sıradan bir seçimden sonra pencere açık kaldı")
	}
}

// Aynı kural FARE yolunda da geçerli olmalı: tıklama ile tuş iki ayrı yoldur
// ve biri düzeltilip öteki unutulursa hata yarı yarıya geri gelir.
func TestCallbackOpenedModalSurvivesMouseClick(t *testing.T) {
	a, img := newTestApp(t)

	inner := NewInfoModal("İç", []string{"kalmalı"})
	outer := NewListModal("Dış", "", []ListItem{{Label: "Aç", Value: "open"}},
		func(app *App, _ int, _ ListItem) bool {
			app.OpenModal(inner)
			return true
		})
	a.OpenModal(outer)
	a.Draw()

	// Satırın tıklama bölgesini bul ve iki kez tıkla (listede çift tık
	// çalıştırır).
	a.mu.Lock()
	var target zone
	found := false
	for _, z := range a.zones {
		if z.kind == zoneModalRow && z.idx == 0 {
			target, found = z, true
		}
	}
	a.mu.Unlock()
	if !found {
		t.Fatal("pencere satırının tıklama bölgesi yok")
	}
	_ = img

	cx := (target.r.Min.X + target.r.Max.X) / 2
	cy := (target.r.Min.Y + target.r.Max.Y) / 2
	clickAt(a, cx, cy)
	clickAt(a, cx, cy)

	if a.ActiveModal() == nil {
		t.Fatal("fare yolunda da yeni pencere kapatıldı")
	}
}
