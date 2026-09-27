// Package sysmon collects host metrics for the dashboard. Platform-specific
// details (memory, disk, /proc parsing) live in build-tagged files; this file
// holds the cross-platform pieces built on the standard library only, so the
// daemon compiles and runs on the Windows dev host as well as the Linux target.
package sysmon

import (
	"crypto/sha256"
	"encoding/hex"
	"mcos/internal/netprobe"
	"net"
	"os"
	"runtime"
	"sort"

	"mcos/internal/model"
)

// HardwareID returns a stable fingerprint of this machine derived from its
// non-loopback NIC MAC addresses. It survives reboots but changes when the
// MCOS disk/USB is moved to different hardware — that is how the first-boot
// wizard decides "same PC → panel" vs "new PC → setup". Returns "" when no
// usable MAC is found (caller treats empty as "can't tell, don't force setup").
func HardwareID() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var macs []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		mac := ifc.HardwareAddr.String()
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		macs = append(macs, mac)
	}
	if len(macs) == 0 {
		return ""
	}
	sort.Strings(macs)
	sum := sha256.Sum256([]byte("mcos-hw:" + joinStrings(macs, ",")))
	return hex.EncodeToString(sum[:8])
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

// Hostname returns the host name, or "mcos" on failure.
func Hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "mcos"
}

// CPU returns processor info. Threads is always accurate; model/cores are
// best-effort and filled by the platform-specific cpuDetails.
func CPU() model.CPUInfo {
	info := model.CPUInfo{Threads: runtime.NumCPU(), Cores: runtime.NumCPU()}
	cpuDetails(&info)
	return info
}

// Memory returns memory info (platform-specific; zero values if unavailable).
func Memory() model.MemInfo { return memory() }

// Disks returns mounted filesystems of interest (platform-specific).
func Disks() []model.DiskInfo { return disks() }

// GPUs returns detected graphics adapters (best-effort, may be empty).
func GPUs() []model.GPUInfo { return gpus() }

// Net returns network interfaces, the primary local IPv4, and an internet
// reachability probe. Built entirely on the stdlib so it is cross-platform.
func Net() model.NetStatus {
	st := model.NetStatus{Hostname: Hostname()}
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, ifc := range ifaces {
			if ifc.Flags&net.FlagLoopback != 0 {
				continue
			}
			nic := model.NICInfo{
				Name: ifc.Name,
				MAC:  ifc.HardwareAddr.String(),
				Up:   ifc.Flags&net.FlagUp != 0,
			}
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok {
					if v4 := ipn.IP.To4(); v4 != nil && !v4.IsLoopback() {
						nic.IPv4 = v4.String()
						if st.LocalIP == "" {
							st.LocalIP = v4.String()
						}
						break
					}
				}
			}
			enrichNIC(ifc.Name, &nic) // kind/driver/link (platform-specific)
			st.NICs = append(st.NICs, nic)
		}
	}
	// Tek bir ölçüm: Check() sonucu önbellekli, iki çağrı aynı şeyi döndürür.
	r := netprobe.Check()
	st.Internet = r.Online
	st.Reason = r.Reason
	return st
}

// checkInternet performs a short TCP probe to a public resolver. A failure
// means "treat the box as offline" (wan/online features are disabled).
// checkInternet delegates to netprobe.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// Burada YALNIZCA TCP PORT 53 sınanıyordu (1.1.1.1:53 ve 8.8.8.8:53). DNS
// normalde UDP kullanır; TCP/53 pek çok ISS, kurumsal ağ ve VPN tarafından
// ENGELLENİR. Sonuç: HTTPS'in sorunsuz çalıştığı ağlarda MCOS "internet yok"
// diyordu — ve o bayrak Java indirmesini KAPIDA reddediyordu, yani indirme
// hiç denenmiyordu bile.
//
// Ölçülen şey artık İŞE YARAYAN şey: dışarıya HTTPS çıkışı (bkz. netprobe).
func checkInternet() bool { return netprobe.Check().Online }

// InternetReason explains a missing connection in words the user can act on.
//
// "İnternet yok" tek başına kullanıcıya ne yapacağını söylemiyor.
func InternetReason() string { return netprobe.Check().Reason }
