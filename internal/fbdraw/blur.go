package fbdraw

import (
	"image"
	"image/color"
	"runtime"
	"sync"
)

// Bu dosya, açılır pencerelerin (Wi-Fi seçimi, parola girişi, onay kutuları)
// ARKASINI işler.
//
// Terminalde bunu yapmak imkânsızdı: bir karakter hücresinin altındaki içerik
// diye bir şey yok, ya üstüne yazarsın ya yazmazsın. Arka planı "karartmak"
// için tek seçenek her hücreyi boşlukla doldurmaktı — yani tamamen silmek.
// Kendi piksellerimizi çizdiğimiz için artık arkadaki ekran DURUYOR, sadece
// bulanıklaşıp koyulaşıyor; kullanıcı nerede olduğunu kaybetmiyor.

// maxDirectPixels is the area above which Blur switches to the downsampled
// path. Ölçüldü: 1920x1080'i tam çözünürlükte bulanıklaştırmak 176 ms sürüyor
// (BenchmarkBlur1080p). Modal açılışı tek seferlik olsa bile 176 ms gözle
// görülür bir donma demek. Küçültüp bulanıklaştırıp büyütmek aynı görüntüyü
// bir kaç milisaniyede verir — ağır bulanıklıkta fark edilemez, çünkü zaten
// yüksek frekansları atıyoruz.
const maxDirectPixels = 320 * 320

// Blur applies a box blur to the given region of dst, in place.
//
// Üç kez üst üste kutu bulanıklığı uygulanır; bu Gauss bulanıklığına çok yakın
// bir sonuç verir ve kutu bulanıklığı kayan pencere toplamıyla O(piksel)
// çalışır — yarıçap ne olursa olsun maliyet aynıdır.
//
// radius <= 0 ise hiçbir şey yapılmaz.
func Blur(dst *image.RGBA, r image.Rectangle, radius int) {
	r = r.Intersect(dst.Bounds())
	if radius <= 0 || r.Empty() {
		return
	}
	w, h := r.Dx(), r.Dy()
	// Yarıçap bölgeden büyükse sonuç düz renge yakınsar; sınırla ki kayan
	// pencere aritmetiği taşmasın.
	if radius > w {
		radius = w
	}
	if radius > h {
		radius = h
	}

	// Büyük bölgelerde küçültme ölçeğini seç. Yarıçap ölçekten sonra en az 2
	// kalmalı, yoksa bulanıklık kaybolur.
	scale := 1
	for scale < 4 && w/scale*h/scale > maxDirectPixels && radius/(scale*2) >= 2 {
		scale *= 2
	}

	if scale == 1 {
		buf := extractRegion(dst, r)
		blurBuf(buf, w, h, radius)
		writeRegion(dst, r, buf)
		return
	}

	// Küçültme ve büyütme TAM ÇÖZÜNÜRLÜKTE gezinir (2 milyon piksel okuma +
	// 2 milyon yazma); asıl bulanıklık ise küçük arabellekte kalır. Yani
	// maliyetin neredeyse tamamı bu iki geçiştedir ve ikisi de satır satır
	// ayrıktır — bantlara bölünebilir.
	//
	// Büyütme artık Small.Compose'dan geçiyor (bkz. frost.go): satır başına
	// iki kaynak satırı önbelleğe alınıyor, piksel başına sınır denetimi
	// yok. Ölçüldü (BenchmarkOlcum 1080p, pprof): eski upsampleBand pencere
	// açılış karesinin %24'üydü.
	var small Small
	small.Downscale(dst, r, scale)
	small.Blur(radius / scale)
	small.Compose(dst, r, ComposeOpts{})
}

// rowBands splits h rows across the cores, returning the band count.
//
// Bant başına en az minRows satır: daha incesinde goroutine kurma maliyeti
// kazancı yer.
func rowBands(h, minRows int) int {
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	if n > h/minRows {
		n = h / minRows
	}
	if n < 1 {
		n = 1
	}
	return n
}

