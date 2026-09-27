// Package mcver parses and compares Minecraft version numbers.
//
// ── Neden ayrı bir paket ────────────────────────────────────────────────────
// Mojang 1.21.11'den sonra takvim sürümlemesine geçti: 26.1, 26.1.1, 26.1.2,
// 26.2, 26.3 (ön sürümler "26.3-rc-3", "26.3-pre-3", "26.3-snapshot-10").
// Kod tabanı ise her yerde "1.x" varsayıyordu ve her paket kendi sürüm
// karşılaştırmasını yazmıştı:
//
//   - internal/files: jar adından sürüm okuyan düzenli ifade yalnızca
//     "1\.\d+" arıyordu; paper-26.3-49.jar içeren USB klasöründe sürüm
//     bulunamıyor, aktarma "Minecraft sürümü anlaşılamadı" diye düşüyordu.
//   - providers/forge.go: NeoForge sürüm öneki "1." ile başlamayan her
//     şeyi reddediyordu (26.3 -> "unsupported MC version") ve derlemeleri
//     METİN olarak sıralıyordu: 1.21.1 için 21.1.252 yerine 21.1.99,
//     1.21.11 için kararlı 21.11.45 yerine 21.11.9-beta seçiliyordu
//     (maven.neoforged.net listesiyle 2026-09-27'de ölçüldü).
//   - providers/paper.go: sayısal karşılaştırma vardı ama "26.3-rc-3"ü
//     "26.3"ten YENİ sayıyordu ("3-rc-3" > "3" metin olarak).
//
// Tek doğru karşılaştırma burada; hepsi bunu kullanır.
package mcver

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Tur, bir sürümün yayın türüdür. Aynı sayılar için sıra:
// snapshot < pre < rc < release ("26.3-snapshot-10" < "26.3-pre-1" <
// "26.3-rc-1" < "26.3"), Mojang'ın yayın sırasıyla aynı.
type Tur int

const (
	TurSnapshot Tur = iota // "26.3-snapshot-10"
	TurPre                 // "26.3-pre-3", "1.21.11-pre5", "1.14 Pre-Release 1"
	TurRC                  // "26.3-rc-3", "1.21.11-rc1"
	TurRelease             // "26.3", "26.1.2", "1.21.11"
)

// Surum is a parsed Minecraft version.
type Surum struct {
	Parca []int // sayısal parçalar: [26 1 2], [1 21 11]
	Tur   Tur
	No    int // ön sürüm numarası ("rc-3" -> 3); release için 0
}

// Release returns the numeric part only ("26.3-rc-3" -> "26.3").
func (s Surum) Release() string {
	p := make([]string, len(s.Parca))
	for i, n := range s.Parca {
		p[i] = strconv.Itoa(n)
	}
	return strings.Join(p, ".")
}

// Parse reads "26.3", "26.1.2", "1.21.11", "26.3-rc-3", "1.21.11-pre5",
// "26.4-snapshot-1" and "1.14 Pre-Release 1".
//
// Haftalık anlık görüntüler ("25w46a", "26w14a") sürüm SAYISI taşımaz;
// onları bir sürüm çizgisine yerleştirmek tahmin olurdu, ok=false döner.
func Parse(v string) (Surum, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	i := 0
	var s Surum
	for {
		j := i
		for j < len(v) && v[j] >= '0' && v[j] <= '9' {
			j++
		}
		if j == i || j-i > 6 {
			return Surum{}, false
		}
		n, _ := strconv.Atoi(v[i:j])
		s.Parca = append(s.Parca, n)
		i = j
		if i < len(v) && v[i] == '.' {
			i++
			continue
		}
		break
	}
	// En az "ana.alt": tek sayı ("26") bir Minecraft sürümü değil.
	if len(s.Parca) < 2 {
		return Surum{}, false
	}
	rest := v[i:]
	if rest == "" {
		s.Tur = TurRelease
		return s, true
	}
	if rest[0] != '-' && rest[0] != ' ' {
		return Surum{}, false
	}
	rest = strings.TrimLeft(rest, "- ")
	var tag string
	for _, t := range []struct {
		ad  string
		tur Tur
	}{
		// Uzun olan önce: "pre-release" "pre" ile de başlar.
		{"pre-release", TurPre}, {"pre release", TurPre}, {"pre", TurPre},
		{"rc", TurRC}, {"snapshot", TurSnapshot},
	} {
		if strings.HasPrefix(rest, t.ad) {
			tag, s.Tur = t.ad, t.tur
			break
		}
	}
	if tag == "" {
		return Surum{}, false
	}
	num := strings.TrimLeft(rest[len(tag):], "- ")
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		return Surum{}, false
	}
	s.No = n
	return s, true
}

// Compare returns -1, 0 or +1 as a is older than, equal to or newer than b.
//
// Parçalar SAYI olarak karşılaştırılır ("1.21.11" > "1.21.8", "26.1" >
// "1.21.11"); eksik parça 0 sayılır ("1.21" == "1.21.0"). Ayrıştırılamayan
// bir değer ayrıştırılabilen her sürümden ESKİ sayılır ve iki ayrıştırılamayan
// metin olarak karşılaştırılır: liste yine deterministik sıralanır.
func Compare(a, b string) int {
	sa, oka := Parse(a)
	sb, okb := Parse(b)
	switch {
	case !oka && !okb:
		return strings.Compare(a, b)
	case !oka:
		return -1
	case !okb:
		return 1
	}
	if c := compareParts(sa.Parca, sb.Parca); c != 0 {
		return c
	}
	if sa.Tur != sb.Tur {
		if sa.Tur < sb.Tur {
			return -1
		}
		return 1
	}
	switch {
	case sa.No < sb.No:
		return -1
	case sa.No > sb.No:
		return 1
	}
	return 0
}

