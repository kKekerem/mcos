package flash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

// Bu dosya imajı aygıta yazar ve yazılanı DOĞRULAR.
//
// ── Neden doğrulama şart ────────────────────────────────────────────────────
// USB bellekler sessizce bozulur. Ucuz bir bellek yazmayı "başarılı" bildirip
// veriyi yanlış yazabilir; sonuç, açılmayan bir disk ve nedeni anlaşılmayan
// bir hatadır. Yazdıktan sonra geri okuyup özet karşılaştırmak bunu yakalar
// ve kullanıcıya "bellek bozuk" diyebilmemizi sağlar.

// openDeviceFn and dropCachesFn are indirections for testing.
//
// ── Neden değişken? ─────────────────────────────────────────────────────────
// Yazma yolunun tamamı (özet hesabı, doğrulama, bozuk bellek tespiti) ancak
// GERÇEK bir diske yazarak sınanabilirdi — ki bu, testin makinedeki bir
// diski silmesi demektir. Kabul edilemez.
//
// Bu iki değişken sayesinde test, bellekte yaşayan sahte bir aygıt takıp
// akışın tamamını doğrulayabiliyor: doğru bayt yazılıyor mu, geri okuma
// farklıysa yakalanıyor mu, sync çağrılıyor mu.
//
// Üretimde DEĞİŞTİRİLMEZ; yalnızca testler geçici olarak değiştirir.
var (
	openDeviceFn = openDevice
	dropCachesFn = dropCaches
)

// bufSize is the read/write chunk.
//
// 4 MiB: USB'de sistem çağrısı başına verimli, ama ilerleme çubuğu da akıcı
// kalacak kadar küçük. Daha büyük parçalar ilerlemeyi sıçramalı gösterir.
const bufSize = 4 << 20

// Writer writes an image file to a device.
type Writer struct {
	// ImagePath is the source image produced by scripts/mkpersist.sh.
	ImagePath string
	// Device is the validated target.
	Device Device
	// Verify re-reads the device and compares digests after writing.
	Verify bool
	// DryRun does everything except open the device for writing.
	//
	// GELİŞTİRME İÇİN ŞART: yazma kodunu gerçek bir diske dokunmadan
	// sınayabilmek gerekir.
	DryRun bool
}

