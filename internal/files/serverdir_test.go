package files

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func yaz(t *testing.T, p, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

// levelDat builds a minimal real level.dat: {"" : {Data: {Version: {Name: v}, LevelName: "x"}}}.
func levelDat(t *testing.T, p, v string) {
	t.Helper()
	var b bytes.Buffer
	str := func(s string) { binary.Write(&b, binary.BigEndian, uint16(len(s))); b.WriteString(s) }
	b.WriteByte(10)
	str("")
	b.WriteByte(10)
	str("Data")
	b.WriteByte(8) // önce ilgisiz bir metin: okuyucu doğru yolu seçmeli
	str("LevelName")
	str("1.99.9 değil")
	b.WriteByte(10)
	str("Version")
	b.WriteByte(3)
	str("Id")
	binary.Write(&b, binary.BigEndian, int32(3955))
	b.WriteByte(8)
	str("Name")
	str(v)
	b.WriteByte(0) // Version sonu
	b.WriteByte(0) // Data sonu
	b.WriteByte(0) // kök sonu
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(b.Bytes())
	w.Close()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPaperSunucuKlasoruTaninir(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "paper-1.21.1-132.jar"), "jar")
	yaz(t, filepath.Join(d, "server.properties"), "server-port=25570\nmax-players=40\ndifficulty=hard\nlevel-name=dunyam\n")
	levelDat(t, filepath.Join(d, "dunyam", "level.dat"), "1.21.1")
	yaz(t, filepath.Join(d, "plugins", "EssentialsX.jar"), "jar")
	i := DetectServerDir(d)
	if i.Software != "paper" || i.MCVersion != "1.21.1" || !i.HasWorld || i.World != "dunyam" || i.Plugins != 1 {
		t.Fatalf("Paper klasörü yanlış tanındı: %+v", i)
	}
	if i.PropInt("server-port") != 25570 || i.PropInt("max-players") != 40 || i.Props["difficulty"] != "hard" {
		t.Fatalf("ayarlar okunmadı: %+v", i.Props)
	}
}

// Jar yok, yalnızca dünya + modlar (kullanıcı sunucu jar'ını kopyalamamış):
// sürüm level.dat'tan okunmalı; dünyayı daha eski sürümle açmak onu bozar.
func TestJarsizKlasordeSurumDunyadanOkunur(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.properties"), "motd=Selam\n")
	levelDat(t, filepath.Join(d, "world", "level.dat"), "1.20.4")
	yaz(t, filepath.Join(d, "mods", "sodium.jar"), "jar")
	i := DetectServerDir(d)
	if i.MCVersion != "1.20.4" {
		t.Fatalf("sürüm dünyadan okunmadı: %q", i.MCVersion)
	}
	if i.Software != "fabric" || i.Mods != 1 {
		t.Fatalf("modlu klasör: %+v", i)
	}
}

func TestForgeKlasoruTaninir(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.properties"), "")
	yaz(t, filepath.Join(d, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.2.0", "unix_args.txt"), "")
	i := DetectServerDir(d)
	if i.Software != "forge" || i.MCVersion != "1.20.1" {
		t.Fatalf("Forge klasörü: %+v", i)
	}
}

func TestSunucuOlmayanKlasorTaninmaz(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "tatil.jpg"), "x")
	yaz(t, filepath.Join(d, "belgeler", "not.txt"), "x")
	if i := DetectServerDir(d); i.Score >= 3 || i.Software != "" {
		t.Fatalf("sıradan klasör sunucu sanıldı: %+v", i)
	}
}

