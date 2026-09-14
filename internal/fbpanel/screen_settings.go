package fbpanel

import (
	"fmt"
	"image"
	"strconv"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// AYARLAR EKRANI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "ayarlardan acılıp kapanabilsin touchpad ve fare
// desteği gelsin" ve "isteğe bağlı şifre olsun".
//
// ── Neden her satırın sağında DEĞERİ var? ───────────────────────────────────
// Bir ayar ekranında en sık sorulan soru "şu an ne ayarlı?" sorusudur.
// Değeri göstermeyen bir liste, her ayar için tek tek içeri girip çıkmayı
// gerektirir. Sağdaki değer bunu tamamen ortadan kaldırır.

// settingKind identifies a settings row.
type settingKind int

const (
	setTheme settingKind = iota
	setPointer
	setAnimations
	setPassword
	setRemote
	setSSH
	setDisplay
	setSharing
	setPersist
	setWizard
	settingCount
)

// settingsRow describes one line.
type settingsRow struct {
	label, desc string
}

var settingsRows = [settingCount]settingsRow{
	setTheme:      {"Tema", "Arayüz vurgu rengini değiştir"},
	setPointer:    {"Fare ve touchpad", "İmleç desteği, hassasiyet, dokunarak tıklama"},
	setAnimations: {"Animasyonlar", "Ekran geçişleri ve açılış animasyonu"},
	setPassword:   {"Panel parolası", "İsteğe bağlı — paneli kilitler"},
	setRemote:     {"Uzaktan kontrol", "Telefon uygulamasıyla bağlan (jeton burada)"},
	setSSH:        {"SSH", "Kabuk erişimi — bilgisayardan bağlan"},
	setDisplay:    {"Ekran ayarları", "Çözünürlük (Ekran bölümüne gider)"},
	setSharing:    {"PC paylaşımı", "Ağdaki diğer MCOS cihazlarıyla çalış"},
	setPersist:    {"USB'yi kalıcı yap", "Bu USB belleğe kalıcı veri bölümü oluştur"},
	setWizard:     {"Kurulum sihirbazı", "İlk kurulum adımlarını yeniden çalıştır"},
}

func (a *App) drawSettings(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Ayarlar")
	_, _, cfg := a.Snapshot()
	ui := a.UIPrefs()

	y := in.Min.Y
	cur := a.Cursor()

	for i := settingKind(0); i < settingCount; i++ {
		it := settingsRows[i]
		rowH := u.F.CellH*2 + u.M.PadY
		if y+rowH > in.Max.Y {
			break
		}
		row := image.Rect(in.Min.X, y, in.Max.X, y+rowH)
		cx, col := a.contentRow(row, int(i))
		ty := row.Min.Y + u.M.PadY/2

		if int(i) == cur && a.contentFocused() {
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		}
		u.Text(cx, ty, it.label, col)
		u.Text(cx, ty+u.F.CellH, it.desc, u.Pal.TextFaint)

		// Sağda ŞU ANKİ değer.
		val, vc := a.settingValue(i, cfg, ui)
		if val != "" {
			u.TextRight(in.Max.X-u.M.PadX, ty, val, vc)
		}
		y = row.Max.Y + u.M.PadY/2
	}

	if y+u.F.CellH*4 > in.Max.Y {
		return
	}
	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	if cfg != nil {
		y = a.kvList(in, y, 20, [][3]any{
			{"Düğüm adı", cfg.Cluster.NodeName, u.Pal.Text},
			{"Zaman dilimi", cfg.Timezone, u.Pal.Text},
			{"Wi-Fi ağı", cfg.WiFiSSID, u.Pal.Text},
		})
	}

	// Bulunan işaretleme aygıtları: "fare desteği açık ama çalışmıyor"
	// şikâyetinin ilk sorusu "sistem fareyi görüyor mu?"dur.
	a.mu.Lock()
	devs := a.pointerDevices
	a.mu.Unlock()
	if y+u.F.CellH*2 > in.Max.Y {
		return
	}
	if len(devs) == 0 {
		u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
			"İşaretleme aygıtı bulunamadı (fare/touchpad takılı mı?)", u.Pal.Warn)
		return
	}
	u.Text(in.Min.X, y, "BULUNAN AYGITLAR", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY/2
	for _, d := range devs {
		if y+u.F.CellH > in.Max.Y {
			return
		}
		u.Text(in.Min.X, y, "· "+d, u.Pal.TextDim)
		y += u.F.CellH
	}
}

// settingValue returns the current value shown on the right of a row.
func (a *App) settingValue(k settingKind, cfg *model.Config,
	ui model.UIConfig) (string, colorRGBA) {

	u := a.ui
	switch k {
	case setTheme:
		if cfg != nil {
			return fbui.ThemeLabel(cfg.Theme), u.Pal.Accent
		}
	case setPointer:
		switch {
		case !ui.Mouse && !ui.Touchpad:
			return "kapalı", u.Pal.TextFaint
		case ui.Mouse && ui.Touchpad:
			return fmt.Sprintf("açık · %%%d", ui.PointerSpeed), u.Pal.OK
		case ui.Mouse:
			return "yalnızca fare", u.Pal.OK
		default:
			return "yalnızca touchpad", u.Pal.OK
		}
	case setAnimations:
		if ui.Animations {
			return "açık", u.Pal.OK
		}
		return "kapalı", u.Pal.TextFaint
	case setPassword:
		if cfg != nil && cfg.Security.PasswordSet() {
			return "kurulu", u.Pal.OK
		}
		return "yok", u.Pal.TextFaint
	case setRemote:
		if cfg != nil && cfg.Remote.Enabled {
			return "açık", u.Pal.OK
		}
		return "kapalı", u.Pal.TextFaint
	case setSSH:
		if cfg != nil && cfg.SSH.Enabled {
			return "açık", u.Pal.OK
		}
		return "kapalı", u.Pal.TextFaint
	case setDisplay:
		return a.displayLine(), u.Pal.TextDim
	case setSharing:
		if cfg != nil && cfg.Cluster.Enabled {
			return "açık", u.Pal.OK
		}
		return "kapalı", u.Pal.TextFaint
	}
	return "", u.Pal.TextFaint
}

// activateSetting handles Enter on a settings row.
func (a *App) activateSetting(idx int) {
	switch settingKind(idx) {
	case setTheme:
		a.openThemePicker()
	case setPointer:
		a.openPointerSettings()
	case setAnimations:
		a.toggleAnimations()
	case setPassword:
		a.openPasswordSettings()
	case setDisplay:
		a.gotoSection(SecDisplay)
		a.setFocus(FocusContent)
	case setSharing:
		a.toggleSharing()
	case setRemote:
		a.openRemoteSettings()
	case setSSH:
		a.openSSHSettings()
	case setPersist:
		a.confirmPersist()
	case setWizard:
		a.confirmRerunWizard()
	}
}

// ── Fare ayarları ───────────────────────────────────────────────────────────

// openPointerSettings shows the pointer options as a live-editing list.
//
// Liste KAPANMAZ: kullanıcı hassasiyeti değiştirip hemen fareyi oynatarak
// sonucu görebilmeli. Her seçim anında uygulanır ve kaydedilir.
func (a *App) openPointerSettings() {
	a.OpenModal(newPointerModal(a))
}

// pointerRows are the options inside the pointer dialog.
type pointerOption int

const (
	ptrMouse pointerOption = iota
	ptrTouchpad
	ptrTap
	ptrSlower
	ptrFaster
	ptrOptionCount
)

// pointerModal edits model.UIConfig in place.
type pointerModal struct{ cursor int }

func newPointerModal(a *App) *pointerModal { return &pointerModal{} }

// Cursor ve SetCursor, rowModal arayuzunu karsilar: fare tiklamalarinin
// bu pencereye ulasabilmesi icin gerekli (bkz. modal.go).
func (m *pointerModal) Cursor() int { return m.cursor }

func (m *pointerModal) SetCursor(i int) {
	if i >= 0 && i < int(ptrOptionCount) {
		m.cursor = i
	}
}

func (m *pointerModal) Title() string    { return "Fare ve touchpad" }
func (m *pointerModal) Size() (int, int) { return 56, 18 }

func (m *pointerModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	ui := a.UIPrefs()

	y := r.Min.Y
	u.Text(r.Min.X, y,
		"Değişiklikler anında uygulanır ve kaydedilir.", u.Pal.TextDim)
	y += u.F.CellH + u.M.PadY*2

	type line struct {
		label string
		check bool
		on    bool
		value string
	}
	lines := [ptrOptionCount]line{
		ptrMouse:    {label: "Fare desteği", check: true, on: ui.Mouse},
		ptrTouchpad: {label: "Touchpad desteği", check: true, on: ui.Touchpad},
		ptrTap:      {label: "Dokunarak tıklama", check: true, on: ui.TapToClick},
		ptrSlower:   {label: "İmleci yavaşlat", value: "−10%"},
		ptrFaster:   {label: "İmleci hızlandır", value: "+10%"},
	}

	for i := pointerOption(0); i < ptrOptionCount; i++ {
		row := image.Rect(r.Min.X, y, r.Max.X, y+u.M.RowH)
		a.addZone(row, zoneModalRow, int(i))
		if int(i) != m.cursor && a.hoverModalRow(int(i)) {
			u.HoverRow(row)
		}
		cx := u.Row(row, int(i) == m.cursor)
		ty := y + (u.M.RowH-u.F.CellH)/2

		l := lines[i]
		if l.check {
			u.Check(cx, ty, l.on)
			cx += u.F.CellW + u.M.Gap
		}
		col := u.Pal.Text
		if int(i) == m.cursor {
			col = u.Pal.Accent
		}
		u.Text(cx, ty, l.label, col)
		if l.value != "" {
			u.TextRight(r.Max.X-u.M.PadX, ty, l.value, u.Pal.TextDim)
		}
		y += u.M.RowH
	}

	y += u.M.PadY
	u.Divider(r.Min.X, r.Max.X, y)
	y += u.M.PadY * 2
	u.Text(r.Min.X, y, "Hassasiyet", u.Pal.TextDim)
	u.TextRight(r.Max.X-u.M.PadX, y, strconv.Itoa(ui.PointerSpeed)+"%", u.Pal.Accent)
	y += u.F.CellH + u.M.PadY/2
	// Çubuk, 20..300 aralığını 0..100 doluluğa eşler.
	barW := r.Dx() - u.M.PadX
	u.Progress(r.Min.X, y, barW, (ui.PointerSpeed-20)*100/280)
	y += u.F.CellH
	// Uç etiketleri ŞART: etiketsiz bir çubukta "%100" yazarken çubuğun
	// üçte bir dolu görünmesi hata gibi okunur. Uçlar, çubuğun neyin
	// aralığı olduğunu söyler.
	u.Text(r.Min.X, y, "20%", u.Pal.TextFaint)
	u.TextRight(r.Min.X+barW, y, "300%", u.Pal.TextFaint)

	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Kapat", Key: "Esc", Style: fbui.ButtonSecondary},
	}, 0)
	if len(btns) > 0 {
		a.addZone(btns[0], zoneModalCancel, 0)
	}
}

