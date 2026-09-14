package fbui

import (
	"image"
	"image/color"
	"math"
	"time"

	"mcos/internal/fbdraw"
)

// Bu dosya ekranı ÇEVRELEYEN parçaları çizer: alt durum çubuğu, açılır pencere
// perdesi ve tema seçimi. Bunlar her ekranda ortaktır; tek yerde tanımlı
// olmaları, ekranlar arasında kayma olmasını engeller.

// ── Temalar ─────────────────────────────────────────────────────────────────

// ThemeOrder is the selectable theme order. panel/theme.Names() ile AYNI
// sırada olmak zorunda: kullanıcı eski panelde 3. temayı seçtiyse yeni panelde
// de 3. tema gelmeli, yoksa yükseltmede tema sessizce değişir.
var ThemeOrder = []string{
	"graphite-teal", "noir-purple", "anthracite-orange",
	"anthracite-green", "crimson-night", "amber-graphite",
}

// themeLabels are the Turkish display names (panel/theme ile aynı).
var themeLabels = map[string]string{
	"graphite-teal":     "Grafit Teal",
	"noir-purple":       "Noir Mor",
	"anthracite-orange": "Antrasit Turuncu",
	"anthracite-green":  "Antrasit Yeşil",
	"crimson-night":     "Gece Kızılı",
	"amber-graphite":    "Kehribar Grafit",
}

// ThemeLabel returns the display name for a theme id.
func ThemeLabel(name string) string {
	if l, ok := themeLabels[name]; ok {
		return l
	}
	return name
}

// ValidTheme reports whether name is a known theme.
func ValidTheme(name string) bool {
	_, ok := Accents[name]
	return ok
}

// ── Açılır pencere perdesi ──────────────────────────────────────────────────

// Scrim blurs and dims a region so a dialog can sit on top of it.
//
// Kullanıcının isteği: "tamamen siyah olmasın hafif karartma olsun". Arkadaki
// ekran DURUR, sadece bulanıklaşıp hafifçe koyulaşır — böylece kullanıcı
// hangi ekranın üstünde olduğunu kaybetmez.
//
// Terminalde bu mümkün değildi: hücrenin altında "içerik" diye bir şey yok.
//
// MALİYET: 1920x1080 için ~50 ms (ölçüldü). Bu yüzden pencere AÇILIRKEN bir
// kez hesaplanıp saklanmalı, her karede değil — ScrimCache bunu yapar.
func (u *UI) Scrim(r image.Rectangle) {
	// Yarıçap hücre yüksekliğinden türetilir: düşük çözünürlükte aşırı
	// bulanıklık okunaksız, yüksek çözünürlükte az bulanıklık etkisiz olurdu.
	//
	// AYARLANDI: önce cellH*3/4 idi ve arka plan tamamen tanınmaz hale
	// geliyordu. Amaç arkadaki ekranı YOK ETMEK değil, geri plana itmek —
	// kullanıcı hangi ekranın üstünde olduğunu görmeye devam etmeli.
	radius := u.F.CellH / 4
	if radius < 3 {
		radius = 3
	}
	fbdraw.Blur(u.dst, r, radius)
	// Karartma bulanıklıktan SONRA: önce karartsaydık bulanıklık karartmayı
	// kenarlardan geri yayardı ve perde kenarı halkalanırdı.
	fbdraw.Dim(u.dst, r, u.Pal.Bg, 0.25)
}

// ScrimCache stores a pre-rendered blurred backdrop.
//
// Açılır pencere açıkken arkaplan değişmez, bu yüzden bulanıklığı her karede
// yeniden hesaplamak saf israftır. Invalidate() ile geçersiz kılınır.
type ScrimCache struct {
	img   *image.RGBA
	valid bool
}

// Capture blurs the current canvas and stores the result.
func (c *ScrimCache) Capture(u *UI) {
	b := u.dst.Bounds()
	if c.img == nil || c.img.Bounds() != b {
		c.img = image.NewRGBA(b)
	}
	copy(c.img.Pix, u.dst.Pix)
	tmp := *u
	tmp.dst = c.img
	tmp.P = fbdraw.New(c.img)
	tmp.Scrim(b)
	c.valid = true
}

// Restore paints the cached backdrop onto the canvas. Returns false if the
// cache is empty, in which case the caller must Capture first.
func (c *ScrimCache) Restore(u *UI) bool {
	if !c.valid || c.img == nil || c.img.Bounds() != u.dst.Bounds() {
		return false
	}
	copy(u.dst.Pix, c.img.Pix)
	return true
}

