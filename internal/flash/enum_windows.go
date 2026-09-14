//go:build windows

package flash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Bu dosya Windows'ta fiziksel diskleri keşfeder.
//
// ── Neden WMI değil? ────────────────────────────────────────────────────────
// WMI sorgusu (Win32_DiskDrive) yaygın bir yöntemdir ama COM başlatmayı,
// bir sorgu dilini ve bazı makinelerde onarılması gereken bozuk bir WMI
// deposunu gerektirir. DeviceIoControl doğrudan çekirdeğin söylediğidir:
// bağımlılık yok, başlatma yok, bozulacak depo yok.
//
// ── Neden PowerShell çağırmıyoruz? ──────────────────────────────────────────
// Get-Disk güzel çıktı verir ama (1) PowerShell yürütme ilkesi kapalı
// olabilir, (2) çıktı biçimi yerelleştirilmiştir (Türkçe Windows'ta sütun
// adları değişir), (3) her çağrı ~1 saniye sürer. Hiçbiri kabul edilebilir
// değil.

// Windows aygıt denetim kodları (winioctl.h).
const (
	// IOCTL_DISK_GET_LENGTH_INFO: diskin bayt cinsinden boyutu.
	//
	// DIKKAT: bu kodun erisim alani FILE_READ_ACCESS'tir. CTL_CODE
	// cozumlemesi: (0x405C >> 14) & 3 == 1. Yani sifir erisim maskesiyle
	// acilmis bir tanitici uzerinde ERROR_ACCESS_DENIED verir. Bu yuzden
	// artik yalnizca YEDEK olarak kullaniliyor.
	ioctlDiskGetLengthInfo = 0x0007405C
	// IOCTL_DISK_GET_DRIVE_GEOMETRY_EX: ayni boyut, erisim maskesi ISTEMEDEN.
	// (0x00A0 >> 14) & 3 == 0 == FILE_ANY_ACCESS.
	ioctlDiskGetDriveGeometryEx = 0x000700A0
	// IOCTL_STORAGE_QUERY_PROPERTY: üretici/model/veriyolu/çıkarılabilirlik.
	ioctlStorageQueryProperty = 0x002D1400
	// IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS: bir birimin hangi diskte olduğu.
	ioctlVolumeGetVolumeDiskExtents = 0x00560000
)

// maxPhysicalDrives is how many drive numbers are probed.
//
// 32: Windows'ta fiziksel disk numaraları 0'dan başlar ve boşluklu olabilir
// (bir USB çıkarıldığında numarası boşta kalır). 32, hiçbir ev veya küçük
// ofis makinesinde aşılmaz.
const maxPhysicalDrives = 32

// storageDeviceDescriptor mirrors STORAGE_DEVICE_DESCRIPTOR.
//
// Dizeler yapının SONUNDA, değişken uzunlukta durur; yapı yalnızca onlara
// olan uzaklıkları (offset) taşır. Bu yüzden tamponu ham bayt olarak okuyup
// uzaklıklardan okuyoruz.
type storageDeviceDescriptor struct {
	Version               uint32
	Size                  uint32
	DeviceType            byte
	DeviceTypeModifier    byte
	RemovableMedia        byte
	CommandQueueing       byte
	VendorIDOffset        uint32
	ProductIDOffset       uint32
	ProductRevisionOffset uint32
	SerialNumberOffset    uint32
	BusType               uint32
	RawPropertiesLength   uint32
	// RawDeviceProperties buradan sonra gelir.
}

// storagePropertyQuery mirrors STORAGE_PROPERTY_QUERY.
type storagePropertyQuery struct {
	PropertyID uint32
	QueryType  uint32
	Additional [1]byte
}

// diskExtent mirrors DISK_EXTENT.
type diskExtent struct {
	DiskNumber     uint32
	_              uint32 // hizalama
	StartingOffset int64
	ExtentLength   int64
}

// busTypeName maps STORAGE_BUS_TYPE to our short label.
func busTypeName(t uint32) string {
	switch t {
	case 0x07:
		return "usb"
	case 0x0B:
		return "sata"
	case 0x11:
		return "nvme"
	case 0x0C:
		return "raid"
	case 0x0D:
		return "iscsi"
	case 0x10:
		return "sd"
	case 0x08:
		return "raid"
	}
	return ""
}

