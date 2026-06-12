package providers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"mcos/internal/log"
)

// runJava runs `javaBin args...` in dir, streaming output to the logger and
// returning combined output. It is used by providers whose installation runs a
// vendor installer jar (Forge/NeoForge/Quilt) or a build tool (Spigot).
func runJava(ctx context.Context, javaBin, dir string, lg *log.Logger, args ...string) (string, error) {
	if javaBin == "" {
		return "", fmt.Errorf("installer requires Java but no runtime was provided")
	}
	cmd := exec.CommandContext(ctx, javaBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if lg != nil {
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			if line != "" {
				lg.Debugf("installer: %s", line)
			}
		}
	}
	if err != nil {
		return text, fmt.Errorf("installer (%s) failed: %w", javaBin, err)
	}
	return text, nil
}
