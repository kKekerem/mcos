package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/timesync"
)

// hasInternet does a fast TCP probe to well-known resolvers (port 53). It avoids
// DNS so a wrong clock / missing certs can't interfere — the same reachability
// signal the time-sync loop and OOBE rely on.
func hasInternet() bool {
	for _, addr := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// --- Power (Kapat / Yeniden Başlat) ---------------------------------------

func (d *Daemon) handleSystemPower(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.PowerParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	var cmd string
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "poweroff", "shutdown", "off", "kapat":
		cmd = "poweroff"
	case "reboot", "restart", "yeniden":
		cmd = "reboot"
	default:
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "action poweroff|reboot olmalı"}
	}
	d.log.Infof("system: %s requested", cmd)
	// Defer briefly so the RPC reply reaches the panel before the box goes down.
	go func(c string) {
		time.Sleep(500 * time.Millisecond)
		d.stopServersForPower()
		if err := exec.Command(c).Run(); err != nil {
			_ = exec.Command("busybox", c).Run() // busybox applet fallback
		}
	}(cmd)
	return ipc.OKResult{OK: true, Message: cmd}, nil
}

// powerStopLimit: kapatmadan önce sunucuların durması için en fazla bekleme.
// Takılan bir sunucu kapatmayı sonsuza dek engellememeli.
var powerStopLimit = 2 * time.Minute

// stopServersForPower stops every server gracefully before poweroff/reboot.
//
// Neden burada: eski yolda "reboot" doğrudan çalışıyordu; S99mcos stop
// mcosd'ye yalnızca SIGTERM gönderip BEKLEMİYOR, busybox init birkaç saniye
// sonra her şeyi öldürüyordu. Sunucu dünyayı kaydedemeden kesilebilirdi —
// güncellemeden sonra "Şimdi yeniden başlat" da bu yoldan geçer ve kullanıcı
// "veri kaybı olmasın" dedi. Process.Stop kendi zaman aşımıyla SIGKILL'e
// düşer; üst sınır yine de burada.
func (d *Daemon) stopServersForPower() {
	if d.sup == nil {
		return
	}
	done := make(chan struct{})
	go func() { d.sup.StopAll(); close(done) }()
	select {
	case <-done:
		d.log.Infof("system: sunucular durduruldu")
	case <-time.After(powerStopLimit):
		d.log.Warnf("system: sunucular %s içinde durmadı; yine de devam ediliyor", powerStopLimit)
	}
}

// --- Turbo (genel boost anahtarı) -----------------------------------------
//
// handleSystemTurbo handlers_turbo.go'ya taşındı: artık yalnızca yapılandırma
// yazmıyor, donanımı ve çalışan sunucuları anında turboya alıyor.

// --- Persistence: make the booted USB persistent --------------------------

func (d *Daemon) handleSystemDisks(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.DisksResult{Disks: listDiskTargets()}, nil
}

func (d *Daemon) handleSystemPersist(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.PersistParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	args := []string{}
	if dev := strings.TrimSpace(p.Device); dev != "" {
		args = append(args, dev)
	}
	// mcos-persist auto-detects the booted USB when no device is given. It is a
	// rootfs-overlay script; on a dev host it simply isn't found (graceful err).
	out, err := exec.Command("mcos-persist", args...).CombinedOutput()
	if err != nil {
		// ── Neden çıkış kodu 3 AYRI ─────────────────────────────────────
		//
		// Canlı ISO'dan (DVD, QEMU -cdrom, VMware) açıldığında boot ortamı
		// SALT OKUNUR olur ve ona bölüm eklemek fiziksel olarak imkânsızdır.
		// Bu bir arıza değil; kullanıcının yapması gereken şey bellidir.
		// Sihirbazın sonunda kırmızı bir "kalıcılık başarısız" göstermek
		// yanlış bilgi veriyordu — mcos-persist artık bu durumda 3 dönüyor.
		if kod(err) == 3 {
			return ipc.OKResult{OK: false, Message: strings.TrimSpace(string(out))}, nil
		}
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: "kalıcılık başarısız: " + strings.TrimSpace(string(out))}
	}
	d.log.Infof("system: persist done: %s", strings.TrimSpace(string(out)))
	return ipc.OKResult{OK: true, Message: "USB kalıcı yapıldı — /data artık reboot'ta korunur"}, nil
}

