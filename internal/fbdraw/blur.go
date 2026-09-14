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

	sw, sh := w/scale, h/scale
	// Küçültme ve büyütme TAM ÇÖZÜNÜRLÜKTE gezinir (2 milyon piksel okuma +
	// 2 milyon yazma); asıl bulanıklık ise küçük arabellekte kalır. Yani
	// maliyetin neredeyse tamamı bu iki geçiştedir ve ikisi de satır satır
	// ayrıktır — bantlara bölünebilir.
	small := downsample(dst, r, scale, sw, sh)
	blurBuf(small, sw, sh, radius/scale)
	upsample(dst, r, small, sw, sh)
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

// downsample box-averages scale×scale blocks into a small buffer.
//
// Ortalama alınır, örnek seçilmez: örnek seçmek (nearest) takma ad (aliasing)
// üretir ve bulanıklık sonrası titrek kenarlar olarak görünür.
func downsample(dst *image.RGBA, r image.Rectangle, scale, sw, sh int) []uint8 {
	out := make([]uint8, sw*sh*4)
	n := scale * scale
	forEachBand(sh, 32, func(ya, yz int) {
		downsampleBand(out, dst, r, scale, sw, n, ya, yz)
	})
	return out
}

func downsampleBand(out []uint8, dst *image.RGBA, r image.Rectangle,
	scale, sw, n, ya, yz int) {

	for y := ya; y < yz; y++ {
		for x := 0; x < sw; x++ {
			var sum [4]int
			for by := 0; by < scale; by++ {
				p := dst.PixOffset(r.Min.X+x*scale, r.Min.Y+y*scale+by)
				for bx := 0; bx < scale; bx++ {
					sum[0] += int(dst.Pix[p])
					sum[1] += int(dst.Pix[p+1])
					sum[2] += int(dst.Pix[p+2])
					sum[3] += int(dst.Pix[p+3])
					p += 4
				}
			}
			o := (y*sw + x) * 4
			out[o] = uint8(sum[0] / n)
			out[o+1] = uint8(sum[1] / n)
			out[o+2] = uint8(sum[2] / n)
			out[o+3] = uint8(sum[3] / n)
		}
	}
}

// upsample writes the small buffer back with bilinear interpolation.
//
// En yakın komşu ile büyütmek blok blok görünürdü; doğrusal ara değerleme
// bulanık görüntüyü pürüzsüz tutar.
func upsample(dst *image.RGBA, r image.Rectangle, src []uint8, sw, sh int) {
	w, h := r.Dx(), r.Dy()
	// 16.16 sabit noktalı adım: kayan noktadan hızlı, piksel başına aynı sonuç.
	stepX := (sw << 16) / w
	stepY := (sh << 16) / h

	forEachBand(h, 64, func(ya, yz int) {
		upsampleBand(dst, r, src, sw, sh, w, stepX, stepY, ya, yz)
	})
}

func upsampleBand(dst *image.RGBA, r image.Rectangle, src []uint8,
	sw, sh, w, stepX, stepY, ya, yz int) {

	for y := ya; y < yz; y++ {
		fy := y * stepY
		y0 := fy >> 16
		if y0 >= sh-1 {
			y0 = sh - 2
			if y0 < 0 {
				y0 = 0
			}
		}
		wy := fy & 0xFFFF
		y1 := y0 + 1
		if y1 > sh-1 {
			y1 = sh - 1
		}

		p := dst.PixOffset(r.Min.X, r.Min.Y+y)
		fx := 0
		for x := 0; x < w; x++ {
			x0 := fx >> 16
			if x0 >= sw-1 {
				x0 = sw - 2
				if x0 < 0 {
					x0 = 0
				}
			}
			wx := fx & 0xFFFF
			x1 := x0 + 1
			if x1 > sw-1 {
				x1 = sw - 1
			}

			i00 := (y0*sw + x0) * 4
			i01 := (y0*sw + x1) * 4
			i10 := (y1*sw + x0) * 4
			i11 := (y1*sw + x1) * 4

			for c := 0; c < 4; c++ {
				top := int(src[i00+c])<<16 + (int(src[i01+c])-int(src[i00+c]))*wx
				bot := int(src[i10+c])<<16 + (int(src[i11+c])-int(src[i10+c]))*wx
				v := (top>>16)<<16 + ((bot>>16)-(top>>16))*wy
				dst.Pix[p+c] = uint8(v >> 16)
			}
			p += 4
			fx += stepX
		}
	}
}

// boxBlurH blurs src into out horizontally with a sliding window.
//
// Kenarlarda piksel değeri KENETLENİR (en dıştaki piksel tekrarlanır), böylece
// pencere genişliği her zaman 2*radius+1 kalır ve kenarlar kararmaz.
func boxBlurH(src, out []uint8, w, h, radius int) {
	win := 2*radius + 1
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
			out[o] = uint8(sum[0] / win)
			out[o+1] = uint8(sum[1] / win)
			out[o+2] = uint8(sum[2] / win)
			out[o+3] = uint8(sum[3] / win)

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

// boxBlurV is boxBlurH transposed.
func boxBlurV(src, out []uint8, w, h, radius int) {
	win := 2*radius + 1
	for x := 0; x < w; x++ {
		col := x * 4
		var sum [4]int
		for i := -radius; i <= radius; i++ {
			yi := clampInt(i, 0, h-1)
			p := yi*w*4 + col
			sum[0] += int(src[p])
			sum[1] += int(src[p+1])
			sum[2] += int(src[p+2])
			sum[3] += int(src[p+3])
		}
		for y := 0; y < h; y++ {
			o := y*w*4 + col
			out[o] = uint8(sum[0] / win)
			out[o+1] = uint8(sum[1] / win)
			out[o+2] = uint8(sum[2] / win)
			out[o+3] = uint8(sum[3] / win)

			lo := clampInt(y-radius, 0, h-1)*w*4 + col
			hi := clampInt(y+radius+1, 0, h-1)*w*4 + col
			sum[0] += int(src[hi]) - int(src[lo])
			sum[1] += int(src[hi+1]) - int(src[lo+1])
			sum[2] += int(src[hi+2]) - int(src[lo+2])
			sum[3] += int(src[hi+3]) - int(src[lo+3])
		}
	}
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
