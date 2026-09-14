package fbpanel

import (
	"errors"
	"image"
	"time"

	"mcos/internal/fbinput"
	"mcos/internal/fbui"
	"mcos/internal/model"
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
	// Pointer yields mouse/touchpad motion and clicks.
	//
	// nil bir kanal döndürmek GEÇERLİDİR: faresiz bir makinede (ya da
	// testte) select bu dalı hiç seçmez ve panel klavyeyle çalışmaya
	// devam eder.
	Pointer() <-chan fbinput.PointerEvent
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
	// Frame is the redraw interval while something is moving.
	//
	// Anim'den AYRI: dönen gösterge saniyede 12 kare yeterken, fare imleci
	// 60 kare ister. İkisini tek sayaca bağlamak ya imleci tökezletir ya da
	// göstergeyi delice döndürür.
	Frame time.Duration
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
		// ~60 kare/sn. Boştayken hiçbir şey çizilmez (yalnızca "kirli mi?"
		// kontrolü yapılır), bu yüzden maliyeti yok denecek kadar azdır.
		Frame: 16 * time.Millisecond,
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
	if opt.Frame <= 0 {
		opt.Frame = 16 * time.Millisecond
	}
	frame := time.NewTicker(opt.Frame)
	defer frame.Stop()

	// İLK AÇILIŞ: yapılandırma tamamlanmadıysa sihirbaz açılır.
	//
	// Sıra önemli: sihirbaz kilitten ÖNCE gelir. Henüz parola kurulmamış
	// bir sistemde kilit ekranı göstermek, kullanıcıyı hiç giremeyeceği bir
	// ekranda bırakırdı.
	a.refresh()
	if a.SetupNeeded() {
		a.StartSetup()
	} else {
		// Parola kuruluysa panel KİLİTLİ açılır. Kurulu değilse Lock()
		// hiçbir şey yapmaz — parola isteğe bağlıdır.
		a.Lock()
	}

	// İKİNCİ refresh() KALDIRILDI.
	//
	// Burada arka arkaya iki refresh() vardı. Aralarında yalnızca
	// StartSetup() ya da Lock() çalışır; ikisi de daemon'a HİÇ gitmez, yani
	// ikinci çağrı birincisinin aynısını getiriyordu.
	//
	// Bedeli ölçüldü: refresh() yeni açılmış bir mcosd'den sunucu, durum,
	// Java ve küme bilgisini toplayan bloklayan bir IPC çağrısıdır; QEMU'da
	// (donanım hızlandırması yok) ikisi birlikte ~1.7 saniye sürüyordu ve bu
	// süre boyunca ekranda açılış ekranının donmuş son karesi duruyordu.
	a.Draw()
	if err := disp.Flip(canvas); err != nil {
		return err
	}

	// AÇILIŞ GEÇİŞİNİN SAATİ BURADA BAŞLAR.
	//
	// Yukarıdaki refresh() mcosd'ye yapılan bloklayan bir IPC çağrısıdır ve
	// yeni açılmış bir daemon'dan sunucu/durum/Java/küme bilgisi toplar;
	// yavaş bir makinede saniyeleri bulur. Saat BeginIntro'da
	// başlasaydı 900 ms'lik yakınlaşma, HİÇBİR kare çizilmeden biterdi —
	// ölçüldü, tam olarak öyle oluyordu (bkz. transition.pending).
	//
	// Buraya kadar ekranda açılış ekranının son karesi durur; o kare zaten
	// ekranda olanın aynısı olduğu için bekleme görünmez.
	a.armIntro()

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
				a.lockAfterWake()
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

		case ev, ok := <-host.Pointer():
			if !ok {
				// İşaretleme aygıtı kayboldu (USB çıkarıldı): panel
				// klavyeyle çalışmaya devam etmeli.
				continue
			}
			if a.Wake() {
				disp.Blank(false)
				a.lockAfterWake()
				a.Draw()
				_ = disp.Flip(canvas)
				continue
			}
			switch a.Pointer(ev) {
			case ActQuit:
				return nil
			case ActSleep:
				a.Sleep()
				a.Draw()
				_ = disp.Flip(canvas)
				disp.Blank(true)
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
			// Klavye/fare etkinliği: yalnızca uyandırır.
			if a.Wake() {
				disp.Blank(false)
				a.lockAfterWake()
				a.Draw()
				_ = disp.Flip(canvas)
			}
			continue

		case <-frame.C:
			// Yalnızca bir şey değiştiyse çizer; boştayken bu dal
			// neredeyse bedavadır.
			if a.Asleep() {
				continue
			}
			// Pencere içi hareketler (yazma, silme, sarsılma) BU dala ait:
			// 80 ms'lik animasyon tikine bağlanırsa 150 ms'lik bir "yerine
			// oturma" iki kareye düşer ve hareket kekemeleşir.
			if a.needsFastRedraw() {
				a.Invalidate()
			}
			if !a.Dirty() {
				continue
			}
			a.Draw()
			if err := disp.Flip(canvas); err != nil {
				return err
			}
			continue

		case <-poll.C:
			// ARKA PLANDA: RPC'ler ana döngüyü BLOKLAMAMALI.
			//
			// ── Bulunan hata ─────────────────────────────────────────────
			// Etkin ağ taraması 25 saniye sürebiliyor ve IPC istemcisi
			// çağrıları tek bir kilitle sıralıyor. Yoklama ana döngüde
			// yapıldığında, tarama süresince hiçbir kare çizilmiyordu —
			// yani "aranıyor" göstergesi tam da ararken DONUYORDU.
			//
			// Artık yoklama ayrı bir goroutine'de; döngü çizmeye devam eder.
			a.refreshAsync()
			if a.Asleep() {
				// Uykudayken durum yine toplanır (olay geçmişi eksik
				// kalmasın), ama ekrana hiçbir şey basılmaz.
				continue
			}

		case <-anim.C:
			if a.Asleep() {
				continue
			}
			busy := a.Tick()
			if a.setupTick() {
				busy = true
			}
			if !busy {
				continue
			}
			a.Invalidate()
		}

		// Onay penceresinden gelen guc eylemi: modal kapaninca burada
		// islenir. Yikici islemler tek Enter ile degil, onaydan sonra
		// calisir.
		switch a.TakePending() {
		case ActReboot:
			a.Emit(fbui.EventBusy, "Yeniden baslatiliyor...")
			a.Draw()
			_ = disp.Flip(canvas)
			return host.Power("reboot")
		case ActPoweroff:
			a.Emit(fbui.EventBusy, "Kapatiliyor...")
			a.Draw()
			_ = disp.Flip(canvas)
			return host.Power("poweroff")
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

// refreshAsync polls the daemon without blocking the caller.
//
// TEK UÇUŞLU: bir yenileme sürerken ikincisi başlatılmaz. Aksi halde yavaş
// bir daemon'da her saniye yeni bir goroutine birikir ve hepsi aynı IPC
// kilidinde sıraya girer.
func (a *App) refreshAsync() {
	if !a.refreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.refreshing.Store(false)
		a.refresh()
	}()
}

// saveConfigAsync persists a config change without blocking the run loop.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Ayar yazan altı yer (tema, fare/dokunmatik yüzey, parola, uykuda kilit,
// PC paylaşımı, eş eşleştirme) UpdateConfig'i ANA DÖNGÜDE çağırıyordu.
// ipc.Client tüm çağrıları TEK bir kilitle sıraya dizer; bir ağ taraması
// sürerken o kilit 25 saniyeye kadar tutulu kalır.
//
// Sonuç: kullanıcı tarama sürerken bir ayarı açıp kapattığında panel
// tamamen donuyordu — çizim de, tuş da, fare de. Kaydetme işlemi kısa
// olduğu için bu hata ancak tarama sırasında görünürdü, yani en çok
// kullanıldığı anda.
//
// Ekran ZATEN iyimser güncellenmiştir (çağıran a.cfg'yi değiştirip dirty
// işaretler), yani kullanıcı değişikliği anında görür; diske yazmanın
// beklemesi görünmez. Yalnızca başarısızlık bildirilir.
//
// onOK, yazma başarılı olduğunda çalışır; çevrimdışıyken de çalışır, çünkü
// o durumda yazılacak bir yer yoktur ve değişiklik zaten bellektedir.
func (a *App) saveConfigAsync(next *model.Config, failMsg string, onOK func()) {
	if a.offline() {
		if onOK != nil {
			onOK()
		}
		return
	}
	// KOPYA: çağıran next'i sonradan değiştirirse goroutine yarı yazılmış
	// bir yapılandırmayı diske geçirirdi.
	cp := *next
	go func() {
		if err := a.cl.UpdateConfig(&cp); err != nil {
			a.Fail(failMsg, err)
			return
		}
		if onOK != nil {
			onOK()
		}
	}()
}

// loadSectionAsync fetches a section's data off the run loop.
//
// Bölüme girer girmez yapılan RPC'ler normalde hızlıdır, ama bir tarama
// sürerken IPC kilidi tutulu olur ve bu çağrı saniyelerce bekleyebilir.
// Ana döngüde beklemek arayüzü dondurur.
func (a *App) loadSectionAsync() {
	go a.loadSection()
}

// refresh polls the daemon for fresh state.
//
// Her çağrının hatası AYRI ele alınır: durum alınamıyorsa sunucu listesi yine
// de güncellenebilir. Tek bir hatada her şeyi bırakmak, geçici bir RPC
// hatasında ekranı tamamen dondururdu.
func (a *App) refresh() {
	if a.offline() {
		return
	}
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

// lockAfterWake re-locks the panel when the user asked for that.
//
// Uyku, ekranı kapatır ama sistemi çalışır bırakır. Parola kurulmuş ve
// "uyandığında sor" seçilmişse, ekranın açılması paneli de açmamalı —
// yoksa uyku, kilidi atlamanın en kolay yolu olurdu.
func (a *App) lockAfterWake() {
	if a.lockOnSleepWanted() {
		a.Lock()
	}
}
