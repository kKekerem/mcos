package fbpanel

import (
	"image"
	"math"
	"os/exec"
	"time"
	"unicode"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
)

// ── Yazma animasyonu: süreler ───────────────────────────────────────────────
//
// Hepsi 400 ms'nin altında. Bunun bir sebebi var: metin alanı bir GERİ
// BİLDİRİM yüzeyidir, bir gösteri değil. Tuşa basıldıktan 400 ms sonra hâlâ
// hareket eden bir arayüz, hızlı yazan kullanıcıyı geride bırakır ve
// "takılıyor" hissi verir. Ölçü şu: animasyon, bir sonraki tuşa basılana
// kadar BİTMİŞ olmalı; ortalama yazma hızı 5 karakter/sn, yani 200 ms.
const (
	// typePopDur, yeni giren işaretin yerine oturma süresi.
	typePopDur = 150 * time.Millisecond
	// delGhostDur, silinen işaretin sönüp kaybolma süresi.
	delGhostDur = 190 * time.Millisecond
	// clearDur, Ctrl+U ile hepsinin dalga hâlinde silinme süresi.
	clearDur = 260 * time.Millisecond
	// clearStagger, temizlemede işaretler arası gecikme. Sağdan sola değil
	// SOLDAN SAĞA dalga: göz satırı zaten soldan sağa okur.
	clearStagger = 14 * time.Millisecond
	// shakeDur, doğrulama hatasında alanın sarsılma süresi.
	shakeDur = 320 * time.Millisecond
	// caretGlideDur, imlecin yeni konumuna kayma süresi. typePopDur'dan
	// KISA: imleç, işaret yerine oturmadan önce varmalı, yoksa imleç
	// hareketi gecikmiş görünür.
	caretGlideDur = 95 * time.Millisecond
	// caretSolidAfterKey, tuşa bastıktan sonra imlecin yanıp SÖNMEDİĞİ süre.
	//
	// Gerçek metin düzenleyiciler bunu yapar ve sebebi pratiktir: yazarken
	// yanıp sönen bir imleç, yazılan karakterin yanında kaybolup görünerek
	// göz yorar. Yazma durunca yanıp sönme geri gelir ve "burası yazılabilir"
	// mesajını taşır.
	caretSolidAfterKey = 650 * time.Millisecond
)

// TextModal collects one line of text over a blurred backdrop.
//
// ── Neden ayrı bir tür? ─────────────────────────────────────────────────────
// Metin girişinin listeden TAMAMEN farklı kuralları var: her yazdırılabilir
// karakter alana gider, hiçbiri kısayol sayılmaz. Eski panelde tam tersi
// yapılıyordu ve "h", "l", boşluk alana hiç ulaşmıyordu — kullanıcı "salon"
// yazınca "saon" oluyordu.
//
// ── Neden tek tür, birçok kullanım? ─────────────────────────────────────────
// Wi-Fi parolası, panel parolası, elle IP girişi, sunucu adı… hepsi aynı
// şeydir: başlık, açıklama, bir alan, iki düğme. Her biri için ayrı pencere
// yazmak, dördünün de farklı davranmasıyla sonuçlanırdı (birinde Ctrl+G
// çalışır, öbüründe çalışmaz gibi).
type TextModal struct {
	title  string
	prompt string
	// masked ise girilen metin nokta olarak çizilir.
	masked bool
	// show, masked bir alanda metni geçici olarak açar (Ctrl+G).
	show bool
	// maxLen karakter sınırı (0 = 128).
	maxLen int
	// placeholder alan boşken gösterilen soluk örnek.
	placeholder string
	// hint alanın altındaki açıklama satırı (boş olabilir).
	hint string
	// okLabel onay düğmesinin metni.
	okLabel string

	value []rune
	// errText, validate'in reddettiği girişten sonra gösterilir.
	errText string

	// validate boş olmayan bir hata döndürürse pencere KAPANMAZ.
	//
	// Doğrulamayı pencerenin içinde yapmak şart: kullanıcı geçersiz bir IP
	// girdiğinde pencere kapanıp alt çubukta bir hata belirirse, yazdığı
	// metni kaybeder ve baştan yazmak zorunda kalır.
	validate func(s string) string
	onDone   func(a *App, text string)

	// ── Animasyon durumu ────────────────────────────────────────────────
	//
	// Hepsi DUVAR SAATİ. Kare sayacı kullanılmadı çünkü panelin iki ayrı
	// tiki var (80 ms animasyon, 16 ms kare) ve yazma animasyonu hızlı
	// olanla çizilir; kare sayacına bağlanan bir süre, hangi tikle
	// çizildiğine göre farklı hızda akardı.

	// typedAt, son karakterin eklendiği an. Sıfır değer = animasyon yok.
	typedAt time.Time
	// delAt / delIdx / delRune, silinen son karakterin hayaleti.
	delAt   time.Time
	delIdx  int
	delRune rune
	// clearAt / clearN, Ctrl+U ile topluca silinenler.
	clearAt time.Time
	clearN  int
	// shakeAt, alanın sarsıldığı an (reddedilen giriş).
	shakeAt time.Time
	// caretAt / caretFrom, imlecin kaymakta olduğu aralık (rune indisi).
	caretAt   time.Time
	caretFrom int
	// lastKeyAt, imlecin yanıp sönmesini geçici olarak durdurmak için.
	lastKeyAt time.Time
}

