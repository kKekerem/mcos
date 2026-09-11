package theme

import (
	"fmt"
	"io"
	"strings"
)

// RENK MİMARİSİ — neden böyle.
//
// Panel fbterm (framebuffer terminali) altında çalışır. fbterm 1.7.0'ın renk
// yeteneği kaynak kodundan ölçüldü:
//
//   - src/lib/vterm_action.cpp  "case 30 ... 37" → indeks 0-7 çalışır.
//   - Aynı switch'te 90-97 (parlak renkler) için CASE YOK → sessizce yok
//     sayılır. Yani lipgloss'un "8".."15" renkleri HİÇ görünmez.
//   - src/fbshell.cpp:704  bold (intensity==2) ise fcolor ^= 8 → parlak
//     varyantlara YALNIZCA bold ile erişilir.
//   - src/fbshell.cpp:701  faint (intensity==0) ise fcolor = 8 → Faint her
//     zaman 8. yuvayı kullanır, o yüzden 8. yuva okunabilir bir gri olmalı.
//   - src/lib/vterm_action.cpp:522  "case 38" SGR'yi ALTÇİZGİ olarak yorumlar
//     → 256 renk (ESC[38;5;Nm) DESTEKLENMEZ. termenv.ANSI zorunludur.
//
// Sonuç: yalnızca 0-7 doğrudan adreslenebilir, 8-15 bold ile. Bu yüzden
// paletin 8-15 aralığı, 0-7'nin AYNI TONDA parlak karşılıkları olarak
// tasarlandı — böylece bir öğeyi bold yapmak rengini değiştirmez, sadece
// vurgular.
//
// Ayrıca: .fbtermrc içindeki "color-0=..." satırları fbterm tarafından HİÇ
// OKUNMAZ (src/fbconfig.cpp yalnızca color-foreground ve color-background
// seçeneklerini tanır). O blok yıllardır ölü kod olduğu için kullanıcı
// fbterm'in gömülü VGA paletini görüyordu: yuva 5 = #aa00aa koyu magenta,
// yani kenarlıklar ve ikincil metin magenta çiziliyordu.
//
// Paleti gerçekten değiştirmenin yolu Linux konsol escape dizisidir:
//
//	ESC ] P <i> <rr> <gg> <bb>      (i: tek onaltılık hane, rrggbb: 6 hane)
//
// fbterm bunu src/lib/vterm_action.cpp:655 set_palette() içinde işler ve
// 7 onaltılık haneyi toplayıp PaletteSet isteğine dönüştürür.

// Slot indices. Görünümler renk seçmek için BU adları kullanır, ham sayı
// yazmaz. Yalnızca 0-7 doğrudan kullanılabilir (bkz. yukarıdaki not).
const (
	SlotBg     = "0" // sayfa arka planı
	SlotError  = "1" // hata / çöktü
	SlotOK     = "2" // çalışıyor / başarılı
	SlotWarn   = "3" // başlıyor / uyarı
	SlotBorder = "4" // kenarlık, ayırıcı
	SlotMuted  = "5" // ikincil metin, ipucu
	SlotAccent = "6" // vurgu: seçim, odak, başlık
	SlotText   = "7" // ana metin
)

// paletteRGB is one full 16-entry fbterm palette as RRGGBB hex strings.
// 0-7 taban, 8-15 aynı tonun parlak karşılığı (bold ile erişilir).
type paletteRGB [16]string

