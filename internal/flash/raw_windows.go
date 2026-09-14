//go:build windows

package flash

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// Bu dosya Windows'ta fiziksel diske HAM yazma yapar.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN BU KADAR UĞRAŞ GEREKİYOR
// ════════════════════════════════════════════════════════════════════════════
//
// Windows, bir diskin üzerinde BAĞLI bir birim (C:, D:…) varken o diske ham
// yazmayı REDDEDER. Vista'dan beri geçerli bu koruma, kazara dosya sistemi
// bozmayı engeller. Doğru yol, yazmadan önce her birimi:
//
//	1. KİLİTLEMEK   (FSCTL_LOCK_VOLUME)     — başka kimse yazamasın,
//	2. AYIRMAK      (FSCTL_DISMOUNT_VOLUME) — dosya sistemi sürücüsü çekilsin.
//
// Bunu yapmayan araçlar "Erişim engellendi" hatası verir ya da daha kötüsü,
// ilk birkaç sektörü yazıp gerisini sessizce atlar — açılmayan bir USB ve
// nedeni anlaşılmayan bir hata.
//
// ── Neden FILE_FLAG_NO_BUFFERING kullanmıyoruz? ─────────────────────────────
// Önbelleksiz G/Ç, hem tampon adresinin hem uzunluğun sektör boyutuna
// hizalı olmasını ister. İmaj boyutu 512'nin katı olmayabilir ve son parçayı
// doldurmak, diskte imajın sonrasına çöp yazmak demektir. Bunun yerine
// normal G/Ç + FlushFileBuffers kullanıyoruz; birim zaten ayrılmış olduğu
// için önbellek tutarsızlığı sorunu ortadan kalkıyor.

// Windows dosya sistemi denetim kodları (winioctl.h).
const (
	fsctlLockVolume     = 0x00090018
	fsctlDismountVolume = 0x00090020
	fsctlUnlockVolume   = 0x0009001C
	// IOCTL_DISK_UPDATE_PROPERTIES: yazdıktan sonra Windows'a bölüm
	// tablosunu yeniden okutur; olmazsa Gezgin eski bölümleri gösterir.
	ioctlDiskUpdateProperties = 0x00070140
)

// winDevice is an open physical drive plus the volumes we locked.
type winDevice struct {
	h       windows.Handle
	volumes []windows.Handle
	name    string
	offset  int64
}

// openDevice opens a physical drive for writing and locks its volumes.
func openDevice(path string) (deviceFile, error) {
	n, err := driveNumber(path)
	if err != nil {
		return nil, err
	}

	// ── 1. Birimleri kilitle ve ayır ────────────────────────────────────
	// Disk tanıtıcısını açmadan ÖNCE yapılır: birim sürücüsü hâlâ
	// yazarken disk tanıtıcısı açmak yarış durumu yaratır.
	var vols []windows.Handle
	for _, letter := range lettersOnDisk(n) {
		vh, err := lockVolume(letter)
		if err != nil {
			// Kilitlenemeyen birimi ZORLAMA: açık dosyası olan bir birimi
			// ayırmak veri kaybına yol açar. Kullanıcıya ne yapacağını
			// söylüyoruz.
			closeAll(vols)
			return nil, fmt.Errorf(
				"%s: birim kilitlenemedi (%v) — o sürücüdeki dosyaları/pencereleri kapatın",
				letter+":", err)
		}
		vols = append(vols, vh)
	}

	// ── 2. Diski aç ─────────────────────────────────────────────────────
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		closeAll(vols)
		return nil, err
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		closeAll(vols)
		if err == windows.ERROR_ACCESS_DENIED {
			return nil, fmt.Errorf(
				"erişim engellendi — mcos-flash'ı YÖNETİCİ olarak çalıştırın")
		}
		return nil, err
	}

	return &winDevice{h: h, volumes: vols, name: path}, nil
}

