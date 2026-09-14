//go:build linux

package flash

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
)

// openDevice opens a block device for raw read/write.
//
// O_SYNC KULLANILMIYOR: her yazımı beklemek USB'de hızı on kat düşürür.
// Bunun yerine sonunda tek bir Sync() çağrılıyor — aynı garantiyi verir,
// çok daha hızlıdır.
//
// Linux'ta *os.File zaten deviceFile arayüzünü karşılar; Windows'taki gibi
// ayrı bir sarmalayıcıya gerek yok (orada birim kilitleme gerekiyor).
func openDevice(path string) (deviceFile, error) {
	// -- Yakalanan gercek hata -------------------------------------------
	// Burasi eskiden duz O_RDWR ile aciyordu. GNOME/KDE bir USB bellegi
	// takilir takilmaz kendiliginden baglar, ve Linux bagli bir aygiti
	// yazmak icin acmaya IZIN VERIR. Imaj yaziliyor, ardindan hala bagli
	// olan ESKI dosya sistemi kirli ust verisini ayni sektorlere geri
	// yaziyor (cikarirken bir kez daha) ve imajin bir kismini bozuyor.
	//
	// Sonuc sessizdi: bellek acilmiyordu, hicbir hata gorunmuyordu.
	// Dogrulama okumasi bile gecebiliyordu, cunku geri yazma daha sonra
	// geliyor.
	//
	// Iki katmanli koruma:
	//   1. Bu diskin bolumlerini ayir (kullanici zaten diski silmeyi
	//      onaylamis durumda; Windows tarafi da FSCTL_DISMOUNT_VOLUME ile
	//      ayni seyi yapiyor),
	//   2. O_EXCL ile ac: ayrilamayan bir sey kaldiysa cekirdek EBUSY verir
	//      ve sessizce bozmak yerine acikca reddederiz.
	if err := unmountPartitions(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_EXCL, 0)
	if err != nil {
		if errors.Is(err, syscall.EBUSY) {
			return nil, fmt.Errorf(
				"%s kullanımda (bir bölümü hâlâ bağlı ya da başka bir program "+
					"kullanıyor) — aygıtı çıkarıp yeniden takın", path)
		}
		return nil, err
	}
	return f, nil
}

// unmountPartitions detaches every filesystem living on the target disk.
//
// Yalnizca HEDEF diskin bolumleri ayrilir: /proc/mounts taranir ve her
// girdinin ust diski hesaplanip yolun diskiyle karsilastirilir. Baska bir
// diskteki hicbir sey etkilenmez.
//
// Tembel ayirma (MNT_DETACH) BILEREK kullanilmiyor: tembel ayirma dosya
// sistemini gorunurden kaldirir ama kirli ust verinin geri yazilmasini
// engellemez -- yani duzeltmeye calistigimiz hatayi aynen birakirdi.
func unmountPartitions(devPath string) error {
	target := parentDisk(strings.TrimPrefix(devPath, "/dev/"))
	if target == "" {
		return nil
	}

	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil // bilgi yoksa O_EXCL yine de koruyor
	}
	defer f.Close()

	var points []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		backing := map[string]bool{}
		collectBackingDisks(kernelName(fields[0]), backing, 0)
		if backing[target] {
			points = append(points, unescapeMount(fields[1]))
		}
	}

	// En derin yoldan basla: /media/x/y, /media/x'ten once ayrilmali.
	sort.Slice(points, func(i, j int) bool { return len(points[i]) > len(points[j]) })

	for _, mp := range points {
		if err := syscall.Unmount(mp, 0); err != nil {
			return fmt.Errorf("%s ayrılamadı: %w — aygıtı elle çıkarın", mp, err)
		}
	}
	return nil
}

// dropCaches makes the next read come from the MEDIUM, not the page cache.
//
// NEDEN ŞART: doğrulama için geri okurken çekirdek önbelleği devreye
// girerse, az önce YAZDIĞIMIZ veriyi bellekten geri okuruz ve bozuk bir
// belleği "sağlam" ilan ederiz. BLKFLSBUF o önbelleği boşaltır.
func dropCaches(f deviceFile) error {
	// Yalnızca gerçek bir dosya tanıtıcısında anlamlı; başka bir uygulama
	// gelirse (test sahtesi) sessizce atlanır.
	file, ok := f.(*os.File)
	if !ok {
		return nil
	}
	// BLKFLSBUF = 0x1261 (linux/fs.h): blok aygıtının arabelleklerini boşalt.
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), 0x1261, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// DefaultImageDirs returns where mcos-flash looks for images.
//
// Uygulamanın YANINDAKİ klasör de taranır: kullanıcı imajı ikilinin yanına
// koyup çalıştırabilmeli, bir yol yazmak zorunda kalmamalı.
func DefaultImageDirs() []string {
	dirs := []string{".", "dist", "images"}
	if exe, err := os.Executable(); err == nil {
		base := exe[:len(exe)-len(baseName(exe))]
		dirs = append([]string{base, base + "images"}, dirs...)
	}
	return dirs
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
