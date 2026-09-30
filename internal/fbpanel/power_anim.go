package fbpanel

import (
	"image"
	"math"
	"time"

	"mcos/internal/fbdraw"
	"mcos/internal/sound"
)

// Kapanış / yeniden başlatma animasyonu.
//
// ── Kullanıcının isteği ─────────────────────────────────────────────────────
//
//	"yeniden baslatma ve kapatma animasyonu olsun. onda da dışa zoom ile
//	 kapatılıyor animasyonu gelsin en son kapanacakken o animasyon olan menü
//	 de dışa zoom ile gitsin ekran siyah olsun."
//
// Yani İKİ aşamalı bir dışa-zoom:
//
//	1. Panel dışa zoomlanarak uzaklaşır, yerine "Kapatılıyor…" ekranı gelir.
//	2. O ekran da dışa zoomlanarak uzaklaşır ve ekran SİYAH kalır.
//
// ── Neden açılış geçişinin tersi değil ──────────────────────────────────────
//
// Açılışta ZoomBlurFade scale > 1 ile çağrılıyor: eski kare BÜYÜR, yani
// kullanıcı görüntünün İÇİNE dalar. Kapanışta scale < 1 veriyoruz: kare
// KÜÇÜLÜR, görüntü uzaklaşır. Aynı fonksiyon, ters yön — ayrı bir efekt
// yazmak iki kod yolunu zamanla ayrıştırırdı.
//
// ── Neden panelin içinde, ayrı bir program değil ────────────────────────────
//
// Kapanış animasyonunu mcos-splash'e devretmek, tam da kapanış anında YENİ
// bir süreç başlatmak demekti: framebuffer'ı devralması, konsolu alması ve
// panelin çıkmasını beklemesi gerekirdi. Panel zaten ekranın sahibi; kendi
// son karesini kendisi oynatıyor.

// powerOutroIn, panelin uzaklaşıp kapanış ekranının geldiği süre.
//
// 700 ms: göz "bir şey oldu" demeye yetiyor, kapanmak isteyen kullanıcıyı
// bekletmiyor. Toplam gecikme (giriş + bekleme + çıkış) 1,6 saniyedir ve
// zaten arkasından gelen gerçek kapanış saniyeler sürüyor.
const (
	powerOutroIn   = 700 * time.Millisecond
	powerOutroHold = 320 * time.Millisecond
	powerOutroOut  = 620 * time.Millisecond
	// powerFrame, animasyon kare aralığı (~60 kare/sn).
	powerFrame = 16 * time.Millisecond
)

// PowerLabel returns the heading shown while shutting down.
func PowerLabel(act Action) (title, note string) {
	if act == ActReboot {
		return "Yeniden başlatılıyor", "Sunucular düzgünce durduruluyor…"
	}
	return "Kapatılıyor", "Sunucular düzgünce durduruluyor…"
}

// drawPowerScreen paints the full-screen shutdown notice.
//
// Panelin hiçbir parçası çizilmez (kenar çubuğu, alt çubuk, imleç yok):
// kapanan bir makinede tıklanabilir bir arayüz göstermek yanlış olurdu.
func (a *App) drawPowerScreen(act Action, frame int) {
	u := a.ui
	b := u.Bounds()
	u.Clear()

	cx := float64(b.Min.X+b.Max.X) / 2
	cy := float64(b.Min.Y+b.Max.Y) / 2

	size := math.Min(float64(b.Dx()), float64(b.Dy())) * 0.16
	u.LogoPulse(cx, cy-size*0.5, size, frame, u.Pal.Accent)

	title, note := PowerLabel(act)
	ty := int(cy + size*0.9)
	u.TextCenter(b.Min.X, b.Max.X, ty, title, u.Pal.Text)
	u.TextCenter(b.Min.X, b.Max.X, ty+u.F.CellH*2, note, u.Pal.TextDim)

	// Belirsiz süreli bir işlem: yüzde göstermek yalan olurdu, dönen bir
	// gösterge dürüst.
	u.SpinnerAt(cx, float64(ty)+float64(u.F.CellH)*4.2,
		float64(u.F.CellH)*0.8, frame, u.Pal.Accent)
}

