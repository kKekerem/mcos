//go:build linux

// Package fbvt takes ownership of the Linux virtual terminal so a program can
// draw to the framebuffer without the kernel's text console painting over it.
//
// ── Neden gerekli? ──────────────────────────────────────────────────────────
// /dev/fb0'a piksel yazmak tek başına yetmez: çekirdeğin framebuffer konsolu
// (fbcon) aynı belleğe imleç, boot mesajları ve kabuk çıktısı yazmaya devam
// eder. Sonuç, arayüzün üstünde beliren rastgele metin olur. KD_GRAPHICS kipi
// fbcon'u susturur.
//
// ── En kritik kural ─────────────────────────────────────────────────────────
// KD_GRAPHICS kipinde program çıkarsa veya çökerse KONSOL KULLANILAMAZ KALIR:
// ekran donar, kabuk görünmez. Bu yüzden Restore() her çıkış yolunda
// çağrılmalıdır — normal çıkış, sinyal ve panik dahil. Open() bunu kendisi
// kurar; çağıran yalnızca defer c.Restore() yazmalıdır.
//
// ── Devralma zinciri ────────────────────────────────────────────────────────
// Açılışta konsol iki programdan geçer: mcos-splash devralır, --keep ile
// grafik kipinde BIRAKIP çıkar (metin kipine bir an dönmek geçiş animasyonunu
// bozardı), sonra mcos-panel-fb aynı konsolu devralır. Bu yüzden Restore()
// "Open() anında ne gördüysem onu geri yazarım" diyemez: ikinci program grafik
// kipini görür ve onu geri yazarsa konsol asla metne dönmez. Restore() DAİMA
// kullanılabilir bir METİN konsolu bırakır; hedefi, ilk devralanın
// /run/mcos-vt.state dosyasına yazdığı gerçek "önceki durum"dur.
package fbvt

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Konsol ioctl numaraları (linux/kd.h).
const (
	kdGetMode  = 0x4B3B
	kdSetMode  = 0x4B3A
	kdGkbMode  = 0x4B44
	kdSkbMode  = 0x4B45
	kdTEXT     = 0x00
	kdGRAPHICS = 0x01

	// Klavye kipleri (linux/kd.h). K_UNICODE seçildi, K_OFF DEĞİL:
	// K_OFF çekirdeğin tuş çevirisini tamamen kapatır ve tuş kodlarını
	// ham okumak zorunda kalırdık — bu da Türkçe klavye düzenini (ı, ğ, ş,
	// ö, ç, ü) elle uygulamak demekti. K_UNICODE'da çekirdek yüklü konsol
	// düzenini uygular ve bize hazır UTF-8 verir.
	//
	// kXLATE de KULLANILABİLİR bir kiptir (klasik konsol): çıkışta geri
	// yüklenebilecek kipleri ayırt edebilmek için burada tanımlı.
	// K_RAW (0x00), K_MEDIUMRAW (0x02) ve K_OFF (0x04) ise kabuğun tuş
	// almasını engeller; onlara ASLA geri dönmeyiz.
	kXLATE   = 0x01
	kUNICODE = 0x03
)

// Framebuffer karartma (linux/fb.h).
const (
	fbioBlank        = 0x4611
	fbBlankUnblank   = 0
	fbBlankPowerdown = 4
)

// Console owns the VT and provides raw keyboard input.
type Console struct {
	tty *os.File
	fb  *os.File

	// prev*: Open() anındaki durumun ANLIK GÖRÜNTÜSÜ. Bu, "devralmadan
	// önceki durum" DEĞİLDİR — bizden önce başka bir MCOS programı konsolu
	// devralıp bilerek bırakmış olabilir (bkz. vtState).
	prevMode    int
	prevKbMode  int
	prevTermios unix.Termios

	haveMode    bool
	haveKbMode  bool
	haveTermios bool

	// exit: Restore()'un GERİ YAZACAĞI durum.
	exit vtState
	// chained: devralma zincirine gerçekten katıldık mı? Yalnızca o zaman
	// zinciri kapatmaya (durum dosyasını silmeye) hakkımız var — Open() daha
	// ilk ioctl'de düşerse dosya bizden ÖNCEKİ halkaya aittir ve silinirse
	// özgün termios sonsuza dek kaybolur.
	chained bool

	mu       sync.Mutex
	restored bool
	stopSig  chan struct{}
}

