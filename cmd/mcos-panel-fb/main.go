// Command mcos-panel-fb draws the MCOS interface straight to the framebuffer.
//
// ── Eski panelden farkı ─────────────────────────────────────────────────────
// mcos-panel bir TERMİNAL uygulamasıdır: fbterm içinde çalışır, her şeyi
// karakterlerle çizer. Bu ikili ise /dev/fb0'a doğrudan piksel yazar. Aradaki
// fbterm ve font yığını tamamen ortadan kalkar; çerçeveler gerçek vektör
// şekillerdir ve açılır pencerelerin arkası gerçekten bulanıklaşır.
//
// ── Güvenlik ────────────────────────────────────────────────────────────────
// Konsolu grafik kipine alıyoruz. Program çökerse konsol kullanılamaz kalır,
// bu yüzden geri yükleme HER çıkış yolunda çalışır: defer, sinyal işleyicisi
// ve panik kurtarma. Bu dosyadaki en önemli satır o defer'dır.
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strings"
	"time"

	"mcos/internal/drm"
	"mcos/internal/fbdev"
	"mcos/internal/fbfont"
	"mcos/internal/fbinput"
	"mcos/internal/fbpanel"
	"mcos/internal/fbui"
	"mcos/internal/fbvt"
	"mcos/internal/framebus"
	"mcos/internal/ipcclient"
)

func main() {
	var (
		sock    = flag.String("connect", "/run/mcosd.sock", "mcosd soket yolu")
		fbPath  = flag.String("fb", "/dev/fb0", "framebuffer aygıtı")
		ttyPath = flag.String("tty", "/dev/tty", "konsol aygıtı")
		fontPx  = flag.Float64("font", 0, "yazı boyutu (piksel); 0 = ekrana göre otomatik")
		shot    = flag.String("screenshot", "", "bir kare çizip PNG olarak kaydet ve çık")
		shotSec = flag.Int("section", 0, "ekran görüntüsü: hangi bölüm (0..11)")
		shotFoc = flag.Bool("content", false, "ekran görüntüsü: odak içerik sütununda olsun")
		shotMod = flag.String("modal", "", "ekran görüntüsü: açılır pencere (list|password|confirm)")
		intro   = flag.String("intro", "/dev/.mcos/frame.rgba",
			"açılış ekranının son karesi (geçiş animasyonu için)")
		ready = flag.String("ready", "/dev/.mcos/ready",
			"panel hazır olunca bu dosyayı oluştur (açılış ekranı buna bakar)")
		forceSetup = flag.Bool("setup", false,
			"kurulum sihirbazını yapılandırma tamamlanmış olsa da aç")
		shotSetup = flag.Int("setup-page", -1,
			"ekran görüntüsü: kurulum sihirbazının şu sayfası (0..9)")
		shotPower = flag.String("power", "",
			"ekran görüntüsü: kapanış ekranı (poweroff|reboot)")
		shotWizard = flag.Int("wizard-page", -1,
			"ekran görüntüsü: sunucu sihirbazının şu sayfası (0..9)")
	)
	flag.Parse()

	err := run(runOpts{
		sock: *sock, fbPath: *fbPath, ttyPath: *ttyPath, fontPx: *fontPx,
		shot: *shot, shotSec: *shotSec, shotFoc: *shotFoc, shotMod: *shotMod,
		intro: *intro, ready: *ready,
		forceSetup: *forceSetup, shotSetup: *shotSetup,
		shotWizard: *shotWizard,
		shotPower:  *shotPower,
	})
	switch {
	case err == nil:
		return
	case errors.Is(err, fbpanel.ErrLegacyPanel):
		// F12: kullanıcı eski paneli istedi. Bu bir HATA DEĞİL, bu yüzden
		// stderr'e bir şey yazılmaz; mcos-launch yalnızca çıkış koduna bakar.
		os.Exit(exitLegacyPanel)
	case errors.Is(err, fbpanel.ErrPowerPending):
		// Kapatma/yeniden başlatma isteği çekirdeğe iletildi. Hiçbir şey
		// YAZILMAZ: ekran kapanış animasyonunun bıraktığı siyahta kalmalı.
		os.Exit(exitPowerPending)
	default:
		fmt.Fprintln(os.Stderr, "mcos-panel-fb:", err)
		os.Exit(1)
	}
}

// exitLegacyPanel, "eski paneli aç" isteğinin çıkış kodudur.
//
// 64 seçildi: 1 (genel hata) ve 2 (kullanım hatası) ile karışmaz, 128+ aralığı
// ise sinyalle ölüme ayrılmıştır. mcos-launch bu kodu GERİ DÜŞÜŞ olarak değil,
// AÇIK BİR İSTEK olarak yorumlar.
const exitLegacyPanel = 64

