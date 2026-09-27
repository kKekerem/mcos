// Command mcos-flash writes a persistent MCOS image to a USB stick or disk.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN VAR
// ════════════════════════════════════════════════════════════════════════════
//
// MCOS'u kalıcı olarak kurmanın iki yolu vardı: canlı sistemi USB'den açıp
// içeriden kurmak, ya da imajı elle `dd` ile yazmak. İlki iki adımlıdır
// (önce canlı USB, sonra kurulum), ikincisi ise tek harflik bir hatada
// sistem diskini siler.
//
// Bu program üçüncü yolu verir: masaüstünden, TEK adımda, yanlış diski
// seçmesi neredeyse imkânsız biçimde.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN SUNUCU YOK
// ════════════════════════════════════════════════════════════════════════════
//
// Önceki sürüm, 127.0.0.1'de küçük bir web sunucusu açıp tarayıcıda arayüz
// gösteriyordu. Çalışıyordu ama üç sorunu vardı:
//
//   - Tarayıcı gerekiyordu ve bazı sistemlerde varsayılan tarayıcı
//     yönetici hakkıyla açılmıyor.
//   - Güvenlik duvarı uyarısı çıkıyordu (yerel bile olsa bir port dinlemek).
//   - "Çift tıkla çalıştır" beklentisini karşılamıyordu.
//
// Artık her şey terminalde. Başlatmak için `mcos-flash.bat` (Windows) veya
// `mcos-flash.sh` (Linux) yeterli; ikisi de yönetici/kök hakkını kendisi
// ister.
//
// ════════════════════════════════════════════════════════════════════════════
// GÜVENLİK
// ════════════════════════════════════════════════════════════════════════════
//
//  1. SİSTEM DİSKİ HİÇ LİSTELENMEZ. --all-disks ile bile (internal/flash).
//  2. Yazmadan önce kullanıcı aygıt yolunu ELLE YAZAR. "e/h" onayı alışkanlık
//     hâline gelen bir tuştur; yol yazmak dikkat ister.
//  3. Yazmadan hemen önce aygıt listesi YENİDEN çekilir ve seçilen aygıtın
//     hâlâ aynı boyutta olduğu doğrulanır — kullanıcı seçim yaparken belleği
//     değiştirmiş olabilir.
//  4. --dry-run hiçbir aygıtı AÇMAZ.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mcos/internal/flash"
	"mcos/internal/version"
)

func main() {
	var (
		list     = flag.Bool("list", false, "aygıtları listele ve çık")
		allDisks = flag.Bool("all-disks", false,
			"dahili diskleri de listele (sistem diski yine gizlidir)")
		imagePath = flag.String("image", "", "yazılacak imaj dosyası")
		devPath   = flag.String("device", "", "hedef aygıt (ör. /dev/sdb)")
		yes       = flag.Bool("yes", false,
			"onay sorma (yalnızca --device ve --image ile birlikte)")
		dryRun = flag.Bool("dry-run", false,
			"hiçbir şey yazma; akışı sına")
		noVerify = flag.Bool("no-verify", false,
			"yazdıktan sonra geri okuyup doğrulama")
		imageDir = flag.String("images", "", "imajların aranacağı ek klasör")
		showVer  = flag.Bool("version", false, "sürümü yaz ve çık")
	)
	flag.Usage = usage
	flag.Parse()

	// Pencereli arayüz (gui.go): Windows'ta çift tıklamanın varsayılanı.
	if guiHook() {
		return
	}

	if *showVer {
		fmt.Println("mcos-flash " + version.Version)
		return
	}

	opts := options{
		list:     *list,
		allDisks: *allDisks,
		image:    *imagePath,
		device:   *devPath,
		yes:      *yes,
		dryRun:   *dryRun,
		verify:   !*noVerify,
		imageDir: *imageDir,
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "\nHATA: "+err.Error())
		waitOnWindows()
		os.Exit(1)
	}
	waitOnWindows()
}

func usage() {
	fmt.Fprint(os.Stderr, `mcos-flash `+version.Version+` — MCOS'u USB belleğe kurar

KULLANIM
  mcos-flash                       etkileşimli (önerilen)
  mcos-flash --list                aygıtları listele
  mcos-flash --image X --device Y  doğrudan yaz
  mcos-flash --dry-run             hiçbir şey yazmadan akışı sına

SEÇENEKLER
`)
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, `
GÜVENLİK
  Sistem diski hiçbir koşulda listelenmez. Yazmadan önce aygıt yolunu elle
  yazmanız istenir; "e/h" sorusu bilerek kullanılmaz.

YÖNETİCİ HAKKI
  Ham diske yazmak yönetici (Windows) veya kök (Linux) hakkı ister.
  mcos-flash.bat ve mcos-flash.sh bunu kendisi halleder.
`)
}

