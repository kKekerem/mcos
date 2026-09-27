package sound

import (
	"math"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// SES: TIKIRTI YOK, SEVİYE YETERLİ, EFEKTLER BİRBİRİNDEN AYRI
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek sorunlar ───────────────────────────────────────────────
//
// Kullanıcı "ses efekti çalmıyor, bip sesi çalıyor" dedi. Ölçüm üç ayrı kusur
// buldu; bu dosya üçünü de sayıyla kilitler:
//
//   1. Zarf exp(-3.2*pos) ile bitiyordu: her nota genliğinin %4,08'inde
//      ANIDEN kesiliyordu. 7 efektte 15 nota bitişi = 15 tıkırtı kaynağı.
//   2. Tepe seviyeler -21,2...-14,4 dBFS — küçük hoparlörde duyulmuyordu.
//   3. Bipçi her efekte sabit 880 Hz yazıyordu; 7 efektin 4'ü bit bit aynıydı.

func tumEfektler() []Effect {
	out := make([]Effect, 0, len(effects))
	for e := range effects {
		out = append(out, e)
	}
	return out
}

// Her efektin SON örneği tam sıfır olmalı: süreksizlik = tıkırtı.
func TestEfektSonuTamSifir(t *testing.T) {
	for _, e := range tumEfektler() {
		pcm := render(e)
		if len(pcm) == 0 {
			t.Errorf("efekt %v boş PCM üretti", e)
			continue
		}
		if son := pcm[len(pcm)-1]; son != 0 {
			t.Errorf("efekt %v son örnek %d (0 olmalı) — "+
				"sessizliğe sıçrama hoparlörde ÇIT diye duyulur", e, son)
		}
	}
}

// Sondaki birkaç örnek de sıfıra YAKIN olmalı: tek örnek sıfırlamak
// süreksizliği gidermez, rampanın gerçekten çalışması gerekir.
func TestEfektSonuYumusakIniyor(t *testing.T) {
	for _, e := range tumEfektler() {
		pcm := render(e)
		if len(pcm) < 100 {
			continue
		}
		// Son 1 ms (44 örnek) içindeki en büyük mutlak değer, tepenin
		// %1'ini geçmemeli.
		tepe := 0
		for _, v := range pcm {
			if a := abs16(v); a > tepe {
				tepe = a
			}
		}
		kuyruk := 0
		for _, v := range pcm[len(pcm)-44:] {
			if a := abs16(v); a > kuyruk {
				kuyruk = a
			}
		}
		if tepe > 0 && float64(kuyruk) > 0.01*float64(tepe) {
			t.Errorf("efekt %v: kuyruk %d, tepe %d (%%%.1f) — "+
				"rampa çalışmıyor, tıkırtı kalır",
				e, kuyruk, tepe, 100*float64(kuyruk)/float64(tepe))
		}
	}
}

// Seviye: her efekt duyulabilir olmalı ama tam ölçeğe dayanmamalı.
func TestEfektSeviyesiYeterli(t *testing.T) {
	for _, e := range tumEfektler() {
		pcm := render(e)
		tepe := 0
		for _, v := range pcm {
			if a := abs16(v); a > tepe {
				tepe = a
			}
		}
		dbfs := 20 * math.Log10(float64(tepe)/32768)
		if dbfs < -6 {
			t.Errorf("efekt %v tepe %.1f dBFS — çok sessiz "+
				"(eskiden -21,2...-14,4 dBFS idi, küçük hoparlörde duyulmuyordu)",
				e, dbfs)
		}
		if dbfs > -1 {
			t.Errorf("efekt %v tepe %.1f dBFS — tam ölçeğe çok yakın, "+
				"DAC'ta ara örnek taşması riski", e, dbfs)
		}
	}
}

// Efektler birbirinden AYIRT EDİLEBİLİR olmalı.
func TestEfektlerBirbirindenFarkli(t *testing.T) {
	gorulen := map[string]Effect{}
	for _, e := range tumEfektler() {
		pcm := render(e)
		// Kaba bir parmak izi: uzunluk + birkaç noktadaki değer.
		iz := ""
		for i := 0; i < len(pcm); i += len(pcm)/8 + 1 {
			iz += string(rune('A' + (abs16(pcm[i])/2000)%26))
		}
		iz += string(rune('0' + len(pcm)%10))
		if onceki, var_ := gorulen[iz]; var_ {
			t.Errorf("efekt %v ile %v aynı parmak izini üretti (%q) — "+
				"kullanıcı ikisini ayırt edemez", e, onceki, iz)
		}
		gorulen[iz] = e
	}
}

// Bipçi her efekte AYRI nota dizisi çalmalı: eskiden hepsi 880 Hz idi.
func TestBipciEfektNotalariniKullanir(t *testing.T) {
	farkli := map[float64]bool{}
	for _, e := range tumEfektler() {
		tones := effects[e]
		if len(tones) == 0 {
			t.Errorf("efekt %v için nota tablosu boş — bipçi 880 Hz'e düşer", e)
			continue
		}
		farkli[tones[0].freq] = true
	}
	if len(farkli) < 3 {
		t.Errorf("yalnızca %d ayrı başlangıç frekansı var — "+
			"bipçide efektler birbirine benzer", len(farkli))
	}
}
