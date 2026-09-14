package fbdraw

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// Bu dosya TAM KARE efektleri yapar: iki kareyi karıştırmak, bir kareyi
// yakınlaştırmak, kaydırmak.
//
// ── Neden burada? ───────────────────────────────────────────────────────────
// Geçiş efektleri "widget" değildir: tek bir düğmeyi değil, ekranın tamamını
// ilgilendirirler. fbui katmanı şekil çizer; burası PİKSEL taşır.
//
// ── Neden hız önemli? ───────────────────────────────────────────────────────
// 1920x1080 bir kare 2.07 milyon piksel = 8.3 MB'dır. Bir geçiş sırasında
// saniyede 30 kez iki kareyi karıştırmak, saniyede 500 MB bellek trafiği
// demektir. Bu yüzden:
//
//   - Her fonksiyon YALNIZCA verilen dikdörtgeni işler (tüm ekranı değil).
//   - İç döngüler []uint8 üzerinde doğrudan çalışır; image.At/Set ÇAĞRILMAZ
//     (arayüz çağrısı + renk dönüşümü, piksel başına ~20 kat yavaştır).
//   - Ölçekleme tamsayı sabit noktalı (16.16) aritmetik kullanır.

// CrossFade blends `from` into dst over the rectangle r.
//
// t=0 → dst tamamen `from` olur. t=1 → dst değişmez.
// Yani t, YENİ karenin ağırlığıdır.
//
// from, dst ile AYNI boyutta olmalıdır; değilse hiçbir şey yapılmaz (sessiz
// bozulma yerine sessiz atlama: yanlış boyutlu bir arabellek çöp piksel
// üretirdi).
func CrossFade(dst, from *image.RGBA, r image.Rectangle, t float64) {
	if from == nil || dst == nil || from.Bounds() != dst.Bounds() {
		return
	}
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	switch {
	case t <= 0:
		copyRect(dst, from, r)
		return
	case t >= 1:
		return
	}

	// Sabit noktalı ağırlık: 0..256. Kayan noktalı çarpmayı iç döngüden
	// çıkarmak ölçülebilir fark yaratır (8.3 MB × 4 kanal).
	wNew := int(t*256 + 0.5)
	wOld := 256 - wNew

	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := dst.PixOffset(r.Min.X, y)
		end := o + r.Dx()*4
		for i := o; i < end; i++ {
			dst.Pix[i] = uint8((int(dst.Pix[i])*wNew + int(from.Pix[i])*wOld) >> 8)
		}
	}
}

// copyRect copies one rectangle between identically-sized images.
func copyRect(dst, src *image.RGBA, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := dst.PixOffset(r.Min.X, y)
		copy(dst.Pix[o:o+r.Dx()*4], src.Pix[o:o+r.Dx()*4])
	}
}

// SlideBlend composites two frames sliding horizontally.
//
// Yeni kare `dx` piksel sağdan (dx>0) veya soldan (dx<0) girer; eski kare
// ters yöne çıkar. İkisi t ağırlığıyla da karıştırılır, böylece geçiş hem
// kayar hem soluklaşır — sert bir kesme yerine akıcı bir devir.
//
// dst: yeni kare (yerinde değiştirilir). old: önceki kare.
func SlideBlend(dst, old *image.RGBA, r image.Rectangle, dx int, t float64) {
	if old == nil || dst == nil || old.Bounds() != dst.Bounds() {
		return
	}
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	if t >= 1 && dx == 0 {
		return
	}

	w := r.Dx()
	row := make([]uint8, w*4)
	wNew := int(t*256 + 0.5)
	if wNew > 256 {
		wNew = 256
	}
	if wNew < 0 {
		wNew = 0
	}
	wOld := 256 - wNew

	// Eski kare ters yöne, yeni karenin kaydığı mesafenin bir miktar azı
	// kadar kayar (paralaks): iki katman farklı hızda hareket edince derinlik
	// hissi oluşur ve geçiş "tek blok kayması" gibi görünmez.
	oldDX := -dx / 3

	for y := r.Min.Y; y < r.Max.Y; y++ {
		base := dst.PixOffset(r.Min.X, y)
		for x := 0; x < w; x++ {
			// Yeni kareyi dx kadar kaydırarak oku.
			sn := x - dx
			var nr, ng, nb, na int
			if sn >= 0 && sn < w {
				o := base + sn*4
				nr, ng, nb, na = int(dst.Pix[o]), int(dst.Pix[o+1]),
					int(dst.Pix[o+2]), int(dst.Pix[o+3])
			}
			so := x - oldDX
			var orr, og, ob, oa int
			if so >= 0 && so < w {
				o := base + so*4
				orr, og, ob, oa = int(old.Pix[o]), int(old.Pix[o+1]),
					int(old.Pix[o+2]), int(old.Pix[o+3])
			}
			o := x * 4
			row[o] = uint8((nr*wNew + orr*wOld) >> 8)
			row[o+1] = uint8((ng*wNew + og*wOld) >> 8)
			row[o+2] = uint8((nb*wNew + ob*wOld) >> 8)
			row[o+3] = uint8((na*wNew + oa*wOld) >> 8)
		}
		copy(dst.Pix[base:base+w*4], row)
	}
}

