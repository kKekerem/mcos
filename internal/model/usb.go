package model

import (
	"fmt"
	"path"
)

// USBJar is a .jar file discovered on a removable drive.
//
// Konum, mutlak yol olarak DEĞİL, (bölüm aygıtı + bölüm kökünden göreli yol)
// çifti olarak taşınır. Sebep: tarama, bölümü geçici olarak bağlar ve işi
// bitince ayırır — bu yüzden tarama sırasındaki mutlak yol kurulum anında
// artık geçerli değildir. Kurulum, aygıtı yeniden bağlayıp RelPath'i kullanır.
type USBJar struct {
	Name      string `json:"name"`      // dosya adı, ör. "ViaVersion-5.0.3.jar"
	Device    string `json:"device"`    // kaynak bölüm, ör. "/dev/sdb1"
	RelPath   string `json:"relPath"`   // bölüm kökünden göreli yol
	SizeBytes int64  `json:"sizeBytes"` // dosya boyutu
}

// Origin returns a short, human-readable source label for the list view:
// aygıt adı + varsa içindeki dizin ("sdb1", "sdb1:mods").
func (j USBJar) Origin() string {
	dev := path.Base(j.Device)
	dir := path.Dir(j.RelPath)
	if dir == "." || dir == "/" || dir == "" {
		return dev
	}
	return fmt.Sprintf("%s:%s", dev, dir)
}

// Key uniquely identifies a jar across devices. İsim+boyut yeterli değildi:
// iki farklı USB'deki aynı isim/boyuttaki dosyalar birleşiyordu.
func (j USBJar) Key() string { return j.Device + "|" + j.RelPath }
