package theme

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestPaletteEscapeFormat, fbterm'in beklediği biçimi doğrular.
//
// fbterm src/lib/vterm_action.cpp:696 (fbterm_specific case 3):
//
//	if (npar == 4) request(PaletteSet, (param[1]<<24)|(param[2]<<16)|(param[3]<<8)|param[4]);
//
// yani dizi ESC [ 3 ; indeks ; r ; g ; b } biçiminde ve TAM 4 parametre
// taşımalıdır (3'ten sonra).
func TestPaletteEscapeFormat(t *testing.T) {
	esc := PaletteEscapes("graphite-teal")

	// 16 yuvanın hepsi ayarlanmalı. Eski kod yalnızca 0-7'yi ayarlıyordu; bold
	// metin 8-15'e düştüğü için (fbshell.cpp:704) vurgular gömülü VGA
	// renklerinde çiziliyordu.
	if got := strings.Count(esc, "\x1b[3;"); got != 16 {
		t.Errorf("palet dizisi sayısı = %d, 16 olmalı", got)
	}

	for i, want := range paletteFor("graphite-teal") {
		r, g, b, ok := parseHexRGB(want)
		if !ok {
			t.Fatalf("yuva %d: geçersiz hex %q", i, want)
		}
		expect := fmt.Sprintf("\x1b[3;%d;%d;%d;%d}", i, r, g, b)
		if !strings.Contains(esc, expect) {
			t.Errorf("yuva %d için dizi bulunamadı: %q", i, strings.TrimPrefix(expect, "\x1b"))
		}
	}

	// Her dizi "}" ile kapanmalı ve tam 4 parametre taşımalı.
	for _, seq := range strings.Split(esc, "\x1b")[1:] {
		if !strings.HasSuffix(seq, "}") {
			t.Errorf("dizi } ile kapanmıyor: %q", seq)
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(seq, "[3;"), "}")
		if n := len(strings.Split(body, ";")); n != 4 {
			t.Errorf("dizi %q: %d parametre, 4 olmalı", seq, n)
		}
	}
}

// TestSemanticSlotsAreDistinct, B2'nin regresyon testi: eskiden Border ve
// Muted ikisi de "5" yuvasındaydı, yani kenarlıklar ile ikincil metin ayırt
// edilemiyordu.
func TestSemanticSlotsAreDistinct(t *testing.T) {
	p := semanticPalette
	slots := map[string]string{
		"Bg":     string(p.Bg),
		"Border": string(p.Border),
		"Accent": string(p.Accent),
		"Text":   string(p.Text),
		"Muted":  string(p.Muted),
	}
	seen := map[string]string{}
	for role, slot := range slots {
		if other, dup := seen[slot]; dup {
			t.Errorf("%s ve %s aynı yuvayı (%s) kullanıyor — ayırt edilemezler", role, other, slot)
		}
		seen[slot] = role
	}
}

// TestUsableSlotsOnly: fbterm 90-97 (parlak) SGR'sini işlemez
// (vterm_action.cpp'de case yok), bu yüzden hiçbir semantik rol 8-15
// yuvalarını DOĞRUDAN kullanmamalı. Parlak renklere yalnızca bold ile erişilir.
func TestUsableSlotsOnly(t *testing.T) {
	p := semanticPalette
	for role, slot := range map[string]string{
		"Bg": string(p.Bg), "Border": string(p.Border), "Accent": string(p.Accent),
		"Text": string(p.Text), "Muted": string(p.Muted),
		"OK": string(p.OK), "Warn": string(p.Warn), "Error": string(p.Error),
	} {
		switch slot {
		case "0", "1", "2", "3", "4", "5", "6", "7":
		default:
			t.Errorf("%s yuva %q kullanıyor — fbterm yalnızca 0-7'yi doğrudan adresler", role, slot)
		}
	}
}

// TestFaintSlotIsLegible: fbterm faint'i her zaman 8. yuvaya sabitler
// (fbshell.cpp:701). Bu yüzden 8. yuva arka planla karışmayacak kadar açık
// olmalı, yoksa yanlışlıkla Faint kullanan bir metin görünmez olur.
func TestFaintSlotIsLegible(t *testing.T) {
	p := paletteFor(defaultTheme)
	bgR, bgG, bgB, _ := parseHexRGB(p[0])
	dR, dG, dB, _ := parseHexRGB(p[8])

	// Basit parlaklık farkı ölçüsü yeterli.
	lum := func(r, g, b int) int { return (299*r + 587*g + 114*b) / 1000 }
	diff := lum(dR, dG, dB) - lum(bgR, bgG, bgB)
	if diff < 40 {
		t.Errorf("yuva 8 (faint hedefi) arka plandan yalnızca %d parlaklık farklı — okunmaz", diff)
	}
}

