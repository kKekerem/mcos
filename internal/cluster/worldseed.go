package cluster

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bu dosya bir Minecraft dünyasının GERÇEK tohumunu okur ve tohumu uymayan
// bir dünyayı kenara çeker.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Uçtan uca sınamada (iki ağ ad alanı, gerçek Paper 1.21.11) görüldü: kurucu
// MCOS'taki sunucu ortak dünya açılmadan ÖNCE kurulmuştu. "Ortak dünyayı aç"
// tohumu (424242) ve zorluğu (hard) yalnızca kayda yazdı; server.properties
// kurulumda bir kez yazıldığı için MCOS tarafında "level-seed=" boş,
// "difficulty=easy" kaldı. Düğüm ise 424242/hard ile kuruldu. Sonuç: İKİ
// AYRI DÜNYA — sınırda arazi kesik kesik, oyuncu bir uçurumun kenarına
// aktarılır.
//
// Daha sinsi yarısı: dünya zaten oluşmuşsa server.properties'teki tohum
// HİÇ OKUNMAZ; Minecraft tohumu level.dat'tan alır. Yani kurucuda doğru olan
// tohum "yazılan" değil, level.dat'ta DURANDIR. Bu yüzden ortak dünya
// açılırken dünya varsa tohum oradan okunur ve eşlere o gönderilir.

// ErrNoWorld means the level has not been generated yet.
var ErrNoWorld = errors.New("dünya henüz oluşturulmamış")

// LevelName returns the world folder name from server.properties.
//
// Varsayılan "world". Kullanıcı level-name'i değiştirmişse başka bir klasöre
// bakmak, var olan dünyayı "yok" sanmak olurdu.
func LevelName(dataDir string) string {
	f, err := os.Open(filepath.Join(dataDir, "server.properties"))
	if err != nil {
		return "world"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "level-name="); ok {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	return "world"
}

// WorldSeed reads the seed of the world under dataDir.
//
// Dönüş ErrNoWorld ise dünya yoktur: tohum henüz serbestçe seçilebilir.
func WorldSeed(dataDir string) (string, error) {
	path := filepath.Join(dataDir, LevelName(dataDir), "level.dat")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", ErrNoWorld
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("level.dat okunamadı: %w", err)
	}
	defer zr.Close()
	seed, ok, err := nbtFindSeed(bufio.NewReader(zr))
	if err != nil {
		return "", fmt.Errorf("level.dat çözülemedi: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("level.dat içinde tohum bulunamadı")
	}
	return strconv.FormatInt(seed, 10), nil
}

// SameSeed compares two seed strings the way Minecraft interprets them.
//
// Minecraft sayı OLMAYAN bir tohumu String.hashCode() ile sayıya çevirir;
// "elma" yazan kurucunun level.dat'ında 3116155 durur. Karşılaştırma bu
// dönüşümü bilmezse aynı dünyayı "farklı" sanıp düğümdeki dünyayı boşuna
// kenara çekerdik.
func SameSeed(a, b string) bool {
	return seedValue(a) == seedValue(b)
}

func seedValue(s string) int64 {
	s = strings.TrimSpace(s)
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	// Java String.hashCode: UTF-16 kod birimleri üzerinden s[0]*31^(n-1)+…
	var h int32
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			h = 31*h + int32(0xD800+(r>>10))
			h = 31*h + int32(0xDC00+(r&0x3FF))
			continue
		}
		h = 31*h + int32(r)
	}
	return int64(h)
}

