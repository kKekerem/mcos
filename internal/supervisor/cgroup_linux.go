//go:build linux

package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Bu dosya kaynak sınırlarını GERÇEKTEN uygular.
//
// ── Neden yazıldı ───────────────────────────────────────────────────────────
// "Sunucu başına CPU payı" ayarı arayüzde vardı, yapılandırmaya kaydediliyordu
// ve sunucu ayrıntılarında gösteriliyordu — ama HİÇBİR ŞEY YAPMIYORDU. Depo
// genelinde tek bir cgroup çağrısı yoktu; çekirdek yapılandırmasında cgroup
// desteği bile derlenmemişti. Yani ayar tamamen görüntüden ibaretti.
//
// Aynı şekilde "Turbo" yalnızca nice değerini -10 yapıyor ve bellek tavanını
// atlıyordu; CPU tarafında hiçbir etkisi yoktu.
//
// ── Neden cgroup v2 ─────────────────────────────────────────────────────────
// Tek birleşik hiyerarşi, basit dosya arayüzü (cpu.max, memory.max), BusyBox
// ile sorunsuz. v1'in denetleyici başına ayrı ağacı bu boyutta bir cihazda
// gereksiz karmaşıklık.
//
// ── Dayanıklılık ────────────────────────────────────────────────────────────
// cgroup2 bağlı değilse (eski çekirdekle derlenmiş bir imaj, ya da geliştirme
// makinesi) tüm çağrılar sessizce hiçbir şey yapmaz. Kaynak sınırı
// uygulayamamak, sunucuyu hiç başlatmamaktan iyidir.

// cgroupRoot is where the unified hierarchy is mounted (see /etc/fstab).
const cgroupRoot = "/sys/fs/cgroup"

// mcosSlice is the parent group holding every managed server.
//
// Ayrı bir üst grup: MCOS'un yönettiği süreçleri sistemin geri kalanından
// ayırır, böylece bir sınır yanlışlıkla daemon'un veya panelin kendisini
// boğmaz.
const mcosSlice = "mcos"

// Limits describes the OS-level resource ceiling for one server process.
type Limits struct {
	// CPUPercent is the share of ONE core, 0 = unlimited.
	// 100 = one full core, 400 = four cores.
	CPUPercent int
	// MemoryMB is a hard memory ceiling, 0 = unlimited.
	//
	// Bu, -Xmx'ten FARKLI ve ondan daha güçlüdür: JVM yığın dışında da bellek
	// kullanır (metaspace, doğrudan arabellekler, iş parçacığı yığınları).
	// -Xmx yalnızca yığını sınırlar; bu, sürecin TAMAMINI sınırlar.
	MemoryMB int
	// IOWeight is the relative disk bandwidth share (1..10000, 0 = default).
	IOWeight int
}

// cgroupAvailable reports whether the unified hierarchy is usable.
func cgroupAvailable() bool {
	// cgroup.controllers yalnızca cgroup2 bağlıyken vardır; varlığı hem
	// bağlamayı hem de sürümü tek seferde doğrular.
	_, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers"))
	return err == nil
}

// enableControllers makes cpu/memory/io available to child groups.
//
// cgroup v2'de bir denetleyici, ÜST grubun subtree_control dosyasına
// yazılmadan alt gruplarda kullanılamaz. Bu adım atlanırsa cpu.max dosyası
// hiç oluşmaz ve sınır sessizce uygulanmaz — tam da yakalamak istediğimiz
// sessiz başarısızlık türü.
func enableControllers(dir string) error {
	avail, err := os.ReadFile(filepath.Join(dir, "cgroup.controllers"))
	if err != nil {
		return err
	}
	var want []string
	for _, c := range []string{"cpu", "memory", "io", "pids"} {
		if strings.Contains(string(avail), c) {
			want = append(want, "+"+c)
		}
	}
	if len(want) == 0 {
		return nil
	}
	// Hepsini tek yazımda denemek, biri desteklenmiyorsa TAMAMINI düşürür;
	// bu yüzden tek tek yazıyoruz.
	f := filepath.Join(dir, "cgroup.subtree_control")
	for _, w := range want {
		_ = os.WriteFile(f, []byte(w), 0o644)
	}
	return nil
}

// groupPath returns the cgroup directory for a server id.
func groupPath(id string) string {
	return filepath.Join(cgroupRoot, mcosSlice, "srv-"+sanitizeID(id))
}

