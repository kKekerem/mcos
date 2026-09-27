package files

import (
	"path/filepath"
	"sort"
	"strings"
)

// USBISOFile is an .iso file found on a USB partition (sistem güncellemesi).
type USBISOFile struct {
	Device  string // bölüm aygıtı (/dev/sdb1)
	RelPath string // bölüm köküne göre, "/" ayraçlı
	Abs     string // YALNIZCA tarama geri çağrısı sürerken geçerli
	Size    int64
}

// isoPatterns: ISO kökte ya da bir klasör içinde olur ("ISO/mcos.iso",
// Ventoy'da "MCOS/..."). Daha derine inmek büyük bir USB'de taramayı
// dakikalara çıkarırdı; kullanıcı dosyayı genelde köke kopyalar.
var isoPatterns = []string{"*.iso", "*.ISO", "*.Iso", "*/*.iso", "*/*.ISO", "*/*.Iso"}

// findISOs lists .iso files under root (kök + bir klasör), sıralı ve tekil.
func findISOs(root string) []string {
	seen := map[string]bool{}
	var out []string
	for _, pat := range isoPatterns {
		m, _ := filepath.Glob(filepath.Join(root, pat))
		for _, p := range m {
			rel, err := filepath.Rel(root, p)
			if err != nil || seen[rel] || strings.HasPrefix(filepath.Base(p), ".") {
				continue
			}
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}
