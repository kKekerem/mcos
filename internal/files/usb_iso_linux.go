//go:build linux

package files

import (
	"fmt"
	"os"
	"path/filepath"
)

// ScanUSBISOs mounts each removable partition read-only and calls fn for
// every .iso file on it.
//
// fn bölüm BAĞLIYKEN çağrılır: çağıran ISO'yu o sırada açıp (mcos-update
// --denetle) kimliğini okuyabilir. Dönüşten sonra Abs artık geçersizdir.
func ScanUSBISOs(fn func(USBISOFile)) error {
	parts, err := removablePartitions()
	if err != nil {
		return err
	}
	mounts := currentMounts()
	for _, dev := range parts {
		if mp, ok := mounts[dev]; ok && criticalMountPoints[mp] {
			continue
		}
		_ = withMounted(dev, mounts, func(root string) error {
			for _, rel := range findISOs(root) {
				abs := filepath.Join(root, rel)
				st, err := os.Stat(abs)
				if err != nil || !st.Mode().IsRegular() {
					continue
				}
				fn(USBISOFile{Device: dev, RelPath: filepath.ToSlash(rel), Abs: abs, Size: st.Size()})
			}
			return nil
		})
	}
	return nil
}

// WithUSBFile mounts dev read-only and calls fn with the absolute path of rel.
// Bölüm fn dönene dek bağlı kalır (güncelleme ISO'yu baştan sona okur).
func WithUSBFile(dev, rel string, fn func(abs string) error) error {
	return withMounted(dev, currentMounts(), func(root string) error {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if !withinRoot(root, abs) || abs == filepath.Clean(root) {
			return fmt.Errorf("geçersiz dosya yolu: %s", rel)
		}
		st, err := os.Stat(abs)
		if err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("dosya bulunamadı: %s (USB çıkarılmış olabilir)", rel)
		}
		return fn(abs)
	})
}
