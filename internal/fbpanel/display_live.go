package fbpanel

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcos/internal/drm"
	"mcos/internal/fbfont"
	"mcos/internal/fbui"
)

// ════════════════════════════════════════════════════════════════════════════
// CANLI EKRAN: çözünürlük ve tazeleme hızı YENİDEN BAŞLATMADAN değişir
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "yeniden baslatmadan çözünürlük ve hz değişsin adam akıllı olsun".
//
// Panel ekran kartına (DRM) çiziyorsa modu kendisi kurar. Mod değişince tuval,
// yazı tipi ve arayüz yeni boyuta göre YENİDEN KURULUR; uygulama durumu
// (bölüm, imleç, açık sunucu) korunur. Yanlış bir mod ekranı karartabilir;
// bu yüzden her değişiklik 15 saniyelik bir onaya bağlı — onay gelmezse eski
// moda dönülür.

// LiveDisplay is a display whose mode can change while running.
//
// drm.Screen bunu karşılıyor. fbdev yolunda (ekran kartı yok) App.live nil
// kalır ve Ekran bölümü eski, GRUB tabanlı (yeniden başlatmalı) yolu gösterir.
type LiveDisplay interface {
	Size() (int, int)
	FramePeriod() time.Duration
	Check() bool
	Info() drm.Info
	SetMode(key string) error
	SetSaved(key string)
}

// ScreenAware hosts want to know the new size (touchpad ölçeklemesi).
type ScreenAware interface {
	SetScreen(w, h int)
}

// SetLiveDisplay enables live mode switching.
func (a *App) SetLiveDisplay(d LiveDisplay) {
	a.mu.Lock()
	a.live = d
	a.mu.Unlock()
}

// liveDisplay returns the live display, if any.
func (a *App) liveDisplay() LiveDisplay {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live
}

// AutoFontSize picks a readable cell size for the screen height.
//
// Sabit bir punto her çözünürlükte yanlış olur: 1080p'de doğru olan boyut
// 4K'da okunamayacak kadar küçük, 800x600'de ekranı kaplayacak kadar büyüktür.
// Yaklaşık 45 satır hedefleniyor. (cmd/mcos-panel-fb açılışta, panel mod
// değişiminde aynı kuralı kullanıyor — iki kural ayrışmasın diye burada.)
func AutoFontSize(screenH int) float64 {
	const targetRows = 45
	px := float64(screenH) / targetRows * 0.8
	switch {
	case px < 12:
		return 12
	case px > 40:
		return 40
	}
	return px
}

// resizeCanvas rebuilds the canvas, font and UI for a new screen size.
//
// ANA DÖNGÜDE çağrılır (Run), çizim ile yarışmaz. Arka plan goroutine'leri
// a.ui'ye dokunmuyor (yalnızca Emit/setupUpdate ile durum yazıyorlar).
func (a *App) resizeCanvas(w, h int) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	a.mu.Lock()
	old := a.ui
	a.mu.Unlock()

	face := old.F
	if px := AutoFontSize(h); face == nil || px != face.SizePx {
		if f, err := fbfont.Load(px); err == nil {
			face = f
		}
	}
	ui := fbui.NewUI(canvas, face, old.Pal)

	a.mu.Lock()
	a.ui = ui
	a.screenW, a.screenH = w, h
	// Eski boyuttaki önbellekler artık geçersiz: geçiş karesi, karalama
	// tamponu, perde önbelleği eski tuvalin boyutunda.
	a.trans = nil
	a.prevFrame = nil
	a.scratch = nil
	a.scrim = fbui.ScrimCache{}
	a.dirty = true
	oldOwned := a.ownedFace
	if face != old.F {
		a.ownedFace = face
	}
	a.mu.Unlock()

	// Önceki yeniden boyutlanmada BİZİM yüklediğimiz yazı tipi artık
	// kullanılmıyor. Açılıştakini (cmd katmanı kapatır) kapatmıyoruz.
	if oldOwned != nil && oldOwned != face {
		oldOwned.Close()
	}
	return canvas
}

// ── Kalıcı tercih ───────────────────────────────────────────────────────────

// displayConfPath, seçilen modun saklandığı yer. Testte değiştirilir.
var displayConfPath = "/data/mcos/display.conf"

