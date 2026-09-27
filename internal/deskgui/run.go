package deskgui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// RunOptions says how the interface is shown.
type RunOptions struct {
	// Title and Width/Height (CSS pikseli, 96 DPI) are for the window.
	Title         string
	Width, Height int
	// MinWidth/MinHeight: pencere bundan küçültülemez (0 = sınırsız).
	MinWidth, MinHeight int
	// IconID is the icon resource in the .exe (go-winres: 1).
	IconID int
	// DataDir holds WebView2's profile (Windows). Boş bırakılırsa WebView2
	// %APPDATA% altına (dolaşan profil) yazar; bunu istemiyoruz.
	DataDir string
	// Browser forces the default browser even when WebView2 exists.
	Browser bool
	// NoOpen prints the address and opens nothing.
	NoOpen bool
	// Duration closes the interface after this long (sınama; 0 = kapalı).
	Duration time.Duration
	// Done closes the interface when the program wants to quit.
	Done <-chan struct{}
	// Out receives the address line (nil = os.Stdout).
	Out io.Writer
	// IdleExit: tarayıcı kipinde sayfa bu kadar sessiz kalırsa (sekme
	// kapandı) çık. 0 = 3 dakika.
	IdleExit time.Duration
}

// Mode tells how the interface was shown.
type Mode string

const (
	ModeWindow  Mode = "pencere"  // WebView2 penceresi
	ModeBrowser Mode = "tarayıcı" // varsayılan tarayıcı açıldı
	ModePrint   Mode = "adres"    // yalnızca adres yazdırıldı
)

// Run shows the interface and blocks until it is closed.
//
// Kapanma yolları: pencere kapatıldı, Done kapandı, Duration doldu,
// Ctrl+C/SIGTERM geldi ya da (tarayıcı kipinde) sayfa IdleExit boyunca
// hiç istek göndermedi.
func (s *Server) Run(o RunOptions) (Mode, error) {
	out := o.Out
	if out == nil {
		out = os.Stdout
	}
	// Adres HER kipte yazılır: Linux'ta kullanıcı onu tarayıcıya
	// yapıştırır; Windows'ta ise sınamalar (WSL, curl.exe) API'ye buradan
	// ulaşır. Pencereli bir Windows programının stdout'u çoğu zaman yoktur
	// ve bu satır sessizce kaybolur — zararsız.
	fmt.Fprintf(out, "MCOS arayüzü: %s\n", s.URL())

	stop := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sig)
		var until <-chan time.Time
		if o.Duration > 0 {
			until = time.After(o.Duration)
		}
		select {
		case <-sig:
		case <-until:
		case <-o.Done:
		}
		close(stop)
	}()

	if !o.Browser && !o.NoOpen {
		shown, err := openWindow(s, o, stop)
		if shown {
			return ModeWindow, err
		}
		if err != nil {
			fmt.Fprintf(out, "Pencere açılamadı (%v); tarayıcı kullanılıyor.\n", err)
		}
	}

	mode := ModePrint
	if !o.NoOpen {
		if err := openBrowser(s.URL()); err == nil {
			mode = ModeBrowser
		} else {
			fmt.Fprintf(out, "Tarayıcı açılamadı (%v). Adresi tarayıcınıza yapıştırın.\n", err)
		}
	}
	s.waitIdle(o, mode, stop)
	return mode, nil
}

// waitIdle blocks in browser/print mode until stop or the page goes quiet.
//
// Tarayıcı sekmesinin kapandığını bilmenin güvenilir tek yolu isteklerin
// kesilmesidir. Arka plandaki sekmelerde tarayıcılar zamanlayıcıları
// dakikada bire kadar kısar; bu yüzden eşik birkaç dakikadır, saniye değil.
func (s *Server) waitIdle(o RunOptions, mode Mode, stop <-chan struct{}) {
	idle := o.IdleExit
	if idle <= 0 {
		idle = 3 * time.Minute
	}
	started := time.Now()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		last := s.LastSeen()
		switch {
		case !last.IsZero() && time.Since(last) > idle:
			return
		case last.IsZero() && mode == ModeBrowser && time.Since(started) > idle:
			// Tarayıcı açıldı dendi ama sayfa hiç gelmedi: süreç sonsuza
			// dek arka planda asılı kalmasın.
			return
		}
	}
}
