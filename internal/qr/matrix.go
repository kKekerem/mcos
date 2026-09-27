package qr

// ════════════════════════════════════════════════════════════════════════════
// MATRİS: İŞLEV DESENLERİ, VERİ YERLEŞİMİ, MASKELEME
// ════════════════════════════════════════════════════════════════════════════

func yeniMatris(surum int) *Matrix {
	n := 17 + 4*surum
	return &Matrix{Size: n, mods: make([]bool, n*n), set: make([]bool, n*n)}
}

// islevDesenleri paints everything that is NOT data: finders, timing,
// alignment, the dark module and the reserved format/version areas.
func (m *Matrix) islevDesenleri(surum int) {
	n := m.Size

	// Üç bulucu desen + ayırıcıları.
	for _, p := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		m.bulucu(p[0], p[1])
	}

	// Zamanlama desenleri: 6. satır ve 6. sütun, bir koyu bir açık.
	for i := 8; i < n-8; i++ {
		koyu := i%2 == 0
		m.put(i, 6, koyu, true)
		m.put(6, i, koyu, true)
	}

	// Hizalama desenleri. Bulucu desenlerle ÇAKIŞANLAR atlanır — standart
	// bunu böyle tanımlıyor ve atlamamak kodu okunmaz yapar.
	merkezler := hizaMerkezleri[surum]
	for _, cy := range merkezler {
		for _, cx := range merkezler {
			if (cx == 6 && cy == 6) ||
				(cx == 6 && cy == n-7) ||
				(cx == n-7 && cy == 6) {
				continue
			}
			m.hizalama(cx, cy)
		}
	}

	// Koyu modül: her zaman (8, 4*surum+9).
	m.put(8, 4*surum+9, true, true)

	// ── Biçim bilgisi alanları REZERVE edilir ───────────────────────────
	//
	// ── Yakalanan iki gerçek hata ───────────────────────────────────────
	//
	// Burada saf bir döngü vardı: (i,8) ve (8,i) için i = 0..8, artı
	// n-1-i için i = 0..7. İkisi de FAZLA yer kaplıyordu:
	//
	//   - (8,6) ve (6,8) ZAMANLAMA deseninin parçasıdır; biçim bilgisi
	//     onları ATLAR. Döngü üzerlerine açık modül yazıp deseni bozuyordu.
	//   - (8, n-8) KOYU MODÜLDÜR ve her zaman koyudur; döngü onu da
	//     siliyordu. Okuyucular o modülü şart koşar.
	//
	// İkisi de testle yakalandı. Artık rezervasyon, bicimYaz'ın GERÇEKTEN
	// kullandığı koordinatların aynısını kullanıyor — iki liste ayrılamaz.
	for i := 0; i < 15; i++ {
		x, y := bicimKonum1(i)
		m.put(x, y, false, true)
		x, y = bicimKonum2(i, n)
		m.put(x, y, false, true)
	}

	// Sürüm bilgisi yalnızca sürüm 7 ve üstünde var.
	if surum >= 7 {
		v := surumBitleri(surum)
		for i := 0; i < 18; i++ {
			bit := (v>>uint(i))&1 == 1
			x, y := i/3, n-11+i%3
			m.put(x, y, bit, true)
			m.put(y, x, bit, true)
		}
	}
}

func (m *Matrix) bulucu(x, y int) {
	for dy := -1; dy <= 7; dy++ {
		for dx := -1; dx <= 7; dx++ {
			px, py := x+dx, y+dy
			if px < 0 || py < 0 || px >= m.Size || py >= m.Size {
				continue
			}
			// 7x7 çerçeve, 3x3 dolu merkez; kenarında bir modül ayırıcı.
			ic := dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6
			koyu := ic && (dx == 0 || dx == 6 || dy == 0 || dy == 6 ||
				(dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4))
			m.put(px, py, koyu, true)
		}
	}
}

func (m *Matrix) hizalama(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			koyu := dx == -2 || dx == 2 || dy == -2 || dy == 2 ||
				(dx == 0 && dy == 0)
			m.put(cx+dx, cy+dy, koyu, true)
		}
	}
}