// NewTextModal builds a single-line input dialog.
func NewTextModal(title, prompt string, onDone func(a *App, text string)) *TextModal {
	return &TextModal{title: title, prompt: prompt, onDone: onDone,
		okLabel: "Tamam", maxLen: 128}
}

// Masked makes the field a password field.
func (m *TextModal) Masked() *TextModal { m.masked = true; return m }

// WithValue pre-fills the field.
func (m *TextModal) WithValue(s string) *TextModal { m.value = []rune(s); return m }

// WithPlaceholder sets the faint example shown while empty.
func (m *TextModal) WithPlaceholder(s string) *TextModal { m.placeholder = s; return m }

// WithHint sets an explanatory line under the field.
func (m *TextModal) WithHint(s string) *TextModal { m.hint = s; return m }

// WithOK sets the confirm button label.
func (m *TextModal) WithOK(s string) *TextModal { m.okLabel = s; return m }

// WithMaxLen caps the input length.
func (m *TextModal) WithMaxLen(n int) *TextModal { m.maxLen = n; return m }

// WithValidate installs a validator; a non-empty return keeps the dialog open.
func (m *TextModal) WithValidate(f func(string) string) *TextModal {
	m.validate = f
	return m
}

// Value returns the current text.
func (m *TextModal) Value() string { return string(m.value) }

// Title implements Modal.
func (m *TextModal) Title() string { return m.title }

// Size implements Modal.
func (m *TextModal) Size() (int, int) {
	w := 48
	for _, l := range []string{m.prompt, m.hint} {
		if n := len([]rune(l)) + 8; n > w {
			w = n
		}
	}
	if w > 80 {
		w = 80
	}
	h := 12
	if m.hint != "" {
		h += 2
	}
	if m.masked {
		h += 2 // "parolayı göster" satırı
	}
	return w, h
}

// Animating implements Animated: bu pencere yeniden çizilmeli mi?
//
// fast=true, 60 kare/sn dalıdır ve YALNIZCA kısa süreli hareketler için
// istenir. fast=false, 12 kare/sn dalıdır; imleç yanıp sönmesi oraya aittir.
//
// Ayrımın sebebi ölçülebilir: pencere açıkken sürekli 60 kare/sn çizmek,
// üzerinde Minecraft sunucusu koşan bir makinede boşuna CPU demek. Yanıp
// sönme saniyede 12 kareyle de aynı görünür; yazma animasyonu görünmez.
func (m *TextModal) Animating(fast bool) bool {
	if !fast {
		// İmleç her zaman ya yanıp söner ya da kayar: yavaş dal hep açık.
		return true
	}
	now := time.Now()
	for _, s := range []struct {
		at  time.Time
		dur time.Duration
	}{
		{m.typedAt, typePopDur},
		{m.delAt, delGhostDur},
		{m.clearAt, clearDur + time.Duration(m.clearN)*clearStagger},
		{m.shakeAt, shakeDur},
		{m.caretAt, caretGlideDur},
	} {
		if !s.at.IsZero() && now.Sub(s.at) < s.dur {
			return true
		}
	}
	return false
}

// progress returns 0..1 for an animation started at `at` lasting `dur`,
// or -1 when it is not running.
func progress(at time.Time, dur time.Duration) float64 {
	if at.IsZero() {
		return -1
	}
	el := time.Since(at)
	if el < 0 || el >= dur {
		return -1
	}
	return float64(el) / float64(dur)
}

