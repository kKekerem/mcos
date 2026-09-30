package portmgr

import (
	"testing"

	"mcos/internal/model"
)

func TestUsedPortsAndConflict(t *testing.T) {
	servers := []*model.Server{
		{ID: "a", Port: 25565},
		{ID: "b", Port: 25566},
	}
	used := UsedPorts(servers, "")
	if !used[25565] || !used[25566] {
		t.Fatalf("UsedPorts missing entries: %v", used)
	}
	// Excluding a server frees its port.
	used = UsedPorts(servers, "a")
	if used[25565] {
		t.Fatalf("excluded server's port should be free")
	}

	if id := Conflict(servers, 25566, ""); id != "b" {
		t.Fatalf("Conflict = %q, want b", id)
	}
	if id := Conflict(servers, 25566, "b"); id != "" {
		t.Fatalf("Conflict excluding owner should be empty, got %q", id)
	}
	if id := Conflict(servers, 30000, ""); id != "" {
		t.Fatalf("Conflict for unused port should be empty, got %q", id)
	}
}

func TestFindFreePrefersAvailable(t *testing.T) {
	// A high port that's almost certainly free and not in the used set.
	used := map[int]bool{}
	p, err := FindFree(0, used)
	if err != nil {
		t.Fatalf("FindFree: %v", err)
	}
	if p <= 0 || p > 65535 {
		t.Fatalf("FindFree returned invalid port %d", p)
	}
}
