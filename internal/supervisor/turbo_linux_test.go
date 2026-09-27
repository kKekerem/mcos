//go:build linux

package supervisor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Bu sınamalar GERÇEK sched_setaffinity kullanır, ama yalnızca kendi
// başlattıkları alt süreçte (test ikilisinin yardımcı kipi). Ana makinenin
// başka hiçbir sürecine ya da /sys'ine dokunulmaz: cgroupRoot boş bir geçici
// dizine çevrilir.

const helperEnv = "MCOS_TURBO_YARDIMCI"

// TestHelperProcess sınama değil: alt süreç olarak çalıştırıldığında birkaç
// işletim sistemi iş parçacığı açıp bekler (çalışan bir JVM'in yerine).
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("yalnızca alt süreç kipinde çalışır")
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			runtime.LockOSThread() // her goroutine kendi iş parçacığında
			if i == 0 {
				// Bir JVM'deki oyun döngüsü gibi: turbo YALNIZCA bunu
				// P-çekirdeğine sabitlemeli (bkz. pinTurboThreads).
				ad := append([]byte(mainThreadName), 0)
				_ = unix.Prctl(unix.PR_SET_NAME, uintptr(unsafe.Pointer(&ad[0])), 0, 0, 0)
			}
			wg.Done()
			select {}
		}(i)
	}
	wg.Wait()
	fmt.Println("HAZIR")
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

func helperCmd() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	return cmd
}

// allowedList bir iş parçacığının Cpus_allowed_list değerini okur.
func allowedList(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", path, err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && k == "Cpus_allowed_list" {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("%s içinde Cpus_allowed_list yok", path)
	return ""
}

// threadMasks sürecin her iş parçacığının maskesini döner.
func threadMasks(t *testing.T, pid int) map[int]string {
	t.Helper()
	out := map[int]string{}
	for _, tid := range taskIDs(pid) {
		out[tid] = allowedList(t, fmt.Sprintf("/proc/%d/task/%d/status", pid, tid))
	}
	return out
}

// checkTurboMasks: turbo açıkken oyun döngüsü hedef çekirdekte, DİĞER tüm iş
// parçacıkları her yerde olmalı (kullanıcı: "turbo açınca 50 cps'den 30'a
// düştü" — tüm iş parçacıklarını P-çekirdeğine hapsetmek üretimi boğuyordu).
func checkTurboMasks(t *testing.T, pid, target int, all, what string) {
	t.Helper()
	ana := 0
	for tid, m := range threadMasks(t, pid) {
		if threadName(pid, tid) == mainThreadName {
			ana++
			if m != strconv.Itoa(target) {
				t.Errorf("%s: oyun döngüsü %d %q, beklenen %d", what, tid, m, target)
			}
			continue
		}
		if m != all {
			t.Errorf("%s: işçi iş parçacığı %d %q hapsedildi, beklenen tüm çekirdekler %q", what, tid, m, all)
		}
	}
	if ana != 1 {
		t.Fatalf("%s: %d oyun döngüsü iş parçacığı bulundu, 1 bekleniyordu", what, ana)
	}
}

// pickCPU sınama sürecinin izinli ilk çekirdeğini ve tüm listeyi verir.
func pickCPU(t *testing.T) (int, string) {
	all := allowedList(t, "/proc/self/status")
	first := strings.FieldsFunc(all, func(r rune) bool { return r == ',' || r == '-' })[0]
	n, _ := strconv.Atoi(first)
	if all == first {
		t.Skip("tek çekirdek izinli; sabitleme farkı ölçülemez")
	}
	return n, all
}

func waitReady(t *testing.T, sc *bufio.Scanner) {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "HAZIR" {
				done <- true
				return
			}
		}
		done <- false
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("yardımcı süreç HAZIR demeden kapandı")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("yardımcı süreç 10 sn'de hazır olmadı")
	}
}