// ReadDisplayMode returns the saved live mode key ("2560x1440@143912").
//
// Dosyada mcos-display'in "gfxmode=" satırı da durur (açılış çözünürlüğü);
// ikisi ayrı: "mode=" ekran kartına kurulan TAM mod, tazelemesiyle birlikte.
func ReadDisplayMode() string {
	b, err := os.ReadFile(displayConfPath)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "mode="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// writeDisplayMode stores the key, keeping every other line.
func writeDisplayMode(key string) error {
	var keep []string
	if b, err := os.ReadFile(displayConfPath); err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if l == "" || strings.HasPrefix(strings.TrimSpace(l), "mode=") {
				continue
			}
			keep = append(keep, l)
		}
	}
	if key != "" {
		keep = append(keep, "mode="+key)
	}
	if err := os.MkdirAll(filepath.Dir(displayConfPath), 0o755); err != nil {
		return err
	}
	// Geçici dosya + fsync + ad değiştirme + dizin fsync'i.
	//
	// Ölçüldü (QEMU): fsync'siz yazılan tercih, sistem ani kapanınca diske
	// HİÇ inmedi — misafir çekirdeğin önbelleğinde kaldı ve kayboldu. Gerçek
	// makinede elektrik kesintisi aynı sonucu verir; kullanıcı "4K seçtim,
	// yeniden açınca yine 1080p" derdi.
	tmp := displayConfPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(strings.Join(keep, "\n") + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, displayConfPath); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(displayConfPath)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// ── Ekran bölümünün satırları ───────────────────────────────────────────────

// displayRow is one selectable line of the live display list.
type displayRow struct {
	auto bool
	mode drm.Mode
}

// liveRows lists the selectable rows: "Otomatik" + every mode.
func liveRows(info drm.Info) []displayRow {
	rows := []displayRow{{auto: true}}
	for _, m := range info.Modes {
		rows = append(rows, displayRow{mode: m})
	}
	return rows
}

// liveSelectable reports whether the live list (not the GRUB list) is shown.
func (a *App) liveSelectable() (drm.Info, bool) {
	ld := a.liveDisplay()
	if ld == nil {
		return drm.Info{}, false
	}
	info := ld.Info()
	return info, info.Changeable
}

// displayRowCount is the row count of the display section for the cursor.
func (a *App) displayRowCount() int {
	if info, ok := a.liveSelectable(); ok {
		return len(liveRows(info))
	}
	return len(displayModes)
}

// ── Mod değişimi + 15 saniyelik onay ────────────────────────────────────────

// modeConfirmWindow, yeni modun onaylanması için verilen süre.
//
// 15 saniye: monitörlerin kendi "bu ayarı koruyayım mı?" diyaloğuyla aynı
// büyüklükte. Daha kısası ekranın oturmasını bekleyen kullanıcıyı yakalar
// (bazı monitörler mod değişiminde 3-5 sn kararır), daha uzunu kararmış bir
// ekranda beklemeyi işkenceye çevirir.
const modeConfirmWindow = 15 * time.Second

// nowFunc, testlerin saati ilerletebilmesi için.
var nowFunc = time.Now

// applyDisplayRow switches to the selected row's mode.
func (a *App) applyDisplayRow(idx int) {
	ld := a.liveDisplay()
	if ld == nil {
		return
	}
	info := ld.Info()
	rows := liveRows(info)
	if idx < 0 || idx >= len(rows) {
		return
	}
	var hedef drm.Mode
	if rows[idx].auto {
		// Otomatik: kaydı yok say, açılıştaki politikanın aynısı (gerçek
		// monitörde doğal çözünürlük + en yüksek tazeleme).
		hedef, _ = drm.Choose(info.Modes, info.ConnType, "")
	} else {
		hedef = rows[idx].mode
	}
	onceki := info.Current
	if hedef.Key() == onceki.Key() {
		if rows[idx].auto {
			_ = a.keepDisplayMode(hedef, true)
		}
		a.Emit(fbui.EventInfo, "Zaten bu modda: "+hedef.String())
		return
	}
	if err := ld.SetMode(hedef.Key()); err != nil {
		a.Fail("mod uygulanamadı", err)
		return
	}
	a.OpenModal(&modeConfirmModal{
		yeni: hedef, onceki: onceki, auto: rows[idx].auto,
		bitis: nowFunc().Add(modeConfirmWindow),
	})
	a.Invalidate()
}

// keepDisplayMode makes the mode permanent. Kayıt başarısızsa false döner.
func (a *App) keepDisplayMode(m drm.Mode, auto bool) bool {
	key := m.Key()
	if auto {
		key = "" // otomatik: tercih yok, her açılışta en iyisi seçilir
	}
	if ld := a.liveDisplay(); ld != nil {
		ld.SetSaved(key)
	}
	if err := writeDisplayMode(key); err != nil {
		// Günlüğe de yazılıyor: ekrandaki mesaj birkaç saniye sonra kaybolur,
		// "seçtiğim mod yeniden açınca gelmiyor" şikâyetinin izi kalmalı.
		fmt.Fprintf(os.Stderr, "mcos-panel-fb: ekran modu kaydedilemedi (%s): %v\n",
			displayConfPath, err)
		a.Fail("mod kaydedilemedi — yeniden açılışta eski moda dönülecek", err)
		return false
	}
	a.mu.Lock()
	a.displayPref = fmt.Sprintf("%dx%d", m.Width, m.Height)
	a.mu.Unlock()
	if a.headless {
		return true
	}
	// Açılış (GRUB) çözünürlüğünü de eşitle: logo ekranı da aynı boyutta
	// gelsin. Boot bölümünü bağlamak saniyeler sürebilir — arka planda,
	// ve başarısızlığı yalnızca bilgi (canlı mod zaten uygulandı).
	res := fmt.Sprintf("%dx%d", m.Width, m.Height)
	go func() {
		if _, err := runHelper("mcos-display", "set", res); err != nil {
			a.Emit(fbui.EventInfo, "Açılış çözünürlüğü güncellenemedi: "+err.Error())
		}
	}()
	return true
}

