package panel

import (
	"testing"

	"mcos/panel/theme"
)

// TestRandomHexUnique guards the original bug where randomHex returned a fixed
// "abcdef01…" string, making every fallback NodeID identical.
func TestRandomHexUnique(t *testing.T) {
	a := randomHex(8)
	b := randomHex(8)
	if a == b {
		t.Fatalf("randomHex is not random: both = %s", a)
	}
	if len(a) != 16 {
		t.Fatalf("randomHex(8) want 16 hex chars, got %d", len(a))
	}
}

// TestSetupBuildConfig verifies the OOBE collects theme, budget, identity, and
// a hardware fingerprint into the saved config.
func TestSetupBuildConfig(t *testing.T) {
	s := newSetup(theme.New("noir-purple"), "noir-purple")
	s.pcName.SetValue("pc")
	s.nodeName.SetValue("node-x")
	s.ssid.SetValue("homewifi")
	s.themeIdx = 1
	s.ramIdx = 2
	s.cpuIdx = 2
	s.clusterOn = true

	cfg := s.buildConfig()
	if !cfg.SetupComplete {
		t.Fatal("SetupComplete should be true")
	}
	if cfg.Theme != theme.Names()[1] {
		t.Fatalf("theme not applied: %s", cfg.Theme)
	}
	if cfg.Budget.MaxServerRAMMB != budgetRAM[2] || cfg.Budget.MaxServerCPUPercent != budgetCPU[2] {
		t.Fatalf("budget not applied: %+v", cfg.Budget)
	}
	if cfg.NodeID == "" {
		t.Fatal("NodeID should be generated")
	}
	if cfg.Cluster.NodeName != "node-x" || !cfg.Cluster.Enabled {
		t.Fatalf("cluster config wrong: %+v", cfg.Cluster)
	}
	if cfg.WiFiSSID != "homewifi" {
		t.Fatalf("wifi ssid: %s", cfg.WiFiSSID)
	}
}
