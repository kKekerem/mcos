package vnc

// ════════════════════════════════════════════════════════════════════════════
// X11 KEYSYM -> LINUX TUŞ KODU
// ════════════════════════════════════════════════════════════════════════════
//
// VNC istemcileri tuşları X11 keysym olarak gönderir (RFB'nin tanımı). uinput
// ise Linux girdi katmanının tuş kodlarını ister. Arada bir tablo gerekiyor.
//
// ── Neden tam bir tablo değil ───────────────────────────────────────────────
//
// X11'de binlerce keysym var. MCOS'un paneli klavyeden yalnızca şunları
// kullanıyor: harfler, rakamlar, yön tuşları, Enter/Esc/Tab/Backspace, boşluk,
// işlev tuşları (F12 eski panele dönüş) ve değiştiriciler. Türkçe klavye
// düzenindeki harfler (ığüşöç) da parola alanlarında gerekiyor.
//
// Tablo bunları kapsıyor. Eşlenmemiş bir keysym SESSİZCE yok sayılıyor —
// yanlış bir tuş göndermek, kullanıcının yazdığından başka bir şeyin
// yazılması demekti ki bu sessiz veri bozulmasıdır.
//
// ── Türkçe harfler ──────────────────────────────────────────────────────────
//
// Sistem Türkçe Q klavye düzeni yüklüyor (loadkeys trq). Yani ığüşöç tuşları
// KONUM olarak İngilizce düzendeki karşılıklarına denk gelir: ör. "ı" tuşu
// fiziksel olarak "i" konumundadır. Bu yüzden Latin-5 keysym'leri de o
// konumların tuş koduna eşleniyor; VNC istemcisi hangi düzeni kullanırsa
// kullansın harf doğru çıkıyor.

// Linux input-event-codes.h değerleri.
const (
	kEsc        = 1
	k1          = 2
	kMinus      = 12
	kEqual      = 13
	kBackspace  = 14
	kTab        = 15
	kQ          = 16
	kW          = 17
	kE          = 18
	kR          = 19
	kT          = 20
	kY          = 21
	kU          = 22
	kI          = 23
	kO          = 24
	kP          = 25
	kLeftBrace  = 26
	kRightBrace = 27
	kEnter      = 28
	kLeftCtrl   = 29
	kA          = 30
	kS          = 31
	kD          = 32
	kF          = 33
	kG          = 34
	kH          = 35
	kJ          = 36
	kK          = 37
	kL          = 38
	kSemicolon  = 39
	kApostrophe = 40
	kGrave      = 41
	kLeftShift  = 42
	kBackslash  = 43
	kZ          = 44
	kX          = 45
	kC          = 46
	kV          = 47
	kB          = 48
	kN          = 49
	kM          = 50
	kComma      = 51
	kDot        = 52
	kSlash      = 53
	kRightShift = 54
	kLeftAlt    = 56
	kSpace      = 57
	kCapsLock   = 58
	kF1         = 59
	kF11        = 87
	kF12        = 88
	kHome       = 102
	kUp         = 103
	kPageUp     = 104
	kLeft       = 105
	kRight      = 106
	kEnd        = 107
	kDown       = 108
	kPageDown   = 109
	kInsert     = 110
	kDelete     = 111
	kRightCtrl  = 97
	kRightAlt   = 100
	kLeftMeta   = 125
)

// keysymToCode maps an X11 keysym to a Linux key code.
func keysymToCode(ks uint32) (int, bool) {
	// Harfler: keysym'de büyük/küçük ayrı değerlerdir, tuş kodu AYNIDIR.
	// Büyük harf, istemcinin ayrıca Shift göndermesiyle oluşur.
	switch {
	case ks >= 'a' && ks <= 'z':
		return letterCode(byte(ks)), true
	case ks >= 'A' && ks <= 'Z':
		return letterCode(byte(ks - 'A' + 'a')), true
	case ks >= '1' && ks <= '9':
		return k1 + int(ks-'1'), true
	case ks == '0':
		return k1 + 9, true
	}

	if c, ok := specialKeysyms[ks]; ok {
		return c, true
	}
	// İşlev tuşları: XK_F1 = 0xFFBE ... XK_F12 = 0xFFC9
	if ks >= 0xFFBE && ks <= 0xFFC8 {
		return kF1 + int(ks-0xFFBE), true
	}
	if ks == 0xFFC9 {
		return kF12, true
	}
	return 0, false
}

