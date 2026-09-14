package fbui

import (
	"image"
	"image/color"
	"math"

	"mcos/internal/fbdraw"
)

// Bu dosya HAREKETLİ parçaları çizer: dönen göstergeler, tarama dalgaları,
// ilerleme halkaları, fare imleci ve MCOS işareti.
//
// ── Neden hepsi vektör? ─────────────────────────────────────────────────────
// Terminal arayüzünde animasyon "|/-\" karakterlerini sırayla basmaktı. Bu
// üç sorunu vardı: (1) font her karakteri aynı genişlikte çizmezse satır
// kayar, (2) eğik çizgi bir daireye benzemez, (3) hız kare hızına değil
// karakter genişliğine bağlıydı. Burada her şey gerçek yay ve daire.
//
// ── Kare sayacı sözleşmesi ──────────────────────────────────────────────────
// Animasyonlu her fonksiyon `frame int` alır: panelin animasyon sayacı.
// Sayaç her ~80 ms'de bir artar (bkz. fbpanel.DefaultOptions). Fonksiyonlar
// kendi periyotlarını kare cinsinden tanımlar, böylece hız tek yerden
// (tik süresinden) ayarlanabilir.

// ── Fare imleci ─────────────────────────────────────────────────────────────

// Cursor draws the mouse pointer at (x, y).
//
// Klasik ok biçimi bilerek seçildi: kullanıcı bu şekli tanır ve nereyi
// gösterdiği (sol üst uç) tartışmasızdır. Daire veya artı işareti,
// "tam olarak neresi tıklanacak?" sorusunu doğurur.
//
// İki katman çizilir: önce koyu bir kontur, sonra açık bir gövde. Tek renkli
// bir imleç açık zeminde kaybolur; kontur her zeminde görünür kalır.
func (u *UI) Cursor(x, y int) {
	s := float64(u.F.CellH) * 0.95
	if s < 12 {
		s = 12
	}
	fx, fy := float64(x), float64(y)

	// Ok gövdesi: sol üst köşeden aşağı, içe kıvrım, kuyruk.
	body := []fbdraw.Pt{
		{X: fx, Y: fy},
		{X: fx, Y: fy + s},
		{X: fx + s*0.27, Y: fy + s*0.74},
		{X: fx + s*0.44, Y: fy + s*1.08},
		{X: fx + s*0.60, Y: fy + s*1.01},
		{X: fx + s*0.43, Y: fy + s*0.68},
		{X: fx + s*0.72, Y: fy + s*0.66},
	}

	// Kontur: aynı şekli merkezinden dışa doğru büyütülmüş hâli.
	outline := make([]fbdraw.Pt, len(body))
	ox, oy := fx+s*0.33, fy+s*0.48 // gövdenin yaklaşık ağırlık merkezi
	const grow = 1.22
	for i, p := range body {
		outline[i] = fbdraw.Pt{
			X: ox + (p.X-ox)*grow,
			Y: oy + (p.Y-oy)*grow,
		}
	}

	u.P.FillPolygon(outline, color.RGBA{R: 8, G: 10, B: 13, A: 235})
	u.P.FillPolygon(body, u.Pal.Text)
}

// CursorBusy draws the pointer with a small spinning ring beside it.
//
// Bir işlem sürerken imlecin görünümünün değişmesi, kullanıcının "tıkladım
// ama bir şey olmadı" diye tekrar tıklamasını engeller.
func (u *UI) CursorBusy(x, y, frame int) {
	u.Cursor(x, y)
	r := float64(u.F.CellH) * 0.34
	u.SpinnerAt(float64(x)+r*2.4, float64(y)+r*3.0, r, frame, u.Pal.Accent)
}

// ── Dönen gösterge ──────────────────────────────────────────────────────────