// Zoom scales src about the centre of r and writes the result into dst.
//
// scale > 1 → YAKINLAŞTIRIR (görüntü büyür, kenarlar dışarı taşar).
// scale < 1 → uzaklaştırır (görüntü küçülür, kenarlarda src'nin kenar
// pikselleri tekrarlanır — siyah çerçeve yerine, ki bu geçiş sırasında
// göze batardı).
//
// dst ve src FARKLI arabellekler olmalıdır; aynı arabelleği hem okuyup hem
// yazmak, okunan pikselin çoktan üzerine yazılmış olmasına yol açar.
func Zoom(dst, src *image.RGBA, r image.Rectangle, scale float64) {
	if dst == nil || src == nil || dst.Bounds() != src.Bounds() {
		return
	}
	r = r.Intersect(dst.Bounds())
	if r.Empty() || scale == 1 {
		if scale == 1 && dst != src {
			copyRect(dst, src, r)
		}
		return
	}
	if scale <= 0 {
		scale = 0.01
	}

	cx := float64(r.Min.X) + float64(r.Dx())/2
	cy := float64(r.Min.Y) + float64(r.Dy())/2
	inv := 1 / scale

	// 16.16 sabit nokta: piksel başına kayan noktalı çarpmayı kaldırır.
	const fp = 16
	invFP := int64(inv * (1 << fp))

	// Satır başına bir kayan noktalı hesap, piksel başına yalnızca tamsayı
	// toplama: 2 milyon pikselde fark ölçülebilir.
	for y := r.Min.Y; y < r.Max.Y; y++ {
		srcY := clampInt(int(cy+(float64(y)-cy)*inv), r.Min.Y, r.Max.Y-1)

		dOff := dst.PixOffset(r.Min.X, y)
		sRow := src.PixOffset(0, srcY)

		// Negatif değerlerde >> aritmetik kaydırmadır, yani aşağı yuvarlar —
		// tam olarak istediğimiz taban alma davranışı.
		sxFP := int64((cx + (float64(r.Min.X)-cx)*inv) * (1 << fp))
		for x := 0; x < r.Dx(); x++ {
			srcX := clampInt(int(sxFP>>fp), r.Min.X, r.Max.X-1)
			s := sRow + srcX*4
			d := dOff + x*4
			dst.Pix[d] = src.Pix[s]
			dst.Pix[d+1] = src.Pix[s+1]
			dst.Pix[d+2] = src.Pix[s+2]
			dst.Pix[d+3] = src.Pix[s+3]
			sxFP += invFP
		}
	}
}