// Draw implements Modal.
func (m *TextModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	y := r.Min.Y

	if m.prompt != "" {
		u.Text(r.Min.X, y, m.prompt, u.Pal.TextDim)
		y += u.F.CellH + u.M.PadY*2
	}

	// ── Sarsılma ────────────────────────────────────────────────────────
	//
	// Reddedilen bir girişte alan yatayda birkaç kez gider gelir. Bu, "hayır"
	// anlamına gelen evrensel bir jesttir ve hata METNİNDEN önce algılanır:
	// kullanıcı yazıyı okumadan önce yanlış olduğunu anlar. Genlik kare kök
	// gibi değil DOĞRUSAL sönüyor ve üç tam salınım yapıyor — daha fazlası
	// oyuncak gibi görünür.
	shakeDX := 0
	if p := progress(m.shakeAt, shakeDur); p >= 0 {
		amp := float64(u.F.CellW) * 0.5 * (1 - p)
		shakeDX = int(math.Round(amp * math.Sin(p*3*2*math.Pi)))
	}

	// Giriş alanı: odaklı olduğu kalın vurgu çerçevesinden belli.
	fieldH := u.M.ButtonH
	border := u.Pal.BorderFocus
	if m.errText != "" {
		border = u.Pal.Error
	}
	fieldBox := image.Rect(r.Min.X+shakeDX, y, r.Max.X+shakeDX, y+fieldH)
	u.P.StrokeRoundRect(rect(fieldBox), u.M.RadiusSmall, u.M.StrokeFocus, border)

	tx := fieldBox.Min.X + u.M.PadX
	ty := y + (fieldH-u.F.CellH)/2

	switch {
	case len(m.value) == 0 && m.placeholder != "" && !m.animatingRemoval():
		u.Text(tx, ty, m.placeholder, u.Pal.TextFaint)
	case m.masked && !m.show:
		m.drawDots(u, tx, ty)
	default:
		m.drawGlyphs(u, tx, ty)
	}

	m.drawCaret(u, tx, ty)
	y += fieldH + u.M.PadY

	if m.errText != "" {
		u.WarnTriangle(r.Min.X, y, u.Pal.Error)
		u.Text(r.Min.X+u.F.CellW+u.M.Gap, y, m.errText, u.Pal.Error)
		y += u.F.CellH + u.M.PadY/2
	} else if m.hint != "" {
		u.Text(r.Min.X, y, m.hint, u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
	}

	if m.masked {
		u.Check(r.Min.X, y, m.show)
		u.Text(r.Min.X+u.F.CellW+u.M.Gap, y, "Göster  (Ctrl+G)", u.Pal.TextDim)
	}

	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: m.okLabel, Key: "Enter", Style: fbui.ButtonPrimary},
		{Label: "Vazgeç", Key: "Esc", Style: fbui.ButtonSecondary},
	}, 0)
	a.addModalButtons(btns)
}

// animatingRemoval reports whether a delete/clear ghost is still on screen.
//
// Alan BOŞ ama hayalet hâlâ çiziliyorsa örnek metin (placeholder) YAZILMAZ:
// yoksa son karakter silinirken örnek metin onun üstüne biner ve iki yazı
// üst üste görünür.
func (m *TextModal) animatingRemoval() bool {
	return progress(m.delAt, delGhostDur) >= 0 ||
		progress(m.clearAt, clearDur+time.Duration(m.clearN)*clearStagger) >= 0
}

// dotRadius is the resting radius of one masked-input mark.
func dotRadius(u *fbui.UI) float64 { return float64(u.F.CellW) * 0.24 }

// drawDots paints the masked field as animated circles.
//
// ── Neden metin değil de daire? ─────────────────────────────────────────────
// Eskiden burada strings.Repeat("•", n) vardı ve tek parça metin olarak
// çiziliyordu. Metin ÖLÇEKLENEMEZ ve tek tek saydamlaştırılamaz: font hücresi
// sabittir. Yani "yeni giren işaret yerine otursun, silinen sönerek kaybolsun"
// istendiği anda metin yolu çalışmaz. Daire, yarıçapı ve saydamlığı serbest
// olan tek çizim ilkesidir; üstelik hücre ızgarasında aynı yeri kaplar, yani
// hizalama bozulmaz.
func (m *TextModal) drawDots(u *fbui.UI, tx, ty int) {
	baseR := dotRadius(u)
	cy := float64(ty) + float64(u.F.CellH)/2
	cellW := float64(u.F.CellW)

	pop := progress(m.typedAt, typePopDur)
	for i := range m.value {
		cx := float64(tx) + (float64(i)+0.5)*cellW
		if i == len(m.value)-1 && pop >= 0 {
			// Son işaret: büyük ve soluk başlar, yerine oturur.
			e := fbdraw.EaseOutCubic(pop)
			rad := baseR * (1.85 - 0.85*e)
			c := fbdraw.Blend(u.Pal.Accent, u.Pal.Text, e)
			u.P.FillCircle(cx, cy, rad, fbdraw.Alpha(c, 0.35+0.65*e))
			continue
		}
		u.P.FillCircle(cx, cy, baseR, u.Pal.Text)
	}

	m.drawDotGhosts(u, tx, cy, baseR, cellW)
}

