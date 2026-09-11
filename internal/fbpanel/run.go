package fbpanel

import (
	"errors"
	"image"
	"time"

	"mcos/internal/fbinput"
	"mcos/internal/fbui"
)

// ErrLegacyPanel is returned when the user asks for the old Bubble Tea panel.
//
// Hata DEĞİL, bir istek: çağıran bunu görüp eski paneli açar. Ayrı bir değer
// olarak tanımlı, çünkü normal çıkışla aynı şeyi döndürseydik F12 yeni paneli
// yeniden açardı ve kullanıcı yeni arayüzde bir sorun olduğunda kilitli
// kalırdı. mcos-launch bunu çıkış kodundan ayırt eder.
var ErrLegacyPanel = errors.New("kullanıcı eski paneli istedi")

// Display is what the run loop needs from the screen.
//
// Arayüz olarak tanımlandı ki test bir sahte ekranla çalışabilsin: gerçek
// framebuffer olmadan tüm döngü sınanabiliyor.
type Display interface {
	// Flip pushes the canvas to the screen.
	Flip(*image.RGBA) error
	// Blank turns the display off/on. Returns false if the hardware
	// cannot actually be blanked (firmware framebuffer).
	Blank(off bool) bool
}

// Host is what the run loop needs from the outside world.
type Host interface {
	// Keys yields decoded keystrokes.
	Keys() <-chan fbinput.Key
	// Activity fires on any input (used to wake from sleep).
	Activity() <-chan struct{}
	// Power performs a reboot/poweroff after restoring the console.
	Power(action string) error
}

// Options tune the run loop.
type Options struct {
	// Poll is how often daemon state is refreshed.
	Poll time.Duration
	// Anim is the animation tick (spinner).
	Anim time.Duration
	// EscTimeout is how long to wait before deciding a lone ESC is Esc.
	EscTimeout time.Duration
	// IdleSleep blanks the screen after this much inactivity. Zero disables.
	IdleSleep time.Duration
}

// DefaultOptions returns sane timings.
func DefaultOptions() Options {
	return Options{
		Poll: time.Second,
		// 80 ms ≈ 12 kare/sn: dönen gösterge akıcı görünür ama boştaki bir
		// sunucu makinesinde CPU yakmaz. Zaten yalnızca bir şey dönüyorsa
		// yeniden çizim yapılır (bkz. App.Tick).
		Anim:       80 * time.Millisecond,
		EscTimeout: 50 * time.Millisecond,
		// Varsayılan olarak kendiliğinden uyumaz: bir sunucu makinesinin
		// ekranı beklenmedik şekilde kararırsa kullanıcı çöktüğünü sanır.
		// Uyku yalnızca Güç menüsünden açılır.
		IdleSleep: 0,
	}
}

// Run drives the panel until the user quits.
//
// Döngü OLAY GÜDÜMLÜ: hiçbir şey değişmediyse çizim yapılmaz. Sürekli kare
// basmak, üzerinde Minecraft sunucusu çalışan bir makinede boşuna CPU demek.
func (a *App) Run(canvas *image.RGBA, disp Display, host Host, opt Options) error {
	poll := time.NewTicker(opt.Poll)
	defer poll.Stop()
	anim := time.NewTicker(opt.Anim)
	defer anim.Stop()

	a.refresh()
	a.Draw()
	if err := disp.Flip(canvas); err != nil {
		return err
	}

	for {
		select {
		case k, ok := <-host.Keys():
			if !ok {
				return nil
			}
			// Uykudayken HERHANGİ bir tuş yalnızca uyandırır; o tuş
			// arayüzde bir eylem tetiklemez. Aksi halde kullanıcı ekranı
			// açmak için bastığı tuşla yanlışlıkla sunucu durdurabilirdi.
			if a.Wake() {
				disp.Blank(false)
				a.Draw()
				_ = disp.Flip(canvas)
				continue
			}
			switch a.Key(k.String()) {
			case ActQuit:
				return nil
			case ActLegacyPanel:
				// AYRI bir dönüş değeri: mcos-launch bunu görüp ESKİ paneli
				// açar. Normal çıkışla aynı şeyi döndürseydik, F12 yeni
				// paneli yeniden açardı ve kullanıcı yeni arayüzde bir sorun
				// olduğunda kilitli kalırdı.
				return ErrLegacyPanel
			case ActSleep:
				a.Sleep()
				a.Draw()
				_ = disp.Flip(canvas)
				if disp.Blank(true) {
					a.SetSleepNote("Ekran donanımda kapatıldı.")
				} else {
					a.SetSleepNote("Bu ekran kartı donanımda kapatılamıyor; " +
						"ekran karartıldı (firmware framebuffer).")
				}
				continue
			case ActReboot:
				a.Emit(fbui.EventBusy, "Yeniden başlatılıyor…")
				a.Draw()
				_ = disp.Flip(canvas)
				return host.Power("reboot")
			case ActPoweroff:
				a.Emit(fbui.EventBusy, "Kapatılıyor…")
				a.Draw()
				_ = disp.Flip(canvas)
				return host.Power("poweroff")
			}

		case <-host.Activity():
			// Fare hareketi: yalnızca uyandırır.
			if a.Wake() {
				disp.Blank(false)
				a.Draw()
				_ = disp.Flip(canvas)
			}
			continue

		case <-poll.C:
			if a.Asleep() {
				// Uykudayken de durum toplanır (olay geçmişi eksik kalmasın),
				// ama ekrana hiçbir şey basılmaz.
				a.refresh()
				continue
			}
			a.refresh()

		case <-anim.C:
			if a.Asleep() {
				continue
			}
			if !a.Tick() {
				continue
			}
			a.Invalidate()
		}

		if a.Asleep() || !a.Dirty() {
			continue
		}
		a.Draw()
		if err := disp.Flip(canvas); err != nil {
			return err
		}
	}
}

// refresh polls the daemon for fresh state.
//
// Her çağrının hatası AYRI ele alınır: durum alınamıyorsa sunucu listesi yine
// de güncellenebilir. Tek bir hatada her şeyi bırakmak, geçici bir RPC
// hatasında ekranı tamamen dondururdu.
func (a *App) refresh() {
	if st, err := a.cl.Status(); err == nil {
		a.SetStatus(st)
	} else {
		a.Fail("durum alınamadı", err)
	}
	if sv, err := a.cl.Servers(); err == nil {
		a.SetServers(sv)
	}
	if _, _, cfg := a.Snapshot(); cfg == nil {
		if c, err := a.cl.Config(); err == nil {
			a.SetConfig(c)
		}
	}
}
