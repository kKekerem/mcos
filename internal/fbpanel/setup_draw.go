package fbpanel

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
	"mcos/internal/model"
	"mcos/internal/version"
)

// Bu dosya kurulum sihirbazını çizer.
//
// ── Neden panelden farklı bir düzen? ────────────────────────────────────────
// Panelde kenar çubuğu vardır çünkü kullanıcı bölümler arasında GEZİNİR.
// Sihirbazda gezinme yoktur: ileri gidilir. Kenar çubuğu koymak, kullanıcıya
// "istediğim yere atlayabilirim" der ve adımların sırası anlamını yitirir.
//
// Bunun yerine üstte bir ADIM GÖSTERGESİ var: kaç adım kaldığını söyler ve
// tamamlanma hissi verir. Bitiş çizgisi görünmeyen bir sihirbaz, yarıda
// bırakılır.

// setupMaxCols is how wide the wizard body may get, in character cells.
//
// 88 sütun: okunabilirliğin üst sınırı. 1080p'de metni ekranın tamamına
// yaymak, gözün satır sonundan satır başına dönmesini zorlaştırır — kitap
// sayfalarının neden dar olduğuyla aynı sebep.
const setupMaxCols = 88

// pageFlow describes a multi-page wizard so ONE renderer can draw both.
//
// ── Neden ortak? ────────────────────────────────────────────────────────────
// İki sihirbazın (ilk kurulum ve sunucu oluşturma) farklı görünmesi ya da
// farklı davranması, kullanıcıyı her seferinde yeniden öğrenmeye zorlar.
// Ayrı ayrı çizilselerdi biri düzeltilip öteki unutulurdu — arayüzlerde en
// sık görülen tutarsızlık kaynağı budur.
type pageFlow struct {
	title    string
	subtitle string
	step     int
	steps    int
	rows     []setupRow
	cursor   int
	err      string
	// body draws page-specific content above the rows; returns the new y.
	body func(body image.Rectangle, y int) int
	// keys are the status-bar shortcuts.
	keys []fbui.Shortcut
}

// drawSetup paints the first-boot wizard.
//
// Durumun TAMAMI tek kilitli kopyayla alınır (setupSnapshot): alanları tek tek
// okumak, hem arka plan goroutine'leriyle yarışmak hem de aynı karenin başında
// ve sonunda FARKLI bir sihirbaz durumu çizmek demekti.
func (a *App) drawSetup(s *Setup, b image.Rectangle) {
	v := a.setupSnapshot(s)
	a.drawFlow(pageFlow{
		title:    setupTitles[v.step],
		subtitle: setupSubtitle(v.step),
		step:     int(v.step),
		steps:    int(setupStepCount),
		rows:     v.rows,
		cursor:   v.cursor,
		err:      v.err,
		body: func(body image.Rectangle, y int) int {
			return a.drawSetupBody(v, body, y)
		},
		keys: a.setupShortcuts(v),
	}, b)
}

// drawWizard paints the create-server wizard.
func (a *App) drawWizard(w *Wizard, b image.Rectangle) {
	a.drawFlow(pageFlow{
		title:    wizTitles[w.step],
		subtitle: wizSubtitle(w.step),
		step:     int(w.step),
		steps:    int(wizStepCount),
		rows:     w.rows(a),
		cursor:   w.cursor,
		err:      w.err,
		body: func(body image.Rectangle, y int) int {
			return a.drawWizardBody(w, body, y)
		},
		keys: a.wizardShortcuts(w),
	}, b)
}