// vtState is the console state from before the FIRST takeover.
//
// ── Neden ayrı bir tür ve neden diskte? ─────────────────────────────────────
// Açılış zinciri iki ayrı SÜREÇTEN geçer: önce mcos-splash konsolu devralır,
// sonra --keep ile grafik kipinde BIRAKARAK çıkar, ardından mcos-panel-fb aynı
// konsolu devralır. İkinci süreç KDGETMODE ile artık KD_GRAPHICS okur; yani
// "önceki durum" bilgisi splash çıkarken süreçle birlikte kaybolur.
//
// Bu yüzden ilk devralan, devralmadan önceki gerçek durumu /run altındaki bir
// dosyaya yazar. Zincirdeki sonraki süreçler kendi anlık görüntüleri yerine bu
// dosyayı kullanır ve Restore() gerçekten METİN kipine döner.
type vtState struct {
	mode        int
	kbMode      int
	termios     unix.Termios
	haveTermios bool
}

// vtStateFile lives on tmpfs: her açılışta kendiliğinden temizlenir.
//
// Tek bir dosya kullanılıyor, tty başına değil: cihazda tek framebuffer
// konsolu var ve zincirdeki iki süreç ona FARKLI adlarla ulaşıyor (splash
// "/dev/tty2", panel "/dev/tty"). Dosyayı tty adına göre anahtarlamak, aynı
// VT'yi iki ayrı konsol sanmak demek olurdu.
var vtStateFile = "/run/mcos-vt.state"

