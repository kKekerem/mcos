//go:build linux

package supervisor

import (
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Bu dosya turbo_linux_test.go'nun kapsamadığı iki yolu sınar:
//  1. Turbo AÇILDIKTAN SONRA çöküp yeniden başlatılan sunucu. Eskiden turbo
//     yalnızca daemon'un başlatma anındaki kopyasına uygulanıyordu; supervisor
//     çöken süreci Add anındaki (turbosuz) tanımla kaldırıyordu.
//  2. Kullanıcının kendi çekirdek sabitlemesi: turbo kapanınca sunucu "tüm
//     çekirdeklere" değil, kullanıcının seçtiği çekirdeğe dönmeli.
// Yardımcılar (helperEnv, threadMasks, pickCPU, allowedList) turbo_linux_test.go'da.

// lastCPU izinli listenin son çekirdeğini verir ("0-23" -> 23).
func lastCPU(all string) int {
	f := strings.FieldsFunc(all, func(r rune) bool { return r == ',' || r == '-' })
	n, _ := strconv.Atoi(f[len(f)-1])
	return n
}

// helperSpec yardımcı alt süreci çalıştıran, her "HAZIR" satırını kanala
// bildiren bir tanım üretir.
func helperSpec(id string, ready chan<- struct{}) Spec {
	c := helperCmd()
	return Spec{
		Path: c.Path, Args: c.Args[1:], Env: c.Env, ID: id,
		OnLine: func(l string) {
			if strings.TrimSpace(l) == "HAZIR" {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		},
		StopTimeout: 2 * time.Second,
	}
}

func waitChan(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: yardımcı süreç 10 sn'de hazır olmadı", what)
	}
}

func TestTurboSurvivesCrashRestart(t *testing.T) {
	old := cgroupRoot
	cgroupRoot = t.TempDir() // gerçek /sys/fs/cgroup'a dokunma
	defer func() { cgroupRoot = old }()

	target, all := pickCPU(t)
	sup := New(nil)
	defer sup.StopAll()
	ready := make(chan struct{}, 4)
	p := sup.Add("c", helperSpec("turbo-c", ready), RestartPolicy{OnCrash: true, Backoff: 20 * time.Millisecond})
	if err := sup.Start("c"); err != nil {
		t.Fatal(err)
	}
	waitChan(t, ready, "ilk başlatma")
	for tid, m := range threadMasks(t, p.PID()) {
		if m != all {
			t.Fatalf("turbo kapalıyken iş parçacığı %d zaten sabit: %q", tid, m)
		}
	}

	// Turbo sunucu ÇALIŞIRKEN açılır, sonra sunucu çöker.
	sup.SetTurbo(true, []int{target})
	oldPID := p.PID()
	if err := syscall.Kill(oldPID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitChan(t, ready, "çökme sonrası yeniden başlatma")
	if p.PID() == oldPID || p.PID() == 0 {
		t.Fatalf("süreç yeniden başlamadı (pid %d)", p.PID())
	}
	masks := threadMasks(t, p.PID())
	if len(masks) < 5 {
		t.Fatalf("yeniden başlayan süreçte %d iş parçacığı", len(masks))
	}
	sup.RepinTurbo()
	checkTurboMasks(t, p.PID(), target, all, "çöküp yeniden başlayan sunucu")
}

func TestTurboOffRestoresUserAffinity(t *testing.T) {
	old := cgroupRoot
	cgroupRoot = t.TempDir()
	defer func() { cgroupRoot = old }()

	target, all := pickCPU(t)
	user := lastCPU(all)
	if user == target {
		t.Skip("iki ayrı çekirdek gerekli")
	}
	sup := New(nil)
	defer sup.StopAll()
	ready := make(chan struct{}, 2)
	spec := helperSpec("turbo-u", ready)
	spec.CPUAffinity = []int{user} // kullanıcının seçtiği çekirdek
	p := sup.Add("u", spec, RestartPolicy{})
	if err := sup.Start("u"); err != nil {
		t.Fatal(err)
	}
	waitChan(t, ready, "başlatma")

	sup.SetTurbo(true, []int{target})
	checkTurboMasks(t, p.PID(), target, all, "turbo açıkken")
	sup.SetTurbo(false, nil)
	for tid, m := range threadMasks(t, p.PID()) {
		if m != strconv.Itoa(user) {
			t.Errorf("turbo kapanınca kullanıcının sabitlemesi kayboldu: iş parçacığı %d %q, beklenen %d", tid, m, user)
		}
	}
}

// turboSpec kaynak tavanlarını kaldırmalı, önceliği yükseltmeli ve özgün
// tanımı DEĞİŞTİRMEMELİ (turbo kapanınca ona dönülüyor).
func TestTurboSpecClearsLimits(t *testing.T) {
	orig := Spec{Nice: 10, CPUAffinity: []int{3},
		Limits: Limits{CPUPercent: 50, MemoryMB: 1024, IOWeight: 100}}
	got := turboSpec(orig, []int{0, 1})
	// Süreç TÜM çekirdeklerde başlar (JVM havuzlarını tam kursun); oyun
	// döngüsü sonradan sabitlenir.
	if got.Nice != TurboNice || len(got.CPUAffinity) != 0 ||
		got.Limits != (Limits{IOWeight: 1000}) {
		t.Errorf("turboSpec = nice %d, çekirdek %v, sınırlar %+v", got.Nice, got.CPUAffinity, got.Limits)
	}
	if orig.Nice != 10 || !reflect.DeepEqual(orig.CPUAffinity, []int{3}) || orig.Limits.CPUPercent != 50 {
		t.Errorf("özgün tanım değişti: %+v", orig)
	}
	// Zaten daha yüksek öncelikli (nice -15) sunucu düşürülmemeli.
	if s := turboSpec(Spec{Nice: -15}, nil); s.Nice != -15 {
		t.Errorf("nice -15 turbo altında %d oldu", s.Nice)
	}
}