// kod extracts a helper's exit status, or -1 when it didn't run at all.
func kod(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// bootDisks returns the whole disks the running system lives on; they are
// never offered as install targets.
//
// ── Ölçülen hatalar ─────────────────────────────────────────────────────────
//
//   - MCOS Ventoy'dan açılınca Ventoy USB'si hedef listesinde görünüyordu
//     (QEMU, system.disks: "/dev/sda ... isBootDisk:false"). O disk hem ISO'yu
//     hem /data'nın arka dosyasını (loop) taşıyor; seçilirse mcos-install ya
//     çalışan sistemi bozar ya da 1 ile çıkar.
//   - Kurulu bir diskten açılınca kök bölümü ("/") hiç denetlenmiyordu.
//   - Yedek olarak "blkid -L MCOS-BOOT" çağrılıyordu. busybox blkid seçenek
//     TANIMAZ ("-L"yi aygıt adı sanıp boş döner — ölçüldü), yani hiç
//     çalışmadı; çalışsaydı da hedef diskteki ESKİ bir MCOS kurulumunun
//     MCOS-BOOT'unu bulup kurulmak istenen diski listeden düşürürdü.
func bootDisks() map[string]bool {
	out := map[string]bool{}
	mounts, _ := os.ReadFile("/proc/mounts")
	for dev := range bootDevicesFromMounts(string(mounts), loopBackingFile) {
		out[parentDisk(dev)] = true
	}
	// Ventoy açılışı: Ventoy USB'si, ISO bir bölüm olarak görünmediği için
	// mount tablosunda hiç yer almayabilir (kalıcılık dosyası yoksa).
	if _, err := os.Stat("/ventoy/ventoy_os_param"); err == nil {
		if b, err := exec.Command("mcos-vtoydata", "bul").Output(); err == nil {
			if f := strings.Fields(string(b)); len(f) > 0 && strings.HasPrefix(f[0], "/dev/") {
				out[parentDisk(f[0])] = true
			}
		}
	}
	return out
}

// bootDevicesFromMounts extracts, from /proc/mounts text, the block devices the
// running system boots/runs from. A loop-backed /data counts as the device
// holding its backing file (Ventoy persistence file). A /data that is itself a
// partition does NOT count: mcos-install renews such a disk in place and keeps
// that partition.
func bootDevicesFromMounts(mounts string, backing func(loopDev string) string) map[string]bool {
	type mnt struct{ dev, mp string }
	var all []mnt
	for _, line := range strings.Split(mounts, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		all = append(all, mnt{f[0], strings.ReplaceAll(f[1], `\040`, " ")})
	}
	out := map[string]bool{}
	for _, m := range all {
		if !strings.HasPrefix(m.dev, "/dev/") {
			continue
		}
		isLoop := strings.HasPrefix(m.dev, "/dev/loop")
		switch {
		case m.mp == "/", m.mp == "/boot", m.mp == "/mnt/cdrom", m.mp == "/run/mcos/boot",
			m.mp == "/run/mcos/ventoy", strings.HasPrefix(m.mp, "/media/"):
			if !isLoop {
				out[m.dev] = true
			}
		}
		if m.mp == "/data" && isLoop && backing != nil {
			// Arka dosyanın durduğu dosya sistemi: en uzun eşleşen bağlama noktası.
			file := backing(m.dev)
			bestMP, bestDev := "", ""
			for _, o := range all {
				if !strings.HasPrefix(o.dev, "/dev/") || strings.HasPrefix(o.dev, "/dev/loop") {
					continue
				}
				prefix := strings.TrimSuffix(o.mp, "/") + "/"
				if file != "" && strings.HasPrefix(file, prefix) && len(o.mp) >= len(bestMP) {
					bestMP, bestDev = o.mp, o.dev
				}
			}
			if bestDev != "" {
				out[bestDev] = true
			}
		}
	}
	return out
}

// loopBackingFile returns the file behind /dev/loopN (sysfs), or "".
func loopBackingFile(loopDev string) string {
	return readSysfs("/sys/block/" + strings.TrimPrefix(loopDev, "/dev/") + "/loop/backing_file")
}

// listDiskTargets enumerates real block devices from sysfs. Returns an empty
// list off-Linux (the sysfs paths simply don't exist), so it compiles and runs
// harmlessly on the dev host.
func listDiskTargets() []ipc.DiskTarget {
	ents, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	persist := disksWithLabel("MCOS-DATA")
	boot := bootDisks()
	var out []ipc.DiskTarget
	for _, e := range ents {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "loop"), strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "sr"), strings.HasPrefix(name, "dm-"),
			strings.HasPrefix(name, "md"), strings.HasPrefix(name, "zram"):
			continue
		case strings.HasPrefix(name, "nvme"):
			// Kullanıcının isteği: NVMe aygıtlar listelenmesin. NVMe bir
			// bilgisayarda neredeyse her zaman Windows'un/asıl sistemin
			// kurulu olduğu DAHİLİ disktir; kurulum listesinde görünmesi
			// yanlış seçimle o diskin silinmesi riskidir.
			continue
		}
		base := "/sys/block/" + name
		dev := "/dev/" + name

		// Exclude the currently booted installation media USB
		if boot[dev] {
			continue
		}

		// Sysfs ağacında yukarı çıkarak (parent) cihazın USB üzerinden gelip gelmediğini bul.
		// Bu yöntem "removable=0" olan (Windows To Go vs) USB'leri de kesin olarak tespit eder.
		isUSB := false
		path := base + "/device"
		for i := 0; i < 8; i++ {
			link, err := os.Readlink(path + "/subsystem")
			if err == nil && strings.HasSuffix(link, "/usb") {
				isUSB = true
				break
			}
			path = path + "/.."
		}

		// Also check using EvalSymlinks to be robust against complex sysfs topologies
		if !isUSB {
			realDevPath, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", name))
			if err == nil && strings.Contains(realDevPath, "/usb") {
				isUSB = true
			}
		}

		removable := readSysfsUint(base+"/removable") == 1

		// Ortamı takılı olmayan kart okuyucu yuvası 0 bayt görünür; seçilirse
		// kurulum "geçersiz aygıt" ile 1 döner. Listelenmez.
		if readSysfsUint(base+"/size") == 0 {
			continue
		}

		out = append(out, ipc.DiskTarget{
			Device:     dev,
			Model:      strings.TrimSpace(readSysfs(base + "/device/model")),
			SizeBytes:  readSysfsUint(base+"/size") * 512,
			Removable:  removable,
			HasPersist: persist[dev],
		})
	}
	return out
}

