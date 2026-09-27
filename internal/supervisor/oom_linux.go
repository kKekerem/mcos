package supervisor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ── Bellek yetmediği için öldürülme (OOM) tespiti ───────────────────────────
//
// Kullanıcı yalnızca "çıkış kodu -1, signal: killed" görüyordu ve sebebini
// anlayamıyordu. SIGKILL'in en yaygın sebebi çekirdeğin OOM katilidir: ya
// sunucunun kendi bellek tavanı (cgroup memory.max) aşıldı ya da sistemin
// tamamında bellek bitti. İkisi de sayaçlardan okunabilir:
//   - cgroup v2 memory.events içindeki "oom_kill" (yalnızca bu sunucu),
//   - /proc/vmstat içindeki "oom_kill" (tüm sistem; açılıştan beri toplam).

// cgroupOOMKills returns the oom_kill count of a server's cgroup.
func cgroupOOMKills(id string) int {
	b, err := os.ReadFile(filepath.Join(groupPath(id), "memory.events"))
	if err != nil {
		return 0
	}
	return sayac(string(b), "oom_kill")
}

// globalOOMKills returns the system-wide OOM kill counter.
func globalOOMKills() int {
	b, err := os.ReadFile("/proc/vmstat")
	if err != nil {
		return 0
	}
	return sayac(string(b), "oom_kill")
}

// sayac, "ad deger" satırlarından birini okur.
func sayac(metin, ad string) int {
	for _, satir := range strings.Split(metin, "\n") {
		alan := strings.Fields(satir)
		if len(alan) == 2 && alan[0] == ad {
			n, _ := strconv.Atoi(alan[1])
			return n
		}
	}
	return 0
}
