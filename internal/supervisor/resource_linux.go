//go:build linux

package supervisor

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// applyLimits applies the process's scheduling priority (nice) and CPU affinity
// after it has started. Negative nice and affinity changes require privilege
// (the appliance runs as root); failures are ignored so a non-privileged dev
// host degrades gracefully.
func applyLimits(pid int, spec Spec) {
	if spec.Nice != 0 {
		niceAllThreads(pid, spec.Nice)
	}
	if len(spec.CPUAffinity) > 0 {
		pinAllThreads(pid, spec.CPUAffinity)
	}
}

// cpuMask builds a cpu_set_t bitmask by hand to avoid pulling in
// golang.org/x/sys. Boş liste = TÜM çekirdekler: bütün bitler açık bir maske
// çekirdek tarafından izin verilen kümeyle (cpuset/çevrimiçi) kesiştirilir,
// yani "her yere geri dön" demektir.
func cpuMask(cpus []int) (mask [128]byte, ok bool) {
	if len(cpus) == 0 {
		for i := range mask {
			mask[i] = 0xff
		}
		return mask, true
	}
	for _, c := range cpus {
		if c >= 0 && c < len(mask)*8 {
			mask[c/8] |= 1 << (uint(c) % 8)
			ok = true
		}
	}
	return mask, ok
}

func setTaskAffinity(tid int, mask *[128]byte) error {
	_, _, e := syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY,
		uintptr(tid), uintptr(len(mask)), uintptr(unsafe.Pointer(&mask[0])))
	if e != 0 {
		return e
	}
	return nil
}

// taskIDs lists every thread of a process from /proc/<pid>/task.
func taskIDs(pid int) []int {
	f, err := os.Open("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil {
		return nil
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	out := make([]int, 0, len(names))
	for _, n := range names {
		if t, err := strconv.Atoi(n); err == nil {
			out = append(out, t)
		}
	}
	return out
}

// pinAllThreads, sürecin TÜM iş parçacıklarını verilen çekirdeklere sabitler
// ve sabitlenen iş parçacığı sayısını döner.
//
// ── Yakalanan hata ──────────────────────────────────────────────────────────
// Eski setAffinity sched_setaffinity(pid) çağırıyordu. Linux'ta bu YALNIZCA
// o kimlikteki iş parçacığını (ana iş parçacığını) etkiler. Süreç yeni
// başlarken bu yeterliydi (sonraki iş parçacıkları maskeyi miras alır), ama
// çalışan bir JVM'i (50-100 iş parçacığı: sunucu döngüsü, GC, ağ, parça
// üretimi) turbo açılınca P-çekirdeklerine taşımak için her birini tek tek
// sabitlemek gerekir. Sınama: TestPinAllThreadsPinsEveryThread.
//
// Listeleme ile sabitleme arasında doğan iş parçacığı, henüz taşınmamış
// ebeveyninin maskesini alabilir; bu yüzden yeni kimlik çıkmayana dek (en
// çok 5 tur) yeniden listelenir.
func pinAllThreads(pid int, cpus []int) int {
	mask, ok := cpuMask(cpus)
	if !ok || pid <= 0 {
		return 0
	}
	done := map[int]bool{}
	for pass := 0; pass < 5; pass++ {
		fresh := 0
		for _, tid := range taskIDs(pid) {
			if done[tid] {
				continue
			}
			fresh++
			if setTaskAffinity(tid, &mask) == nil {
				done[tid] = true
			}
		}
		if fresh == 0 {
			break
		}
	}
	return len(done)
}

// niceAllThreads applies a nice value to every thread. Linux'ta nice de iş
// parçacığı başınadır (setpriority(PRIO_PROCESS, tid)); yalnızca ana iş
// parçacığına uygulamak çalışan bir JVM'de etkisizdir.
func niceAllThreads(pid, nice int) {
	for _, tid := range taskIDs(pid) {
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, tid, nice)
	}
}

// mainThreadName: Minecraft sunucusunun oyun döngüsünü (tik) çalıştıran iş
// parçacığının adı. Vanilla, Paper/Purpur, Fabric ve Forge'da aynıdır; Linux
// adı 15 bayta kırpar, bu ad (13 bayt) sığar.
const mainThreadName = "Server thread"

// threadName reads a thread's kernel name (/proc/<pid>/task/<tid>/comm).
func threadName(pid, tid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/task/" + strconv.Itoa(tid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// pinTurboThreads: YALNIZCA oyun döngüsü iş parçacığını main çekirdeklerine
// sabitler; diğer bütün iş parçacıklarını TÜM çekirdeklere açar. Ana iş
// parçacığı bulunduysa true döner.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Kullanıcı (gerçek PC, Chunky ile dünya üretimi): "turboyu kapatınca 50 cps,
// açınca 30 cps". Turbo sürecin TÜM iş parçacıklarını P-çekirdeklerine
// sabitliyordu. Hibrit işlemcide bu, parça üretim işçilerinin, ağın ve
// GC'nin E-çekirdeklerini kaybetmesi demekti; JVM başlarken bu maskeyi
// görünce işçi havuzlarını da küçültüyordu. Minecraft'ta tek iş parçacığına
// bağlı olan yalnızca tik döngüsüdür: onu en hızlı çekirdeğe koymak
// yeterli, geri kalanı her yerde koşmalı.
func pinTurboThreads(pid int, main []int) (n int, found bool) {
	mainMask, ok := cpuMask(main)
	allMask, _ := cpuMask(nil)
	if !ok || pid <= 0 {
		return 0, false
	}
	for _, tid := range taskIDs(pid) {
		m := &allMask
		if threadName(pid, tid) == mainThreadName {
			m = &mainMask
			found = true
		}
		if setTaskAffinity(tid, m) == nil {
			n++
		}
	}
	return n, found
}

// PinProcess sabitler (bir sürecin tüm iş parçacıkları). Daemon turbo
// açıkken MCOS'un kendi süreçlerini (panel, mcosd) E-çekirdeklerine çeker.
func PinProcess(pid int, cpus []int) int { return pinAllThreads(pid, cpus) }