// ── Minecraft 26.x (takvim sürümleri) ───────────────────────────────────────
//
// Jar adları GERÇEK kaynaklardan: fill.papermc.io v3 derleme listesi
// ("paper-26.3-49.jar"), api.purpurmc.org content-disposition
// ("purpur-26.3-2641.jar"), meta.fabricmc.net content-disposition
// ("fabric-server-mc.26.3-loader.0.19.5-launcher.1.1.2.jar"). Eski ifade
// yalnızca "1\.\d+" aradığı için hepsinde sürüm BOŞ çıkıyordu.
func TestSurum26JarAdlarindanOkunur(t *testing.T) {
	for _, c := range []struct{ jar, sw, ver string }{
		{"paper-26.3-49.jar", "paper", "26.3"},
		{"paper-26.1.2-74.jar", "paper", "26.1.2"},
		{"purpur-26.3-2641.jar", "purpur", "26.3"},
		{"fabric-server-mc.26.3-loader.0.19.5-launcher.1.1.2.jar", "fabric", "26.3"},
		// Aynı Fabric adlandırması 1.x'te de sürümsüz kalıyordu ("mc." öneki).
		{"fabric-server-mc.1.21.11-loader.0.19.5-launcher.1.1.2.jar", "fabric", "1.21.11"},
		{"paper-1.21.11-132.jar", "paper", "1.21.11"},
	} {
		d := t.TempDir()
		yaz(t, filepath.Join(d, c.jar), "jar")
		yaz(t, filepath.Join(d, "server.properties"), "motd=x\n")
		i := DetectServerDir(d)
		if i.Software != c.sw || i.MCVersion != c.ver {
			t.Errorf("%s: yazılım %q sürüm %q; istenen %q %q", c.jar, i.Software, i.MCVersion, c.sw, c.ver)
		}
	}
}

// Jar "server.jar" adıyla (MCOS'un kendisi de öyle adlandırıyor), dünya yok:
// sürüm yalnızca Paper'ın version_history.json'undan okunabilir. İçerik
// gerçek bir Paper 26.3 sunucusunun ilk açılışta yazdığı dosyanın biçimi.
func TestSurum26SurumGecmisindenOkunur(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.jar"), "jar")
	yaz(t, filepath.Join(d, "server.properties"), "motd=x\n")
	yaz(t, filepath.Join(d, "version_history.json"), `{"currentVersion":"26.3-49-0fdc088 (MC: 26.3)"}`)
	if i := DetectServerDir(d); i.MCVersion != "26.3" {
		t.Fatalf("sürüm geçmişinden 26.3 okunmadı: %+v", i)
	}
	// Karşı durum: 1.x biçimi de okunmaya devam etmeli.
	yaz(t, filepath.Join(d, "version_history.json"), `{"currentVersion":"git-Paper-196 (MC: 1.20.1)"}`)
	if i := DetectServerDir(d); i.MCVersion != "1.20.1" {
		t.Fatalf("1.x sürüm geçmişi okunmadı: %+v", i)
	}
}

// Yalnızca dünya: 26.x level.dat'ı da aynı Data.Version.Name alanını taşır
// (gerçek bir 26.3 sunucusunun ürettiği level.dat'ta doğrulandı).
func TestSurum26DunyadanOkunur(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.properties"), "level-name=world\n")
	levelDat(t, filepath.Join(d, "world", "level.dat"), "26.3")
	if i := DetectServerDir(d); i.MCVersion != "26.3" {
		t.Fatalf("26.3 dünyası: %+v", i)
	}
}

// Forge 26.x klasör adı ("26.3-66.0.4", files.minecraftforge.net
// promotions_slim.json'daki 26.3-latest) ve birden çok kurulum kalmış bir
// klasörde EN YENİ sürüm. Eski kod ReadDir'in metin sırasındaki son
// klasörü alıyordu: "1.21.9-…" metin olarak "1.21.11-…"den sonra gelir.
func TestForge26VeEnYeniKurulum(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.properties"), "")
	yaz(t, filepath.Join(d, "libraries", "net", "minecraftforge", "forge", "26.3-66.0.4", "unix_args.txt"), "")
	if i := DetectServerDir(d); i.Software != "forge" || i.MCVersion != "26.3" {
		t.Fatalf("Forge 26.3: %+v", i)
	}

	d = t.TempDir()
	yaz(t, filepath.Join(d, "server.properties"), "")
	for _, v := range []string{"1.21.11-61.2.0", "1.21.9-58.0.0"} {
		yaz(t, filepath.Join(d, "libraries", "net", "minecraftforge", "forge", v, "unix_args.txt"), "")
	}
	if i := DetectServerDir(d); i.MCVersion != "1.21.11" {
		t.Fatalf("iki Forge kurulumundan en yenisi seçilmedi: %+v", i)
	}
}