// Write copies the image to the device, reporting progress.
//
// progress nil olabilir. Bağlam iptal edilirse yazma temiz biçimde durur —
// yarım yazılmış bir disk kalır ama kullanıcı ne olduğunu bilir.
func (w Writer) Write(ctx context.Context, progress func(Progress)) error {
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}

	// SON GÜVENLİK KAPISI. Arayüz de kontrol eder ama burada tekrar
	// bakılır: tek bir yerde unutulan kontrol, silinmiş bir disk demektir.
	if err := Validate(w.Device); err != nil {
		report(Progress{Stage: "doğrulama", Err: err, Done: true})
		return err
	}

	src, err := os.Open(w.ImagePath)
	if err != nil {
		err = fmt.Errorf("imaj açılamadı: %w", err)
		report(Progress{Stage: "imaj", Err: err, Done: true})
		return err
	}
	defer src.Close()

	st, err := src.Stat()
	if err != nil {
		return err
	}
	total := uint64(st.Size())

	if total > w.Device.SizeBytes {
		err = fmt.Errorf("imaj (%s) aygıttan (%s) büyük",
			HumanBytes(total), HumanBytes(w.Device.SizeBytes))
		report(Progress{Stage: "doğrulama", Err: err, Done: true})
		return err
	}

	if w.DryRun {
		report(Progress{Stage: "deneme (yazılmadı)", BytesDone: total,
			BytesTotal: total, Done: true})
		return nil
	}

	dst, err := openDeviceFn(w.Device.Path)
	if err != nil {
		err = fmt.Errorf("aygıt açılamadı (%s): %w", w.Device.Path, err)
		report(Progress{Stage: "aygıt", Err: err, Done: true})
		return err
	}
	defer dst.Close()

	sum, err := copyWithProgress(ctx, dst, src, total, "yazılıyor", report)
	if err != nil {
		report(Progress{Stage: "yazılıyor", Err: err, Done: true})
		return err
	}

	// Çekirdek önbelleğini diske zorla. Bu OLMADAN "tamamlandı" demek
	// yalan olur: veri hâlâ bellekte olabilir ve kullanıcı belleği
	// çıkardığında kaybolur.
	report(Progress{Stage: "diske yazılıyor (sync)", BytesDone: total, BytesTotal: total})
	if err := dst.Sync(); err != nil {
		report(Progress{Stage: "sync", Err: err, Done: true})
		return fmt.Errorf("sync başarısız: %w", err)
	}

	if !w.Verify {
		report(Progress{Stage: "tamamlandı", BytesDone: total, BytesTotal: total, Done: true})
		return nil
	}

	// ── Doğrulama ───────────────────────────────────────────────────────
	if err := dropCachesFn(dst); err != nil {
		// Önbellek boşaltılamazsa doğrulama, diskten değil BELLEKTEN okur
		// ve bozuk bir belleği "sağlam" gösterir. Bu yüzden hata bildirilir.
		report(Progress{Stage: "doğrulama", Err: err, Done: true})
		return fmt.Errorf("doğrulama için önbellek boşaltılamadı: %w", err)
	}

	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return err
	}
	readSum, err := digestN(ctx, dst, total, "doğrulanıyor", report)
	if err != nil {
		report(Progress{Stage: "doğrulanıyor", Err: err, Done: true})
		return err
	}

	if readSum != sum {
		err = fmt.Errorf("doğrulama BAŞARISIZ: yazılan veri geri okunduğunda farklı "+
			"(yazılan %s, okunan %s) — bellek bozuk olabilir",
			sum[:16], readSum[:16])
		report(Progress{Stage: "doğrulanıyor", Err: err, Done: true})
		return err
	}

	report(Progress{Stage: "tamamlandı ve doğrulandı",
		BytesDone: total, BytesTotal: total, Done: true})
	return nil
}

// copyWithProgress streams src to dst, hashing as it goes.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader,
	total uint64, stage string, report func(Progress)) (string, error) {

	h := sha256.New()
	buf := make([]byte, bufSize)
	var done uint64
	last := time.Now()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return "", fmt.Errorf("yazma hatası: %w", werr)
			}
			h.Write(buf[:n])
			done += uint64(n)

			// İlerlemeyi saniyede ~10 kez bildir: her parçada bildirmek
			// arayüzü gereksiz meşgul eder.
			if time.Since(last) > 100*time.Millisecond {
				report(Progress{Stage: stage, BytesDone: done, BytesTotal: total})
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("okuma hatası: %w", rerr)
		}
	}
	report(Progress{Stage: stage, BytesDone: done, BytesTotal: total})
	return hex.EncodeToString(h.Sum(nil)), nil
}

// digestN hashes exactly n bytes from r.
func digestN(ctx context.Context, r io.Reader, n uint64,
	stage string, report func(Progress)) (string, error) {

	h := sha256.New()
	buf := make([]byte, bufSize)
	var done uint64
	last := time.Now()

	for done < n {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		want := uint64(len(buf))
		if r := n - done; r < want {
			want = r
		}
		read, err := io.ReadFull(r, buf[:want])
		if read > 0 {
			h.Write(buf[:read])
			done += uint64(read)
			if time.Since(last) > 100*time.Millisecond {
				report(Progress{Stage: stage, BytesDone: done, BytesTotal: n})
				last = time.Now()
			}
		}
		if err != nil {
			return "", fmt.Errorf("geri okuma hatası: %w", err)
		}
	}
	report(Progress{Stage: stage, BytesDone: done, BytesTotal: n})
	return hex.EncodeToString(h.Sum(nil)), nil
}
