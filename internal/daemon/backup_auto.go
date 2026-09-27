package daemon

import (
	"context"
	"strings"
	"sync"
	"time"

	"mcos/internal/model"
)

// ── Otomatik yedek zamanlayıcısı ────────────────────────────────────────────
//
// daemon.go'dan buraya taşındı; plan artık saat ya da gün aralığı ve isteğe
// bağlı günün saati olabiliyor (bkz. model.ParseBackupSchedule). Ölçülmüş
// davranışlar AYNEN korunuyor:
//
//   - Zamanlama DİSKTEKİ en son yedeğe dayanır. Eskiden süreç içi harita tek
//     gerçek kaynaktı ve her daemon yeniden başlatmasında sıfırlanıyordu;
//     "seed" dalı bir aralık daha beklettiği için sık yeniden başlayan bir
//     cihazda otomatik yedek HİÇ alınmıyordu.
//   - Hiç yedek yoksa ilk yedek HEMEN alınır; ilk yedek için bir tam aralık
//     beklenmez.
//   - Budama yedeği başlattığımız turda DEĞİL, sonraki turda yapılır: görev
//     henüz yeni başlamışken budamak bir tur gecikmeli çalışıyordu.
//
// ── Düzeltilen gerçek hata: cluster kapalıyken yedek hiç alınmıyordu ────────
// Yedekler cluster görevi olarak kuyruğa atılıyordu; kuyruğu tüketen döngü
// (cluster.taskConsumerLoop) ise YALNIZCA cluster.Start()'ta başlıyor.
// Cluster varsayılan olarak KAPALI (model.DefaultConfig: eşleştirme portu
// kullanıcı istemeden açılmasın) — yani varsayılan kurulumda otomatik yedek
// görevleri sonsuza dek "queued" kalıyor, tek bir yedek bile alınmıyordu;
// cluster sonradan açılınca da birikmiş görevlerin hepsi birden koşacaktı.
// Artık cluster çalışmıyorsa yedek aynı yürütücüyle (taskExecutor) doğrudan
// alınıyor (TestOtomatikYedekClusterKapaliykenAlinir).

// autoBackupState is the scheduler's in-process memory.
type autoBackupState struct {
	mu sync.Mutex
	// pending: bu süreçte başlatılan yedeğin BAŞLAMA anı (monotonik saatli).
	// Yedek bitip diske yazılana kadar (büyük bir dünyada dakikalar) disk eski
	// yedeği gösterir; bu kayıt olmasa zamanlayıcı her dakika yeni bir yedek
	// başlatırdı. Saat aralıklı planda monotonik fark kullanıldığından duvar
	// saati sıçramaları (NTP) süreç içinde yedeği ne erteler ne yineler.
	pending map[string]time.Time
	// inflight: cluster kapalıyken doğrudan yürüyen yedekler. Aralıktan uzun
	// süren bir yedeğin üstüne aynı sunucunun İKİNCİ yedeği başlamasın.
	inflight map[string]bool
	// warned: aynı sorun için günlüğü her dakika doldurmamak.
	warned map[string]string
	// Saat dilimi önbelleği (bkz. backupLocation).
	locName string
	loc     *time.Location
	// run, testlerde gerçek yedeğin yerine geçer (nil = gerçek yol).
	run func(model.Task)
}

const (
	// backupLateAfter: planlanan andan bu kadar sonra alınan yedek
	// "kaçırılmış" sayılır. Zamanlayıcı dakikada bir döner; bir dakikalık
	// gecikme normaldir, cihazın kapalı geçirdiği saatler değildir.
	backupLateAfter = 5 * time.Minute
	// backupSaneYear: bundan önceki bir saat bozuktur (pilsiz RTC; bkz.
	// timesync.minSaneYear).
	backupSaneYear = 2024
)

// autoBackupVerdict is the scheduler's decision for one server.
type autoBackupVerdict struct {
	due     bool
	next    time.Time
	last    time.Time
	hasLast bool
	// reason: yedek şimdi alınacaksa nedeni (günlük ve panel için Türkçe).
	reason string
	// ignoredFuture: şimdiden bir aralıktan fazla ileri tarihli, yok sayılan
	// yedek sayısı (saat geri alınmış).
	ignoredFuture int
}