// NeoForge kendi numarasını taşır (maven.neoforged.net'teki gerçek adlar):
// 26.x'te "26.3.0.23-beta" -> 26.3, 1.x'te "21.1.77" -> 1.21.1. Eskiden
// NeoForge klasöründen sürüm hiç okunmuyordu.
func TestNeoForgeSurumuKlasordenOkunur(t *testing.T) {
	for dir, want := range map[string]string{
		"26.3.0.23-beta": "26.3",
		"26.1.2.111":     "26.1.2",
		"21.1.77":        "1.21.1",
	} {
		d := t.TempDir()
		yaz(t, filepath.Join(d, "server.properties"), "")
		yaz(t, filepath.Join(d, "libraries", "net", "neoforged", "neoforge", dir, "unix_args.txt"), "")
		if i := DetectServerDir(d); i.Software != "neoforge" || i.MCVersion != want {
			t.Errorf("NeoForge %s: %+v; istenen %s", dir, i, want)
		}
	}
}

// ── Gerçek MCOS klasörleri (jar adı "server.jar") ───────────────────────────
//
// Düzenler, MCOS'un server.Manager.Install ile kurup Java 25'le GERÇEKTEN
// açtığı 26.3 sunucularından kopyalandı. Eski kod üçünü de "vanilla" sanıyordu.
func TestServerJarliKlasorYazilimiIzlerindenTaninir(t *testing.T) {
	for _, c := range []struct {
		sw    string
		files []string
	}{
		{"paper", []string{".paper/version_history.json", "bukkit.yml", "spigot.yml", "versions/26.3/paper-26.3.jar"}},
		{"purpur", []string{".paper/version_history.json", "bukkit.yml", "spigot.yml", "purpur.yml", "versions/26.3/purpur-26.3.jar"}},
		{"fabric", []string{".fabric/server/26.3-server.jar", ".fabric/server/fabric-loader-server-0.19.5-minecraft-26.3.jar", "versions/26.3/server-26.3.jar"}},
		{"vanilla", []string{"versions/26.3/server-26.3.jar"}},
	} {
		d := t.TempDir()
		yaz(t, filepath.Join(d, "server.jar"), "jar")
		yaz(t, filepath.Join(d, "server.properties"), "server-port=25565\n")
		yaz(t, filepath.Join(d, "eula.txt"), "eula=true\n")
		for _, f := range c.files {
			icerik := "x"
			if filepath.Base(f) == "version_history.json" {
				icerik = `{"currentVersion":"26.3-49-0fdc088 (MC: 26.3)"}`
			}
			yaz(t, filepath.Join(d, filepath.FromSlash(f)), icerik)
		}
		i := DetectServerDir(d)
		if i.Software != c.sw || i.MCVersion != "26.3" {
			t.Errorf("%s klasörü: yazılım %q sürüm %q; istenen %q 26.3", c.sw, i.Software, i.MCVersion, c.sw)
		}
	}
}

// Paper/Purpur 26.x sürüm geçmişini .paper/ ALTINA yazıyor (gerçek sunucuda
// görüldü); dünya ve bundler klasörü yoksa sürüm yalnızca oradan okunur.
func TestSurumGecmisiPaperKlasorundenOkunur(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.jar"), "jar")
	yaz(t, filepath.Join(d, "server.properties"), "motd=x\n")
	yaz(t, filepath.Join(d, ".paper", "version_history.json"), `{"currentVersion":"26.3-2641-6f1f2c1 (MC: 26.3)"}`)
	if i := DetectServerDir(d); i.MCVersion != "26.3" || i.Software != "paper" {
		t.Fatalf(".paper/version_history.json okunmadı: %+v", i)
	}
}

