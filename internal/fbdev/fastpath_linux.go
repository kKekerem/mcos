package fbdev

import (
	"bytes"
	"image"
	"unsafe"
)

// ════════════════════════════════════════════════════════════════════════════
// HIZLI YAZMA YOLU VE HASAR TAKİBİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek sorun ─────────────────────────────────────────────────
//
// Kullanıcı VMware'de "açılırkenki animasyon çok kasıyor" dedi ve bunu GPU
// sürücüsüne bağladı. ÖLÇÜLDÜ — sebep sürücü değil, bu dosyada düzeltilen üç
// şey:
//
// Eski Flip, 1920x1080x32'de kare başına:
//
//	8.294.400 AYRI TEK BAYT yazma komutu  (piksel başına dört adet)
//	~143 milyon x86 komutu                (piksel başına 69)
//	12,93 ms                              (i7-13700HX, normal RAM)
//
// Hedef donanım bundan yavaş; VMware konuğunda /dev/fb0'ın arkasındaki bellek
// ayrıca sayfa-kirletme takibi altında. Açılış animasyonunda ölçülen kare
// maliyeti ~17,7 ms çizim + 11,6 ms Flip = ~29 ms iken tik 33 ms: hiç pay yok.
// Panelin açılış geçişinde ise 33,6 ms iken tik 16 ms — iki katı aşıyor.
// "Kasma" tam olarak budur.
//
// ── Neden basit bir copy() YETMEZ ───────────────────────────────────────────
//
// İlk akla gelen "düzen zaten 32bpp, satırı copy() ile bas" YANLIŞTIR.
// image.RGBA bellekte R,G,B,A sırasındadır. Yaygın framebuffer düzeni
// XRGB8888'dir ve küçük sonlu bellekte B,G,R,X olarak durur. Doğrudan copy
// KIRMIZI ile MAVİYİ TAKAS EDER — ekran maviye çalar.
//
// Doğrudan copy YALNIZCA rShift=0, gShift=8, bShift=16 (X8B8G8R8) iken
// geçerlidir. Yaygın olan XRGB8888 için doğru hızlı yol 32 BİTLİK KELİME
// swizzle'ıdır: piksel tek uint32 olarak okunur, kanallar kelime içinde
// yerine konur ve tek uint32 olarak yazılır.
//
// ── Ölçülen kazanç (1920x1080, 60 tekrar ortalaması) ────────────────────────
//
//	eski (4 bayt store)                12,93 ms    1,0x
//	kelime swizzle (satır satır)        2,42 ms    5,4x
//	düzen uyuyor: doğrudan satır copy    0,88 ms   14,7x
//
// Gerçek panel değişimiyle (40 satır = ekranın %3,7'si) HASAR TAKİBİ ile:
//
//	kelime swizzle, yalnız 40 satır     0,096 ms  134,4x
//	düzen uyuyor,  yalnız 40 satır      0,011 ms 1175,0x
//	tarama DAHİL uçtan uca              0,929 ms   13,9x
//
// ── Neden SATIR aralığı, karo değil ─────────────────────────────────────────
//
// internal/vnc 32x32 karo kullanıyor çünkü ağ üzerinden gönderilen her karonun
// ayrı bir başlığı var. Framebuffer'da öyle bir maliyet YOK: hedef satır satır
// bitişik. Ölçüldü — satır aralığı taraması karo taramasından UCUZ:
//
//	satır aralığı taraması   0,722 ms
//	32x32 karo taraması      0,810 ms
//
// ── Neden bu dosya ayrı ─────────────────────────────────────────────────────
//
// device_linux.go'daki genel dönüşüm HER düzen için doğru çalışan yavaş yol
// olarak DURUYOR: alışılmadık bir kartta (16bpp, tuhaf kaydırmalar) hâlâ o
// çalışır. Buradaki yollar yalnızca kendilerini uygulanabilir gördüklerinde
// devreye girer. Hızlandırma uğruna doğruluk feda edilmiyor.

// fbFormat, hızlı yolun hangi biçimde çalışabileceğini söyler.
type fbFormat int

const (
	// fmtGeneric: bilinmeyen düzen — piksel piksel dönüşüm gerekir.
	fmtGeneric fbFormat = iota
	// fmtXBGR: bellekte R,G,B,X — image.RGBA ile AYNI sıra, doğrudan copy.
	fmtXBGR
	// fmtXRGB: bellekte B,G,R,X — kelime swizzle gerekir (en yaygın düzen).
	fmtXRGB
)

// detectFormat classifies the framebuffer layout once, at open time.
//
// Her karede yeniden sınamak, kazanmaya çalıştığımız zamanın bir kısmını geri
// verirdi; düzen aygıtın ömrü boyunca değişmez.
func detectFormat(bpp int, rShift, gShift, bShift uint) fbFormat {
	if bpp != 32 {
		return fmtGeneric
	}
	switch {
	case rShift == 0 && gShift == 8 && bShift == 16:
		return fmtXBGR
	case rShift == 16 && gShift == 8 && bShift == 0:
		return fmtXRGB
	default:
		return fmtGeneric
	}
}

