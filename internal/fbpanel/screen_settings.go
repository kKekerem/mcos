package fbpanel

import (
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/sound"
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
	setSounds
	setPassword
	setRemote
	setVNC
	setSSH
	setDisplay
	setSharing
	setPersist
	setInstall
	setUpdate
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
	setAnimations: {"Animasyonlar", "Geçiş hızı: normal → hızlı → kapalı → yavaş (Enter)"},
	setSounds:     {"Ses efektleri", "Geçişlerde kısa ses; hoparlör yoksa sessiz"},
	setPassword:   {"Panel parolası", "İsteğe bağlı — paneli kilitler"},
	setRemote:     {"Uzaktan kontrol", "Telefon uygulamasıyla bağlan (jeton burada)"},
	setVNC:        {"Ekran paylaşımı (VNC)", "RealVNC ile bu ekrana bağlan"},
	setSSH:        {"SSH", "Kabuk erişimi — bilgisayardan bağlan"},
	setDisplay:    {"Ekran ayarları", "Çözünürlük (Ekran bölümüne gider)"},
	setSharing:    {"PC paylaşımı", "Ağdaki diğer MCOS cihazlarıyla çalış"},
	setPersist:    {"USB'yi kalıcı yap", "Bu USB belleğe kalıcı veri bölümü oluştur"},
	setInstall:    {"Diske / USB'ye kur", "Sihirbazı açmadan MCOS'u bir diske ya da USB'ye kur"},
	setUpdate:     {"Sistemi güncelle (USB'deki ISO)", "Yeni sürüme geç; sunucular ve dünyalar korunur"},
	setWizard:     {"Kurulum sihirbazı", "İlk kurulum adımlarını yeniden çalıştır"},
}

