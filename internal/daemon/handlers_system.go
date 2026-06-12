package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/store"
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
		if err := exec.Command(c).Run(); err != nil {
			_ = exec.Command("busybox", c).Run() // busybox applet fallback
		}
	}(cmd)
	return ipc.OKResult{OK: true, Message: cmd}, nil
}

// --- Turbo (genel boost anahtarı) -----------------------------------------

func (d *Daemon) handleSystemTurbo(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.TurboParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	cfg := d.Config()
	cp := *cfg
	cp.Turbo = p.Enabled
	if err := store.SaveConfig(d.cfgPath, &cp); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cfg = &cp
	d.mu.Unlock()
	d.log.Infof("system: turbo = %v", cp.Turbo)
	// New/restarted servers pick up turbo on their next launch (RAM/JVM changes
	// need a fresh process); already-running servers keep their current limits.
	return ipc.TurboResult{Enabled: cp.Turbo}, nil
}

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
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: "kalıcılık başarısız: " + strings.TrimSpace(string(out))}
	}
	d.log.Infof("system: persist done: %s", strings.TrimSpace(string(out)))
	return ipc.OKResult{OK: true, Message: "USB kalıcı yapıldı — /data artık reboot'ta korunur"}, nil
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
	var out []ipc.DiskTarget
	for _, e := range ents {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "loop"), strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "sr"), strings.HasPrefix(name, "dm-"),
			strings.HasPrefix(name, "md"), strings.HasPrefix(name, "zram"):
			continue
		}
		base := "/sys/block/" + name
		dev := "/dev/" + name
		
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

// disksWithLabel maps parent disks that carry a partition with the given label
// (via blkid). Best-effort: returns an empty map when blkid is unavailable.
func disksWithLabel(label string) map[string]bool {
	out := map[string]bool{}
	b, err := exec.Command("blkid", "-L", label).Output()
	if err != nil {
		return out
	}
	part := strings.TrimSpace(string(b)) // e.g. /dev/sda2
	if part == "" {
		return out
	}
	out[parentDisk(part)] = true
	return out
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
