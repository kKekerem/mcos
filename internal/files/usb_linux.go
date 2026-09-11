//go:build linux

package files

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mcos/internal/model"
)

// USB jar tarama ve kurulumu.
//
// Tasarım kararları:
//
//   - Aygıt seçimi /sys üzerinden yapılır: bir bölümün üst diski
//     "removable=1" ise VEYA aygıt yolu USB veri yolundan geçiyorsa aday
//     kabul edilir. Eski sürüm yalnızca isme bakıyordu ("sd*", "nvme*",
//     "mmcblk*") ve son karakteri rakam olan her şeyi bölüm sanıyordu — bu
//     yüzden dahili NVMe diskinin TAMAMINI (nvme0n1) bağlamaya çalışıyordu.
//
//   - Tarama, bölümü geçici olarak salt-okunur bağlar ve işi bitince ayırır.
//     Bu yüzden sonuçlarda mutlak yol DEĞİL, (aygıt + göreli yol) taşınır;
//     kurulum aygıtı yeniden bağlar. Eski sürüm mutlak yol döndürüp mount'u
//     kaldırdığı için kurulum daima 0 dosya kopyalıyor, üstelik hata da
//     döndürmüyordu ("0 mod/eklenti kuruldu" başarı mesajı).
//
//   - Zaten bağlı olan bölümler yeniden bağlanmaz; mevcut bağlama noktası
//     kullanılır ve ayrılmaz.

const (
	// maxJarResults, patolojik dizin ağaçlarında taramanın sınırsız
	// büyümesini engeller.
	maxJarResults = 500
	// maxWalkDepth, bölüm kökünden itibaren taranacak en fazla dizin derinliği.
	maxWalkDepth = 6
	// criticalMounts, asla taranmayacak bağlama noktaları.
	sysBlock = "/sys/block"
)

// criticalMountPoints are never scanned: sistemin kendi bölümleri.
var criticalMountPoints = map[string]bool{
	"/": true, "/boot": true, "/data": true, "/usr": true, "/var": true,
}

// ScanUSBJars finds .jar files on removable/USB partitions.
func ScanUSBJars() ([]model.USBJar, error) {
	parts, err := removablePartitions()
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, nil
	}

	mounts := currentMounts()
	var out []model.USBJar

	for _, dev := range parts {
		if mp, ok := mounts[dev]; ok && criticalMountPoints[mp] {
			continue // sistem bölümü
		}
		err := withMounted(dev, mounts, func(root string) error {
			found, werr := walkJars(root, dev, maxJarResults-len(out))
			out = append(out, found...)
			return werr
		})
		if err != nil {
			// Tek bir bozuk/şifreli bölüm tüm taramayı düşürmesin.
			continue
		}
		if len(out) >= maxJarResults {
			break
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out, nil
}

// InstallUSBJars copies the selected jars into destDir, re-mounting each source
// device as needed. Returns the number of files actually copied.
func InstallUSBJars(items []model.USBJar, destDir string) (int, error) {
	if len(items) == 0 {
		return 0, fmt.Errorf("kurulacak dosya seçilmedi")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, fmt.Errorf("hedef klasör oluşturulamadı (%s): %w", destDir, err)
	}

	// Aygıta göre grupla: her aygıt yalnızca bir kez bağlanır.
	byDevice := map[string][]model.USBJar{}
	order := []string{}
	for _, it := range items {
		if _, seen := byDevice[it.Device]; !seen {
			order = append(order, it.Device)
		}
		byDevice[it.Device] = append(byDevice[it.Device], it)
	}

	mounts := currentMounts()
	copied := 0
	var failures []string

	for _, dev := range order {
		group := byDevice[dev]
		err := withMounted(dev, mounts, func(root string) error {
			for _, it := range group {
				src := filepath.Join(root, filepath.FromSlash(it.RelPath))
				// Bağlama noktasından çıkışı engelle (kötü niyetli RelPath).
				if !withinRoot(root, src) {
					failures = append(failures, it.Name+" (geçersiz yol)")
					continue
				}
				dst := filepath.Join(destDir, filepath.Base(it.Name))
				if err := copyFile(src, dst); err != nil {
					failures = append(failures, fmt.Sprintf("%s (%v)", it.Name, err))
					continue
				}
				copied++
			}
			return nil
		})
		if err != nil {
			for _, it := range group {
				failures = append(failures, fmt.Sprintf("%s (%s bağlanamadı)", it.Name, dev))
			}
		}
	}

	// Sessiz başarısızlık YOK: hiçbir şey kopyalanmadıysa hata döndür.
	if copied == 0 {
		return 0, fmt.Errorf("hiçbir dosya kopyalanamadı: %s", strings.Join(failures, ", "))
	}
	if len(failures) > 0 {
		return copied, fmt.Errorf("%d dosya kuruldu, %d dosya başarısız: %s",
			copied, len(failures), strings.Join(failures, ", "))
	}
	return copied, nil
}

// ── /sys ve /proc üzerinden aygıt keşfi ─────────────────────────────────────

// removablePartitions returns partition device paths that live on a removable
// or USB-attached disk.
func removablePartitions() ([]string, error) {
	disks, err := os.ReadDir(sysBlock)
	if err != nil {
		return nil, fmt.Errorf("/sys/block okunamadı: %w", err)
	}

	var parts []string
	for _, d := range disks {
		disk := d.Name()
		// Sanal aygıtları atla (loop, ram, dm, zram, sr...).
		switch {
		case strings.HasPrefix(disk, "loop"),
			strings.HasPrefix(disk, "ram"),
			strings.HasPrefix(disk, "dm-"),
			strings.HasPrefix(disk, "zram"),
			strings.HasPrefix(disk, "md"):
			continue
		}
		if !isRemovableOrUSB(disk) {
			continue
		}
		// Bölümler: /sys/block/<disk>/<disk-adı-eki>/partition dosyası olanlar.
		entries, err := os.ReadDir(filepath.Join(sysBlock, disk))
		if err != nil {
			continue
		}
		hasPart := false
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), disk) {
				continue
			}
			if _, err := os.Stat(filepath.Join(sysBlock, disk, e.Name(), "partition")); err != nil {
				continue
			}
			hasPart = true
			parts = append(parts, filepath.Join("/dev", e.Name()))
		}
		// Bölümlenmemiş (süperfloppy) USB'ler: diskin kendisi dosya sistemi taşır.
		if !hasPart {
			parts = append(parts, filepath.Join("/dev", disk))
		}
	}
	sort.Strings(parts)
	return parts, nil
}