// ZoomBlurFade is the OOBE intro effect in one call.
//
// Kullanıcının isteği: "boot animasyonu bitince içeri zoomlanarak blur felan
// ile OOBE'nin ilk ekranı gelsin".
//
// Yaptığı iş, sırayla:
//
//  1. `from` karesini scale kadar YAKINLAŞTIRIR (içeri dalma hissi),
//  2. blur yarıçapı kadar BULANIKLAŞTIRIR (odak kaybı),
//  3. hedef kareyle t ağırlığında karıştırır (yeni ekran belirir).
//
// scratch, `from` ile aynı boyutta bir çalışma arabelleğidir; her karede
// yeniden ayırmamak için dışarıdan verilir (8 MB'lık bir ayırma, saniyede
// 30 kez yapılırsa çöp toplayıcıyı boğar).
func ZoomBlurFade(dst, from, scratch *image.RGBA, r image.Rectangle,
	scale float64, blurRadius int, t float64) {

	if dst == nil || from == nil || scratch == nil {
		return
	}
	if from.Bounds() != dst.Bounds() || scratch.Bounds() != dst.Bounds() {
		return
	}
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}

	// ── Neden iki yol var ───────────────────────────────────────────────
	// Ölçüldü (BenchmarkZoomBlurFade1080p, i7-13700HX): tam çözünürlükte
	// bu üçlü 1080p'de KARE BAŞINA 78 ms sürüyordu — yani ~13 fps. Açılış
	// geçişi tam olarak kullanıcının "blurlar geçiş efektleri çok iyi
	// olsun" dediği yer; 13 fps takılma demek. Hedef donanım bu dizüstünden
	// yavaş olduğu için orada çok daha kötü olurdu.
	//
	// Maliyetin kaynağı yarıçap değil, TAM ÇÖZÜNÜRLÜKTE KAÇ KEZ GEZİLDİĞİ:
	// Zoom bir kez yazar, Blur bir kez okuyup bir kez yazar (kendi içinde
	// küçültüp büyütür), CrossFade bir kez daha okur ve yazar. 2 milyon
	// pikselde beş geçiş.
	//
	// Oysa sonuç ZATEN bulanık: yüksek frekansları bilerek atıyoruz. O
	// hâlde yakınlaştırmayı ve bulanıklaştırmayı 1/f çözünürlükte yapıp
	// büyütmeyi karıştırmayla BİRLEŞTİRMEK aynı görüntüyü tek tam
	// çözünürlük geçişiyle verir.
	//
	// f, yarıçaptan seçilir: küçültmeden sonra yarıçap en az 2 kalmalı,
	// yoksa bulanıklık kaybolur ve küçültme blok blok görünür.
	f := 1
	switch {
	case blurRadius >= 8:
		f = 4
	case blurRadius >= 4:
		f = 2
	}
	if f > 1 && zoomBlurFadeReduced(dst, from, r, scale, blurRadius, f, t) {
		return
	}

	Zoom(scratch, from, r, scale)
	// Eşik 2 değil 4: 1080'lik bir ekranda 2-3 piksellik bulanıklık gözle
	// seçilmez ama tam çözünürlükte 50 ms'ye mal olur. Geçişin ilk anında
	// (yarıçap küçükken) bulanıklığı atlamak, hem daha hızlı hem de
	// görsel olarak aynı.
	if blurRadius >= 4 {
		Blur(scratch, r, blurRadius)
	}
	CrossFade(dst, scratch, r, t)
}

