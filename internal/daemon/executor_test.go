package daemon

import (
	"strings"
	"testing"
)

func TestAnalyzeLog(t *testing.T) {
	out := analyzeLog("[12:00] INFO start\n[12:01] ERROR boom\n[12:02] WARN careful\n[12:03] INFO ok\n")
	if !strings.Contains(out, "4 satır") {
		t.Errorf("line count wrong: %q", out)
	}
	if !strings.Contains(out, "1 hata") {
		t.Errorf("error count wrong: %q", out)
	}
	if !strings.Contains(out, "1 uyarı") {
		t.Errorf("warn count wrong: %q", out)
	}
}

func TestAnalyzeLogEmpty(t *testing.T) {
	if got := analyzeLog("   "); got != "log boş" {
		t.Errorf("expected empty marker, got %q", got)
	}
}

func TestBackupPolicyFor(t *testing.T) {
	if p := backupPolicyFor(false); p.Auto {
		t.Error("disabled policy should not be auto")
	}
	p := backupPolicyFor(true)
	if !p.Auto || p.Schedule == "" || p.Keep <= 0 {
		t.Errorf("enabled policy incomplete: %+v", p)
	}
}
