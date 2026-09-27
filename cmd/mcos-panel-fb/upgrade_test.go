package main

import (
	"errors"
	"image"
	"testing"
	"time"

	"mcos/internal/drm"
)

type sahteFB struct {
	w, h    int
	kareler int
	kapali  bool
}

func (f *sahteFB) Flip(*image.RGBA) error { f.kareler++; return nil }
func (f *sahteFB) Size() (int, int)       { return f.w, f.h }
func (f *sahteFB) Close() error           { f.kapali = true; return nil }

type sahteEkran struct {
	w, h    int
	kareler int
}

func (s *sahteEkran) Present(*image.RGBA) error  { s.kareler++; return nil }
func (s *sahteEkran) Check() bool                { return false }
func (s *sahteEkran) SetMode(string) error       { return nil }
func (s *sahteEkran) SetSaved(string)            {}
func (s *sahteEkran) Size() (int, int)           { return s.w, s.h }
func (s *sahteEkran) FramePeriod() time.Duration { return time.Second / 144 }
func (s *sahteEkran) Info() drm.Info {
	return drm.Info{Backend: "drm", Current: drm.Mode{Width: s.w, Height: s.h}}
}
func (s *sahteEkran) Blank(bool) bool { return true }
func (s *sahteEkran) Close()          {}

// Monitörsüz açılış: panel framebuffer'la başlar; monitör takılınca (kart
// artık açılabiliyor) ekran kartı yoluna, monitörün boyutu ve tazelemesiyle
// geçer. Eskiden panel sonsuza dek 1024x768 fbdev'de kalıyordu.
func TestMonitorsuzAcilistaMonitorTakilincaEkranKartinaGecer(t *testing.T) {
	fb := &sahteFB{w: 1024, h: 768}
	ekran := &sahteEkran{w: 2560, h: 1440}
	denemeler := 0
	monitorVar := false
	u := &upgradeDisplay{fb: fb, sonDene: time.Now(), why: "monitör bağlı değildi",
		open: func(string) (drmScreen, error) {
			denemeler++
			if !monitorVar {
				return nil, drm.ErrNoMonitor
			}
			return ekran, nil
		}}

	img := image.NewRGBA(image.Rect(0, 0, 1024, 768))
	_ = u.Flip(img)
	if fb.kareler != 1 || u.FramePeriod() != 0 {
		t.Fatalf("başlangıçta kareler framebuffer'a gitmeli (fb=%d)", fb.kareler)
	}
	if u.Info().Changeable || u.SetMode("1920x1080@60000") == nil {
		t.Fatal("fbdev durumunda mod değiştirilebilir görünmemeli")
	}

	// Hotplug olayı yok ve 10 sn dolmadı: kart HER SANİYE yoklanmamalı
	// (her yoklama bağlantıları zorla tarar).
	for i := 0; i < 5; i++ {
		if u.Check() {
			t.Fatal("monitör yokken geçiş bildirildi")
		}
	}
	if denemeler != 0 {
		t.Fatalf("olay yokken %d kez yoklandı; %v dolmadan yoklanmamalı", denemeler, upgradeRetry)
	}

	// Süre doldu, monitör hâlâ yok: bir deneme, geçiş yok.
	u.sonDene = time.Now().Add(-upgradeRetry - time.Second)
	if u.Check() || denemeler != 1 {
		t.Fatalf("süre dolunca bir kez denenmeli (deneme=%d)", denemeler)
	}

	// Monitör takıldı.
	monitorVar = true
	u.sonDene = time.Now().Add(-upgradeRetry - time.Second)
	if !u.Check() {
		t.Fatal("monitör takıldığı hâlde ekran kartı yoluna geçilmedi")
	}
	if w, h := u.Size(); w != 2560 || h != 1440 {
		t.Fatalf("geçişten sonra boyut %dx%d, beklenen 2560x1440", w, h)
	}
	if u.FramePeriod() != time.Second/144 {
		t.Fatalf("geçişten sonra kare periyodu %v; monitörün tazelemesi kullanılmalı", u.FramePeriod())
	}
	if !fb.kapali {
		t.Fatal("geçişten sonra framebuffer kapatılmadı")
	}
	_ = u.Flip(image.NewRGBA(image.Rect(0, 0, 2560, 1440)))
	if ekran.kareler != 1 || fb.kareler != 1 {
		t.Fatalf("geçişten sonra kareler ekran kartına gitmeli (drm=%d fb=%d)", ekran.kareler, fb.kareler)
	}
	if u.Info().Backend != "drm" {
		t.Fatalf("Info hâlâ %q", u.Info().Backend)
	}
}

// Kart açılabilir ama hata ErrNoMonitor DEĞİLSE (CPU tamponu yok vb.)
// panel geçiş beklemez: bu hatalar kalıcı.
func TestKaliciHatadaGecisBeklenmez(t *testing.T) {
	if !errors.Is(errors.Join(drm.ErrNoMonitor), drm.ErrNoMonitor) {
		t.Fatal("ErrNoMonitor sarmalanınca tanınmıyor")
	}
	if errors.Is(errors.New("drm: kullanılabilir ekran yok"), drm.ErrNoMonitor) {
		t.Fatal("başka bir hata ErrNoMonitor sanıldı")
	}
}
