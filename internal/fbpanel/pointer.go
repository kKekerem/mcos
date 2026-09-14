package fbpanel

import (
	"image"
	"time"

	"mcos/internal/fbinput"
	"mcos/internal/model"
)

// Bu dosya FARE ve TOUCHPAD desteğini uygular.
//
// ── Sorun ───────────────────────────────────────────────────────────────────
// Panel pikselleri kendisi çiziyor; işletim sisteminde pencere yöneticisi,
// "düğme" nesnesi veya tıklama olayı yok. "Şu noktada ne var?" sorusunu
// yanıtlayacak hiçbir şey yoktur.
//
// ── Çözüm: çizim sırasında bölge kaydı ──────────────────────────────────────
// Her kare çizilirken, tıklanabilir her şey kendi dikdörtgenini kaydeder.
// Bir tıklama geldiğinde listede SONDAN BAŞA aranır — sonra çizilen üstte
// olduğu için (açılır pencere, alt çubuk) doğru olan budur.
//
// Bu yaklaşım, çizim ile tıklama alanının AYRI AYRI hesaplanmasını engeller.
// İkisini ayrı hesaplamak, düzen değiştiğinde sessizce kayan tıklama
// alanlarına yol açar — kullanıcı bir satıra tıklar, bir üsttekinin açılır.
//
// ── Neden tek tık seçer, çift tık çalıştırır? ───────────────────────────────
// Listelerdeki satırlar yıkıcı olabilir (sunucu durdur, Java kaldır). Tek
// tıkla çalıştırmak, yanlışlıkla değen bir parmağın sunucuyu kapatması
// demektir. Menü (kenar çubuğu) ise güvenlidir: orada tek tık yeterli.

// ZoneKind classifies a clickable region.
type ZoneKind int

const (
	// zoneNone: geçersiz bölge.
	zoneNone ZoneKind = iota
	// zoneSidebar: kenar çubuğundaki bir bölüm. Idx = Section.
	zoneSidebar
	// zoneRow: içerik listesindeki bir satır. Idx = satır sırası.
	zoneRow
	// zoneModalRow: açılır pencere listesindeki bir satır.
	zoneModalRow
	// zoneModalPrimary: açılır pencerenin onay düğmesi.
	zoneModalPrimary
	// zoneModalCancel: açılır pencerenin iptal düğmesi.
	zoneModalCancel
	// zoneShortcut: alt çubuktaki bir kısayol tuşu kapağı.
	zoneShortcut
	// zoneAction: ekrana özgü bir düğme. Key eylemi belirtir.
	zoneAction
)

// zone is one clickable rectangle recorded during Draw.
type zone struct {
	r    image.Rectangle
	kind ZoneKind
	idx  int
	// key is the action name for zoneAction ("pair-manual", "playit-claim"…).
	key string
	// keyPress is the keystroke a zoneShortcut synthesises.
	keyPress string
}

// doubleClickWindow is how close two clicks must be to count as a double click.
//
// 450 ms: Windows varsayılanı 500 ms, GNOME 400 ms. Aradaki değer, hem
// alışkanlıkla uyumlu hem de yanlışlıkla iki kez tıklamayı çift tık saymayacak
// kadar dar.
const doubleClickWindow = 450 * time.Millisecond

// pointerHideAfter hides the cursor when the mouse has not moved for a while.
//
// Klavyeyle çalışırken ekranın ortasında duran bir ok işareti dikkat dağıtır.
// 8 saniye: kullanıcı fareyi bırakıp yazmaya başladığında kaybolur, ama
// düşünürken kaybolmaz.
const pointerHideAfter = 8 * time.Second

// pointerState tracks the cursor.
type pointerState struct {
	x, y    int
	visible bool
	moved   time.Time

	// down, basılı tutulan düğmenin yakalandığı bölgedir. Basma ve bırakma
	// AYNI bölgede olmalıdır: kullanıcı yanlış yere basıp fareyi kaydırarak
	// vazgeçebilmeli (her arayüzün yaptığı gibi).
	down     bool
	downZone zone

	lastClickAt   time.Time
	lastClickZone zone
}

// ── Bölge kaydı ─────────────────────────────────────────────────────────────

// resetZones clears the clickable list at the start of a frame.
func (a *App) resetZones() {
	a.mu.Lock()
	a.zones = a.zones[:0]
	a.mu.Unlock()
}

