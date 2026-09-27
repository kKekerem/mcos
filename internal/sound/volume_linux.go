//go:build linux

package sound

import (
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
)

// ════════════════════════════════════════════════════════════════════════════
// SES SEVİYESİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek eksik ─────────────────────────────────────────────────
//
// Ses seviyesi hiçbir yerden ayarlanamıyordu: açılışta bir kez amixer'e SABİT
// "%70" yazılıyor ve bir daha dokunulmuyordu (backend_linux.go, unmute).
// Kullanıcının elinde ne bir tuş ne bir menü vardı.
//
// ── Neden amixer, neden doğrudan ALSA değil ─────────────────────────────────
//
// Doğrudan ALSA mikser API'si libasound ister; CGO_ENABLED=0 olduğu için o yol
// kapalı. amixer imajda ZATEN var (unmute onu kullanıyor) ve kontrol adlarını
// kendisi çözüyor — hangi kartın "Master", hangisinin yalnızca "PCM" taşıdığını
// bilmek zorunda kalmıyoruz.
//
// ── Neden seviye BURADA da saklanıyor ───────────────────────────────────────
//
// amixer'den okumak her seferinde bir süreç çatallamak demek. Panel ses
// göstergesini her karede çizebilir; seviye bu yüzden bellekte tutuluyor ve
// yalnızca DEĞİŞTİĞİNDE donanıma yazılıyor.

// volumeStep, bir tuş basışının değiştirdiği miktar (yüzde).
//
// 5: 20 basışta sıfırdan sona. 10 kaba, 2 ise "tuşa basıyorum ama bir şey
// olmuyor" hissi veriyor.
const volumeStep = 5

// defaultVolume, açılış seviyesi.
//
// 70: eski sabit değerle aynı tutuldu — kullanıcı bir şeyin DEĞİŞTİĞİNİ değil,
// artık AYARLANABİLDİĞİNİ fark etmeli.
const defaultVolume = 70

// mixerControls, sırayla denenen kontrol adları.
//
// Hepsi denenir ve hata YOK SAYILIR: bir kartta "Master" varken ötekinde
// yalnızca "PCM" olabiliyor. Var olmayan bir kontrole yazmak zararsızdır.
var mixerControls = []string{"Master", "PCM", "Speaker", "Headphone"}

// level, 0..100 arası geçerli ses seviyesi.
//
// atomic: panel çizim döngüsünden okunuyor, tuş işleyicisinden yazılıyor.
var level atomic.Int32

func init() { level.Store(defaultVolume) }

// Volume returns the current level (0..100).
func Volume() int { return int(level.Load()) }

// SetVolume applies a new level and returns what was actually set.
//
// Sınırlar içeride kırpılıyor: çağıranın her yerde aynı kontrolü tekrarlaması
// gerekmesin.
func SetVolume(v int) int {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	level.Store(int32(v))
	applyVolume(v)
	return v
}

// VolumeUp raises the level by one step and returns the new value.
func VolumeUp() int { return SetVolume(Volume() + volumeStep) }

// VolumeDown lowers the level by one step and returns the new value.
func VolumeDown() int { return SetVolume(Volume() - volumeStep) }

// ToggleMute switches between silent and the last audible level.
//
// Sessize alınmadan önceki seviye HATIRLANIYOR: sıfırdan geri açan kullanıcı
// yirmi kez tuşa basmak zorunda kalmamalı.
func ToggleMute() (int, bool) {
	if v := Volume(); v > 0 {
		lastAudible.Store(int32(v))
		SetVolume(0)
		return 0, true
	}
	geri := int(lastAudible.Load())
	if geri <= 0 {
		geri = defaultVolume
	}
	SetVolume(geri)
	return geri, false
}

var lastAudible atomic.Int32

func init() { lastAudible.Store(defaultVolume) }

// applyVolume writes the level to the sound card.
//
// Hatalar yok sayılıyor: ses kartı olmayan bir makinede (bipçi yolunda)
// amixer yoktur ve bu bir arıza değildir. Kullanıcının gördüğü seviye yine de
// değişir — bipçide seviye zaten anlamsız.
func applyVolume(v int) {
	amixer, err := exec.LookPath("amixer")
	if err != nil {
		return
	}
	arg := strconv.Itoa(v) + "%"
	for _, ctl := range mixerControls {
		// unmute BİRLİKTE veriliyor: seviyeyi yükseltip sessizde bırakmak,
		// "sesi açtım ama duyulmuyor" demek olurdu.
		cmd := exec.Command(amixer, "-q", "sset", ctl, arg, "unmute")
		cmd.Stdout, cmd.Stderr = nil, nil
		_ = cmd.Run()
	}
}

// ReadVolume queries the card once, at startup.
//
// Donanımın gerçek seviyesiyle başlamak, kullanıcının gördüğü sayının doğru
// olmasını sağlıyor. Okunamazsa varsayılan korunur.
func ReadVolume() int {
	amixer, err := exec.LookPath("amixer")
	if err != nil {
		return Volume()
	}
	for _, ctl := range mixerControls {
		out, err := exec.Command(amixer, "sget", ctl).Output()
		if err != nil {
			continue
		}
		if v, ok := parseAmixerPercent(string(out)); ok {
			level.Store(int32(v))
			if v > 0 {
				lastAudible.Store(int32(v))
			}
			return v
		}
	}
	return Volume()
}

// parseAmixerPercent pulls the first "[NN%]" out of amixer's output.
//
// amixer'in çıktısı insan içindir ve sürümden sürüme biçim değiştirir; tek
// dayanağımız köşeli parantez içindeki yüzde. Bulunamazsa YANLIŞ BİR SAYI
// UYDURMUYORUZ, "okuyamadım" diyoruz.
func parseAmixerPercent(s string) (int, bool) {
	for {
		i := strings.IndexByte(s, '[')
		if i < 0 {
			return 0, false
		}
		j := strings.IndexByte(s[i:], ']')
		if j < 0 {
			return 0, false
		}
		ic := s[i+1 : i+j]
		s = s[i+j:]
		if !strings.HasSuffix(ic, "%") {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSuffix(ic, "%")); err == nil &&
			v >= 0 && v <= 100 {
			return v, true
		}
	}
}