// drawFlow renders one page of a wizard.
func (a *App) drawFlow(f pageFlow, b image.Rectangle) {
	u := a.ui
	u.Clear()

	barH := u.StatusBarH()
	content := image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Max.Y-barH)

	// Gövdeyi ortala ve genişliğini sınırla.
	maxW := setupMaxCols * u.F.CellW
	w := content.Dx() - u.M.PadX*4
	if w > maxW {
		w = maxW
	}
	x := content.Min.X + (content.Dx()-w)/2
	body := image.Rect(x, content.Min.Y+u.M.PadY*3,
		x+w, content.Max.Y-u.M.PadY*2)

	y := body.Min.Y

	// ── Adım göstergesi ─────────────────────────────────────────────────
	y = a.drawStepDots(f.step, f.steps, body, y)
	y += u.M.PadY * 2

	// ── Başlık ──────────────────────────────────────────────────────────
	u.Text(body.Min.X, y, f.title, u.Pal.Text)
	y += u.F.CellH + u.M.PadY/2
	if f.subtitle != "" {
		u.Text(body.Min.X, y, f.subtitle, u.Pal.TextDim)
		y += u.F.CellH + u.M.PadY
	}
	u.Divider(body.Min.X, body.Max.X, y)
	y += u.M.PadY * 2

	// ── Sayfaya özgü gövde ──────────────────────────────────────────────
	if f.body != nil {
		y = f.body(body, y)
	}

	// ── Satırlar ────────────────────────────────────────────────────────
	// İpucu metni satır kaydırılır. Kaydırmadan yazmak, uzun açıklamaların
	// panelin kenarından taşıp komşu sütunun üstüne binmesine yol açıyordu.
	//
	// -- Yakalanan gerçek hata ------------------------------------------
	// Bir pencere açıkken de her satır kendi TIKLAMA BÖLGESİNİ kaydediyor ve
	// farenin altında vurgulanıyordu. Sihirbaz gövdesi 88 sütun, pencere en
	// çok 80: perdenin iki yanında kalan onlarca sütun "tıkla beni" diye
	// parlıyordu. Tıklamanın kendisi artık dispatchClick'te engelleniyor
	// (pointer.go), ama BÖLGEYİ HİÇ KAYDETMEMEK doğrusu: vurgu da kalkar,
	// basılı tutulan düğme arkadaki satırı yakalamaz.
	modalOpen := a.ActiveModal() != nil
	hintW := body.Dx() - u.M.PadX*3
	for i, r := range f.rows {
		var hintLines []string
		if r.hint != "" {
			hintLines = u.WrapLines(r.hint, hintW)
		}
		rowH := u.F.CellH + u.M.PadY + len(hintLines)*u.F.CellH
		if y+rowH > body.Max.Y {
			break
		}
		rect := image.Rect(body.Min.X, y, body.Max.X, y+rowH)
		if !modalOpen {
			a.addZone(rect, zoneRow, i)
		}

		selected := i == f.cursor
		isButton := r.kind == rowContinue || r.kind == rowBack

		// Düğme satırlarında TAM GENİŞLİK vurgusu çizilmez: düğmenin
		// kendisi zaten odağı gösteriyor ve arkasına uzanan bir şerit,
		// düğmenin nerede bittiğini belirsizleştiriyordu.
		if !modalOpen && !selected && !isButton && a.hoverRow(i) {
			u.HoverRow(rect)
		}
		cx := body.Min.X + u.M.PadX
		col := u.Pal.Text
		if r.kind == rowInfo {
			col = u.Pal.TextDim
		}
		if selected && !isButton {
			cx = u.Row(rect, true)
			col = u.Pal.Accent
			u.Chevron(cx-u.F.CellW, y+u.M.PadY/2, 0, u.Pal.Accent)
		}

		ty := y + u.M.PadY/2
		switch r.kind {
		case rowToggle:
			u.Check(cx, ty, r.on)
			u.Text(cx+u.F.CellW+u.M.Gap, ty, r.label, col)
		case rowContinue:
			// Devam satırı bir DÜĞME olarak çizilir: sihirbazın ana eylemi
			// diğer satırlardan ayırt edilebilmeli.
			btn := u.Button(cx, ty-u.M.PadY/4, r.label, "Enter",
				fbui.ButtonPrimary, selected)
			if !modalOpen {
				a.addZone(btn, zoneRow, i)
			}
		case rowBack:
			btn := u.Button(cx, ty-u.M.PadY/4, r.label, "Esc",
				fbui.ButtonSecondary, selected)
			if !modalOpen {
				a.addZone(btn, zoneRow, i)
			}
		default:
			u.Text(cx, ty, r.label, col)
		}

		if r.value != "" {
			vc := u.Pal.Accent
			if r.kind == rowToggle || r.kind == rowInfo {
				vc = u.Pal.TextDim
			}
			u.TextRight(body.Max.X-u.M.PadX, ty, r.value, vc)
		}
		hy := ty + u.F.CellH
		for _, line := range hintLines {
			u.Text(cx, hy, line, u.Pal.TextFaint)
			hy += u.F.CellH
		}
		y = rect.Max.Y
	}

	// ── Hata ────────────────────────────────────────────────────────────
	if f.err != "" && y+u.F.CellH*2 < body.Max.Y {
		y += u.M.PadY
		u.WarnTriangle(body.Min.X, y, u.Pal.Error)
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y, f.err, u.Pal.Error)
	}

	// ── Açılır pencere ──────────────────────────────────────────────────
	if m := a.ActiveModal(); m != nil {
		if !a.scrim.Restore(u) {
			a.scrim.Capture(u)
			a.scrim.Restore(u)
		}
		cols, rows := m.Size()
		in := u.Modal(cols*u.F.CellW, rows*u.F.CellH, m.Title())
		m.Draw(a, in)
	}

	_, caps := u.StatusBar(a.LastEvent(), f.keys, a.spinFrame())
	for i := range f.keys {
		if i < len(caps) {
			a.addShortcutZone(caps[i], shortcutKeyFor(f.keys[i].Key))
		}
	}

	a.applyTransition(content)

	if px, py, ok := a.PointerPos(); ok {
		if a.busy() {
			u.CursorBusy(px, py, a.spinFrame())
		} else {
			u.Cursor(px, py)
		}
	}
}

