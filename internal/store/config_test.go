package store

import (
	"testing"

	"mcos/internal/model"
	"mcos/internal/sysmon"
)

// TestCheckHardwareChangeEmpty: a config without a recorded fingerprint (pre-
// setup, or an old config) must not be forced back into the wizard.
func TestCheckHardwareChangeEmpty(t *testing.T) {
	cfg := &model.Config{SetupComplete: true}
	checkHardwareChange(cfg)
	if !cfg.SetupComplete {
		t.Fatal("empty HardwareID must not reset SetupComplete")
	}
}

// TestCheckHardwareChangeMismatch: a stored fingerprint that differs from this
// machine's forces the first-boot wizard to run again (disk moved to a new PC).
func TestCheckHardwareChangeMismatch(t *testing.T) {
	if sysmon.HardwareID() == "" {
		t.Skip("no NIC fingerprint on this host")
	}
	cfg := &model.Config{SetupComplete: true, HardwareID: "0000000000000000"}
	checkHardwareChange(cfg)
	if cfg.SetupComplete {
		t.Fatal("mismatched HardwareID must reset SetupComplete")
	}
}
