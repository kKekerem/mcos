// Package tier classifies host capability (LOW/MEDIUM/HIGH) and resolves the
// effective adaptive feature flags by combining the detected tier, hardware
// presence, and the user's manual overrides. The daemon uses it on startup and
// periodically; mcos-detect uses Classify to pick which panel binary to launch.
package tier

import (
	"fmt"
	"strings"

	"mcos/internal/model"
)

const (
	gib       = 1 << 30
	RichPanel = "mcos-panel"
	LitePanel = "mcos-panel-lite"
)

// Decision is the complete adaptive policy result for the current host. It is
// intentionally small and serializable through SystemStatus so every frontend
// can explain why MCOS picked the rich or lite experience.
type Decision struct {
	Detected       model.Tier
	Effective      model.Tier
	Panel          string
	PollIntervalMS int
	MaxTasks       int
	Reasons        []string
}

// Classify maps memory + CPU into a capability tier. Thresholds are intentionally
// conservative so a weak box drops to LOW (lite panel, reduced background work).
func Classify(mem model.MemInfo, cpu model.CPUInfo) model.Tier {
	totalGiB := float64(mem.TotalBytes) / gib
	threads := cpu.Threads

	switch {
	case totalGiB >= 16 && threads >= 8:
		return model.TierHigh
	case totalGiB >= 6 && threads >= 4:
		return model.TierMedium
	default:
		return model.TierLow
	}
}

// Decide combines hardware detection, config overrides, and tier policy into a
// single decision used by mcosd and mcos-detect.
func Decide(cfg *model.Config, mem model.MemInfo, cpu model.CPUInfo) Decision {
	detected := Classify(mem, cpu)
	effective := Resolve(cfg, detected)
	return Decision{
		Detected:       detected,
		Effective:      effective,
		Panel:          PanelFor(effective),
		PollIntervalMS: PollIntervalMS(effective),
		MaxTasks:       MaxBackgroundTasks(effective),
		Reasons:        reasons(mem, cpu, detected, effective),
	}
}

// Resolve applies the config's tier mode/override on top of a detected tier.
func Resolve(cfg *model.Config, detected model.Tier) model.Tier {
	if cfg == nil {
		return detected
	}
	if t := Normalize(cfg.Tier.Override); t != "" {
		return t
	}
	if cfg.Tier.Mode != "" && cfg.Tier.Mode != "auto" {
		if t := Normalize(cfg.Tier.Mode); t != "" {
			return t
		}
	}
	return detected
}

// Normalize parses user/config tier strings.
func Normalize(s string) model.Tier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return model.TierLow
	case "medium", "med":
		return model.TierMedium
	case "high":
		return model.TierHigh
	default:
		return ""
	}
}

// PanelFor maps a tier to the frontend binary that should own tty1.
func PanelFor(t model.Tier) string {
	if t == model.TierLow {
		return LitePanel
	}
	return RichPanel
}

// PollIntervalMS is the default foreground refresh cadence for panels.
func PollIntervalMS(t model.Tier) int {
	switch t {
	case model.TierHigh:
		return 1000
	case model.TierMedium:
		return 2000
	default:
		return 4000
	}
}

// MaxBackgroundTasks limits expensive maintenance work by host tier.
func MaxBackgroundTasks(t model.Tier) int {
	switch t {
	case model.TierHigh:
		return 4
	case model.TierMedium:
		return 2
	default:
		return 1
	}
}

// Effective resolves a single feature mode against the tier default. When the
// mode is "auto", autoOn decides; otherwise the explicit on/off wins.
func Effective(mode model.FeatureMode, autoOn bool) bool {
	switch mode {
	case model.FeatureOn:
		return true
	case model.FeatureOff:
		return false
	default:
		return autoOn
	}
}

// Apply fills the effective adaptive flags on a SystemStatus based on tier,
// hardware presence (GPU/internet), peer count, and manual overrides.
func Apply(cfg *model.Config, st *model.SystemStatus, peers int) {
	if cfg == nil {
		cfg = model.DefaultConfig()
	}
	t := st.Tier
	hasGPU := len(st.GPUs) > 0
	hasNet := st.Net.Internet

	// GPU monitor: auto-on only when a GPU exists.
	st.GPUMonitorOn = Effective(cfg.Features.GPUMonitor, hasGPU)
	if st.GPUMonitorOn && !hasGPU {
		st.GPUMonitorOn = false // cannot monitor what isn't there
	}

	// Advanced analytics: heavy; auto-on only on MEDIUM/HIGH tiers.
	st.AdvancedAnalyticsOn = Effective(cfg.Features.AdvancedAnalytics, t != model.TierLow)

	// Full performance enables the most expensive paths only on HIGH by default.
	st.FullPerformanceOn = Effective(cfg.Features.FullPerformance, t == model.TierHigh)

	// Cluster: meaningful only when enabled in config (peer presence shown
	// separately); a single-PC box still works, the UI just dims the section.
	st.ClusterOn = cfg.Cluster.Enabled

	// WAN: requires internet to be useful.
	st.WANOn = hasNet
}

func reasons(mem model.MemInfo, cpu model.CPUInfo, detected, effective model.Tier) []string {
	totalGiB := float64(mem.TotalBytes) / gib
	threads := cpu.Threads
	out := []string{fmt.Sprintf("hardware: %.1f GiB RAM, %d threads -> %s", totalGiB, threads, detected)}
	if effective != detected {
		out = append(out, fmt.Sprintf("override: %s -> %s", detected, effective))
	}
	out = append(out, fmt.Sprintf("panel: %s", PanelFor(effective)))
	return out
}