// Open takes over the current terminal for graphics output.
//
// ttyPath boş ise /dev/tty kullanılır. fbPath boş ise /dev/fb0.
//
// Hata durumunda yapılmış DEĞİŞİKLİKLER GERİ ALINIR: yarı yapılandırılmış bir
// konsol bırakmak, hiç başlamamaktan çok daha kötüdür.
func Open(ttyPath, fbPath string) (c *Console, err error) {
	if ttyPath == "" {
		ttyPath = "/dev/tty"
	}
	if fbPath == "" {
		fbPath = "/dev/fb0"
	}

	tty, err := os.OpenFile(ttyPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("konsol açılamadı (%s): %w", ttyPath, err)
	}
	// exit'in BAŞLANGIÇ değeri güvenli olmalı: aşağıdaki adımlardan biri
	// hata verirse defer c.Restore() hemen çalışır ve o an elimizde henüz
	// okunmuş bir durum yoktur. Sıfır değer (mode=0=KD_TEXT, kbMode=0=K_RAW)
	// klavyeyi ham bırakırdı; bu yüzden açıkça kullanılabilir bir çifte
	// ayarlıyoruz.
	c = &Console{
		tty:     tty,
		stopSig: make(chan struct{}),
		exit:    vtState{mode: kdTEXT, kbMode: kUNICODE},
	}

	// Bu noktadan sonraki her hata, o ana kadarki değişiklikleri geri alır.
	defer func() {
		if err != nil {
			c.Restore()
		}
	}()

	fd := int(tty.Fd())

	// 1) Mevcut kipleri KAYDET. Geri yükleyebilmek için önce okumak şart;
	//    sabit bir "eski değer" varsaymak (ör. hep KD_TEXT) yanlış olurdu.
	if m, e := ioctlGetInt(fd, kdGetMode); e == nil {
		c.prevMode, c.haveMode = m, true
	} else {
		return nil, fmt.Errorf("konsol kipi okunamadı (bu bir sanal terminal değil?): %w", e)
	}
	if m, e := ioctlGetInt(fd, kdGkbMode); e == nil {
		c.prevKbMode, c.haveKbMode = m, true
	}

	// 2) Terminali ham kipe al: satır arabelleği ve yankı kapalı, tuşlar
	//    anında gelsin.
	t, e := unix.IoctlGetTermios(fd, unix.TCGETS)
	if e != nil {
		return nil, fmt.Errorf("termios okunamadı: %w", e)
	}
	c.prevTermios, c.haveTermios = *t, true

	// 2.5) ÇIKIŞ DURUMUNU BELİRLE — hiçbir şeyi değiştirmeden önce.
	//
	// ── Yakalanan gerçek hata ───────────────────────────────────────────
	// Eskiden Restore() doğrudan prev* anlık görüntüsünü geri yazıyordu.
	// mcos-splash --keep konsolu BİLEREK KD_GRAPHICS + ham termios olarak
	// panele devrettiği için panelin Open()'ı prevMode = KD_GRAPHICS
	// okuyordu ve panel çıkarken KDSETMODE(KD_GRAPHICS) yazıyordu; yani
	// konsol asla metin kipine dönmüyordu. Kullanıcı bunu şöyle görüyordu:
	// panelden "Yeniden başlat"ı seçince ekran son panel karesinde DONUYOR,
	// kapanış mesajları görünmüyordu; panel düzgün kapandığında ise
	// mcos-launch'ın 5 seçenekli menüsü ve "read -t 5" istemi görünmez bir
	// konsola basılıyordu — üstelik devralınan ham termios yüzünden tuşlar
	// yankılanmıyor ve Ctrl+C çalışmıyordu.
	//
	// Doğru davranış: İLK devralmadan önceki durumu kullan. O bilgi süreçler
	// arasında yaşamak zorunda olduğu için diskte tutuluyor.
	if st, ok := loadVTState(); ok {
		c.exit = st
	} else {
		c.exit = c.snapshotAsExitState()
		saveVTState(ttyPath, c.exit)
	}
	// Dosyada termios yoksa (eski sürüm ya da yazılamamış) ham termios'u
	// öylece geri vermek yerine pişmiş bir sürümünü kullan.
	if !c.exit.haveTermios && c.haveTermios {
		c.exit.termios, c.exit.haveTermios = cookedTermios(c.prevTermios), true
	}
	c.chained = true

	raw := *t
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	// VMIN=1, VTIME=0: en az bir bayt gelene kadar blokla. Zaman aşımı yok,
	// çünkü okuma ayrı bir goroutine'de yapılır.
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if e := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); e != nil {
		return nil, fmt.Errorf("termios ayarlanamadı: %w", e)
	}

	// 3) Klavyeyi UTF-8 veren kipe al.
	if c.haveKbMode {
		_ = ioctlSetInt(fd, kdSkbMode, kUNICODE)
	}

	// 4) EN SON grafik kipine geç. Bundan sonra ekranda metin görünmez, bu
	//    yüzden hata mesajları artık kullanıcıya ulaşmaz — önceki adımların
	//    hepsi bu satırdan ÖNCE bitmiş olmalı.
	if e := ioctlSetInt(fd, kdSetMode, kdGRAPHICS); e != nil {
		return nil, fmt.Errorf("grafik kipine geçilemedi: %w", e)
	}

	// Framebuffer'ı karartma için ayrıca aç (isteğe bağlı; yoksa uyku kipi
	// ekranı boyayarak taklit eder).
	if f, e := os.OpenFile(fbPath, os.O_RDWR, 0); e == nil {
		c.fb = f
	}

	c.installSignalHandler()
	return c, nil
}

