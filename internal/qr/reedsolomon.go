package qr

// ════════════════════════════════════════════════════════════════════════════
// REED-SOLOMON HATA DÜZELTME
// ════════════════════════════════════════════════════════════════════════════
//
// QR, GF(256) üzerinde 0x11D indirgenemez polinomuyla çalışır. Kod sözcükleri
// bir polinomun katsayılarıdır; hata düzeltme sözcükleri, veri polinomunun
// üretici polinoma bölümünden kalandır.
//
// Tablolar açılışta BİR KEZ kuruluyor: her kodlamada yeniden üretmek, panelde
// QR çizen her karede 512 çarpma demekti.

var (
	gfExp [512]byte // alfa^i
	gfLog [256]byte // log_alfa(i)
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			// 0x11D: QR'ın kullandığı indirgenemez polinom. Standart bunu
			// SABİTLER; başka bir değer geçerli QR üretmez.
			x ^= 0x11D
		}
	}
	// İkinci yarı, çarpmada "mod 255" almayı gereksiz kılar.
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

// gfCarp multiplies two field elements.
func gfCarp(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// ureticiPolinom returns the generator polynomial of the given degree.
//
// g(x) = (x - alfa^0)(x - alfa^1)...(x - alfa^(n-1))
func ureticiPolinom(n int) []byte {
	// Katsayılar EN YÜKSEK dereceden başlar: g[0] baş katsayı (her zaman 1).
	g := []byte{1}
	for i := 0; i < n; i++ {
		// g = g * (x + alfa^i)
		//
		// Derece d'lik g için sonuç d+1 derecedir ve
		//	q[k] = g[k]  (x ile çarpım)  XOR  g[k-1]*alfa^i  (sabit ile çarpım)
		//
		// ── Yakalanan gerçek hata ───────────────────────────────────────
		// Burada iki çarpım YER DEĞİŞTİRMİŞTİ: baş katsayı alfa^i ile
		// çarpılıyor, kaydırma ise sabitle yapılıyordu. Sonuç geçerli
		// görünen ama STANDARDA UYMAYAN bir üretici polinomdu; ürettiği
		// QR'ı hiçbir telefon okuyamazdı.
		//
		// Standardın yayınlanmış vektörü yakaladı (bkz. qr_test.go):
		//	alınan  : 7B A6 46 7D 71 A4 1F 53 C5 46
		//	beklenen: A5 24 D4 C1 ED 36 C7 87 2C 55
		yeni := make([]byte, len(g)+1)
		for j, c := range g {
			yeni[j] ^= c
			yeni[j+1] ^= gfCarp(c, gfExp[i])
		}
		g = yeni
	}
	return g
}

// rsKodla returns the `n` error-correction codewords for data.
func rsKodla(data []byte, n int) []byte {
	g := ureticiPolinom(n)
	// Kalan, veri polinomunun x^n ile çarpılıp g'ye bölünmesinden kalandır.
	kalan := make([]byte, len(data)+n)
	copy(kalan, data)

	for i := 0; i < len(data); i++ {
		k := kalan[i]
		if k == 0 {
			continue
		}
		for j, c := range g {
			kalan[i+j] ^= gfCarp(c, k)
		}
	}
	return kalan[len(data):]
}
