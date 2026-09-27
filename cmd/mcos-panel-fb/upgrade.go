package main

import (
	"errors"
	"fmt"
	"image"
	"os"
	"time"

	"mcos/internal/drm"
	"mcos/internal/fbdev"
	"mcos/internal/framebus"
)

// drmScreen is what the panel needs from drm.Screen (testte sahtesi var).
type drmScreen interface {
	Present(*image.RGBA) error
	Check() bool
	SetMode(string) error
	SetSaved(string)
	Size() (int, int)
	FramePeriod() time.Duration
	Info() drm.Info
	Blank(bool) bool
	Close()
}

// fbTarget is the firmware framebuffer (fbdev.Device; testte sahtesi var).
type fbTarget interface {
	Flip(*image.RGBA) error
	Size() (int, int)
	Close() error
}

// upgradeDisplay: monitörsüz açılışta framebuffer'la başlar, monitör
// takılınca EKRAN KARTI yoluna geçer.
//
// NEDEN: MCOS bir sunucu işletim sistemi; makine sık sık monitörsüz açılır,
// ekran sonradan bakmak için takılır. Ekran kartı açılışta "bağlı ekran yok"
// dediği için panel /dev/fb0'a düşüyordu ve ÖYLE KALIYORDU: monitör
// takıldığında görüntü çekirdeğin 1024x768'lik öykünme tamponundan
// geliyordu (monitörün doğal çözünürlüğü değil), tazeleme ~20 Hz'e kilitliydi
// ve Ekran bölümünde çözünürlük değiştirilemiyordu. Yeniden başlatmak
// gerekiyordu.
//
// Geçiş, çekirdeğin DRM hotplug olayıyla tetiklenir; olay kaçarsa diye 10
// saniyede bir de denenir (her deneme kartı açıp bağlantıları yoklar,
// monitörsüz çıkışta bu ucuzdur: HPD düşükken EDID okunmaz).
type upgradeDisplay struct {
	fb    fbTarget
	bus   *framebus.Writer
	hp    *drm.HotplugWatch
	saved string
	why   string

	drm     *drmDisplay // geçişten sonra dolu
	sonDene time.Time
	open    func(saved string) (drmScreen, error) // testte değiştirilir
}

const upgradeRetry = 10 * time.Second

func newUpgradeDisplay(fb *fbdev.Device, saved string, why error) *upgradeDisplay {
	u := &upgradeDisplay{fb: fb, saved: saved, hp: drm.WatchHotplug(), sonDene: time.Now(),
		open: func(s string) (drmScreen, error) {
			scr, err := drm.OpenScreen(s)
			if err != nil {
				return nil, err // (*drm.Screen)(nil) arayüze sarılıp nil-olmayan görünmesin
			}
			return scr, nil
		}}
	u.why = "monitör bağlı değildi"
	if why != nil && !errors.Is(why, drm.ErrNoMonitor) {
		u.why = why.Error()
	}
	w, h := fb.Size()
	// VNC her iki durumda da paneli aynı kanaldan görsün: geçişte kaynak
	// değişmez, yalnızca boyut değişir (Publish kendisi büyütür).
	if bus, err := framebus.Create(framebus.Path, w, h); err == nil {
		u.bus = bus
	}
	return u
}

func (u *upgradeDisplay) Flip(img *image.RGBA) error {
	if u.drm != nil {
		return u.drm.Flip(img)
	}
	err := u.fb.Flip(img)
	if u.bus != nil {
		u.bus.Publish(img)
	}
	return err
}

func (u *upgradeDisplay) Blank(off bool) bool {
	if u.drm != nil {
		return u.drm.Blank(off)
	}
	return false
}

func (u *upgradeDisplay) Size() (int, int) {
	if u.drm != nil {
		return u.drm.Size()
	}
	return u.fb.Size()
}

func (u *upgradeDisplay) FramePeriod() time.Duration {
	if u.drm != nil {
		return u.drm.FramePeriod()
	}
	return 0 // panelin varsayılanı
}

// Check runs once a second (panel poll): geçiş burada yapılır.
func (u *upgradeDisplay) Check() bool {
	if u.drm != nil {
		return u.drm.Check()
	}
	if u.bus != nil {
		u.bus.Heartbeat()
	}
	olay := u.hp.Take()
	if !olay && time.Since(u.sonDene) < upgradeRetry {
		return false
	}
	u.sonDene = time.Now()
	scr, err := u.open(u.saved)
	if err != nil {
		return false
	}
	u.drm = &drmDisplay{drmScreen: scr, bus: u.bus}
	// Ekran kartı kendi dinleyicisini açtı; bizimki artık gereksiz.
	u.hp.Close()
	u.hp = nil
	w, h := scr.Size()
	fmt.Fprintf(os.Stderr, "mcos-panel-fb: monitör bağlandı — ekran kartı yoluna geçildi: %s (%dx%d)\n",
		scr.Info().Summary(), w, h)
	// fbdev tamponu artık taranmıyor (DRM master bizde); kapat.
	u.fb.Close()
	return true
}

func (u *upgradeDisplay) Info() drm.Info {
	if u.drm != nil {
		return u.drm.Info()
	}
	w, h := u.fb.Size()
	return drm.Info{
		Backend: "fbdev",
		Device:  "/dev/fb0",
		Method:  "fbdev",
		Current: drm.Mode{Width: w, Height: h},
		Reason: "açılışta " + u.why + "; monitör takılınca ekran kartı yoluna " +
			"kendiliğinden geçilir (yeniden başlatmak gerekmez)",
	}
}

func (u *upgradeDisplay) SetMode(key string) error {
	if u.drm != nil {
		return u.drm.SetMode(key)
	}
	return errors.New("ekran kartı yolu henüz açılmadı (monitör bağlı değil)")
}

func (u *upgradeDisplay) SetSaved(key string) {
	u.saved = key
	if u.drm != nil {
		u.drm.SetSaved(key)
	}
}

// Close releases whatever is open (panel çıkarken).
func (u *upgradeDisplay) Close() {
	if u.drm != nil {
		u.drm.Close()
	} else {
		u.fb.Close()
	}
	u.hp.Close()
	if u.bus != nil {
		u.bus.Close()
	}
}