// Kullanıcı: "turbo açılınca sunucular P-çekirdeklerinde çalışsın".
// Çalışan bir JVM onlarca iş parçacığıdır; eski setAffinity yalnızca ana
// iş parçacığını (pid) taşıyordu. Bu sınama HER iş parçacığının taşındığını
// ve kapatınca tüm çekirdeklere geri döndüğünü /proc'tan okuyarak doğrular.
func TestPinAllThreadsPinsEveryThread(t *testing.T) {
	target, all := pickCPU(t)
	cmd := helperCmd()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	waitReady(t, bufio.NewScanner(out))
	pid := cmd.Process.Pid

	before := threadMasks(t, pid)
	if len(before) < 5 {
		t.Fatalf("yardımcıda yalnızca %d iş parçacığı var; sınama anlamsız olur", len(before))
	}

	n := pinAllThreads(pid, []int{target})
	got := threadMasks(t, pid)
	if n != len(got) {
		t.Errorf("pinAllThreads %d dedi, süreçte %d iş parçacığı var", n, len(got))
	}
	for tid, m := range got {
		if m != strconv.Itoa(target) {
			t.Errorf("iş parçacığı %d sabitlenmedi: Cpus_allowed_list=%q, beklenen %d (pid %d)", tid, m, target, pid)
		}
	}

	pinAllThreads(pid, nil)
	for tid, m := range threadMasks(t, pid) {
		if m != all {
			t.Errorf("turbo kapanınca iş parçacığı %d tüm çekirdeklere dönmedi: %q, beklenen %q", tid, m, all)
		}
	}
}

// Supervisor.SetTurbo çalışan sunucuyu P-çekirdeklerine taşımalı, turbo
// açıkken BAŞLAYAN süreç de (çökme sonrası yeniden başlatma dahil) aynı
// yere sabitlenmeli, kapatınca ikisi de her yere dönmeli.
func TestSupervisorTurboRunningAndNewProcesses(t *testing.T) {
	old := cgroupRoot
	cgroupRoot = t.TempDir() // gerçek /sys/fs/cgroup'a dokunma
	defer func() { cgroupRoot = old }()

	target, all := pickCPU(t)
	sup := New(nil)
	ready := map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{})}
	add := func(name string) *Process {
		var once sync.Once
		return sup.Add(name, Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$"},
			Env: append(os.Environ(), helperEnv+"=1"),
			ID:  "turbo-" + name,
			OnLine: func(l string) {
				if strings.TrimSpace(l) == "HAZIR" {
					once.Do(func() { close(ready[name]) })
				}
			},
			StopTimeout: 2 * time.Second,
		}, RestartPolicy{})
	}
	wait := func(name string) {
		select {
		case <-ready[name]:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s hazır olmadı", name)
		}
	}
	defer sup.StopAll()

	pa := add("a")
	if err := sup.Start("a"); err != nil {
		t.Fatal(err)
	}
	wait("a")

	res := sup.SetTurbo(true, []int{target})
	if res.Processes != 1 || res.Threads < 5 {
		t.Fatalf("SetTurbo: %+v — çalışan sunucu taşınmadı", res)
	}
	checkTurboMasks(t, pa.PID(), target, all, "çalışan sunucu")

	// Turbo AÇIKKEN başlayan süreç: Add'in bağladığı kanca başlatmada
	// P-çekirdeklerini uygulamalı.
	pb := add("b")
	if err := sup.Start("b"); err != nil {
		t.Fatal(err)
	}
	wait("b")
	// Oyun döngüsü başlatmadan SONRA doğar; dönemsel sabitleme onu yakalar
	// (daemon RepinTurbo'yu 15 sn'de bir, süreç kendisi 3/10/30 sn'de çağırır).
	sup.RepinTurbo()
	checkTurboMasks(t, pb.PID(), target, all, "turbo açıkken başlayan sunucu")

	res = sup.SetTurbo(false, nil)
	if res.Processes != 2 {
		t.Errorf("turbo kapatma %d sürece uygulandı, beklenen 2", res.Processes)
	}
	for _, p := range []*Process{pa, pb} {
		for tid, m := range threadMasks(t, p.PID()) {
			if m != all {
				t.Errorf("turbo kapanınca iş parçacığı %d %q, beklenen %q", tid, m, all)
			}
		}
	}
}
