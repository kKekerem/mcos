package supervisor

import "time"

// Turbo kancası: turbo açıkken sunucu süreçleri P-çekirdeklerine sabitlenir,
// öncelikleri yükseltilir ve kaynak tavanları kaldırılır — hem ÇALIŞAN
// süreçlerde hem de sonradan başlayan ya da çöküp yeniden başlatılanlarda.
//
// ── Neden supervisor'da ─────────────────────────────────────────────────────
// Eskiden turbo yalnızca handleServerStart içinde, BAŞLATMA anındaki kopyaya
// uygulanıyordu: açık olan sunucular hiçbir şey hissetmiyordu ("turbo hiçbir
// işe yaramıyor"), çöküp yeniden başlatılan bir sunucu ise eski (turbosuz)
// tanımla kalkıyordu. Supervisor her başlatmayı ve her yeniden başlatmayı
// gördüğü için kanca burada; Add, tanıma supervisor'un anlık turbo durumunu
// soran bir işlev bağlar.

// applyCgroup/removeCgroup: sınamalar bunları değiştirir, böylece ana
// makinenin gerçek /sys/fs/cgroup ağacına hiç dokunulmaz.
var (
	applyCgroup  = ApplyCgroup
	removeCgroup = RemoveCgroup
)

// TurboNice turbo açıkken sunucuların nice değeridir (FullPerf ile aynı).
const TurboNice = -10

// TurboApplied, SetTurbo/RepinTurbo'nun kaç sürece ve iş parçacığına
// dokunduğunu söyler (arayüzde kanıt olarak gösterilir).
type TurboApplied struct {
	Processes int
	Threads   int
}

type turboState struct {
	on   bool
	cpus []int
}

func (s *Supervisor) turboNow() (bool, []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turbo.on, append([]int(nil), s.turbo.cpus...)
}

// turboSpec, tanımın turbo altındaki hâlini döner.
//
// Süreç TÜM çekirdeklerde başlar (CPUAffinity boş): JVM kullanılabilir
// çekirdek sayısını başlarken okuyup GC ve parça üretim havuzlarını ona
// göre kurar; P-çekirdeklerine hapsedilen JVM daha az işçiyle açılıyordu
// (bkz. pinTurboThreads). P-çekirdeğine yalnızca oyun döngüsü, doğduktan
// sonra sabitlenir.
func turboSpec(spec Spec, cpus []int) Spec {
	_ = cpus
	spec.CPUAffinity = nil
	if spec.Nice > TurboNice {
		spec.Nice = TurboNice
	}
	io := spec.Limits.IOWeight
	if io < 1000 {
		io = 1000
	}
	spec.Limits = Limits{IOWeight: io}
	return spec
}

// startSpec, Process.Start'ın uygulayacağı tanımı hesaplar.
func (p *Process) startSpec() Spec {
	if p.spec.turbo == nil || p.spec.ID == "" {
		return p.spec
	}
	if on, cpus := p.spec.turbo(); on {
		return turboSpec(p.spec, cpus)
	}
	return p.spec
}

// lateMainPin: oyun döngüsü iş parçacığı JVM açıldıktan saniyeler sonra
// doğar; başlatmadan kısa süre sonra birkaç kez dener (daemon'un 15 sn'lik
// dönemsel sabitlemesini beklemesin).
func (p *Process) lateMainPin(pid int, cpus []int) {
	for _, d := range []time.Duration{3 * time.Second, 10 * time.Second, 30 * time.Second} {
		time.Sleep(d)
		if p.PID() != pid {
			return // süreç bitti ya da yeniden başladı
		}
		if on, _ := p.spec.turbo(); !on {
			return
		}
		if _, found := pinTurboThreads(pid, cpus); found {
			return
		}
	}
}

// servers çalışan, turboya tabi (ID'li) süreçleri döner.
func (s *Supervisor) servers() []*Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Process
	for _, e := range s.entries {
		if e.proc.spec.ID != "" {
			out = append(out, e.proc)
		}
	}
	return out
}

// SetTurbo turboyu açar ya da kapatır ve ÇALIŞAN sunuculara hemen uygular.
//
// Açarken: tüm iş parçacıkları cpus'a (boşsa tüm çekirdeklere) sabitlenir,
// nice TurboNice olur, cgroup tavanları kaldırılır. Kapatırken: her süreç
// kendi başlatma tanımına (CPUAffinity yoksa tüm çekirdekler, Nice, Limits)
// döner.
func (s *Supervisor) SetTurbo(on bool, cpus []int) TurboApplied {
	s.mu.Lock()
	s.turbo = turboState{on: on, cpus: append([]int(nil), cpus...)}
	s.mu.Unlock()
	return s.applyTurbo(true)
}

// RepinTurbo turbo açıkken periyodik çağrılır: sabitlemeden sonra doğan ve
// (yarış nedeniyle) eski maskeyi almış iş parçacıklarını da yakalar. Yalnızca
// sabitleme yapar; nice ve cgroup her turda yeniden yazılmaz.
func (s *Supervisor) RepinTurbo() TurboApplied {
	if on, _ := s.turboNow(); !on {
		return TurboApplied{}
	}
	return s.applyTurbo(false)
}

func (s *Supervisor) applyTurbo(full bool) TurboApplied {
	on, cpus := s.turboNow()
	var res TurboApplied
	for _, p := range s.servers() {
		pid := p.PID()
		if pid <= 0 {
			continue
		}
		spec := p.spec
		var n int
		if on {
			spec = turboSpec(p.spec, cpus)
			n, _ = pinTurboThreads(pid, cpus)
		} else {
			// Kapalıyken CPUAffinity boşsa pinAllThreads tüm çekirdeklere
			// açar: turbo kapanınca oyun döngüsü P-çekirdeklerinde kalmasın.
			n = pinAllThreads(pid, spec.CPUAffinity)
		}
		if n == 0 {
			continue
		}
		res.Processes++
		res.Threads += n
		if full {
			niceAllThreads(pid, spec.Nice)
			if spec.ID != "" {
				_ = applyCgroup(spec.ID, pid, spec.Limits)
			}
		}
	}
	return res
}
