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
package fbvt

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
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

	prevMode    int
	prevKbMode  int
	prevTermios unix.Termios

	haveMode    bool
	haveKbMode  bool
	haveTermios bool

	mu       sync.Mutex
	restored bool
	stopSig  chan struct{}
}

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
	c = &Console{tty: tty, stopSig: make(chan struct{})}

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

// Restore puts the console back exactly as it was. Safe to call repeatedly and
// safe to call on a partially-initialised Console.
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
		if c.haveMode {
			if e := ioctlSetInt(fd, kdSetMode, c.prevMode); e != nil {
				errs = append(errs, fmt.Errorf("konsol kipi geri alınamadı: %w", e))
			}
		} else {
			// Kip okunamamış olsa bile metin kipine zorla: grafik kipinde
			// bırakmaktansa yanlış ama kullanılabilir bir kip iyidir.
			_ = ioctlSetInt(fd, kdSetMode, kdTEXT)
		}
		if c.haveKbMode {
			if e := ioctlSetInt(fd, kdSkbMode, c.prevKbMode); e != nil {
				errs = append(errs, fmt.Errorf("klavye kipi geri alınamadı: %w", e))
			}
		}
		if c.haveTermios {
			if e := unix.IoctlSetTermios(fd, unix.TCSETS, &c.prevTermios); e != nil {
				errs = append(errs, fmt.Errorf("termios geri alınamadı: %w", e))
			}
		}
		_ = c.tty.Close()
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