// drawDotGhosts paints the fading marks of deleted characters.
func (m *TextModal) drawDotGhosts(u *fbui.UI, tx int, cy, baseR, cellW float64) {
	if p := progress(m.delAt, delGhostDur); p >= 0 {
		cx := float64(tx) + (float64(m.delIdx)+0.5)*cellW
		e := fbdraw.EaseOutCubic(p)
		u.P.FillCircle(cx, cy, baseR*(1-e), fbdraw.Alpha(u.Pal.Accent, 1-e))
		// Dışa açılan ince halka: "buradan bir şey çıktı" der. Dolu dairenin
		// tek başına küçülmesi, silinmekle uzaklaşmak arasında ayrım bırakmaz.
		u.P.StrokeCircle(cx, cy, baseR*(1+2.2*e), 1, fbdraw.Alpha(u.Pal.Accent, 0.5*(1-e)))
	}

	total := clearDur + time.Duration(m.clearN)*clearStagger
	if p := progress(m.clearAt, total); p >= 0 {
		el := time.Since(m.clearAt)
		for i := 0; i < m.clearN; i++ {
			// Soldan sağa dalga.
			q := float64(el-time.Duration(i)*clearStagger) / float64(clearDur)
			if q <= 0 {
				q = 0
			}
			if q >= 1 {
				continue
			}
			e := fbdraw.EaseOutCubic(q)
			cx := float64(tx) + (float64(i)+0.5)*cellW
			u.P.FillCircle(cx, cy-6*e, baseR*(1-e), fbdraw.Alpha(u.Pal.Accent, 1-e))
		}
	}
}

// drawGlyphs paints a visible (unmasked) value with the same entrance cues.
//
// Metin ölçeklenemediği için buradaki hareket SAYDAMLIK ve RENK üzerinden
// yapılır: yeni karakter vurgu renginden metin rengine geçerek belirir.
// Daha azı değil — daha fazlası da değil: harf zıplatmak okumayı zorlaştırır.
func (m *TextModal) drawGlyphs(u *fbui.UI, tx, ty int) {
	pop := progress(m.typedAt, typePopDur)
	last := len(m.value) - 1
	for i, r := range m.value {
		x := tx + i*u.F.CellW
		c := u.Pal.Text
		if i == last && pop >= 0 {
			e := fbdraw.EaseOutCubic(pop)
			c = fbdraw.Alpha(fbdraw.Blend(u.Pal.Accent, u.Pal.Text, e), 0.45+0.55*e)
		}
		u.Text(x, ty, string(r), c)
	}

	if p := progress(m.delAt, delGhostDur); p >= 0 && m.delRune != 0 {
		e := fbdraw.EaseOutCubic(p)
		u.Text(tx+m.delIdx*u.F.CellW, ty, string(m.delRune),
			fbdraw.Alpha(u.Pal.Accent, 1-e))
	}
}

// drawCaret paints the insertion bar, gliding to its new column.
//
// ── Neden kayıyor? ──────────────────────────────────────────────────────────
// Zıplayan bir imleç, hızlı yazarken nerede olduğunu kaybettirir; göz onu
// her karakterde yeniden aramak zorunda kalır. 95 ms'lik bir kayma, gözün
// takip edebileceği en kısa süredir ve yazma hızını hiç geciktirmez.
func (m *TextModal) drawCaret(u *fbui.UI, tx, ty int) {
	target := len(m.value)
	pos := float64(target)
	if p := progress(m.caretAt, caretGlideDur); p >= 0 {
		e := fbdraw.EaseOutCubic(p)
		pos = float64(m.caretFrom) + (float64(target)-float64(m.caretFrom))*e
	}
	cx := float64(tx) + pos*float64(u.F.CellW)

	// Yazarken SABİT, boştayken yanıp sönen.
	solid := !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < caretSolidAfterKey
	if !solid {
		// ~1.4 sn periyot, %60 açık: standart metin imleci ritmi.
		ph := math.Mod(float64(time.Now().UnixNano())/float64(time.Second), 1.4) / 1.4
		if ph > 0.6 {
			return
		}
	}
	u.P.VLine(float64(ty), float64(ty+u.F.CellH), cx, 2, u.Pal.Accent)
}