// disksWithLabel maps parent disks that carry a partition with the given label.
//
// Eskiden "blkid -L <etiket>" çağrılıyordu; busybox blkid seçenek tanımadığı
// için çıktı HER ZAMAN boştu (ölçüldü: önceden MCOS kurulu NVMe diskte
// hasPersist:false). Artık argümansız blkid'in tüm listesi ayrıştırılıyor.
func disksWithLabel(label string) map[string]bool {
	b, err := exec.Command("blkid").Output()
	if err != nil {
		return map[string]bool{}
	}
	return parseBlkidLabel(string(b), label)
}

// parseBlkidLabel parses busybox/util-linux blkid output lines such as
//
//	/dev/sda3: LABEL="MCOS-DATA" UUID="…" TYPE="ext4"
//
// and returns the parent disks of partitions whose label matches exactly.
func parseBlkidLabel(out, label string) map[string]bool {
	res := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		dev := line[:i]
		j := strings.Index(line, ` LABEL="`)
		if j < 0 {
			continue
		}
		rest := line[j+len(` LABEL="`):]
		k := strings.Index(rest, `"`)
		if k < 0 || rest[:k] != label {
			continue
		}
		res[parentDisk(dev)] = true
	}
	return res
}

// parentDisk strips a partition device down to its whole-disk node
// (/dev/sda2 -> /dev/sda, /dev/nvme0n1p2 -> /dev/nvme0n1, /dev/mmcblk0p1 -> /dev/mmcblk0).
func parentDisk(part string) string {
	p := strings.TrimPrefix(part, "/dev/")
	if i := strings.LastIndex(p, "p"); i > 0 && (strings.HasPrefix(p, "nvme") || strings.HasPrefix(p, "mmcblk")) {
		return "/dev/" + p[:i]
	}
	p = strings.TrimRight(p, "0123456789")
	return "/dev/" + p
}

func readSysfs(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readSysfsUint(path string) uint64 {
	v, _ := strconv.ParseUint(readSysfs(path), 10, 64)
	return v
}

// --- Time sync loop --------------------------------------------------------

// timeSyncLoop waits for internet, sets the clock once from NTP, then refreshes
// it periodically. RTC-less PCs boot with a wrong date that breaks every TLS
// download until this runs, so the OOBE gates Java behind ClockSynced.
func (d *Daemon) timeSyncLoop(ctx context.Context) {
	// Wait for connectivity (cheap probe; reuses the same idea as sysmon).
	for !hasInternet() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
	if t, err := timesync.Sync(false); err != nil {
		d.log.Warnf("timesync: %v", err)
	} else {
		d.log.Infof("timesync: clock set to %s", t.Format(time.RFC3339))
	}
	tk := time.NewTicker(6 * time.Hour)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			if _, err := timesync.Sync(false); err != nil {
				d.log.Warnf("timesync: refresh: %v", err)
			}
		}
	}
}

// syncClockAsync kicks a one-shot, non-blocking sync (used right after the
// network comes up via the OOBE / Donanım actions).
func (d *Daemon) syncClockAsync() {
	go func() {
		if t, err := timesync.Sync(false); err != nil {
			d.log.Warnf("timesync: %v", err)
		} else {
			d.log.Infof("timesync: clock set to %s", t.Format(time.RFC3339))
		}
	}()
}
