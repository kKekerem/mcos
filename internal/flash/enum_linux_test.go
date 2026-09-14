//go:build linux

package flash

import (
	"os"
	"path/filepath"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// SİSTEM DİSKİ HİÇBİR KOŞULDA HEDEF OLMAMALI
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// systemDisk() kökü yalnızca DÜZ bir bölümse çözebiliyordu (sda1 -> sda).
// Sıradan bir Ubuntu/Fedora/Debian kurulumu ise kökü LVM ya da LUKS üzerine
// koyar ve /proc/mounts şunu der:
//
//	/dev/mapper/ubuntu--vg-ubuntu--lv  /  ext4 ...
//
// Eski kod bu addan "mapper/ubuntu--vg-ubuntu--lv" üretiyordu. Bu ad hiçbir
// /sys/block girdisiyle eşleşmediği için "bu disk sistem diski mi?" denetimi
// HİÇBİR disk için doğru olmuyordu — makinenin açılış diski dahil. Yani
// --all-disks ile çalıştırıldığında kullanıcının Windows/Linux kurulumunu
// taşıyan disk, hiçbir uyarı olmadan silinebilir hedefler arasında
// listeleniyordu.
//
// Bu testler sahte bir sysfs ağacı kurar ve gerçek çekirdek düzenini taklit
// eder. Hiçbir aygıta dokunulmaz.

// fakeSys builds a throwaway /sys/block + /proc/mounts + /dev tree.
type fakeSys struct {
	root string
	t    *testing.T
}

func newFakeSys(t *testing.T) *fakeSys {
	t.Helper()
	f := &fakeSys{root: t.TempDir(), t: t}

	oldSys, oldMounts, oldDev := sysBlock, procMounts, devDir
	sysBlock = filepath.Join(f.root, "sys", "block")
	procMounts = filepath.Join(f.root, "mounts")
	devDir = filepath.Join(f.root, "dev")
	t.Cleanup(func() { sysBlock, procMounts, devDir = oldSys, oldMounts, oldDev })

	mk(t, sysBlock)
	mk(t, devDir)
	return f
}

func mk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	mk(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// disk adds a whole disk with the given size in 512-byte sectors.
func (f *fakeSys) disk(name string, sectors uint64, removable bool) {
	base := filepath.Join(sysBlock, name)
	mk(f.t, base)
	write(f.t, filepath.Join(base, "size"), itoa(sectors))
	rm := "0"
	if removable {
		rm = "1"
	}
	write(f.t, filepath.Join(base, "removable"), rm)
	// Bölüm de bir dizindir; parentDisk onu disk sanmamalı, o yüzden
	// bölümleri /sys/block altına DEĞİL, diskin altına koyuyoruz (gerçek
	// çekirdek düzeni budur).
}

// partition adds a partition under a disk, plus the /sys/class/block link
// path that parentDisk consults.
func (f *fakeSys) partition(disk, part string) {
	mk(f.t, filepath.Join(sysBlock, disk, part))
}

// dm adds a device-mapper node whose slaves are the given members.
func (f *fakeSys) dm(name string, slaves ...string) {
	base := filepath.Join(sysBlock, name)
	mk(f.t, base)
	write(f.t, filepath.Join(base, "size"), "1")
	write(f.t, filepath.Join(base, "removable"), "0")
	for _, sl := range slaves {
		mk(f.t, filepath.Join(base, "slaves", sl))
	}
}

// mapperLink creates /dev/mapper/<name> -> ../dm-N, like LVM does.
func (f *fakeSys) mapperLink(name, dmName string) {
	mk(f.t, filepath.Join(devDir, "mapper"))
	if err := os.Symlink(filepath.Join("..", dmName),
		filepath.Join(devDir, "mapper", name)); err != nil {
		f.t.Fatal(err)
	}
	write(f.t, filepath.Join(devDir, dmName), "")
}

func (f *fakeSys) mounts(lines string) {
	write(f.t, procMounts, lines)
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ── LVM ─────────────────────────────────────────────────────────────────────

// TestSystemDiskSeenThroughLVM — ASIL HATA.
func TestSystemDiskSeenThroughLVM(t *testing.T) {
	f := newFakeSys(t)
	f.disk("nvme0n1", 1000000, false) // açılış diski
	f.partition("nvme0n1", "nvme0n1p3")
	f.disk("sdb", 60000000, true) // USB bellek
	f.dm("dm-0", "nvme0n1p3")
	f.mapperLink("ubuntu--vg-ubuntu--lv", "dm-0")
	f.mounts("/dev/mapper/ubuntu--vg-ubuntu--lv / ext4 rw 0 0\n")

	got := systemDisks()
	if !got["nvme0n1"] {
		t.Fatalf("LVM kökünün altındaki nvme0n1 sistem diski sayılmadı: %v", got)
	}

	// Ve listede hiç görünmemeli — bayrak ne olursa olsun.
	for _, includeInternal := range []bool{false, true} {
		devs, err := Enumerate(includeInternal)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range devs {
			if d.Name == "nvme0n1" {
				t.Errorf("includeInternal=%v: SİSTEM DİSKİ listelendi (%s)",
					includeInternal, d.Path)
			}
		}
	}
}

// LUKS üzerine LVM: iki kat iner.
func TestSystemDiskSeenThroughLUKSOnLVM(t *testing.T) {
	f := newFakeSys(t)
	f.disk("sda", 1000000, false)
	f.partition("sda", "sda3")
	f.dm("dm-0", "sda3") // LUKS
	f.dm("dm-1", "dm-0") // üstünde LVM
	f.mapperLink("vg-root", "dm-1")
	f.mounts("/dev/mapper/vg-root / ext4 rw 0 0\n")

	if got := systemDisks(); !got["sda"] {
		t.Errorf("LUKS+LVM altındaki sda bulunamadı: %v", got)
	}
}

// RAID: kök birden çok diske yayılır, HEPSİ korunmalı.
func TestSystemDisksCoverEveryRAIDMember(t *testing.T) {
	f := newFakeSys(t)
	f.disk("sda", 1000000, false)
	f.disk("sdb", 1000000, false)
	f.partition("sda", "sda1")
	f.partition("sdb", "sdb1")
	f.dm("md0", "sda1", "sdb1")
	f.mounts("/dev/md0 / ext4 rw 0 0\n")

	got := systemDisks()
	for _, want := range []string{"sda", "sdb"} {
		if !got[want] {
			t.Errorf("RAID üyesi %s korunmadı: %v", want, got)
		}
	}
}

// Düz bölüm (LVM yok) hâlâ çalışmalı.
func TestSystemDiskPlainPartition(t *testing.T) {
	f := newFakeSys(t)
	f.disk("sda", 1000000, false)
	f.partition("sda", "sda1")
	f.mounts("/dev/sda1 / ext4 rw 0 0\n")

	if got := systemDisks(); !got["sda"] {
		t.Errorf("düz bölüm çözülemedi: %v", got)
	}
}

// ── EMNİYET YEDEĞİ ──────────────────────────────────────────────────────────

// Kök çözülemiyorsa hiçbir iç disk gösterilmemeli.
//
// "Bilmiyorum" ile "güvenli" aynı şey değildir: sistem diskini ayırt
// edemiyorsak, iç diskleri listelemek kullanıcıya kendi kurulumunu silme
// seçeneği sunmak demektir.
func TestUnknownSystemDiskHidesEveryInternalDisk(t *testing.T) {
	f := newFakeSys(t)
	f.disk("sda", 1000000, false)         // iç disk
	f.disk("sdb", 60000000, true)         // USB
	f.mounts("tmpfs /run tmpfs rw 0 0\n") // kök YOK

	if got := systemDisks(); len(got) != 0 {
		t.Fatalf("kök yokken sistem diski uydurdu: %v", got)
	}

	devs, err := Enumerate(true) // --all-disks
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		if !d.Removable {
			t.Errorf("sistem diski bilinmezken iç disk listelendi: %s", d.Path)
		}
	}
	// USB yine de görünmeli, yoksa araç işe yaramaz hâle gelirdi.
	found := false
	for _, d := range devs {
		if d.Name == "sdb" {
			found = true
		}
	}
	if !found {
		t.Error("çıkarılabilir aygıt da gizlendi — araç kullanılamaz olurdu")
	}
}

// ── parentDisk ──────────────────────────────────────────────────────────────

// Tam disk adı olduğu gibi dönmeli: nvme0n1 -> nvme0n1, nvme0 DEĞİL.
func TestParentDiskKeepsWholeDiskNames(t *testing.T) {
	f := newFakeSys(t)
	f.disk("nvme0n1", 100, false)
	f.disk("mmcblk0", 100, true)
	f.disk("sda", 100, false)

	for _, name := range []string{"nvme0n1", "mmcblk0", "sda"} {
		if got := parentDisk(name); got != name {
			t.Errorf("parentDisk(%q) = %q — tam disk adı bozuldu", name, got)
		}
	}
}

// Bölüm adları üst diske çözülmeli.
func TestParentDiskResolvesPartitions(t *testing.T) {
	f := newFakeSys(t)
	f.disk("nvme0n1", 100, false)
	f.disk("mmcblk0", 100, true)
	f.disk("sda", 100, false)

	cases := map[string]string{
		"nvme0n1p3": "nvme0n1",
		"mmcblk0p1": "mmcblk0",
		"sda1":      "sda",
		"sda10":     "sda",
	}
	for part, want := range cases {
		if got := parentDisk(part); got != want {
			t.Errorf("parentDisk(%q) = %q, %q bekleniyordu", part, got, want)
		}
	}
}