type options struct {
	list     bool
	allDisks bool
	image    string
	device   string
	yes      bool
	dryRun   bool
	verify   bool
	imageDir string
}

func run(o options) error {
	banner(o.dryRun)

	// ── Aygıtlar ────────────────────────────────────────────────────────
	devices, err := flash.Enumerate(o.allDisks)
	if err != nil {
		return err
	}
	sort.Slice(devices, func(i, j int) bool {
		// Çıkarılabilirler önce: kullanıcının aradığı neredeyse her zaman
		// onlardan biridir.
		if devices[i].Removable != devices[j].Removable {
			return devices[i].Removable
		}
		return devices[i].Path < devices[j].Path
	})

	if o.list {
		printDevices(devices, o.allDisks)
		return nil
	}
	if len(devices) == 0 {
		printDevices(devices, o.allDisks)
		return errors.New("yazılacak aygıt yok")
	}

	// ── Seçim ───────────────────────────────────────────────────────────
	dev, err := chooseDevice(devices, o)
	if err != nil {
		return err
	}
	img, err := chooseImage(o)
	if err != nil {
		return err
	}

	info, err := os.Stat(img)
	if err != nil {
		return fmt.Errorf("imaj okunamadı: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s bir klasör, imaj dosyası değil", img)
	}

	// ── Onay ────────────────────────────────────────────────────────────
	if !o.yes {
		if err := confirm(dev, img, uint64(info.Size()), o.dryRun); err != nil {
			return err
		}
	}

	// ── SON DENETİM ─────────────────────────────────────────────────────
	// Kullanıcı seçim yaparken belleği çıkarıp başkasını takmış olabilir;
	// aynı yol (/dev/sdb) artık BAŞKA bir diski gösteriyor olabilir.
	fresh, err := reverify(dev, o.allDisks)
	if err != nil {
		return err
	}
	dev = fresh

	// ── Yazma ───────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr,
			"\n\nDurduruluyor… (yarım yazılmış bir disk açılmaz — baştan yazın)")
		cancel()
	}()

	w := flash.Writer{
		ImagePath: img,
		Device:    dev,
		Verify:    o.verify && !o.dryRun,
		DryRun:    o.dryRun,
	}

	start := time.Now()
	bar := newProgress()
	err = w.Write(ctx, bar.update)
	bar.finish()
	if err != nil {
		return err
	}

	fmt.Printf("\n  Tamamlandı — %s\n", took(time.Since(start)))
	if o.dryRun {
		fmt.Println("  (DENEME kipiydi: hiçbir aygıta yazılmadı)")
		return nil
	}
	fmt.Println()
	fmt.Println("  Belleği çıkarmadan önce birkaç saniye bekleyin.")
	fmt.Println("  Açılış için BIOS/UEFI'de USB'yi ilk sıraya alın.")
	return nil
}

func banner(dry bool) {
	fmt.Println()
	fmt.Println("  ==============================================")
	fmt.Println("    MCOS Flash " + version.Version)
	fmt.Println("    Minecraft Sunucu İşletim Sistemi kurucusu")
	fmt.Println("  ==============================================")
	if dry {
		fmt.Println("  ** DENEME KİPİ — hiçbir aygıta yazılmayacak **")
	}
	fmt.Println()
}

// printDevices renders the device table.
func printDevices(devices []flash.Device, allDisks bool) {
	fmt.Println("  AYGITLAR")
	fmt.Println("  ------------------------------------------------------------")
	if len(devices) == 0 {
		fmt.Println("  Uygun aygıt bulunamadı.")
		fmt.Println()
		fmt.Println("  · USB belleği takın ve birkaç saniye bekleyin.")
		if !allDisks {
			fmt.Println("  · Dahili diskler varsayılan olarak gizlidir: --all-disks")
		}
		fmt.Println("  · Yönetici/kök hakkı gerekebilir.")
		return
	}
	for i, d := range devices {
		fmt.Printf("  %2d) %-22s %-10s %s\n",
			i+1, d.Path, flash.HumanBytes(d.SizeBytes), d.Model)
		for _, w := range d.Warnings() {
			fmt.Printf("      ! %s\n", w)
		}
	}
	fmt.Println()
}