// SpinnerAt draws a rotating arc of radius r centred at (cx, cy).
//
// StatusBar'daki küçük göstergeyle aynı görsel dil, ama her boyutta
// kullanılabilir: tarama ekranında büyük, alt çubukta küçük.
func (u *UI) SpinnerAt(cx, cy, r float64, frame int, c color.RGBA) {
	thick := math.Max(1.2, r*0.22)

	// Soluk tam halka: göstergenin "yörüngesi" görünür kalsın.
	u.P.StrokeCircle(cx, cy, r, thick*0.8, fbdraw.Alpha(c, 0.20))

	// Üstünde giderek soluklaşan noktalardan oluşan kısa yay.
	const dots = 7
	step := 2 * math.Pi / float64(spinnerFrames)
	base := float64(frame%spinnerFrames) * step
	for i := 0; i < dots; i++ {
		a := base - float64(i)*step*0.55
		fade := 1 - float64(i)/float64(dots)
		u.P.FillCircle(cx+r*math.Cos(a), cy+r*math.Sin(a),
			thick*0.62*(0.55+0.45*fade), fbdraw.Alpha(c, fade*fade))
	}
}

// ── Tarama dalgası ──────────────────────────────────────────────────────────

// scanPeriod is how many frames one radar sweep takes.
//
// 26 kare × 80 ms ≈ 2 saniye: bir "arıyorum" jesti için doğal hız. Daha
// hızlısı telaşlı, daha yavaşı donmuş görünür.
const scanPeriod = 26

// ScanPulse draws expanding rings — "bir şey aranıyor".
//
// Kullanıcının isteği: "ekranda tararken dönen animasyon olacak, listeye
// seçenek gelince animasyon [dursun]". Wi-Fi taraması, PC eşleştirme taraması
// ve USB taraması AYNI bu göstergeyi kullanır: aynı anlam, aynı görüntü.
//
// Üç halka aynı anda, aralarında 1/3 periyot fark vardır; böylece dalga
// kesintisiz görünür.
func (u *UI) ScanPulse(cx, cy, maxR float64, frame int, c color.RGBA) {
	const rings = 3
	for i := 0; i < rings; i++ {
		ph := math.Mod(float64(frame)/scanPeriod+float64(i)/rings, 1)
		// Yarıçap hızlanarak değil, yavaşlayarak büyür: dalga uzaklaştıkça
		// yavaşlar, gerçek bir yayılma gibi.
		r := maxR * fbdraw.EaseOutCubic(ph)
		if r < 1 {
			continue
		}
		// Dışa doğru soluklaşır.
		u.P.StrokeCircle(cx, cy, r, math.Max(1, maxR*0.045),
			fbdraw.Alpha(c, 0.55*(1-ph)*(1-ph)))
	}
	// Merkezde sabit bir nokta: dalganın nereden çıktığı belli olsun.
	u.P.FillCircle(cx, cy, math.Max(2, maxR*0.09), c)
}

// ScanBanner draws the standard "searching…" block used by every scan screen.
//
// Tek bir yerde tanımlı olması, Wi-Fi ile PC eşleştirme ekranlarının
// birbirinden farklı görünmesini imkânsız kılar.
// Returns the y just below the banner.
func (u *UI) ScanBanner(r image.Rectangle, y int, frame int, text, sub string) int {
	cx := float64(r.Min.X+r.Max.X) / 2
	maxR := math.Min(float64(r.Dx())*0.16, float64(u.F.CellH)*3.2)
	cy := float64(y) + maxR + float64(u.M.PadY)

	u.ScanPulse(cx, cy, maxR, frame, u.Pal.Accent)

	ty := int(cy+maxR) + u.M.PadY*2
	u.TextCenter(r.Min.X, r.Max.X, ty, text, u.Pal.Text)
	ty += u.F.CellH + u.M.PadY/2
	if sub != "" {
		u.TextCenter(r.Min.X, r.Max.X, ty, sub, u.Pal.TextFaint)
		ty += u.F.CellH
	}
	return ty + u.M.PadY
}

// ── İlerleme halkası ────────────────────────────────────────────────────────

// arcChordSteps, bir yayı kaç düz parçaya böleceğimizi söyler.
//
// Hedef, kirişlerin ~1.5 pikselden uzun olmaması. Bir kirişin yaydan en çok
// saptığı miktar (kiriş/2)²/(2·yarıçap)'tır; 1.5 px kiriş ve 200 px yarıçapta
// bu 0.0014 piksel eder, yani kenar yumuşatma değerlerini bile değiştirmez.
// Böylece çokgen yaklaşımı gerçek yayla GÖRSEL OLARAK aynı kalır.
func arcChordSteps(radius, span float64) int {
	n := int(math.Abs(span) * radius / 1.5)
	if n < 4 {
		n = 4
	}
	if n > 512 {
		// Üst sınır güvenlik içindir: nokta sayısı maliyeti belirlemez
		// (maliyet tuval boyutundadır), ama sınırsız bir slice istemiyoruz.
		n = 512
	}
	return n
}