func (m *pointerModal) Key(a *App, key string) bool {
	n := int(ptrOptionCount)
	switch key {
	case "esc":
		return true
	case "up", "k":
		m.cursor = (m.cursor - 1 + n) % n
	case "down", "j":
		m.cursor = (m.cursor + 1) % n
	case "left", "h":
		a.adjustPointerSpeed(-10)
	case "right", "l":
		a.adjustPointerSpeed(10)
	case "enter":
		switch pointerOption(m.cursor) {
		case ptrMouse:
			a.updateUI(func(u *model.UIConfig) { u.Mouse = !u.Mouse })
		case ptrTouchpad:
			a.updateUI(func(u *model.UIConfig) { u.Touchpad = !u.Touchpad })
		case ptrTap:
			a.updateUI(func(u *model.UIConfig) { u.TapToClick = !u.TapToClick })
		case ptrSlower:
			a.adjustPointerSpeed(-10)
		case ptrFaster:
			a.adjustPointerSpeed(10)
		}
	}
	return false
}

func (a *App) adjustPointerSpeed(delta int) {
	a.updateUI(func(u *model.UIConfig) {
		u.PointerSpeed += delta
		*u = u.Normalize()
	})
}

// toggleAnimations flips transitions on/off.
func (a *App) toggleAnimations() {
	on := false
	a.updateUI(func(u *model.UIConfig) {
		u.Animations = !u.Animations
		u.BootAnimation = u.Animations
		on = u.Animations
	})
	if on {
		a.Emit(fbui.EventOK, "Animasyonlar açıldı")
	} else {
		a.Emit(fbui.EventInfo, "Animasyonlar kapatıldı")
	}
}

