package server

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ── Sunucu yığınını GERÇEKTEN boş olan belleğe sığdırma ─────────────────────
//
// YAKALANAN HATA (kullanıcı, VirtualBox ve gerçek PC): "sunucu açılıyor
// diyor, sonra işlem kesildi, çıkış kodu -1, signal: killed". Sebep çekirdeğin
// OOM katili:
//
//   - Sunucuya ayrılan RAM (varsayılan 2048 MB) -Xms VE -Xmx olarak veriliyor,
//     Aikar ayarlarındaki -XX:+AlwaysPreTouch da yığının TAMAMINI açılışta
//     fiziksel belleğe dokunarak ayırıyor.
//   - MCOS'un kök dosya sistemi RAM'de (initramfs; firmware'lerle birlikte
//     yüzlerce MB) ve bu bellek geri alınamaz.
//   - İstenen yığın + JVM'in yığın dışı belleği (metaspace, kod önbelleği,
//     iş parçacıkları; cgroup tavanıyla aynı hesap: yığın*1,25+256 MB) boş
//     belleği aşınca çekirdek Java'yı SIGKILL ile öldürüyordu. Sunucu her
//     otomatik yeniden başlatmada AYNI boyutla açıldığı için sonsuza dek
//     ölüyordu.
//
// Artık her başlatmada /proc/meminfo'daki MemAvailable okunur; istenen yığın
// sığmıyorsa sığan en büyük değere düşürülür ve kullanıcıya konsolda NEDEN
// söylenir. Bellek darsa ön dokunma (AlwaysPreTouch) kapatılır: sayfalar
// kullanıldıkça ayrılır, açılışta bir anda değil.

// memReserveMB, sistemin geri kalanına (panel, daemon, sayfa önbelleği,
// ağ) bırakılan pay. Altında panel donmaya ve diğer sunucular sıkışmaya
// başlar.
const memReserveMB = 384

// memMinHeapMB altına düşürülmez: daha küçük bir yığınla modern bir Paper
// sunucusu dünyayı yükleyemez; o durumda denemek ve açık bir uyarı vermek
// sessizce hiç başlatmamaktan iyidir.
const memMinHeapMB = 512

// preTouchSlackMB: ön dokunmanın açık kalması için gereken ek boş bellek.
// AlwaysPreTouch ısınma süresini düzgünleştirir ama belleği bir anda ister;
// ancak bol bellek varken değerli.
const preTouchSlackMB = 1024

// memNeedMB, bir yığın boyutu için JVM'in toplam gereksinimi (cgroup
// tavanıyla aynı formül: bkz. limitsForServer).
func memNeedMB(heapMB int) int { return heapMB + heapMB/4 + 256 }

// memPlan is the heap decision for one launch.
type memPlan struct {
	HeapMB   int  // -Xms/-Xmx olarak kullanılacak
	PreTouch bool // -XX:+AlwaysPreTouch kalsın mı
	Reduced  bool // istenenden küçük mü
	WantMB   int  // kullanıcının istediği
	AvailMB  int  // ölçülen MemAvailable (0 = okunamadı)
}

// planMemory fits the wanted heap into the available memory.
//
// availMB <= 0 (ölçülemedi) ise karar değişmez: tahminle kullanıcının
// ayarını kesmek, ölçümle kesmekten daha kötü olurdu.
func planMemory(wantMB, availMB int) memPlan {
	p := memPlan{HeapMB: wantMB, PreTouch: true, WantMB: wantMB, AvailMB: availMB}
	if wantMB <= 0 || availMB <= 0 {
		return p
	}
	usable := availMB - memReserveMB
	if memNeedMB(wantMB) <= usable {
		p.PreTouch = usable-memNeedMB(wantMB) >= preTouchSlackMB
		return p
	}
	// need(h) = h*5/4 + 256 <= usable  ->  h <= (usable-256)*4/5
	heap := (usable - 256) * 4 / 5
	heap = heap / 128 * 128
	if heap < memMinHeapMB {
		heap = memMinHeapMB
	}
	if heap > wantMB {
		heap = wantMB
	}
	p.HeapMB = heap
	p.Reduced = heap < wantMB
	p.PreTouch = false
	return p
}

// memAvailableMB reads MemAvailable from /proc/meminfo (MB). Testte
// değiştirilir.
var memAvailableMB = func() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		alan := strings.Fields(sc.Text())
		if len(alan) >= 2 && alan[0] == "MemAvailable:" {
			kb, err := strconv.Atoi(alan[1])
			if err != nil {
				return 0
			}
			return kb / 1024
		}
	}
	return 0
}

// dropPreTouch removes -XX:+AlwaysPreTouch from JVM args.
func dropPreTouch(args []string) []string {
	out := args[:0:0]
	for _, a := range args {
		if a == "-XX:+AlwaysPreTouch" {
			continue
		}
		out = append(out, a)
	}
	return out
}