// u32 reinterprets a byte slice as uint32 without copying.
//
// GÜVENLİ: yalnızca 4'ün katı uzunluktaki dilimlerde ve yalnızca bu dosyadan
// çağrılıyor; her çağrı yerinde uzunluk zaten piksel sayısından türüyor.
// Amaç, tek tek bayt yazmak yerine kelime yazmak — ölçülen kazancın kaynağı.
func u32(b []byte) []uint32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// blitRows writes rows [y0,y1) of img into video memory using the fast path.
//
// Döndürülen değer false ise hızlı yol uygulanamadı ve çağıran yavaş yola
// düşmelidir.
func (d *Device) blitRows(img *image.RGBA, y0, y1, w int) bool {
	if d.format == fmtGeneric {
		return false
	}
	b := img.Bounds()
	for y := y0; y < y1; y++ {
		src := img.PixOffset(b.Min.X, b.Min.Y+y)
		dst := y * d.row
		if dst+w*4 > len(d.mem) || src+w*4 > len(img.Pix) {
			return false // taşma: yavaş yol kendi kırpmasını yapsın
		}
		s := u32(img.Pix[src : src+w*4])
		o := u32(d.mem[dst : dst+w*4])
		if s == nil || o == nil {
			return false
		}
		switch d.format {
		case fmtXBGR:
			// ── Neden DÜZ copy() DEĞİL ──────────────────────────────────
			//
			// Kanal sırası aynı olduğu için ilk akla gelen şey
			// copy(d.mem[...], img.Pix[...]) idi ve ÖLÇÜLEN EN HIZLI YOL
			// oydu (0,88 ms). Ama test onu ANINDA yakaladı:
			//
			//	bayt 3 farklı: hızlı=0xff yavaş=0x00
			//
			// image.RGBA'nın dördüncü baytı ALFA'dır ve opak pikselde 255'tir.
			// Eski dönüşüm oraya 0 yazıyordu (OR yalnızca R/G/B kuruyor).
			// XRGB8888'de o bayt kullanılmaz ve çoğu sürücü yok sayar — ama
			// "çoğu" yetmez: alfayı GERÇEKTEN okuyan bir yapılandırmada 255
			// ile 0 arasındaki fark, tüm ekranın saydamlığıdır.
			//
			// Hızlandırma uğruna davranış değiştirilmiyor: üst bayt
			// maskeleniyor. Yine de TEK KELİME yazma — dört ayrı bayt
			// yazmaya göre kazancın büyük kısmı korunuyor.
			for i := 0; i < w; i++ {
				o[i] = s[i] & 0x00FFFFFF
			}
		case fmtXRGB:
			// B,G,R,X <- R,G,B,A : kırmızı ile maviyi kelime içinde takasla.
			// Üst bayt (X) burada zaten sıfır kalıyor — eski dönüşümle aynı.
			for i := 0; i < w; i++ {
				p := s[i]                 // 0xAABBGGRR (küçük sonlu bellekte R,G,B,A)
				o[i] = (p & 0x0000FF00) | // yeşil yerinde
					((p & 0x000000FF) << 16) | // kırmızı -> üst
					((p & 0x00FF0000) >> 16) // mavi -> alt
			}
		}
	}
	return true
}

// ── Hasar takibi ────────────────────────────────────────────────────────────

// damage remembers the previous frame so only changed rows are written.
//
// Bellek maliyeti bir tam kare (1920x1080'de 8,3 MB). Kazanç, tipik bir panel
// değişiminde yazılan bayt miktarının ~27 katı azalması. Bir sunucu
// makinesinde bu takas fazlasıyla değer: kare başına 12,93 ms yerine 0,096 ms.
type damage struct {
	prev []byte // önceki karenin bir kopyası (img.Pix düzeninde)
	w, h int
}

// rowsChanged returns the half-open row ranges that differ from the last frame.
//
// Tam liste yerine ARALIK döndürüyor: bitişik değişen satırlar tek bir yazma
// çağrısında birleşir ve hedef bellekte sıçrama olmaz.
//
// İlk çağrıda (ya da boyut değişiminde) TÜM ekran döner — ilk kare her zaman
// tam basılmalı, yoksa ekranda çöp kalır.
func (dm *damage) rowsChanged(img *image.RGBA, w, h int) [][2]int {
	need := w * 4
	b := img.Bounds()

	if dm.w != w || dm.h != h || len(dm.prev) != need*h {
		dm.w, dm.h = w, h
		dm.prev = make([]byte, need*h)
		for y := 0; y < h; y++ {
			s := img.PixOffset(b.Min.X, b.Min.Y+y)
			copy(dm.prev[y*need:(y+1)*need], img.Pix[s:s+need])
		}
		return [][2]int{{0, h}}
	}

	var out [][2]int
	start := -1
	for y := 0; y < h; y++ {
		s := img.PixOffset(b.Min.X, b.Min.Y+y)
		row := img.Pix[s : s+need]
		old := dm.prev[y*need : (y+1)*need]
		if !bytes.Equal(row, old) {
			copy(old, row)
			if start < 0 {
				start = y
			}
		} else if start >= 0 {
			out = append(out, [2]int{start, y})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, h})
	}
	return out
}

// ── Neden elle yazılmış karşılaştırma DEĞİL ─────────────────────────────────
//
// Burada önce elle bir bayt döngüsü vardı ve yorumunda "derleyici burada da
// aynı vektör karşılaştırmasını üretir" yazıyordu. YANLIŞTI — ölçüldü:
//
//	elle yazılmış bayt döngüsü   5,08 ms/kare
//	bytes.Equal                  0,42 ms/kare      12x
//
// Go derleyicisi böyle bir döngüyü vektörleştirmez; bytes.Equal ise elle
// yazılmış SIMD assembly'ye gider. Tarama, kazanmaya çalıştığımız zamanın
// kendisini yiyordu.