// addZone records a clickable rectangle.
func (a *App) addZone(r image.Rectangle, kind ZoneKind, idx int) {
	a.mu.Lock()
	a.zones = append(a.zones, zone{r: r, kind: kind, idx: idx})
	a.mu.Unlock()
}

// addActionZone records a screen-specific button.
func (a *App) addActionZone(r image.Rectangle, key string) {
	a.mu.Lock()
	a.zones = append(a.zones, zone{r: r, kind: zoneAction, key: key})
	a.mu.Unlock()
}

// addShortcutZone records a status-bar key cap so it can be clicked.
func (a *App) addShortcutZone(r image.Rectangle, press string) {
	a.mu.Lock()
	a.zones = append(a.zones, zone{r: r, kind: zoneShortcut, keyPress: press})
	a.mu.Unlock()
}

// zoneAt returns the topmost zone containing the point.
//
// SONDAN BAŞA arar: en son çizilen en üsttedir.
func (a *App) zoneAt(x, y int) (zone, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := image.Pt(x, y)
	for i := len(a.zones) - 1; i >= 0; i-- {
		if p.In(a.zones[i].r) {
			return a.zones[i], true
		}
	}
	return zone{}, false
}

// ── İmleç durumu ────────────────────────────────────────────────────────────

// PointerPos returns the cursor position and whether it should be drawn.
func (a *App) PointerPos() (int, int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ptr.visible {
		return 0, 0, false
	}
	if time.Since(a.ptr.moved) > pointerHideAfter {
		return 0, 0, false
	}
	return a.ptr.x, a.ptr.y, true
}

// HoverZone returns the zone under the cursor, if the cursor is visible.
//
// Ekranlar bunu, farenin üzerinde durduğu satırı vurgulamak için kullanır.
func (a *App) hoverZone() (zone, bool) {
	x, y, ok := a.PointerPos()
	if !ok {
		return zone{}, false
	}
	return a.zoneAt(x, y)
}

// hoverRow reports whether the mouse is over content row idx.
func (a *App) hoverRow(idx int) bool {
	z, ok := a.hoverZone()
	return ok && z.kind == zoneRow && z.idx == idx
}

// hoverSidebar reports whether the mouse is over sidebar section s.
func (a *App) hoverSidebar(s Section) bool {
	z, ok := a.hoverZone()
	return ok && z.kind == zoneSidebar && z.idx == int(s)
}

// UIPrefs returns the current interface preferences, with defaults applied.
//
// cfg henüz gelmediyse (daemon'a bağlanmadan önceki ilk kareler) varsayılan
// döner: fare o anda da çalışmalı, yoksa kullanıcı "açılışta fare yok"
// diye bildirirdi.
func (a *App) UIPrefs() model.UIConfig {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil {
		return model.DefaultUI()
	}
	return cfg.UI.Normalize()
}

// pointerAllowed reports whether events from this device class are enabled.
func (a *App) pointerAllowed(k fbinput.Kind) bool {
	ui := a.UIPrefs()
	switch k {
	case fbinput.KindMouse:
		return ui.Mouse
	case fbinput.KindTouchpad, fbinput.KindTouchscreen:
		return ui.Touchpad
	}
	return false
}

// Pointer applies one pointer event. Returns the action the host must take.
//
// Çizim gerekliliği a.Invalidate() ile bildirilir; dönüş değeri yalnızca güç
// eylemleri içindir (tıklayarak kapatma onayı gibi).
func (a *App) Pointer(ev fbinput.PointerEvent) Action {
	if !a.pointerAllowed(ev.Kind) {
		return ActNone
	}
	ui := a.UIPrefs()

	// Dokunmatik ekran: mutlak konum doğrudan imleci koyar.
	if ev.HasAbs {
		a.movePointerTo(ev.AbsX, ev.AbsY)
	} else if ev.DX != 0 || ev.DY != 0 {
		// Hassasiyet yüzde cinsindendir; 100 = ham hız.
		dx := ev.DX * ui.PointerSpeed / 100
		dy := ev.DY * ui.PointerSpeed / 100
		// Yüzde çok düşükse tamsayı bölme hareketi tamamen yutabilir;
		// en az bir piksel hareket garantisi imleci canlı tutar.
		if dx == 0 && ev.DX != 0 {
			dx = sign(ev.DX)
		}
		if dy == 0 && ev.DY != 0 {
			dy = sign(ev.DY)
		}
		a.movePointerBy(dx, dy)
	}

	if ev.Wheel != 0 {
		a.wheel(ev.Wheel)
	}

	switch {
	case ev.Press && ev.Button != fbinput.ButtonNone:
		return a.pointerDown(ev.Button)
	case ev.Release && ev.Button != fbinput.ButtonNone:
		return a.pointerUp(ev.Button)
	}
	return ActNone
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}

