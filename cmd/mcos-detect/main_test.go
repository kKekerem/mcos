package main

import (
	"reflect"
	"testing"

	"mcos/internal/model"
	"mcos/internal/tier"
)

func TestSelectPanel(t *testing.T) {
	decision := tier.Decision{Panel: tier.LitePanel}
	tests := []struct {
		forced string
		want   string
		ok     bool
	}{
		{forced: "auto", want: tier.LitePanel, ok: true},
		{forced: "rich", want: tier.RichPanel, ok: true},
		{forced: "go", want: tier.RichPanel, ok: true},
		{forced: "lite", want: tier.LitePanel, ok: true},
		{forced: "bad", ok: false},
	}
	for _, tt := range tests {
		got, err := selectPanel(decision, tt.forced)
		if tt.ok && err != nil {
			t.Fatalf("%q unexpected error: %v", tt.forced, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("%q expected error", tt.forced)
		}
		if tt.ok && got != tt.want {
			t.Fatalf("%q got %s, want %s", tt.forced, got, tt.want)
		}
	}
}

func TestPanelArgs(t *testing.T) {
	got := panelArgs(tier.RichPanel, "tcp://127.0.0.1:7777", "noir-purple", []string{"--screenshot", "0"})
	want := []string{"--connect", "tcp://127.0.0.1:7777", "--theme", "noir-purple", "--screenshot", "0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rich args = %#v, want %#v", got, want)
	}

	got = panelArgs(tier.LitePanel, "unix:///run/mcos/mcosd.sock", "noir-purple", nil)
	want = []string{"--connect", "unix:///run/mcos/mcosd.sock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lite args = %#v, want %#v", got, want)
	}
}

func TestForcedTierUpdatesDecision(t *testing.T) {
	decision := tier.Decide(model.DefaultConfig(), model.MemInfo{TotalBytes: 2 << 30}, model.CPUInfo{Threads: 2})
	forced := tier.Normalize("high")
	decision.Effective = forced
	decision.Panel = tier.PanelFor(forced)
	if decision.Panel != tier.RichPanel {
		t.Fatalf("forced high panel = %s, want %s", decision.Panel, tier.RichPanel)
	}
}
