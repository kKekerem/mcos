package model

import (
	"fmt"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// AYNI MAKİNEDE BÖLÜNMÜŞ DÜNYA — bir dünya, birden çok sunucu süreci
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "1000 kişiden fazla girecekse aynı makinede bir sunucu
// daha açsın, PC eşleştirmesiyle aynı mantıkla dünya iki sunucu arasında
// bölünsün; sunucu menüsünde tek sunucu gibi görünsün."
//
// Kardeş kopyalar ayrı Minecraft süreçleridir; dünya X dilimlerine bölünür ve
// oyuncu sınırı geçince aktarılır — tıpkı eşleşmiş iki PC gibi (bkz. link.go).
// Tek fark, düğümlerin aynı makinede olmasıdır.

// MaxInstances, bir makinede açılacak en fazla kopya sayısıdır.
//
// 8: her kopya kendi JVM'ini, yığınını ve dünya kaydını taşır; daha fazlası
// tek makinede belleği ve diski kopya başına bölüştürüp hepsini yavaşlatır.
const MaxInstances = 8

// PlayersPerInstance, OTOMATİK kipte bir kopyaya düşen oyuncu sayısıdır.
//
// NEDEN 1000: Minecraft dünyayı TEK bir ana iş parçacığında (tick döngüsü)
// simüle eder. Tek bir JVM'in ana iş parçacığı ~1000 oyuncu civarında 20 TPS'i
// tutturamaz hale gelir; çekirdek eklemek bunu çözmez, ikinci bir süreç çözer.
const PlayersPerInstance = 1000

// instanceRAMMB, otomatik hesapta kopya başına ayrılan bellektir (2 GB).
const instanceRAMMB = 2048

// instanceReserveMB, işletim sistemi ve MCOS için ayrılan bellektir (2 GB).
const instanceReserveMB = 2048

// AutoInstanceCount picks how many copies to run for maxPlayers.
//
// ceil(maxPlayers/1000), sonra makinenin kaldırabileceğiyle sınırlanır:
//   - mantıksal çekirdek/2: her kopyanın ana iş parçacığı bir çekirdeği
//     tamamen doldurur, GC ve ağ iş parçacıkları da bir o kadar ister;
//   - (bellek − 2 GB)/2 GB: sistem için 2 GB bırakılır, her kopya en az
//     2 GB yığınla açılır — daha azında kopya açmak tek sunucudan kötüdür;
//   - en fazla MaxInstances.
//
// Sonuç her zaman ≥ 1'dir: makine zayıfsa dünya bölünmez, tek sunucu kalır.
// cores/ramMB ≤ 0 (bilinmiyor) o sınırı devre dışı bırakır.
func AutoInstanceCount(maxPlayers, logicalCores, ramMB int) int {
	n := (maxPlayers + PlayersPerInstance - 1) / PlayersPerInstance
	limit := MaxInstances
	if logicalCores > 0 {
		limit = min(limit, logicalCores/2)
	}
	if ramMB > 0 {
		limit = min(limit, (ramMB-instanceReserveMB)/instanceRAMMB)
	}
	return max(1, min(n, limit))
}

// InstanceCount is how many copies srv should run on this host (≥ 1).
//
// Elle seçilen sayı yalnızca MaxInstances ile sınırlanır: kullanıcı bilerek
// "4" dediyse makinenin gücünü tahmin edip onu düzeltmeyiz.
func (s *Server) InstanceCount(logicalCores, ramMB int) int {
	if s == nil || s.ParentID != "" {
		return 1
	}
	if s.InstancesAuto {
		return AutoInstanceCount(s.MaxPlayers, logicalCores, ramMB)
	}
	return max(1, min(s.Instances, MaxInstances))
}

// IsSibling reports whether s is a hidden copy of another server.
func (s *Server) IsSibling() bool { return s != nil && s.ParentID != "" }

// SiblingName is the (hidden) record name of copy idx of main.
func SiblingName(main string, idx int) string {
	return fmt.Sprintf("%s #%d", main, idx)
}

// InstanceNodeName is the shared-world node name of local copy idx.
//
// Hem koordinatör (topoloji) hem sunucu yöneticisi (MCOS_LINK_SELF) bunu
// kullanır; iki yerde ayrı ayrı yazılsaydı biri değiştiğinde mod kendini
// topolojide bulamaz ve dilimini sahiplenemezdi.
func InstanceNodeName(machine string, idx int) string {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		machine = "mcos"
	}
	return fmt.Sprintf("%s-%d", machine, idx)
}

// HideSiblings drops sibling copies from a server list.
//
// Kullanıcının isteği: "sunucu menüsünde tek sunucu gibi görünsün". Kardeşler
// yine de diskte ve port listesinde (portmgr) durur; yalnızca liste gizler.
func HideSiblings(list []*Server) []*Server {
	out := list[:0:0]
	for _, s := range list {
		if !s.IsSibling() {
			out = append(out, s)
		}
	}
	return out
}