// surumBitleri returns the 18-bit version information with BCH(18,6).
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// Eski kod üreteci (0x1F25, 13 bit) bölünen bitin hizasına değil 6 bit
// YUKARISINA kaydırıyordu (<< 11-i yerine << 5-i olmalıydı) ve kalanı
// sürümle birleştirirken taşan bitleri de bırakıyordu. Sürüm 7 için
// standardın 0x07C94'ü yerine 0x18FC40 çıkıyordu. Sonuç ÖLÇÜLDÜ: sürüm 7 ve
// üstündeki (≥107 bayt) her QR, bağımsız bir çözücüde (zxing-cpp) OKUNAMADI
// — eşleşme URI'si 140-177 bayt olduğu için panelin QR'ı telefonda HİÇ
// okunmuyordu. Sürüm 1-6'da sürüm bilgisi olmadığından hata görünmüyordu.
//
// Doğrusu: 6 bitlik sürümü 12 bit sola kaydırıp üreteçle GF(2)'de bölmek;
// kalan 12 bit. (ISO/IEC 18004 Ek D; standart değerler testte.)
func surumBitleri(surum int) uint32 {
	kalan := uint32(surum)
	for i := 0; i < 12; i++ {
		kalan = (kalan << 1) ^ ((kalan >> 11) * 0x1F25)
	}
	return uint32(surum)<<12 | kalan&0xFFF
}

// veriYerlestir walks the zig-zag data path and writes the codeword bits.
//
// Yol SAĞ ALT köşeden başlar, iki sütunluk şeritler hâlinde yukarı-aşağı
// zikzak çizer ve işlev desenlerinin üzerinden ATLAR. 6. sütun zamanlama
// desenidir ve şeritlerin dışında tutulur.
func (m *Matrix) veriYerlestir(kodlar []byte) {
	n := m.Size
	bit := 0
	yukari := true

	for sag := n - 1; sag > 0; sag -= 2 {
		if sag == 6 {
			sag-- // zamanlama sütununu atla
		}
		for i := 0; i < n; i++ {
			y := i
			if yukari {
				y = n - 1 - i
			}
			for _, x := range [2]int{sag, sag - 1} {
				if m.isFunc(x, y) {
					continue
				}
				koyu := false
				if bit < len(kodlar)*8 {
					koyu = kodlar[bit/8]&(1<<uint(7-bit%8)) != 0
				}
				m.put(x, y, koyu, false)
				bit++
			}
		}
		yukari = !yukari
	}
}

// ── Maskeleme ───────────────────────────────────────────────────────────────

// maskeUygula flips data modules according to mask pattern k.
func (m *Matrix) maskeUygula(k int) {
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if m.isFunc(x, y) {
				continue
			}
			if maskeKosulu(k, x, y) {
				m.mods[y*m.Size+x] = !m.mods[y*m.Size+x]
			}
		}
	}
}

func maskeKosulu(k, x, y int) bool {
	switch k {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	default:
		return ((x+y)%2+(x*y)%3)%2 == 0
	}
}

// enIyiMaske tries all eight masks and keeps the one with the lowest penalty.
//
// Maske seçimi ZORUNLUDUR ve keyfi değildir: kötü bir maske, geniş tek renk
// alanları ya da bulucu desene benzeyen diziler üretir; okuyucu o zaman kodu
// bulamaz ya da yanlış okur. Standart dört ceza kuralı tanımlıyor.
func (m *Matrix) enIyiMaske(surum int) error {
	enIyi, enIyiCeza := 0, -1
	yedek := make([]bool, len(m.mods))
	copy(yedek, m.mods)

	for k := 0; k < 8; k++ {
		copy(m.mods, yedek)
		m.maskeUygula(k)
		m.bicimYaz(k)
		if c := m.ceza(); enIyiCeza < 0 || c < enIyiCeza {
			enIyi, enIyiCeza = k, c
		}
	}
	copy(m.mods, yedek)
	m.maskeUygula(enIyi)
	m.bicimYaz(enIyi)
	return nil
}

