//go:build linux

package files

// DetectUSB reports attached removable storage WITHOUT mounting anything.
//
// Panel bu bilgiyi durum döngüsünde kullanır (USB bölümünü göster/gizle), bu
// yüzden ucuz olmalı: yalnızca /sys/block ve /proc/mounts okunur.
// ScanUSBJars() ise her bölümü geçici bağlar — o pahalıdır ve yalnızca
// kullanıcı USB ekranını açtığında çağrılır.
func DetectUSB() USBInfo {
	parts, err := removablePartitions()
	if err != nil || len(parts) == 0 {
		return USBInfo{}
	}

	// Sistemin kendi bölümlerini (kök, boot, veri) sayma: onlar "takılan USB"
	// değil. Canlı sistem bir USB'den boot etmiş olabilir ve o bölümler
	// /proc/mounts'ta kritik noktalarda görünür.
	mounts := currentMounts()
	var out []string
	for _, dev := range parts {
		if mp, ok := mounts[dev]; ok && criticalMountPoints[mp] {
			continue
		}
		out = append(out, dev)
	}
	return USBInfo{Present: len(out) > 0, Partitions: len(out), Devices: out}
}
