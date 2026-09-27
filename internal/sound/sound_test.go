package sound

import (
	"math"
	"testing"
	"time"
)

// Ses üretiminin testleri.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Ses, geliştirme makinesinde DUYULAMAZ (ses kartı yok) ve hedef donanımda da
// ancak elle denenebilir. Yani "çalışıyor mu" sorusuna bakarak cevap vermek
// imkânsız. Bu testler sesin SAYISAL özelliklerini kilitliyor: doğru süre,
// sessiz olmayan içerik, kırpılmamış genlik ve "çıt" sesine yol açan
// süreksizliğin yokluğu.

// Her efekt gerçek bir dalga üretmeli — boş dilim, sessizlik demektir.
func TestEveryEffectRenders(t *testing.T) {
	all := []Effect{Nav, Open, Close, Confirm, Error, Boot, Shutdown}
	for _, e := range all {
		pcm := render(e)
		if len(pcm) == 0 {
			t.Errorf("efekt %d hiç örnek üretmedi", e)
			continue
		}
		// En az bir örnek duyulabilir genlikte olmalı.
		peak := 0
		for _, s := range pcm {
			if v := int(s); v > peak {
				peak = v
			} else if -v > peak {
				peak = -v
			}
		}
		if peak < 1000 {
			t.Errorf("efekt %d neredeyse sessiz (tepe %d)", e, peak)
		}
	}
}

// Süreler makul aralıkta olmalı: arayüz sesi hareketten uzun sürerse
// gecikmiş gibi duyulur.
func TestEffectDurations(t *testing.T) {
	limits := map[Effect][2]time.Duration{
		Nav:      {20 * time.Millisecond, 120 * time.Millisecond},
		Open:     {80 * time.Millisecond, 300 * time.Millisecond},
		Close:    {80 * time.Millisecond, 300 * time.Millisecond},
		Confirm:  {150 * time.Millisecond, 400 * time.Millisecond},
		Error:    {100 * time.Millisecond, 400 * time.Millisecond},
		Boot:     {300 * time.Millisecond, 700 * time.Millisecond},
		Shutdown: {300 * time.Millisecond, 700 * time.Millisecond},
	}
	for e, lim := range limits {
		d := time.Duration(len(render(e))) * time.Second / sampleRate
		if d < lim[0] || d > lim[1] {
			t.Errorf("efekt %d süresi %v — beklenen aralık %v..%v",
				e, d, lim[0], lim[1])
		}
	}
}

// Dalga başında ve sonunda süreksizlik OLMAMALI.
//
// Bir sinüsü aniden başlatmak ya da bitirmek hoparlörde duyulur bir "çıt"
// üretir. Zarfın ilk 8 ms'si yükselir, sonu üstel olarak söner; testi
// geçmesi için ilk ve son örneğin sıfıra yakın olması yeterli.
func TestNoClickAtEdges(t *testing.T) {
	for _, e := range []Effect{Nav, Open, Close, Confirm, Error, Boot, Shutdown} {
		pcm := render(e)
		if len(pcm) < 10 {
			t.Fatalf("efekt %d çok kısa", e)
		}
		if v := abs16(pcm[0]); v > 500 {
			t.Errorf("efekt %d ilk örnek %d — başlangıçta çıt sesi olur", e, v)
		}
		if v := abs16(pcm[len(pcm)-1]); v > 2500 {
			t.Errorf("efekt %d son örnek %d — bitişte çıt sesi olur", e, v)
		}
	}
}

// Üst üste binen notalar toplandığında kırpma (clipping) olmamalı: taşan bir
// toplam sesi gürültüye çevirir.
func TestNoHardClipping(t *testing.T) {
	for _, e := range []Effect{Confirm, Boot, Shutdown} {
		pcm := render(e)
		clipped := 0
		for _, s := range pcm {
			if abs16(s) >= 32000 {
				clipped++
			}
		}
		// Tam sınırda birkaç örnek olabilir; yüzlercesi kırpma demektir.
		if clipped > 5 {
			t.Errorf("efekt %d: %d örnek tavana dayandı (kırpma)", e, clipped)
		}
	}
}

// PCM baytları little-endian S16_LE olmalı: aplay'e verilen biçim bu.
func TestPCMBytesAreLittleEndian(t *testing.T) {
	b := pcmBytes([]int16{0x0102, -2})
	want := []byte{0x02, 0x01, 0xFE, 0xFF}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("bayt %d: %#x, beklenen %#x (dizi: %#v)", i, b[i], want[i], b)
		}
	}
}

// Kapalı çalar hiçbir şey yapmamalı ve ASLA bloklamamalı.
func TestDisabledPlayerIsSilentAndFast(t *testing.T) {
	p := New(false)
	if p.Enabled() {
		t.Fatal("kapalı kurulan çalar açık görünüyor")
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			p.Play(Nav)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("kapalı çalar Play() çağrısında bloklandı")
	}
}

// Açık çalar da bloklamamalı: kuyruk dolduğunda ses DÜŞÜRÜLÜR.
//
// Bu, arayüzün ses yüzünden takılmamasının tek güvencesi. Ses aygıtı yavaşsa
// (ya da aplay asılırsa) panel yine 60 kare/sn çizmeye devam etmeli.
func TestEnabledPlayerNeverBlocks(t *testing.T) {
	p := New(true)

	start := time.Now()
	for i := 0; i < 500; i++ {
		p.Play(Open)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("500 Play() çağrısı %v sürdü — kuyruk bloklamış olabilir", el)
	}
}

// Çalma sırasında kapatmak anında etkili olmalı.
func TestSetEnabledTakesEffect(t *testing.T) {
	p := New(true)
	p.SetEnabled(false)
	if p.Enabled() {
		t.Error("SetEnabled(false) sonrası çalar hâlâ açık")
	}
	p.SetEnabled(true)
	if !p.Enabled() {
		t.Error("SetEnabled(true) sonrası çalar hâlâ kapalı")
	}
}

// Arka uç denenmeden önce "denenmedi" demeli: ayar ekranı bunu gösteriyor ve
// yanlış bir "yok" cevabı kullanıcıyı boşuna donanım aramaya yollar.
func TestBackendUnprobedIsHonest(t *testing.T) {
	p := New(true)
	if got := p.Backend(); got != "denenmedi" {
		t.Errorf("henüz aranmamış arka uç %q dedi", got)
	}
}

// Efekt notaları aynı akorttan seçilmiş olmalı: art arda çalan iki farklı
// efektin uyumsuz duyulmaması bilinçli bir karardı (bkz. effects).
func TestTonesStayInOneScale(t *testing.T) {
	allowed := []float64{440.00, 587.33, 880.00, 1174.66}
	for e, tones := range effects {
		for _, tn := range tones {
			ok := false
			for _, f := range allowed {
				if math.Abs(tn.freq-f) < 0.01 {
					ok = true
				}
			}
			if !ok {
				t.Errorf("efekt %d, akort dışı frekans: %.2f Hz", e, tn.freq)
			}
		}
	}
}

func abs16(s int16) int {
	if s < 0 {
		return -int(s)
	}
	return int(s)
}
