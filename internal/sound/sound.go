// Package sound plays the short interface effects that accompany MCOS's
// screen transitions.
//
// ── Kullanıcının isteği ─────────────────────────────────────────────────────
//
//	"bide hoparlörden efekt calsın gecis animasyonlarinda ama kapatılabilsin"
//
// ── Tasarım kararları ───────────────────────────────────────────────────────
//
//  1. SESLER GÖMÜLÜ DOSYA DEĞİL, HESAPLANIR. Bir .wav koleksiyonu imaja
//     megabaytlar ekler (initramfs tamamen RAM'e açılıyor) ve lisans sorusu
//     doğurur. Buradaki efektler birkaç yüz satırlık sinüs + zarf ile
//     üretiliyor: toplam maliyet birkaç kilobayt kod ve çalma anında ~20 KB
//     geçici tampon.
//
//  2. ÇALMA ASLA ARAYÜZÜ BEKLETMEZ. Play() kuyruğa atar ve hemen döner.
//     Ses çıkışı yoksa, alsa kilitlenmişse ya da aplay yavaşsa panelin kare
//     hızı bundan etkilenmez. Kuyruk dolarsa yeni istek DÜŞÜRÜLÜR — geciken
//     bir "tık" sesi, hiç olmayandan kötüdür.
//
//  3. ARKA UÇ ÇALIŞMA ANINDA SEÇİLİR. Sırayla: ALSA (aplay), PC hoparlörü
//     (evdev EV_SND), sessizlik. Böylece aynı ikili hem ses kartı olan bir
//     dizüstünde hem yalnızca bipçisi olan bir sunucu kutusunda hem de
//     geliştirme makinesinde çalışır.
//
//     Bipçi artık YALNIZCA kullanıcı izin verirse (SetBeeper) denenir.
//     Kullanıcı "farklı PC'lerde farklı sesler geliyor, tıklama sesi
//     hoparlörden gelsin" dedi: ses kartı geç tanınan ya da hiç olmayan
//     makinede efektler bipçiden, ötekinde hoparlörden çalıyordu.
//
//  4. VARSAYILAN SESSİZ DEĞİL, AMA KAPATILABİLİR. Ayar model.UIConfig.Sounds
//     alanındadır; kapalıyken hiçbir arka uç aranmaz ve hiçbir örnekleme
//     hesaplanmaz.
package sound

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Effect is one interface sound.
type Effect int

const (
	// Nav: imleç hareketi / sayfa geçişi. En kısa ve en sessiz olan.
	Nav Effect = iota
	// Open: pencere veya ekran açılışı (yukarı doğru iki nota).
	Open
	// Close: pencere kapanışı (aşağı doğru iki nota).
	Close
	// Confirm: onaylanan eylem (sunucu başlatma, ayar kaydı).
	Confirm
	// Error: reddedilen eylem / yanlış parola.
	Error
	// Boot: açılış animasyonu panele devredilirken.
	Boot
	// Shutdown: kapanış animasyonu.
	Shutdown
)

// sampleRate is the PCM rate for every effect.
//
// 44100 Hz: her ses kartının desteklediği tek orandır. 48000 de yaygındır ama
// eski AC'97 kodeklerinin bir kısmı yalnızca 44100'ü yeniden örneklemeden
// kabul eder.
const sampleRate = 44100

// Player owns the backend and the (small) play queue.
// beepName, anakart bipçisi arka ucunun adıdır.
//
// PLATFORMDAN BAĞIMSIZ dosyada duruyor: Player "bipçide miyim, gerçek kartı
// yeniden aramalı mıyım?" sorusunu buna bakarak yanıtlıyor (ensureBackend) ve
// o kod her platformda derleniyor. Linux'a özel dosyaya koymak, macOS/Windows
// derlemesini kırıyordu (ölçüldü: GOOS=darwin -> "undefined: beepName").
const beepName = "pcspkr"

