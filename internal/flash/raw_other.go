//go:build !linux && !windows

package flash

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Bu dosya Linux ve Windows DIŞINDAKİ platformlar içindir (macOS, BSD).
//
// Orada ham disk yazımı, diskleri ayırmak (diskutil unmountDisk) ve /dev/rdiskN
// üzerinden yazmak gerektirir. Bunu doğru yapmadan "deneyelim" demek,
// kullanıcının diskini bozmak demektir — bu yüzden AÇIKÇA desteklenmediğini
// söylüyoruz. Sessizce boş liste döndürmek, kullanıcının "USB bulunamadı"
// sanmasına ve yanlış yerde sorun aramasına yol açardı.

// openDevice is unavailable on this platform.
func openDevice(string) (deviceFile, error) {
	return nil, errors.New(
		"ham aygıta yazma " + runtime.GOOS + " üzerinde desteklenmiyor " +
			"(Linux ve Windows destekleniyor)")
}

// dropCaches is a no-op on this platform.
func dropCaches(deviceFile) error { return nil }

// DefaultImageDirs returns where mcos-flash looks for images.
func DefaultImageDirs() []string {
	dirs := []string{".", "dist", "images"}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		dirs = append([]string{base, filepath.Join(base, "images")}, dirs...)
	}
	return dirs
}