// drawStepDots renders the progress indicator and returns the new y.
func (a *App) drawStepDots(step, steps int, body image.Rectangle, y int) int {
	u := a.ui
	gap := u.F.CellW * 2
	r := float64(u.F.CellH) * 0.18
	cx := float64(body.Min.X) + r
	cy := float64(y) + float64(u.F.CellH)/2

	for i := 0; i < steps; i++ {
		switch {
		case i < step:
			u.P.FillCircle(cx, cy, r, u.Pal.Accent)
		case i == step:
			// Şu anki adım daha büyük ve halkalı: gözün ilk gittiği yer.
			u.P.StrokeCircle(cx, cy, r*2, math.Max(1, r*0.6),
				fbdraw.Alpha(u.Pal.Accent, 0.45))
			u.P.FillCircle(cx, cy, r*1.25, u.Pal.Accent)
		default:
			u.P.FillCircle(cx, cy, r, fbdraw.Alpha(u.Pal.TextFaint, 0.55))
		}
		cx += float64(gap)
	}

	u.TextRight(body.Max.X, y,
		fmt.Sprintf("Adım %d / %d", step+1, steps),
		u.Pal.TextFaint)
	return y + u.F.CellH
}

// setupSubtitle is the one-line explanation under each page title.
func setupSubtitle(step setupStep) string {
	switch step {
	case stepWelcome:
		return "Birkaç adımda sunucu makineniz hazır olacak."
	case stepSystem:
		return "Bulunan donanım. Bir şey yapmanız gerekmiyor."
	case stepIdentity:
		return "Bu makinenin adı ve ağ bağlantısı."
	case stepJava:
		return "Minecraft sunucuları Java ile çalışır."
	case stepLook:
		return "Arayüz rengi ve saat dilimi."
	case stepBudget:
		return "Bir sunucunun kullanabileceği en fazla kaynak."
	case stepFeatures:
		return "Açmak istediklerinizi seçin; hepsi sonradan değiştirilebilir."
	case stepSecurity:
		return "Paneli bir parolayla kilitleyebilirsiniz. İsteğe bağlıdır."
	case stepSummary:
		return "Seçtikleriniz. Kaydetmeden önce son bir kez bakın."
	case stepInstall:
		return "İsterseniz MCOS'u kalıcı olarak bir diske kurabilirsiniz."
	}
	return ""
}

