// Package fbinput turns the raw terminal byte stream into key events.
//
// ── Neden kendi çözücümüz var? ──────────────────────────────────────────────
// Konsolu ham (raw) kipe aldığımız için çekirdek bize satır değil BAYT verir.
// Ok tuşları, F tuşları ve Home/End çok baytlı kaçış dizileri olarak gelir;
// Türkçe harfler (ğ, ı, ş, ö, ç, ü) ise çok baytlı UTF-8 olarak. İkisini
// karıştırmadan ayırmak bu paketin tek işidir.
//
// Klavye DÜZENİ burada uygulanmaz: konsolu K_UNICODE kipinde tuttuğumuz için
// çekirdek yüklü düzeni (ör. trq) zaten uygular ve bize hazır UTF-8 gönderir.
// Ham tuş kodlarını okusaydık Türkçe düzeni elle yazmamız gerekirdi.
package fbinput

import (
	"unicode/utf8"
)

// Key is one decoded keystroke.
//
// Name boş ise tuş yazdırılabilir bir karakterdir ve Rune doludur.
// Name doluysa özel bir tuştur ("up", "enter", "f1", "ctrl+c"...).
type Key struct {
	Name string
	Rune rune
}

// String renders the key the way the panel's key switches expect.
func (k Key) String() string {
	if k.Name != "" {
		return k.Name
	}
	return string(k.Rune)
}

// IsRune reports whether this is an ordinary printable character.
func (k Key) IsRune() bool { return k.Name == "" }

// Decoder accumulates bytes and emits keys.
//
// Kısmi diziler saklanır: okuma sınırı bir kaçış dizisinin ortasına denk
// gelirse tuş kaybolmaz, sonraki okumada tamamlanır.
type Decoder struct {
	buf []byte
}

// Decode consumes b and returns the keys that could be fully decoded.
//
// Tamamlanmamış son dizi içeride tutulur; Flush() ile zorlanabilir.
func (d *Decoder) Decode(b []byte) []Key {
	d.buf = append(d.buf, b...)
	var out []Key
	for {
		k, n, ok := d.next(false)
		if !ok {
			break
		}
		d.buf = d.buf[n:]
		if k != nil {
			out = append(out, *k)
		}
	}
	return out
}

// Flush forces a decision on a pending partial sequence.
//
// NEDEN GEREKLİ: yalnız başına ESC ile bir kaçış dizisinin BAŞLANGICI aynı
// bayttır. Ayırt etmenin tek yolu zaman aşımıdır: kısa bir süre içinde devamı
// gelmezse kullanıcı gerçekten Esc'ye basmıştır. Çağıran bu zaman aşımını
// yönetir ve Flush'u çağırır.
func (d *Decoder) Flush() []Key {
	var out []Key
	for len(d.buf) > 0 {
		k, n, ok := d.next(true)
		if !ok || n == 0 {
			d.buf = d.buf[:0]
			break
		}
		d.buf = d.buf[n:]
		if k != nil {
			out = append(out, *k)
		}
	}
	return out
}

// Pending reports whether an incomplete sequence is being held.
func (d *Decoder) Pending() bool { return len(d.buf) > 0 }

// next decodes one key from the front of the buffer.
//
// ok=false: daha fazla bayt gerekiyor (final=false iken).
// k=nil, ok=true: bayt tüketildi ama tuş üretmedi (tanınmayan dizi).
func (d *Decoder) next(final bool) (k *Key, n int, ok bool) {
	if len(d.buf) == 0 {
		return nil, 0, false
	}
	c := d.buf[0]

	if c == 0x1B { // ESC
		return d.escape(final)
	}
	if key, consumed := controlKey(d.buf); consumed > 0 {
		return &key, consumed, true
	}

	// Yazdırılabilir karakter: UTF-8 çöz.
	r, size := utf8.DecodeRune(d.buf)
	if r == utf8.RuneError && size <= 1 {
		if !final && len(d.buf) < utf8.UTFMax {
			// Çok baytlı karakterin tamamı henüz gelmemiş olabilir.
			return nil, 0, false
		}
		// Gerçekten bozuk bayt: at.
		return nil, 1, true
	}
	return &Key{Rune: r}, size, true
}

// controlKey maps C0 control bytes to key names.
func controlKey(b []byte) (Key, int) {
	switch b[0] {
	case 0x0D, 0x0A:
		return Key{Name: "enter"}, 1
	case 0x09:
		return Key{Name: "tab"}, 1
	case 0x7F, 0x08:
		return Key{Name: "backspace"}, 1
	case 0x00:
		return Key{Name: "ctrl+@"}, 1
	}
	// Ctrl+A .. Ctrl+Z = 0x01..0x1A. Tab (0x09), LF (0x0A) ve CR (0x0D)
	// yukarıda ele alındığı için buraya düşmez.
	if b[0] >= 0x01 && b[0] <= 0x1A {
		return Key{Name: "ctrl+" + string(rune('a'+b[0]-1))}, 1
	}
	return Key{}, 0
}

