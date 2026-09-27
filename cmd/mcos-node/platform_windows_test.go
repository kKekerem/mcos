//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Düğüm Görev Yöneticisi'nden (ya da oturum kapanırken) SERT biçimde
// öldürülürse java.exe de ölmeli; yoksa dünyayı kilitli tutan sahipsiz bir
// sunucu kalır. Bunu gerçek Windows süreçleriyle ölçüyoruz.
//
// Yardımcı kip: MCOS_JOB_HELPER=1 ise bu ikili "düğüm" rolünü oynar:
// (isteğe bağlı) iş nesnesine girer, uzun süren bir alt süreç başlatır,
// alt sürecin PID'ini yazar ve bekler.
func TestMain(m *testing.M) {
	if os.Getenv("MCOS_JOB_HELPER") == "1" {
		if os.Getenv("MCOS_JOB_USE") == "1" {
			platformJob()
		}
		c := exec.Command("ping", "-n", "120", "127.0.0.1")
		if err := c.Start(); err != nil {
			fmt.Println("HATA", err)
			os.Exit(1)
		}
		fmt.Println("COCUK", c.Process.Pid)
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func runHelper(t *testing.T, useJob bool) (helper *exec.Cmd, child int) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper = exec.Command(self)
	helper.Env = append(os.Environ(), "MCOS_JOB_HELPER=1", "MCOS_JOB_USE="+map[bool]string{true: "1", false: "0"}[useJob])
	out, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "COCUK ") {
		t.Fatalf("yardımcı çıktısı %q (%v)", line, err)
	}
	child, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "COCUK ")))
	return helper, child
}

func TestJobKillsChildrenWhenNodeDies(t *testing.T) {
	helper, child := runHelper(t, true)
	if !alive(child) {
		t.Fatal("alt süreç hiç başlamadı")
	}
	_ = helper.Process.Kill() // TerminateProcess: dünyayı kaydetme şansı YOK
	_, _ = helper.Process.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for alive(child) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(child) {
		t.Fatalf("düğüm öldü ama alt süreç (%d) yaşıyor — sahipsiz java kalırdı", child)
	}
}

// KARŞI-SINAMA: iş nesnesi OLMADAN alt süreç hayatta kalır (Windows üst süreç
// ölünce alt süreçleri öldürmez). Bu geçmezse yukarıdaki sınama bir şey
// kanıtlamıyor demektir.
func TestWithoutJobChildSurvives(t *testing.T) {
	helper, child := runHelper(t, false)
	_ = helper.Process.Kill()
	_, _ = helper.Process.Wait()
	time.Sleep(1500 * time.Millisecond)
	survived := alive(child)
	if p, err := os.FindProcess(child); err == nil {
		_ = p.Kill() // temizlik
	}
	if !survived {
		t.Fatal("iş nesnesi olmadan da alt süreç öldü — ölçüm yöntemi yanlış")
	}
}
