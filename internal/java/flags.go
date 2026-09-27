package java

import (
	"fmt"
	"strings"
)

// FlagProfile names a JVM tuning profile.
const (
	ProfileAikar   = "aikar"
	ProfileDefault = "default"
	// ProfileTurbo is Aikar's tuning plus extra throughput flags, used when the
	// global Turbo switch is on. It hands the GC all the host's cores and lets
	// the JVM use NUMA/large pages where available.
	ProfileTurbo = "turbo"
)

// JVMArgs builds the JVM argument list (excluding -jar / nogui) for a server,
// given a tuning profile, heap size in MB, and the Java major version.
//
// "aikar" applies the well-known G1GC tuning used across the Minecraft hosting
// community, with the large-heap variant kicking in at >=12 GB. Java 8 omits a
// couple of options it doesn't understand. "default" is a minimal heap-only set.
func JVMArgs(profile string, ramMB, major int) []string {
	if ramMB <= 0 {
		ramMB = 1024
	}
	heap := fmt.Sprintf("%dM", ramMB)
	base := []string{"-Xms" + heap, "-Xmx" + heap}

	// Turbo = Aikar core + extra throughput flags scaled to the host's cores.
	if strings.EqualFold(profile, ProfileTurbo) {
		return turboArgs(ramMB, major)
	}

	if !strings.EqualFold(profile, ProfileAikar) {
		return base
	}

	large := ramMB >= 12*1024
	g1NewSize, g1MaxNewSize, regionSize, reserve, ihop := "30", "40", "8M", "20", "15"
	if large {
		g1NewSize, g1MaxNewSize, regionSize, reserve, ihop = "40", "50", "16M", "15", "20"
	}

	args := append(base,
		"-XX:+UseG1GC",
		"-XX:+ParallelRefProcEnabled",
		"-XX:MaxGCPauseMillis=200",
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+DisableExplicitGC",
		"-XX:+AlwaysPreTouch",
		"-XX:G1NewSizePercent="+g1NewSize,
		"-XX:G1MaxNewSizePercent="+g1MaxNewSize,
		"-XX:G1HeapRegionSize="+regionSize,
		"-XX:G1ReservePercent="+reserve,
		"-XX:G1HeapWastePercent=5",
		"-XX:G1MixedGCCountTarget=4",
		"-XX:InitiatingHeapOccupancyPercent="+ihop,
		"-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:G1RSetUpdatingPauseTimePercent=5",
		"-XX:SurvivorRatio=32",
		"-XX:+PerfDisableSharedMem",
		"-XX:MaxTenuringThreshold=1",
	)
	// Büyük sayfalar (2 MB): büyük yığında TLB ıskalarını azaltır. Çekirdek
	// THP'yi "madvise" kipinde açar (S02mcostune); bu bayrak JVM'in yığını
	// için madvise ister. Java 8'in bu bayrağı güvenilir değil.
	if major >= 11 {
		args = append(args, "-XX:+UseTransparentHugePages")
	}
	// Java 8 predates a couple of the experimental knobs above being stable; it
	// still accepts the core G1 set, so we only drop the riskiest extras.
	if major <= 8 {
		args = removeFlags(args, "-XX:G1MixedGCLiveThresholdPercent=90", "-XX:G1RSetUpdatingPauseTimePercent=5")
	}
	args = append(args,
		"-Dusing.aikars.flags=https://mcflags.emc.gs",
		"-Daikars.new.flags=true",
	)
	return args
}

// turboArgs builds the aggressive Turbo profile: Aikar's flags plus parallel/
// concurrent GC thread counts pinned to the host's cores and NUMA awareness.
// Java 8 omits the experimental thread knobs it doesn't accept.
func turboArgs(ramMB, major int) []string {
	// ── Yakalanan gerçek hata: turbo JVM'i YAVAŞLATIYORDU ────────────────
	//
	// Kullanıcı (gerçek PC, Chunky ile dünya üretimi): "turbo kapalı 50 cps,
	// açık 30 cps". Buradaki "iş hacmi" bayrakları üretimi boğuyordu:
	//   - ConcGCThreads = çekirdeklerin YARISI: G1'in eşzamanlı işaretleme
	//     aşaması (Minecraft'ta sürekli) parça üretim işçilerinin yarım
	//     makinesini alıyordu (varsayılan: ParallelGCThreads'in dörtte biri).
	//   - ParallelGCThreads = tüm mantıksal çekirdekler (E + SMT dahil):
	//     duraklamalar kısalmıyor, önbellek çekişmesi artıyordu.
	//   - UseStringDeduplication: Minecraft'ta kazancı yok, GC'ye iş ekler.
	//   - UseNUMA: tek soketli makinede anlamsız.
	// JVM'in kendi ergonomisi bunları çekirdek sayısına göre zaten doğru
	// seçer. Turbo artık JVM'e dokunmuyor: kazanç işlemci frekansı, güç
	// sınırları, fanlar ve oyun döngüsünün P-çekirdeğine alınmasından geliyor
	// (internal/turbo, supervisor/turbo.go).
	return JVMArgs(ProfileAikar, ramMB, major)
}

func removeFlags(args []string, drop ...string) []string {
	want := map[string]bool{}
	for _, d := range drop {
		want[d] = true
	}
	out := args[:0:0]
	for _, a := range args {
		if !want[a] {
			out = append(out, a)
		}
	}
	return out
}