// updateUI mutates the UI preferences and persists them.
//
// Yapılandırmanın KOPYASI üzerinde çalışır: kaydetme başarısız olursa
// bellekteki durum diskle uyuşmaz kalırdı ve kullanıcı ayarın uygulandığını
// sanırdı.
func (a *App) updateUI(mut func(*model.UIConfig)) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil {
		return
	}
	next := *cfg
	ui := next.UI.Normalize()
	mut(&ui)
	next.UI = ui.Normalize()

	a.mu.Lock()
	a.cfg = &next
	a.dirty = true
	a.mu.Unlock()

	a.saveConfigAsync(&next, "ayar kaydedilemedi", nil)
}

// ── Panel parolası ──────────────────────────────────────────────────────────

// openPasswordSettings offers to set, change, or remove the panel password.
func (a *App) openPasswordSettings() {
	_, _, cfg := a.Snapshot()
	set := cfg != nil && cfg.Security.PasswordSet()

	items := []ListItem{}
	if set {
		items = append(items,
			ListItem{Label: "Parolayı değiştir", Value: "set"},
			ListItem{Label: "Parolayı kaldır", Value: "clear"},
			ListItem{Label: "Şimdi kilitle", Value: "lock"},
		)
		lockLabel := "Uykudan uyanınca parola sor"
		items = append(items, ListItem{
			Label:   lockLabel,
			Current: cfg.Security.LockOnSleep,
			Value:   "sleep",
		})
	} else {
		items = append(items, ListItem{Label: "Parola koy", Value: "set"})
	}

	intro := "Parola İSTEĞE BAĞLIDIR; diski şifrelemez."
	a.OpenModal(NewListModal("Panel parolası", intro, items,
		func(app *App, _ int, it ListItem) bool {
			switch it.Value.(string) {
			case "set":
				app.askNewPassword()
			case "clear":
				app.setPanelPassword("")
			case "lock":
				app.Lock()
			case "sleep":
				app.toggleLockOnSleep()
			}
			return true
		}))
}