type Player struct {
	mu      sync.Mutex
	backend backend
	probed  bool
	// lastProbe, son aramanın zamanı. Gerçek bir ses kartı bulunana kadar
	// (bipçideyken YA DA hiçbir şey yokken) aralıklarla yeniden aranıyor.
	lastProbe time.Time

	// beeper, anakart bipçisine düşülmesine izin var mı. Varsayılan KAPALI.
	beeper atomic.Bool

	// enabled atomik: panel çizim döngüsünden okunuyor, ayar ekranından
	// yazılıyor. Kilit almak, her kare için gereksiz bir kilit demekti.
	enabled atomic.Bool

	// queue kısa BİLEREK: sesler anlıktır. Kuyruk dolduysa yeni ses
	// düşürülür, çünkü 300 ms geciken bir tık, sesin ait olduğu hareketten
	// kopar ve kullanıcıya "arayüz takılıyor" hissi verir.
	queue chan Effect
	once  sync.Once
}

// backend is anything that can play one PCM buffer.
type backend interface {
	// play blocks until the sound is done (or gives up).
	//
	// Effect DE geçiyor, yalnızca PCM değil.
	//
	// ── Düzeltilen gerçek hata ──────────────────────────────────────────
	// Arayüz yalnızca PCM taşıyordu. ALSA için bu yeterli, ama anakart
	// bipçisi PCM çalamaz: tek frekans üretir. Notaları PCM'den geri
	// çıkaramadığı için sabit 880 Hz yazıyordu ve YEDİ EFEKTİN DÖRDÜ bit
	// bit AYNI duyuluyordu (ölçüldü: 7 efekt -> yalnızca 3 ayrı desen).
	//
	// Kullanıcının "hepsi aynı bip" şikâyetinin sebebi buydu. Efektin
	// kimliği geçince bipçi gerçek nota dizisini çalabiliyor.
	play(e Effect, pcm []int16, rate int) error
	// name is what the settings screen shows.
	name() string
}

// New creates a player. Nothing is probed until the first Play.
func New(enabled bool) *Player {
	p := &Player{queue: make(chan Effect, 4)}
	p.enabled.Store(enabled)
	return p
}

// SetEnabled turns sound on or off at runtime.
func (p *Player) SetEnabled(on bool) { p.enabled.Store(on) }

// SetBeeper allows (or forbids) falling back to the motherboard beeper.
//
// Kapatıldığı anda etkili: bipçideyken kapatılırsa bir sonraki seste arka uç
// bırakılır ve yalnızca gerçek ses kartı aranır (ensureBackend).
func (p *Player) SetBeeper(on bool) { p.beeper.Store(on) }

// Enabled reports whether sound is on.
func (p *Player) Enabled() bool { return p.enabled.Load() }

// Backend returns the chosen backend's name ("alsa", "pcspkr", "yok").
//
// Ayarlar ekranı bunu gösteriyor: "ses açık ama duyulmuyor" şikâyetinin ilk
// sorusu "sistem bir ses aygıtı görüyor mu?"dur — tıpkı fare ayarlarında
// bulunan aygıtları listelemek gibi.
func (p *Player) Backend() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.probed {
		return "denenmedi"
	}
	if p.backend == nil {
		return "yok"
	}
	return p.backend.name()
}

// Output, çalan çıkışın okunur adı ("ALC892 Analog · analog"); bilinmiyorsa "".
//
// "Ses kartı" demek yetmiyor: aynı makinede HDMI ve analog iki kart olabilir
// ve kullanıcının asıl sorusu "hangisinden çalıyor?".
func (p *Player) Output() string {
	p.mu.Lock()
	b := p.backend
	p.mu.Unlock()
	if o, ok := b.(interface{ output() string }); ok {
		return o.output()
	}
	return ""
}

// Play queues an effect. It never blocks and never fails.
func (p *Player) Play(e Effect) {
	if p == nil || !p.enabled.Load() {
		return
	}
	p.once.Do(func() { go p.run() })
	select {
	case p.queue <- e:
	default:
		// Kuyruk dolu: sesi düşür (yukarıdaki gerekçe).
	}
}

// run is the single playback goroutine.
//
// TEK goroutine bilerek: iki sesi aynı anda çalmak ALSA aygıtını "meşgul"
// hatasına sokar ve ikisi de duyulmaz.
func (p *Player) run() {
	for e := range p.queue {
		if !p.enabled.Load() {
			continue
		}
		b := p.ensureBackend()
		if b == nil {
			continue
		}
		pcm := render(e)
		if len(pcm) == 0 {
			continue
		}
		if err := b.play(e, pcm, sampleRate); err != nil {
			// Arka uç kayboldu (USB ses kartı çıkarıldı gibi): bir dahaki
			// sefere yeniden ara. Sessizce pes etmek, aygıt geri takılınca
			// sesin bir daha hiç gelmemesi demekti.
			p.mu.Lock()
			p.backend, p.probed = nil, false
			p.mu.Unlock()
		}
	}
}