// chooseDevice resolves --device or asks interactively.
func chooseDevice(devices []flash.Device, o options) (flash.Device, error) {
	if o.device != "" {
		for _, d := range devices {
			if strings.EqualFold(d.Path, o.device) {
				return d, nil
			}
		}
		// Aranan aygıt listede YOKSA nedenini söyle: sessiz bir "bulunamadı",
		// kullanıcının sistem diskini yazmaya çalıştığını gizler.
		return flash.Device{}, fmt.Errorf(
			"%s listede yok — sistem diski olabilir ya da çıkarılmış olabilir "+
				"(--list ile bakın)", o.device)
	}

	printDevices(devices, o.allDisks)
	if len(devices) == 1 {
		d := devices[0]
		fmt.Printf("  Tek aygıt bulundu: %s (%s)\n\n",
			d.Path, flash.HumanBytes(d.SizeBytes))
		return d, nil
	}
	n, err := askNumber("  Aygıt numarası", len(devices))
	if err != nil {
		return flash.Device{}, err
	}
	return devices[n-1], nil
}

// chooseImage resolves --image or finds candidates on disk.
func chooseImage(o options) (string, error) {
	if o.image != "" {
		return o.image, nil
	}

	dirs := flash.DefaultImageDirs()
	if o.imageDir != "" {
		dirs = append([]string{o.imageDir}, dirs...)
	}
	images := findImages(dirs)

	fmt.Println("  İMAJLAR")
	fmt.Println("  ------------------------------------------------------------")
	if len(images) == 0 {
		fmt.Println("  Hiç imaj bulunamadı.")
		fmt.Println()
		fmt.Println("  Aranan klasörler:")
		for _, d := range dirs {
			fmt.Println("    · " + d)
		}
		fmt.Println()
		fmt.Println("  Kalıcı imaj üretmek için:  sh scripts/mkpersist.sh")
		return "", errors.New("imaj yok")
	}
	for i, im := range images {
		not := ""
		if i == 0 {
			not = "  <- EN YENİ"
		} else if bayatMi(im, images[0]) {
			not = "  (ESKİ — en yenisinden " + eskilik(images[0].mod.Sub(im.mod)) + " önce)"
		}
		fmt.Printf("  %2d) %-34s %8s  %s%s\n", i+1, im.path,
			flash.HumanBytes(im.size), im.mod.Format("02.01.2006 15:04"), not)
	}
	fmt.Println()

	if len(images) == 1 {
		fmt.Printf("  Tek imaj bulundu: %s\n\n", images[0].path)
		return images[0].path, nil
	}
	n, err := askNumber("  İmaj numarası", len(images))
	if err != nil {
		return "", err
	}
	if sec := images[n-1]; n > 1 && bayatMi(sec, images[0]) {
		fmt.Println()
		fmt.Printf("  ! DİKKAT: %s, en yeni imajdan %s ESKİ.\n",
			sec.path, eskilik(images[0].mod.Sub(sec.mod)))
		fmt.Println("  ! O tarihten sonra yapılan düzeltmeler bu imajda YOK.")
		fmt.Printf("  ! En yenisi: %s\n\n", images[0].path)
	}
	return images[n-1].path, nil
}

// eskilik renders an age in the largest sensible unit.
func eskilik(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d gün", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d saat", int(d.Hours()))
	default:
		return fmt.Sprintf("%d dakika", int(d.Minutes()))
	}
}

type imageFile struct {
	path string
	size uint64
	// mod, dosyanın son değişiklik zamanı. Sıralama ve "bayat" uyarısı
	// buna dayanıyor.
	mod time.Time
}

// minImageBytes is the smallest file treated as an image.
//
// 16 MB: bunun altındaki bir .img yarım kalmış bir indirme ya da yer
// tutucudur. Listede göstermek kullanıcıyı yanlış seçime iter.
const minImageBytes = 16 << 20

// findImages looks for .img/.iso files in the given directories.
func findImages(dirs []string) []imageFile {
	seen := map[string]bool{}
	var out []imageFile
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".img" && ext != ".iso" {
				continue
			}
			p := filepath.Join(dir, e.Name())
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			if seen[abs] {
				continue
			}
			info, err := e.Info()
			if err != nil || info.Size() < minImageBytes {
				continue
			}
			seen[abs] = true
			out = append(out, imageFile{path: p, size: uint64(info.Size()), mod: info.ModTime()})
		}
	}
	// ── Yakalanan gerçek hata: BAYAT İMAJ YAZILIYORDU ──────────────────────
	//
	// Burada boyuta göre sıralanıyordu (en büyük önce). dist/ içinde ölçülen
	// hâl:
	//
	//	mcos-uefi.img     4,0 GB   10 Eylül   <- listede 1. sıra
	//	mcos-x86_64.img   605 MB    5 Haziran
	//	mcos-x86_64.iso   236 MB   17 Eylül   <- EN YENİ, ama 2. sıra
	//	m12cos-x86_64.iso 127 MB    9 Haziran
	//
	// "1"e basan kullanıcı bir hafta eski imajı yazıyordu: o arada yapılan
	// hiçbir düzeltme makinesine ulaşmıyor, her denemede "hâlâ aynı" diyordu.
	// Artık EN YENİ önce geliyor ve eskiler açıkça işaretleniyor.
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