// RetireMismatchedWorld moves a world aside when its seed differs from want.
//
// SİLMEZ, taşır: dünya kullanıcının verisidir. Klasör
// "<ad>-eski-20260925-185700" olur; kullanıcı isterse geri alabilir.
// Dönüş: taşınan klasörlerin adları (boş: bir şey yapılmadı).
//
// NEDEN GEREKLİ: düğüm daha önce BAŞKA bir tohumla kurulmuş aynı adlı bir
// ortak dünyayı yeniden kullanıyorsa, yeni kurulum yalnızca
// server.properties'i değiştirir; Minecraft eski level.dat'ı okur ve eski
// araziyi üretmeye devam eder.
func RetireMismatchedWorld(dataDir, want string) ([]string, error) {
	if strings.TrimSpace(want) == "" {
		return nil, nil
	}
	have, err := WorldSeed(dataDir)
	if errors.Is(err, ErrNoWorld) {
		return nil, nil
	}
	if err != nil {
		// Okunamayan bir level.dat'ı "uymuyor" saymak, sağlam bir dünyayı
		// yerinden oynatmak olabilir. Dokunmuyoruz.
		return nil, err
	}
	if SameSeed(have, want) {
		return nil, nil
	}
	level := LevelName(dataDir)
	stamp := time.Now().Format("20060102-150405")
	var moved []string
	// Paper/Spigot boyutları ayrı klasörde tutar; vanilla/Fabric içeride.
	for _, name := range []string{level, level + "_nether", level + "_the_end"} {
		src := filepath.Join(dataDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := src + "-eski-" + stamp
		if err := os.Rename(src, dst); err != nil {
			return moved, fmt.Errorf("%s kenara alınamadı: %w", name, err)
		}
		moved = append(moved, filepath.Base(dst))
	}
	return moved, nil
}

// ── Asgari NBT okuyucu ──────────────────────────────────────────────────────
//
// Yalnızca tohumu bulmaya yeter: her etiketi okur ya da ATLAR, bellekte
// ağaç kurmaz. Tam bir NBT kütüphanesi eklemek bu tek alan için gereksiz.
//
// Tohumun yeri sürüme göre değişir:
//   1.16+ : Data.WorldGenSettings.seed  (TAG_Long)
//   eski  : Data.RandomSeed             (TAG_Long)
// İkisi de aranır; yenisi önceliklidir.

const (
	tagEnd       = 0
	tagByte      = 1
	tagShort     = 2
	tagInt       = 3
	tagLong      = 4
	tagFloat     = 5
	tagDouble    = 6
	tagByteArray = 7
	tagString    = 8
	tagList      = 9
	tagCompound  = 10
	tagIntArray  = 11
	tagLongArray = 12
)

// maxNBTDepth bounds recursion on a corrupt/hostile file.
const maxNBTDepth = 64

type nbtSeedFinder struct {
	r       io.Reader
	newSeed int64
	haveNew bool
	oldSeed int64
	haveOld bool
}

func nbtFindSeed(r io.Reader) (int64, bool, error) {
	f := &nbtSeedFinder{r: r}
	var t [1]byte
	if _, err := io.ReadFull(r, t[:]); err != nil {
		return 0, false, err
	}
	if t[0] != tagCompound {
		return 0, false, fmt.Errorf("kök bileşik değil (etiket %d)", t[0])
	}
	if _, err := f.str(); err != nil {
		return 0, false, err
	}
	if err := f.compound(nil, 0); err != nil {
		return 0, false, err
	}
	switch {
	case f.haveNew:
		return f.newSeed, true, nil
	case f.haveOld:
		return f.oldSeed, true, nil
	}
	return 0, false, nil
}

func (f *nbtSeedFinder) str() (string, error) {
	var n uint16
	if err := binary.Read(f.r, binary.BigEndian, &n); err != nil {
		return "", err
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(f.r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func (f *nbtSeedFinder) skip(n int64) error {
	_, err := io.CopyN(io.Discard, f.r, n)
	return err
}

// compound reads entries until TAG_End. path is the chain of names above us.
func (f *nbtSeedFinder) compound(path []string, depth int) error {
	if depth > maxNBTDepth {
		return fmt.Errorf("NBT çok derin")
	}
	for {
		var t [1]byte
		if _, err := io.ReadFull(f.r, t[:]); err != nil {
			return err
		}
		if t[0] == tagEnd {
			return nil
		}
		name, err := f.str()
		if err != nil {
			return err
		}
		if t[0] == tagLong {
			var v int64
			if err := binary.Read(f.r, binary.BigEndian, &v); err != nil {
				return err
			}
			switch {
			case name == "seed" && len(path) == 2 && path[0] == "Data" &&
				path[1] == "WorldGenSettings":
				f.newSeed, f.haveNew = v, true
			case name == "RandomSeed" && len(path) == 1 && path[0] == "Data":
				f.oldSeed, f.haveOld = v, true
			}
			continue
		}
		if err := f.payload(t[0], append(path, name), depth+1); err != nil {
			return err
		}
	}
}

func (f *nbtSeedFinder) payload(tag byte, path []string, depth int) error {
	if depth > maxNBTDepth {
		return fmt.Errorf("NBT çok derin")
	}
	switch tag {
	case tagByte:
		return f.skip(1)
	case tagShort:
		return f.skip(2)
	case tagInt, tagFloat:
		return f.skip(4)
	case tagLong, tagDouble:
		return f.skip(8)
	case tagByteArray, tagIntArray, tagLongArray:
		var n int32
		if err := binary.Read(f.r, binary.BigEndian, &n); err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("negatif dizi boyu")
		}
		size := map[byte]int64{tagByteArray: 1, tagIntArray: 4, tagLongArray: 8}[tag]
		return f.skip(int64(n) * size)
	case tagString:
		_, err := f.str()
		return err
	case tagList:
		var hdr [1]byte
		if _, err := io.ReadFull(f.r, hdr[:]); err != nil {
			return err
		}
		var n int32
		if err := binary.Read(f.r, binary.BigEndian, &n); err != nil {
			return err
		}
		for i := int32(0); i < n; i++ {
			// Liste içindeki bileşikler tohum taşımaz; yolu "[]" ile
			// işaretleyip yanlış bir "seed" eşleşmesini önlüyoruz.
			if err := f.payload(hdr[0], append(path, "[]"), depth+1); err != nil {
				return err
			}
		}
		return nil
	case tagCompound:
		return f.compound(path, depth)
	}
	return fmt.Errorf("bilinmeyen NBT etiketi %d", tag)
}
