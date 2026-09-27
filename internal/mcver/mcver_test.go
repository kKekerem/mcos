package mcver

import (
	"reflect"
	"testing"
)

// Mojang manifestinin 2026-09-27'deki GERÇEK sırası (en yeni önce), ön
// sürümler dahil. SortNewestFirst karışık girdiden aynı sırayı üretmeli.
var mojangSirasi = []string{
	"26.4-snapshot-1", "26.3", "26.3-rc-3", "26.3-rc-1", "26.3-pre-3", "26.3-pre-1",
	"26.3-snapshot-10", "26.3-snapshot-9", "26.3-snapshot-1", "26.2", "26.2-rc-2",
	"26.1.2", "26.1.2-rc-1", "26.1.1", "26.1", "26.1-rc-3", "26.1-snapshot-11",
	"1.21.11", "1.21.11-rc3", "1.21.11-pre5", "1.21.10", "1.21.9", "1.21.8",
	"1.21.1", "1.21", "1.21-rc1", "1.21-pre1", "1.20.6", "1.14.4", "1.14.4 Pre-Release 3",
}

func TestMojangSirasiYenidenUretilir(t *testing.T) {
	karisik := []string{}
	// Belirlenimci karıştırma: tersine çevir, sonra çiftleri yer değiştir.
	for i := len(mojangSirasi) - 1; i >= 0; i-- {
		karisik = append(karisik, mojangSirasi[i])
	}
	for i := 0; i+1 < len(karisik); i += 2 {
		karisik[i], karisik[i+1] = karisik[i+1], karisik[i]
	}
	SortNewestFirst(karisik)
	if !reflect.DeepEqual(karisik, mojangSirasi) {
		t.Fatalf("sıra yanlış:\n aldım:   %v\n istedim: %v", karisik, mojangSirasi)
	}
}

func TestKarsilastirma(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"26.1", "1.21.11", 1},   // takvim sürümü 1.x'ten yeni
		{"1.21.11", "1.21.8", 1}, // sayı, metin değil
		{"26.1.2", "26.1", 1},    // düzeltme sürümü
		{"26.3", "26.3-rc-3", 1}, // eski surumDaha tersini söylüyordu
		{"26.3-rc-1", "26.3-pre-3", 1},
		{"26.3-pre-1", "26.3-snapshot-10", 1},
		{"26.3-snapshot-10", "26.3-snapshot-9", 1},
		{"1.21.11-rc1", "1.21.10", 1}, // bir sonraki sürümün ön sürümü öncekinden yeni
		{"1.21", "1.21.0", 0},
		{"26.2", "26.2", 0},
		{"25w46a", "1.8.8", -1}, // ayrıştırılamayan her zaman eski
		{"26w14a", "25w46a", 1}, // ikisi de ayrıştırılamaz: metin
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, istenen %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, istenen %d (simetri)", c.b, c.a, got, -c.want)
		}
	}
}

func TestAyristirma(t *testing.T) {
	for _, c := range []struct {
		in   string
		ok   bool
		tur  Tur
		no   int
		base string
	}{
		{"26.3", true, TurRelease, 0, "26.3"},
		{"26.1.2", true, TurRelease, 0, "26.1.2"},
		{"26.3-rc-3", true, TurRC, 3, "26.3"},
		{"1.21.11-pre5", true, TurPre, 5, "1.21.11"},
		{"26.4-snapshot-1", true, TurSnapshot, 1, "26.4"},
		{"1.14 Pre-Release 1", true, TurPre, 1, "1.14"},
		{" 1.20.4 ", true, TurRelease, 0, "1.20.4"},
		{"26w14a", false, 0, 0, ""},
		{"26", false, 0, 0, ""},
		{"1.21.11-foo", false, 0, 0, ""},
		{"", false, 0, 0, ""},
		{"26.", false, 0, 0, ""},
	} {
		s, ok := Parse(c.in)
		if ok != c.ok {
			t.Errorf("Parse(%q) ok=%v, istenen %v", c.in, ok, c.ok)
			continue
		}
		if ok && (s.Tur != c.tur || s.No != c.no || s.Release() != c.base) {
			t.Errorf("Parse(%q) = %+v (%s), istenen tür %d no %d taban %s",
				c.in, s, s.Release(), c.tur, c.no, c.base)
		}
	}
	if IsRelease("26.3-rc-3") || !IsRelease("26.3") || IsRelease("25w46a") {
		t.Fatal("IsRelease ön sürüm/tam sürüm ayrımı yanlış")
	}
}

