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
//
// allowBeep false ise bipçi HİÇ aranmaz (varsayılan; bkz. model.UIConfig.Beeper).
func probe(allowBeep bool) backend {
	if a := probeALSA(); a != nil {
		return a
	}
	if !allowBeep {
		return nil
	}
	if b := probeBeep(); b != nil {
		return b
	}
	return nil
}

// ── ALSA (aplay) ────────────────────────────────────────────────────────────

type alsaBackend struct {
	bin string
	// adaylar, denenecek PCM aygıtları, en iyisi önde (cards.go sıralar).
	adaylar []alsaAday

	// device/kart, çalışan ilk aday. İlk başarılı çalmadan sonra
	// sabitlenir; her seste yeniden denemek gereksiz süreç başlatmaktır.
	mu     sync.Mutex
	device string
	kart   int
	tanim  string
	// hazir, mikseri açılmış kartlar (kart başına BİR kez).
	hazir map[int]*mikserHedefi
}

// alsaAday, aplay'e verilecek bir aygıt ve ait olduğu kart.
//
// kart -1: kart numarası bilinmiyor ("default" gibi); amixer'e -c verilmez.
type alsaAday struct {
	dev  string
	kart int
	// tanim, ayarlar ekranı için okunur ad ("ALC892 Analog · analog").
	tanim string
}

// alsaDevices, /proc/asound okunamazsa kullanılan eski sabit liste.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// İlk sürüm yalnızca varsayılan aygıtı kullanıyordu. QEMU'da uçtan uca
// ölçümde tek bir örnek bile çalınmadı; mcos-soundcheck sebebi gösterdi:
//
//	ALSA lib pcm_direct.c:2188:(_snd_pcm_direct_new)
//	    unable to create IPC semaphore
//	aplay: main:850: audio open error: Function not implemented
//
// alsa-lib'in "default" aygıtı dmix'tir ve System V semaforu açar; çekirdekte
// CONFIG_SYSVIPC kapalıysa hiçbir şey duyulmaz. Bu yüzden "default" artık
// SON halka; önce kartına göre seçilmiş plughw:K,A aygıtları deneniyor
// (cards.go: HDMI'ye değil hoparlöre gitsin diye).
var alsaDevices = []string{"plughw:0,0", "default"}

func (a *alsaBackend) name() string { return "alsa" }

// output, çalan aygıtın okunur adı (henüz çalmadıysa en iyi aday).
func (a *alsaBackend) output() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tanim != "" {
		return a.tanim
	}
	if len(a.adaylar) > 0 {
		return a.adaylar[0].tanim
	}
	return ""
}

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
	return &alsaBackend{bin: bin, adaylar: alsaAdaylari(), kart: -1,
		hazir: map[int]*mikserHedefi{}}
}

// alsaAdaylari, /proc/asound'dan sıralanmış çalma aygıtlarını okur.
//
// Sonuna "default" EKLENİYOR: hiçbir plughw açılamazsa (ör. aygıtı başka
// bir süreç tutuyor) dmix hâlâ çalabilir.
func alsaAdaylari() []alsaAday {
	cikislar := siraliCikislar(readProc("/proc/asound/pcm"), readProc("/proc/asound/cards"))
	var out []alsaAday
	for _, c := range cikislar {
		out = append(out, alsaAday{dev: c.aplayAygiti(), kart: c.kart, tanim: c.Tanim()})
	}
	if len(out) == 0 {
		for _, d := range alsaDevices {
			k := -1
			if strings.HasPrefix(d, "plughw:0,") {
				k = 0
			}
			out = append(out, alsaAday{dev: d, kart: k})
		}
		return out
	}
	return append(out, alsaAday{dev: "default", kart: -1})
}