// exitPowerPending, "kapatma/yeniden başlatma yolda" çıkış kodudur.
//
// mcos-launch bunu görünce ekranı siyah bırakır ve paneli YENİDEN AÇMAZ.
// Ayrı bir kod olmasaydı (yani 0 dönseydi) kurtarma menüsü kapanış
// animasyonunun üstüne basılır ve 15 saniye sonra panel kapanmakta olan
// sistemde yeniden açılırdı — ölçüldü, tam olarak bu oluyordu.
const exitPowerPending = 65

// runOpts groups the command-line options.
//
// Sekiz konumsal argüman, çağrı yerinde hangisinin hangisi olduğunu
// okunmaz hale getiriyordu; adlandırılmış alanlar bunu çözer.
type runOpts struct {
	sock, fbPath, ttyPath string
	fontPx                float64
	shot                  string
	shotSec               int
	shotFoc               bool
	shotMod               string
	intro, ready          string
	forceSetup            bool
	shotSetup             int
	shotWizard            int
	shotPower             string
}

func run(o runOpts) error {
	sock, fbPath, ttyPath := o.sock, o.fbPath, o.ttyPath
	fontPx, shot := o.fontPx, o.shot
	shotSec, shotFoc, shotMod := o.shotSec, o.shotFoc, o.shotMod

	cl, err := ipcclient.Dial(sock)
	if err != nil {
		// Ekran görüntüsü kipinde daemon ZORUNLU DEĞİL: arayüz, çalışan bir
		// sistem olmadan (derleme makinesinde, gözden geçirmede) da
		// çizilebilmeli. Etkileşimli kipte ise bağlantı şart.
		if shot == "" {
			return fmt.Errorf("mcosd'a bağlanılamadı (%s): %w", sock, err)
		}
		cl = nil
	}

	// ── Ekran ───────────────────────────────────────────────────────────────
	var (
		canvas *image.RGBA
		disp   fbpanel.Display
		live   fbpanel.LiveDisplay
		w, h   int
	)
	if shot != "" {
		// Ekran görüntüsü kipi: gerçek framebuffer gerekmez. Bu sayede
		// arayüz, donanım olmadan da (derleme makinesinde, testte)
		// görülebilir ve gözden geçirilebilir.
		w, h = 1920, 1080
		canvas = image.NewRGBA(image.Rect(0, 0, w, h))
		disp = nullDisplay{}
	} else if scr, err := openScreen(fbpanel.ReadDisplayMode()); err == nil {
		// ── EKRAN KARTI (DRM/KMS) ─────────────────────────────────────────
		//
		// Birincil yol. Panel modu kendisi kurar (monitörün doğal
		// çözünürlüğü, en yüksek tazeleme), kareleri sayfa çevirmeyle
		// monitörün hızında basar ve modu ÇALIŞIRKEN değiştirebilir.
		defer scr.Close()
		w, h = scr.Size()
		canvas = image.NewRGBA(image.Rect(0, 0, w, h))
		dd := &drmDisplay{drmScreen: scr}
		if bus, err := framebus.Create(framebus.Path, w, h); err == nil {
			dd.bus = bus
			defer bus.Close()
		} else {
			fmt.Fprintln(os.Stderr, "mcos-panel-fb: ekran paylaşımı kanalı açılamadı "+
				"(VNC paneli göremeyecek):", err)
		}
		disp, live = dd, dd
		info := scr.Info()
		fmt.Fprintln(os.Stderr, "mcos-panel-fb: ekran:", info.Summary())
		// Tam mod listesi de günlükte: kullanıcı "çözünürlük düşük" dediğinde
		// monitörün NE sunduğu ve NEYİN seçildiği tek bakışta görülsün.
		var ml []string
		for _, m := range info.Modes {
			ml = append(ml, fmt.Sprintf("%dx%d@%s", m.Width, m.Height, m.HzText()))
		}
		fmt.Fprintf(os.Stderr, "mcos-panel-fb: modlar (%s): %s\n", info.Why, strings.Join(ml, " "))
	} else {
		// ── FIRMWARE FRAMEBUFFER (geri düşüş) ─────────────────────────────
		//
		// DRM açılamadıysa NEDENİ günlüğe yazılır; Ekran bölümü de bu yolda
		// çözünürlüğün neden çalışırken değişmediğini söyler.
		fmt.Fprintln(os.Stderr, "mcos-panel-fb: ekran kartı yolu kullanılamıyor, "+
			"framebuffer'a düşülüyor:", err)
		drmErr := err
		dev, err := fbdev.Open(fbPath)
		if err != nil {
			return fmt.Errorf("framebuffer açılamadı (%s): %w", fbPath, err)
		}
		w, h = dev.Size()
		canvas = image.NewRGBA(image.Rect(0, 0, w, h))
		if errors.Is(drmErr, drm.ErrNoMonitor) {
			// Kart sağlam, yalnızca monitör yok: takılınca ekran kartı
			// yoluna geçilir (bkz. upgradeDisplay).
			fmt.Fprintln(os.Stderr, "mcos-panel-fb: monitör takılınca ekran kartı yoluna geçilecek")
			u := newUpgradeDisplay(dev, fbpanel.ReadDisplayMode(), drmErr)
			defer u.Close()
			disp, live = u, u
		} else {
			defer dev.Close()
			disp = &fbDisplay{dev: dev}
		}
	}

	// ── Yazı tipi ───────────────────────────────────────────────────────────
	if fontPx <= 0 {
		fontPx = fbpanel.AutoFontSize(h)
	}
	face, err := fbfont.Load(fontPx)
	if err != nil {
		return err
	}
	defer face.Close()

	ui := fbui.NewUI(canvas, face, fbui.DefaultPalette)
	app := fbpanel.New(ui, cl)
	if live != nil {
		app.SetLiveDisplay(live)
	}

	// Ekran bolumu GERCEK cozunurlugu gostermeli: kullanici tercihinin
	// uygulanip uygulanmadigini ancak boyle anlar.
	app.SetScreenSize(w, h)
	if out, err := exec.Command("mcos-display", "pref").Output(); err == nil {
		app.SetDisplayPref(strings.TrimSpace(string(out)))
	}

	if shot != "" {
		// Ekran görüntüsü kipi: ses çalma, arka plan goroutine'i başlatma.
		app.SetHeadless(true)
		if cl == nil {
			// Gercek veri yok: ornek veriyle ciz ki ekranin dolu hali
			// gorulebilsin. Bu YALNIZCA ekran goruntusu kipindedir;
			// calisan sistemde her zaman gercek veri gosterilir.
			fbpanel.FillDemo(app)
		}
		switch {
		case o.shotSetup >= 0:
			fbpanel.DemoSetup(app, o.shotSetup)
		case o.shotWizard >= 0:
			fbpanel.DemoWizard(app, o.shotWizard)
		default:
			fbpanel.DemoView(app, shotSec, shotFoc)
		}
		if shotMod != "" {
			fbpanel.DemoModal(app, shotMod)
		}
		if o.shotPower != "" {
			// Kapanış ekranı Draw()'un YERİNE geçer: panelin üstüne
			// çizilmez, panelin yerini alır.
			fbpanel.DemoPower(app, o.shotPower)
		} else {
			app.Draw()
		}
		if err := fbdev.SavePNG(canvas, shot); err != nil {
			return err
		}
		fmt.Printf("yazıldı: %s (%dx%d, yazı %.0fpx, hücre %dx%d)\n",
			shot, w, h, fontPx, face.CellW, face.CellH)
		return nil
	}

	// ── Konsol devralma ─────────────────────────────────────────────────────
	con, err := fbvt.Open(ttyPath, fbPath)
	if err != nil {
		return fmt.Errorf("konsol devralınamadı: %w", err)
	}
	// EN ÖNEMLİ SATIR: panik olsa bile konsol geri verilir.
	defer con.Restore()
	defer func() {
		if r := recover(); r != nil {
			con.Restore()
			panic(r)
		}
	}()

	act := fbinput.WatchActivity()
	defer act.Close()
	// TEŞHİS: kullanıcı "touchpad yok" dediğinde ilk soru "sistem onu görüyor
	// mu?" olur. Bulunan işaretçiler ve sonradan gelenler günlüğe yazılıyor.
	if ps := act.Pointers(); len(ps) > 0 {
		fmt.Fprintf(os.Stderr, "mcos-panel-fb: girdi: %d aygıt, işaretçiler: %s\n",
			act.Devices(), strings.Join(ps, ", "))
	} else {
		fmt.Fprintf(os.Stderr, "mcos-panel-fb: girdi: %d aygıt, HİÇ işaretçi (fare/touchpad) "+
			"bulunamadı — geç gelen aygıtlar 2 sn'de bir aranıyor\n", act.Devices())
	}
	// Touchpad ham değerlerini piksele çevirmek için ekran boyutu gerekir.
	act.SetScreen(w, h)
	app.SetPointerDevices(act.Pointers())
	act.OnNewDevice(func(ad string, k fbinput.Kind) {
		fmt.Fprintf(os.Stderr, "mcos-panel-fb: yeni girdi aygıtı: %s (%s)\n", ad, k)
		app.SetPointerDevices(act.Pointers())
	})

	host := &console{con: con, act: act, cl: cl}
	keys := host.start(fbpanel.DefaultOptions().EscTimeout, act)
	defer close(host.stop)

	// ── Açılış geçişi ───────────────────────────────────────────────────
	// Açılış ekranı (mcos-splash) son karesini kaydetmişse, panel oradan
	// YAKINLAŞARAK açılır. Kullanıcının isteği tam olarak buydu:
	// "boot animasyonu bitince içeri zoomlanarak blur felan ile OOBE'nin
	// ilk ekranı gelsin".
	//
	// Kare bir KEZ kullanılır ve silinir: ikinci kez panele girildiğinde
	// (F12 ile eski panele geçip dönmek gibi) aynı animasyonu tekrarlamak
	// yavaş hissettirir.
	if o.intro != "" {
		if fr := fbdev.LoadFrame(o.intro, canvas.Bounds()); fr != nil {
			app.BeginIntro(fr)
			_ = os.Remove(o.intro)
		}
	}

	// Açılış ekranına "hazırım" de. Bunu ÇİZİMDEN ÖNCE yapmıyoruz: splash
	// hemen çıkarsa ekran bir an siyah kalır.
	app.Draw()
	_ = disp.Flip(canvas)
	if o.ready != "" {
		markReady(o.ready)
		defer os.Remove(o.ready)
	}

	if o.forceSetup {
		app.StartSetup()
	}

	err = app.Run(canvas, disp, host, fbpanel.DefaultOptions())
	_ = keys
	return err
}

