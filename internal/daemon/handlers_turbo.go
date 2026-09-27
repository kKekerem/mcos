package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/store"
	"mcos/internal/supervisor"
	"mcos/internal/turbo"
)

// ── Turbo: donanım + sunucu süreçleri ───────────────────────────────────────
//
// Eskiden bu anahtar yalnızca yapılandırmaya "turbo: true" yazıyordu; etkisi
// bir SONRAKİ sunucu başlatmasında nice -10'dan ibaretti. Gerçek PC'de
// kullanıcı turbo açıkken işlemcinin 400 MHz'de, fanların sessizde kaldığını
// gördü ("turbo hiçbir işe yaramıyor"). Artık anahtar ANINDA:
//   - internal/turbo ile frekans/EPP/turbo boost/platform profili/fanları,
//   - supervisor ile çalışan sunucuların TÜM iş parçacıklarını P-çekirdeklerine
//     sabitlemeyi, önceliği ve cgroup tavanlarını
// uygular ve sonucu madde madde döner.

// turboState turbo denetleyicisini ve son sabitleme sonucunu tutar.
type turboState struct {
	once   sync.Once
	ctl    *turbo.Controller
	mu     sync.Mutex
	pinned supervisor.TurboApplied
}

// turboKokEnv: yalnızca uçtan uca sınama için. Ayarlıysa turbo sysfs'i bu
// sahte kökte arar; geliştirme makinesinin gerçek /sys'ine hiç yazılmaz.
const turboKokEnv = "MCOS_TURBO_KOK"

// turboDiagWindow: tanı ölçüm penceresi (RAPL gücü, APERF/MPERF). 250 ms,
// turbostat'ın öntanımlı 5 sn'sinden kısa ama tek bir zamanlayıcı
// tıkının (4 ms) çok üstünde: meşgul çekirdeğin frekansı kararlı ölçülür.
const turboDiagWindow = 250 * time.Millisecond

// turboLogEvery: turbo açıkken günlüğe kaç turda bir anlık görüntü
// yazılacağı (15 sn × 20 = 5 dk). Isınma ve firmware'in güç sınırını geri
// çekmesi dakikalar içinde olur; her tur yazmak /data'yı şişirirdi.
const turboLogEvery = 20

// turboLogMax: /data/log/turbo.log bu boyu aşınca turbo.log.1'e döner.
const turboLogMax = 512 << 10

func turboRoot() string {
	if r := os.Getenv(turboKokEnv); r != "" {
		return r
	}
	return "/"
}