// beginCaretMove records where the caret was, so Draw can glide it.
func (m *TextModal) beginCaretMove(from int) {
	m.caretFrom = from
	m.caretAt = time.Now()
	m.lastKeyAt = m.caretAt
}

// Key implements Modal.
//
// KURAL: yazdırılabilir HER karakter alana gider. Hiçbir harf kısayol olarak
// yorumlanmaz — parolada "h", "l", boşluk ya da "q" geçebilir.
func (m *TextModal) Key(a *App, key string) bool {
	switch key {
	case "esc":
		return true
	case "enter":
		s := string(m.value)
		if m.validate != nil {
			if msg := m.validate(s); msg != "" {
				m.errText = msg
				// Sarsılma, hata metninden ÖNCE algılanır: kullanıcı yazıyı
				// okumadan girişin reddedildiğini anlar.
				m.shakeAt = time.Now()
				return false // pencere AÇIK kalır, metin korunur
			}
		}
		if m.onDone != nil {
			m.onDone(a, s)
		}
		return true
	case "backspace":
		if len(m.value) > 0 {
			m.delIdx = len(m.value) - 1
			m.delRune = m.value[m.delIdx]
			m.delAt = time.Now()
			m.beginCaretMove(len(m.value))
			m.value = m.value[:m.delIdx]
			m.errText = ""
			// Yeni bir silme, süren bir yazma animasyonunu iptal eder:
			// ikisi aynı anda çalışırsa silinen işaretin yerinde hem
			// hayalet hem "yeni giren" işaret çizilirdi.
			m.typedAt = time.Time{}
		}
		return false
	case "ctrl+u":
		if n := len(m.value); n > 0 {
			m.clearN = n
			m.clearAt = time.Now()
			m.beginCaretMove(n)
			m.value = m.value[:0]
			m.typedAt = time.Time{}
			m.delAt = time.Time{}
		}
		m.errText = ""
		return false
	case "ctrl+g":
		if m.masked {
			m.show = !m.show
			m.lastKeyAt = time.Now()
		}
		return false
	case "space":
		// Bazı çözücüler boşluğu ad olarak bildirir; parolada boşluk
		// geçerlidir, yutulmamalı.
		m.insert(' ')
		return false
	}

	// Tek karakterlik tuşlar metindir. Çok karakterli adlar ("up", "tab"…)
	// özel tuşlardır ve yok sayılır.
	rs := []rune(key)
	if len(rs) == 1 && unicode.IsPrint(rs[0]) {
		m.insert(rs[0])
	}
	return false
}

// insert appends one rune and starts its entrance animation.
//
// Sınıra dayanıldığında karakter SESSİZCE yutulmaz: alan sarsılır. Sessiz
// yutma, kullanıcıya "tuşum çalışmıyor" dedirten türden bir arızadır —
// özellikle maskeli bir alanda, çünkü orada yazdığını okuyup sayamaz.
func (m *TextModal) insert(r rune) {
	if len(m.value) >= m.limit() {
		m.shakeAt = time.Now()
		m.lastKeyAt = m.shakeAt
		return
	}
	m.beginCaretMove(len(m.value))
	m.value = append(m.value, r)
	m.typedAt = time.Now()
	m.delAt = time.Time{}
	m.errText = ""
}

func (m *TextModal) limit() int {
	if m.maxLen <= 0 {
		return 128
	}
	return m.maxLen
}

// ── Wi-Fi parolası ──────────────────────────────────────────────────────────

// NewPasswordModal builds the Wi-Fi password prompt for a secured network.
//
// TextModal üstünde ince bir sarmalayıcı: Wi-Fi'ye özgü tek şey başlık,
// açıklama ve 63 karakterlik WPA2 sınırıdır.
func NewPasswordModal(ssid string, onDone func(a *App, pass string)) Modal {
	return NewTextModal(ssid+" ağına bağlan",
		"Bu ağ korumalı. Parolayı girin.", onDone).
		Masked().
		WithOK("Bağlan").
		WithMaxLen(63). // WPA2 parola üst sınırı
		WithHint("Parola en az 8 karakter olmalı.")
}

// runHelper executes a small system helper script and returns its output.
//
// Panel bölüm bağlamak, önyükleyici dosyası yazmak gibi işleri KENDİSİ
// yapmaz: bunlar ayrı, test edilebilir kabuk programlarının işidir. Panel
// yalnızca çağırır ve çıktısını kullanıcıya gösterir.
func runHelper(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}