// markReady creates the flag file the splash screen waits for.
//
// Hata YUTULUR: /run yazılamıyorsa (salt okunur kök, tuhaf bir kurulum)
// açılış ekranı kendi zaman aşımıyla çıkar. Panel bu yüzden açılmamalı.
func markReady(path string) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	_ = f.Close()
}

// ── Ekran uyarlayıcıları ────────────────────────────────────────────────────

// drmDisplay adapts drm.Screen to the panel.
//
// Kare HATASI PANELİ KAPATMAZ: eskiden Flip bir hata döndürünce döngü
// kapanıyor, kullanıcı konsola düşüyordu. Sürücü devralmasının ortasında ya
// da geçici bir -EBUSY'de bu yanlış; hata günlüğe yazılır (saniyede bir
// taşmasın diye seyreltilerek) ve bir sonraki kare denenir. Aygıt tamamen
// kaybolduysa drm.Screen zaten yenisini arıyor.
//
// Kareler ayrıca framebus'a yayımlanır: uzaktan ekran paylaşımı (VNC) artık
// /dev/fb0'dan değil oradan okuyor. İzleyen yoksa yayın bedava.
type drmDisplay struct {
	drmScreen
	bus     *framebus.Writer
	sonHata time.Time
}

func (d *drmDisplay) Flip(img *image.RGBA) error {
	if err := d.Present(img); err != nil && time.Since(d.sonHata) > 5*time.Second {
		d.sonHata = time.Now()
		fmt.Fprintln(os.Stderr, "mcos-panel-fb: kare gönderilemedi:", err)
	}
	if d.bus != nil {
		d.bus.Publish(img)
	}
	return nil
}