// TestAllThemesResolve: her tema geçerli olmalı ve kendi vurgu rengini almalı.
func TestAllThemesResolve(t *testing.T) {
	seenAccent := map[string]string{}
	for _, name := range Names() {
		if !Valid(name) {
			t.Errorf("Names() geçersiz tema döndürdü: %q", name)
			continue
		}
		th := New(name)
		if th.Name != name {
			t.Errorf("New(%q).Name = %q", name, th.Name)
		}
		if Label(name) == name {
			t.Errorf("%q için Türkçe etiket tanımlı değil", name)
		}

		// B2: eskiden anthracite-orange ve amber-graphite BİREBİR aynıydı
		// (ikisi de Accent="3"). Artık her temanın vurgu rengi farklı olmalı.
		hex := AccentHex(name)
		if other, dup := seenAccent[hex]; dup {
			t.Errorf("%q ve %q aynı vurgu rengini (%s) kullanıyor", name, other, hex)
		}
		seenAccent[hex] = name
	}
}

func TestUnknownThemeFallsBack(t *testing.T) {
	th := New("boyle-bir-tema-yok")
	if th.Name != defaultTheme {
		t.Errorf("bilinmeyen tema %q'ya düşmedi: %q", defaultTheme, th.Name)
	}
}

// TestApplyThemeSkipsNonFbterm: palet dizisi yalnızca fbterm'de yazılmalı,
// aksi halde geliştirici terminaline çöp basar.
func TestApplyThemeSkipsNonFbterm(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	var buf bytes.Buffer
	ApplyTheme(&buf, "graphite-teal")
	if buf.Len() != 0 {
		t.Errorf("fbterm olmayan terminale %d bayt yazıldı", buf.Len())
	}

	t.Setenv("TERM", "fbterm")
	buf.Reset()
	ApplyTheme(&buf, "graphite-teal")
	if buf.Len() == 0 {
		t.Error("fbterm'de palet yazılmadı")
	}
}

// TestTokenConsistency: ölçü token'ları birbiriyle tutarlı olmalı.
func TestTokenConsistency(t *testing.T) {
	if ContentWidthMin >= ContentWidthMax {
		t.Error("ContentWidthMin >= ContentWidthMax")
	}
	if LabelWidth+ValueWidthMin > ContentWidthMin {
		t.Errorf("etiket(%d)+değer(%d) en küçük içerik genişliğine(%d) sığmıyor",
			LabelWidth, ValueWidthMin, ContentWidthMin)
	}
	if InnerWidth(ContentWidthMin) < 1 {
		t.Error("InnerWidth(ContentWidthMin) < 1")
	}
	if ChromeHeight != HeaderHeight+FooterHeight+2 {
		t.Error("ChromeHeight bileşenleriyle tutarsız")
	}
	if ListHeight(0) < ListHeightMin {
		t.Error("ListHeight en küçük değeri korumuyor")
	}

	// StateLabelWidth, en uzun durum etiketini tam almalı — yoksa liste
	// sütunları duruma göre kayar.
	for _, l := range []string{
		StateLabelRunning, StateLabelStarting, StateLabelStopping,
		StateLabelStopped, StateLabelError,
	} {
		if n := len([]rune(l)); n > StateLabelWidth {
			t.Errorf("durum etiketi %q (%d) StateLabelWidth(%d) değerini aşıyor", l, n, StateLabelWidth)
		}
	}
}

// TestContentWidthClamps: içerik bandı hem çok dar hem çok geniş ekranı
// sınırlar (200 kolonda satırlar okunamaz hale gelmesin).
func TestContentWidthClamps(t *testing.T) {
	cases := []struct{ in, want int }{
		{10, ContentWidthMin},
		{ContentWidthMin, ContentWidthMin},
		{80, 80},
		{ContentWidthMax, ContentWidthMax},
		{300, ContentWidthMax},
	}
	for _, c := range cases {
		if got := ContentWidth(c.in); got != c.want {
			t.Errorf("ContentWidth(%d) = %d, beklenen %d", c.in, got, c.want)
		}
	}
}

// TestIconsAreSingleRune: TUI hizalaması tek genişlikli glif varsayar. Emoji
// çift genişlikte olduğu için token setinde emoji BULUNMAMALI.
func TestIconsAreSingleRune(t *testing.T) {
	icons := map[string]string{
		"IconCursor": IconCursor, "IconSelected": IconSelected,
		"IconUnselect": IconUnselect, "IconChecked": IconChecked,
		"IconUnchcked": IconUnchcked, "IconOK": IconOK, "IconFail": IconFail,
		"IconWarn": IconWarn, "IconDash": IconDash, "IconDotOn": IconDotOn,
		"IconDotOf": IconDotOf, "IconUp": IconUp, "IconDown": IconDown,
		"IconLeft": IconLeft, "IconRight": IconRight,
		"BarFill": BarFill, "BarEmpty": BarEmpty,
		"SepVert": SepVert, "SepHoriz": SepHoriz, "SepDot": SepDot,
		"Ellipsis": Ellipsis,
	}
	for name, g := range icons {
		if n := len([]rune(g)); n != 1 {
			t.Errorf("%s = %q: %d rune, tek rune olmalı", name, g, n)
		}
		// Emoji aralıkları (çift genişlikli) yasak.
		for _, r := range g {
			if r >= 0x1F300 && r <= 0x1FAFF {
				t.Errorf("%s = %q emoji içeriyor — hizalamayı bozar", name, g)
			}
		}
	}
}