// ProgressRing draws a circular progress indicator. pct is 0..100.
//
// Açılış ekranı bunu kullanır: yatay çubuk için yer yok, ama daire hem
// ortalanır hem de logoyu çevreler.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Bu fonksiyon eskiden yayı ~82 ayrı u.P.FillCircle çağrısıyla kuruyordu
// ("yayı küçük dairelerden kuruyoruz" notuyla). Ama fbdraw.Painter.path()
// HER şekil için tuvalin TAMAMINI sıfırlayıp (ras.Reset(w,h)) tuvalin
// TAMAMINA kompozit ediyor (ras.Draw(dst, dst.Bounds(), ...)) — şeklin
// büyüklüğünden bağımsız olarak. Yani her nokta 1920x1080 = 2.07 megapiksellik
// tam ekran geçişiydi. Üstelik thick, r ile orantılı olduğu için steps ~81'de
// sabitlenir: halka küçülünce bile ucuzlamıyordu.
//
// Sonuç: açılış ekranı 33 ms'de bir kare istiyordu (cmd/mcos-splash,
// "33 ms ≈ 30 kare/sn") ama tek bir ProgressRing çağrısı ölçülen ~1.4-3.3
// SANİYE sürüyordu. Kullanıcının gördüğü: logo nefesi, arkadaki tarama
// halkaları ve ilerleme halkası saniyede ancak bir kez ilerliyor; ekran
// DONMUŞ görünüyordu — kodun tam da önlemek için var olduğunu söylediği
// şey ("Düz bir logo, donmuş bir kare gibi görünür ve kullanıcı makineyi
// kapatmaya kalkar"). Ayrıca boot boyunca bir çekirdeği doldurup başlayan
// servislerle yarışıyor, en pahalı kare olan pct=100 karesi BOOT_READY'den
// sonra saniyelerce ölü zaman ekliyordu; mcos-launch'taki finish_splash
// yalnızca 5 saniye beklediği için yavaş donanımda splash, fbdev.SaveFrame'e
// ulaşamadan öldürülüyor ve panele yapılan --intro geçişi sessizce kayboluyordu.
//
// ── Düzeltme ────────────────────────────────────────────────────────────────
// Aynı şerit artık TEK bir yol olarak çiziliyor: yuvarlak uçlu bir halka
// dilimi (dış yay ileri, uçlarda yarım daire kapaklar, iç yay geri). Geometri
// birebir korunuyor — aynı başlangıç açısı (-π/2), aynı steps/n hesabı, aynı
// süpürme açısı, r±thick/2 aynı iç/dış yarıçaplar, aynı renk. Tek fark,
// eski yöntemin nokta aralıklarından doğan ~2 pikselik tırtıklanmanın
// kaybolması; o zaten "gerçek bir yay rasterleştiricisi yok" diye kabul
// edilmiş bir kusurdu. TestProgressRingGeometryMatchesLegacy bunu ölçüyor:
// eski noktaların boyadığı piksellerin %100'ü yeni şeritte de dolu, fazlalık
// yalnızca tırtıkların dolmasından gelen %7.
//
// ── Ölçüm (1920x1080, i7-13700HX, anim_bench_test.go) ───────────────────────
//
//	                   ÖNCE (nokta yığını)   SONRA (tek yol)   kazanç
//	pct=92 (tavan)     1.49 s/op, 152 alloc  44 ms/op, 5 alloc  ~34x
//	pct=100 (son kare) 1.71 s/op, 166 alloc  42 ms/op, 4 alloc  ~41x
//
// Kalan 42-44 ms hâlâ TAM tuval maliyetidir: fbdraw.Painter.path() şeklin
// sınırlayıcı kutusunu değil dst.Bounds()'u rasterleştiriyor, bu yüzden geriye
// kalan İKİ geçiş (soluk yörünge + şerit) ~22 ms'şer sürüyor. Bu fonksiyondan
// inilebilecek taban budur: iki farklı renk, iki kompozit. Bir sonraki kazanç
// fbdraw tarafında — path()'i şeklin kutusuna sınırlamak — ki o bu dosyanın
// değil, painter.go'nun işi.
func (u *UI) ProgressRing(cx, cy, r float64, pct int, c color.RGBA) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	thick := math.Max(1.5, r*0.14)
	u.P.StrokeCircle(cx, cy, r, thick, fbdraw.Alpha(c, 0.18))

	// steps/n ESKİSİYLE AYNI hesaplanır: süpürme açısı 2π·n/steps olmalı ki
	// yayın ucu tam olarak eskisinin durduğu yerde dursun.
	steps := int(2 * math.Pi * r / (thick * 0.55))
	if steps < 24 {
		steps = 24
	}
	n := steps * pct / 100

	const a0 = -math.Pi / 2 // yay tepeden başlar
	d := thick / 2          // şeridin yarı kalınlığı
	p0x, p0y := cx+r*math.Cos(a0), cy+r*math.Sin(a0)

	if n <= 0 {
		// pct=0: eski döngü (i<=0) de tam olarak tek bir nokta çiziyordu.
		u.P.FillCircle(p0x, p0y, d, c)
		return
	}
	if n >= steps {
		// pct=100: kapaklar üst üste biner, şerit kapanır. StrokeCircle zaten
		// tek geçişte halka çiziyor; dış yarıçap r+d, iç yarıçap r+d-thick = r-d.
		u.P.StrokeCircle(cx, cy, r+d, thick, c)
		return
	}
	if r <= d {
		// r ≤ 0.75 px — halkanın kendisi noktadan küçük. Nokta birleşimi zaten
		// merkeze oturmuş 1.5 pikselik bir leke; tek daire ile ayırt edilemez.
		u.P.FillCircle(cx, cy, r+d, c)
		return
	}

	sweep := 2 * math.Pi * float64(n) / float64(steps)
	a1 := a0 + sweep
	rOut, rIn := r+d, r-d
	p1x, p1y := cx+r*math.Cos(a1), cy+r*math.Sin(a1)

	nCap := arcChordSteps(d, math.Pi)
	nOut := arcChordSteps(rOut, sweep)
	nIn := arcChordSteps(rIn, sweep)
	pts := make([]fbdraw.Pt, 0, 2*(nCap+1)+nOut+nIn)

	// 1) Başlangıç kapağı: iç kenardan GERİYE doğru yarım daire çizip dış
	//    kenara çıkar. Kapak merkezi yay üstündeki ilk nokta.
	for i := 0; i <= nCap; i++ {
		th := a0 + math.Pi + math.Pi*float64(i)/float64(nCap)
		pts = append(pts, fbdraw.Pt{X: p0x + d*math.Cos(th), Y: p0y + d*math.Sin(th)})
	}
	// 2) Dış yay ileri (i=0 noktası kapağın bitişiyle aynı, atlanır).
	for i := 1; i <= nOut; i++ {
		a := a0 + sweep*float64(i)/float64(nOut)
		pts = append(pts, fbdraw.Pt{X: cx + rOut*math.Cos(a), Y: cy + rOut*math.Sin(a)})
	}
	// 3) Bitiş kapağı: dış kenardan İLERİYE doğru dönüp iç kenara iner.
	for i := 0; i <= nCap; i++ {
		th := a1 + math.Pi*float64(i)/float64(nCap)
		pts = append(pts, fbdraw.Pt{X: p1x + d*math.Cos(th), Y: p1y + d*math.Sin(th)})
	}
	// 4) İç yay geri. Son kenarı FillPolygon'un ClosePath'i çizer; başlangıç
	//    noktası zaten iç yayın a0 ucudur, o yüzden i=0'ı tekrar eklemiyoruz.
	for i := nIn - 1; i >= 1; i-- {
		a := a0 + sweep*float64(i)/float64(nIn)
		pts = append(pts, fbdraw.Pt{X: cx + rIn*math.Cos(a), Y: cy + rIn*math.Sin(a)})
	}

	u.P.FillPolygon(pts, c)
}