func (a *App) askNewPassword() {
	a.OpenModal(NewTextModal("Panel parolası",
		"Yeni parolayı girin (boş bırakırsanız parola kaldırılır).",
		func(app *App, pw string) { app.confirmNewPassword(pw) }).
		Masked().
		WithOK("Devam").
		WithHint("En az " + strconv.Itoa(model.MinPasswordLen) + " karakter.").
		WithValidate(func(s string) string {
			if s == "" {
				return "" // boş = kaldır, geçerli
			}
			// KIRPILMIS uzunluk: model de oyle deger biciyor. Ikisi ayrismis
			// olsaydi arayuz dort boslugu kabul eder, model onu reddederdi.
			if len([]rune(strings.TrimSpace(s))) < model.MinPasswordLen {
				return "En az " + strconv.Itoa(model.MinPasswordLen) + " karakter olmalı"
			}
			return ""
		}))
}

// confirmNewPassword asks for the password twice.
//
// İKİ KEZ SORMAK ŞART: yazarken görünmeyen bir alanda tek harflik bir hata,
// kullanıcıyı kendi panelinden kilitler ve tek çıkış yolu yeniden kurulum
// olur.
func (a *App) confirmNewPassword(pw string) {
	if pw == "" {
		a.setPanelPassword("")
		return
	}
	a.OpenModal(NewTextModal("Parolayı doğrula",
		"Aynı parolayı bir kez daha girin.",
		func(app *App, again string) {
			if again != pw {
				app.Emit(fbui.EventError, "Parolalar eşleşmedi — değiştirilmedi")
				return
			}
			app.setPanelPassword(pw)
		}).
		Masked().
		WithOK("Kaydet").
		WithValidate(func(s string) string {
			if s != pw {
				return "Parolalar eşleşmiyor"
			}
			return ""
		}))
}