// drawSetupBody renders page-specific content above the row list.
//
// *Setup DEĞİL kopyasını alır: sihirbazın alanlarını çizim sırasında tek tek
// okumak arka plan goroutine'leriyle yarışırdı (bkz. setup.go, eşzamanlılık).
func (a *App) drawSetupBody(v setupView, body image.Rectangle, y int) int {
	u := a.ui
	st, _, _ := a.Snapshot()

	switch v.step {
	case stepWelcome:
		// Karşılama sayfası: logo + kısa açıklama. Açılış ekranından
		// yakınlaşarak gelen kare tam buraya oturur, bu yüzden logo
		// açılış ekranındakiyle AYNI yerde ve boyutta.
		cx := float64(body.Min.X+body.Max.X) / 2
		size := math.Min(float64(body.Dx()), float64(body.Dy())) * 0.16
		cy := float64(y) + size
		u.LogoPulse(cx, cy, size, a.Spin(), u.Pal.Accent)
		ny := int(cy+size) + u.M.PadY*3
		u.TextCenter(body.Min.X, body.Max.X, ny, "MCOS "+version.Version, u.Pal.Text)
		ny += u.F.CellH + u.M.PadY
		u.TextCenter(body.Min.X, body.Max.X, ny,
			"Minecraft Sunucu İşletim Sistemi", u.Pal.TextDim)
		ny += u.F.CellH + u.M.PadY*3
		for _, line := range []string{
			"· Sunucuları kurar, başlatır ve izler.",
			"· İnternete port yönlendirmeden açar (playit).",
			"· Birden çok bilgisayarla aynı dünyayı çalıştırabilir.",
		} {
			u.TextCenter(body.Min.X, body.Max.X, ny, line, u.Pal.TextFaint)
			ny += u.F.CellH + u.M.PadY/2
		}
		return ny + u.M.PadY*2

	case stepSystem:
		if st == nil {
			u.Text(body.Min.X, y, "Donanım okunuyor…", u.Pal.TextDim)
			return y + u.F.CellH*2
		}
		rows := [][3]any{
			{"İşlemci", fmt.Sprintf("%s · %d çekirdek",
				st.CPU.Model, st.CPU.Cores), u.Pal.Text},
			{"Bellek", bytesShort(st.Memory.TotalBytes), u.Pal.Text},
			{"Ağ", netSummary(st), netColor(a, st)},
			{"Sınıf", tierLabel(st.Tier), u.Pal.Accent},
		}
		if len(st.Disks) > 0 {
			d := st.Disks[0]
			rows = append(rows, [3]any{"Disk",
				fmt.Sprintf("%s · %s boş", d.Mount, bytesShort(d.FreeBytes)),
				u.Pal.Text})
		}
		y = a.kvList(body, y, 18, rows)
		y += u.M.PadY
		y = a.hint(body, y,
			"Bu değerlere göre varsayılan kaynak sınırları önerilecek.")
		return y + u.M.PadY*2

	case stepIdentity:
		if a.scanning() {
			return u.ScanBanner(body, y, a.Spin(),
				"Kablosuz ağlar aranıyor…", "Bu birkaç saniye sürebilir.")
		}
		if v.netNote != "" {
			u.Text(body.Min.X, y, v.netNote, u.Pal.TextDim)
			y += u.F.CellH + u.M.PadY
		}
		if st != nil {
			state, c := "internet yok", u.Pal.Warn
			if st.Net.Internet {
				state, c = "internet var", u.Pal.OK
			}
			u.StatusDot(body.Min.X, y, c)
			u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
				state+"   ·   "+st.Net.LocalIP, u.Pal.TextDim)
			y += u.F.CellH + u.M.PadY*2
		}
		return y

	case stepJava:
		state, c := "denetlenmedi", u.Pal.TextFaint
		switch {
		case v.javaChecking:
			state, c = "denetleniyor…", u.Pal.Accent
		case v.javaOK:
			state, c = v.javaNote, u.Pal.OK
		case v.javaNote != "":
			state, c = v.javaNote, u.Pal.Warn
		}
		if v.javaChecking {
			u.Spinner(body.Min.X, y, a.Spin(), c)
		} else {
			u.StatusDot(body.Min.X, y, c)
		}
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y, "Java: "+state, u.Pal.Text)
		y += u.F.CellH + u.M.PadY*2
		y = a.hint(body, y,
			"Java yoksa şimdi kurabilirsiniz; sonradan Yazılım ekranından da",
			"kurulabilir. Çevrimdışı pakette Java 21 gömülü gelir.")
		return y + u.M.PadY

	case stepBudget:
		if st != nil {
			total := int(st.Memory.TotalBytes >> 20)
			y = a.hint(body, y,
				fmt.Sprintf("Bu makinede %d MB bellek var.", total),
				"Sınırsız bırakmak, tek bir sunucunun tüm belleği almasına izin verir.")
			y += u.M.PadY
		}
		return y

	case stepFeatures:
		return y

	case stepSecurity:
		y = a.hint(body, y,
			"Parola paneli kilitler; diski ŞİFRELEMEZ.",
			"Makineyi açıp diski çıkaran birine karşı koruma sağlamaz.",
			"Unutursanız tek çözüm yeniden kurulumdur.")
		return y + u.M.PadY*2

	case stepSummary:
		return a.drawSummary(v, body, y)

	case stepInstall:
		return a.drawInstall(v, body, y)
	}
	return y
}