// modeConfirmModal counts down and reverts unless the user keeps the mode.
type modeConfirmModal struct {
	yeni, onceki drm.Mode
	auto         bool
	bitis        time.Time
	focused      int // 0 = Koru, 1 = Geri al
	bitti        bool
}

func (m *modeConfirmModal) Title() string { return "Ekran modu değişti" }

func (m *modeConfirmModal) Size() (int, int) { return 54, 11 }

// kalan returns the whole seconds left, rounded UP (son saniyede "0" değil
// "1" görünsün; sıfır, geri almanın olmakta olduğu an demek).
func (m *modeConfirmModal) kalan() int {
	d := m.bitis.Sub(nowFunc())
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func (m *modeConfirmModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	y := r.Min.Y
	u.Text(r.Min.X, y, "Yeni mod: "+m.yeni.String(), u.Pal.Text)
	y += u.F.CellH + u.M.PadY
	u.Text(r.Min.X, y, "Önceki:   "+m.onceki.String(), u.Pal.TextDim)
	y += u.F.CellH + u.M.PadY*2
	u.Text(r.Min.X, y, "Görüntü düzgünse korumak için Enter'a basın.", u.Pal.Text)
	y += u.F.CellH + u.M.PadY
	u.Text(r.Min.X, y, fmt.Sprintf("%d saniye içinde onay gelmezse önceki moda dönülecek.", m.kalan()), u.Pal.Warn)

	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Koru", Key: map[bool]string{true: "Enter", false: "Tab"}[m.focused == 0], Style: fbui.ButtonPrimary},
		{Label: fmt.Sprintf("Geri al (%d)", m.kalan()), Key: map[bool]string{true: "Enter", false: "Esc"}[m.focused == 1], Style: fbui.ButtonSecondary},
	}, m.focused)
	a.addModalButtons(btns)
}

func (m *modeConfirmModal) Key(a *App, key string) bool {
	switch key {
	case "esc":
		m.revert(a)
		return true
	case "confirm":
		m.keep(a)
		return true
	case "left", "h", "right", "l", "tab":
		m.focused = 1 - m.focused
	case "enter":
		if m.focused == 0 {
			m.keep(a)
		} else {
			m.revert(a)
		}
		return true
	}
	return false
}

// Animating keeps the countdown ticking on screen.
func (m *modeConfirmModal) Animating(bool) bool { return !m.bitti }

func (m *modeConfirmModal) keep(a *App) {
	if m.bitti {
		return
	}
	m.bitti = true
	// ── Yakalanan gerçek hata ─────────────────────────────────────────
	// Burada kayıt sonucuna bakılmadan "kaydedildi" yazılıyordu: kayıt
	// başarısız olunca hata mesajı bir an görünüp bu yeşil mesajla
	// eziliyordu. QEMU'da tam olarak böyle görüldü — ekran "kaydedildi"
	// diyordu, diske hiçbir şey inmemişti.
	if a.keepDisplayMode(m.yeni, m.auto) {
		a.Emit(fbui.EventOK, "Ekran modu kaydedildi: "+m.yeni.String())
	}
}

func (m *modeConfirmModal) revert(a *App) {
	if m.bitti {
		return
	}
	m.bitti = true
	if ld := a.liveDisplay(); ld != nil {
		if err := ld.SetMode(m.onceki.Key()); err != nil {
			a.Fail("önceki moda dönülemedi", err)
			return
		}
	}
	a.Emit(fbui.EventInfo, "Önceki moda dönüldü: "+m.onceki.String())
}

// displayTick handles the confirm timeout. ANA DÖNGÜDE çağrılır.
//
// Onay penceresi açıkken kullanıcı hiçbir tuşa basmazsa (ekran karardığı
// için göremiyorsa) süre dolunca eski mod geri gelir. Döngünün animasyon
// tikinden çağrılıyor ki tuş gelmese de işlesin.
func (a *App) displayTick() bool {
	m, ok := a.ActiveModal().(*modeConfirmModal)
	if !ok || m.bitti {
		return false
	}
	if nowFunc().Before(m.bitis) {
		return true // geri sayım ekranda akmalı
	}
	m.revert(a)
	a.closeModalIf(m)
	return true
}