// movePointerBy applies relative motion, clamped to the screen.
func (a *App) movePointerBy(dx, dy int) {
	a.mu.Lock()
	b := a.ui.Bounds()
	if !a.ptr.visible {
		// İlk hareket: imleci ekranın ortasında belir. Köşeden başlatmak,
		// kullanıcının onu bulmak için fareyi savurmasına yol açar.
		a.ptr.x = b.Min.X + b.Dx()/2
		a.ptr.y = b.Min.Y + b.Dy()/2
		a.ptr.visible = true
	}
	a.ptr.x = clampI(a.ptr.x+dx, b.Min.X, b.Max.X-1)
	a.ptr.y = clampI(a.ptr.y+dy, b.Min.Y, b.Max.Y-1)
	a.ptr.moved = time.Now()
	a.dirty = true
	a.mu.Unlock()
}

// movePointerTo sets an absolute position (touchscreen).
func (a *App) movePointerTo(x, y int) {
	a.mu.Lock()
	b := a.ui.Bounds()
	a.ptr.x = clampI(x, b.Min.X, b.Max.X-1)
	a.ptr.y = clampI(y, b.Min.Y, b.Max.Y-1)
	a.ptr.visible = true
	a.ptr.moved = time.Now()
	a.dirty = true
	a.mu.Unlock()
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wheel scrolls the focused list.
func (a *App) wheel(notches int) {
	// Tekerleğin yönü: yukarı = listede yukarı.
	step := -notches
	if step == 0 {
		return
	}
	if m := a.ActiveModal(); m != nil {
		// Açılır pencere listesi kendi imlecini taşır.
		for i := 0; i < absI(step); i++ {
			if step > 0 {
				m.Key(a, "down")
			} else {
				m.Key(a, "up")
			}
		}
		a.Invalidate()
		return
	}
	for i := 0; i < absI(step); i++ {
		a.moveCursor(sign(step))
	}
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// pointerDown captures the zone under the cursor.
func (a *App) pointerDown(btn fbinput.Button) Action {
	x, y, ok := a.PointerPos()
	if !ok {
		return ActNone
	}
	z, _ := a.zoneAt(x, y)
	a.mu.Lock()
	a.ptr.down = true
	a.ptr.downZone = z
	a.dirty = true
	a.mu.Unlock()
	return ActNone
}

// pointerUp completes a click if it ended in the same zone it started.
func (a *App) pointerUp(btn fbinput.Button) Action {
	a.mu.Lock()
	wasDown := a.ptr.down
	start := a.ptr.downZone
	a.ptr.down = false
	a.dirty = true
	a.mu.Unlock()

	if !wasDown {
		return ActNone
	}

	// Sağ tık HER YERDE "geri"dir: açılır pencereyi kapatır, içerikten
	// menüye döner. Bağlam menüsü YOK — bir ok tuşu arayüzünde gizli bir
	// menü, keşfedilemeyen bir özelliktir.
	if btn == fbinput.ButtonRight {
		if a.ActiveModal() != nil {
			a.CloseModal()
		} else {
			a.setFocus(FocusSidebar)
		}
		a.Invalidate()
		return ActNone
	}
	if btn != fbinput.ButtonLeft {
		return ActNone
	}

	x, y, ok := a.PointerPos()
	if !ok {
		return ActNone
	}
	end, found := a.zoneAt(x, y)
	if !found || end.kind != start.kind || end.idx != start.idx || end.key != start.key {
		// Basılan yerden farklı bir yerde bırakıldı: kullanıcı vazgeçti.
		return ActNone
	}

	dbl := a.registerClick(end)
	return a.dispatchClick(end, dbl)
}

// registerClick records the click and reports whether it was a double click.
func (a *App) registerClick(z zone) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	same := a.ptr.lastClickZone.kind == z.kind &&
		a.ptr.lastClickZone.idx == z.idx &&
		a.ptr.lastClickZone.key == z.key
	dbl := same && time.Since(a.ptr.lastClickAt) < doubleClickWindow
	a.ptr.lastClickAt = time.Now()
	a.ptr.lastClickZone = z
	if dbl {
		// Çift tık sayıldıktan sonra sayacı sıfırla: üç tık, iki ayrı çift
		// tık sayılmamalı.
		a.ptr.lastClickAt = time.Time{}
	}
	return dbl
}

// dispatchClick performs the click action.
func (a *App) dispatchClick(z zone, dbl bool) Action {
	defer a.Invalidate()

	// -- Yakalanan gercek hata -------------------------------------------
	// Tuslar icin Key() acik bir pencereye ONCELIK veriyordu, ama tiklama
	// yolunda ayni denetim YOKTU. Cizim once arka ekranin bolgelerini
	// kaydettigi icin, pencerenin ortulemedigi her yer (ozellikle kenar
	// cubugu -- pencere ortalanmis ve dar) TIKLANABILIR kaliyordu.
	//
	// Somut sonuc: "Ortak dunya" sunucu secme penceresi acikken kenar
	// cubugundan "Sunucular"a tiklamak bolumu degistiriyor ama pencere
	// acik kaliyordu; sonra secilen sunucu ILGISIZ bir ekranin uzerinde
	// islem goruyordu.
	//
	// Artik oncelik Key() ile aynidir. zoneShortcut disarida birakilmadi,
	// cunku o zaten a.Key()'e gider ve orada pencere onceligi uygulanir.
	if a.ActiveModal() != nil {
		switch z.kind {
		case zoneModalRow, zoneModalPrimary, zoneModalCancel, zoneShortcut:
			// pencereye ait: gecebilir
		default:
			return ActNone
		}
	}

	switch z.kind {
	case zoneSidebar:
		// Menüde tek tık yeterli: yıkıcı bir şey yapmaz, yalnızca bölüm açar.
		a.gotoSection(Section(z.idx))
		a.setFocus(FocusContent)
		a.loadSectionAsync()
		return ActNone

	case zoneRow:
		// Sihirbazlarda satırlar TEK TIKLA çalışır: orada gezinilecek bir
		// kenar çubuğu yok ve her satır zaten güvenli bir seçim.
		if s := a.setupState(); s != nil {
			s.cursor = z.idx
			rows := s.rows(a)
			if z.idx >= 0 && z.idx < len(rows) {
				return a.setupActivate(s, rows[z.idx])
			}
			return ActNone
		}
		if w := a.wizardState(); w != nil {
			w.cursor = z.idx
			rows := w.rows(a)
			if z.idx >= 0 && z.idx < len(rows) {
				a.wizardActivate(w, rows[z.idx])
			}
			return ActNone
		}
		cur := a.Cursor()
		focused := a.Focus() == FocusContent
		a.setFocus(FocusContent)
		a.SetCursor(z.idx)
		// Zaten seçili bir satıra tıklamak ya da çift tıklamak çalıştırır.
		if dbl || (focused && cur == z.idx) {
			return a.activate()
		}
		return ActNone

	case zoneModalRow:
		// SOMUT tip degil ARAYUZ: *ListModal disindaki satirli pencereler
		// (ornegin fare ayarlari) de fareyle kullanilabilmeli.
		rm, okRow := a.ActiveModal().(rowModal)
		if !okRow {
			return ActNone
		}
		prev := rm.Cursor()
		rm.SetCursor(z.idx)
		if dbl || prev == z.idx {
			if rm.Key(a, "enter") {
				a.CloseModal()
			}
		}
		return ActNone

	case zoneModalPrimary:
		m := a.ActiveModal()
		if m == nil {
			return ActNone
		}
		// Onay penceresinde Enter, ODAKLANMIŞ düğmeyi çalıştırır ve odak
		// güvenlik gereği "Vazgeç"tedir. Fare doğrudan onay düğmesine
		// tıkladıysa niyet açıktır; ayrı bir tuşla söylüyoruz.
		key := "enter"
		if _, isConfirm := m.(*ConfirmModal); isConfirm {
			key = "confirm"
		}
		if m.Key(a, key) {
			a.CloseModal()
		}
		return ActNone

	case zoneModalCancel:
		a.CloseModal()
		return ActNone

	case zoneShortcut:
		if z.keyPress != "" {
			return a.Key(z.keyPress)
		}
		return ActNone

	case zoneAction:
		return a.runAction(z.key)
	}
	return ActNone
}
