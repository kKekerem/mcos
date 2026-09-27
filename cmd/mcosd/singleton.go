package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// ════════════════════════════════════════════════════════════════════════════
// TEK ÖRNEK KİLİDİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// QEMU'da çalışan bir MCOS'a SSH ile girilip `ps` çalıştırıldığında İKİ mcosd
// süreci görüldü:
//
//	159 root /usr/bin/mcosd --data-root /data ...   (ppid=1,   S99mcos)
//	184 root /usr/bin/mcosd --data-root /data ...   (ppid=160, mcos-launch)
//
// Sebep: mcos-launch'taki koruma `if ! pgrep mcosd` idi ve **pgrep imajda
// YOK**. Bulunamayan komut hata döndürüyor, `!` onu "çalışmıyor" diye okuyor
// ve ikinci daemon başlatılıyordu.
//
// ── Neden ciddi ─────────────────────────────────────────────────────────────
//
// İki daemon AYNI veri kökünü kullanıyor:
//
//   - İkisinin de süreç denetleyicisi var: aynı Minecraft sunucusunu İKİ KEZ
//     başlatabilirler. İki JVM aynı dünya klasörüne yazar — dünya bozulur.
//   - İkisinin de yedek zamanlayıcısı var: yedekler iki katına çıkar.
//   - İkisi de config.json'a yazar: son yazan kazanır, ayar kaybolur.
//
// ── Neden kilit BURADA ──────────────────────────────────────────────────────
//
// Betikteki koruma da düzeltildi (pgrep yerine /proc taraması), ama tek
// savunma ona bırakılamaz: daemon'u başlatabilecek başka yollar var (SSH'tan
// elle, bir kurtarma betiği, gelecekte eklenecek bir servis). Kilit veri
// kökünün kendisine bağlı: aynı kökü kullanan İKİNCİ süreç açılamaz.
//
// Kilit dosyası ölü bir sürecin ardından TAKILI KALMAZ: flock, süreç ölünce
// çekirdek tarafından bırakılır.

// lockDataRoot takes an exclusive lock on the data root.
//
// Döndürülen dosya AÇIK TUTULMALI: kapatmak kilidi bırakır. Bu yüzden
// çağıran onu sürecin ömrü boyunca saklıyor.
func lockDataRoot(dataRoot string) (*os.File, error) {
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return nil, fmt.Errorf("veri kökü oluşturulamadı: %w", err)
	}
	path := filepath.Join(dataRoot, "mcosd.lock")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("kilit dosyası açılamadı (%s): %w", path, err)
	}

	if err := tryLock(f); err != nil {
		// Kimin tuttuğunu söyle: "başka bir örnek var" demek, kullanıcıyı
		// hangi süreci durduracağını aramaya bırakır.
		other := "bilinmeyen"
		if b, rerr := os.ReadFile(path); rerr == nil && len(b) > 0 {
			other = string(b)
		}
		f.Close()
		return nil, fmt.Errorf(
			"mcosd zaten çalışıyor (pid %s, veri kökü %s) — ikinci örnek "+
				"aynı sunucuları başlatıp dünyayı bozabilirdi", other, dataRoot)
	}

	// PID'i yaz: bir sonraki başarısız denemede kim tuttuğunu söyleyebilelim.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	return f, nil
}
