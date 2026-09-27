package turbo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Topology çekirdeklerin performans (P) ve verimlilik (E) ayrımıdır.
type Topology struct {
	Online []int
	// PCores sunucuların sabitleneceği çekirdekler. Hibrit olmayan
	// işlemcide Online ile aynıdır.
	PCores []int
	// ECores hibrit işlemcide verimlilik çekirdekleri; değilse boş.
	ECores []int
	// Method ayrımın nasıl bulunduğu (kullanıcıya kanıt olarak gösterilir).
	Method string
}

// eRatio: bir çekirdeğin E sayılması için azami frekansının (ya da
// kapasitesinin) en yüksek çekirdeğe oranı bunun ALTINDA olmalı.
//
// Neden 0,85: Turbo Boost Max 3.0'lı işlemcilerde "gözde" çekirdekler
// diğerlerinden %3-5 daha hızlıdır (ör. 5,3 / 5,1 GHz). Eşik çok yüksek
// olsaydı hibrit OLMAYAN bir işlemcide yalnızca iki gözde çekirdek "P"
// sayılır, sunucu iki çekirdeğe hapsolurdu. Gerçek hibritlerde oran
// 0,55-0,78 (Alder Lake 3,6/4,7; Raptor Lake 4,3/5,8; Meteor Lake LP-E
// 2,5/4,8; Zen4c 3,7/5,0).
const eRatio = 0.85

const cpuDir = "sys/devices/system/cpu"

// DetectTopology P/E çekirdeklerini sırayla şu kanıtlardan bulur:
//  1. /sys/devices/cpu_core/cpus ve cpu_atom/cpus — Intel hibrit PMU'su;
//     çekirdeğin kendi bildirdiği, en güvenilir kaynak.
//  2. cpuN/cpu_capacity — zamanlayıcının kapasite değerleri (varsa).
//  3. cpuN/cpufreq/cpuinfo_max_freq kümelemesi — sürücünün çekirdek başına
//     bildirdiği azami frekans (AMD Zen4c ve PMU'suz durumlar için).
//
// Hiçbiri ayrım göstermezse tüm çevrimiçi çekirdekler P sayılır.
func DetectTopology(root string) Topology {
	p := func(rel string) string { return filepath.Join(root, rel) }
	t := Topology{Online: onlineCPUs(root)}

	coreS, err1 := readStr(p("sys/devices/cpu_core/cpus"))
	atomS, err2 := readStr(p("sys/devices/cpu_atom/cpus"))
	if err1 == nil && err2 == nil {
		pc, ec := intersect(ParseList(coreS), t.Online), intersect(ParseList(atomS), t.Online)
		if len(pc) > 0 && len(ec) > 0 {
			t.PCores, t.ECores, t.Method = pc, ec, "Intel hibrit PMU (cpu_core/cpu_atom)"
			return t
		}
	}

	if pc, ec := cluster(t.Online, func(cpu int) int64 {
		return readInt(p(fmt.Sprintf("%s/cpu%d/cpu_capacity", cpuDir, cpu)))
	}); len(ec) > 0 {
		t.PCores, t.ECores, t.Method = pc, ec, "cpu_capacity"
		return t
	}

	if pc, ec := cluster(t.Online, func(cpu int) int64 {
		return readInt(p(fmt.Sprintf("%s/cpu%d/cpufreq/cpuinfo_max_freq", cpuDir, cpu)))
	}); len(ec) > 0 {
		t.PCores, t.ECores, t.Method = pc, ec, "çekirdek başına azami frekans"
		return t
	}

	t.PCores = append([]int(nil), t.Online...)
	t.Method = "hibrit değil"
	return t
}

