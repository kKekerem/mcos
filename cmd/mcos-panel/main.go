// Command mcos-panel is the rich MCOS terminal panel (Bubble Tea). It connects
// to mcosd and renders the dashboard, server cards, install wizard, server
// panel, and live console. It is launched on tty1 by mcos-detect on capable
// hosts; on low-tier hosts the Rust lite panel is used instead.
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

	theme := *themeName
	if theme == "" {
		if cfg, err := cl.Config(); err == nil && cfg != nil {
			theme = cfg.Theme
		}
	}

	// Headless screenshot mode (no TTY): render one static frame and exit.
	if *shot != -2 {
		fmt.Println(panel.Screenshot(cl, theme, *shot, *shotW, *shotH))
		return
	}

	app := panel.NewApp(cl, theme)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
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
