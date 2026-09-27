package supervisor

import (
	"errors"
	"strings"
	"testing"
)

func TestOOMMesajlariTurkceVeNedeniSoyler(t *testing.T) {
	sistem := exitMessage(-1, errors.New("signal: killed"), true, 0, 1)
	if !strings.Contains(sistem, "belleği bittiği") || !strings.Contains(sistem, "RAM") {
		t.Fatalf("sistem OOM'u açıkça söylenmeli: %q", sistem)
	}
	grup := exitMessage(-1, errors.New("signal: killed"), true, 2, 0)
	if !strings.Contains(grup, "bellek tavanını") {
		t.Fatalf("cgroup OOM'u açıkça söylenmeli: %q", grup)
	}
	for _, m := range []string{sistem, grup} {
		if strings.Contains(m, "signal: killed") || strings.Contains(m, "-1") {
			t.Fatalf("kullanıcıya İngilizce/anlamsız metin gitmemeli: %q", m)
		}
	}
	if m := exitMessage(1, errors.New("exit status 1"), false, 0, 0); strings.Contains(m, "exit status") {
		t.Fatalf("Go'nun İngilizce metni sızmamalı: %q", m)
	}
}

func TestSayacOkuma(t *testing.T) {
	if n := sayac("low 0\nhigh 3\noom 1\noom_kill 2\n", "oom_kill"); n != 2 {
		t.Fatalf("oom_kill = %d, 2 bekleniyordu", n)
	}
	if n := sayac("oom_kill_disable 1\n", "oom_kill"); n != 0 {
		t.Fatalf("önek eşleşmesi olmamalı: %d", n)
	}
}