func (a *App) drawSettings(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Ayarlar")
	_, _, cfg := a.Snapshot()
	ui := a.UIPrefs()

	y := in.Min.Y
	cur := a.Cursor()

	// ── Yakalanan gerçek hata: son satır HİÇ çizilmiyordu ──────────────────
	// Döngü sığmayan satırda duruyordu ve kaydırma yoktu. 800x600'de ölçüldü
	// (setup_klavye_test.go, TestAyarlarinHerSatiriCizilir): 12 satırdan 11'i
	// çiziliyordu; "Kurulum sihirbazı" satırına imleç gidiyor ama kullanıcı
	// onu göremiyordu. "Diske / USB'ye kur" eklenince 1024x768 de taşacaktı.
	// Artık sığmayan liste imleci izleyerek kayar ve kaç satırın gizli
	// olduğu altta yazar.
	rowH := u.F.CellH*2 + u.M.PadY
	rowStep := rowH + u.M.PadY/2
	first, last := 0, int(settingCount)
	if fit := (in.Dy() + u.M.PadY/2) / rowStep; fit < int(settingCount) {
		// Bir satırlık yer "daha fazla" notuna ayrılır.
		fit = (in.Dy() - u.F.CellH - u.M.PadY + u.M.PadY/2) / rowStep
		if fit < 1 {
			fit = 1
		}
		if cur >= fit {
			first = cur - fit + 1
		}
		if first+fit > int(settingCount) {
			first = int(settingCount) - fit
		}
		last = first + fit
	}

	for i := settingKind(first); i < settingKind(last); i++ {
		it := settingsRows[i]
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

	if first > 0 || last < int(settingCount) {
		note := ""
		if first > 0 {
			note = fmt.Sprintf("↑ %d ayar yukarıda", first)
		}
		if n := int(settingCount) - last; n > 0 {
			if note != "" {
				note += " · "
			}
			note += fmt.Sprintf("↓ %d ayar aşağıda", n)
		}
		u.Text(in.Min.X, y, note, u.Pal.TextFaint)
		return
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
		if !ui.Animations {
			return "kapalı", u.Pal.TextFaint
		}
		return animSpeedLabel(ui.AnimSpeed), u.Pal.OK
	case setSounds:
		if !ui.Sounds {
			return "kapalı", u.Pal.TextFaint
		}
		// Çıkış aygıtı da yazılıyor: "ses açık ama duyulmuyor" şikâyetinin
		// ilk sorusu "sistem bir ses aygıtı görüyor mu?"dur — fare
		// ayarlarında bulunan aygıtları listelemekle aynı gerekçe.
		switch b := a.SoundBackend(); b {
		case "alsa":
			return "açık · ses kartı", u.Pal.OK
		case "pcspkr":
			return "açık · anakart bipçisi", u.Pal.Warn
		case "yok":
			return "açık · ses aygıtı YOK", u.Pal.Warn
		default:
			return "açık", u.Pal.OK
		}
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
	case setVNC:
		if cfg != nil && cfg.VNC.Enabled {
			if cfg.VNC.ViewOnly {
				return "açık · izleme", u.Pal.Warn
			}
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
	case setPersist:
		if a.runningJobs()[persistJobID] {
			return "sürüyor", u.Pal.Accent
		}
	case setInstall:
		if a.runningJobs()[installJobID] {
			return "kuruluyor", u.Pal.Accent
		}
	case setUpdate:
		if a.runningJobs()[updateJobID] {
			return "güncelleniyor", u.Pal.Accent
		}
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
		a.cycleAnimations()
	case setSounds:
		a.openSoundSettings()
	case setPassword:
		a.openPasswordSettings()
	case setDisplay:
		a.gotoSection(SecDisplay)
		a.setFocus(FocusContent)
	case setSharing:
		a.toggleSharing()
	case setRemote:
		a.openRemoteSettings()
	case setVNC:
		a.openVNCSettings()
	case setSSH:
		a.openSSHSettings()
	case setPersist:
		a.confirmPersist()
	case setInstall:
		a.openInstallPicker()
	case setUpdate:
		a.openUpdatePicker()
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

// animSpeedLabel, hız kademesinin ekranda görünen adı.
func animSpeedLabel(speed string) string {
	switch speed {
	case model.AnimSlow:
		return "yavaş"
	case model.AnimFast:
		return "hızlı"
	}
	return "normal"
}

// nextAnimSpeed, Enter'a her basışta geçilecek kademe.
//
// ── Neden bu sıra: normal → hızlı → kapalı → yavaş → normal ─────────────────
//
// Kullanıcının isteği: "animasyon hızı ayarlanabilsin". Bu satır eskiden
// yalnızca aç/kapa idi. Ayrı bir hız penceresi yerine aynı satırda kademe
// DÖNDÜRÜLÜYOR: değer sağda zaten yazılı ve bir basışta sonuç görülüyor.
//
// Varsayılandan (normal) ilk basış HIZLI'ya gider: hız ayarına bakan
// kullanıcının çoğu geçişleri kısaltmak ister. "Kapalı" ortada, çünkü yavaş
// bir makinede aranan şey odur ve iki basışta ulaşılmalı.
func nextAnimSpeed(ui model.UIConfig) (on bool, speed string) {
	if !ui.Animations {
		return true, model.AnimSlow
	}
	switch ui.AnimSpeed {
	case model.AnimFast:
		// Kapalı. Hız alanına dokunulmuyor; bir sonraki basış zaten
		// yukarıdaki dal ile "yavaş"a geçiyor.
		return false, ui.AnimSpeed
	case model.AnimSlow:
		return true, ""
	}
	return true, model.AnimFast
}

// cycleAnimations steps through the animation speeds (and off).
func (a *App) cycleAnimations() {
	var ui model.UIConfig
	a.updateUI(func(u *model.UIConfig) {
		on, speed := nextAnimSpeed(*u)
		u.Animations = on
		// Açılış animasyonu da aynı ayarı izliyor: "kapalı" diyen kullanıcı
		// açılışta da beklemek istemiyor (eski aç/kapa ile aynı davranış).
		u.BootAnimation = on
		if on {
			u.AnimSpeed = speed
		}
		ui = *u
	})
	if !ui.Animations {
		a.Emit(fbui.EventInfo, "Animasyonlar kapatıldı — geçişler anında")
		return
	}
	a.Emit(fbui.EventOK, "Animasyon hızı: "+animSpeedLabel(ui.AnimSpeed))
	// Yeni hızı HEMEN göster: bir soluklaşma oynatmak, "yavaş" ile "hızlı"
	// arasındaki farkı anlatmanın en kısa yolu.
	a.beginTransition(transFade)
}

// ── Ses ayarları ────────────────────────────────────────────────────────────

// soundOption is one row of the sound dialog.
type soundOption int

const (
	sndEnabled soundOption = iota
	// sndBeeper: ses kartı yokken anakart bipçisine düşülsün mü. Varsayılan
	// kapalı; kullanıcı efektlerin hoparlörden gelmesini istedi.
	sndBeeper
	sndTest
	sndOptionCount
)

// soundModal toggles sound and lets the user HEAR the result.
//
// ── Neden ayrı bir pencere, tek satırlık bir aç/kapa değil ──────────────────
//
// Ses, doğrulanması en zor özelliktir: ayar açık görünür, hoparlör bağlıdır,
// ama kodek kapalı olduğu için hiçbir şey duyulmaz. Kullanıcının elinde
// "açtım, duymuyorum" dışında bir bilgi kalmaz.
//
// Bu pencere iki şeyi birlikte gösteriyor: hangi çıkışın seçildiği ve
// isteğe bağlı bir DENEME sesi. Fare ayarlarında bulunan aygıtların
// listelenmesiyle aynı gerekçe.
type soundModal struct{ cursor int }

func newSoundModal() *soundModal { return &soundModal{} }

func (m *soundModal) Cursor() int { return m.cursor }

func (m *soundModal) SetCursor(i int) {
	if i >= 0 && i < int(sndOptionCount) {
		m.cursor = i
	}
}

func (m *soundModal) Title() string    { return "Ses efektleri" }
func (m *soundModal) Size() (int, int) { return 58, 16 }

func (m *soundModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	ui := a.UIPrefs()

	y := r.Min.Y
	u.Text(r.Min.X, y, "Geçişlerde çalan kısa tonlar.", u.Pal.TextDim)
	y += u.F.CellH + u.M.PadY*2

	rows := [sndOptionCount]struct {
		label string
		check bool
		on    bool
		value string
	}{
		sndEnabled: {label: "Ses efektleri", check: true, on: ui.Sounds},
		sndBeeper:  {label: "Ses kartı yoksa anakart bipçisi", check: true, on: ui.Beeper},
		sndTest:    {label: "Sesi dene", value: "Enter"},
	}

	for i := soundOption(0); i < sndOptionCount; i++ {
		row := image.Rect(r.Min.X, y, r.Max.X, y+u.M.RowH)
		a.addZone(row, zoneModalRow, int(i))
		if int(i) != m.cursor && a.hoverModalRow(int(i)) {
			u.HoverRow(row)
		}
		cx := u.Row(row, int(i) == m.cursor)
		ty := y + (u.M.RowH-u.F.CellH)/2

		l := rows[i]
		if l.check {
			u.Check(cx, ty, l.on)
			cx += u.F.CellW + u.M.Gap
		}
		col := u.Pal.Text
		switch {
		case (i == sndTest || i == sndBeeper) && !ui.Sounds:
			col = u.Pal.TextFaint // ses kapalıyken deneme ve bipçi anlamsız
		case int(i) == m.cursor:
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

	// Hangi çıkış kullanılıyor: "açtım ama duymuyorum" sorusunun cevabı.
	backend, note, col := "denenmedi", "İlk ses çalınca aranacak.", u.Pal.TextDim
	switch a.SoundBackend() {
	case "alsa":
		backend, note, col = "ses kartı (ALSA)", "Hoparlör/kulaklık çıkışı.", u.Pal.OK
		// Hangi çıkış: HDMI mi analog mu? "Ses kartı" demek, aynı makinede
		// iki kart varken asıl soruyu ("hangisinden?") yanıtlamıyordu.
		if o := a.SoundOutput(); o != "" {
			note = o
		}
	case "pcspkr":
		backend, note, col = "anakart bipçisi",
			"Ses kartı bulunamadı; tek tonluk bip çalınır.", u.Pal.Warn
	case "yok":
		backend, col = "YOK", u.Pal.Warn
		note = "Sistem hiçbir ses kartı görmüyor."
		if !ui.Beeper {
			note = "Ses kartı bulunamadı (bipçi kapalı, sessiz)."
		}
	}
	u.Text(r.Min.X, y, "ÇIKIŞ", u.Pal.TextFaint)
	u.TextRight(r.Max.X-u.M.PadX, y, backend, col)
	y += u.F.CellH + u.M.PadY/2
	u.Text(r.Min.X, y, note, u.Pal.TextFaint)

	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Kapat", Key: "Esc", Style: fbui.ButtonPrimary},
	}, 0)
	a.addModalButtons(btns)
}

func (m *soundModal) Key(a *App, key string) bool {
	switch key {
	case "esc", "left", "h":
		return true
	case "up", "k":
		m.cursor = (m.cursor - 1 + int(sndOptionCount)) % int(sndOptionCount)
	case "down", "j", "tab":
		m.cursor = (m.cursor + 1) % int(sndOptionCount)
	case "enter", "right", "l", " ":
		switch soundOption(m.cursor) {
		case sndEnabled:
			a.toggleSounds()
		case sndBeeper:
			a.toggleBeeper()
		case sndTest:
			// Deneme sesi ONAY tonudur: en belirgin olanı.
			a.playSound(sound.Confirm)
			a.Emit(fbui.EventInfo, "Deneme sesi çalındı ("+a.SoundBackend()+")")
		}
	}
	a.Invalidate()
	return false
}

// openSoundSettings shows the sound dialog.
func (a *App) openSoundSettings() { a.OpenModal(newSoundModal()) }

// toggleSounds turns the interface sound effects on or off.
//
// Kullanıcının isteği "kapatılabilsin" idi; kapalıyken ses arka ucu hiç
// aranmaz (bkz. internal/sound), yani ses kartı olmayan bir makinede boşuna
// aygıt taraması da yapılmaz.
func (a *App) toggleSounds() {
	on := false
	a.updateUI(func(u *model.UIConfig) {
		u.Sounds = !u.Sounds
		on = u.Sounds
	})
	if on {
		// Açıldığını DUYURMAK gerekiyor: ayarın işe yarayıp yaramadığı
		// ancak bir ses çalınca anlaşılır. Ses aygıtı yoksa yalnızca
		// satırdaki değer bunu söyler.
		a.playSound(sound.Confirm)
		a.Emit(fbui.EventOK, "Ses efektleri açıldı ("+a.SoundBackend()+")")
	} else {
		a.Emit(fbui.EventInfo, "Ses efektleri kapatıldı")
	}
}

// toggleBeeper allows or forbids the motherboard-beeper fallback.
func (a *App) toggleBeeper() {
	on := false
	a.updateUI(func(u *model.UIConfig) {
		u.Beeper = !u.Beeper
		on = u.Beeper
	})
	if on {
		a.Emit(fbui.EventInfo, "Ses kartı yoksa anakart bipçisi kullanılacak")
	} else {
		a.Emit(fbui.EventInfo, "Anakart bipçisi kapatıldı — efektler yalnızca ses kartından")
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

// persistJobID: kalıcılığın arka plan işi. Ayarlar ve sihirbaz AYNI kimliği
// kullanır: ikisinden birden basılsa bile mcos-persist tek kez çalışır.
const persistJobID = "kalicilik"

// persistConfirmLines, kalıcılık onay penceresinin metni (Ayarlar ve sihirbaz).
func persistConfirmLines() []string {
	return []string{
		"Bu USB belleğe kalıcı bir veri bölümü oluşturulacak.",
		"Mevcut veriler korunur; boş alan kullanılır.",
		"Ventoy USB'sinde bölüm eklenmez: Ventoy bölümüne",
		"/mcos/mcos-data.dat (4 GB) yazılır; birkaç dakika sürebilir.",
		"İş arka planda sürer; bu sırada paneli kullanabilirsiniz.",
	}
}

func (a *App) confirmPersist() {
	a.OpenModal(NewConfirmModal(
		"USB'yi kalıcı yap?",
		persistConfirmLines(),
		"Devam", false,
		func(app *App) {
			if app.offline() {
				return
			}
			app.startPersist(nil)
		}))
}

// startPersist runs mcos-persist as a background job (bkz. jobs.go).
//
// sonuc (nil olabilir) işin sonucunu, iş kayıttan düşmeden ÖNCE alır:
// sihirbaz sayfası kendi durumunu buna göre çizer. Üç sonuç var ve üçü de
// farklı gösterilmeli (ipcclient.Persist): kuruldu (EventOK), bu ortamda
// geçerli değil (EventInfo — canlı DVD/ISO; yeşil "tamam" yanlış olurdu),
// gerçek hata (err).
func (a *App) startPersist(sonuc func(kind fbui.EventKind, msg string, err error)) bool {
	return a.runJobKind(persistJobID, "USB kalıcı yapılıyor (Ventoy'da birkaç dakika sürebilir)",
		func() (fbui.EventKind, string, error) {
			msg, ok, err := a.cl.Persist("")
			if err != nil {
				// Daemon'ın iletisi zaten "kalıcılık başarısız: " ile başlıyor;
				// a.Fail aynı bağlamı bir kez daha ekleyince QEMU'da alt çubukta
				// "kalıcılık başarısız: kalıcılık başarısız: mcos-persist: …"
				// görüldü. Sayfa ve alt çubuk yalnızca betiğin sebebini alır.
				err = errors.New(strings.TrimPrefix(err.Error(), "kalıcılık başarısız: "))
			}
			kind := fbui.EventOK
			switch {
			case err != nil:
				kind = fbui.EventError
			case !ok:
				kind = fbui.EventInfo
			}
			if sonuc != nil {
				sonuc(kind, msg, err)
			}
			if err != nil {
				return fbui.EventError, "kalıcılık başarısız", err
			}
			return kind, msg, nil
		}, nil)
}

// ── Diske / USB'ye kur (sihirbazsız) ────────────────────────────────────────

// openInstallPicker lists install targets without re-running the wizard.
//
// Kullanıcı (gerçek PC): "ayarlardan kurulumu başlatmadan direkt USB'ye
// kurulum yapılabilmeli". Eskiden tek yol "Kurulum sihirbazı"ydı: bütün
// ayarları baştan sormak, kaydetmek, sonra diske kur sayfasına varmak.
//
// Kurulum mantığı sihirbazla AYNIDIR (startDiskInstall → mcos-install
// <aygıt>): aynı hedef listesi (açılış yapılan USB listelenmez), aynı onay
// metni (confirmInstallLines), aynı ilerleme ve aynı gerçek hata metni.
//
// Disk listesi arka planda alınır (IPC ana döngüyü bloklamamalı; bkz.
// run.go). Pencere liste gelince açılır; o arada kullanıcı başka bir pencere
// açtıysa ya da bölümden çıktıysa açılmaz — görmediği bir pencerenin
// tuşları yutması "klavye çalışmıyor" demektir.
func (a *App) openInstallPicker() {
	if a.offline() {
		a.Emit(fbui.EventError, "daemon bağlantısı yok — diskler listelenemez")
		return
	}
	if a.runningJobs()[installJobID] {
		a.Emit(fbui.EventInfo, "Kurulum zaten sürüyor — ilerleme alt çubukta")
		return
	}
	a.Emit(fbui.EventBusy, "Diskler taranıyor…")
	go func() {
		disks, err := a.cl.Disks()
		if err != nil {
			a.Fail("diskler listelenemedi", err)
			return
		}
		if a.ActiveModal() != nil || a.Section() != SecSettings ||
			a.setupState() != nil || a.Locked() {
			a.Emit(fbui.EventInfo, "Disk listesi hazır — Ayarlar > Diske / USB'ye kur ile yeniden açın")
			return
		}
		a.OpenModal(newInstallPicker(disks))
	}()
}

// newInstallPicker builds the target list; seçim onay penceresini açar.
func newInstallPicker(disks []ipc.DiskTarget) *ListModal {
	items := make([]ListItem, 0, len(disks))
	for _, d := range disks {
		it := ListItem{Label: d.Device, Detail: diskDetail(d), Value: d}
		if d.Removable {
			it.Badge, it.BadgeKind = "USB", fbui.EventInfo
		}
		if d.HasPersist {
			it.Badge, it.BadgeKind = "MCOS", fbui.EventOK
		}
		items = append(items, it)
	}
	return NewListModal("Diske / USB'ye kur",
		"Hedef diski seçin. Seçtiğiniz diskteki veriler silinir.", items,
		func(app *App, _ int, it ListItem) bool {
			d := it.Value.(ipc.DiskTarget)
			app.OpenModal(NewConfirmModal("MCOS'u diske kur?",
				append(confirmInstallLines(d),
					"Kurulum arka planda sürer; bitene kadar bilgisayarı kapatmayın."),
				"Kur", true,
				func(app2 *App) { app2.startDiskInstall(d, nil, nil) }))
			return true
		}).
		WithEmpty("Uygun disk bulunamadı (açılış yapılan USB listelenmez).")
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