// isRemovableOrUSB reports whether a whole disk is removable media or sits on
// the USB bus. Bazı USB SSD'ler removable=0 bildirdiği için iki kontrol var.
func isRemovableOrUSB(disk string) bool {
	if b, err := os.ReadFile(filepath.Join(sysBlock, disk, "removable")); err == nil {
		if strings.TrimSpace(string(b)) == "1" {
			return true
		}
	}
	// /sys/block/sdb -> ../devices/pci.../usb1/1-1/1-1:1.0/host4/.../block/sdb
	if target, err := os.Readlink(filepath.Join(sysBlock, disk)); err == nil {
		if strings.Contains(target, "/usb") {
			return true
		}
	}
	return false
}

// currentMounts maps device path -> mount point from /proc/mounts.
func currentMounts() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasPrefix(f[0], "/dev/") {
			if _, exists := out[f[0]]; !exists {
				out[f[0]] = unescapeMount(f[1])
			}
		}
	}
	return out
}

// unescapeMount decodes the octal escapes /proc/mounts uses for spaces etc.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var v int
			ok := true
			for _, c := range s[i+1 : i+4] {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				v = v*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// withMounted runs fn with dev mounted read-only, reusing an existing mount if
// the device is already mounted (and then never unmounting it).
func withMounted(dev string, mounts map[string]string, fn func(root string) error) error {
	if mp, ok := mounts[dev]; ok && mp != "" {
		return fn(mp) // zaten bağlı — dokunma
	}

	tmp, err := os.MkdirTemp("", "mcos-usb-")
	if err != nil {
		return fmt.Errorf("geçici bağlama dizini oluşturulamadı: %w", err)
	}
	defer os.Remove(tmp)

	if err := exec.Command("mount", "-o", "ro,noexec,nosuid,nodev", dev, tmp).Run(); err != nil {
		// Bazı dosya sistemleri bu seçenekleri kabul etmez; sade ro dene.
		if err2 := exec.Command("mount", "-o", "ro", dev, tmp).Run(); err2 != nil {
			return fmt.Errorf("%s bağlanamadı: %w", dev, err2)
		}
	}
	defer func() { _ = exec.Command("umount", tmp).Run() }()

	return fn(tmp)
}

// ── Dosya işlemleri ─────────────────────────────────────────────────────────

// walkJars collects up to limit .jar files under root.
func walkJars(root, dev string, limit int) ([]model.USBJar, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []model.USBJar
	rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // okunamayan dalı atla
		}
		if d.IsDir() {
			if strings.Count(filepath.Clean(p), string(filepath.Separator))-rootDepth >= maxWalkDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".jar") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() == 0 {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		out = append(out, model.USBJar{
			Name:      d.Name(),
			Device:    dev,
			RelPath:   filepath.ToSlash(rel),
			SizeBytes: info.Size(),
		})
		if len(out) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return out, err
}

// withinRoot reports whether p stays inside root after cleaning.
func withinRoot(root, p string) bool {
	rc := filepath.Clean(root)
	pc := filepath.Clean(p)
	return pc == rc || strings.HasPrefix(pc, rc+string(filepath.Separator))
}

// copyFile copies src to dst, fsyncing so the jar survives an abrupt power cut.
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("dizin")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".mcos-part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