// ensureBackend returns the playback backend, re-probing if we are on the beeper.
//
// ── Düzeltilen gerçek hata: BİPÇİYE KALICI KİLİTLENME ───────────────────────
//
// Arka uç BİR KEZ aranıp sonsuza kadar önbelleğe alınıyordu. Önbellek yalnızca
// play() HATA döndürdüğünde temizleniyor — ama bipçinin play'i pratikte hiç
// hata döndürmez. Yani bir kez bipçiye düşüldüyse, oturum boyunca orada
// kalınıyordu.
//
// Bu tek başına zararsız görünürdü, ama çekirdekle YARIŞIYOR:
//
//   - HDA kodek araştırması ERTELENMİŞ bir iş kuyruğuna atılır
//     (hda_intel.c: schedule_delayed_work) ve başarısız olursa 1 saniye
//     aralıklarla 60 KEZ yeniden denenir.
//   - Panel ise ilk sesi panel açılır açılmaz çalar (run.go: sound.Boot).
//
// Yani ses kartı 200 ms sonra görünen bir makinede bile MCOS bipçiye
// kilitleniyordu. Yeniden başlatmak da işe yaramaz: yarış her açılışta
// tekrarlanır. Kullanıcının gerçek PC'de de bip duymasının sebebi budur.
//
// Çözüm: gerçek bir ses kartı bulunana kadar belirli aralıklarla YENİDEN ARA.
// Kart belirdiği anda ona geçilir. ALSA bulunduysa artık aranmaz — orada
// yarış yok.
//
// ── Düzeltilen gerçek hata: HİÇ AYGIT YOKKEN KALICI SESSİZLİK ───────────────
//
// Yeniden arama yalnızca BİPÇİDEYKEN yapılıyordu. Bipçi kapalıyken (artık
// varsayılan) ilk ses HDA kodeği henüz tanınmadan çalınırsa arka uç "yok"
// olarak önbelleğe alınır ve oturum boyunca bir daha aranmazdı. Artık "yok"
// da bipçi gibi geçici sayılıyor.
func (p *Player) ensureBackend() backend {
	p.mu.Lock()
	defer p.mu.Unlock()
	allowBeep := p.beeper.Load()

	// Kullanıcı bipçiyi kapattıysa ondan HEMEN vazgeç: "kapattım ama hâlâ
	// bipliyor" ayarın işe yaramadığı izlenimini verirdi.
	if p.backend != nil && p.backend.name() == beepName && !allowBeep {
		p.backend = nil
		p.probed = false
	}
	// Gerçek ses kartındayız: aramaya gerek yok (her seste /dev/snd taramak
	// boşuna iştir).
	if p.backend != nil && p.backend.name() != beepName {
		return p.backend
	}
	if p.probed && time.Since(p.lastProbe) < beepRetryEvery {
		return p.backend
	}
	p.probed = true
	p.lastProbe = time.Now()
	if b := probe(allowBeep); b != nil || p.backend == nil {
		// Bipçideyken yeni arama da bipçiyi bulursa yenisi alınıyor (zararsız);
		// hiçbir şey bulamazsa ve bipçiye izin varsa eldeki bipçi korunuyor.
		p.backend = b
	}
	return p.backend
}

// beepRetryEvery, gerçek ses kartının yeniden aranma aralığı (bipçideyken ya
// da hiçbir aygıt yokken).
//
// 2 saniye: HDA araştırması 60 saniyeye kadar sürebiliyor, yani birkaç
// denemeye yer olmalı; ama her seste /dev/snd taramak da gereksiz. Kullanıcı
// açılıştan sonraki ilk birkaç saniyede zaten çok az ses tetikler.
const beepRetryEvery = 2 * time.Second

// ── Ses üretimi ─────────────────────────────────────────────────────────────