// ── MCOS işareti ────────────────────────────────────────────────────────────

// Logo draws the MCOS mark: a stylised block seen in isometric projection.
//
// NEDEN BLOK: MCOS bir Minecraft sunucusu işletim sistemidir; küp, konuyu
// tek bakışta anlatan en kısa işarettir. Metin logosu ("MCOS") her yazı
// tipinde farklı görünürdü; bu şekil fonttan bağımsızdır.
//
// size, kübün TOPLAM genişliğidir. (cx, cy) merkezdir.
func (u *UI) Logo(cx, cy, size float64, c color.RGBA) {
	w := size / 2      // yarım genişlik
	h := size * 0.29   // üst yüzün yarı yüksekliği
	side := size * 0.5 // yan yüz yüksekliği

	top := []fbdraw.Pt{
		{X: cx, Y: cy - side/2 - h},
		{X: cx + w, Y: cy - side/2},
		{X: cx, Y: cy - side/2 + h},
		{X: cx - w, Y: cy - side/2},
	}
	left := []fbdraw.Pt{
		{X: cx - w, Y: cy - side/2},
		{X: cx, Y: cy - side/2 + h},
		{X: cx, Y: cy + side/2 + h},
		{X: cx - w, Y: cy + side/2},
	}
	right := []fbdraw.Pt{
		{X: cx + w, Y: cy - side/2},
		{X: cx, Y: cy - side/2 + h},
		{X: cx, Y: cy + side/2 + h},
		{X: cx + w, Y: cy + side/2},
	}

	// Üç yüz üç farklı parlaklıkta: ışık sol üstten geliyormuş gibi.
	// Düz tek renk çizmek, küpü altıgen bir lekeye çevirirdi.
	u.P.FillPolygon(top, c)
	u.P.FillPolygon(left, fbdraw.Blend(c, color.RGBA{A: 255}, 0.42))
	u.P.FillPolygon(right, fbdraw.Blend(c, color.RGBA{A: 255}, 0.22))
}

