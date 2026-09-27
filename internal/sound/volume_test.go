//go:build linux

package sound

import "testing"

// ════════════════════════════════════════════════════════════════════════════
// SES SEVİYESİ
// ════════════════════════════════════════════════════════════════════════════
//
// Ses seviyesi hiçbir yerden ayarlanamıyordu: açılışta bir kez amixer'e SABİT
// "%70" yazılıyor ve bir daha dokunulmuyordu. Bu testler yeni davranışı
// kilitler — özellikle SINIRLARI, çünkü bir kırpma hatası "ses tuşu artık
// hiçbir şey yapmıyor" olarak görünür.

func TestSeviyeSinirlariKirpiliyor(t *testing.T) {
	if v := SetVolume(150); v != 100 {
		t.Errorf("150 -> %d; 100'e kırpılmalı", v)
	}
	if v := SetVolume(-20); v != 0 {
		t.Errorf("-20 -> %d; 0'a kırpılmalı", v)
	}
}

func TestArtirAzalt(t *testing.T) {
	SetVolume(50)
	if v := VolumeUp(); v != 50+volumeStep {
		t.Errorf("artır -> %d; %d bekleniyordu", v, 50+volumeStep)
	}
	SetVolume(50)
	if v := VolumeDown(); v != 50-volumeStep {
		t.Errorf("azalt -> %d; %d bekleniyordu", v, 50-volumeStep)
	}
}

// Sınırda TAKILMAMALI: tavanda "artır", tabanda "azalt" geçerli kalmalı.
func TestSinirdaTakilmiyor(t *testing.T) {
	SetVolume(100)
	if v := VolumeUp(); v != 100 {
		t.Errorf("tavanda artır -> %d; 100 kalmalı", v)
	}
	SetVolume(0)
	if v := VolumeDown(); v != 0 {
		t.Errorf("tabanda azalt -> %d; 0 kalmalı", v)
	}
}

// Sessize alıp geri açınca ESKİ seviye gelmeli: sıfırdan geri açan kullanıcı
// yirmi kez tuşa basmak zorunda kalmamalı.
func TestSessizeAlmaSeviyeyiHatirliyor(t *testing.T) {
	SetVolume(35)
	if v, sessiz := ToggleMute(); v != 0 || !sessiz {
		t.Fatalf("sessize alma -> (%d, %v); (0, true) bekleniyordu", v, sessiz)
	}
	if v, sessiz := ToggleMute(); v != 35 || sessiz {
		t.Errorf("geri açma -> (%d, %v); (35, false) bekleniyordu", v, sessiz)
	}
}

// Zaten sessizken sessize almak, geri dönülecek seviyeyi SIFIRLAMAMALI.
func TestSessizkenTekrarSessizlemeBozmuyor(t *testing.T) {
	SetVolume(40)
	ToggleMute() // 0
	ToggleMute() // 40
	ToggleMute() // 0
	if v, _ := ToggleMute(); v != 40 {
		t.Errorf("geri açma -> %d; 40 bekleniyordu", v)
	}
}

func TestAmixerYuzdeCozumleme(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		giris string
		bekle int
		ok    bool
	}{
		{"tipik", "  Front Left: Playback 65535 [100%] [0.00dB] [on]", 100, true},
		{"orta", "Mono: Playback 45 [70%] [-10.00dB] [on]", 70, true},
		{"sifir", "Mono: Playback 0 [0%] [off]", 0, true},
		{"yuzde yok", "Simple mixer control 'Master',0", 0, false},
		{"bos", "", 0, false},
		{"gecersiz", "[abc%]", 0, false},
		{"aralik disi", "[300%]", 0, false},
	} {
		v, ok := parseAmixerPercent(tc.giris)
		if ok != tc.ok || (ok && v != tc.bekle) {
			t.Errorf("%s: (%d,%v); (%d,%v) bekleniyordu",
				tc.ad, v, ok, tc.bekle, tc.ok)
		}
	}
}

// Adım, kullanılabilir olmalı: çok büyük kaba, çok küçük "hiçbir şey olmuyor".
func TestAdimMakul(t *testing.T) {
	if volumeStep < 2 || volumeStep > 10 {
		t.Errorf("adım %%%d — 2 ile 10 arasında olmalı", volumeStep)
	}
}
