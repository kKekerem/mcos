// Package flash discovers removable drives and writes MCOS images to them.
//
// ════════════════════════════════════════════════════════════════════════════
// EN ÖNEMLİ TASARIM KURALI: YANLIŞ DİSKE YAZMA
// ════════════════════════════════════════════════════════════════════════════
//
// Bu paketin yaptığı iş geri alınamaz. Yanlış aygıta yazmak, kullanıcının
// sistem diskini veya yedek diskini yok eder. Bu yüzden burada güvenlik
// kolaylıktan ÖNCE gelir:
//
//  1. Sistem diski ASLA listelenmez (Linux'ta kök dosya sisteminin bulunduğu
//     disk, Windows'ta sistem sürücüsünü barındıran disk).
//  2. Çıkarılabilir olmayan aygıtlar varsayılan olarak GİZLİ. Kullanıcı
//     bilinçli olarak "dahili diskleri de göster" demedikçe görünmezler.
//  3. Bağlı bölümü olan aygıt AYRI İŞARETLENİR ve yazmadan önce ek onay
//     ister.
//  4. Yazma işlemi, hedefin boyutu ve modeli kullanıcıya tekrar gösterilip
//     onaylanmadan BAŞLAMAZ.
package flash

import (
	"errors"
	"fmt"
)

// Device is a candidate target for writing.
type Device struct {
	// Path is what the writer opens: /dev/sdb on Linux,
	// \\.\PhysicalDrive2 on Windows.
	Path string
	// Name is a short identifier shown to the user (sdb, PhysicalDrive2).
	Name string
	// Model is the vendor/product string, if the OS reports one.
	Model string
	// SizeBytes is the total capacity.
	SizeBytes uint64
	// Removable reports whether the OS flags this as removable media.
	Removable bool
	// System reports whether this disk holds the running operating system.
	//
	// Bu true ise aygıt HİÇBİR KOŞULDA yazılamaz — listelenmez bile.
	System bool
	// Mounted lists mount points currently backed by this device.
	//
	// Boş değilse yazma ek onay ister: kullanıcı o an kullandığı bir diski
	// seçmiş olabilir.
	Mounted []string
	// Bus is the transport (usb, nvme, sata, mmc…), when known.
	Bus string
}

// SizeHuman renders the capacity for display.
func (d Device) SizeHuman() string { return HumanBytes(d.SizeBytes) }

// Safe reports whether this device may be written without extra confirmation.
func (d Device) Safe() bool {
	return !d.System && d.Removable && len(d.Mounted) == 0
}

// Warnings lists reasons the user should look twice before writing.
func (d Device) Warnings() []string {
	var w []string
	if d.System {
		w = append(w, "BU SİSTEM DİSKİ — yazılamaz")
	}
	if !d.Removable {
		w = append(w, "Çıkarılabilir değil (dahili disk olabilir)")
	}
	if len(d.Mounted) > 0 {
		w = append(w, fmt.Sprintf("Şu an bağlı: %v", d.Mounted))
	}
	if d.SizeBytes > 512<<30 {
		// 512 GB üstü bir "flash bellek" büyük olasılıkla harici yedek
		// diskidir. Kullanıcıya bunu söylemek, yanlış seçimi yakalar.
		w = append(w, "512 GB'dan büyük — bu bir yedek diski olabilir")
	}
	return w
}

// MinSizeBytes is the smallest target a persistent install fits on.
//
// boot 768 MB + kök 4096 MB + veri için en az 512 MB + hizalama payı.
// Daha küçük bir aygıta yazmak, kök bölümünün taşması demektir.
const MinSizeBytes = uint64(5600) << 20

// ErrTooSmall is returned when the chosen device cannot hold the image.
var ErrTooSmall = errors.New("aygıt kalıcı kurulum için çok küçük")

// ErrSystemDisk is returned when the target holds the running OS.
var ErrSystemDisk = errors.New("hedef sistem diski — yazma reddedildi")

// Validate checks a device before any write is attempted.
//
// Bu fonksiyon yazma yolundaki SON kapıdır. Arayüz katmanı da kontrol eder
// ama burada tekrar bakılır: tek bir yerde unutulan kontrol, silinmiş bir
// disk demektir.
func Validate(d Device) error {
	if d.System {
		return fmt.Errorf("%w: %s", ErrSystemDisk, d.Path)
	}
	if d.SizeBytes < MinSizeBytes {
		return fmt.Errorf("%w: %s (%s, en az %s gerekli)",
			ErrTooSmall, d.Path, HumanBytes(d.SizeBytes), HumanBytes(MinSizeBytes))
	}
	return nil
}

// HumanBytes renders a byte count for display.
func HumanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTP"[exp])
}

// Progress reports how far a write has got.
type Progress struct {
	// Stage is a short human-readable phase name.
	Stage string
	// BytesDone and BytesTotal drive the progress bar.
	BytesDone, BytesTotal uint64
	// Err is set when the operation failed.
	Err error
	// Done is true on the final update.
	Done bool
}

// Percent returns 0..100.
func (p Progress) Percent() int {
	if p.BytesTotal == 0 {
		return 0
	}
	n := int(p.BytesDone * 100 / p.BytesTotal)
	if n > 100 {
		n = 100
	}
	return n
}

// deviceFile is the minimum a raw block device must support.
//
// ── Neden arayüz? ───────────────────────────────────────────────────────────
// Linux'ta bu bir *os.File'dır ve her şey bedava gelir. Windows'ta ise
// yazmadan önce birimleri kilitleyip ayırmak, kapatırken bölüm tablosunu
// yeniden okutmak gerekir — yani ham bir dosya tanıtıcısı YETMEZ.
//
// Arayüz, yazma kodunun (write.go) bu farkı hiç görmemesini sağlar: tek bir
// akış, iki platform.
type deviceFile interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Seek(offset int64, whence int) (int64, error)
	Sync() error
	Close() error
}
