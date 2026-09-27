//go:build linux

package sound

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// probe picks the best available output.
//
// SIRA ÖNEMLİ: gerçek ses kartı > anakart bipçisi > sessizlik. Bipçiyi önce
// denemek, hoparlörü olan bir makinede cılız bir "bip" çalmak demekti.
func probe() backend {
	if a := probeALSA(); a != nil {
		return a
	}
	if b := probeBeep(); b != nil {
		return b
	}
	return nil
}

// ── ALSA (aplay) ────────────────────────────────────────────────────────────

type alsaBackend struct {
	bin string
	// mixerOnce, Master kanalını bir kez açar.
	mixerOnce sync.Once
	// device, çalışan ilk PCM aygıt adı. İlk başarılı çalmadan sonra
	// sabitlenir; her seste yeniden denemek gereksiz süreç başlatmaktır.
	mu     sync.Mutex
	device string
}

// alsaDevices are tried in order until one plays.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// İlk sürüm yalnızca varsayılan aygıtı kullanıyordu (aplay'e -D vermemek
// "default" demektir). QEMU'da uçtan uca ölçümde tek bir örnek bile
// çalınmadı; mcos-soundcheck sebebi gösterdi:
//
//	ALSA lib pcm_direct.c:2188:(_snd_pcm_direct_new)
//	    unable to create IPC semaphore
//	aplay: main:850: audio open error: Function not implemented
//
// alsa-lib'in "default" aygıtı dmix'tir (yazılım karıştırıcı) ve süreçler
// arası kilit için System V semaforu açar. Çekirdekte CONFIG_SYSVIPC kapalıysa
// semget() ENOSYS döner ve ses kartı çalışır durumdayken bile HİÇBİR ŞEY
// duyulmaz.
//
// Çekirdekte SYSVIPC artık açık, ama tek savunma ona bırakılmadı: MCOS başka
// bir çekirdekle de çalıştırılabilir (kullanıcı kendi imajını üretebilir) ve
// bu liste dmix'i tamamen atlayan bir yola düşmeyi garanti eder.
//
//	default     : normal yol (dmix, birden çok sürecin sesi karışabilir)
//	plughw:0,0  : dmix YOK, ama biçim/oran dönüşümü var
//	hw:0,0      : ham donanım; kart 44100/S16_LE/mono kabul etmezse başarısız
var alsaDevices = []string{"default", "plughw:0,0", "hw:0,0"}

func (a *alsaBackend) name() string { return "alsa" }

// probeALSA requires BOTH a playback device and the aplay binary.
//
// İkisini de denetlemek şart: aplay var ama /dev/snd boşsa her çalma
// "no soundcards found" ile başarısız olur ve her seferinde bir süreç
// başlatmış oluruz.
func probeALSA() backend {
	if !hasPlaybackDevice() {
		return nil
	}
	bin, err := exec.LookPath("aplay")
	if err != nil {
		return nil
	}
	return &alsaBackend{bin: bin}
}

// hasPlaybackDevice reports whether /dev/snd has a playback PCM node.
func hasPlaybackDevice() bool {
	matches, err := filepath.Glob("/dev/snd/pcmC*D*p")
	return err == nil && len(matches) > 0
}

// unmute raises the Master control once.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// HDA kodeklerinin çoğu açılışta Master KAPALI gelir. O hâlde her şey doğru
// kurulur, aplay 0 ile döner ve hiçbir şey duyulmaz. Teşhisi en zor ses
// arızası budur; bir kerelik amixer çağrısı onu tamamen ortadan kaldırır.
//
// Hatalar yok sayılır: "Master" kontrolü olmayan kartlar var (ör. bazı USB
// aygıtlarında yalnızca "PCM"), ve orada ses zaten açıktır.
func (a *alsaBackend) unmute() {
	a.mixerOnce.Do(func() {
		amixer, err := exec.LookPath("amixer")
		if err != nil {
			return
		}
		for _, ctl := range []string{"Master", "PCM", "Speaker", "Headphone"} {
			cmd := exec.Command(amixer, "-q", "sset", ctl, "70%", "unmute")
			cmd.Stdout, cmd.Stderr = nil, nil
			_ = cmd.Run()
		}
	})
}

