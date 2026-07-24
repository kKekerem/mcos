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
)

func main() {
	connect := flag.String("connect", ipc.DefaultEndpoint(), "mcosd IPC endpoint")
	themeName := flag.String("theme", "", "force a theme palette (default: from daemon config)")
	shot := flag.Int("screenshot", -2, "headless: render one frame of section index (use -1 for wizard) and exit")
	shotW := flag.Int("w", 120, "screenshot width")
	shotH := flag.Int("h", 40, "screenshot height")
	flag.Parse()

	cl := connectWithRetry(*connect, 30)
	if cl == nil {
		fmt.Fprintf(os.Stderr, "mcos-panel: could not reach mcosd at %s\n", *connect)
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
	applyFramebufferPalette()
	app := panel.NewApp(cl, *themeName)
	// tty2 belongs exclusively to MCOS. Avoid an alternate screen because some
	// framebuffer terminals enter it but fail to repaint, leaving a black view.
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mcos-panel: %v\n", err)
		os.Exit(1)
	}
}

// applyFramebufferPalette gives fbterm's eight ANSI slots the panel's graphite
// palette. Lip Gloss deliberately uses only these portable slots on fbterm;
// its private palette command keeps the visual design without xterm-only
// 256-colour escape sequences.
func applyFramebufferPalette() {
	if os.Getenv("TERM") != "fbterm" {
		return
	}
	for i, rgb := range [][3]uint8{
		{0x0B, 0x0D, 0x10}, // background
		{0xFF, 0x6B, 0x7A}, // error
		{0x75, 0xE0, 0xA3}, // success
		{0xFF, 0xD1, 0x66}, // warning
		{0x15, 0x19, 0x1F}, // surface
		{0x8A, 0x98, 0xA5}, // muted / border
		{0x7E, 0xE7, 0xD0}, // accent
		{0xEF, 0xF3, 0xF0}, // text
	} {
		fmt.Fprintf(os.Stdout, "\x1b[3;%d;%d;%d;%d}", i, rgb[0], rgb[1], rgb[2])
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