// Invalidate drops the cached backdrop (call when the dialog closes).
func (c *ScrimCache) Invalidate() { c.valid = false }

// Modal draws a centred dialog over a scrimmed background and returns its
// content rectangle.
//
// w ve h piksel cinsindendir; ekrandan büyükse kenar boşluğu bırakacak şekilde
// kırpılır — küçük çözünürlükte pencerenin ekran dışına taşmasını engeller.
func (u *UI) Modal(w, h int, title string) image.Rectangle {
	b := u.dst.Bounds()
	margin := u.M.PadX * 2

	// ALT SINIR: ekranin dortte biri. Hucre sayisina gore olculen bir pencere
	// yuksek cozunurlukte kaybolacak kadar kucuk kalir - 1920 pikselde 44
	// sutun yalnizca 528 piksel eder. Pencere her zaman fark edilir olmali.
	if min := b.Dx() * 34 / 100; w < min {
		w = min
	}
	if w > b.Dx()-margin {
		w = b.Dx() - margin
	}
	if h > b.Dy()-margin {
		h = b.Dy() - margin
	}
	x := b.Min.X + (b.Dx()-w)/2
	y := b.Min.Y + (b.Dy()-h)/2
	rc := image.Rect(x, y, x+w, y+h)

	// Pencerenin altına yumuşak bir gölge: perdeden ayrıldığını gösterir.
	u.shadow(rc)
	return u.Panel(rc, title, true)
}

// shadow draws a soft drop shadow under r.
//
// Tek bir yarı saydam dikdörtgen yerine giderek soluklaşan birkaç halka:
// gerçek gölge kenarı keskin olmaz.
func (u *UI) shadow(r image.Rectangle) {
	steps := 6
	for i := steps; i >= 1; i-- {
		g := float64(i)
		alpha := 0.05 * (1 - float64(i-1)/float64(steps))
		u.P.FillRoundRect(
			fbdraw.R(float64(r.Min.X)-g, float64(r.Min.Y)-g+g*0.6,
				float64(r.Dx())+2*g, float64(r.Dy())+2*g),
			u.M.Radius+g, fbdraw.Alpha(color.RGBA{A: 255}, alpha))
	}
}

// ── Alt durum çubuğu ────────────────────────────────────────────────────────

// EventKind classifies a status message.
type EventKind int

const (
	// EventInfo: nötr bilgi ("sunucu açılıyor").
	EventInfo EventKind = iota
	// EventOK: başarı ("sunucu başladı").
	EventOK
	// EventWarn: dikkat ("bağlantı yok, çevrimdışı kipte").
	EventWarn
	// EventError: hata ("sunucu çöktü").
	EventError
	// EventBusy: sürüyor; dönen gösterge ile çizilir.
	EventBusy
)

// Event is one line in the live activity feed.
type Event struct {
	Kind EventKind
	Text string
	At   time.Time
}

// Shortcut is one key hint shown on the right of the status bar.
type Shortcut struct {
	Key   string // "Enter", "F2", "^S"
	Label string // "Başlat"
}

// StatusBarH returns the height the status bar occupies.
//
// Ekran düzeni bunu içerik alanından DÜŞMELİ; yoksa çubuk içeriğin üstüne
// biner. Tek bir yerden hesaplanması bu hatayı imkânsız kılar.
func (u *UI) StatusBarH() int { return u.F.CellH + u.M.PadY*2 }