// Birden çok bundler klasörü (sunucu 1.21.11'den 26.3'e yükseltilmiş): en
// yenisi seçilir; ön sürüm klasörü ("26.4-snapshot-1") sayılmaz.
func TestSunucuDosyalarindanEnYeniSurum(t *testing.T) {
	d := t.TempDir()
	yaz(t, filepath.Join(d, "server.jar"), "jar")
	yaz(t, filepath.Join(d, "server.properties"), "motd=x\n")
	for _, v := range []string{"1.21.11", "26.3", "26.4-snapshot-1"} {
		yaz(t, filepath.Join(d, "versions", v, "server-"+v+".jar"), "jar")
	}
	if i := DetectServerDir(d); i.MCVersion != "26.3" {
		t.Fatalf("en yeni kurulu sürüm 26.3 olmalıydı: %+v", i)
	}
}

// Yükseltilmiş klasör: kullanıcı 1.21.11'den 26.3'e geçerken eski jar'ı
// silmemiş. Glob metin sırası verir ve "paper-1.21.11-132.jar" önce gelir;
// eski kod ilk eşleşeni alıp klasörü 1.21.11 diye aktarıyordu.
func TestYukseltilmisKlasordeEnYeniJarSecilir(t *testing.T) {
	for _, c := range []struct {
		jars []string
		sw   string
		ver  string
	}{
		{[]string{"paper-1.21.11-132.jar", "paper-26.3-49.jar"}, "paper", "26.3"},
		{[]string{"purpur-1.21.11-2568.jar", "purpur-26.1.2-2600.jar"}, "purpur", "26.1.2"},
		{[]string{"fabric-server-mc.1.21.11-loader.0.19.5-launcher.1.1.2.jar",
			"fabric-server-mc.26.3-loader.0.19.5-launcher.1.1.2.jar"}, "fabric", "26.3"},
		// Sürümsüz bir ad sürümlü olanı silmemeli.
		{[]string{"paper-1.21.11-132.jar", "paper.jar"}, "paper", "1.21.11"},
	} {
		d := t.TempDir()
		for _, j := range c.jars {
			yaz(t, filepath.Join(d, j), "jar")
		}
		yaz(t, filepath.Join(d, "server.properties"), "motd=x\n")
		if i := DetectServerDir(d); i.Software != c.sw || i.MCVersion != c.ver {
			t.Errorf("%v: yazılım %q sürüm %q; istenen %q %q", c.jars, i.Software, i.MCVersion, c.sw, c.ver)
		}
	}
}

// Dünya jar'dan YENİ bir tam sürümde kaydedilmişse dünyanın sürümü alınır:
// 26.3 dünyasını 1.21.11 sunucusuyla açmak onu bozar. Tersi (eski dünya,
// yeni jar) jar'ın sürümünde kalır — Minecraft eski dünyayı yükseltir. Ön
// sürüm adı taşıyan bir dünya tam sürümün önüne geçmez.
func TestDunyaEskiSurumeIndirilmez(t *testing.T) {
	for _, c := range []struct{ jar, world, want string }{
		{"paper-1.21.11-132.jar", "26.3", "26.3"},
		{"paper-26.3-49.jar", "1.21.11", "26.3"},
		{"paper-26.3-49.jar", "26.4-snapshot-1", "26.3"},
		{"paper-26.3-49.jar", "26.3", "26.3"},
	} {
		d := t.TempDir()
		yaz(t, filepath.Join(d, c.jar), "jar")
		yaz(t, filepath.Join(d, "server.properties"), "level-name=world\n")
		levelDat(t, filepath.Join(d, "world", "level.dat"), c.world)
		if i := DetectServerDir(d); i.MCVersion != c.want {
			t.Errorf("jar %s + dünya %s: sürüm %q; istenen %q", c.jar, c.world, i.MCVersion, c.want)
		}
	}
}