// decideAutoBackup is the pure scheduling rule (saat dışarıdan verilir ki
// testler zamanı ileri geri oynatabilsin).
//
// disk: diskteki yedeklerin oluşturulma anları. pending: bu süreçte başlatılan
// son otomatik yedek.
//
// ── Saat geri alınırsa ──────────────────────────────────────────────────────
// Pilsiz ya da ileri kurulu bir saat, NTP düzeltince GERİ gider ve diskteki
// yedekler "gelecekte" kalır. Hepsine güvenmek zamanlamayı o tarihe kadar
// dondururdu (ör. saat bir yıl ilerideyken alınmış yedek, bir yıl boyunca
// yeni yedeği engellerdi). En fazla BİR aralık ileri tarihli yedek küçük bir
// düzeltmedir ve dayanak kalır (yedek o kadar gecikir, yinelenmez); daha
// ilerisi yok sayılır ve yeni, doğru tarihli bir dayanak yedeği alınır.
func decideAutoBackup(sc model.BackupSchedule, disk []time.Time, pending time.Time, hasPending bool,
	now time.Time, loc *time.Location) autoBackupVerdict {
	var v autoBackupVerdict
	limit := now.Add(sc.Every)
	for _, t := range disk {
		if t.After(limit) {
			v.ignoredFuture++
			continue
		}
		if !v.hasLast || t.After(v.last) {
			v.last, v.hasLast = t, true
		}
	}
	if hasPending && (!v.hasLast || pending.After(v.last)) {
		v.last, v.hasLast = pending, true
	}
	if !v.hasLast {
		// Hiç yedek yok: hemen bir tane al, ilk yedek için bir tam aralık
		// beklenmez.
		v.due, v.next = true, now
		v.reason = "henüz yedek yok"
		if v.ignoredFuture > 0 {
			v.reason = "sistem saati geri alınmış: diskteki yedekler ileri tarihli"
		}
		return v
	}
	v.next = sc.Next(v.last, loc)
	if !now.Before(v.next) {
		v.due = true
		// Cihaz planlanan anda kapalıydı (04:00'da kapalı, 09:00'da açıldı):
		// yedek HEMEN alınır, çünkü bir sonraki plana kadar beklemek bir
		// günü yedeksiz bırakırdı. Next bu geç yedeği dayanak alır ve plan
		// ertesi günün 04:00'üne döner (bkz. BackupSchedule.Next).
		if now.Sub(v.next) > backupLateAfter {
			v.reason = "planlanan yedek kaçırıldı (" + v.next.In(loc).Format("02.01 15:04") + ")"
		}
	}
	return v
}

// autoBackupFor computes the verdict of one server from its backup list.
//
// problem boş değilse plan uygulanamıyor (bozuk kayıt, güvenilmez saat).
func (d *Daemon) autoBackupFor(srv *model.Server, list []model.Backup, now time.Time,
	loc *time.Location) (v autoBackupVerdict, problem string) {
	sc, err := srv.Backup.Plan()
	if err != nil {
		return v, "otomatik yedek planı okunamadı: " + err.Error()
	}
	if now.Year() < backupSaneYear {
		// Saat 2010'u gösteriyorsa (pilsiz RTC, NTP henüz eşitlemedi) alınan
		// yedek yanlış tarihle damgalanır: zamanlamanın dayanağını bozar ve
		// yedek listesi ada (oluşturma anına) göre sıralandığından budamada
		// EN YENİ yedek "en eski" sanılıp ilk silinirdi.
		return v, "sistem saati yanlış (" + now.Format("2006-01-02") + ") — NTP eşitlemesi bekleniyor"
	}
	disk := make([]time.Time, 0, len(list))
	for _, b := range list {
		disk = append(disk, b.CreatedAt)
	}
	st := &d.backupSt
	st.mu.Lock()
	p, ok := st.pending[srv.ID]
	st.mu.Unlock()
	return decideAutoBackup(sc, disk, p, ok, now, loc), ""
}

// backupScheduler runs auto-backups on each server's plan and enforces the
// retention count. Kararı autoBackupTick verir.
func (d *Daemon) backupScheduler(ctx context.Context) {
	tk := time.NewTicker(1 * time.Minute)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tk.C:
			d.autoBackupTick(now)
		}
	}
}

// autoBackupTick is one scheduler round.
func (d *Daemon) autoBackupTick(now time.Time) {
	servers, err := d.store.ListServers()
	if err != nil {
		return
	}
	loc := d.backupLocation()
	for _, srv := range servers {
		// Kapalı plan HİÇBİR otomatik işlem yapmaz; budama da. Eskiden budama
		// Auto'ya bakmıyordu: planı kapatan kullanıcının elle aldığı yedekler
		// Keep sayısına inene kadar her dakika siliniyordu.
		if !srv.Backup.Auto {
			continue
		}
		list, lerr := d.backup.List(srv.ID) // en yeni ilk
		if lerr != nil {
			d.warnAutoBackup(srv.ID, "yedek listesi okunamadı: "+lerr.Error())
			continue
		}
		v, problem := d.autoBackupFor(srv, list, now, loc)
		if problem != "" {
			d.warnAutoBackup(srv.ID, problem)
			continue
		}
		d.warnAutoBackup(srv.ID, "")
		if v.due && !d.autoBackupBusy(srv.ID) {
			d.startAutoBackup(srv, now, v.reason)
		}
		// Budama BU TURUN listesiyle: az önce başlatılan yedek onda yok, bir
		// önceki turların yedekleri diskte hazır. Yeni yedek sonraki turda
		// sayılır (ölçülmüş davranış, bkz. dosya başı).
		if srv.Backup.Keep > 0 {
			for i := srv.Backup.Keep; i < len(list); i++ {
				_ = d.backup.Delete(srv.ID, list[i].ID)
			}
		}
	}
}