// play pipes raw PCM into aplay, trying each device until one works.
func (a *alsaBackend) play(_ Effect, pcm []int16, rate int) error {
	a.unmute()

	a.mu.Lock()
	known := a.device
	a.mu.Unlock()

	devices := alsaDevices
	if known != "" {
		devices = []string{known} // çalışan aygıt bulundu; onu kullan
	}

	var lastErr error
	for _, dev := range devices {
		err := a.playOn(dev, pcm, rate)
		if err == nil {
			a.mu.Lock()
			a.device = dev
			a.mu.Unlock()
			return nil
		}
		lastErr = err
	}
	// Bilinen aygıt artık çalışmıyorsa (USB kart çıkarıldı) listeyi
	// baştan denemek için sıfırla.
	a.mu.Lock()
	a.device = ""
	a.mu.Unlock()
	return lastErr
}

// playOn runs one aplay attempt against a named device.
func (a *alsaBackend) playOn(device string, pcm []int16, rate int) error {
	cmd := exec.Command(a.bin,
		"-q",
		"-D", device,
		"-t", "raw",
		"-f", "S16_LE",
		"-c", "1",
		"-r", strconv.Itoa(rate),
		"-") // stdin

	cmd.Stdin = bytes.NewReader(pcmBytes(pcm))
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return err
	}

	// ZAMAN AŞIMI ŞART: aplay, aygıtı başka bir süreç tutuyorsa (ya da
	// sürücü askıya alınmışsa) SONSUZA KADAR bekleyebilir. Panelin ses
	// goroutine'i tek; orada asılmak, bütün sesleri kalıcı olarak
	// susturmak demekti.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Sesin kendi süresi + cömert bir pay.
	limit := time.Duration(len(pcm))*time.Second/time.Duration(rate) + 2*time.Second
	select {
	case err := <-done:
		if err != nil {
			return errors.New("aplay(" + device + "): " + err.Error() + " " +
				strings.TrimSpace(errBuf.String()))
		}
		return nil
	case <-time.After(limit):
		_ = cmd.Process.Kill()
		<-done
		return errors.New("aplay(" + device + ") zaman aşımına uğradı")
	}
}

// ── PC hoparlörü (evdev EV_SND / SND_TONE) ──────────────────────────────────

type beepBackend struct{ path string }

func (b *beepBackend) name() string { return beepName }

const (
	evSnd    = 0x12 // EV_SND
	sndTone  = 0x02 // SND_TONE
	evBitSnd = 1 << evSnd
)

// probeBeep finds an input device that can emit tones.
//
// Aygıt ADINA bakmıyoruz (fbinput'taki kural burada da geçerli: üretici adı
// güvenilmez); çekirdeğe "bu aygıt ses üretebiliyor mu" diye soruyoruz.
func probeBeep() backend {
	entries, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return nil
	}
	for _, path := range entries {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		var bits uint32
		// EVIOCGBIT(0, sizeof(bits)): aygıtın desteklediği olay türleri.
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
			uintptr(eviocgbit(0, 4)), uintptr(unsafe.Pointer(&bits)))
		f.Close()
		if errno != 0 {
			continue
		}
		if bits&evBitSnd != 0 {
			return &beepBackend{path: path}
		}
	}
	return nil
}

// inputEvent mirrors struct input_event (64-bit time fields on amd64).
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

