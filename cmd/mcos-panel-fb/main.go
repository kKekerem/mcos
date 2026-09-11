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
	"time"

	"mcos/internal/fbdev"
	"mcos/internal/fbfont"
	"mcos/internal/fbinput"
	"mcos/internal/fbpanel"
	"mcos/internal/fbui"
	"mcos/internal/fbvt"
	"mcos/internal/ipcclient"
)

func main() {
	var (
		sock    = flag.String("connect", "/run/mcosd.sock", "mcosd soket yolu")
		fbPath  = flag.String("fb", "/dev/fb0", "framebuffer aygıtı")
		ttyPath = flag.String("tty", "/dev/tty", "konsol aygıtı")
		fontPx  = flag.Float64("font", 0, "yazı boyutu (piksel); 0 = ekrana göre otomatik")
		shot    = flag.String("screenshot", "", "bir kare çizip PNG olarak kaydet ve çık")
	)
	flag.Parse()

	err := run(*sock, *fbPath, *ttyPath, *fontPx, *shot)
	switch {
	case err == nil:
		return
	case errors.Is(err, fbpanel.ErrLegacyPanel):
		// F12: kullanıcı eski paneli istedi. Bu bir HATA DEĞİL, bu yüzden
		// stderr'e bir şey yazılmaz; mcos-launch yalnızca çıkış koduna bakar.
		os.Exit(exitLegacyPanel)
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

func run(sock, fbPath, ttyPath string, fontPx float64, shot string) error {
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
		w, h   int
	)
	if shot != "" {
		// Ekran görüntüsü kipi: gerçek framebuffer gerekmez. Bu sayede
		// arayüz, donanım olmadan da (derleme makinesinde, testte)
		// görülebilir ve gözden geçirilebilir.
		w, h = 1920, 1080
		canvas = image.NewRGBA(image.Rect(0, 0, w, h))
		disp = nullDisplay{}
	} else {
		dev, err := fbdev.Open(fbPath)
		if err != nil {
			return fmt.Errorf("framebuffer açılamadı (%s): %w", fbPath, err)
		}
		defer dev.Close()
		w, h = dev.Size()
		canvas = image.NewRGBA(image.Rect(0, 0, w, h))
		disp = &fbDisplay{dev: dev}
	}

	// ── Yazı tipi ───────────────────────────────────────────────────────────
	if fontPx <= 0 {
		fontPx = autoFontSize(h)
	}
	face, err := fbfont.Load(fontPx)
	if err != nil {
		return err
	}
	defer face.Close()

	ui := fbui.NewUI(canvas, face, fbui.DefaultPalette)
	app := fbpanel.New(ui, cl)

	if shot != "" {
		if cl == nil {
			// Gercek veri yok: ornek veriyle ciz ki ekranin dolu hali
			// gorulebilsin. Bu YALNIZCA ekran goruntusu kipindedir;
			// calisan sistemde her zaman gercek veri gosterilir.
			fbpanel.FillDemo(app)
		}
		app.Draw()
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

	host := &console{con: con, act: act, cl: cl}
	keys := host.start(fbpanel.DefaultOptions().EscTimeout, act)
	defer close(host.stop)

	err = app.Run(canvas, disp, host, fbpanel.DefaultOptions())
	_ = keys
	return err
}

// autoFontSize picks a readable cell size for the screen height.
//
// Sabit bir punto her çözünürlükte yanlış olur: 1080p'de doğru olan boyut
// 4K'da okunamayacak kadar küçük, 800x600'de ekranı kaplayacak kadar
// büyüktür. Yaklaşık 45 satır hedefliyoruz.
func autoFontSize(screenH int) float64 {
	const targetRows = 45
	px := float64(screenH) / targetRows * 0.8
	switch {
	case px < 12:
		return 12
	case px > 40:
		return 40
	}
	return px
}

// ── Ekran uyarlayıcıları ────────────────────────────────────────────────────

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

// Power restores the console BEFORE rebooting.
//
// Sıra kritik: grafik kipinde yeniden başlatırsak kapanış mesajları görünmez
// ve bir şey takılırsa kullanıcı kara ekranla kalır.
func (c *console) Power(action string) error {
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
