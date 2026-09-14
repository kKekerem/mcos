package panel

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"mcos/internal/model"
	"mcos/panel/theme"
)

func testApp() *App {
	return &App{
		th:     theme.New("noir-purple"),
		width:  120,
		height: 40,
		config: model.DefaultConfig(),
		status: &model.SystemStatus{
			SystemName: "MCOS", Version: "0.1.0", Tier: model.TierHigh,
			CPU:          model.CPUInfo{Model: "Test CPU", Cores: 8, Threads: 16, UsagePct: 42},
			Memory:       model.MemInfo{TotalBytes: 16 << 30, AvailableBytes: 8 << 30, UsagePct: 50},
			Net:          model.NetStatus{LocalIP: "192.168.1.10", Internet: true},
			ServersTotal: 2, ServersUp: 1, JavaVersions: []int{17, 21},
			WAN: "stopped", ClusterOn: true, WANOn: true,
		},
		servers: []*serverInfo{
			{ID: "srv_a", Name: "Survival", Software: model.SoftwarePaper, MCVersion: "1.21.1",
				JavaMajor: 21, RAMMB: 4096, Port: 25565, State: model.StateRunning, Players: 3},
			{ID: "srv_b", Name: "Creative", Software: model.SoftwareFabric, MCVersion: "1.20.4",
				JavaMajor: 17, RAMMB: 2048, Port: 25566, State: model.StateStopped},
		},
	}
}

func TestDashboardRenderNoPanic(t *testing.T) {
	a := testApp()
	out := a.renderDashboard(90, 38)
	if out == "" {
		t.Fatal("empty dashboard")
	}
	if !strings.Contains(out, "Sistem Durumu") || !strings.Contains(out, "İşlemci") {
		t.Fatalf("dashboard missing expected sections")
	}
}

func TestServersRender(t *testing.T) {
	a := testApp()
	a.section = secServers
	a.focus = focusContent
	out := a.renderServers(90, 38)
	if !strings.Contains(out, "Survival") || !strings.Contains(out, "Creative") {
		t.Fatal("server cards missing names")
	}
}

// TestServerSelectionVisible guards the "which server is selected?" fix: the
// cursor row must carry an explicit SEÇİLİ marker.
func TestServerSelectionVisible(t *testing.T) {
	a := testApp()
	a.section = secServers
	a.focus = focusContent
	a.serverCursor = 1
	out := a.renderServers(90, 38)
	if !strings.Contains(out, "SEÇİLİ") {
		t.Fatalf("selected server not marked: %q", out)
	}
}

// TestDashboardFitsWidth guards the "Genel Bakış kayıyor" fix: no rendered line
// may exceed the pane width, or the layout shifts.
func TestDashboardFitsWidth(t *testing.T) {
	a := testApp()
	const w = 90
	out := a.renderDashboard(w, 38)
	for _, line := range strings.Split(out, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			t.Fatalf("dashboard line overflows width %d (got %d): %q", w, lw, line)
		}
	}
}

// TestPowerModalRender checks the güç dialog offers both real power actions.
func TestPowerModalRender(t *testing.T) {
	a := testApp()
	a.powerMenu = true
	out := a.renderPowerModal()
	for _, want := range []string{"Güç", "Kapat", "Yeniden Başlat", "Vazgeç"} {
		if !strings.Contains(out, want) {
			t.Fatalf("power modal missing %q: %q", want, out)
		}
	}
}

// TestTurboBadge checks the TURBO indicator appears in the top bar when on.
func TestTurboBadge(t *testing.T) {
	a := testApp()
	a.status.TurboOn = true
	if !strings.Contains(a.renderTopBar(), "TURBO") {
		t.Fatal("turbo badge missing from top bar")
	}
}

func TestSidebarRender(t *testing.T) {
	a := testApp()
	out := a.renderSidebar(22, 38)
	for _, name := range []string{"Sistem Durumu", "Sunucular", "Tünel (playit)"} {
		if !strings.Contains(out, name) {
			t.Fatalf("sidebar missing %q", name)
		}
	}
}

func TestDetailView(t *testing.T) {
	d := newDetail(theme.New("noir-purple"), "srv_a")
	d.server = &model.Server{ID: "srv_a", Name: "Survival", Software: model.SoftwarePaper,
		MCVersion: "1.21.1", JavaMajor: 21, RAMMB: 4096, Port: 25565, State: model.StateRunning}
	out := d.view(90, 38)
	if !strings.Contains(out, "Survival") || !strings.Contains(out, "Genel") {
		t.Fatal("detail view missing expected content")
	}
}

// TestFramesFitTerminal keeps the polished panel honest on the terminal sizes
// commonly produced by fbterm. A single over-wide row can make the framebuffer
// appear to jump or leave a dark strip at the edge.
func TestFramesFitTerminal(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		for section := 0; section < secCount; section++ {
			t.Run(fmt.Sprintf("section-%d-width-%d", section, width), func(t *testing.T) {
				a := testApp()
				a.width, a.height = width, 36
				a.section = section
				a.focus = focusContent
				assertFrameFits(t, a.View(), width, a.height)
			})
		}

		a := testApp()
		a.width, a.height = width, 36
		a.powerMenu = true
		assertFrameFits(t, a.View(), width, a.height)

		a = testApp()
		a.width, a.height = width, 36
		a.openWizard()
		assertFrameFits(t, a.View(), width, a.height)
	}
}

func assertFrameFits(t *testing.T, out string, width, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		t.Fatalf("frame overflows height %d (got %d lines)", height, len(lines))
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("frame overflows width %d (got %d): %q", width, got, line)
		}
	}
}

func TestWizardValidateAndParams(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	if w.validate() == "" {
		t.Fatal("expected validation error with empty name")
	}
	w.name.SetValue("MyServer")
	w.version.SetValue("1.21.1")
	w.softwareIdx = 1 // paper
	w.ramIdx = 2      // 3072
	w.eulaAccepted = true
	if msg := w.validate(); msg != "" {
		t.Fatalf("unexpected validation error: %s", msg)
	}
	p := w.params()
	if p.Name != "MyServer" || p.MCVersion != "1.21.1" || p.Software != model.SoftwarePaper || p.RAMMB != 3072 {
		t.Fatalf("params wrong: %+v", p)
	}
}

func TestWizardPortValidation(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	w.name.SetValue("X")
	w.version.SetValue("1.21.1")
	w.eulaAccepted = true
	w.port.SetValue("99999")
	if w.validate() == "" {
		t.Fatal("expected port range error")
	}
	w.port.SetValue("25570")
	if w.validate() != "" {
		t.Fatal("valid port rejected")
	}
}
