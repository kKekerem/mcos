//go:build !linux

package files

import (
	"fmt"

	"mcos/internal/model"
)

// USB tarama/kurulum çekirdeğe özgüdür (mount, /sys/block, /proc/mounts).
// Geliştirici makinelerinde (Windows/macOS) açık bir hata döndürülür — sessiz
// boş sonuç, kullanıcıya "USB'de jar yok" gibi yanlış bir bilgi verirdi.

// ScanUSBJars is unsupported off Linux.
func ScanUSBJars() ([]model.USBJar, error) {
	return nil, fmt.Errorf("USB taraması yalnızca MCOS cihazında (Linux) çalışır")
}

// InstallUSBJars is unsupported off Linux.
func InstallUSBJars(_ []model.USBJar, _ string) (int, error) {
	return 0, fmt.Errorf("USB kurulumu yalnızca MCOS cihazında (Linux) çalışır")
}