// Check runs once a second: aygıt denetimi + VNC için yaşam işareti.
func (d *drmDisplay) Check() bool {
	degisti := d.drmScreen.Check()
	if d.bus != nil {
		d.bus.Heartbeat()
	}
	return degisti
}

// openScreen opens the display card, waiting briefly if none exists yet.
//
// Bazı makinelerde (eski BIOS + VGA metin kipi, firmware framebuffer'ı yok)
// açılışta hiçbir ekran düğümü yoktur; ekran kartı sürücüsü probe'unu panel
// başladıktan SONRA bitirir. Hemen vazgeçmek yerine 15 saniye beklenir
// (DRM_OUTPUT_POLL_PERIOD = 10 sn'yi kapsayacak kadar). Bir framebuffer zaten
// varsa ama DRM açılmıyorsa beklenmez: fbdev yolu hemen kullanılır.
func openScreen(saved string) (*drm.Screen, error) {
	son := time.Now().Add(15 * time.Second)
	for {
		scr, err := drm.OpenScreen(saved)
		if err == nil {
			return scr, nil
		}
		_, fbVar := os.Stat("/dev/fb0")
		if fbVar == nil || time.Now().After(son) {
			return nil, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

type fbDisplay struct{ dev *fbdev.Device }

func (d *fbDisplay) Flip(img *image.RGBA) error { return d.dev.Flip(img) }

// Blank is best-effort: firmware framebuffers usually cannot power down.
func (d *fbDisplay) Blank(off bool) bool { return false }

type nullDisplay struct{}

func (nullDisplay) Flip(*image.RGBA) error { return nil }
func (nullDisplay) Blank(bool) bool        { return false }

// ── Konsol ana makinesi ─────────────────────────────────────────────────────

type console struct {
	con  *fbvt.Console
	act  *fbinput.Activity
	cl   *ipcclient.Client
	ch   chan fbinput.Key
	stop chan struct{}
}

// start spawns the key reader and returns the key channel.
//
// ESC belirsizliği burada çözülür: tek başına gelen ESC bir kaçış dizisinin
// başlangıcı da olabilir. Kısa bir süre bekleyip devamı gelmezse Esc kabul
// edilir. Bu bekleme OKUMA döngüsünde yapılır, ana döngüyü bloklamaz.
func (c *console) start(escTimeout time.Duration, act *fbinput.Activity) <-chan fbinput.Key {
	c.ch = make(chan fbinput.Key, 32)
	c.stop = make(chan struct{})

	raw := make(chan []byte, 8)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := c.con.Read(buf)
			if err != nil {
				close(raw)
				return
			}
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				select {
				case raw <- b:
				case <-c.stop:
					return
				}
			}
		}
	}()

	go func() {
		defer close(c.ch)
		var dec fbinput.Decoder
		var timer <-chan time.Time
		for {
			select {
			case b, ok := <-raw:
				if !ok {
					return
				}
				act.Touch() // klavye de etkinliktir: uyku sayacını sıfırla
				for _, k := range dec.Decode(b) {
					select {
					case c.ch <- k:
					case <-c.stop:
						return
					}
				}
				if dec.Pending() {
					timer = time.After(escTimeout)
				} else {
					timer = nil
				}
			case m := <-act.MediaKeys():
				// Dizüstü medya tuşları (Fn+F3 vb.) TTY'ye karakter
				// göndermez; evdev'den gelip aynı tuş akışına katılıyorlar.
				select {
				case c.ch <- fbinput.Key{Name: m}:
				case <-c.stop:
					return
				}
			case <-timer:
				timer = nil
				for _, k := range dec.Flush() {
					select {
					case c.ch <- k:
					case <-c.stop:
						return
					}
				}
			case <-c.stop:
				return
			}
		}
	}()
	return c.ch
}

