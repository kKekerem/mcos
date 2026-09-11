package fbinput

import (
	"reflect"
	"testing"
)

// names decodes b and returns the key names/runes as strings.
func names(d *Decoder, b string) []string {
	var out []string
	for _, k := range d.Decode([]byte(b)) {
		out = append(out, k.String())
	}
	return out
}

// TestTurkishCharactersSurvive — EN KRİTİK TEST.
//
// Türkçe harfler çok baytlı UTF-8'dir. Baytları tek tek ele alan bir çözücü
// onları bozuk karaktere çevirir ve kullanıcı "ağ" yazamaz. Eski panelde
// düğüm adı alanına 'l' ve 'h' yazılamıyordu; bu test o sınıftaki hataların
// yeni çözücüde olmamasını garanti eder.
func TestTurkishCharactersSurvive(t *testing.T) {
	var d Decoder
	got := names(&d, "ığüşöçİĞÜŞÖÇ")
	want := []string{"ı", "ğ", "ü", "ş", "ö", "ç", "İ", "Ğ", "Ü", "Ş", "Ö", "Ç"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Türkçe harfler bozuldu:\n aldı:  %q\n bekle: %q", got, want)
	}
}

// TestLettersAreNotSwallowed — h, l ve boşluk normal karakter olarak gelmeli.
//
// Eski panelde "left"/"h" ve "right"/"l"/" " tuş anahtarları metin alanından
// ÖNCE yakalanıyordu; sonuç: "salon" yazınca "saon" oluyordu. Çözücü
// katmanında h/l/boşluk her zaman düz karakterdir; hangi bağlamda kısayol
// sayılacağına ekran karar verir.
func TestLettersAreNotSwallowed(t *testing.T) {
	var d Decoder
	got := names(&d, "hl salon")
	want := []string{"h", "l", " ", "s", "a", "l", "o", "n"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("harfler yutuldu:\n aldı:  %q\n bekle: %q", got, want)
	}
}

// TestArrowKeys — xterm ve Linux konsolu ok dizileri.
func TestArrowKeys(t *testing.T) {
	var d Decoder
	got := names(&d, "\x1b[A\x1b[B\x1b[C\x1b[D")
	want := []string{"up", "down", "right", "left"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ok tuşları:\n aldı:  %q\n bekle: %q", got, want)
	}
}

// TestNavigationKeys — Home/End/PgUp/PgDn/Delete, her iki biçimde.
func TestNavigationKeys(t *testing.T) {
	cases := map[string]string{
		"\x1b[1~": "home",
		"\x1b[4~": "end",
		"\x1b[5~": "pgup",
		"\x1b[6~": "pgdn",
		"\x1b[3~": "delete",
		"\x1b[2~": "insert",
		"\x1b[H":  "home",
		"\x1b[F":  "end",
		"\x1bOH":  "home",
		"\x1bOF":  "end",
		"\x1b[Z":  "shift+tab",
	}
	for in, want := range cases {
		var d Decoder
		got := names(&d, in)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q -> %q, beklenen [%q]", in, got, want)
		}
	}
}

// TestFunctionKeys — Linux konsolu F1-F5 ESC [ [ A biçimini kullanır,
// xterm ise ESC O P. İkisi de çalışmalı.
func TestFunctionKeys(t *testing.T) {
	cases := map[string]string{
		"\x1b[[A":  "f1", // Linux konsolu
		"\x1b[[B":  "f2",
		"\x1b[[E":  "f5",
		"\x1bOP":   "f1", // xterm
		"\x1bOS":   "f4",
		"\x1b[15~": "f5",
		"\x1b[17~": "f6",
		"\x1b[24~": "f12",
	}
	for in, want := range cases {
		var d Decoder
		got := names(&d, in)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q -> %q, beklenen [%q]", in, got, want)
		}
	}
}

// TestControlKeys — Enter/Tab/Backspace ve Ctrl kombinasyonları.
func TestControlKeys(t *testing.T) {
	cases := map[string]string{
		"\r":   "enter",
		"\n":   "enter",
		"\t":   "tab",
		"\x7f": "backspace",
		"\x08": "backspace",
		"\x03": "ctrl+c",
		"\x13": "ctrl+s",
	}
	for in, want := range cases {
		var d Decoder
		got := names(&d, in)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q -> %q, beklenen [%q]", in, got, want)
		}
	}
}

