package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// TestApplyProperties verifies the wizard's gameplay fields actually land in
// server.properties (the fix for the dropped render/sim distance bug).
func TestApplyProperties(t *testing.T) {
	dir := t.TempDir()
	srv := &model.Server{Port: 25570, ViewDistance: 10, SimDistance: 6, MOTD: "Merhaba"}
	if err := applyProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"server-port=25570", "view-distance=10", "simulation-distance=6", "motd=Merhaba"} {
		if !strings.Contains(s, want) {
			t.Errorf("server.properties missing %q\n--- got ---\n%s", want, s)
		}
	}
}

// TestApplyPropertiesSkipsZero ensures untouched fields don't override defaults.
func TestApplyPropertiesSkipsZero(t *testing.T) {
	dir := t.TempDir()
	srv := &model.Server{Port: 25565}
	if err := applyProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if strings.Contains(string(data), "view-distance") {
		t.Errorf("zero view-distance should not be written:\n%s", string(data))
	}
}

// TestApplyPropertiesGameplay verifies the gameplay step's fields are written,
// including the always-written booleans and gamemode/difficulty.
func TestApplyPropertiesGameplay(t *testing.T) {
	dir := t.TempDir()
	srv := &model.Server{
		Port: 25565, MaxPlayers: 30, Gamemode: "creative", Difficulty: "hard",
		OnlineMode: true, PVP: false, Hardcore: true, Whitelist: true,
	}
	if err := applyProperties(dir, srv); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	s := string(data)
	for _, want := range []string{
		"max-players=30", "gamemode=creative", "difficulty=hard",
		"online-mode=true", "pvp=false", "hardcore=true", "white-list=true",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\n--- got ---\n%s", want, s)
		}
	}
}