// basePalette, tüm temaların paylaştığı nötr grafit iskelet. Temalar yalnızca
// vurgu yuvalarını (6 ve 14) değiştirir; böylece "5 tema" gerçekten farklı
// görünür ama okunabilirlik hiç bozulmaz.
//
// Eskiden 6 palet tanımlıydı ama hepsi Accent dışında BİREBİR aynıydı ve
// dahası Border ile Muted ikisi de "5" yuvasını kullanıyordu — yani kenarlık
// ile ikincil metin ayırt edilemiyordu.
var basePalette = paletteRGB{
	/* 0  bg           */ "0f1216",
	/* 1  error        */ "e5484d",
	/* 2  ok           */ "46a758",
	/* 3  warn         */ "d9a21b",
	// Kenarlık, arka plandan AÇIKÇA ayrılmalı. Önceki değer (#2f3945) arka
	// planla (#0f1216) yalnızca ~40 parlaklık farkındaydı ve düşük kontrastlı
	// panellerde çerçeveler kayboluyordu — "kenarlar görünmüyor / parlaklık
	// böyle olmamalı" şikâyetinin bir parçası.
	/* 4  border       */ "465463",
	/* 5  muted        */ "78838f",
	/* 6  accent       */ "23a99c", // tema tarafından değiştirilir
	/* 7  text         */ "c9d1d9",
	/* 8  dim (Faint)  */ "5a6673", // okunabilir olmalı: Faint hep buraya düşer
	/* 9  error+       */ "ff6369",
	/* 10 ok+          */ "5bd97a",
	/* 11 warn+        */ "ffd25e",
	/* 12 border+      */ "6b7c8f", // odaklı kenarlık = bold(border)
	/* 13 muted+       */ "9aa7b4",
	/* 14 accent+      */ "3fdecd", // tema tarafından değiştirilir
	/* 15 text+        */ "f0f4f8", // başlık = bold(text)
}

// accents maps a theme name to its (accent, accentBright) pair.
type accentPair struct{ base, bright string }

var accents = map[string]accentPair{
	"graphite-teal":     {"23a99c", "3fdecd"}, // teal
	"noir-purple":       {"8b5cf6", "b794ff"}, // mor
	"anthracite-orange": {"e07a3f", "ff9d5c"}, // turuncu
	"anthracite-green":  {"46a758", "5bd97a"}, // yeşil
	"crimson-night":     {"d64550", "ff6b76"}, // kızıl
	"amber-graphite":    {"d9a21b", "ffd25e"}, // kehribar
}

// paletteFor returns the full 16-entry palette for a theme name.
func paletteFor(name string) paletteRGB {
	p := basePalette
	if a, ok := accents[name]; ok {
		p[6] = a.base
		p[14] = a.bright
	}
	return p
}

// InstallPalette writes the palette escapes for a theme to w.
//
// Panel açılışında ve tema değiştiğinde çağrılır. Palet dizileri imleci
// oynatmaz ve ekranı temizlemez — yalnızca renk yazmaçlarını değiştirir, bu
// yüzden Bubble Tea çizim yaparken araya girmeleri görsel olarak güvenlidir.
func InstallPalette(w io.Writer, themeName string) error {
	_, err := io.WriteString(w, PaletteEscapes(themeName))
	return err
}

// PaletteEscapes returns the escape string that installs a theme's 16-colour
// palette into fbterm.
//
// fbterm'e özgü biçim kullanılır:
//
//	ESC [ 3 ; <indeks> ; <r> ; <g> ; <b> }      (ondalık)
//
// Bu, src/lib/vterm_action.cpp:696 içindeki fbterm_specific case 3 tarafından
// işlenir ve 0-255 arası indeksi destekler. Klasik Linux konsol biçimi
// (ESC]P<i><rrggbb>) de çalışır ama indeksi tek onaltılık haneyle sınırlıdır;
// ayrıca bu biçim depoda zaten kullanılıyordu, yani bu fbterm derlemesinde
// çalıştığı doğrulanmış durumda.
//
// 16 yuvanın TAMAMI ayarlanır. Eski kod yalnızca 0-7'yi ayarlıyordu; bold
// metin 8-15 yuvalarına düştüğü için (fbshell.cpp:704) vurgulu her metin
// fbterm'in gömülü VGA parlak renklerinde çiziliyordu.
func PaletteEscapes(themeName string) string {
	p := paletteFor(themeName)
	var b strings.Builder
	b.Grow(16 * 22)
	for i, hex := range p {
		r, g, bl, ok := parseHexRGB(hex)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\x1b[3;%d;%d;%d;%d}", i, r, g, bl)
	}
	return b.String()
}

// parseHexRGB decodes an "rrggbb" string into its components.
func parseHexRGB(s string) (r, g, b int, ok bool) {
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v := make([]int, 3)
	for i := 0; i < 3; i++ {
		hi, ok1 := hexNibble(s[i*2])
		lo, ok2 := hexNibble(s[i*2+1])
		if !ok1 || !ok2 {
			return 0, 0, 0, false
		}
		v[i] = hi<<4 | lo
	}
	return v[0], v[1], v[2], true
}

func hexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
