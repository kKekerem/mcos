package fbpanel

import (
	"testing"

	"mcos/internal/fbui"
)

// ════════════════════════════════════════════════════════════════════════════
// BEKLEME SATIRI PENCERE AÇILINCA DÜŞER
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Pencere açan her eylem önce durum çubuğuna "… alınıyor" yazıyor, sonra gelen
// yanıtla pencereyi açıyordu — ama o satırı kimse silmiyordu. QEMU'da ölçüldü:
// ekran paylaşımı penceresi kapandıktan sonra bile alt çubukta
// "ekran paylaşımı durumu alınıyor…" dönmeye devam ediyordu.
//
// Görüntüden pahalısı: needsFastRedraw son olay EventBusy ise HER tikte
// yeniden çizim ister. Unutulmuş tek satır, boştaki bir sunucuda paneli
// sonsuza kadar saniyede 12 kare çizdiriyordu.

// resetEvents empties the demo history so a test starts from a known state.
//
// FillDemo gerçekçi bir olay geçmişi kuruyor ve o geçmişin SONU da bir
// bekleme satırı ("... kuruluyor"). Bunu temizlemeden yapılan ölçüm, kendi
// yazdığımız satırın silinip silinmediğini değil demo verisini sınar.
func resetEvents(a *App) {
	a.mu.Lock()
	a.events = nil
	a.mu.Unlock()
}

func lastEventKind(a *App) (fbui.EventKind, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) == 0 {
		return 0, false
	}
	return a.events[len(a.events)-1].Kind, true
}

func TestOpenModalClearsTrailingBusy(t *testing.T) {
	a, _ := newTestApp(t)
	resetEvents(a)
	a.Emit(fbui.EventBusy, "ekran paylaşımı durumu alınıyor…")

	if k, ok := lastEventKind(a); !ok || k != fbui.EventBusy {
		t.Fatal("bekleme satırı yazılamadı — test kurulumu bozuk")
	}

	a.OpenModal(NewListModal("Ekran Paylaşımı", "", []ListItem{
		{Label: "Kapat", Value: "disable"},
	}, nil))

	if k, ok := lastEventKind(a); ok && k == fbui.EventBusy {
		t.Error("pencere açıldığı hâlde bekleme satırı duruyor — durum " +
			"çubuğu sonsuza kadar döner ve panel her tikte yeniden çizilir")
	}
}

// Bekleme satırı sürüyorsa panel hızlı yeniden çizim İSTER; düştüğünde
// istememeli. Asıl maliyetin ölçüldüğü yer burası.
func TestBusyDrivesFastRedrawAndStops(t *testing.T) {
	a, _ := newTestApp(t)
	resetEvents(a)
	// Demo verisinde başlamakta olan bir sunucu var ve o da tek başına
	// yeniden çizim istiyor. Ölçmek istediğimiz şey YALNIZCA bekleme
	// satırının payı, o yüzden ortam durgunlaştırılıyor.
	a.mu.Lock()
	a.servers = nil
	a.mu.Unlock()

	a.Emit(fbui.EventBusy, "bir şeyler yapılıyor…")
	if !a.Tick() {
		t.Fatal("bekleme satırı varken yeniden çizim istenmiyor — dönen " +
			"gösterge donuk kalır")
	}

	a.OpenModal(NewListModal("Pencere", "", []ListItem{{Label: "Bir"}}, nil))
	// Geçiş animasyonunu bekleyip ölçüyoruz: geçiş süresince Tick zaten
	// true döner, ölçmek istediğimiz şey geçişten SONRAKİ durgunluk.
	a.mu.Lock()
	a.trans = nil
	a.mu.Unlock()

	if a.Tick() {
		t.Error("pencere açıldıktan sonra hâlâ her tikte yeniden çizim " +
			"isteniyor — boştaki sunucuda boşuna CPU yakılır")
	}
}

// Sonucu bildiren olaylar (OK/hata) silinmez: kullanıcı ne olduğunu görmeli.
func TestOpenModalKeepsResultEvents(t *testing.T) {
	a, _ := newTestApp(t)
	resetEvents(a)
	a.Emit(fbui.EventOK, "Ekran paylaşımı açıldı")
	a.OpenModal(NewListModal("Pencere", "", []ListItem{{Label: "Bir"}}, nil))

	k, ok := lastEventKind(a)
	if !ok || k != fbui.EventOK {
		t.Error("pencere açılınca sonuç satırı da silindi — kullanıcı " +
			"eyleminin sonucunu göremez")
	}
}