// installSignalHandler restores the console on fatal signals.
//
// Bu OLMADAN: kullanıcı Ctrl+C yapar veya bir servis programı öldürür, ekran
// grafik kipinde donmuş kalır ve makineye ancak kör bir "reboot" yazarak
// erişilir. Sinyal yakalamak isteğe bağlı bir incelik değil, zorunluluktur.
func (c *Console) installSignalHandler() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		select {
		case s := <-ch:
			c.Restore()
			// Sinyali varsayılan davranışına bırakıp kendimize tekrar
			// gönderiyoruz: çıkış kodunun doğru olması için.
			signal.Reset(s.(syscall.Signal))
			_ = syscall.Kill(os.Getpid(), s.(syscall.Signal))
		case <-c.stopSig:
		}
	}()
}

// Restore hands the console back as a USABLE TEXT console.
//
// "Bulduğu gibi bırakmak" bilerek yapılmıyor: bulduğu durum, bizden önceki
// MCOS programının devrettiği grafik kipi olabilir (bkz. vtState). Geri
// yüklenen hedef Open()'da belirlenir ve asla KD_GRAPHICS / ham termios
// olamaz. Tekrar tekrar çağrılabilir ve yarım kurulmuş bir Console üzerinde
// de güvenlidir.
func (c *Console) Restore() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restored {
		return nil
	}
	c.restored = true
	close(c.stopSig)

	var errs []error
	if c.tty != nil {
		fd := int(c.tty.Fd())
		// SIRA ÖNEMLİ: önce metin kipine dön ki sonraki hatalar ekranda
		// görünebilsin.
		if e := ioctlSetInt(fd, kdSetMode, c.exit.mode); e != nil {
			errs = append(errs, fmt.Errorf("konsol kipi geri alınamadı: %w", e))
		}
		// Klavye kipi anlık görüntü alınamamış olsa bile YAZILIR: devralma
		// zincirinde bizden öncekinin bıraktığı kip ham olabilir. Hata ancak
		// kipi gerçekten okuyabildiysek raporlanır.
		if e := ioctlSetInt(fd, kdSkbMode, c.exit.kbMode); e != nil && c.haveKbMode {
			errs = append(errs, fmt.Errorf("klavye kipi geri alınamadı: %w", e))
		}
		if c.exit.haveTermios {
			if e := unix.IoctlSetTermios(fd, unix.TCSETS, &c.exit.termios); e != nil {
				errs = append(errs, fmt.Errorf("termios geri alınamadı: %w", e))
			}
		}
		_ = c.tty.Close()
	}
	// Devralma zinciri burada BİTTİ: konsol artık metin kipinde. Bir sonraki
	// Open() sıfırdan başlamalı, yoksa bu boot boyunca eski durumu taşırdı.
	// Open() yarıda kaldıysa zincire hiç katılmadık; o zaman dosya bizim
	// değildir ve DOKUNMAYIZ (bkz. Console.chained).
	if c.chained {
		clearVTState()
	}
	if c.fb != nil {
		// Uykudan çıkmadan kapanırsak ekran kapalı kalır.
		_ = c.blankFB(false)
		_ = c.fb.Close()
	}
	return errors.Join(errs...)
}

// Read returns raw bytes from the terminal (UTF-8, no line buffering).
func (c *Console) Read(p []byte) (int, error) {
	if c.tty == nil {
		return 0, errors.New("konsol açık değil")
	}
	return c.tty.Read(p)
}

// Blank turns the display off (true) or on (false).
//
// DÜRÜSTLÜK NOTU: efifb/simpledrm gibi firmware framebuffer'larında donanım
// gerçekten uyutulamaz — FBIOBLANK çoğu zaman EINVAL döner. O durumda bu
// çağrı false döner ve çağıran ekranı SİYAHA BOYAYARAK taklit etmelidir.
// Gerçek güç tasarrufu ancak yerel bir KMS sürücüsüyle mümkündür.
func (c *Console) Blank(off bool) (hardware bool) {
	if c.fb == nil {
		return false
	}
	return c.blankFB(off) == nil
}