// play approximates the effect with the motherboard beeper.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// Burada sabit 880 Hz yazılıyordu, çünkü arka uç arayüzü yalnızca PCM
// taşıyordu ve PCM'den notaları geri çıkarmak mümkün değil. Sonuç ölçüldü:
//
//	nav      880 Hz / 45 ms
//	open     880 Hz / 165 ms   ┐ ikisi
//	close    880 Hz / 165 ms   ┘ BİT BİT AYNI
//	confirm  880 Hz / 200 ms   ┐
//	error    880 Hz / 200 ms   │ dördü
//	boot     880 Hz / 200 ms   │ BİT BİT AYNI
//	shutdown 880 Hz / 200 ms   ┘
//
// Yani yedi ayrı efekt üç sese iniyordu ve kullanıcı haklı olarak "hepsi bip"
// diyordu. Bipçi donanımı suçsuz: PIT çözünürlüğü 440-1568 Hz aralığında
// 1,2 cent'ten iyi (ölçüldü), yani ayrı notalar tamamen mümkün.
//
// Artık efektin KENDİ nota dizisi çalınıyor: aynı tablo (effects), aynı
// frekanslar, aynı süreler. Tek fark genlik denetiminin olmaması — bipçide
// gain diye bir şey yok.
//
// Not: birçok modern dizüstünde fiziksel bipçi YOKTUR; o makinelerde bu arka
// uç hiç bulunmaz ve sessizliğe düşülür.
func (b *beepBackend) play(e Effect, pcm []int16, rate int) error {
	f, err := os.OpenFile(b.path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	tones := effects[e]
	if len(tones) == 0 {
		// Bilinmeyen efekt: PCM süresi kadar tek bip. Eski davranış, ama
		// artık yalnızca gerçekten bilinmeyen bir efekt için.
		dur := time.Duration(len(pcm)) * time.Second / time.Duration(rate)
		if dur > 200*time.Millisecond {
			dur = 200 * time.Millisecond
		}
		if err := writeTone(f, 880); err != nil {
			return err
		}
		time.Sleep(dur)
		return writeTone(f, 0)
	}

	// Notaları BAŞLANGIÇ ZAMANINA göre sırayla çal. Efekt tablosundaki
	// notalar üst üste binebiliyor (start+dur bir sonrakinin start'ını
	// geçiyor); bipçi aynı anda tek ses çalabildiği için binen kısım
	// kırpılıyor — akor yerine arpej duyuluyor, ki bu zaten tablodaki niyet.
	gecen := 0.0
	for i, t := range tones {
		if t.start > gecen {
			time.Sleep(time.Duration((t.start - gecen) * float64(time.Second)))
			gecen = t.start
		}
		sure := t.dur
		if i+1 < len(tones) {
			if kalan := tones[i+1].start - t.start; kalan > 0 && kalan < sure {
				sure = kalan
			}
		}
		// Kayan notada (slide != 1) ortalama frekansı çal: bipçi kayma
		// yapamaz, ama "aşağı inen" hata sesi hiç olmazsa daha pes duyulur.
		hz := t.freq
		if t.slide > 0 && t.slide != 1 {
			hz = t.freq * (1 + t.slide) / 2
		}
		if err := writeTone(f, int32(hz)); err != nil {
			return err
		}
		time.Sleep(time.Duration(sure * float64(time.Second)))
		gecen += sure
		// Notalar arasında KISA bir sessizlik: yoksa iki komşu nota tek
		// uzun sese yapışır ve arpej duyulmaz.
		if i+1 < len(tones) {
			if err := writeTone(f, 0); err != nil {
				return err
			}
			time.Sleep(8 * time.Millisecond)
			gecen += 0.008
		}
	}
	return writeTone(f, 0) // 0 = sustur
}

func writeTone(f *os.File, hz int32) error {
	ev := inputEvent{Type: evSnd, Code: sndTone, Value: hz}
	buf := (*[unsafe.Sizeof(ev)]byte)(unsafe.Pointer(&ev))[:]
	_, err := f.Write(buf)
	return err
}

// eviocgbit builds the EVIOCGBIT(ev, len) ioctl request number.
//
// _IOC(_IOC_READ, 'E', 0x20+ev, len) — fbinput/evdev_linux.go ile aynı şema;
// burada yeniden hesaplanıyor çünkü o paket girdi okumaya ait ve ses için ona
// bağımlı olmak katmanları karıştırırdı.
func eviocgbit(ev, length uint32) uint32 {
	const (
		iocRead    = 2
		iocNRBits  = 8
		iocTypeBit = 8
		iocSizeBit = 14
		nrShift    = 0
		typeShift  = nrShift + iocNRBits
		sizeShift  = typeShift + iocTypeBit
		dirShift   = sizeShift + iocSizeBit
	)
	return iocRead<<dirShift | length<<sizeShift |
		uint32('E')<<typeShift | (0x20+ev)<<nrShift
}