// TestLoneEscapeNeedsFlush — yalnız ESC ancak zaman aşımıyla anlaşılır.
//
// Bu, terminal tabanlı her arayüzün klasik belirsizliğidir: ESC hem "Esc
// tuşu" hem de bir dizinin başlangıcıdır. Çözücü kendiliğinden karar VERMEZ;
// bekletir, çağıran zaman aşımında Flush çağırır.
func TestLoneEscapeNeedsFlush(t *testing.T) {
	var d Decoder
	if got := d.Decode([]byte{0x1B}); len(got) != 0 {
		t.Errorf("tek ESC hemen tuş üretti: %v — dizi başlangıcı olabilirdi", got)
	}
	if !d.Pending() {
		t.Error("ESC bekletilmedi")
	}
	out := d.Flush()
	if len(out) != 1 || out[0].Name != "esc" {
		t.Errorf("Flush sonrası %v, [esc] bekleniyordu", out)
	}
	if d.Pending() {
		t.Error("Flush sonrası hâlâ bekleyen bayt var")
	}
}

// TestSplitSequenceAcrossReads — dizi iki okumaya bölünse de kaybolmamalı.
//
// Gerçek dünyada read() bir kaçış dizisinin ortasında dönebilir. Baytları
// saklamayan bir çözücü o tuşu kaybeder veya çöp üretir.
func TestSplitSequenceAcrossReads(t *testing.T) {
	var d Decoder
	if got := d.Decode([]byte{0x1B, '['}); len(got) != 0 {
		t.Errorf("yarım dizi tuş üretti: %v", got)
	}
	got := d.Decode([]byte{'A'})
	if len(got) != 1 || got[0].Name != "up" {
		t.Errorf("bölünmüş dizi çözülemedi: %v", got)
	}
}

// TestSplitUTF8AcrossReads — çok baytlı harf bölünse de kaybolmamalı.
func TestSplitUTF8AcrossReads(t *testing.T) {
	var d Decoder
	b := []byte("ş") // 2 bayt
	if got := d.Decode(b[:1]); len(got) != 0 {
		t.Errorf("yarım UTF-8 karakter üretti: %v", got)
	}
	got := d.Decode(b[1:])
	if len(got) != 1 || got[0].Rune != 'ş' {
		t.Errorf("bölünmüş UTF-8 çözülemedi: %v", got)
	}
}

// TestModifiedArrows — Shift/Ctrl+ok dizileri.
func TestModifiedArrows(t *testing.T) {
	cases := map[string]string{
		"\x1b[1;2A": "shift+up",
		"\x1b[1;5B": "ctrl+down",
		"\x1b[1;3C": "alt+right",
		"\x1b[1;6D": "ctrl+shift+left",
	}
	for in, want := range cases {
		var d Decoder
		got := names(&d, in)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q -> %q, beklenen [%q]", in, got, want)
		}
	}
}

// TestMixedStream — gerçekçi bir akış: yazı, ok, yazı.
func TestMixedStream(t *testing.T) {
	var d Decoder
	got := names(&d, "ağ\x1b[Bşifre\r")
	want := []string{"a", "ğ", "down", "ş", "i", "f", "r", "e", "enter"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("karışık akış:\n aldı:  %q\n bekle: %q", got, want)
	}
}

// TestUnknownSequenceIsDropped — tanınmayan dizi çöp karakter üretmemeli.
//
// Aksi halde fare hareketi veya terminal sorgusu ekrana rastgele harf yazardı.
func TestUnknownSequenceIsDropped(t *testing.T) {
	var d Decoder
	got := names(&d, "\x1b[>0;276;0c"+"x")
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("tanınmayan dizi çöp üretti: %q", got)
	}
}

// TestIsRune — ayrım doğru olmalı.
func TestIsRune(t *testing.T) {
	if !(Key{Rune: 'a'}).IsRune() {
		t.Error("'a' yazdırılabilir sayılmadı")
	}
	if (Key{Name: "up"}).IsRune() {
		t.Error("up yazdırılabilir sayıldı")
	}
}