func (c *Console) blankFB(off bool) error {
	mode := fbBlankUnblank
	if off {
		mode = fbBlankPowerdown
	}
	return ioctlSetInt(int(c.fb.Fd()), fbioBlank, mode)
}

// ── Devralma durumu (süreçler arası) ────────────────────────────────────────

// snapshotAsExitState turns this process' snapshot into a safe exit state.
//
// Anlık görüntü OLDUĞU GİBİ kullanılamaz: grafik kipi ve ham termios bir
// "önceki durum" değil, kullanılamaz bir konsoldur. Buradaki süzgeç, zincirin
// ilk halkası bile bozuk bir konsol devralsa (ör. önceki bir çalışmadan kalan
// KD_GRAPHICS) çıkışta kullanılabilir bir konsol bırakmayı garanti eder.
func (c *Console) snapshotAsExitState() vtState {
	st := vtState{mode: kdTEXT, kbMode: kUNICODE}
	if c.haveMode && c.prevMode != kdGRAPHICS {
		st.mode = c.prevMode
	}
	if c.haveKbMode && (c.prevKbMode == kXLATE || c.prevKbMode == kUNICODE) {
		st.kbMode = c.prevKbMode
	}
	if c.haveTermios {
		st.termios, st.haveTermios = cookedTermios(c.prevTermios), true
	}
	return st
}

// cookedTermios turns a raw termios back into a usable line-edited one.
//
// Zaten pişmiş bir termios'a DOKUNULMAZ: kullanıcının (veya getty'nin) kendi
// ayarlarını ezmek istemiyoruz. Yalnızca ECHO/ICANON/ISIG kapalıysa — yani
// başka bir programın ham ayarları elimizde kalmışsa — kabuğun çalışabileceği
// asgari bayrak kümesi zorlanır. Aksi hâlde kullanıcı yazdığını göremez,
// Enter satırı bitirmez ve Ctrl+C ölü kalır.
func cookedTermios(t unix.Termios) unix.Termios {
	const cooked = unix.ICANON | unix.ECHO | unix.ISIG
	if t.Lflag&cooked == cooked {
		return t
	}
	t.Iflag |= unix.BRKINT | unix.ICRNL | unix.IXON
	t.Oflag |= unix.OPOST | unix.ONLCR
	t.Lflag |= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ECHONL |
		unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8 | unix.CREAD
	// VMIN/VTIME kanonik kipte kullanılmaz ama ham kipten kalan değerler
	// başka bir programı yanıltabilir; varsayılana çekiyoruz.
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	return t
}

// saveVTState records the pre-takeover state for the next process in the chain.
//
// Hata YUTULUYOR: /run yazılamıyorsa (geliştirici makinesi, salt okunur kök)
// yine de çalışmalıyız — o durumda süreç-içi anlık görüntüye düşeriz ve
// Restore() gene de metin kipine döner, yalnızca özgün termios kaybolur.
// Açılış ekranını yazılamayan bir dosya yüzünden iptal etmek çok daha kötü
// olurdu.
func saveVTState(ttyPath string, s vtState) {
	_ = os.MkdirAll(filepath.Dir(vtStateFile), 0o755)
	_ = os.WriteFile(vtStateFile, []byte(s.encode(ttyPath)), 0o600)
}

// loadVTState reads the state written by the first process that took over.
func loadVTState() (vtState, bool) {
	data, err := os.ReadFile(vtStateFile)
	if err != nil {
		return vtState{}, false
	}
	return decodeVTState(data)
}

// clearVTState ends the handover chain.
func clearVTState() { _ = os.Remove(vtStateFile) }