// drawSummary lists every choice before saving.
func (a *App) drawSummary(v setupView, body image.Rectangle, y int) int {
	u := a.ui

	net := v.ssid
	if net == "" {
		net = "kablolu"
	}
	// Parola satırı KAYDEDİLECEK DURUMU söyler ("kurulu" / "kaldırılacak" /
	// "kurulacak" / "yok"), kullanıcının o an yazdığını değil: özet ile
	// setupSave'in yazdığı şey ayrışırsa kullanıcı yanlış bilgilendirilir.
	rows := [][3]any{
		{"Bilgisayar adı", v.pcName, u.Pal.Text},
		{"Ağ", net, u.Pal.Text},
		{"Tema", fbui.ThemeLabel(fbui.ThemeOrder[v.themeIdx]), u.Pal.Text},
		{"Zaman dilimi", timezones[v.tzIdx], u.Pal.Text},
		{"En fazla RAM", budgetLabel(budgetRAM[v.ramIdx], "MB"), u.Pal.Text},
		{"En fazla CPU", budgetLabel(budgetCPU[v.cpuIdx], "%"), u.Pal.Text},
		{"PC paylaşımı", onOff(v.sharing), u.Pal.Text},
		{"playit tüneli", onOff(v.playitSetup), u.Pal.Text},
		{"Fare / touchpad", onOff(v.mouse) + " / " + onOff(v.touchpad), u.Pal.Text},
		{"Animasyonlar", onOff(v.animations), u.Pal.Text},
		{"Panel parolası", v.password, u.Pal.Text},
	}
	y = a.kvList(body, y, 22, rows)
	y += u.M.PadY

	if v.saving {
		u.Spinner(body.Min.X, y, a.Spin(), u.Pal.Accent)
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y, "Kaydediliyor…", u.Pal.Accent)
		y += u.F.CellH + u.M.PadY
	}
	return y + u.M.PadY
}

