//go:build linux

package sysmon

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"mcos/internal/model"
)

var (
	cpuMu       sync.Mutex
	lastCpuUser uint64
	lastCpuNice uint64
	lastCpuSys  uint64
	lastCpuIdle uint64
)

// cpuDetails fills model/cores from /proc/cpuinfo and calculates usage from /proc/stat.
func cpuDetails(info *model.CPUInfo) {
	// 1. Read cpuinfo for static details
	f, err := os.Open("/proc/cpuinfo")
	if err == nil {
		defer f.Close()
		physical := map[string]struct{}{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			k, v, ok := splitKV(line)
			if !ok {
				continue
			}
			switch k {
			case "model name":
				if info.Model == "" {
					info.Model = v
				}
			case "cpu MHz":
				if info.MHz == 0 {
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						info.MHz = int(f)
					}
				}
			case "core id":
				physical[v] = struct{}{}
			}
		}
		if n := len(physical); n > 0 {
			info.Cores = n
		}
	}

	// 2. Read stat for CPU usage
	sf, err := os.Open("/proc/stat")
	if err == nil {
		defer sf.Close()
		sc := bufio.NewScanner(sf)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) > 4 && fields[0] == "cpu" {
				user, _ := strconv.ParseUint(fields[1], 10, 64)
				nice, _ := strconv.ParseUint(fields[2], 10, 64)
				sys, _ := strconv.ParseUint(fields[3], 10, 64)
				idle, _ := strconv.ParseUint(fields[4], 10, 64)

				cpuMu.Lock()
				totalDelta := (user + nice + sys + idle) - (lastCpuUser + lastCpuNice + lastCpuSys + lastCpuIdle)
				idleDelta := idle - lastCpuIdle

				if totalDelta > 0 && lastCpuUser > 0 {
					info.UsagePct = 100.0 * (1.0 - float64(idleDelta)/float64(totalDelta))
				}

				lastCpuUser = user
				lastCpuNice = nice
				lastCpuSys = sys
				lastCpuIdle = idle
				cpuMu.Unlock()
				break
			}
		}
	}
}

func splitKV(line string) (string, string, bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

// memory reads /proc/meminfo (values are in kB).
func memory() model.MemInfo {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return model.MemInfo{}
	}
	defer f.Close()
	var total, avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		val *= 1024
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			avail = val
		}
	}
	m := model.MemInfo{TotalBytes: total, AvailableBytes: avail}
	if total > 0 {
		m.UsedBytes = total - avail
		m.UsagePct = float64(m.UsedBytes) / float64(total) * 100
	}
	return m
}

// disks reports usage for the root filesystem (and /var/lib/mcos if separate).
func disks() []model.DiskInfo {
	var out []model.DiskInfo
	for _, mount := range []string{"/"} {
		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		free := st.Bavail * bs
		used := total - free
		d := model.DiskInfo{Mount: mount, TotalBytes: total, FreeBytes: free, UsedBytes: used}
		if total > 0 {
			d.UsagePct = float64(used) / float64(total) * 100
		}
		out = append(out, d)
	}
	return out
}

// gpus does a light probe of /sys for a DRM device name. Full GPU telemetry is
// out of scope here; absence simply disables the GPU panel.
func gpus() []model.GPUInfo {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "card") && !strings.Contains(e.Name(), "-") {
			vendor := readTrim("/sys/class/drm/" + e.Name() + "/device/vendor")
			if vendor != "" {
				return []model.GPUInfo{{Vendor: vendor, Model: "GPU"}}
			}
		}
	}
	return nil
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// enrichNIC fills Kind/Driver/Link from /sys/class/net for the Linux target so
// the OOBE and the Donanım tab can show what was actually detected (wired vs
// wireless, which driver bound, whether a cable/association is present).
func enrichNIC(name string, nic *model.NICInfo) {
	base := "/sys/class/net/" + name
	switch {
	case name == "lo":
		nic.Kind = "loopback"
	case dirExists(base + "/wireless"):
		nic.Kind = "wireless"
	default:
		nic.Kind = "wired"
	}
	if drv, err := os.Readlink(base + "/device/driver"); err == nil {
		nic.Driver = drv[strings.LastIndexByte(drv, '/')+1:]
	}
	if readTrim(base+"/carrier") == "1" {
		nic.Link = true
	}
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