// LogoPulse draws the logo with a slow breathing halo, for the boot screen.
func (u *UI) LogoPulse(cx, cy, size float64, frame int, c color.RGBA) {
	// 40 kare ≈ 3.2 s: sakin bir nefes ritmi. Hızlı yanıp sönen bir logo
	// "hata" gibi okunur.
	p := fbdraw.Pulse(frame, 40)
	halo := size * (0.78 + 0.10*p)
	u.P.FillCircle(cx, cy, halo, fbdraw.Alpha(c, 0.05+0.05*p))
	u.P.StrokeCircle(cx, cy, halo, math.Max(1, size*0.012),
		fbdraw.Alpha(c, 0.10+0.10*p))
	u.Logo(cx, cy, size, c)
}

// ── İskelet satır ───────────────────────────────────────────────────────────

// SkeletonRow draws a shimmering placeholder bar used while data loads.
//
// Boş bir listeye "yükleniyor…" yazmak yerine, gelecek satırların yerini
// gösteren gri bloklar çizmek, bekleyişi kısa gösterir ve düzenin
// veri gelince ZIPLAMAYACAĞINI baştan belli eder.
func (u *UI) SkeletonRow(r image.Rectangle, frame, index int) {
	// Parıltı soldan sağa geçer; her satır bir öncekinden biraz gecikmeli
	// başlar, böylece dalga listenin üstünden aşağı akar.
	p := fbdraw.Pulse(frame*2-index*3, 34)
	base := fbdraw.Blend(u.Pal.Bg, u.Pal.Raised, 0.55+0.35*p)

	h := float64(r.Dy()) * 0.42
	y := float64(r.Min.Y) + (float64(r.Dy())-h)/2
	// Genişlikler bilerek farklı: eşit uzunlukta bloklar tablo gibi görünür,
	// oysa gelecek olan bir listedir.
	wFrac := []float64{0.62, 0.44, 0.71, 0.38, 0.55}[index%5]
	u.P.FillRoundRect(
		fbdraw.R(float64(r.Min.X)+float64(u.M.PadX), y,
			float64(r.Dx())*wFrac, h),
		h/2, base)
}

// ── Vurgu çerçevesi ─────────────────────────────────────────────────────────

// HoverRow draws the highlight used when the MOUSE is over a row but the row
// is not selected.
//
// Klavye seçimi (Row) ile fare üzerinde durması (HoverRow) FARKLI
// görünmelidir: ikisi aynı olursa kullanıcı Enter'a bastığında hangi satırın
// çalışacağını bilemez.
func (u *UI) HoverRow(r image.Rectangle) {
	u.P.FillRoundRect(
		fbdraw.R(float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy())),
		u.M.RadiusSmall, fbdraw.Blend(u.Pal.Bg, u.Pal.Raised, 0.38))
}