// ── Çizim ───────────────────────────────────────────────────────────────────

// drawDisplayLive renders the live (DRM) display section.
func (a *App) drawDisplayLive(r image.Rectangle, info drm.Info) {
	u := a.ui
	in := a.contentPanel(r, "Ekran")
	y := in.Min.Y

	u.Text(in.Min.X, y, "ŞU AN", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	hz := info.Current.HzText() + " Hz"
	if info.FPS >= 1 {
		// Ölçülen hız yalnızca hareket varken anlamlı: panel boştayken kare
		// üretmez. Yine de gösteriyoruz — kullanıcı "tazeleme kötü" dediğinde
		// monitörün değil panelin ne yaptığını görebilmeli.
		hz += fmt.Sprintf("   (son saniye: %.0f kare)", info.FPS)
	}
	cikis := info.Connector
	if len(info.Outputs) > 1 {
		cikis = strings.Join(info.Outputs, ",  ")
	}
	y = a.kvList(in, y, 16, [][3]any{
		{"Çözünürlük", fmt.Sprintf("%d x %d", info.Current.Width, info.Current.Height), u.Pal.Accent},
		{"Tazeleme", hz, u.Pal.Accent},
		{"Ekran kartı", strings.TrimSpace(info.Driver + "  " + info.Device), u.Pal.Text},
		{"Çıkış", cikis, u.Pal.Text},
		{"Yöntem", info.Method, u.Pal.Text},
		{"Seçim nedeni", info.Why, u.Pal.TextDim},
	})

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	u.Text(in.Min.X, y, "MODLAR", u.Pal.TextFaint)
	u.TextRight(in.Max.X-u.M.PadX, y, "Enter: hemen uygula (15 sn içinde onay)", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY

	rows := liveRows(info)
	altPay := u.F.CellH * 2
	sigan := (in.Max.Y - altPay - y) / u.M.RowH
	if sigan < 1 {
		sigan = 1
	}
	// İmleci görünür tutan kaydırma: liste ekrana sığmıyorsa (gerçek
	// monitörler 30+ mod bildirir) imlecin çevresi gösterilir.
	cur := a.Cursor()
	bas := 0
	if cur >= sigan {
		bas = cur - sigan + 1
	}
	if bas+sigan > len(rows) {
		bas = max(0, len(rows)-sigan)
	}
	oncekiCoz := ""
	if bas > 0 {
		oncekiCoz = rowRes(rows[bas-1])
	}
	for i := bas; i < len(rows) && i < bas+sigan; i++ {
		rw := rows[i]
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx, col := a.contentRow(row, i)
		ty := y + (u.M.RowH-u.F.CellH)/2

		if rw.auto {
			best, _ := drm.Choose(info.Modes, info.ConnType, "")
			u.Radio(cx, ty, false)
			u.Text(cx+u.F.CellW+u.M.Gap, ty, "Otomatik — en iyi mod", col)
			u.TextRight(in.Max.X-u.M.PadX, ty,
				fmt.Sprintf("%d x %d · %s Hz", best.Width, best.Height, best.HzText()), u.Pal.TextDim)
			y += u.M.RowH
			continue
		}
		m := rw.mode
		coz := rowRes(rw)
		etkin := m.Key() == info.Current.Key()
		u.Radio(cx, ty, etkin)
		tx := cx + u.F.CellW + u.M.Gap
		// Aynı çözünürlüğün ikinci ve sonraki tazelemelerinde çözünürlük
		// tekrar yazılmıyor: liste "hangi çözünürlükte hangi hızlar var"
		// sorusunu tek bakışta yanıtlasın.
		if coz != oncekiCoz {
			u.Text(tx, ty, fmt.Sprintf("%d x %d", m.Width, m.Height), col)
		}
		u.Text(tx+u.F.CellW*14, ty, m.HzText()+" Hz", col)
		switch {
		case etkin:
			u.TextRight(in.Max.X-u.M.PadX, ty, "şu an", u.Pal.OK)
		case m.Preferred:
			u.TextRight(in.Max.X-u.M.PadX, ty, "monitörün önerdiği", u.Pal.TextDim)
		}
		oncekiCoz = coz
		y += u.M.RowH
	}
	if len(rows) > sigan {
		u.TextRight(in.Max.X-u.M.PadX, in.Max.Y-u.F.CellH,
			fmt.Sprintf("%d / %d", min(cur+1, len(rows)), len(rows)), u.Pal.TextFaint)
	}
}

func rowRes(r displayRow) string {
	if r.auto {
		return ""
	}
	return fmt.Sprintf("%dx%d", r.mode.Width, r.mode.Height)
}
