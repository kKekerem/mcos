package daemon

import (
	"reflect"
	"testing"
)

// Kurulum hedef listesi (system.disks) için saf ayrıştırıcıların sınaması.
// Girdiler QEMU'da ölçülen gerçek /proc/mounts ve busybox blkid çıktılarıdır.

// TestBootDevicesVentoy: Ventoy'dan açılınca /data bir loop ve arka dosyası
// Ventoy bölümünde. Ölçülen hata: Ventoy USB'si listede görünüyordu.
func TestBootDevicesVentoy(t *testing.T) {
	mounts := `rootfs / rootfs rw,size=1418828k,nr_inodes=354707 0 0
/dev/sda1 /run/mcos/ventoy exfat rw,relatime 0 0
/dev/loop0 /data ext4 rw,relatime 0 0
`
	back := func(dev string) string {
		if dev == "/dev/loop0" {
			return "/run/mcos/ventoy/mcos/mcos-data.dat"
		}
		return ""
	}
	got := bootDevicesFromMounts(mounts, back)
	want := map[string]bool{"/dev/sda1": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestBootDevicesLoopArkaDosyaBaskaDiskte: /data'nın arka dosyası AYRI bir
// bağlama noktasındaysa o aygıt sayılır (en uzun önek), kök sayılmaz.
func TestBootDevicesLoopArkaDosyaBaskaDiskte(t *testing.T) {
	mounts := `/dev/sdb2 /mnt/x ext4 rw 0 0
/dev/sdc1 /mnt/x/alt vfat rw 0 0
/dev/loop3 /data ext4 rw 0 0
`
	got := bootDevicesFromMounts(mounts, func(string) string { return "/mnt/x/alt/mcos/veri.dat" })
	want := map[string]bool{"/dev/sdc1": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// TestBootDevicesKuruluDiskVeCanliISO: kurulu diskte kök bölümü açılış
// diskidir; canlı ISO'da (kök rootfs) /data bölümü açılış diski DEĞİLDİR —
// mcos-install o diski yerinde yeniler (ölçülen hata 2).
func TestBootDevicesKuruluDiskVeCanliISO(t *testing.T) {
	kurulu := "/dev/nvme0n1p2 / ext4 rw 0 0\n/dev/nvme0n1p3 /data ext4 rw 0 0\n"
	if got := bootDevicesFromMounts(kurulu, nil); !reflect.DeepEqual(got, map[string]bool{"/dev/nvme0n1p2": true}) {
		t.Fatalf("kurulu: %v", got)
	}
	canli := "rootfs / rootfs rw 0 0\n/dev/nvme0n1p3 /data ext4 rw,relatime 0 0\n"
	if got := bootDevicesFromMounts(canli, nil); len(got) != 0 {
		t.Fatalf("canlı ISO: /data bölümü açılış diski sayıldı: %v", got)
	}
}

// TestParseBlkidLabel: busybox blkid (argümansız) çıktısı. Eski kod
// "blkid -L" kullanıyordu ve busybox'ta hep boş dönüyordu.
func TestParseBlkidLabel(t *testing.T) {
	out := `/dev/nvme0n1p1: LABEL="MCOS-BOOT" UUID="1234-ABCD" TYPE="vfat"
/dev/nvme0n1p2: LABEL="MCOS-ROOT-3fa9c1" UUID="aa" TYPE="ext4"
/dev/nvme0n1p3: LABEL="MCOS-DATA" UUID="bb" TYPE="ext4"
/dev/sda1: LABEL="MCOS-DATA-ESKI" TYPE="ext4"
/dev/sdb3: UUID="cc" TYPE="ext4"
/dev/mmcblk0p3: LABEL="MCOS-DATA" TYPE="ext4"
`
	got := parseBlkidLabel(out, "MCOS-DATA")
	want := map[string]bool{"/dev/nvme0n1": true, "/dev/mmcblk0": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