// startAutoBackup launches one automatic backup.
func (d *Daemon) startAutoBackup(srv *model.Server, now time.Time, reason string) {
	st := &d.backupSt
	st.mu.Lock()
	if st.pending == nil {
		st.pending = map[string]time.Time{}
	}
	st.pending[srv.ID] = now
	run := st.run
	st.mu.Unlock()

	if reason != "" {
		d.log.Infof("backup: %s için otomatik yedek alınıyor (%s)", srv.ID, reason)
	} else {
		d.log.Infof("backup: %s için otomatik yedek alınıyor", srv.ID)
	}
	t := model.Task{
		ID: generateTaskID(), Kind: model.TaskBackup, State: model.TaskQueued,
		ServerID: srv.ID, Params: map[string]string{"name": "", "type": "auto"}, CreatedAt: now,
	}
	if run != nil {
		run(t)
		return
	}
	if d.cluster != nil && d.cluster.Running() {
		// Cluster açıkken görev kuyruğu üzerinden (görevler listesinde
		// görünür); yedekler her zaman YEREL koşar, sunucu verisine muhtaç.
		d.cluster.SubmitTask(t)
		return
	}
	st.mu.Lock()
	if st.inflight == nil {
		st.inflight = map[string]bool{}
	}
	st.inflight[srv.ID] = true
	st.mu.Unlock()
	go func() {
		defer func() {
			st.mu.Lock()
			delete(st.inflight, srv.ID)
			st.mu.Unlock()
		}()
		if _, err := (taskExecutor{d}).Execute(t); err != nil {
			d.log.Warnf("backup: %s otomatik yedeği alınamadı: %v", srv.ID, err)
		}
	}()
}

// autoBackupBusy reports whether an automatic backup of the server is running
// or still waiting in the cluster queue.
func (d *Daemon) autoBackupBusy(id string) bool {
	st := &d.backupSt
	st.mu.Lock()
	busy := st.inflight[id]
	st.mu.Unlock()
	if busy || d.cluster == nil || !d.cluster.Running() {
		return busy
	}
	for _, t := range d.cluster.Tasks() {
		if t.Kind != model.TaskBackup || t.ServerID != id {
			continue
		}
		switch t.State {
		case model.TaskQueued, model.TaskRunning, model.TaskMigrating:
			return true
		}
	}
	return false
}

// warnAutoBackup logs a scheduling problem once per distinct message.
// Boş mesaj sorunun geçtiğini kaydeder.
func (d *Daemon) warnAutoBackup(id, msg string) {
	st := &d.backupSt
	st.mu.Lock()
	if st.warned == nil {
		st.warned = map[string]string{}
	}
	prev := st.warned[id]
	st.warned[id] = msg
	st.mu.Unlock()
	if msg != "" && msg != prev && d.log != nil {
		d.log.Warnf("backup: %s: %s", id, msg)
	}
}

// backupLocation is the time zone that "04:00" means.
//
// time.Local DEĞİL, yapılandırmadaki Timezone: Go /etc/localtime'ı (ya da
// TZ'yi) sürecin İLK saat kullanımında bir kez okur ve bir daha bakmaz
// (ölçüldü: TZ süreç içinde değiştirildikten sonra time.Now().Zone() eski
// dilimi vermeye devam etti). mcosd dilimi netcfg.ApplyTimezone ile açılışta
// ve panelden değiştirildiğinde uyguluyor, ama o anda kendi time.Local'ı
// çoktan sabitlenmiş oluyor; "her gün 04:00" başka bir dilimin 04:00'ünde
// alınırdı. Dilim yüklenemezse (imajda yok) time.Local'a düşülür.
func (d *Daemon) backupLocation() *time.Location {
	tz := ""
	if cfg := d.Config(); cfg != nil {
		tz = strings.TrimSpace(cfg.Timezone)
	}
	if tz == "" {
		return time.Local
	}
	st := &d.backupSt
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.locName == tz && st.loc != nil {
		return st.loc
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		if d.log != nil {
			d.log.Warnf("backup: saat dilimi %q yüklenemedi, sistem dilimi kullanılıyor: %v", tz, err)
		}
		loc = time.Local
	}
	st.locName, st.loc = tz, loc
	return loc
}
