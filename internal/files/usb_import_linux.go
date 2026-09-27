//go:build linux

package files

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mcos/internal/model"
)

// importScanDepth: sunucu klasörü USB'de genelde kökte ya da bir iki klasör
// içindedir ("Sunucular/Hayatta Kalma"). Daha derine inmek, dünyanın
// içindeki "region" klasörlerini de dolaşmak demek.
const importScanDepth = 3

// ScanUSBServerFolders lists importable server folders on USB drives.
//
// Önce SUNUCU OLARAK TANINAN klasörler (server.properties, dünya, sunucu
// jar'ı); hiç tanınan yoksa kullanıcı elle seçebilsin diye üst düzey
// klasörler de listelenir.
func ScanUSBServerFolders() ([]model.USBServerFolder, error) {
	parts, err := removablePartitions()
	if err != nil {
		return nil, err
	}
	mounts := currentMounts()
	var found, plain []model.USBServerFolder
	for _, dev := range parts {
		if mp, ok := mounts[dev]; ok && criticalMountPoints[mp] {
			continue
		}
		_ = withMounted(dev, mounts, func(root string) error {
			found = append(found, scanServerDirs(root, dev)...)
			ents, _ := os.ReadDir(root)
			for _, e := range ents {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") ||
					strings.EqualFold(e.Name(), "System Volume Information") {
					continue
				}
				plain = append(plain, model.USBServerFolder{Device: dev, RelPath: e.Name(), Name: e.Name()})
			}
			return nil
		})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].RelPath < found[j].RelPath })
	if len(found) > 0 {
		return found, nil
	}
	return plain, nil
}

func scanServerDirs(root, dev string) []model.USBServerFolder {
	var out []model.USBServerFolder
	rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if strings.Count(filepath.Clean(p), string(filepath.Separator))-rootDepth > importScanDepth {
			return filepath.SkipDir
		}
		info := DetectServerDir(p)
		if info.Score < 3 {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			rel = ""
		}
		name := filepath.Base(p)
		if rel == "" {
			name = filepath.Base(dev)
		}
		out = append(out, model.USBServerFolder{
			Device: dev, RelPath: filepath.ToSlash(rel), Name: name,
			Software: info.Software, MCVersion: info.MCVersion, HasWorld: info.HasWorld,
			Mods: info.Mods, Plugins: info.Plugins, SizeBytes: dirSize(p), Recognized: true,
		})
		return filepath.SkipDir // sunucunun içindeki dünyayı ayrıca listeleme
	})
	return out
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if st, e := d.Info(); e == nil {
				n += st.Size()
			}
		}
		return nil
	})
	return n
}

// CopyUSBFolder copies a folder from a USB partition into dest (yoksa
// oluşturulur) and returns what the folder looked like.
//
// Sembolik bağlar ve aygıt dosyaları kopyalanmaz (FAT/exFAT'ta zaten yok;
// ext4 bir USB'de bağlama noktasının dışına işaret edebilirler).
func CopyUSBFolder(dev, rel, dest string) (ServerDirInfo, error) {
	var info ServerDirInfo
	err := withMounted(dev, currentMounts(), func(root string) error {
		src := filepath.Join(root, filepath.FromSlash(rel))
		if !withinRoot(root, src) {
			return fmt.Errorf("geçersiz klasör yolu")
		}
		st, err := os.Stat(src)
		if err != nil || !st.IsDir() {
			return fmt.Errorf("klasör bulunamadı: %s", rel)
		}
		info = DetectServerDir(src)
		return copyTree(src, dest)
	})
	return info, err
}

func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("%s okunamadı: %w", p, err)
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dest, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyPlain(p, target)
		}
		return nil // sembolik bağ, aygıt, soket: atla
	})
}

// copyPlain is a streaming copy (büyük bölge dosyaları için belleğe almaz).
func copyPlain(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("%s kopyalanamadı: %w", filepath.Base(src), err)
	}
	return out.Close()
}
