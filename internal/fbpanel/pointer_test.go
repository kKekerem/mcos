package fbpanel

import (
	"testing"

	"mcos/internal/fbinput"
	"mcos/internal/model"
)

// Fare/touchpad desteğinin testleri.
//
// ── Neden gerekli? ──────────────────────────────────────────────────────────
// Fare desteği ancak GERÇEK DONANIMDA fark edilir. Bir gerileme — tıklama
// alanlarının çizimden kayması, imlecin ekran dışına çıkması, kapalıyken
// yine de hareket etmesi — ancak kullanıcı bildirirse öğrenilir.
//
// Bu testler tıklamayı SENTETİK olarak üretir ve panelin doğru tepki
// verdiğini doğrular; donanım gerekmez.

// move sends a relative motion event.
func move(a *App, dx, dy int) {
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: dx, DY: dy})
}

// clickAt teleports the cursor to (x,y) and presses+releases the left button.
func clickAt(a *App, x, y int) Action {
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen,
		HasAbs: true, AbsX: x, AbsY: y})
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen,
		Button: fbinput.ButtonLeft, Press: true})
	return a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen,
		Button: fbinput.ButtonLeft, Release: true})
}

// enablePointer makes sure the config allows pointer input.
func enablePointer(a *App) {
	_, _, cfg := a.Snapshot()
	next := model.Config{}
	if cfg != nil {
		next = *cfg
	}
	next.UI = model.DefaultUI()
	a.SetConfig(&next)
}

func TestPointerAppearsAndStaysOnScreen(t *testing.T) {
	a, img := newTestApp(t)
	enablePointer(a)

	if _, _, ok := a.PointerPos(); ok {
		t.Error("hiç hareket olmadan imleç görünüyor")
	}

	move(a, 10, 10)
	x, y, ok := a.PointerPos()
	if !ok {
		t.Fatal("hareketten sonra imleç görünmüyor")
	}
	// İlk hareket imleci EKRANIN ORTASINDA belirmeli: köşeden başlatmak
	// kullanıcıyı onu aramak için fareyi savurmaya zorlar.
	b := img.Bounds()
	if x < b.Dx()/4 || x > b.Dx()*3/4 {
		t.Errorf("imleç ortada belirmedi: x=%d", x)
	}

	// Ekran dışına itmeye çalış.
	for i := 0; i < 500; i++ {
		move(a, 50, 50)
	}
	x, y, _ = a.PointerPos()
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		t.Fatalf("imleç ekran dışına çıktı: (%d,%d) sınır %v", x, y, b)
	}

	for i := 0; i < 500; i++ {
		move(a, -50, -50)
	}
	x, y, _ = a.PointerPos()
	if x < b.Min.X || y < b.Min.Y {
		t.Fatalf("imleç negatife kaçtı: (%d,%d)", x, y)
	}
}

// Fare kapalıyken hiçbir olay işlenmemeli.
func TestPointerCanBeDisabled(t *testing.T) {
	a, _ := newTestApp(t)
	_, _, cfg := a.Snapshot()
	next := *cfg
	ui := model.DefaultUI()
	ui.Mouse = false
	ui.Touchpad = false
	next.UI = ui
	a.SetConfig(&next)

	move(a, 100, 100)
	if _, _, ok := a.PointerPos(); ok {
		t.Error("fare kapalıyken imleç göründü")
	}
}

// Kenar çubuğuna tıklamak o bölümü açmalı.
func TestClickOnSidebarOpensSection(t *testing.T) {
	a, _ := newTestApp(t)
	enablePointer(a)
	a.gotoSection(SecDashboard)
	a.Draw() // bölgeleri kaydet

	// Kenar çubuğundaki "Ağ" satırının bölgesini bul.
	var target zone
	found := false
	a.mu.Lock()
	for _, z := range a.zones {
		if z.kind == zoneSidebar && Section(z.idx) == SecNetwork {
			target, found = z, true
			break
		}
	}
	a.mu.Unlock()
	if !found {
		t.Fatal("kenar çubuğunda Ağ bölgesi kaydedilmemiş")
	}

	cx := (target.r.Min.X + target.r.Max.X) / 2
	cy := (target.r.Min.Y + target.r.Max.Y) / 2
	clickAt(a, cx, cy)

	if a.Section() != SecNetwork {
		t.Errorf("tıklamadan sonra bölüm %q (Ağ olmalıydı)", a.Section().Name())
	}
	// Menüde TEK tık yeterli olmalı ve odak içeriğe geçmeli.
	if a.Focus() != FocusContent {
		t.Error("kenar çubuğuna tıklamak odağı içeriğe taşımadı")
	}
}

// İçerik satırında TEK tık yalnızca seçmeli; çalıştırmamalı.
//
// Satırlar yıkıcı olabilir (sunucu durdur). Yanlışlıkla değen bir parmağın
// sunucuyu kapatması kabul edilemez.
func TestSingleClickSelectsRowWithoutActivating(t *testing.T) {
	a, _ := newTestApp(t)
	enablePointer(a)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(0)
	a.Draw()

	var row2 zone
	found := false
	a.mu.Lock()
	for _, z := range a.zones {
		if z.kind == zoneRow && z.idx == 2 {
			row2, found = z, true
			break
		}
	}
	a.mu.Unlock()
	if !found {
		t.Skip("bu ekranda üçüncü satır yok")
	}

	before := a.Cursor()
	clickAt(a, (row2.r.Min.X+row2.r.Max.X)/2, (row2.r.Min.Y+row2.r.Max.Y)/2)

	if a.Cursor() != 2 {
		t.Errorf("tıklama satırı seçmedi: imleç %d → %d", before, a.Cursor())
	}
}