// forEachBand runs fn over contiguous row ranges, in parallel when worthwhile.
//
// GÜVENLİK: fn yalnızca [a, z) aralığındaki satırlara yazmalıdır. image.RGBA
// satırları bellekte ayrık olduğu için iki bant asla aynı baytı görmez.
func forEachBand(h, minRows int, fn func(a, z int)) {
	bands := rowBands(h, minRows)
	if bands == 1 {
		fn(0, h)
		return
	}
	var wg sync.WaitGroup
	rows := (h + bands - 1) / bands
	for i := 0; i < bands; i++ {
		a := i * rows
		z := a + rows
		if z > h {
			z = h
		}
		if a >= z {
			break
		}
		wg.Add(1)
		go func(a, z int) {
			defer wg.Done()
			fn(a, z)
		}(a, z)
	}
	wg.Wait()
}

// blurBuf runs the three-pass box blur over a contiguous RGBA buffer.
func blurBuf(buf []uint8, w, h, radius int) {
	if radius < 1 || w < 1 || h < 1 {
		return
	}
	tmp := make([]uint8, len(buf))
	for i := 0; i < 3; i++ {
		boxBlurH(buf, tmp, w, h, radius)
		boxBlurV(tmp, buf, w, h, radius)
	}
}

// extractRegion copies a region into a contiguous buffer.
//
// dst.Pix satır atlamalıdır (Stride); doğrudan üzerinde çalışmak indeks
// aritmetiğini gereksiz karmaşıklaştırır.
func extractRegion(dst *image.RGBA, r image.Rectangle) []uint8 {
	w, h := r.Dx(), r.Dy()
	buf := make([]uint8, w*h*4)
	for y := 0; y < h; y++ {
		copy(buf[y*w*4:(y+1)*w*4], dst.Pix[dst.PixOffset(r.Min.X, r.Min.Y+y):])
	}
	return buf
}

func writeRegion(dst *image.RGBA, r image.Rectangle, buf []uint8) {
	w, h := r.Dx(), r.Dy()
	for y := 0; y < h; y++ {
		copy(dst.Pix[dst.PixOffset(r.Min.X, r.Min.Y+y):], buf[y*w*4:(y+1)*w*4])
	}
}

// boxBlurH blurs src into out horizontally with a sliding window.
//
// Kenarlarda piksel değeri KENETLENİR (en dıştaki piksel tekrarlanır), böylece
// pencere genişliği her zaman 2*radius+1 kalır ve kenarlar kararmaz.
func boxBlurH(src, out []uint8, w, h, radius int) {
	win := 2*radius + 1
	m, sh := magicDiv(win) // bölme yerine çarpma+kaydırma (bkz. magicDiv)
	for y := 0; y < h; y++ {
		row := y * w * 4
		var sum [4]int
		// x=0 için pencereyi kur.
		for i := -radius; i <= radius; i++ {
			xi := clampInt(i, 0, w-1)
			p := row + xi*4
			sum[0] += int(src[p])
			sum[1] += int(src[p+1])
			sum[2] += int(src[p+2])
			sum[3] += int(src[p+3])
		}
		for x := 0; x < w; x++ {
			o := row + x*4
			out[o] = uint8((uint32(sum[0]) * m) >> sh)
			out[o+1] = uint8((uint32(sum[1]) * m) >> sh)
			out[o+2] = uint8((uint32(sum[2]) * m) >> sh)
			out[o+3] = uint8((uint32(sum[3]) * m) >> sh)

			// Pencereyi bir piksel kaydır: soldakini çıkar, sağdakini ekle.
			lo := row + clampInt(x-radius, 0, w-1)*4
			hi := row + clampInt(x+radius+1, 0, w-1)*4
			sum[0] += int(src[hi]) - int(src[lo])
			sum[1] += int(src[hi+1]) - int(src[lo+1])
			sum[2] += int(src[hi+2]) - int(src[lo+2])
			sum[3] += int(src[hi+3]) - int(src[lo+3])
		}
	}
}