// PlayPowerOutro runs the two-stage zoom-out and leaves the screen black.
//
// flip, çizilen kareyi ekrana basar (panelin ana döngüsündeki Display.Flip).
// Hata dönerse animasyon sessizce kesilir: kapanmayı bir çizim hatası
// engellememeli.
func (a *App) PlayPowerOutro(flip func() error, act Action) {
	u := a.ui
	canvas := u.Canvas()
	if canvas == nil {
		return
	}
	b := canvas.Bounds()

	// Kapanış sesi EN BAŞTA: animasyonun süresi (1,6 sn) sesin süresinden
	// (0,5 sn) uzun, yani ses bitişe yetişir. Sonda çalmak, tam ekran
	// siyahken çalmak olurdu ve kullanıcı çoğu zaman duymadan makineyi
	// kapanmış sayardı.
	a.playSound(sound.Shutdown)

	// Animasyon kapalıysa: tek kare kapanış ekranı, sonra siyah. Kullanıcı
	// yine ne olduğunu görür ama hiçbir tampon ayrılmaz (1080p'de 8,3 MB × 2).
	if !a.animationsOn() {
		a.drawPowerScreen(act, 0)
		_ = flip()
		time.Sleep(powerOutroHold)
		fillBlack(canvas, b)
		_ = flip()
		return
	}

	// Üç evre de kullanıcının hız ayarına uyuyor (bkz. animDur). Oranlar
	// korunuyor: kapanış ekranının "bekleme" evresi de ölçekleniyor, yoksa
	// hızlı kipte uzaklaşma biter ama ekran yine aynı süre asılı kalırdı.
	inDur, holdDur, outDur := a.animDur(powerOutroIn), a.animDur(powerOutroHold),
		a.animDur(powerOutroOut)

	from := image.NewRGBA(b)
	copy(from.Pix, canvas.Pix)
	scratch := image.NewRGBA(b)

	// ── 1. Panel uzaklaşır, kapanış ekranı belirir ─────────────────────
	start := time.Now()
	frame := 0
	for {
		el := time.Since(start)
		t := float64(el) / float64(inDur)
		if t >= 1 {
			break
		}
		e := fbdraw.EaseInOutCubic(t)

		a.drawPowerScreen(act, frame)
		// scale 1 -> 0.82: eski ekran küçülerek uzaklaşır.
		// blur 0 -> 16: uzaklaşan görüntü odağını kaybeder.
		fbdraw.ZoomBlurFade(canvas, from, scratch, b,
			1-0.18*e, int(16*e), e)
		if err := flip(); err != nil {
			return
		}
		frame++
		time.Sleep(powerFrame)
	}

	// ── 2. Kapanış ekranı beklerken döner ──────────────────────────────
	hold := time.Now()
	for time.Since(hold) < holdDur {
		a.drawPowerScreen(act, frame)
		if err := flip(); err != nil {
			return
		}
		frame++
		time.Sleep(powerFrame)
	}

	// ── 3. Kapanış ekranı da uzaklaşır, ekran siyah kalır ──────────────
	a.drawPowerScreen(act, frame)
	copy(from.Pix, canvas.Pix)

	start = time.Now()
	for {
		t := float64(time.Since(start)) / float64(outDur)
		if t >= 1 {
			break
		}
		e := fbdraw.EaseInOutCubic(t)

		fillBlack(canvas, b)
		fbdraw.ZoomBlurFade(canvas, from, scratch, b,
			1-0.30*e, int(20*e), e)
		if err := flip(); err != nil {
			return
		}
		time.Sleep(powerFrame)
	}

	// Son kare KESİN siyah: animasyonun son adımı t<1'de bittiği için
	// ekranda soluk bir hayalet kalabilir ve o hayalet, konsol geri
	// verilene kadar ekranda donar.
	fillBlack(canvas, b)
	_ = flip()
}

// fillBlack paints r black (not Pal.Bg: kapanışta gerçekten siyah istiyoruz).
func fillBlack(dst *image.RGBA, r image.Rectangle) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	row := make([]uint8, r.Dx()*4)
	for i := 3; i < len(row); i += 4 {
		row[i] = 255 // opak siyah
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := dst.PixOffset(r.Min.X, y)
		copy(dst.Pix[o:o+len(row)], row)
	}
}