// tone is one component of an effect.
type tone struct {
	freq  float64 // Hz
	start float64 // saniye (efektin başından)
	dur   float64 // saniye
	gain  float64 // 0..1
	// slide, notanın sonundaki frekans oranı (1 = sabit, 2 = bir oktav yukarı).
	slide float64
}

// effects defines every sound as a handful of tones.
//
// ── Neden bu notalar ────────────────────────────────────────────────────────
// Hepsi aynı beşli aralıktan (D5-A5-D6) seçildi: art arda çalan iki farklı
// efekt uyumsuz duyulmasın. Süreler 60-260 ms arasında — arayüz sesleri
// hareketten UZUN sürerse gecikmiş gibi duyulur; geçiş animasyonları 180 ms.
var effects = map[Effect][]tone{
	// Kısa, yumuşak bir tık. Sık çalar, bu yüzden en sessiz olanı.
	Nav: {{freq: 880, start: 0, dur: 0.045, gain: 0.16, slide: 1}},

	// Açılış: yukarı iki nota.
	Open: {
		{freq: 587.33, start: 0, dur: 0.070, gain: 0.22, slide: 1},
		{freq: 880.00, start: 0.055, dur: 0.110, gain: 0.20, slide: 1},
	},

	// Kapanış: aynı iki nota, ters sırada.
	Close: {
		{freq: 880.00, start: 0, dur: 0.070, gain: 0.20, slide: 1},
		{freq: 587.33, start: 0.055, dur: 0.110, gain: 0.18, slide: 1},
	},

	// Onay: üçlü yükselen arpej.
	Confirm: {
		{freq: 587.33, start: 0, dur: 0.070, gain: 0.20, slide: 1},
		{freq: 880.00, start: 0.060, dur: 0.070, gain: 0.20, slide: 1},
		{freq: 1174.66, start: 0.120, dur: 0.150, gain: 0.18, slide: 1},
	},

	// Hata: aşağı kayan tek nota. Titremeyen, "hayır" diyen bir ses.
	Error: {{freq: 440, start: 0, dur: 0.220, gain: 0.22, slide: 0.62}},

	// Açılış jingle'ı: panel devralınırken.
	Boot: {
		{freq: 440.00, start: 0, dur: 0.110, gain: 0.18, slide: 1},
		{freq: 587.33, start: 0.090, dur: 0.110, gain: 0.18, slide: 1},
		{freq: 880.00, start: 0.180, dur: 0.260, gain: 0.20, slide: 1},
	},

	// Kapanış jingle'ı: aynı notalar inerek.
	Shutdown: {
		{freq: 880.00, start: 0, dur: 0.110, gain: 0.18, slide: 1},
		{freq: 587.33, start: 0.090, dur: 0.110, gain: 0.18, slide: 1},
		{freq: 440.00, start: 0.180, dur: 0.300, gain: 0.20, slide: 0.9},
	},
}