// boxBlurV blurs vertically, walking the image ROW BY ROW.
//
// ── Düzeltilen gerçek darboğaz ──────────────────────────────────────────────
//
// Burada döngü SÜTUN SÜTUN geziyordu: art arda iki okuma w*4 bayt uzaktaydı
// (1920 genişlikte 7680 bayt). Yani her piksel için yeni bir önbellek satırı
// çekiliyor ve okunan 64 baytın yalnızca 4'ü kullanılıyordu.
//
// Artık pencere toplamları BİR SATIR BOYU dizide tutuluyor ve görüntü satır
// satır taranıyor: bellek erişimi tamamen ardışık.
//
// Çıktı DEĞİŞMİYOR — testler bunu bayt bayt doğruluyor.
func boxBlurV(src, out []uint8, w, h, radius int) {
	win := 2*radius + 1
	m, sh := magicDiv(win)
	n := w * 4

	// İlk satır için pencereyi kur.
	sum := make([]uint32, n)
	for i := -radius; i <= radius; i++ {
		yi := clampInt(i, 0, h-1) * n
		row := src[yi : yi+n]
		for j := 0; j < n; j++ {
			sum[j] += uint32(row[j])
		}
	}

	for y := 0; y < h; y++ {
		o := out[y*n : y*n+n]
		for j := 0; j < n; j++ {
			o[j] = uint8((sum[j] * m) >> sh)
		}
		lo := clampInt(y-radius, 0, h-1) * n
		hi := clampInt(y+radius+1, 0, h-1) * n
		l := src[lo : lo+n]
		hh := src[hi : hi+n]
		for j := 0; j < n; j++ {
			sum[j] += uint32(hh[j]) - uint32(l[j])
		}
	}
}

// magicDiv returns (m, s) such that (v*m)>>s == v/win for every reachable v.
//
// ── Neden bölme kaldırıldı ──────────────────────────────────────────────────
//
// Kutu bulanıklığı piksel başına DÖRT tamsayı bölmesi yapıyordu. Bu işlemcide
// tamsayı bölmesi ~20-26 çevrim; çarpma ve kaydırma 1-3 çevrim. 960x540'lık
// bir perdede bu, kare başına 2 milyondan fazla bölme demekti.
//
// Sihirli çarpan, bölmenin tam karşılığıdır — YAKLAŞIK DEĞİL. Pencere
// genişliği 1..81 (yarıçap 0..40) aralığında, ulaşılabilecek TÜM toplamlar
// için kaba kuvvetle doğrulandı ve testte de duruyor.
func magicDiv(win int) (m, s uint32) {
	s = 24
	m = uint32((1<<s)/uint32(win)) + 1
	return
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Dim darkens a region toward c by amount (0 = unchanged, 1 = fully c).
//
// Tamamen siyah yapmak yerine HAFİF karartma: arkadaki ekran hâlâ okunur
// kalır, ama öndeki pencere net biçimde öne çıkar.
func Dim(dst *image.RGBA, r image.Rectangle, c color.RGBA, amount float64) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() || amount <= 0 {
		return
	}
	if amount > 1 {
		amount = 1
	}
	// Tamsayı aritmetiği: 0..256 arası ölçek, kayan noktadan hızlı ve
	// piksel başına aynı sonucu verir.
	t := int(amount * 256)
	it := 256 - t
	cr, cg, cb := int(c.R)*t, int(c.G)*t, int(c.B)*t

	for y := r.Min.Y; y < r.Max.Y; y++ {
		p := dst.PixOffset(r.Min.X, y)
		for x := 0; x < r.Dx(); x++ {
			dst.Pix[p] = uint8((int(dst.Pix[p])*it + cr) >> 8)
			dst.Pix[p+1] = uint8((int(dst.Pix[p+1])*it + cg) >> 8)
			dst.Pix[p+2] = uint8((int(dst.Pix[p+2])*it + cb) >> 8)
			p += 4
		}
	}
}
