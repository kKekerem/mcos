package files

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// worldVersion reads Data.Version.Name ("1.21.1") from a world's level.dat.
//
// NEDEN: aktarılan klasörde sunucu jar'ı yoksa (kullanıcı yalnızca dünyayı
// ve modları kopyalamış olabilir) Minecraft sürümünü bilmek gerekir. Dünyayı
// DAHA ESKİ bir sürümle açmak onu bozar; level.dat son açıldığı sürümü
// taşır. Bozuk/beklenmedik dosyada "" döner (tahmin yapılmaz).
func worldVersion(levelDat string) string {
	f, err := os.Open(levelDat)
	if err != nil {
		return ""
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return ""
	}
	defer gz.Close()
	r := &nbtReader{r: io.LimitReader(gz, 64<<20), want: []string{"Data", "Version", "Name"}}
	var t [1]byte
	if _, err := io.ReadFull(r.r, t[:]); err != nil || t[0] != 10 {
		return ""
	}
	if _, err := r.str(); err != nil {
		return ""
	}
	_ = r.compound(nil, 0)
	return r.found
}

type nbtReader struct {
	r     io.Reader
	want  []string
	found string
}

func (n *nbtReader) str() (string, error) {
	var l uint16
	if err := binary.Read(n.r, binary.BigEndian, &l); err != nil {
		return "", err
	}
	b := make([]byte, l)
	_, err := io.ReadFull(n.r, b)
	return string(b), err
}

func (n *nbtReader) skip(k int64) error { _, err := io.CopyN(io.Discard, n.r, k); return err }

func (n *nbtReader) compound(path []string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("çok derin")
	}
	for n.found == "" {
		var t [1]byte
		if _, err := io.ReadFull(n.r, t[:]); err != nil {
			return err
		}
		if t[0] == 0 {
			return nil
		}
		name, err := n.str()
		if err != nil {
			return err
		}
		if err := n.payload(t[0], append(path, name), depth); err != nil {
			return err
		}
	}
	return nil
}

func (n *nbtReader) payload(tag byte, path []string, depth int) error {
	sizes := map[byte]int64{1: 1, 2: 2, 3: 4, 4: 8, 5: 4, 6: 8}
	if s, ok := sizes[tag]; ok {
		return n.skip(s)
	}
	switch tag {
	case 8:
		s, err := n.str()
		if err == nil && strings.Join(path, ".") == strings.Join(n.want, ".") {
			n.found = s
		}
		return err
	case 7, 11, 12:
		var l int32
		if err := binary.Read(n.r, binary.BigEndian, &l); err != nil || l < 0 {
			return fmt.Errorf("bozuk dizi")
		}
		return n.skip(int64(l) * map[byte]int64{7: 1, 11: 4, 12: 8}[tag])
	case 9:
		var et [1]byte
		var l int32
		if _, err := io.ReadFull(n.r, et[:]); err != nil {
			return err
		}
		if err := binary.Read(n.r, binary.BigEndian, &l); err != nil || l < 0 {
			return fmt.Errorf("bozuk liste")
		}
		for i := int32(0); i < l && n.found == ""; i++ {
			if err := n.payload(et[0], nil, depth+1); err != nil {
				return err
			}
		}
		return nil
	case 10:
		return n.compound(path, depth+1)
	}
	return fmt.Errorf("bilinmeyen etiket %d", tag)
}