// StatusBar draws the bottom bar: live event on the left, shortcuts on the right.
//
// Kullanıcının isteği: "altta biraz alan olacak oradan girdiğimiz yerin
// kısayolu ve su an olan şey olacak". Yani iki iş birden: NEREDEYİZ (kısayollar)
// ve NE OLUYOR (canlı olay).
//
// ev nil ise sol taraf boş bırakılır.
//
// İki değer döner: çubuğun kendisi ve her kısayol tuş kapağının dikdörtgeni.
// İkincisi fare desteği içindir — kullanıcı alttaki "Enter Başlat" kapağına
// tıklayabilmeli. Sıra keys ile AYNIDIR.
func (u *UI) StatusBar(ev *Event, keys []Shortcut, spin int) (image.Rectangle, []image.Rectangle) {
	b := u.dst.Bounds()
	h := u.StatusBarH()
	bar := image.Rect(b.Min.X, b.Max.Y-h, b.Max.X, b.Max.Y)

	u.P.Fill(bar, u.Pal.Surface)
	// Üst kenarda ince ayırıcı: çubuğun içerikten ayrıldığı belli olsun.
	u.P.HLine(float64(bar.Min.X), float64(bar.Max.X), float64(bar.Min.Y),
		u.M.DividerStroke, u.Pal.Divider)

	ty := bar.Min.Y + (h-u.F.CellH)/2
	x := bar.Min.X + u.M.PadX

	if ev != nil {
		col, dot := u.EventColors(ev.Kind)
		if ev.Kind == EventBusy {
			u.Spinner(x, ty, spin, dot)
		} else {
			u.StatusDot(x, ty, dot)
		}
		x += u.F.CellW + u.M.Gap
		u.Text(x, ty, ev.Text, col)
	}

	// Kısayollar sağdan sola dizilir: en sağdaki her zaman görünür kalır.
	caps := make([]image.Rectangle, len(keys))
	rx := bar.Max.X - u.M.PadX
	for i := len(keys) - 1; i >= 0; i-- {
		var capR image.Rectangle
		rx, capR = u.shortcut(rx, ty, keys[i])
		caps[i] = capR
		rx -= u.M.Gap * 2
	}
	return bar, caps
}

// EventColors maps a kind to (text colour, indicator colour).
func (u *UI) EventColors(k EventKind) (color.RGBA, color.RGBA) {
	switch k {
	case EventOK:
		return u.Pal.Text, u.Pal.OK
	case EventWarn:
		return u.Pal.Text, u.Pal.Warn
	case EventError:
		return u.Pal.Error, u.Pal.Error
	case EventBusy:
		return u.Pal.Text, u.Pal.Accent
	default:
		return u.Pal.TextDim, u.Pal.TextFaint
	}
}

// shortcut draws one "key label" pair right-aligned ending at x, returning the
// new left edge and the key cap's clickable rectangle.
func (u *UI) shortcut(x, y int, s Shortcut) (int, image.Rectangle) {
	lw := u.TextWidth(s.Label)
	u.Text(x-lw, y, s.Label, u.Pal.TextDim)
	x -= lw + u.M.Gap

	// Tuş kapağı: metinden ayrıldığı belli olsun diye yuvarlak zeminli.
	kw := u.TextWidth(s.Key) + u.F.CellW
	kh := u.F.CellH + u.M.PadY/2
	ky := y - u.M.PadY/4
	u.P.FillRoundRect(
		fbdraw.R(float64(x-kw), float64(ky), float64(kw), float64(kh)),
		float64(kh)*0.3, u.Pal.Raised)
	u.TextCenter(x-kw, x, y, s.Key, u.Pal.Text)
	// Tıklama alanı, kapağın çizildiği dikdörtgenin AYNISI olmalı; birkaç
	// piksel pay bırakmak, iki kapak arasına tıklandığında yanlış olanı
	// tetiklerdi.
	return x - kw, image.Rect(x-kw, ky, x, ky+kh)
}

// spinnerFrames are the rotation angles (in eighths) of the busy indicator.
//
// Karakter animasyonu (|/-\) DEĞİL: gerçek dönen yay çiziyoruz, çünkü font
// bu işaretleri sabit genişlikte çizmeyebilir ve satır kayar.
const spinnerFrames = 12

// Spinner draws a rotating arc used for in-progress events.
func (u *UI) Spinner(x, y, frame int, c color.RGBA) {
	cx := float64(x) + float64(u.F.CellW)/2
	cy := float64(y) + float64(u.F.CellH)/2
	r := u.M.MarkerR

	// Soluk tam halka + üstünde parlak kısa yay.
	u.P.StrokeCircle(cx, cy, r, u.M.Stroke*1.2, fbdraw.Alpha(c, 0.25))

	// Yayı, çemberi izleyen küçük noktalarla çiziyoruz: StrokeArc yok, ve
	// noktalar her açıda eşit kalınlıkta göründüğü için daha temiz.
	const dots = 4
	step := 2 * 3.14159265358979 / float64(spinnerFrames)
	base := float64(frame%spinnerFrames) * step
	for i := 0; i < dots; i++ {
		a := base + float64(i)*step*0.5
		fade := 1 - float64(i)/float64(dots)
		u.P.FillCircle(
			cx+r*cosf(a), cy+r*sinf(a),
			u.M.Stroke*0.9, fbdraw.Alpha(c, fade))
	}
}

func cosf(a float64) float64 { return math.Cos(a) }
func sinf(a float64) float64 { return math.Sin(a) }