// drawInstall renders the optional disk-install page.
func (a *App) drawInstall(v setupView, body image.Rectangle, y int) int {
	u := a.ui

	if v.installDone {
		u.StatusDot(body.Min.X, y, u.Pal.OK)
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
			"KURULUM BAŞARILI", u.Pal.OK)
		y += u.F.CellH + u.M.PadY
		if v.installMsg != "" {
			u.Text(body.Min.X, y, v.installMsg, u.Pal.TextDim)
			y += u.F.CellH + u.M.PadY
		}
		y = a.hint(body, y,
			"Yeniden başlatmadan önce USB belleği çıkarın.",
			"Sistem artık diskten açılacak ve kalıcı olarak diskte çalışacak.")
		return y + u.M.PadY*2
	}

	if v.installing {
		u.Spinner(body.Min.X, y, a.Spin(), u.Pal.Accent)
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
			"Kuruluyor — bu birkaç dakika sürebilir…", u.Pal.Accent)
		y += u.F.CellH + u.M.PadY*2
		if v.installMsg != "" {
			u.Text(body.Min.X, y, v.installMsg, u.Pal.TextFaint)
			y += u.F.CellH + u.M.PadY
		}
		// Kurulum sürerken disk listesi ÇİZİLMEZ ve satırlar da sunulmaz
		// (bkz. setup.go, rowsLocked): ikinci bir mcos-install diski bozar.
		return y
	}

	if v.disksLoading {
		return u.ScanBanner(body, y, a.Spin(), "Diskler taranıyor…", "")
	}

	y = a.hint(body, y,
		"USB'den çalışmak sürekli çalışan bir sunucu için yavaştır.",
		"Diske kurulum, sistemi RAM'de değil DİSKTE kalıcı çalıştırır.",
		"Kurulum yapmadan da panele geçip MCOS'u kullanabilirsiniz.")
	y += u.M.PadY * 2

	if v.diskCount == 0 {
		u.WarnTriangle(body.Min.X, y, u.Pal.Warn)
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
			"Uygun disk bulunamadı (açılış yapılan USB listelenmez).",
			u.Pal.Warn)
		y += u.F.CellH + u.M.PadY
	} else {
		u.Text(body.Min.X, y, "HEDEF DİSK SEÇİN", u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
	}
	return y
}

func onOff(b bool) string {
	if b {
		return "açık"
	}
	return "kapalı"
}

func netSummary(st *model.SystemStatus) string {
	if st == nil {
		return ""
	}
	if st.Net.Internet {
		return st.Net.LocalIP + " · internet var"
	}
	if st.Net.LocalIP != "" {
		return st.Net.LocalIP + " · internet yok"
	}
	return "bağlantı yok"
}

func netColor(a *App, st *model.SystemStatus) any {
	if st != nil && st.Net.Internet {
		return a.ui.Pal.OK
	}
	return a.ui.Pal.Warn
}

func tierLabel(t model.Tier) string {
	switch t {
	case model.TierHigh:
		return "yüksek"
	case model.TierMedium:
		return "orta"
	case model.TierLow:
		return "düşük"
	}
	return string(t)
}

// setupShortcuts are the key hints for the wizard's status bar.
func (a *App) setupShortcuts(v setupView) []fbui.Shortcut {
	if a.ActiveModal() != nil {
		return []fbui.Shortcut{
			{Key: "↑↓", Label: "Gezin"},
			{Key: "Enter", Label: "Seç"},
			{Key: "Esc", Label: "Kapat"},
		}
	}
	// Kaydetme ya da diske kurulum sürerken tuşlar yok sayılır (setupKey);
	// çalışmayan bir kısayol göstermek, kullanıcıya panelin DONDUĞUNU
	// düşündürür. Ne olduğunu zaten alt çubuktaki olay ve sayfanın kendisi
	// söylüyor.
	if v.saving || v.installing {
		return nil
	}
	out := []fbui.Shortcut{
		{Key: "↑↓", Label: "Gezin"},
		{Key: "Enter", Label: "Seç"},
	}
	if v.step > stepWelcome {
		out = append(out, fbui.Shortcut{Key: "Esc", Label: "Geri"})
	}
	return out
}

// setupSummaryLine is used by tests to assert the wizard collected everything.
//
// Testin ekran görüntüsü karşılaştırmasına gerek kalmadan, sihirbazın
// topladığı değerleri tek satırda doğrulayabilmesi için.
func (s *Setup) summaryLine() string {
	parts := []string{
		"ad=" + s.pcName,
		"tema=" + fbui.ThemeOrder[s.themeIdx],
		"tz=" + timezones[s.tzIdx],
		"ram=" + strconv.Itoa(budgetRAM[s.ramIdx]),
		"cpu=" + strconv.Itoa(budgetCPU[s.cpuIdx]),
		"paylasim=" + onOff(s.sharing),
		"playit=" + onOff(s.playitSetup),
		"fare=" + onOff(s.mouse),
		"animasyon=" + onOff(s.animations),
	}
	return strings.Join(parts, " ")
}