// escape decodes an ESC-prefixed sequence.
func (d *Decoder) escape(final bool) (*Key, int, bool) {
	b := d.buf
	if len(b) == 1 {
		if final {
			return &Key{Name: "esc"}, 1, true
		}
		return nil, 0, false // devamı gelebilir
	}

	switch b[1] {
	case '[':
		return d.csi(final)
	case 'O':
		// SS3: uygulama kipinde F1-F4 ve bazı tuş takımı tuşları.
		if len(b) < 3 {
			if final {
				return &Key{Name: "esc"}, 1, true
			}
			return nil, 0, false
		}
		switch b[2] {
		case 'P':
			return &Key{Name: "f1"}, 3, true
		case 'Q':
			return &Key{Name: "f2"}, 3, true
		case 'R':
			return &Key{Name: "f3"}, 3, true
		case 'S':
			return &Key{Name: "f4"}, 3, true
		case 'H':
			return &Key{Name: "home"}, 3, true
		case 'F':
			return &Key{Name: "end"}, 3, true
		}
		return nil, 3, true
	}

	// ESC + harf = Alt+harf. Menü kısayolları için kullanışlı.
	if r, size := utf8.DecodeRune(b[1:]); r != utf8.RuneError {
		return &Key{Name: "alt+" + string(r)}, 1 + size, true
	}
	return &Key{Name: "esc"}, 1, true
}

// csi decodes ESC [ ... sequences.
//
// Linux konsolu ile xterm FARKLI diziler üretir (özellikle F tuşlarında);
// ikisi de desteklenir, çünkü panel hem gerçek konsolda hem de geliştirme
// sırasında bir terminal öykünücüsünde çalışır.
func (d *Decoder) csi(final bool) (*Key, int, bool) {
	b := d.buf
	// ESC [ [ X  -> Linux konsolunda F1..F5
	if len(b) >= 3 && b[2] == '[' {
		if len(b) < 4 {
			if final {
				return &Key{Name: "esc"}, 1, true
			}
			return nil, 0, false
		}
		if b[3] >= 'A' && b[3] <= 'E' {
			return &Key{Name: fkey(int(b[3]-'A') + 1)}, 4, true
		}
		return nil, 4, true
	}

	// CSI biçimi (ECMA-48): ESC [ <parametre baytları 0x30-0x3F>
	// <ara baytlar 0x20-0x2F> <sonlandırıcı 0x40-0x7E>.
	//
	// Parametre aralığı YALNIZCA rakam ve ';' DEĞİLDİR: '<', '=', '>', '?'
	// de özel parametre önekleridir. Bunları saymayan bir ayrıştırıcı,
	// terminalin aygıt yanıtını (ör. "\x1b[>0;276;0c") dizinin ortasında
	// kesip geri kalanını EKRANA YAZARDI.
	i := 2
	for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3F {
		i++
	}
	paramEnd := i
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2F { // ara baytlar
		i++
	}
	if i >= len(b) {
		if final {
			return &Key{Name: "esc"}, 1, true
		}
		return nil, 0, false // sonlandırıcı henüz gelmedi
	}
	if b[i] < 0x40 || b[i] > 0x7E {
		// Geçersiz sonlandırıcı: diziyi at, akışı bozma.
		return nil, i + 1, true
	}
	params := string(b[2:paramEnd])
	end := b[i]
	n := i + 1

	// Özel önekli diziler (ESC [ > … , ESC [ ? …) tuş değil, terminal
	// yanıtıdır: sessizce yutulur.
	if len(params) > 0 && (params[0] < '0' || params[0] > '9') {
		return nil, n, true
	}

	switch end {
	case 'A':
		return &Key{Name: withMod(params, "up")}, n, true
	case 'B':
		return &Key{Name: withMod(params, "down")}, n, true
	case 'C':
		return &Key{Name: withMod(params, "right")}, n, true
	case 'D':
		return &Key{Name: withMod(params, "left")}, n, true
	case 'H':
		return &Key{Name: "home"}, n, true
	case 'F':
		return &Key{Name: "end"}, n, true
	case 'Z':
		return &Key{Name: "shift+tab"}, n, true
	case '~':
		if k := tildeKey(params); k != "" {
			return &Key{Name: k}, n, true
		}
		return nil, n, true
	}
	return nil, n, true // tanınmayan dizi: sessizce at
}

// tildeKey maps the numeric parameter of ESC [ N ~ sequences.
func tildeKey(params string) string {
	// "5;2" gibi değiştirici ekleri olabilir; ilk sayıyı al.
	num := 0
	for i := 0; i < len(params); i++ {
		if params[i] < '0' || params[i] > '9' {
			break
		}
		num = num*10 + int(params[i]-'0')
	}
	switch num {
	case 1, 7:
		return "home"
	case 2:
		return "insert"
	case 3:
		return "delete"
	case 4, 8:
		return "end"
	case 5:
		return "pgup"
	case 6:
		return "pgdn"
	case 11, 12, 13, 14, 15:
		return fkey(num - 10)
	case 17, 18, 19, 20, 21:
		return fkey(num - 11)
	case 23, 24:
		return fkey(num - 12)
	}
	return ""
}

// withMod prefixes a key name with its modifier, e.g. "shift+up".
//
// Biçim: ESC [ 1 ; M <harf>, burada M-1 bir bit maskesidir
// (1=shift, 2=alt, 4=ctrl).
func withMod(params, base string) string {
	semi := -1
	for i := 0; i < len(params); i++ {
		if params[i] == ';' {
			semi = i
			break
		}
	}
	if semi < 0 || semi+1 >= len(params) {
		return base
	}
	m := 0
	for i := semi + 1; i < len(params); i++ {
		if params[i] < '0' || params[i] > '9' {
			break
		}
		m = m*10 + int(params[i]-'0')
	}
	if m <= 1 {
		return base
	}
	m--
	prefix := ""
	if m&4 != 0 {
		prefix += "ctrl+"
	}
	if m&2 != 0 {
		prefix += "alt+"
	}
	if m&1 != 0 {
		prefix += "shift+"
	}
	return prefix + base
}

func fkey(n int) string {
	if n < 1 || n > 12 {
		return ""
	}
	if n < 10 {
		return "f" + string(rune('0'+n))
	}
	return "f1" + string(rune('0'+n-10))
}