// compareParts compares numeric parts, treating missing ones as zero.
func compareParts(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// CompareNumeric compares dot-separated numeric strings such as loader or
// build versions ("21.1.252" vs "21.1.99"). Anything after the first '-' or
// '+' is ignored; the caller ranks such suffixes itself.
//
// NEDEN: NeoForge derlemeleri metin olarak sıralanıyordu ve "21.1.99",
// "21.1.252"den büyük çıkıyordu (bkz. paket açıklaması).
func CompareNumeric(a, b string) int {
	return compareParts(numericParts(a), numericParts(b))
}

func numericParts(v string) []int {
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// Newer reports whether a is a newer version than b.
func Newer(a, b string) bool { return Compare(a, b) > 0 }

// SortNewestFirst sorts versions in place, newest first.
//
// Kararlı sıralama: eşit sayılan iki değer ("1.21" ve "1.21.0") geldiği
// sırada kalır.
func SortNewestFirst(vs []string) {
	sort.SliceStable(vs, func(i, j int) bool { return Compare(vs[i], vs[j]) > 0 })
}

// IsRelease reports whether v is a full release ("26.3"), not a
// pre-release/snapshot ("26.3-rc-3") or something unparseable.
func IsRelease(v string) bool {
	s, ok := Parse(v)
	return ok && s.Tur == TurRelease
}

// Line returns the release line: "26.1" for "26.1.2", "1.21" for "1.21.11",
// "26.4" for "26.4-snapshot-1". Unparseable input is returned unchanged.
//
// Çizgi ilk iki parçadır: 26.1, 26.1.1 ve 26.1.2 aynı içerik güncellemesinin
// düzeltmeleridir (1.21, 1.21.1 ... için olduğu gibi). Eski kod noktadan
// bölüyordu ve "26.4-snapshot-1"in çizgisi "26.4-snapshot-1" çıkıyordu.
func Line(v string) string {
	s, ok := Parse(v)
	if !ok {
		return v
	}
	return strconv.Itoa(s.Parca[0]) + "." + strconv.Itoa(s.Parca[1])
}

// reFind finds a RELEASE version inside free text (jar names, logs).
//
// Ana sayı ya 1 (eski düzen) ya da 26–39 (takvim yılı, 2026–2039). Neden
// "her iki basamaklı sayı" değil: NeoForge'un kendi sürümü 1.x için "21.1.77"
// gibi başlar, Forge derleme numaraları 40–66 arası ("47.2.0", "66.0.4");
// bunlar Minecraft sürümü sanılırdı. Önündeki karakter rakam ya da nokta
// olamaz ("0.19.5" içindeki "19.5" eşleşmesin) — tek istisna harf+nokta:
// "fabric-server-mc.26.3-loader…" ve "minecraft_server.1.21.1.jar". Arkası
// da rakam ya da sayı sürdüren bir nokta olamaz; ".jar" gibi nokta+harf
// olabilir. Eski ifade bu iki adı da kaçırıyordu.
var reFind = regexp.MustCompile(
	`(?:^|[^0-9.]|[a-z_]\.)((?:1|2[6-9]|3[0-9])\.\d{1,2}(?:\.\d{1,2})?)(?:[^0-9.]|\.[a-z]|$)`)

// Find returns the first release version found in text ("" if none).
func Find(text string) string {
	if m := reFind.FindStringSubmatch(strings.ToLower(text)); m != nil {
		return m[1]
	}
	return ""
}

// ── NeoForge sürüm eşlemesi ─────────────────────────────────────────────────
//
// NeoForge kendi sürümünü Minecraft sürümünden türetir ve iki düzen vardır
// (maven.neoforged.net sürüm listesinden 2026-09-27'de okundu):
//
//	1.20.4  -> 20.4.N        1.21 -> 21.0.N        1.21.11 -> 21.11.N
//	26.1    -> 26.1.0.N      26.1.2 -> 26.1.2.N    26.3 -> 26.3.0.N(-beta)
//
// Yani 1.x'te baştaki "1." atılır; 26.x'te Minecraft sürümü üç parçaya
// tamamlanır ve derleme numarası DÖRDÜNCÜ parça olur.

// NeoForgePrefix returns the NeoForge version prefix (without the trailing
// dot) for a Minecraft release, e.g. "21.1" for 1.21.1, "26.3.0" for 26.3.
func NeoForgePrefix(mc string) (string, bool) {
	s, ok := Parse(mc)
	if !ok || s.Tur != TurRelease || len(s.Parca) > 3 {
		return "", false
	}
	p := append(append([]int(nil), s.Parca...), 0, 0)[:3]
	switch {
	case p[0] == 1 && p[1] >= 20:
		return strconv.Itoa(p[1]) + "." + strconv.Itoa(p[2]), true
	case p[0] >= 26:
		return strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1]) + "." + strconv.Itoa(p[2]), true
	}
	return "", false
}

// FromNeoForge maps a NeoForge version back to its Minecraft release:
// "21.1.77" -> "1.21.1", "21.0.167" -> "1.21", "26.3.0.23-beta" -> "26.3",
// "26.1.2.111" -> "26.1.2". "" if it does not look like NeoForge.
func FromNeoForge(neo string) string {
	p := numericParts(neo)
	switch {
	case len(p) == 3 && p[0] >= 20 && p[0] < 26:
		if p[1] == 0 {
			return "1." + strconv.Itoa(p[0])
		}
		return "1." + strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1])
	case len(p) == 4 && p[0] >= 26:
		if p[2] == 0 {
			return strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1])
		}
		return strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1]) + "." + strconv.Itoa(p[2])
	}
	return ""
}
