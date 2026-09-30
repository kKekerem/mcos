package panel

import (
	"testing"

	"mcos/internal/model"
	"mcos/panel/theme"
)

// TestWizardParamsCarriesAllFields guards the original data-loss bug: the
// wizard must forward render/sim distance, CPU quota, cluster share, data dir,
// and the autostart/backup/wan/ViaVersion flags to the daemon.
func TestWizardParamsCarriesAllFields(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	w.name.SetValue("Test")
	w.version.SetValue("1.20.4")
	w.softwareIdx = 1 // paper
	w.port.SetValue("25570")
	w.renderDistance.SetValue("12")
	w.simDistance.SetValue("8")
	w.cpuQuota.SetValue("150")
	w.clusterShare = true
	w.dataDir.SetValue("/data/x")
	w.autostart = true
	w.autoBackup = true
	w.wan = true
	w.viaVersion = true

	p := w.params()
	if p.Name != "Test" || p.MCVersion != "1.20.4" {
		t.Fatalf("identity dropped: %+v", p)
	}
	if p.Software != model.AllSoftware[1] {
		t.Fatalf("software dropped: %v", p.Software)
	}
	if p.Port != 25570 {
		t.Fatalf("port dropped: %d", p.Port)
	}
	if p.ViewDistance != 12 || p.SimDistance != 8 {
		t.Fatalf("distances dropped: view=%d sim=%d", p.ViewDistance, p.SimDistance)
	}
	if p.CPUQuota != 150 {
		t.Fatalf("cpu quota dropped: %d", p.CPUQuota)
	}
	if !p.ClusterShare || p.DataDir != "/data/x" {
		t.Fatalf("cluster/dataDir dropped: share=%v dir=%q", p.ClusterShare, p.DataDir)
	}
	if !p.Autostart || !p.AutoBackup || !p.WAN || !p.AllowOldVersions {
		t.Fatalf("flags dropped: %+v", p)
	}
}

// TestWizardDistanceDefaults verifies empty distance fields fall back to 10.
func TestWizardDistanceDefaults(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	w.name.SetValue("D")
	w.version.SetValue("1.21.1")
	p := w.params()
	if p.ViewDistance != 10 || p.SimDistance != 10 {
		t.Fatalf("expected default 10/10, got %d/%d", p.ViewDistance, p.SimDistance)
	}
}

// TestWizardGameplayDefaults verifies the gameplay step's defaults reach params:
// online-mode and pvp default true, max-players 20, gamemode survival.
func TestWizardGameplayDefaults(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	w.name.SetValue("G")
	w.version.SetValue("1.21.1")
	p := w.params()
	if p.OnlineMode == nil || !*p.OnlineMode {
		t.Fatal("online-mode should default true")
	}
	if p.PVP == nil || !*p.PVP {
		t.Fatal("pvp should default true")
	}
	if p.MaxPlayers != 20 {
		t.Fatalf("max-players default want 20, got %d", p.MaxPlayers)
	}
	if p.Gamemode != "survival" || p.Difficulty != "easy" {
		t.Fatalf("gameplay defaults wrong: %s/%s", p.Gamemode, p.Difficulty)
	}
}

// TestWizardLiveVersionPicker verifies a fetched version is used when no manual
// override is typed, and that a manual entry still wins.
func TestWizardLiveVersionPicker(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	w.setVersions([]string{"1.21.4", "1.21.1", "1.20.4"})
	if w.effectiveVersion() != "1.21.4" {
		t.Fatalf("picked version want 1.21.4, got %q", w.effectiveVersion())
	}
	w.version.SetValue("1.7.10")
	if w.effectiveVersion() != "1.7.10" {
		t.Fatalf("manual override should win, got %q", w.effectiveVersion())
	}
}

// TestWizardTemplate verifies a template prefills software + gameplay.
func TestWizardTemplate(t *testing.T) {
	w := newWizard(theme.New("noir-purple"))
	// Find the "Hardcore — Vanilla" template index.
	for i, tpl := range templates {
		if tpl.hardcore {
			w.templateIdx = i
		}
	}
	w.applyTemplate()
	if model.AllSoftware[w.softwareIdx] != model.SoftwareVanilla {
		t.Fatalf("template software not applied: %v", model.AllSoftware[w.softwareIdx])
	}
	if !w.hardcore || difficulties[w.difficultyIdx] != "hard" {
		t.Fatalf("template gameplay not applied: hardcore=%v diff=%s", w.hardcore, difficulties[w.difficultyIdx])
	}
}
