// Command mcos-detect classifies host capability and launches the appropriate
// front-end: the rich Go panel (mcos-panel) on MEDIUM/HIGH tiers, or the Rust
// lite panel (mcos-panel-lite) on LOW tier. It runs on tty1 as the boot UI.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/store"
	"mcos/internal/sysmon"
	"mcos/internal/tier"
)

func main() {
	forceTier := flag.String("tier", envOr("MCOS_TIER", ""), "force a tier (low|medium|high), empty = config/auto")
	forcePanel := flag.String("panel", envOr("MCOS_PANEL", "auto"), "force panel: auto|rich|lite")
	cfgPath := flag.String("config", envOr("MCOS_CONFIG", defaultConfigPath()), "path to config.json")
	connect := flag.String("connect", envOr("MCOS_ENDPOINT", ipc.DefaultEndpoint()), "mcosd IPC endpoint")
	richBin := flag.String("rich-bin", envOr("MCOS_RICH_PANEL", ""), "path to mcos-panel")
	liteBin := flag.String("lite-bin", envOr("MCOS_LITE_PANEL", ""), "path to mcos-panel-lite")
	printOnly := flag.Bool("print", false, "print the decision instead of launching a panel")
	flag.Parse()

	cfg, err := store.LoadConfig(*cfgPath)
	if err != nil {
		fail("load config %s: %v", *cfgPath, err)
	}

	cpu := sysmon.CPU()
	mem := sysmon.Memory()
	decision := tier.Decide(cfg, mem, cpu)

	if strings.TrimSpace(*forceTier) != "" {
		forced := tier.Normalize(*forceTier)
		if forced == "" {
			fail("invalid tier %q (use low, medium, or high)", *forceTier)
		}
		decision.Effective = forced
		decision.Panel = tier.PanelFor(forced)
	}

	panel, err := selectPanel(decision, *forcePanel)
	if err != nil {
		fail("%v", err)
	}
	if *printOnly {
		printDecision(cpu, mem, decision, panel)
		return
	}

	path, err := resolvePanel(panel, *richBin, *liteBin)
	if err != nil {
		fail("%v", err)
	}
	args := panelArgs(panel, *connect, cfg.Theme, flag.Args())
	if err := launch(path, args); err != nil {
		fail("launch %s: %v", path, err)
	}
}

func printDecision(cpu model.CPUInfo, mem model.MemInfo, decision tier.Decision, panel string) {
	fmt.Printf("MCOS hardware detection\n")
	fmt.Printf("  CPU      : %s (%d cores / %d threads)\n", cpu.Model, cpu.Cores, cpu.Threads)
	fmt.Printf("  Memory   : %.1f GiB\n", float64(mem.TotalBytes)/(1<<30))
	fmt.Printf("  Detected : %s\n", decision.Detected)
	fmt.Printf("  Effective: %s\n", decision.Effective)
	fmt.Printf("  Poll     : %d ms\n", decision.PollIntervalMS)
	fmt.Printf("  Panel    : %s\n", panel)
	for _, reason := range decision.Reasons {
		fmt.Printf("  Reason   : %s\n", reason)
	}
}

func selectPanel(decision tier.Decision, forced string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(forced)) {
	case "", "auto":
		return decision.Panel, nil
	case "rich", "go", tier.RichPanel:
		return tier.RichPanel, nil
	case "lite", "rust", tier.LitePanel:
		return tier.LitePanel, nil
	default:
		return "", fmt.Errorf("invalid panel %q (use auto, rich, or lite)", forced)
	}
}

func panelArgs(panel, endpoint, theme string, extra []string) []string {
	args := []string{"--connect", endpoint}
	if panel == tier.RichPanel && theme != "" {
		args = append(args, "--theme", theme)
	}
	return append(args, extra...)
}

func resolvePanel(panel, richPath, litePath string) (string, error) {
	explicit := ""
	if panel == tier.RichPanel {
		explicit = richPath
	} else {
		explicit = litePath
	}
	if explicit != "" {
		return explicit, nil
	}

	name := executableName(panel)
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("could not find %s (set --rich-bin/--lite-bin or PATH)", panel)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func launch(path string, args []string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func defaultConfigPath() string {
	if runtime.GOOS == "linux" {
		return "/etc/mcos/config.json"
	}
	return filepath.Join(".", "run", "config.json")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mcos-detect: "+format+"\n", args...)
	os.Exit(1)
}
