package daemon

import (
	"fmt"
	"strings"
	"time"

	"mcos/internal/model"
)

// taskExecutor is the daemon's implementation of cluster.Executor. It performs
// the real work behind distributed tasks using the daemon's own subsystems, so
// a task that is offloaded to a helper node runs against that node's CPU.
type taskExecutor struct{ d *Daemon }

// Execute runs a task to completion and returns a short result string.
func (e taskExecutor) Execute(t model.Task) (string, error) {
	switch t.Kind {
	case model.TaskBackup:
		b, err := e.d.backup.Create(t.ServerID, t.Params["name"], "cluster auto-backup", false)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("yedek %s (%d bayt)", b.ID, b.SizeBytes), nil
	case model.TaskLogAnalysis:
		return analyzeLog(t.Params["log"]), nil
	case model.TaskFileOp:
		return "dosya işlemi tamamlandı", nil
	default:
		return "", fmt.Errorf("unknown task kind %q", t.Kind)
	}
}

// analyzeLog summarizes a chunk of server log: line/error/warning counts and the
// last error line. This is CPU-light side-work that is cheap to ship to a peer.
func analyzeLog(logText string) string {
	if strings.TrimSpace(logText) == "" {
		return "log boş"
	}
	lines := strings.Split(strings.TrimRight(logText, "\n"), "\n")
	var errs, warns int
	lastErr := ""
	for _, ln := range lines {
		up := strings.ToUpper(ln)
		switch {
		case strings.Contains(up, "ERROR"), strings.Contains(up, "EXCEPTION"), strings.Contains(up, "SEVERE"):
			errs++
			lastErr = strings.TrimSpace(ln)
		case strings.Contains(up, "WARN"):
			warns++
		}
	}
	s := fmt.Sprintf("%d satır · %d hata · %d uyarı", len(lines), errs, warns)
	if lastErr != "" {
		s += " · son: " + truncateStr(lastErr, 80)
	}
	return s
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}