// Enumerate lists candidate target devices on Windows.
//
// includeInternal false ise yalnızca çıkarılabilir aygıtlar döner. Sistem
// diski HER ZAMAN elenir — bayrak ne olursa olsun.
func Enumerate(includeInternal bool) ([]Device, error) {
	sysDisk, sysErr := systemDiskNumber()

	var out []Device
	var opened int
	// Acilabilen ama boyutu sorulamayan diskler. Bunlari ayri saymak sart:
	// aksi halde bos bir liste "USB takili degil" gibi okunur ve kullanici
	// yanlis yerde arar.
	var sizeFailed int
	for n := 0; n < maxPhysicalDrives; n++ {
		path := fmt.Sprintf(`\\.\PhysicalDrive%d`, n)
		h, err := openForQuery(path)
		if err != nil {
			continue // o numarada disk yok
		}
		opened++

		size, err := diskLength(h)
		if err != nil || size == 0 {
			windows.CloseHandle(h)
			sizeFailed++
			continue
		}
		model, bus, removable := deviceInfo(h)
		windows.CloseHandle(h)

		d := Device{
			Path:      path,
			Name:      fmt.Sprintf("PhysicalDrive%d", n),
			SizeBytes: size,
			Removable: removable,
			Model:     model,
			Bus:       bus,
			System:    sysErr == nil && uint32(n) == sysDisk,
			Mounted:   volumesOnDisk(uint32(n)),
		}
		// USB üzerindeki diskler "removable=0" bildirebilir (özellikle SSD
		// kutuları). Veriyolu USB ise onu çıkarılabilir saymak, gerçek
		// kullanımı yansıtır.
		if d.Bus == "usb" {
			d.Removable = true
		}

		// SİSTEM DİSKİ HİÇ LİSTELENMEZ. Kullanıcıya seçenek olarak bile
		// sunulmamalı: tek bir yanlış tuş makineyi yok eder.
		if d.System {
			continue
		}
		if !includeInternal && !d.Removable {
			continue
		}
		out = append(out, d)
	}

	// Hiçbir diski AÇAMADIYSAK bu bir yetki sorunudur ve kullanıcıya
	// söylenmelidir: boş bir liste "USB takılı değil" gibi okunur ve
	// kullanıcı yanlış yerde arar.
	if opened == 0 {
		return nil, fmt.Errorf(
			"hiçbir fiziksel disk açılamadı — uygulamayı yönetici olarak çalıştırın")
	}
	if len(out) == 0 && sizeFailed > 0 {
		return nil, fmt.Errorf(
			"%d disk açıldı ama boyutu okunamadı — uygulamayı yönetici olarak çalıştırın",
			sizeFailed)
	}
	if sysErr != nil {
		// Sistem diski bulunamadıysa GÜVENLİ tarafta kal: hiçbir dahili
		// diski listeleme. Yanlışlıkla Windows'un kurulu olduğu diski
		// göstermektense hiçbir şey göstermemek yeğdir.
		filtered := out[:0]
		for _, d := range out {
			if d.Removable {
				filtered = append(filtered, d)
			}
		}
		out = filtered
	}
	return out, nil
}

// openForQuery opens a device read-only for metadata queries.
//
// GENERIC_READ bile İSTENMEZ: yalnızca öznitelik sorgusu yapacağız ve
// sıfır erişim maskesiyle açmak, disk kilitli/kullanımdayken bile çalışır.
func openForQuery(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p,
		0, // yalnızca öznitelik erişimi
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
}

// diskGeometryEx mirrors DISK_GEOMETRY_EX (winioctl.h).
//
// DiskSize alani 24. bayttadir: DISK_GEOMETRY 24 bayttir (Cylinders 8 +
// dort adet 4 baytlik alan) ve 24, 8'in kati oldugu icin dolgu eklenmez.
type diskGeometryEx struct {
	Cylinders         int64
	MediaType         uint32
	TracksPerCylinder uint32
	SectorsPerTrack   uint32
	BytesPerSector    uint32
	DiskSize          int64
	// Data[1] ve olasi dolgu: cekirdegin yazacagi tampon buyuk olmali.
	_ [16]byte
}

// diskLength asks the kernel for the device size in bytes.
//
// -- Yakalanan gercek hata ---------------------------------------------------
// Burasi eskiden yalnizca IOCTL_DISK_GET_LENGTH_INFO kullaniyordu. O kodun
// erisim alani FILE_READ_ACCESS'tir, oysa openForQuery tanitiyi BILEREK sifir
// erisim maskesiyle aciyor (kilitli diskler de sorgulanabilsin diye). Sonuc:
// cagri HER diskte ERROR_ACCESS_DENIED veriyordu, Enumerate her aygiti
// atliyordu ve liste HER ZAMAN bos donuyordu.
//
// Hata gorunmuyordu, cunku CreateFile sifir erisimle basarili oluyor: yani
// "opened" sayaci artiyor, o yuzden "yonetici olarak calistirin" uyarisi da
// hic tetiklenmiyordu. Kullanici her makinede "Uygun aygit bulunamadi"
// goruyordu, USB takili olsa bile.
//
// GET_DRIVE_GEOMETRY_EX ayni bilgiyi FILE_ANY_ACCESS ile verir.
func diskLength(h windows.Handle) (uint64, error) {
	var g diskGeometryEx
	var ret uint32
	err := windows.DeviceIoControl(h, ioctlDiskGetDriveGeometryEx,
		nil, 0,
		(*byte)(unsafe.Pointer(&g)), uint32(unsafe.Sizeof(g)),
		&ret, nil)
	if err == nil && g.DiskSize > 0 {
		return uint64(g.DiskSize), nil
	}

	// Yedek: okuma izniyle acilmis bir taniticida bu da calisir.
	var length int64
	err = windows.DeviceIoControl(h, ioctlDiskGetLengthInfo,
		nil, 0,
		(*byte)(unsafe.Pointer(&length)), uint32(unsafe.Sizeof(length)),
		&ret, nil)
	if err != nil {
		return 0, err
	}
	if length < 0 {
		return 0, fmt.Errorf("geçersiz disk boyutu")
	}
	return uint64(length), nil
}