func readProc(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// hasPlaybackDevice reports whether /dev/snd has a playback PCM node.
func hasPlaybackDevice() bool {
	matches, err := filepath.Glob("/dev/snd/pcmC*D*p")
	return err == nil && len(matches) > 0
}

// ── Mikser: hangi kartın hangi kontrolü ─────────────────────────────────────

// mikserHedefi, ses seviyesinin yazıldığı kart ve kontrol.
type mikserHedefi struct {
	kart int    // -1: varsayılan kart (amixer'e -c verilmez)
	ana  string // kullanıcı seviyesini taşıyan kontrol ("Master" ...)
}

// hedef, F3/F4'ün değiştirdiği mikser. Çalan kart belli olunca o yazılır;
// henüz hiç ses çalmadıysa en iyi çıkışın kartından hesaplanır.
var (
	hedefMu sync.Mutex
	hedef   *mikserHedefi
)

// kartArg, amixer'e kart seçtiren argümanlar.
func kartArg(kart int) []string {
	if kart < 0 {
		return nil
	}
	return []string{"-c", strconv.Itoa(kart)}
}

// amixerRun, amixer'i çalıştırır; çıktı yok sayılır, yalnızca başarı döner.
func amixerRun(amixer string, kart int, args ...string) error {
	cmd := exec.Command(amixer, append(kartArg(kart), args...)...)
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Run()
}

// mikserAc, bir kartın çıkış yolunu açar ve seviye hedefini döndürür.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// HDA kodeklerinin çoğu açılışta çıkışları KAPALI getirir. O hâlde her şey
// doğru kurulur, aplay 0 ile döner ve hiçbir şey duyulmaz. Teşhisi en zor ses
// arızası budur. Plan (hangi kontrol seviyeyi taşır, hangileri yalnızca
// açılır) cards.go'daki mikserPlani'dir.
//
// Hatalar yok sayılır: kontrolü olmayan kartlar var ve orada ses zaten açıktır.
func mikserAc(kart int) *mikserHedefi {
	h := &mikserHedefi{kart: kart}
	amixer, err := exec.LookPath("amixer")
	if err != nil {
		return h
	}
	out, err := exec.Command(amixer, append(kartArg(kart), "scontrols")...).Output()
	var kontroller []string
	if err == nil {
		kontroller = parseScontrols(string(out))
	}
	ana, yan := mikserPlani(kontroller)
	if len(kontroller) == 0 {
		// Kontrol listesi okunamadı: eski davranış, bilinen adları dene.
		ana, yan = "Master", []string{"PCM", "Speaker", "Headphone", "Front"}
	}
	h.ana = ana
	// Yan kontroller 0 dB'e (geçirgen) açılıyor: seviye zincirde YALNIZCA
	// ana kontrolde uygulanmalı (bkz. mikserPlani). dB bilgisi olmayan
	// kontrolde "0dB" reddedilir; o zaman tam açık.
	for _, k := range yan {
		if amixerRun(amixer, kart, "-q", "sset", k, "0dB", "unmute") != nil {
			_ = amixerRun(amixer, kart, "-q", "sset", k, "100%", "unmute")
		}
	}
	if ana != "" {
		seviyeYaz(amixer, h, Volume())
	}
	return h
}

// seviyeYaz, kullanıcı seviyesini ana kontrole yazar.
//
// -M (eşlenmiş ölçek): alsamixer'in gösterdiği algısal ölçek. Ham yüzdeyle
// HDA'da %70 = -22 dB idi, yani "%70" kulağa neredeyse sessiz geliyordu.
func seviyeYaz(amixer string, h *mikserHedefi, v int) {
	arg := strconv.Itoa(v) + "%"
	// unmute BİRLİKTE veriliyor: seviyeyi yükseltip sessizde bırakmak,
	// "sesi açtım ama duyulmuyor" demek olurdu.
	if amixerRun(amixer, h.kart, "-q", "-M", "sset", h.ana, arg, "unmute") != nil {
		_ = amixerRun(amixer, h.kart, "-q", "sset", h.ana, arg, "unmute")
	}
}

// secilenMikser, ses seviyesinin yazılacağı yeri döndürür (nil: bilinmiyor).
func secilenMikser() *mikserHedefi {
	hedefMu.Lock()
	defer hedefMu.Unlock()
	if hedef != nil {
		return hedef
	}
	// Henüz ses çalmadı (ya da efektler kapalı): en iyi çıkışın kartını al.
	// Bu olmadan F3/F4, kart 0'ın (çoğu masaüstünde HDMI) mikserini
	// değiştiriyordu.
	c := siraliCikislar(readProc("/proc/asound/pcm"), readProc("/proc/asound/cards"))
	if len(c) == 0 {
		return nil
	}
	hedef = mikserAc(c[0].kart)
	return hedef
}

// hazirla, bir kartın mikserini BİR KEZ açar.
func (a *alsaBackend) hazirla(kart int) *mikserHedefi {
	a.mu.Lock()
	h, ok := a.hazir[kart]
	a.mu.Unlock()
	if ok {
		return h
	}
	h = mikserAc(kart)
	a.mu.Lock()
	a.hazir[kart] = h
	a.mu.Unlock()
	return h
}

// play pipes raw PCM into aplay, trying each device until one works.
func (a *alsaBackend) play(_ Effect, pcm []int16, rate int) error {
	a.mu.Lock()
	adaylar := a.adaylar
	if a.device != "" {
		adaylar = []alsaAday{{dev: a.device, kart: a.kart, tanim: a.tanim}} // çalışan aygıt bulundu
	}
	a.mu.Unlock()

	var lastErr error
	for _, ad := range adaylar {
		h := a.hazirla(ad.kart)
		err := a.playOn(ad.dev, pcm, rate)
		if err == nil {
			a.mu.Lock()
			a.device, a.kart, a.tanim = ad.dev, ad.kart, ad.tanim
			a.mu.Unlock()
			// Ses seviyesi tuşları artık ÇALAN kartı değiştirsin.
			if h != nil && h.ana != "" {
				hedefMu.Lock()
				hedef = h
				hedefMu.Unlock()
			}
			return nil
		}
		lastErr = err
	}
	// Bilinen aygıt artık çalışmıyorsa (USB kart çıkarıldı) listeyi
	// baştan denemek için sıfırla. Hata dönünce Player da arka ucu bırakıp
	// yeniden arıyor ve /proc/asound yeniden okunuyor.
	a.mu.Lock()
	a.device, a.kart, a.tanim = "", -1, ""
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