// cluster, değeri en yüksek değerin eRatio katının altında kalan çekirdekleri
// E sayar. Değeri okunamayan (0) çekirdek varsa ayrım YAPILMAZ: eksik veriyle
// bir çekirdeği yanlışlıkla E sayıp sunucuyu ondan uzak tutmak, hiç
// ayırmamaktan kötüdür.
func cluster(cpus []int, val func(int) int64) (p, e []int) {
	if len(cpus) < 2 {
		return nil, nil
	}
	vals := make(map[int]int64, len(cpus))
	var top int64
	for _, c := range cpus {
		v := val(c)
		if v <= 0 {
			return nil, nil
		}
		vals[c] = v
		if v > top {
			top = v
		}
	}
	for _, c := range cpus {
		if float64(vals[c]) < float64(top)*eRatio {
			e = append(e, c)
		} else {
			p = append(p, c)
		}
	}
	if len(e) == 0 {
		return nil, nil
	}
	return p, e
}

// onlineCPUs çevrimiçi çekirdekleri okur; dosya yoksa cpuN dizinlerinden.
func onlineCPUs(root string) []int {
	if s, err := readStr(filepath.Join(root, cpuDir, "online")); err == nil {
		if l := ParseList(s); len(l) > 0 {
			return l
		}
	}
	m, _ := filepath.Glob(filepath.Join(root, cpuDir, "cpu[0-9]*"))
	var out []int
	for _, d := range m {
		if n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(d), "cpu")); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// ParseList "0-3,8,10-11" biçimindeki çekirdek listesini çözer.
func ParseList(s string) []int {
	var out []int
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if a, b, ok := strings.Cut(part, "-"); ok {
			lo, err1 := strconv.Atoi(a)
			hi, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil || hi < lo || hi-lo > 4096 {
				continue
			}
			for i := lo; i <= hi; i++ {
				out = append(out, i)
			}
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return dedupe(out)
}

// FormatList çekirdek listesini kısa biçimde yazar: [0 1 2 3 8] -> "0-3,8".
func FormatList(l []int) string {
	if len(l) == 0 {
		return ""
	}
	s := append([]int(nil), l...)
	sort.Ints(s)
	s = dedupe(s)
	var b strings.Builder
	for i := 0; i < len(s); {
		j := i
		for j+1 < len(s) && s[j+1] == s[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		if j > i {
			fmt.Fprintf(&b, "%d-%d", s[i], s[j])
		} else {
			fmt.Fprintf(&b, "%d", s[i])
		}
		i = j + 1
	}
	return b.String()
}

func dedupe(s []int) []int {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func intersect(a, b []int) []int {
	in := make(map[int]bool, len(b))
	for _, v := range b {
		in[v] = true
	}
	var out []int
	for _, v := range a {
		if in[v] {
			out = append(out, v)
		}
	}
	return out
}

// CoreMHz çekirdek başına ANLIK frekansı MHz olarak döner (dizin = çekirdek
// numarası, çevrimdışı çekirdek 0). cpufreq yoksa /proc/cpuinfo'daki
// "cpu MHz" satırlarına düşer (sanal makinede de bir değer görünsün).
func CoreMHz(root string) []int {
	online := onlineCPUs(root)
	if len(online) == 0 {
		return nil
	}
	out := make([]int, online[len(online)-1]+1)
	any := false
	for _, cpu := range online {
		base := filepath.Join(root, cpuDir, fmt.Sprintf("cpu%d/cpufreq", cpu))
		v := readInt(filepath.Join(base, "scaling_cur_freq"))
		if v <= 0 {
			v = readInt(filepath.Join(base, "cpuinfo_cur_freq"))
		}
		if v > 0 {
			out[cpu] = int(v / 1000)
			any = true
		}
	}
	if any {
		return out
	}
	f, err := os.Open(filepath.Join(root, "proc/cpuinfo"))
	if err != nil {
		return nil
	}
	defer f.Close()
	cur := -1
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "processor":
			cur, _ = strconv.Atoi(strings.TrimSpace(v))
		case "cpu MHz":
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && cur >= 0 && cur < len(out) {
				out[cur] = int(f + 0.5)
				any = true
			}
		}
	}
	if !any {
		return nil
	}
	return out
}