func (c *console) Keys() <-chan fbinput.Key  { return c.ch }
func (c *console) Activity() <-chan struct{} { return c.act.Wake() }

// Pointer forwards decoded mouse/touchpad events to the panel.
//
// Aynı evdev okuyucusundan gelir (bkz. fbinput.Activity): aygıtı iki kez
// açmak gereksiz kopya demektir ve bazı sürücülerde ikinci açış başarısız
// olur.
func (c *console) Pointer() <-chan fbinput.PointerEvent { return c.act.Pointer() }

// SetScreen follows a live mode change: touchpad ham değerleri piksele
// çevrilirken ekran boyutu kullanılıyor; eski boyutla kalsaydı imleç yeni
// ekranın bir köşesine sıkışırdı.
func (c *console) SetScreen(w, h int) { c.act.SetScreen(w, h) }

// Power restores the console BEFORE rebooting.
//
// Sıra kritik: grafik kipinde yeniden başlatırsak kapanış mesajları görünmez
// ve bir şey takılırsa kullanıcı kara ekranla kalır.
func (c *console) Power(action string) error {
	// Metin arabelleği ÖNCE silinir: Restore konsolu metin kipine alırken
	// çekirdek eski satırları yeniden çizer ve kapanış siyahının ortasında
	// açılıştan kalma metin yanıp söner (ölçüldü: iki kare, ≈0,2 sn).
	c.con.ClearText()
	_ = c.con.Restore()
	if err := c.cl.Power(action); err == nil {
		return nil
	}
	// Daemon yanıt vermiyorsa doğrudan dene: kullanıcı makineyi
	// kapatabilmeli.
	bin := "poweroff"
	if action == "reboot" {
		bin = "reboot"
	}
	return exec.Command(bin).Run()
}
