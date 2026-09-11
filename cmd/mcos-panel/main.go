// Command mcos-panel is the rich MCOS terminal panel (Bubble Tea). It connects
// to mcosd and renders the dashboard, server cards, install wizard, server
// panel, and live console. It is launched as the keyboard-only boot UI.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mcos/internal/ipc"
	"mcos/panel"
	"mcos/panel/theme"
)

func main() {
	connect := flag.String("connect", ipc.DefaultEndpoint(), "mcosd IPC endpoint")
	themeName := flag.String("theme", "", "force a theme palette (default: from daemon config)")
	shot := flag.Int("screenshot", -2, "headless: render one frame of section index (use -1 for wizard) and exit")
	shotW := flag.Int("w", 120, "screenshot width")
	shotH := flag.Int("h", 40, "screenshot height")
	flag.Parse()

	// Renk profili ve fbterm paleti: TEK seferlik global kurulum, HERHANGİ bir
	// çizimden ÖNCE. Palet artık panel/theme içinde tanımlıdır (eskiden burada,
	// .fbtermrc'den kopyalanmış ve tema paketinden kopuk bir liste vardı).
	//
	// Ekran görüntüsü dalından ÖNCE çağrılır: aksi halde lipgloss profili
	// otomatik algılamaya kalıyor, boru hattında Ascii'ye düşüyor ve stiller
	// boş "ESC[;m" dizileri basıyordu. Palet dizileri TERM=fbterm ile korumalı
	// olduğu için boru hattını kirletmez.
	theme.SetupTerminal(os.Stdout, *themeName)

	cl := connectWithRetry(*connect, 30)
	if cl == nil {
		fmt.Fprintf(os.Stderr, "mcos-panel: mcosd'ye ulaşılamadı: %s\n", *connect)
		os.Exit(1)
	}

	// Headless screenshot mode (no TTY): render one static frame and exit.
	if *shot != -2 {
		fmt.Println(panel.Screenshot(cl, *themeName, *shot, *shotW, *shotH))
		return
	}

	// The daemon creates its socket before all its background initialisation has
	// completed. Do not block tty2 on Config here: App.Init fetches it
	// asynchronously after the first frame is already visible.
	app := panel.NewApp(cl, *themeName)
	// tty2 belongs exclusively to MCOS. Avoid an alternate screen because some
	// framebuffer terminals enter it but fail to repaint, leaving a black view.
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mcos-panel: %v\n", err)
		os.Exit(1)
	}
}

// connectWithRetry waits for the daemon to come up (it may still be starting at
// boot), retrying up to attempts times.
func connectWithRetry(endpoint string, attempts int) *panel.Client {
	for i := 0; i < attempts; i++ {
		if cl, err := panel.Dial(endpoint); err == nil {
			return cl
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}