// lockVolume locks and dismounts one drive letter, returning its handle.
//
// Tanıtıcı AÇIK TUTULUR: kapatıldığı anda Windows birimi yeniden bağlar ve
// yazmanın ortasında dosya sistemi sürücüsü geri gelir.
func lockVolume(letter string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(`\\.\` + letter + `:`)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, err
	}
	var ret uint32
	if err := windows.DeviceIoControl(h, fsctlLockVolume,
		nil, 0, nil, 0, &ret, nil); err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	if err := windows.DeviceIoControl(h, fsctlDismountVolume,
		nil, 0, nil, 0, &ret, nil); err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	return h, nil
}

func closeAll(hs []windows.Handle) {
	for _, h := range hs {
		var ret uint32
		_ = windows.DeviceIoControl(h, fsctlUnlockVolume, nil, 0, nil, 0, &ret, nil)
		windows.CloseHandle(h)
	}
}

// driveNumber parses `\\.\PhysicalDriveN`.
func driveNumber(path string) (uint32, error) {
	const prefix = `\\.\PhysicalDrive`
	if !strings.HasPrefix(path, prefix) {
		return 0, fmt.Errorf("beklenmeyen aygıt yolu: %s", path)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(path, prefix))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("geçersiz aygıt numarası: %s", path)
	}
	return uint32(n), nil
}

// lettersOnDisk lists drive letters backed by a physical drive.
func lettersOnDisk(disk uint32) []string {
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
		out = append(out, letter)
	}
	return out
}

// ── deviceFile arayüzü ──────────────────────────────────────────────────────

func (d *winDevice) Write(p []byte) (int, error) {
	var done uint32
	if err := windows.WriteFile(d.h, p, &done, nil); err != nil {
		return int(done), err
	}
	d.offset += int64(done)
	if int(done) != len(p) {
		// Kısmi yazma SESSİZCE geçilmemeli: kalan baytlar diske hiç
		// ulaşmadığı halde "başarılı" görünürdü.
		return int(done), fmt.Errorf("kısmi yazma: %d/%d bayt", done, len(p))
	}
	return int(done), nil
}

func (d *winDevice) Read(p []byte) (int, error) {
	var done uint32
	if err := windows.ReadFile(d.h, p, &done, nil); err != nil {
		return int(done), err
	}
	if done == 0 {
		return 0, fmt.Errorf("okuma sonu")
	}
	d.offset += int64(done)
	return int(done), nil
}

func (d *winDevice) Seek(off int64, whence int) (int64, error) {
	// FILE_BEGIN=0, FILE_CURRENT=1, FILE_END=2 (winbase.h). io.Seek*
	// sabitleriyle AYNI değerler, bu yüzden doğrudan geçiyoruz; ama
	// geçerliliği yine de denetliyoruz, çünkü geçersiz bir yöntem
	// numarasıyla SetFilePointer beklenmedik bir konuma gider.
	if whence < 0 || whence > 2 {
		return 0, fmt.Errorf("geçersiz konumlandırma: %d", whence)
	}
	method := uint32(whence)
	hi := int32(off >> 32)
	lo, err := windows.SetFilePointer(d.h, int32(off), &hi, method)
	if err != nil {
		return 0, err
	}
	d.offset = int64(hi)<<32 | int64(uint32(lo))
	return d.offset, nil
}

func (d *winDevice) Sync() error {
	return windows.FlushFileBuffers(d.h)
}

func (d *winDevice) Close() error {
	// Bölüm tablosunu yeniden okut: olmazsa Gezgin eski bölümleri gösterir
	// ve kullanıcı "yazılmamış" sanır.
	var ret uint32
	_ = windows.DeviceIoControl(d.h, ioctlDiskUpdateProperties,
		nil, 0, nil, 0, &ret, nil)

	err := windows.CloseHandle(d.h)
	// Birimleri EN SON serbest bırak: önce bırakırsak Windows onları
	// yeniden bağlar ve disk tanıtıcısı kapanmadan yazma tamamlanmamış olur.
	closeAll(d.volumes)
	return err
}

// dropCaches has no direct equivalent on Windows.
//
// Gerek de yoktur: birimler AYRILMIŞ durumdadır, yani dosya sistemi
// önbelleği zaten devre dışıdır ve FlushFileBuffers verinin diske indiğini
// garanti eder. Doğrulama okuması aynı tanıtıcıdan yapılır ve ham sektörleri
// okur.
func dropCaches(f deviceFile) error {
	return f.Sync()
}