// "26.1" ve "26.1.2" AYNI çizgide; 26.2 değil; ön sürüm çizgisini taşır.
func TestCizgi(t *testing.T) {
	for in, want := range map[string]string{
		"26.1": "26.1", "26.1.2": "26.1", "26.2": "26.2", "1.21.11": "1.21",
		"26.4-snapshot-1": "26.4", "1.21-pre1": "1.21", "25w46a": "25w46a",
	} {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, istenen %q", in, got, want)
		}
	}
}

// Jar adları GERÇEK kaynaklardan: fill.papermc.io, api.purpurmc.org
// (content-disposition), meta.fabricmc.net (content-disposition), Forge
// kütüphane klasörü ve eski Mojang adlandırması.
func TestMetindenSurumBulma(t *testing.T) {
	for in, want := range map[string]string{
		"paper-26.3-49.jar":           "26.3",
		"paper-26.1.2-74.jar":         "26.1.2",
		"purpur-26.3-2641.jar":        "26.3",
		"paper-1.21.11-132.jar":       "1.21.11",
		"folia-26.2-7.jar":            "26.2",
		"26.3-66.0.4":                 "26.3", // Forge kütüphane klasörü
		"1.20.1-47.2.0":               "1.20.1",
		"minecraft_server.1.21.1.jar": "1.21.1",
		"fabric-server-mc.26.3-loader.0.19.5-launcher.1.1.2.jar":    "26.3",
		"fabric-server-mc.1.21.11-loader.0.19.5-launcher.1.1.2.jar": "1.21.11",
		"26.3-49-0fdc088 (MC: 26.3)":                                "26.3",
		// Minecraft sürümü OLMAYANLAR:
		"neoforge-21.1.77-installer.jar": "", // NeoForge'un kendi sürümü
		"forge-47.2.0.jar":               "",
		"fabric-server-launch.jar":       "",
		"sodium-0.19.5.jar":              "",
		"server.jar":                     "",
	} {
		if got := Find(in); got != want {
			t.Errorf("Find(%q) = %q, istenen %q", in, got, want)
		}
	}
}

// NeoForge eşlemesi maven.neoforged.net'teki GERÇEK sürüm adlarıyla.
func TestNeoForgeEslemesi(t *testing.T) {
	for mc, want := range map[string]string{
		"1.21.1": "21.1", "1.21": "21.0", "1.21.11": "21.11", "1.20.4": "20.4",
		"26.3": "26.3.0", "26.1": "26.1.0", "26.1.2": "26.1.2", "26.2": "26.2.0",
	} {
		if got, ok := NeoForgePrefix(mc); !ok || got != want {
			t.Errorf("NeoForgePrefix(%q) = %q,%v; istenen %q", mc, got, ok, want)
		}
	}
	for _, mc := range []string{"1.19.2", "26.3-rc-3", "25w46a", ""} {
		if got, ok := NeoForgePrefix(mc); ok {
			t.Errorf("NeoForgePrefix(%q) = %q; NeoForge'da karşılığı yok, reddedilmeliydi", mc, got)
		}
	}
	for neo, want := range map[string]string{
		"21.1.77": "1.21.1", "21.0.167": "1.21", "21.11.45": "1.21.11", "20.4.237": "1.20.4",
		"26.3.0.23-beta": "26.3", "26.1.2.111": "26.1.2", "26.1.0.19-beta": "26.1",
		"47.2.0": "", "forge": "",
	} {
		if got := FromNeoForge(neo); got != want {
			t.Errorf("FromNeoForge(%q) = %q, istenen %q", neo, got, want)
		}
	}
}

func TestSayisalKarsilastirma(t *testing.T) {
	if CompareNumeric("21.1.252", "21.1.99") != 1 || CompareNumeric("26.3.0.9-beta", "26.3.0.23-beta") != -1 ||
		CompareNumeric("21.11.45", "21.11.45") != 0 {
		t.Fatal("derleme numaraları sayı olarak karşılaştırılmıyor")
	}
}