// render synthesises one effect as 16-bit mono PCM.
//
// Zarf (envelope) ŞART: bir sinüsü aniden başlatıp bitirmek, dalga biçiminde
// süreksizlik yaratır ve hoparlörde "çıt" diye duyulur. 8 ms'lik yükseliş ve
// üstel düşüş bunu tamamen giderir.
func render(e Effect) []int16 {
	tones := effects[e]
	if len(tones) == 0 {
		return nil
	}

	total := 0.0
	for _, t := range tones {
		if end := t.start + t.dur; end > total {
			total = end
		}
	}
	n := int(total * sampleRate)
	if n <= 0 {
		return nil
	}
	out := make([]float64, n)

	const (
		attack = 0.008 // 8 ms yükseliş
		// release, notanın sonunda TAM SIFIRA inmesi için ayrılan süre.
		//
		// 8 ms: duyulabilir bir "sönme" yaratmayacak kadar kısa, ama
		// süreksizliği gidermeye fazlasıyla yeter (44100 Hz'de 353 örnek).
		release = 0.008
	)

	for _, t := range tones {
		startIdx := int(t.start * sampleRate)
		count := int(t.dur * sampleRate)
		phase := 0.0
		for i := 0; i < count; i++ {
			idx := startIdx + i
			if idx < 0 || idx >= n {
				continue
			}
			pos := float64(i) / float64(count) // 0..1

			// Frekans kayması: doğrusal.
			f := t.freq * (1 + (t.slide-1)*pos)
			phase += 2 * math.Pi * f / sampleRate

			// ── Düzeltilen gerçek hata: NOTA SONUNDA TIKIRTI ─────────
			//
			// Zarf yalnızca exp(-3.2*pos) ile bitiyordu. exp(-3.2) = 0,0408
			// — yani her nota, genliğinin HÂLÂ %4'ündeyken aniden kesilip
			// sessizliğe düşüyordu. Bu sıçrama, o frekanstaki doğal sinüs
			// eğiminin 4-14 katı: sentetik bir süreksizlik, yani "çıt".
			//
			// Ölçüldü: 7 efektte 15 nota bitişi = 15 ayrı tıkırtı kaynağı.
			// Son örnek değerleri 112...248 (-49...-42 dBFS) arasındaydı.
			//
			// Çözüm: sondaki son 8 ms'de doğrusal olarak TAM SIFIRA in.
			// Artık son örnek gerçekten 0 ve süreksizlik kalmıyor.
			env := 1.0
			if a := float64(i) / sampleRate; a < attack {
				env = a / attack
			}
			env *= math.Exp(-3.2 * pos)
			// Bitiş rampası: kalan örnek sayısı release penceresine
			// girdiğinde sıfıra doğru doğrusal azalt.
			// count-1-i: SON örnekte kalan tam 0 olsun. "count-i" yazılsaydı
			// son örnekte env = 1/(44100*0.008) ≈ 0,0028 kalırdı ve testin
			// gördüğü ±1..3'lük artık örnek buydu. Duyulmaz, ama sözleşme
			// "tam sıfır" — yaklaşık sıfır, bir gün yaklaşık tıkırtı olur.
			if kalan := float64(count-1-i) / sampleRate; kalan < release {
				env *= kalan / release
			}

			out[idx] += math.Sin(phase) * env * t.gain
		}
	}

	// ── Düzeltilen gerçek sorun: EFEKTLER ÇOK SESSİZDİ ──────────────────
	//
	// Ölçülen tepe seviyeleri -21,2 ile -14,4 dBFS arasındaydı; yani 14-21 dB
	// başlık boşa gidiyordu. Küçük hoparlörlerde bu, "ses çalmıyor" demekle
	// aynı şey.
	//
	// Kırpmak yerine NORMALİZE ediliyor: en yüksek örnek hedefe çekilip
	// bütün efekt aynı oranda ölçekleniyor. Böylece hem dalga biçimi
	// bozulmuyor (kırpma harmonik gürültü üretir) hem de yedi efekt
	// birbirine göre DENGELİ kalıyor — Nav hâlâ en sessizi, çünkü tablodaki
	// gain oranları korunuyor.
	//
	// Hedef -2,9 dBFS: tam ölçeğe dayamak, DAC'ta ara örneklerin taşmasına
	// (intersample peak) yol açabilir.
	const hedefTepe = 0.717 // -2,9 dBFS

	tepe := 0.0
	for _, v := range out {
		if a := math.Abs(v); a > tepe {
			tepe = a
		}
	}
	olcek := 1.0
	if tepe > 0 {
		olcek = hedefTepe / tepe
	}

	pcm := make([]int16, n)
	for i, v := range out {
		v *= olcek
		// Kırpma yine de duruyor: normalizasyon tepeyi hedefe çeker, ama
		// kayan nokta yuvarlaması sınırda 1'i geçebilir.
		if v > 1 {
			v = 1
		}
		if v < -1 {
			v = -1
		}
		pcm[i] = int16(v * 32000)
	}
	return pcm
}

// Render exposes the synthesised PCM for review tooling (scripts/wavdump).
//
// Arayüz sesleri hedef donanımda ancak elle denenebilir. Bu giriş noktası,
// efektlerin bir WAV dosyasına dökülüp geliştirme makinesinde DİNLENEBİLMESİNİ
// sağlıyor — "çalıyor mu" ile "kulağa doğru geliyor mu" ayrı sorulardır.
func Render(e Effect) []int16 { return render(e) }

// pcmBytes converts samples to little-endian S16_LE bytes.
func pcmBytes(pcm []int16) []byte {
	b := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		b[i*2] = byte(uint16(s))
		b[i*2+1] = byte(uint16(s) >> 8)
	}
	return b
}