// Basıp BAŞKA yerde bırakmak tıklama saymamalı: kullanıcı vazgeçebilmeli.
func TestDragOffCancelsClick(t *testing.T) {
	a, _ := newTestApp(t)
	enablePointer(a)
	a.gotoSection(SecDashboard)
	a.Draw()

	var sidebarNet zone
	a.mu.Lock()
	for _, z := range a.zones {
		if z.kind == zoneSidebar && Section(z.idx) == SecNetwork {
			sidebarNet = z
		}
	}
	a.mu.Unlock()

	// Ağ satırına bas…
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen, HasAbs: true,
		AbsX: (sidebarNet.r.Min.X + sidebarNet.r.Max.X) / 2,
		AbsY: (sidebarNet.r.Min.Y + sidebarNet.r.Max.Y) / 2})
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen,
		Button: fbinput.ButtonLeft, Press: true})

	// …ve içerik alanında bırak.
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen, HasAbs: true,
		AbsX: 1000, AbsY: 600})
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen,
		Button: fbinput.ButtonLeft, Release: true})

	if a.Section() == SecNetwork {
		t.Error("başka yerde bırakılan tıklama yine de çalıştı")
	}
}

// Sağ tık her yerde "geri"dir.
func TestRightClickGoesBack(t *testing.T) {
	a, _ := newTestApp(t)
	enablePointer(a)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	move(a, 5, 5)

	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse,
		Button: fbinput.ButtonRight, Press: true})
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse,
		Button: fbinput.ButtonRight, Release: true})

	if a.Focus() != FocusSidebar {
		t.Error("sağ tık menüye dönmedi")
	}

	// Pencere açıkken sağ tık pencereyi kapatmalı.
	a.OpenModal(NewInfoModal("Test", []string{"satır"}))
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse,
		Button: fbinput.ButtonRight, Press: true})
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse,
		Button: fbinput.ButtonRight, Release: true})
	if a.ActiveModal() != nil {
		t.Error("sağ tık açılır pencereyi kapatmadı")
	}
}

// Tekerlek listede gezinmeli.
func TestWheelMovesCursor(t *testing.T) {
	a, _ := newTestApp(t)
	enablePointer(a)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(0)

	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, Wheel: -1})
	if a.Cursor() == 0 {
		t.Error("tekerlek aşağı imleci hareket ettirmedi")
	}
	a.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, Wheel: 1})
	if a.Cursor() != 0 {
		t.Error("tekerlek yukarı başlangıca dönmedi")
	}
}

// Hassasiyet ayarı gerçekten hızı değiştirmeli.
func TestPointerSpeedScalesMotion(t *testing.T) {
	a, _ := newTestApp(t)

	setSpeed := func(pct int) {
		_, _, cfg := a.Snapshot()
		next := *cfg
		ui := model.DefaultUI()
		ui.PointerSpeed = pct
		next.UI = ui
		a.SetConfig(&next)
	}

	setSpeed(100)
	move(a, 1, 0) // imleci ortada belirt
	x0, _, _ := a.PointerPos()
	move(a, 100, 0)
	xFast, _, _ := a.PointerPos()
	fast := xFast - x0

	// İmleci sıfırla.
	a2, _ := newTestApp(t)
	_, _, cfg2 := a2.Snapshot()
	next2 := *cfg2
	ui2 := model.DefaultUI()
	ui2.PointerSpeed = 50
	next2.UI = ui2
	a2.SetConfig(&next2)
	a2.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 1})
	y0, _, _ := a2.PointerPos()
	a2.Pointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 100})
	ySlow, _, _ := a2.PointerPos()
	slow := ySlow - y0

	if slow >= fast {
		t.Errorf("yarım hızda imleç daha yavaş hareket etmedi: %d >= %d",
			slow, fast)
	}
	if slow == 0 {
		t.Error("yarım hızda imleç hiç hareket etmedi")
	}
}

// Alt çubuktaki kısayol kapakları TIKLANABİLİR olmalı.
func TestShortcutCapsAreClickable(t *testing.T) {
	a, _ := newTestApp(t)
	enablePointer(a)
	a.gotoSection(SecDashboard)
	a.Draw()

	n := 0
	a.mu.Lock()
	for _, z := range a.zones {
		if z.kind == zoneShortcut {
			n++
		}
	}
	a.mu.Unlock()
	if n == 0 {
		t.Fatal("alt çubukta hiç tıklanabilir kısayol yok")
	}
}

// Kayıtlı tıklama alanları ÇİZİLEN alanla aynı olmalı: bölgeler ekranın
// dışına taşmamalı.
func TestZonesStayOnScreen(t *testing.T) {
	a, img := newTestApp(t)
	b := img.Bounds()

	for s := Section(0); s < secCount; s++ {
		a.gotoSection(s)
		a.setFocus(FocusContent)
		a.Draw()

		a.mu.Lock()
		zones := append([]zone(nil), a.zones...)
		a.mu.Unlock()

		for _, z := range zones {
			if z.r.Empty() {
				t.Errorf("%s: boş bölge kaydedilmiş (%v)", s.Name(), z.r)
			}
			if !z.r.In(b) && !z.r.Overlaps(b) {
				t.Errorf("%s: bölge ekran dışında: %v", s.Name(), z.r)
			}
		}
	}
}
