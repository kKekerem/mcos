package store

import (
	"mcos/internal/model"
	"mcos/internal/sysmon"
)

// LoadConfig reads the global config from path, returning defaults if missing.
func LoadConfig(path string) (*model.Config, error) {
	var cfg model.Config
	if err := readJSON(path, &cfg); err != nil {
		if err == ErrNotFound {
			return model.DefaultConfig(), nil
		}
		return nil, err
	}
	checkHardwareChange(&cfg)
	return &cfg, nil
}

// checkHardwareChange forces the first-boot wizard to re-run when the MCOS
// media has been moved to different hardware. The previous implementation
// compared a node-id file that travels with the disk (so it could never detect
// a move); we now compare a MAC fingerprint, which is bound to the machine.
func checkHardwareChange(cfg *model.Config) {
	if cfg.HardwareID == "" {
		return // never recorded (pre-setup); SetupComplete already governs
	}
	current := sysmon.HardwareID()
	if current != "" && current != cfg.HardwareID {
		cfg.SetupComplete = false
	}
}

// SaveConfig atomically persists the global config to path.
func SaveConfig(path string, cfg *model.Config) error {
	return writeJSON(path, cfg)
}