// zoomBlurFadeReduced runs zoom → blur → cross-fade at 1/f resolution.
//
// Döndürdüğü değer: hızlı yol kullanıldı mı. Bölge küçültülemeyecek kadar
// küçükse false döner ve çağıran tam çözünürlüklü yola düşer.
//
// scratch KULLANILMAZ: ara görüntü küçük olduğu için tam kare arabelleğe
// gerek yoktur.
func zoomBlurFadeReduced(dst, from *image.RGBA, r image.Rectangle,
	scale float64, blurRadius, f int, t float64) bool {

	w, h := r.Dx(), r.Dy()
	sw, sh := w/f, h/f
	// İki taraflı ara değerleme en az 2 piksel ister; 4 ile güvenli pay.
	if sw < 4 || sh < 4 {
		return false
	}

	if scale <= 0 {
		scale = 0.01
	}
	inv := 1 / scale
	cx := float64(r.Min.X) + float64(w)/2
	cy := float64(r.Min.Y) + float64(h)/2

	// ── 1. Yakınlaştırarak küçült ───────────────────────────────────────
	//
	// Her küçük piksel için f×f'lik bloğun tamamı değil, çaprazlama iki
	// nokta (toplam 4 örnek) okunur. Tek nokta seçmek (nearest) takma ad
	// üretir ve yakınlaştırma sürerken kenarlar titrer; 4 örnek bunu
	// bastırmaya yetiyor ve f=4'te 16 yerine 4 okuma demek.
	o1, o2 := f/4, (3*f)/4
	const fp = 16
	invFP := int64(inv * (1 << fp))
	stepFP := invFP * int64(f)

	small := make([]uint8, sw*sh*4)
	for sy := 0; sy < sh; sy++ {
		dy1 := r.Min.Y + sy*f + o1
		dy2 := r.Min.Y + sy*f + o2
		sy1 := clampInt(int(cy+(float64(dy1)-cy)*inv), r.Min.Y, r.Max.Y-1)
		sy2 := clampInt(int(cy+(float64(dy2)-cy)*inv), r.Min.Y, r.Max.Y-1)
		row1 := from.PixOffset(0, sy1)
		row2 := from.PixOffset(0, sy2)

		x1FP := int64((cx + (float64(r.Min.X+o1)-cx)*inv) * (1 << fp))
		x2FP := int64((cx + (float64(r.Min.X+o2)-cx)*inv) * (1 << fp))

		out := sy * sw * 4
		for sx := 0; sx < sw; sx++ {
			ax := clampInt(int(x1FP>>fp), r.Min.X, r.Max.X-1)
			bx := clampInt(int(x2FP>>fp), r.Min.X, r.Max.X-1)
			pa, pb := row1+ax*4, row1+bx*4
			pc, pd := row2+ax*4, row2+bx*4
			for c := 0; c < 4; c++ {
				small[out+c] = uint8((int(from.Pix[pa+c]) +
					int(from.Pix[pb+c]) +
					int(from.Pix[pc+c]) +
					int(from.Pix[pd+c])) >> 2)
			}
			out += 4
			x1FP += stepFP
			x2FP += stepFP
		}
	}

	// ── 2. Küçük arabellekte bulanıklaştır ──────────────────────────────
	// f kat küçülttük, yarıçap da f kat küçülmeli: aynı görsel yayılma.
	blurBuf(small, sw, sh, blurRadius/f)

	// ── 3. Büyüt ve karıştır (TEK tam çözünürlük geçişi) ────────────────
	wNew := int(t*256 + 0.5)
	if wNew < 0 {
		wNew = 0
	}
	if wNew > 256 {
		wNew = 256
	}
	wOld := 256 - wNew
	if wOld == 0 {
		// t>=1: dst zaten sonuç.
		return true
	}

	stepX := (sw << 16) / w
	stepY := (sh << 16) / h

	// Yatay ara değerleme katsayıları hedef sütun başına SABİTTİR; her
	// satırda yeniden hesaplamak 2 milyon kez aynı işi yapmak olurdu.
	xi := make([]int32, w)
	xw := make([]int32, w)
	fx := 0
	for x := 0; x < w; x++ {
		x0 := fx >> 16
		if x0 > sw-2 {
			x0 = sw - 2
		}
		if x0 < 0 {
			x0 = 0
		}
		xi[x] = int32(x0)
		xw[x] = int32(fx & 0xFFFF)
		fx += stepX
	}

	// ── Satır bantlarını çekirdeklere dağıt ─────────────────────────────
	//
	// Bu adım tam çözünürlüktedir ve kaçınılmazdır: her hedef pikselin
	// yazılması gerekir (1080p'de 8.3 milyon bayt). Ölçüldü: tek çekirdekte
	// geçişin kalan maliyetinin neredeyse tamamı burada.
	//
	// NEDEN GÜVENLİ: her işçi KENDİ satır aralığına yazar. image.RGBA'da
	// satırlar bellekte ayrıktır (Stride), yani iki işçi asla aynı baytı
	// görmez. Paylaşılan tek şey `small`, ve ona yalnızca OKUMA yapılır.
	//
	// Her işçinin kendi rowA/rowB arabelleği vardır; paylaşılsalardı
	// birbirlerinin ara değerlemesini bozarlardı.
	bands := runtime.NumCPU()
	if bands > 8 {
		bands = 8
	}
	// Bant başına en az 64 satır: daha incesi goroutine kurma maliyetini
	// kurtarmaz.
	if bands > h/64 {
		bands = h / 64
	}
	if bands < 1 {
		bands = 1
	}

	if bands == 1 {
		upscaleBlendBand(dst, small, r, sw, sh, w, 0, h, stepY, xi, xw, wNew, wOld)
		return true
	}

	var wg sync.WaitGroup
	rows := (h + bands - 1) / bands
	for b := 0; b < bands; b++ {
		y0 := b * rows
		y1 := y0 + rows
		if y1 > h {
			y1 = h
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(a, z int) {
			defer wg.Done()
			upscaleBlendBand(dst, small, r, sw, sh, w, a, z, stepY, xi, xw, wNew, wOld)
		}(y0, y1)
	}
	wg.Wait()
	return true
}

// upscaleBlendBand upsamples and blends destination rows [yStart, yEnd).
func upscaleBlendBand(dst *image.RGBA, small []uint8, r image.Rectangle,
	sw, sh, w, yStart, yEnd, stepY int, xi, xw []int32, wNew, wOld int) {

	n := w * 4
	rowA := make([]uint8, n)
	rowB := make([]uint8, n)
	cachedY0 := -1

	for y := yStart; y < yEnd; y++ {
		fy := y * stepY
		y0 := fy >> 16
		if y0 > sh-2 {
			y0 = sh - 2
		}
		if y0 < 0 {
			y0 = 0
		}
		wy := fy & 0xFFFF

		switch {
		case y0 == cachedY0:
			// aynı kaynak satır çifti: yeniden hesaplama yok
		case y0 == cachedY0+1:
			// Bir sonraki bloğun ÜST satırı, bu bloğun alt satırıdır.
			// Arabellekleri takas etmek o satırı ikinci kez ara
			// değerlemekten kurtarır — yatay geçişlerin yarısı gider.
			rowA, rowB = rowB, rowA
			lerpRowX(rowB, small, sw, y0+1, xi, xw)
			cachedY0 = y0
		default:
			lerpRowX(rowA, small, sw, y0, xi, xw)
			lerpRowX(rowB, small, sw, y0+1, xi, xw)
			cachedY0 = y0
		}

		// Üç dilim de AYNI uzunlukta kesilir: derleyici böylece iç
		// döngüdeki sınır denetimlerini kaldırabilir.
		p := dst.PixOffset(r.Min.X, r.Min.Y+y)
		dp := dst.Pix[p : p+n : p+n]
		ra := rowA[:n:n]
		rb := rowB[:n:n]
		for i := range dp {
			a := int(ra[i])
			v := a + ((int(rb[i])-a)*wy)>>16
			dp[i] = uint8((int(dp[i])*wNew + v*wOld) >> 8)
		}
	}
}

// lerpRowX interpolates one source row horizontally into out.
func lerpRowX(out, src []uint8, sw, y int, xi, xw []int32) {
	row := src[y*sw*4 : (y+1)*sw*4]
	n := len(out) / 4
	for x := 0; x < n; x++ {
		i0 := int(xi[x]) * 4
		wx := int(xw[x])
		// Sekiz bayt: iki komşu pikselin dört kanalı. Tek dilim olarak
		// kesmek, kanal döngüsündeki denetimleri kaldırır.
		q := row[i0 : i0+8 : i0+8]
		o := out[x*4 : x*4+4 : x*4+4]
		for c := 0; c < 4; c++ {
			a := int(q[c])
			o[c] = uint8(a + ((int(q[4+c])-a)*wx)>>16)
		}
	}
}

// EaseOutCubic decelerates towards the end. Geçişlerin varsayılanı.
//
// Doğrusal hareket makineye ait görünür; canlı bir arayüz hızlı başlar ve
// yumuşak durur.
func EaseOutCubic(t float64) float64 {
	t = clamp01(t)
	u := 1 - t
	return 1 - u*u*u
}

// EaseInOutCubic accelerates then decelerates. İki durum arası geçişler için.
func EaseInOutCubic(t float64) float64 {
	t = clamp01(t)
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := -2*t + 2
	return 1 - u*u*u/2
}

// EaseOutBack overshoots slightly then settles. Açılan pencereler için.
func EaseOutBack(t float64) float64 {
	t = clamp01(t)
	const c1 = 1.70158
	const c3 = c1 + 1
	u := t - 1
	return 1 + c3*u*u*u + c1*u*u
}

// Pulse returns a 0..1 value oscillating smoothly with the frame counter.
//
// period, tam bir gidiş-dönüş için gereken kare sayısıdır.
func Pulse(frame, period int) float64 {
	if period <= 0 {
		return 0
	}
	ph := float64(frame%period) / float64(period)
	return 0.5 - 0.5*math.Cos(2*math.Pi*ph)
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}