// bayatEsik, en yeni imajdan ne kadar eski olanın "bayat" sayılacağı.
//
// Bir saat: aynı derlemede üretilen ISO ve USB imajı birkaç dakika arayla
// çıkar; onları birbirine göre bayat saymak yanlış alarm olurdu.
const bayatEsik = time.Hour

// bayatMi reports whether im is noticeably older than the newest image.
func bayatMi(im, enYeni imageFile) bool {
	return enYeni.mod.Sub(im.mod) > bayatEsik
}

// confirm requires the user to type the device path.
//
// "e/h" SORULMAZ: o soru alışkanlık hâline gelir ve kullanıcı okumadan
// onaylar. Aygıt yolunu yazmak, gerçekten hangi diski sildiğine bakmayı
// zorunlu kılar — tek gerçek koruma budur.
func confirm(d flash.Device, img string, imgSize uint64, dry bool) error {
	fmt.Println("  ------------------------------------------------------------")
	fmt.Printf("  Hedef : %s   %s   %s\n", d.Path,
		flash.HumanBytes(d.SizeBytes), d.Model)
	fmt.Printf("  İmaj  : %s   %s\n", img, flash.HumanBytes(imgSize))
	for _, w := range d.Warnings() {
		fmt.Printf("  !     : %s\n", w)
	}
	if len(d.Mounted) > 0 {
		fmt.Printf("  !     : şu an bağlı: %s\n", strings.Join(d.Mounted, ", "))
	}
	fmt.Println("  ------------------------------------------------------------")
	if dry {
		fmt.Println("  DENEME kipi: hiçbir şey yazılmayacak.")
	} else {
		fmt.Println("  BU AYGITTAKİ TÜM VERİ SİLİNECEK. Geri alınamaz.")
	}
	fmt.Println()
	fmt.Printf("  Onaylamak için aygıt yolunu yazın (%s): ", d.Path)

	line, err := readLine()
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != d.Path {
		return errors.New("onay eşleşmedi — hiçbir şey yazılmadı")
	}
	fmt.Println()
	return nil
}

// reverify re-enumerates and checks the device is still the same one.
func reverify(want flash.Device, allDisks bool) (flash.Device, error) {
	devices, err := flash.Enumerate(allDisks)
	if err != nil {
		return flash.Device{}, err
	}
	for _, d := range devices {
		if d.Path != want.Path {
			continue
		}
		if d.SizeBytes != want.SizeBytes {
			return flash.Device{}, fmt.Errorf(
				"%s değişmiş görünüyor (%s → %s) — bellek çıkarılıp başkası mı takıldı?",
				d.Path, flash.HumanBytes(want.SizeBytes),
				flash.HumanBytes(d.SizeBytes))
		}
		if err := flash.Validate(d); err != nil {
			return flash.Device{}, err
		}
		return d, nil
	}
	return flash.Device{}, fmt.Errorf(
		"%s artık listede yok — çıkarıldı mı?", want.Path)
}

// ── Terminal yardımcıları ───────────────────────────────────────────────────

var stdin = bufio.NewReader(os.Stdin)

func readLine() (string, error) {
	s, err := stdin.ReadString('\n')
	if err != nil {
		// Boru hattından çalıştırıldıysa (girdi yok) etkileşim mümkün değil.
		return "", errors.New("girdi okunamadı — etkileşimsiz çalıştırıyorsanız " +
			"--device, --image ve --yes kullanın")
	}
	return strings.TrimRight(s, "\r\n"), nil
}

func askNumber(prompt string, max int) (int, error) {
	for attempt := 0; attempt < 5; attempt++ {
		fmt.Printf("%s [1-%d]: ", prompt, max)
		line, err := readLine()
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && n >= 1 && n <= max {
			fmt.Println()
			return n, nil
		}
		fmt.Println("  Geçersiz numara.")
	}
	return 0, errors.New("geçerli bir numara girilmedi")
}

func took(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d saniye", int(d.Seconds()))
	}
	return fmt.Sprintf("%d dk %d sn", int(d.Minutes()), int(d.Seconds())%60)
}