func letterCode(c byte) int {
	// QWERTY sırası: tuş kodları klavyedeki KONUMA göre dizilidir.
	const row1 = "qwertyuiop"
	const row2 = "asdfghjkl"
	const row3 = "zxcvbnm"
	if i := indexByte(row1, c); i >= 0 {
		return kQ + i
	}
	if i := indexByte(row2, c); i >= 0 {
		return kA + i
	}
	if i := indexByte(row3, c); i >= 0 {
		return kZ + i
	}
	return 0
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// specialKeysyms covers everything that is not a plain letter or digit.
var specialKeysyms = map[uint32]int{
	0xFF08: kBackspace, // XK_BackSpace
	0xFF09: kTab,
	0xFF0D: kEnter,
	0xFF1B: kEsc,
	0xFF50: kHome,
	0xFF51: kLeft,
	0xFF52: kUp,
	0xFF53: kRight,
	0xFF54: kDown,
	0xFF55: kPageUp,
	0xFF56: kPageDown,
	0xFF57: kEnd,
	0xFF63: kInsert,
	0xFFFF: kDelete,
	0xFF8D: kEnter, // KP_Enter
	0xFFE1: kLeftShift,
	0xFFE2: kRightShift,
	0xFFE3: kLeftCtrl,
	0xFFE4: kRightCtrl,
	0xFFE9: kLeftAlt,
	0xFFEA: kRightAlt, // AltGr
	0xFFE5: kCapsLock,
	0xFFEB: kLeftMeta,

	' ':  kSpace,
	'-':  kMinus,
	'=':  kEqual,
	'[':  kLeftBrace,
	']':  kRightBrace,
	';':  kSemicolon,
	'\'': kApostrophe,
	'`':  kGrave,
	'\\': kBackslash,
	',':  kComma,
	'.':  kDot,
	'/':  kSlash,

	// Shift'li işaretler: istemci Shift'i AYRICA gönderse de bazı istemciler
	// yalnızca sonuç keysym'ini yollar. Tuş KONUMUNU veriyoruz; Shift durumu
	// istemciden gelir.
	'!': k1, '@': k1 + 1, '#': k1 + 2, '$': k1 + 3, '%': k1 + 4,
	'^': k1 + 5, '&': k1 + 6, '*': k1 + 7, '(': k1 + 8, ')': k1 + 9,
	'_': kMinus, '+': kEqual, '{': kLeftBrace, '}': kRightBrace,
	':': kSemicolon, '"': kApostrophe, '~': kGrave, '|': kBackslash,
	'<': kComma, '>': kDot, '?': kSlash,

	// ── Türkçe (Latin-5) harfler ────────────────────────────────────────
	// Türkçe Q düzeninde bu harflerin fiziksel konumları:
	//   ı -> i konumu      İ -> i konumu
	//   ğ -> [ konumu      Ğ -> [ konumu
	//   ü -> ] konumu      Ü -> ] konumu
	//   ş -> ; konumu      Ş -> ; konumu
	//   i -> i konumu (düzen zaten Türkçe)
	//   ö -> , konumu      Ö -> , konumu
	//   ç -> . konumu      Ç -> . konumu
	0x00FD: kI,         // ı (yumuşak ı)
	0x00DD: kI,         // İ
	0x00F0: kLeftBrace, // ğ
	0x00D0: kLeftBrace, // Ğ
	0x00FC: kRightBrace,
	0x00DC: kRightBrace,
	0x00FE: kSemicolon, // ş
	0x00DE: kSemicolon, // Ş
	0x00F6: kComma,     // ö
	0x00D6: kComma,     // Ö
	0x00E7: kDot,       // ç
	0x00C7: kDot,       // Ç
}