// encode writes the state as plain text.
//
// İkili bir biçim yerine düz metin: cihazda hata ararken "cat
// /run/mcos-vt.state" tek başına yeterli olsun. tty satırı yalnızca teşhis
// içindir, okunurken kullanılmaz (bkz. vtStateFile).
func (s vtState) encode(ttyPath string) string {
	var b strings.Builder
	b.WriteString("# MCOS konsol devralma durumu (ilk devralmadan onceki hal)\n")
	fmt.Fprintf(&b, "tty %s\n", ttyPath)
	fmt.Fprintf(&b, "mode %d\nkb %d\n", s.mode, s.kbMode)
	if s.haveTermios {
		t := s.termios
		fmt.Fprintf(&b, "termios %x %x %x %x %x %x %x",
			t.Iflag, t.Oflag, t.Cflag, t.Lflag, t.Line, t.Ispeed, t.Ospeed)
		for _, cc := range t.Cc {
			fmt.Fprintf(&b, " %x", cc)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// decodeVTState parses the file written by encode.
//
// Bozuk/eksik dosya SESSİZCE reddedilir (ok=false) ve çağıran kendi anlık
// görüntüsüne düşer: yarım okunmuş bir durumu geri yüklemek, hiç yüklememekten
// daha tehlikelidir.
func decodeVTState(data []byte) (vtState, bool) {
	st := vtState{mode: -1, kbMode: -1}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "mode":
			if v, e := strconv.Atoi(f[1]); e == nil {
				st.mode = v
			}
		case "kb":
			if v, e := strconv.Atoi(f[1]); e == nil {
				st.kbMode = v
			}
		case "termios":
			if t, ok := decodeTermios(f[1:]); ok {
				st.termios, st.haveTermios = t, true
			}
		}
	}
	if st.mode < 0 {
		return vtState{}, false
	}
	// Dosya ne derse desin kullanılamaz bir kipe DÖNMEYİZ. KD kipleri
	// yalnızca METİN ve GRAFİK'tir; grafik kipi bir "çıkış durumu" olamaz,
	// çünkü tam da bu dosyanın çözmeye çalıştığı sorun odur.
	if st.mode != kdTEXT {
		st.mode = kdTEXT
	}
	if st.kbMode != kXLATE && st.kbMode != kUNICODE {
		st.kbMode = kUNICODE
	}
	return st, true
}

// decodeTermios reads the hex fields written by encode.
func decodeTermios(f []string) (unix.Termios, bool) {
	var t unix.Termios
	if len(f) != 7+len(t.Cc) {
		return t, false
	}
	v := make([]uint64, len(f))
	for i, s := range f {
		n, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return unix.Termios{}, false
		}
		v[i] = n
	}
	t.Iflag = uint32(v[0])
	t.Oflag = uint32(v[1])
	t.Cflag = uint32(v[2])
	t.Lflag = uint32(v[3])
	t.Line = uint8(v[4])
	t.Ispeed = uint32(v[5])
	t.Ospeed = uint32(v[6])
	for i := range t.Cc {
		t.Cc[i] = uint8(v[7+i])
	}
	// Diskten gelen bir termios da pişmiş olmak zorunda: dosyayı elle
	// bozan/eskiten bir durum konsolu kilitlemesin.
	return cookedTermios(t), true
}

// ── ioctl yardımcıları ──────────────────────────────────────────────────────

// ioctlGetInt reads an int-valued ioctl into a local variable.
func ioctlGetInt(fd int, req uint) (int, error) {
	var v int32
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req),
		uintptr(ptrOf(&v))); e != 0 {
		return 0, e
	}
	return int(v), nil
}

// ioctlSetInt passes the value BY VALUE, not by pointer.
//
// KDSETMODE ve KDSKBMODE argümanı işaretçi değil doğrudan sayı bekler; bunu
// işaretçi olarak geçmek sessizce yanlış kip kurar (veya EFAULT verir).
func ioctlSetInt(fd int, req uint, val int) error {
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req),
		uintptr(val)); e != 0 {
		return e
	}
	return nil
}