// turboLog tanı raporunu /data/log/turbo.log'a ekler.
//
// Neden dosya: kullanıcı gerçek PC'de "4,4 yerine 2,4 GHz" gördü ve
// elimizde hiçbir kayıt yoktu. Ekran görüntüsüne ek olarak dosya, açılıştan
// (turbo kapalıyken) itibaren durumu ve turbo açıldıktan sonraki değişimi
// (ısınma, firmware'in PL1'i geri çekmesi) zaman damgasıyla tutar.
func (d *Daemon) turboLog(reason string, st model.TurboStatus) {
	p := filepath.Join(turboRoot(), "data/log/turbo.log")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	if fi, err := os.Stat(p); err == nil && fi.Size() > turboLogMax {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(turbo.Report(st, reason, time.Now()) + "\n")
}

func (d *Daemon) turboCtl() *turbo.Controller {
	d.turboSt.once.Do(func() {
		root := turboRoot()
		// Özgün değerler /run altında: tmpfs, yeniden başlatmada silinir.
		// Bu doğru davranış — yeniden başlatmada BIOS fanları ve frekansı
		// zaten sıfırlar; kalıcı bir kayıt, turbonun kendi değerlerini
		// "özgün" diye geri yazmaya yol açardı.
		d.turboSt.ctl = turbo.New(root, filepath.Join(root, "run/mcos/turbo-ozgun.json"))
	})
	return d.turboSt.ctl
}

func (d *Daemon) handleSystemTurbo(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.TurboParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	cfg := d.Config()
	cp := *cfg
	cp.Turbo = p.Enabled
	if err := store.SaveConfig(d.cfgPath, &cp); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cfg = &cp
	d.mu.Unlock()
	st := d.applyTurbo(cp.Turbo)
	d.log.Infof("system: turbo = %v (%s)", cp.Turbo, st.Summary)
	return ipc.TurboResult{Enabled: cp.Turbo, Detail: &st}, nil
}

// applyTurbo donanımı ve çalışan sunucuları turboya alır ya da geri döndürür.
func (d *Daemon) applyTurbo(on bool) model.TurboStatus {
	ctl := d.turboCtl()
	var st model.TurboStatus
	var res supervisor.TurboApplied
	if on {
		ctl.Enable()
		// Hibrit değilse PCores tüm çevrimiçi çekirdeklerdir; sabitleme o
		// zaman "her yerde çalış" demektir (kullanıcının eski bir çekirdek
		// sabitlemesi turbo altında kalkar).
		topo := ctl.Topology()
		res = d.sup.SetTurbo(true, topo.PCores)
		// Panel (60 kare/sn çizim) sunucunun oyun döngüsüyle aynı P-çekirdeği
		// için yarışmasın: hibrit işlemcide E-çekirdeklerine çekilir.
		if len(topo.ECores) > 0 {
			pinPanel(topo.ECores)
		}
	} else {
		ctl.Disable()
		res = d.sup.SetTurbo(false, nil)
		pinPanel(nil)
	}
	// Yazımlardan SONRA ölç: dönen durum, donanımdan geri okunan tanıyı
	// (PL1/PL2, ölçülen MHz, fanlar) içersin — yalnızca "yazdık" değil.
	st = ctl.Diagnose(turboDiagWindow)
	d.turboSt.mu.Lock()
	d.turboSt.pinned = res
	d.turboSt.mu.Unlock()
	st.PinnedThreads = res.Threads
	for _, it := range st.Items {
		d.log.Infof("turbo: %s: %s — %s", it.Name, it.State, it.Detail)
	}
	if res.Processes > 0 {
		d.log.Infof("turbo: %d sunucu, %d iş parçacığı yeniden yerleştirildi", res.Processes, res.Threads)
	}
	if st.Limiter != "" {
		d.log.Infof("turbo: sınırlayan: %s", st.Limiter)
	}
	reason := "turbo kapatıldı"
	if on {
		reason = "turbo açıldı"
	}
	d.turboLog(reason, st)
	return st
}

// turboStartup açılışta çağrılır: yapılandırmada turbo açıksa uygular;
// kapalıysa önceki (çökmüş) oturumdan kalan değişiklikleri geri alır.
//
// Sunucular otomatik başlatılmadan ÖNCE çağrılmalı: böylece autostart
// sunucuları başlatma kancasıyla doğrudan P-çekirdeklerinde doğar.
func (d *Daemon) turboStartup(ctx context.Context) {
	if d.Config().Turbo {
		d.applyTurbo(true)
	} else if n, errs := d.turboCtl().RestoreLeftover(); n > 0 || len(errs) > 0 {
		// Daemon turbo açıkken çöktüyse fanlar elle tam güçte, yönetici
		// "performance" kalmıştır. Kullanıcı turboyu kapalı biliyor.
		d.log.Warnf("turbo: önceki oturumdan kalan %d ayar geri alındı (hatalar: %v)", n, errs)
	}
	go d.turboLoop(ctx)
}

// turboRefresh: donanımın geri aldığı fanı yeniden tam güce çekme ve sonradan
// doğan iş parçacıklarını sabitleme aralığı. JVM'in iş parçacığı listesini
// okumak ve birkaç sysfs dosyası yazmak mikrosaniyeler sürer.
const turboRefresh = 15 * time.Second

func (d *Daemon) turboLoop(ctx context.Context) {
	// Açılış tanısı turbo KAPALIYKEN de alınır: "turbo açmadan önce işlemci
	// neredeydi, PL1 kaçtı" sorusunun cevabı günlükte dursun.
	if st := d.turboCtl().Diagnose(turboDiagWindow); !st.Active {
		d.turboLog("açılış (turbo kapalı)", st)
	}
	tk := time.NewTicker(turboRefresh)
	defer tk.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			ctl := d.turboCtl()
			// Tanı her turda yenilenir (panel "Turbo tanısı" canlı kalsın);
			// birkaç yüz sysfs okuması ve 250 ms bekleme bu goroutine'de.
			if !ctl.Active() {
				ctl.Diagnose(turboDiagWindow)
				continue
			}
			ctl.Refresh()
			res := d.sup.RepinTurbo()
			d.turboSt.mu.Lock()
			d.turboSt.pinned = res
			d.turboSt.mu.Unlock()
			st := ctl.Diagnose(turboDiagWindow)
			if n++; n%turboLogEvery == 0 {
				d.turboLog("turbo açık — dönemsel ölçüm", st)
			}
		}
	}
}

// turboShutdown daemon kapanırken donanımı özgün hâline döndürür (fanlar
// otomatiğe). Yapılandırma DEĞİŞMEZ: bir sonraki açılışta turbo yeniden
// uygulanır.
func (d *Daemon) turboShutdown() {
	ctl := d.turboCtl()
	if !ctl.Active() {
		return
	}
	st := ctl.Disable()
	for _, it := range st.Items {
		d.log.Infof("turbo: kapanış: %s — %s", it.Name, it.Detail)
	}
}

// turboStatus durum ekranı için canlı turbo ayrıntısı.
func (d *Daemon) turboStatus() *model.TurboStatus {
	st := d.turboCtl().Status()
	d.turboSt.mu.Lock()
	st.PinnedThreads = d.turboSt.pinned.Threads
	d.turboSt.mu.Unlock()
	return &st
}

// coreMHz çekirdek başına anlık frekans (Performans/Donanım ekranı).
func (d *Daemon) coreMHz() []int { return turbo.CoreMHz(turboRoot()) }

// pinPanel MCOS panellerinin tüm iş parçacıklarını cpus'a (boşsa her yere)
// taşır. mcosd'nin KENDİSİ taşınmaz: sunucular onun çocuğudur ve JVM
// başlarken devraldığı maskeyle işçi havuzunu küçük kurardı.
func pinPanel(cpus []int) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		switch strings.TrimSpace(string(b)) {
		case "mcos-panel-fb", "mcos-panel":
			supervisor.PinProcess(pid, cpus)
		}
	}
}
