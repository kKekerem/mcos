package tier

import (
	"testing"

	"mcos/internal/model"
)

func TestClassifyThresholds(t *testing.T) {
	tests := []struct {
		name string
		mem  uint64
		cpu  int
		want model.Tier
	}{
		{name: "low memory", mem: 4 << 30, cpu: 8, want: model.TierLow},
		{name: "low cpu", mem: 16 << 30, cpu: 2, want: model.TierLow},
		{name: "medium", mem: 8 << 30, cpu: 4, want: model.TierMedium},
		{name: "high", mem: 32 << 30, cpu: 16, want: model.TierHigh},
	}
	for _, tt := range tests {
		got := Classify(model.MemInfo{TotalBytes: tt.mem}, model.CPUInfo{Threads: tt.cpu})
		if got != tt.want {
			t.Fatalf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestDecideHonorsOverrideAndPanel(t *testing.T) {
	cfg := model.DefaultConfig()
	cfg.Tier.Override = "high"
	decision := Decide(cfg, model.MemInfo{TotalBytes: 2 << 30}, model.CPUInfo{Threads: 2})
	if decision.Detected != model.TierLow {
		t.Fatalf("detected = %s, want low", decision.Detected)
	}
	if decision.Effective != model.TierHigh {
		t.Fatalf("effective = %s, want high", decision.Effective)
	}
	if decision.Panel != RichPanel {
		t.Fatalf("panel = %s, want %s", decision.Panel, RichPanel)
	}
}

func TestApplyFeatureModes(t *testing.T) {
	cfg := model.DefaultConfig()
	st := &model.SystemStatus{Tier: model.TierHigh, Net: model.NetStatus{Internet: true}}
	Apply(cfg, st, 0)
	if !st.AdvancedAnalyticsOn || !st.FullPerformanceOn || !st.WANOn {
		t.Fatalf("high auto flags not enabled: %+v", st)
	}
	if st.GPUMonitorOn {
		t.Fatal("gpu monitor enabled without a GPU")
	}

	cfg.Features.FullPerformance = model.FeatureOff
	Apply(cfg, st, 0)
	if st.FullPerformanceOn {
		t.Fatal("full performance should respect explicit off")
	}
}
