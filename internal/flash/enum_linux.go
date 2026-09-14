//go:build linux

package flash

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Bu dosya Linux'ta blok aygıtlarını /sys üzerinden keşfeder.
//
// Neden lsblk çağırmıyoruz: lsblk her dağıtımda yok, sürüm sürüm çıktı
// biçimi değişiyor ve JSON desteği eski sürümlerde bulunmuyor. /sys doğrudan
// çekirdeğin söylediğidir ve biçimi sabittir.

// sysBlock is where the kernel exposes block devices.
//
// DEGISKEN, sabit degil: bu paketin en yuksek riskli karari "hangi disk
// sistem diski" sorusudur ve onu GERCEKTEN sinayabilmek icin sahte bir sysfs
// agacina yonlendirebilmek gerekiyor. Uretimde hicbir yerde degistirilmez.
var sysBlock = "/sys/block"

// procMounts is where the kernel lists mounted filesystems.
var procMounts = "/proc/mounts"

// devDir is the device directory, used to resolve /dev/mapper symlinks.
var devDir = "/dev"

// Enumerate lists candidate target devices.
//
// includeInternal false ise yalnızca çıkarılabilir aygıtlar döner. Sistem
// diski HER ZAMAN elenir — bayrak ne olursa olsun.
func Enumerate(includeInternal bool) ([]Device, error) {
	entries, err := os.ReadDir(sysBlock)
	if err != nil {
		return nil, err
	}

	// Sistem diski bir KUMEdir, tek ad degil: LVM, LUKS ve md (RAID) kokleri
	// birden cok fiziksel diske yayilabilir ve hepsi korunmalidir.
	sysDisks := systemDisks()
	mounts := mountsByDevice()

	// -- EMNIYET YEDEGI --------------------------------------------------
	// Kok aygiti cozulemediyse hangi diskin sistem diski oldugunu BILMIYORUZ.
	// O durumda ic diskleri hic gostermemek tek guvenli davranistir:
	// "bilmiyorum" ile "guvenli" ayni sey degildir.
	//
	// (Windows tarafi bunu bastan boyle yapiyordu: enum_windows.go, sistem
	// disk numarasi bulunamazsa yalnizca cikarilabilir aygitlari listeler.
	// Linux tarafinda bu yedek yoktu.)
	unknownSystem := len(sysDisks) == 0

	var out []Device
	for _, e := range entries {
		name := e.Name()

		// Sanal ve döngü aygıtlarını ele: bunlar hiçbir zaman hedef değil.
		switch {
		case strings.HasPrefix(name, "loop"),
			strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "dm-"),
			strings.HasPrefix(name, "md"),
			strings.HasPrefix(name, "zram"),
			strings.HasPrefix(name, "sr"): // optik sürücü
			continue
		}

		base := filepath.Join(sysBlock, name)

		// Boyut 512 baytlık sektör sayısıdır.
		sectors := readUint(filepath.Join(base, "size"))
		if sectors == 0 {
			continue // ortam yok (boş kart okuyucu)
		}
		size := sectors * 512

		d := Device{
			Path:      "/dev/" + name,
			Name:      name,
			SizeBytes: size,
			Removable: readUint(filepath.Join(base, "removable")) == 1,
			Model:     deviceModel(base),
			Bus:       deviceBus(base, name),
			System:    sysDisks[name],
			Mounted:   mounts[name],
		}

		// USB üzerindeki diskler "removable=0" bildirebilir (özellikle SSD
		// kutuları ve bazı bellekler). Veriyolu USB ise onu çıkarılabilir
		// saymak, gerçek kullanımı yansıtır.
		if d.Bus == "usb" {
			d.Removable = true
		}

		// SİSTEM DİSKİ HİÇ LİSTELENMEZ. Kullanıcıya seçenek olarak bile
		// sunulmamalı: tek bir yanlış tık makineyi yok eder.
		if d.System {
			continue
		}
		if (!includeInternal || unknownSystem) && !d.Removable {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// systemDisks returns the kernel names of every disk that backs "/".
//
// -- Yakalanan gercek hata ---------------------------------------------------
// Eskiden bu tek bir ad dondururdu ve koku yalnizca DUZ bir bolumse
// cozebiliyordu (sda1 -> sda). Oysa siradan bir Ubuntu/Fedora/Debian kurulumu
// koku LVM ya da LUKS uzerine koyar; /proc/mounts orada sunu der:
//
//	/dev/mapper/ubuntu--vg-ubuntu--lv  /  ext4 ...
//
// Eski kod "mapper/ubuntu--vg-ubuntu--lv" adini donduruyordu. Bu ad hicbir
// /sys/block girdisiyle eslesmez, dolayisiyla "System: name == sysDisk"
// denetimi HICBIR disk icin dogru olmuyordu -- sistem diski dahil. Sonuc:
// --all-disks ile calistirildiginda makinenin acilis diski, uzerinde hicbir
// uyari olmadan, silinebilir hedefler arasinda listeleniyordu.
//
// Artik kok aygit /dev/mapper/... ise once sembolik bag cozuluyor (dm-N),
// sonra /sys/block/dm-N/slaves altindan fiziksel uyelere iniliyor. LVMin
// LUKS uzerine kuruldugu durumlar icin ozyineleme var; RAID icin de ayni yol
// calisir (md0in slaves girdileri sda1, sdb1dir), bu yuzden kume donuyor.
func systemDisks() map[string]bool {
	out := map[string]bool{}
	root := rootDevice()
	if root == "" {
		return out
	}
	collectBackingDisks(root, out, 0)
	return out
}

// rootDevice returns the kernel name of the device mounted at "/".
func rootDevice() string {
	f, err := os.Open(procMounts)
	if err != nil {
		return ""
	}
	defer f.Close()

	var rootDev string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[1] == "/" {
			rootDev = fields[0]
			break
		}
	}
	return kernelName(rootDev)
}

// kernelName turns a /dev path into the name the kernel uses in /sys/block.
//
// /dev/mapper/... ve /dev/disk/by-uuid/... birer SEMBOLIK BAGDIR; cozulmeden
// /sys altinda karsiliklari yoktur.
func kernelName(devPath string) string {
	if !strings.HasPrefix(devPath, "/dev/") {
		return ""
	}
	rel := strings.TrimPrefix(devPath, "/dev/")
	if resolved, err := filepath.EvalSymlinks(filepath.Join(devDir, rel)); err == nil {
		if r, err := filepath.Rel(devDir, resolved); err == nil {
			return r
		}
	}
	return rel
}

// collectBackingDisks walks dm/md slaves down to the physical disks.
//
// depth: LVM-on-LUKS iki kat iner; sinir, bozuk bir sysfste sonsuz
// ozyinelemeyi engellemek icin var.
func collectBackingDisks(name string, out map[string]bool, depth int) {
	if name == "" || depth > 8 || out[name] {
		return
	}
	// Sanal aygitin altindaki gercek uyeler.
	slaves := filepath.Join(sysBlock, name, "slaves")
	if entries, err := os.ReadDir(slaves); err == nil && len(entries) > 0 {
		for _, e := range entries {
			collectBackingDisks(e.Name(), out, depth+1)
		}
		return
	}
	// Bolum ya da diskin kendisi.
	if disk := parentDisk(name); disk != "" {
		out[disk] = true
	}
}

// parentDisk maps a partition name to its whole-disk name.
//
// Ad ZATEN bir tam diskse oldugu gibi doner. Bu denetim sart: nvme0n1 icin
// sysfs bagi ".../nvme/nvme0/nvme0n1" bicimindedir ve bir ust dizin "nvme0",
// yani DENETLEYICIdir, disk degil. Denetim olmadan nvme0n1 -> nvme0 olurdu
// ve hicbir /sys/block girdisiyle eslesmezdi.
func parentDisk(part string) string {
	if part == "" {
		return ""
	}
	if st, err := os.Stat(filepath.Join(sysBlock, part)); err == nil && st.IsDir() {
		return part
	}
	// Önce /sys'e sor: en güvenilir yol, ad kalıbı tahmin etmeye gerek yok.
	if link, err := os.Readlink(filepath.Join("/sys/class/block", part)); err == nil {
		// .../block/sda/sda1 biçiminde; bir üst dizin disk adıdır.
		parts := strings.Split(filepath.Clean(link), "/")
		if len(parts) >= 2 {
			cand := parts[len(parts)-2]
			if cand != "block" && cand != "" {
				return cand
			}
		}
	}
	// Yedek: ad kalıbından türet.
	// nvme0n1p3 -> nvme0n1, mmcblk0p1 -> mmcblk0, sda1 -> sda
	if i := strings.LastIndex(part, "p"); i > 0 && isAllDigits(part[i+1:]) {
		if c := part[i-1]; c >= '0' && c <= '9' {
			return part[:i]
		}
	}
	return strings.TrimRight(part, "0123456789")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mountsByDevice maps a whole-disk name to the mount points it backs.
//
// Bağlı bir diski yazmak, o an kullanılan bir dosya sistemini yok etmek
// demektir. Kullanıcı bunu görmeli.
func mountsByDevice() map[string][]string {
	out := map[string][]string{}
	f, err := os.Open(procMounts)
	if err != nil {
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		// Ayni cozumleme: LVM/LUKS uzerindeki bir baglama noktasi da altindaki
		// FIZIKSEL diske yazilmali, yoksa "bagli" uyarisi hic gorunmez.
		backing := map[string]bool{}
		collectBackingDisks(kernelName(fields[0]), backing, 0)
		mp := unescapeMount(fields[1])
		for disk := range backing {
			dup := false
			for _, existing := range out[disk] {
				if existing == mp {
					dup = true
					break
				}
			}
			if !dup {
				out[disk] = append(out[disk], mp)
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
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// deviceModel reads the vendor/product strings the kernel exposes.
func deviceModel(base string) string {
	var parts []string
	for _, f := range []string{"device/vendor", "device/model"} {
		if b, err := os.ReadFile(filepath.Join(base, f)); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				parts = append(parts, s)
			}
		}
	}
	if len(parts) == 0 {
		// NVMe modeli farklı yerde durur.
		if b, err := os.ReadFile(filepath.Join(base, "device/device/model")); err == nil {
			return strings.TrimSpace(string(b))
		}
		return ""
	}
	return strings.Join(parts, " ")
}

// deviceBus reports the transport by walking the sysfs device link.
func deviceBus(base, name string) string {
	link, err := os.Readlink(filepath.Join(base, "device"))
	if err != nil {
		// nvme0n1 gibi aygıtlarda "device" yoksa ada bak.
		if strings.HasPrefix(name, "nvme") {
			return "nvme"
		}
		if strings.HasPrefix(name, "mmcblk") {
			return "mmc"
		}
		return ""
	}
	switch {
	case strings.Contains(link, "usb"):
		return "usb"
	case strings.Contains(link, "nvme"):
		return "nvme"
	case strings.Contains(link, "mmc"):
		return "mmc"
	case strings.Contains(link, "ata"):
		return "sata"
	}
	return ""
}

func readUint(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