// deviceInfo reads the model string, bus type and removability.
func deviceInfo(h windows.Handle) (model, bus string, removable bool) {
	q := storagePropertyQuery{PropertyID: 0, QueryType: 0} // StorageDeviceProperty
	buf := make([]byte, 1024)
	var ret uint32

	err := windows.DeviceIoControl(h, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&q)), uint32(unsafe.Sizeof(q)),
		&buf[0], uint32(len(buf)), &ret, nil)
	if err != nil || ret < uint32(unsafe.Sizeof(storageDeviceDescriptor{})) {
		return "", "", false
	}

	d := (*storageDeviceDescriptor)(unsafe.Pointer(&buf[0]))
	removable = d.RemovableMedia != 0
	bus = busTypeName(d.BusType)

	vendor := cstringAt(buf, d.VendorIDOffset)
	product := cstringAt(buf, d.ProductIDOffset)
	parts := []string{}
	if vendor != "" {
		parts = append(parts, vendor)
	}
	if product != "" {
		parts = append(parts, product)
	}
	return strings.Join(parts, " "), bus, removable
}

// cstringAt reads a NUL-terminated ASCII string at a byte offset.
func cstringAt(buf []byte, off uint32) string {
	if off == 0 || int(off) >= len(buf) {
		return ""
	}
	s := buf[off:]
	for i, c := range s {
		if c == 0 {
			return strings.TrimSpace(string(s[:i]))
		}
	}
	return strings.TrimSpace(string(s))
}

// systemDiskNumber returns the physical drive number holding Windows.
func systemDiskNumber() (uint32, error) {
	win, err := windows.GetSystemWindowsDirectory()
	if err != nil || len(win) < 2 || win[1] != ':' {
		return 0, fmt.Errorf("Windows dizini bulunamadı")
	}
	return diskNumberForVolume(string(win[0]))
}

// diskNumberForVolume maps a drive letter to its physical drive number.
func diskNumberForVolume(letter string) (uint32, error) {
	path := `\\.\` + letter + `:`
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)

	// VOLUME_DISK_EXTENTS: bir uint32 sayı + hizalama + N adet DISK_EXTENT.
	// Yayılmış (spanned) birimler için birden çok extent olabilir; ilki
	// bizim için yeterli, çünkü herhangi birinde olması o diski sistem
	// diski yapar.
	buf := make([]byte, 4+4+16*int(unsafe.Sizeof(diskExtent{})))
	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlVolumeGetVolumeDiskExtents,
		nil, 0, &buf[0], uint32(len(buf)), &ret, nil); err != nil {
		return 0, err
	}
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	if count == 0 {
		return 0, fmt.Errorf("birim hiçbir diske ait değil")
	}
	ext := (*diskExtent)(unsafe.Pointer(&buf[8]))
	return ext.DiskNumber, nil
}

// volumesOnDisk lists mounted drive letters backed by a physical drive.
//
// Bağlı bir diski yazmak, o an kullanılan bir dosya sistemini yok etmek
// demektir. Kullanıcı bunu görmeli.
func volumesOnDisk(disk uint32) []string {
	var out []string
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A' + i))
		n, err := diskNumberForVolume(letter)
		if err != nil || n != disk {
			continue
		}
		out = append(out, letter+":\\")
	}
	return out
}

// DefaultImageDirs returns where mcos-flash looks for images on Windows.
//
// Uygulamanın YANINDAKİ klasör önce gelir: kullanıcı imajı ikilinin yanına
// koyup çift tıklayabilmeli, bir yol yazmak zorunda kalmamalı.
func DefaultImageDirs() []string {
	dirs := []string{".", "dist", "images"}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		dirs = append([]string{base, filepath.Join(base, "images")}, dirs...)
	}
	return dirs
}
