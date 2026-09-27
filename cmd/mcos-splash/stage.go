package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// AÇILIŞ EKRANINDAKİ HER ŞEY GERÇEK
// ════════════════════════════════════════════════════════════════════════════
//
// ── Kullanıcının isteği ─────────────────────────────────────────────────────
//
//	"acılıs ekranındaki her sey gercek olsun"
//
// ── Neden gerekliydi ────────────────────────────────────────────────────────
//
// İlerleme halkası GERÇEK DEĞİLDİ. Yüzde şu eğriden geliyordu:
//
//	pct := int(92 * (1 - math.Exp(-el.Seconds()/4.0)))
//
// Yani yalnızca GEÇEN SÜREYE bakıyordu: sistem takılsa da, iki kat hızlı
// açılsa da halka aynı hızla doluyordu. Hızlı bir makinede panel %40'ta
// açılıyor, yavaş bir makinede halka %92'de donup bekliyordu. İlerleme
// çubuğunun anlatması gereken tek şeyi — "ne kadarı bitti" — anlatmıyordu.
//
// ── Yenisi ──────────────────────────────────────────────────────────────────
//
// Açılışın GERÇEK adımları numaralandırıldı. Her adım BİTTİĞİNDE onu bitiren
// betik durumu yazıyor (mcos-stage yardımcısı). Yüzde, tamamlanan adımın
// ağırlığından geliyor; süreden değil.
//
// Biçim tek satır:  "<pct>|<metin>"   ya da yalnızca "<metin>" (eski biçim).
//
// Eski biçim hâlâ kabul ediliyor: bir betik güncellenmeyi unutursa açılış
// ekranı yine de çalışır, yalnızca yüzde son bilinen değerde kalır.

// stageInfo is one parsed status line.
type stageInfo struct {
	pct  int // -1 = satırda yüzde yok
	text string
}

// parseStage reads "<pct>|<text>" or a bare text line.
func parseStage(line string) stageInfo {
	s := strings.TrimSpace(line)
	if s == "" {
		return stageInfo{pct: -1}
	}
	if i := strings.IndexByte(s, '|'); i > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(s[:i])); err == nil {
			if n < 0 {
				n = 0
			}
			if n > 100 {
				n = 100
			}
			return stageInfo{pct: n, text: strings.TrimSpace(s[i+1:])}
		}
	}
	return stageInfo{pct: -1, text: s}
}

// readStageInfo reads the status file written by the boot scripts.
func readStageInfo(path string) stageInfo {
	if path == "" {
		return stageInfo{pct: -1}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return stageInfo{pct: -1}
	}
	s := string(data)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	st := parseStage(s)
	// Çok uzun bir satır ekranın dışına taşar; kesmek tek doğru davranış.
	if r := []rune(st.text); len(r) > 64 {
		st.text = string(r[:63]) + "…"
	}
	return st
}

// ── Gerçek donanım satırı ───────────────────────────────────────────────────
//
// Açılış ekranının altındaki satır eskiden sabit bir slogan yazıyordu. Artık
// MAKİNENİN KENDİSİNİ yazıyor: işlemci modeli, çekirdek sayısı ve bellek.
// Kullanıcı ilk açılışta MCOS'un donanımı gerçekten gördüğünü buradan anlar —
// "sistem beni tanıdı mı?" sorusunun cevabı.
//
// Her şey /proc'tan okunuyor: hiçbir tahmin, hiçbir sabit değer yok. Okuma
// başarısızsa satır BOŞ bırakılır; uydurma bir değer yazmak, tam da
// kaçınılmak istenen şeydir.

// hardwareLine returns "Intel Core i5-9400F · 6 çekirdek · 16 GB" or "".
func hardwareLine() string {
	cpu := cpuModel()
	cores := cpuCores()
	mem := memTotalGB()

	var parts []string
	if cpu != "" {
		parts = append(parts, cpu)
	}
	if cores > 0 {
		parts = append(parts, strconv.Itoa(cores)+" çekirdek")
	}
	if mem != "" {
		parts = append(parts, mem)
	}
	return strings.Join(parts, "  ·  ")
}

// cpuModel reads the first "model name" from /proc/cpuinfo.
func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "model name") {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[i+1:])
		// Pazarlama süsleri ekranda yer kaplıyor ve bilgi taşımıyor.
		for _, junk := range []string{"(R)", "(TM)", "(tm)", "CPU ", "Processor"} {
			name = strings.ReplaceAll(name, junk, "")
		}
		name = strings.Join(strings.Fields(name), " ")
		// Frekans kuyruğunu at: "@ 2.90GHz" zaten /proc/cpuinfo'da ayrı var
		// ve satırı uzatıyor.
		if i := strings.Index(name, " @ "); i > 0 {
			name = name[:i]
		}
		if r := []rune(name); len(r) > 40 {
			name = string(r[:39]) + "…"
		}
		return name
	}
	return ""
}

// cpuCores counts "processor" lines in /proc/cpuinfo.
//
// runtime.NumCPU KULLANILMIYOR: o, sürecin ZAMANLAMA MASKESİNE göre görünen
// çekirdek sayısıdır (cgroup/taskset ile kısıtlanmış olabilir). Açılış
// ekranında makinenin gerçek çekirdek sayısı yazmalı.
func cpuCores() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "processor") && strings.Contains(line, ":") {
			n++
		}
	}
	return n
}

// memTotalGB reads MemTotal from /proc/meminfo as "16 GB".
func memTotalGB() string {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return ""
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			return ""
		}
		// MemTotal, çekirdeğin AYIRDIĞI belleği içermez; bu yüzden 16 GB'lık
		// bir makinede 15,6 GB görünür. En yakın yaygın boyuta yuvarlamak
		// yalan olurdu — "15,6 GB" gerçektir ve öyle yazılır.
		gb := float64(kb) / (1024 * 1024)
		if gb >= 10 {
			return strconv.FormatFloat(gb, 'f', 0, 64) + " GB"
		}
		return strings.Replace(
			strconv.FormatFloat(gb, 'f', 1, 64), ".", ",", 1) + " GB"
	}
	return ""
}