// sanitizeID keeps only characters that are safe in a directory name.
func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		s = "unknown"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// ApplyCgroup creates (or updates) a cgroup for the server and moves pid into
// it. Returns a short human-readable description of what was applied.
//
// Boş açıklama = hiçbir sınır uygulanmadı (cgroup yok veya sınır istenmedi).
func ApplyCgroup(id string, pid int, lim Limits) string {
	if !cgroupAvailable() {
		return ""
	}

	slice := filepath.Join(cgroupRoot, mcosSlice)
	if err := os.MkdirAll(slice, 0o755); err != nil {
		return ""
	}
	// Denetleyiciler HEM kökte HEM üst grupta açılmalı.
	_ = enableControllers(cgroupRoot)
	_ = enableControllers(slice)

	dir := groupPath(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}

	var applied []string

	// ── CPU ─────────────────────────────────────────────────────────────
	//
	// cpu.max biçimi: "<kota> <periyot>", ikisi de mikrosaniye.
	// 100000 (100 ms) standart periyot; kota 100000 = tam bir çekirdek.
	if lim.CPUPercent > 0 {
		const period = 100000
		quota := lim.CPUPercent * period / 100
		if quota < 1000 {
			quota = 1000 // altında zamanlayıcı anlamsız derecede kısıtlar
		}
		if err := os.WriteFile(filepath.Join(dir, "cpu.max"),
			[]byte(fmt.Sprintf("%d %d", quota, period)), 0o644); err == nil {
			applied = append(applied, fmt.Sprintf("CPU %%%d", lim.CPUPercent))
		}
	} else {
		// Sınırsız: önceki bir sınır kalmışsa temizle.
		_ = os.WriteFile(filepath.Join(dir, "cpu.max"), []byte("max 100000"), 0o644)
	}

	// ── Bellek ──────────────────────────────────────────────────────────
	if lim.MemoryMB > 0 {
		bytes := int64(lim.MemoryMB) * 1024 * 1024
		if err := os.WriteFile(filepath.Join(dir, "memory.max"),
			[]byte(strconv.FormatInt(bytes, 10)), 0o644); err == nil {
			applied = append(applied, fmt.Sprintf("bellek %d MB", lim.MemoryMB))
		}
		// memory.high biraz altta: sert sınıra çarpıp OOM ile öldürülmek
		// yerine önce baskı uygulanır ve JVM geri çekilme şansı bulur.
		_ = os.WriteFile(filepath.Join(dir, "memory.high"),
			[]byte(strconv.FormatInt(bytes*90/100, 10)), 0o644)
	} else {
		_ = os.WriteFile(filepath.Join(dir, "memory.max"), []byte("max"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "memory.high"), []byte("max"), 0o644)
	}

	// ── Disk ────────────────────────────────────────────────────────────
	if lim.IOWeight > 0 {
		_ = os.WriteFile(filepath.Join(dir, "io.weight"),
			[]byte(strconv.Itoa(lim.IOWeight)), 0o644)
	}

	// ── Süreci gruba taşı ───────────────────────────────────────────────
	//
	// EN SON yapılır: sınırlar süreç girmeden önce yerinde olmalı, yoksa
	// süreç kısa bir süre sınırsız çalışır.
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"),
		[]byte(strconv.Itoa(pid)), 0o644); err != nil {
		return "" // gruba giremediyse sınırların hiçbiri geçerli değil
	}

	if len(applied) == 0 {
		return "sınırsız"
	}
	return strings.Join(applied, ", ")
}

// RemoveCgroup deletes a server's group after it exits.
//
// Temizlenmezse her başlatma yeni bir dizin bırakır; binlerce boş cgroup
// çekirdek belleğini boşa harcar ve /sys/fs/cgroup listesini okunamaz yapar.
func RemoveCgroup(id string) {
	if !cgroupAvailable() {
		return
	}
	// Süreçler çıkmadan dizin silinemez; bu yüzden hata yok sayılır ve
	// bir sonraki başlatmada aynı dizin yeniden kullanılır.
	_ = os.Remove(groupPath(id))
}

// CgroupStatus reports what is actually enforced right now.
//
// Arayüz bunu gösterir: "uygulanıyor" ile "yalnızca kayıtlı" arasındaki farkı
// kullanıcının GÖREBİLMESİ gerekir — eskiden göremiyordu.
func CgroupStatus() (available bool, controllers []string) {
	if !cgroupAvailable() {
		return false, nil
	}
	b, err := os.ReadFile(filepath.Join(cgroupRoot, "cgroup.controllers"))
	if err != nil {
		return true, nil
	}
	return true, strings.Fields(string(b))
}