// bicimYaz writes the 15-bit format information (ECC level M + mask).
func (m *Matrix) bicimYaz(maske int) {
	// ECC-M'in iki bitlik göstergesi 0b00.
	veri := uint32(0b00)<<3 | uint32(maske)
	bch := veri << 10
	for i := 0; i < 5; i++ {
		if bch&(1<<uint(14-i)) != 0 {
			bch ^= 0x537 << uint(4-i)
		}
	}
	bits := (veri<<10 | bch) ^ 0x5412 // standart maske deseni

	n := m.Size
	for i := 0; i < 15; i++ {
		koyu := (bits>>uint(i))&1 == 1
		x, y := bicimKonum1(i)
		m.put(x, y, koyu, true)
		// İkinci kopya bilerek var: biri hasar görürse kod hâlâ okunur.
		x, y = bicimKonum2(i, n)
		m.put(x, y, koyu, true)
	}
}

// bicimKonum1 returns the i-th format bit position around the top-left finder.
//
// 6. satır ve 6. sütun ATLANIR: onlar zamanlama desenidir.
func bicimKonum1(i int) (int, int) {
	switch {
	case i < 6:
		return 8, i
	case i == 6:
		return 8, 7
	case i == 7:
		return 8, 8
	case i == 8:
		return 7, 8
	default:
		return 14 - i, 8
	}
}

// bicimKonum2 returns the i-th position of the duplicate format copy.
//
// İlk 8 bit sağ üstte (8. satır), kalan 7 bit sol altta (8. sütun).
// (8, n-8) KOYU MODÜLDÜR ve bu listeye GİRMEZ.
func bicimKonum2(i, n int) (int, int) {
	if i < 8 {
		return n - 1 - i, 8
	}
	return 8, n - 15 + i
}

// ceza scores a masked matrix with the standard's four rules.
func (m *Matrix) ceza() int {
	n, toplam := m.Size, 0

	// Kural 1: aynı renkten 5+ ardışık modül.
	say := func(getir func(i, j int) bool) {
		for i := 0; i < n; i++ {
			uzunluk, onceki := 0, false
			for j := 0; j < n; j++ {
				v := getir(i, j)
				if j > 0 && v == onceki {
					uzunluk++
				} else {
					if uzunluk >= 5 {
						toplam += 3 + (uzunluk - 5)
					}
					uzunluk = 1
				}
				onceki = v
			}
			if uzunluk >= 5 {
				toplam += 3 + (uzunluk - 5)
			}
		}
	}
	say(func(i, j int) bool { return m.At(j, i) })
	say(func(i, j int) bool { return m.At(i, j) })

	// Kural 2: 2x2 tek renk blokları.
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			v := m.At(x, y)
			if m.At(x+1, y) == v && m.At(x, y+1) == v && m.At(x+1, y+1) == v {
				toplam += 3
			}
		}
	}

	// Kural 3: bulucu desene benzeyen 1:1:3:1:1 dizisi.
	desen := []bool{true, false, true, true, true, false, true}
	bosluk := []bool{false, false, false, false}
	esles := func(x, y, dx, dy int, d []bool) bool {
		for k := range d {
			if m.At(x+dx*k, y+dy*k) != d[k] {
				return false
			}
		}
		return true
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			for _, d := range [2][2]int{{1, 0}, {0, 1}} {
				dx, dy := d[0], d[1]
				if x+dx*10 < n && y+dy*10 < n {
					if esles(x, y, dx, dy, desen) && esles(x+dx*7, y+dy*7, dx, dy, bosluk) {
						toplam += 40
					}
					if esles(x, y, dx, dy, bosluk) && esles(x+dx*4, y+dy*4, dx, dy, desen) {
						toplam += 40
					}
				}
			}
		}
	}

	// Kural 4: koyu modül oranının %50'den sapması.
	koyu := 0
	for _, v := range m.mods {
		if v {
			koyu++
		}
	}
	yuzde := koyu * 100 / (n * n)
	sapma := yuzde - 50
	if sapma < 0 {
		sapma = -sapma
	}
	toplam += (sapma / 5) * 10

	return toplam
}