// setPanelPassword stores (or clears) the password.
func (a *App) setPanelPassword(pw string) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil {
		return
	}
	next := *cfg
	if err := next.Security.SetPassword(pw); err != nil {
		a.Fail("parola ayarlanamadı", err)
		return
	}
	a.mu.Lock()
	a.cfg = &next
	a.dirty = true
	a.mu.Unlock()

	// Onay mesajı YAZMA BAŞARILI olunca verilir: "kaydedildi" deyip sonra
	// kaydedememek, kullanıcının parolasız kaldığını fark etmemesi demekti.
	a.saveConfigAsync(&next, "parola kaydedilemedi", func() {
		if pw == "" {
			a.Emit(fbui.EventInfo, "Panel parolası kaldırıldı")
		} else {
			a.Emit(fbui.EventOK, "Panel parolası kaydedildi")
		}
	})
}

func (a *App) toggleLockOnSleep() {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil || !cfg.Security.PasswordSet() {
		a.Emit(fbui.EventWarn, "Önce bir parola koyun")
		return
	}
	next := *cfg
	next.Security.LockOnSleep = !next.Security.LockOnSleep
	a.mu.Lock()
	a.cfg = &next
	a.dirty = true
	a.mu.Unlock()
	a.saveConfigAsync(&next, "ayar kaydedilemedi", func() {
		if next.Security.LockOnSleep {
			a.Emit(fbui.EventOK, "Uykudan uyanınca parola sorulacak")
		} else {
			a.Emit(fbui.EventInfo, "Uykudan uyanınca parola sorulmayacak")
		}
	})
}

// ── PC paylaşımı ────────────────────────────────────────────────────────────

// toggleSharing turns LAN peer sharing on or off.
//
// Kullanıcının belirttiği mantık hatası buydu: eşleştirmenin KENDİSİ
// kurulum sihirbazına ait değil; oraya yalnızca "bu açılsın mı?" sorusu
// aittir. Açma/kapama burada, eşleştirme MCOS Paylaşım ekranında.
func (a *App) toggleSharing() {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil {
		return
	}
	next := *cfg
	next.Cluster.Enabled = !next.Cluster.Enabled

	a.mu.Lock()
	a.cfg = &next
	a.dirty = true
	a.mu.Unlock()

	a.saveConfigAsync(&next, "ayar kaydedilemedi", func() {
		if next.Cluster.Enabled {
			a.Emit(fbui.EventOK,
				"PC paylaşımı açıldı — MCOS Paylaşım ekranından cihaz ekleyin")
		} else {
			a.Emit(fbui.EventInfo, "PC paylaşımı kapatıldı")
		}
	})
}

func (a *App) confirmPersist() {
	a.OpenModal(NewConfirmModal(
		"USB'yi kalıcı yap?",
		[]string{
			"Bu USB belleğe kalıcı bir veri bölümü oluşturulacak.",
			"Mevcut veriler korunur; boş alan kullanılır.",
		},
		"Devam", false,
		func(app *App) {
			if app.offline() {
				return
			}
			app.Emit(fbui.EventBusy, "USB kalıcı yapılıyor…")
			go func() {
				msg, err := app.cl.Persist("")
				if err != nil {
					app.Fail("kalıcılık başarısız", err)
					return
				}
				app.Emit(fbui.EventOK, msg)
			}()
		}))
}

// confirmRerunWizard re-opens the first-boot wizard.
//
// Sihirbaz artık AYNI programın içinde (internal/fbpanel/setup.go), bu
// yüzden başka bir ikili çalıştırmaya gerek yok. Yine de onay soruyoruz:
// sihirbaz ekranı kaplar ve kullanıcı yanlışlıkla girmiş olabilir.
func (a *App) confirmRerunWizard() {
	a.OpenModal(NewConfirmModal("Kurulum sihirbazını yeniden çalıştır?",
		[]string{
			"Ayarlar baştan sorulacak.",
			"Çalışan sunucular DURDURULMAZ; yalnızca yapılandırma değişir.",
			"İstediğiniz adımda Esc ile geri dönebilirsiniz.",
		},
		"Başlat", false,
		func(app *App) { app.StartSetup() }))
}
