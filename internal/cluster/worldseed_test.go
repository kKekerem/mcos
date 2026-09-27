package cluster

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// nbtBuilder writes just enough NBT to fake a level.dat.
type nbtBuilder struct{ bytes.Buffer }

func (b *nbtBuilder) name(s string) {
	_ = binary.Write(&b.Buffer, binary.BigEndian, uint16(len(s)))
	b.WriteString(s)
}
func (b *nbtBuilder) open(n string) { b.WriteByte(tagCompound); b.name(n) }
func (b *nbtBuilder) end()          { b.WriteByte(tagEnd) }
func (b *nbtBuilder) long(n string, v int64) {
	b.WriteByte(tagLong)
	b.name(n)
	_ = binary.Write(&b.Buffer, binary.BigEndian, v)
}
func (b *nbtBuilder) str(n, v string) { b.WriteByte(tagString); b.name(n); b.name(v) }

func writeLevel(t *testing.T, dir string, raw []byte) {
	t.Helper()
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(raw)
	w.Close()
	if err := os.MkdirAll(filepath.Join(dir, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "world", "level.dat"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// modernLevel: Data.WorldGenSettings.seed (1.16+). Yanıltıcı bir "seed"
// başka bir bileşikte de var — okuyucu YOLA bakmalı, ada değil.
func modernLevel(seed int64) []byte {
	var b nbtBuilder
	b.open("")
	b.open("Data")
	b.str("LevelName", "world")
	// Liste içinde bileşik: okuyucu listeyi doğru atlamalı.
	b.WriteByte(tagList)
	b.name("ServerBrands")
	b.WriteByte(tagString)
	_ = binary.Write(&b.Buffer, binary.BigEndian, int32(2))
	b.name("vanilla")
	b.name("paper")
	b.open("WorldGenSettings")
	b.long("seed", seed)
	b.end()
	// Tuzak gerçek alandan SONRA: okuyucu yalnızca ada baksaydı son gördüğü
	// "seed" (999) kazanırdı.
	b.open("DragonFight")
	b.long("seed", 999)
	b.end()
	b.end()
	b.end()
	return b.Bytes()
}

func TestWorldSeedModern(t *testing.T) {
	dir := t.TempDir()
	writeLevel(t, dir, modernLevel(-4172144997902289642))
	got, err := WorldSeed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "-4172144997902289642" {
		t.Fatalf("tohum = %s", got)
	}
}

func TestWorldSeedLegacy(t *testing.T) {
	var b nbtBuilder
	b.open("")
	b.open("Data")
	b.long("RandomSeed", 424242)
	b.end()
	b.end()
	dir := t.TempDir()
	writeLevel(t, dir, b.Bytes())
	if got, err := WorldSeed(dir); err != nil || got != "424242" {
		t.Fatalf("tohum = %q, %v", got, err)
	}
}

func TestWorldSeedNoWorld(t *testing.T) {
	if _, err := WorldSeed(t.TempDir()); !errors.Is(err, ErrNoWorld) {
		t.Fatalf("dünya yokken hata = %v, ErrNoWorld bekleniyordu", err)
	}
}

// level-name değiştirilmişse o klasöre bakılmalı.
func TestWorldSeedHonoursLevelName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("motd=x\nlevel-name=benimdunyam\n"), 0o644)
	writeLevel(t, dir, modernLevel(7))
	// "world" klasörüne yazıldı; "benimdunyam" yok → dünya yok sayılmalı.
	if _, err := WorldSeed(dir); !errors.Is(err, ErrNoWorld) {
		t.Fatalf("level-name yok sayıldı: %v", err)
	}
	os.Rename(filepath.Join(dir, "world"), filepath.Join(dir, "benimdunyam"))
	if got, err := WorldSeed(dir); err != nil || got != "7" {
		t.Fatalf("tohum = %q, %v", got, err)
	}
}

func TestSameSeedJavaHash(t *testing.T) {
	// Değerler jshell ile ÖLÇÜLDÜ (JDK 21): "elma".hashCode() == 3116155,
	// "çğ😀".hashCode() == 8930427 (vekil çift içeren UTF-16 yolu).
	if !SameSeed("elma", "3116155") {
		t.Error("metin tohum Java hashCode'u ile eşlenmedi")
	}
	if !SameSeed("çğ😀", "8930427") {
		t.Error("BMP dışı karakterli tohum Java hashCode'u ile eşlenmedi")
	}
	if SameSeed("424242", "424243") {
		t.Error("farklı tohumlar aynı sayıldı")
	}
	if !SameSeed(" 42 ", "42") {
		t.Error("boşluk farkı ayrı tohum sayıldı")
	}
}

func TestRetireMismatchedWorld(t *testing.T) {
	dir := t.TempDir()
	writeLevel(t, dir, modernLevel(1))
	os.MkdirAll(filepath.Join(dir, "world_nether"), 0o755)

	// Aynı tohum: dokunulmamalı.
	if moved, err := RetireMismatchedWorld(dir, "1"); err != nil || len(moved) != 0 {
		t.Fatalf("aynı tohumda taşındı: %v %v", moved, err)
	}
	// Farklı tohum: iki klasör de kenara alınmalı, SİLİNMEMELİ.
	moved, err := RetireMismatchedWorld(dir, "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 2 {
		t.Fatalf("taşınan = %v, world ve world_nether bekleniyordu", moved)
	}
	if _, err := os.Stat(filepath.Join(dir, "world")); !os.IsNotExist(err) {
		t.Error("eski dünya yerinde kaldı")
	}
	if _, err := os.Stat(filepath.Join(dir, moved[0], "level.dat")); err != nil {
		t.Errorf("kenara alınan dünya kayıp: %v", err)
	}
}

// Gerçek bir level.dat ile (elle): MCOS_TEST_LEVEL_DIR=<sunucu veri klasörü>.
func TestWorldSeedRealFile(t *testing.T) {
	dir := os.Getenv("MCOS_TEST_LEVEL_DIR")
	if dir == "" {
		t.Skip("MCOS_TEST_LEVEL_DIR verilmedi")
	}
	seed, err := WorldSeed(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: tohum %s", dir, seed)
	if want := os.Getenv("MCOS_TEST_LEVEL_SEED"); want != "" && !SameSeed(seed, want) {
		t.Fatalf("tohum %s, beklenen %s", seed, want)
	}
}
