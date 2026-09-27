package daemon

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"

	_ "time/tzdata"
)

// ════════════════════════════════════════════════════════════════════════════
// OTOMATİK YEDEK ZAMANLAYICISI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "yedek alma saat/gün aralığını da ayarlayalım". Zamanlama kararı
// saf bir işlevde (decideAutoBackup); saat testte dışarıdan verilir.

var testIST = func() *time.Location {
	l, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		panic(err)
	}
	return l
}()

func istAt(day, h, m int) time.Time { return time.Date(2026, 9, day, h, m, 0, 0, testIST) }

func mustSched(t *testing.T, s string) model.BackupSchedule {
	t.Helper()
	sc, err := model.ParseBackupSchedule(s)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// Saat aralığı: son yedekten tam 6 saat sonra, bir dakika önce değil.
func TestZamanlayiciSaatAraligi(t *testing.T) {
	sc := mustSched(t, "6h")
	disk := []time.Time{istAt(27, 10, 0), istAt(27, 4, 0)}
	if v := decideAutoBackup(sc, disk, time.Time{}, false, istAt(27, 15, 59), testIST); v.due {
		t.Fatalf("aralık dolmadan yedek: %+v", v)
	}
	v := decideAutoBackup(sc, disk, time.Time{}, false, istAt(27, 16, 0), testIST)
	if !v.due || !v.next.Equal(istAt(27, 16, 0)) || v.reason != "" {
		t.Fatalf("16:00'da zamanında yedek bekleniyordu: %+v", v)
	}
}

// Hiç yedek yoksa ilk yedek HEMEN (ölçülmüş davranış: bir aralık beklemek sık
// yeniden başlayan cihazda hiç yedek almamak demekti).
func TestZamanlayiciIlkYedekHemen(t *testing.T) {
	v := decideAutoBackup(mustSched(t, "1d@04:00"), nil, time.Time{}, false, istAt(27, 13, 0), testIST)
	if !v.due || v.reason != "henüz yedek yok" {
		t.Fatalf("ilk yedek hemen alınmadı: %+v", v)
	}
}

// Her gün 04:00: cihaz 04:00'da kapalıydı, 09:00'da açıldı → hemen alınır
// (neden: kaçırıldı), sonra plan ertesi günün 04:00'üne döner.
func TestZamanlayiciKacirilanYedekHemenSonraPlan(t *testing.T) {
	sc := mustSched(t, "1d@04:00")
	disk := []time.Time{istAt(20, 4, 2)}
	v := decideAutoBackup(sc, disk, time.Time{}, false, istAt(21, 9, 0), testIST)
	if !v.due || !strings.Contains(v.reason, "kaçırıldı (21.09 04:00)") {
		t.Fatalf("kaçırılan yedek hemen alınmadı ya da nedeni yok: %+v", v)
	}
	// Telafi yedeği 09:00'da başladı ve 09:03'te diske yazıldı.
	disk = append(disk, istAt(21, 9, 3))
	pending := istAt(21, 9, 0)
	if v := decideAutoBackup(sc, disk, pending, true, istAt(22, 3, 59), testIST); v.due {
		t.Fatalf("telafiden sonra 04:00'ü beklemeden yedek: %+v", v)
	}
	v = decideAutoBackup(sc, disk, pending, true, istAt(22, 4, 0), testIST)
	if !v.due || v.reason != "" || !v.next.Equal(istAt(22, 4, 0)) {
		t.Fatalf("plana dönülmedi: %+v", v)
	}
}

// Bu süreçte başlatılan yedek henüz diske yazılmadıysa (büyük dünya,
// dakikalar sürer) bir sonraki dakika İKİNCİ yedek başlamamalı.
func TestZamanlayiciSurenYedegiYinelemez(t *testing.T) {
	for _, spec := range []string{"6h", "1d@04:00"} {
		sc := mustSched(t, spec)
		disk := []time.Time{istAt(26, 4, 1)}
		start := istAt(27, 4, 0)
		if v := decideAutoBackup(sc, disk, time.Time{}, false, start, testIST); !v.due {
			t.Fatalf("%s: 04:00'da yedek bekleniyordu: %+v", spec, v)
		}
		if v := decideAutoBackup(sc, disk, start, true, start.Add(time.Minute), testIST); v.due {
			t.Fatalf("%s: süren yedeğin üstüne ikinci yedek: %+v", spec, v)
		}
	}
}

// Saat GERİ alınırsa (NTP ileri kurulu saati düzeltti):
//   - az geri (dakikalar): diskteki yedek dayanak kalır → YİNELEME YOK;
//   - çok geri (aralıktan fazla): ileri tarihli yedekler yok sayılır ve yeni
//     bir dayanak yedeği alınır → zamanlama o tarihe kadar DONMAZ.
func TestZamanlayiciSaatGeriAlininca(t *testing.T) {
	sc := mustSched(t, "6h")
	disk := []time.Time{istAt(27, 10, 0)}
	v := decideAutoBackup(sc, disk, time.Time{}, false, istAt(27, 9, 58), testIST)
	if v.due {
		t.Fatalf("saat 2 dakika geri gitti diye yedek yinelendi: %+v", v)
	}
	if !v.next.Equal(istAt(27, 16, 0)) {
		t.Fatalf("sonraki %v", v.next)
	}

	future := []time.Time{istAt(27, 10, 0).AddDate(1, 0, 0)}
	v = decideAutoBackup(sc, future, time.Time{}, false, istAt(27, 10, 0), testIST)
	if !v.due || v.ignoredFuture != 1 || !strings.Contains(v.reason, "geri alınmış") {
		t.Fatalf("bir yıl ileri tarihli yedek zamanlamayı dondurdu: %+v", v)
	}
	// Dayanak yedeği alındıktan sonra plan normale döner (ileri tarihli
	// yedek hâlâ diskte).
	future = append(future, istAt(27, 10, 1))
	if v := decideAutoBackup(sc, future, time.Time{}, false, istAt(27, 10, 30), testIST); v.due {
		t.Fatalf("dayanak yedeğinden sonra yine yedek: %+v", v)
	}

	// Her gün 04:00, yedekten yarım saat sonra saat BİR SAAT geri gitti:
	// duvar saati 04:00'ı yeniden gösterse de aynı gün ikinci yedek yok.
	day := mustSched(t, "1d@04:00")
	disk = []time.Time{istAt(27, 4, 1)}
	if v := decideAutoBackup(day, disk, time.Time{}, false, istAt(27, 4, 0), testIST); v.due {
		t.Fatalf("saat geri alınınca aynı gün ikinci yedek: %+v", v)
	}
}

// Bir hafta, dakika dakika: her gün 04:00, yedek 3 dakikada bitiyor, cihaz
// bir gece 00:00–09:00 kapalı, bir kez de saat 1 saat geri alınıyor.
// Beklenen: her gün TAM bir yedek, hiçbiri yinelenmiyor.
func TestZamanlayiciHaftalikBenzetim(t *testing.T) {
	sc := mustSched(t, "1d@04:00")
	var disk []time.Time
	var pending time.Time
	hasPending := false
	finishAt := time.Time{}
	perDay := map[string][]string{}
	skew := time.Duration(0) // duvar saati sapması
	for real := istAt(21, 0, 0); real.Before(istAt(28, 0, 0)); real = real.Add(time.Minute) {
		// 23 Eylül 00:00–09:00 cihaz kapalı: zamanlayıcı dönmez.
		if !real.Before(istAt(23, 0, 0)) && real.Before(istAt(23, 9, 0)) {
			continue
		}
		// 25 Eylül 04:30'da saat bir saat geri alındı.
		if real.Equal(istAt(25, 4, 30)) {
			skew = -time.Hour
		}
		now := real.Add(skew)
		if hasPending && !finishAt.IsZero() && !now.Before(finishAt) {
			disk = append(disk, finishAt)
			finishAt = time.Time{}
		}
		v := decideAutoBackup(sc, disk, pending, hasPending, now, testIST)
		if !v.due {
			continue
		}
		pending, hasPending = now, true
		finishAt = now.Add(3 * time.Minute)
		perDay[now.Format("2006-01-02")] = append(perDay[now.Format("2006-01-02")], now.Format("15:04")+" "+v.reason)
	}
	want := map[string]string{
		"2026-09-21": "00:00 henüz yedek yok",
		"2026-09-22": "04:00 ",
		"2026-09-23": "09:00 planlanan yedek kaçırıldı (23.09 04:00)",
		"2026-09-24": "04:00 ",
		"2026-09-25": "04:00 ",
		"2026-09-26": "04:00 ",
		"2026-09-27": "04:00 ",
	}
	for day, w := range want {
		got := perDay[day]
		if len(got) != 1 || got[0] != w {
			t.Errorf("%s: yedekler %q, [%q] bekleniyordu", day, got, w)
		}
	}
	if len(perDay) != len(want) {
		t.Errorf("beklenmeyen günler: %v", perDay)
	}
}

// ── Daemon turu (gerçek depo ve gerçek yedek arşivleri) ─────────────────────

func newBackupTestDaemon(t *testing.T, pol model.BackupPolicy) (*Daemon, *model.Server) {
	t.Helper()
	d := newRemoteTestDaemon(t)
	srv := &model.Server{ID: "srv_yedek", Name: "Yedek", Software: model.SoftwarePaper,
		MCVersion: "1.21.1", Port: 25565, Backup: pol}
	if err := d.store.SaveServer(srv); err != nil {
		t.Fatal(err)
	}
	data := d.store.Paths.ServerData(srv.ID)
	if err := os.MkdirAll(filepath.Join(data, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "world", "level.dat"), []byte("dunya"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d, srv
}

// writeFakeBackup writes a minimal backup archive (yalnızca manifest).
func writeFakeBackup(t *testing.T, d *Daemon, serverID, id string, created time.Time) {
	t.Helper()
	dir := d.store.Paths.ServerBackups(serverID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, id+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("backup.json")
	_ = json.NewEncoder(w).Encode(model.Backup{ID: id, ServerID: serverID, Name: id, CreatedAt: created, Type: "manual"})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// recordRuns replaces the real backup with a counter.
func recordRuns(d *Daemon) *[]model.Task {
	var mu sync.Mutex
	var runs []model.Task
	d.backupSt.mu.Lock()
	d.backupSt.run = func(t model.Task) {
		mu.Lock()
		runs = append(runs, t)
		mu.Unlock()
	}
	d.backupSt.mu.Unlock()
	return &runs
}

// Varsayılan kurulumda (cluster KAPALI) otomatik yedek GERÇEKTEN alınmalı.
// Eskiden görev kuyruğa atılıyor ve kuyruğu tüketen döngü yalnızca cluster
// açıkken çalıştığı için hiçbir yedek alınmıyordu.
func TestOtomatikYedekClusterKapaliykenAlinir(t *testing.T) {
	d, srv := newBackupTestDaemon(t, model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5})
	if d.cluster.Running() {
		t.Fatal("test cluster kapalı varsayıyor")
	}
	d.autoBackupTick(time.Now())
	deadline := time.Now().Add(10 * time.Second)
	for {
		list, err := d.backup.List(srv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 {
			if list[0].Type != "auto" {
				t.Errorf("otomatik yedeğin türü %q", list[0].Type)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cluster kapalıyken otomatik yedek alınmadı (%d yedek)", len(list))
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Yedek sürerken/bittikten hemen sonra ikinci tur yeni yedek başlatmaz.
	runs := recordRuns(d)
	d.autoBackupTick(time.Now().Add(time.Minute))
	if len(*runs) != 0 {
		t.Fatalf("aynı aralıkta ikinci otomatik yedek: %d", len(*runs))
	}
	// İş parçacığı bitmeden test klasörü silinmesin.
	for d.autoBackupBusy(srv.ID) {
		time.Sleep(20 * time.Millisecond)
	}
}

// Budama yedeği başlattığımız turda değil SONRAKİ turda (ölçülmüş davranış)
// ve yalnızca otomatik yedek AÇIKKEN.
func TestOtomatikYedekBudama(t *testing.T) {
	d, srv := newBackupTestDaemon(t, model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 2})
	runs := recordRuns(d)
	now := time.Now()
	for i, id := range []string{"bak_0001", "bak_0002", "bak_0003"} {
		writeFakeBackup(t, d, srv.ID, id, now.Add(time.Duration(i-3)*time.Hour))
	}
	d.autoBackupTick(now)
	list, _ := d.backup.List(srv.ID)
	if len(list) != 2 || list[0].ID != "bak_0003" || list[1].ID != "bak_0002" {
		t.Fatalf("en eski yedek budanmadı: %+v", list)
	}
	if len(*runs) != 0 {
		t.Fatalf("son yedek 1 saat önceyken 6 saatlik planda yedek: %d", len(*runs))
	}

	// Plan KAPALI: elle alınan yedekler Keep'e rağmen silinmez.
	srv.Backup.Auto = false
	if err := d.store.SaveServer(srv); err != nil {
		t.Fatal(err)
	}
	writeFakeBackup(t, d, srv.ID, "bak_0004", now)
	d.autoBackupTick(now.Add(time.Minute))
	if list, _ := d.backup.List(srv.ID); len(list) != 3 {
		t.Fatalf("kapalı plan elle alınmış yedekleri budadı: %d yedek kaldı", len(list))
	}
}

// Saat dilimi yapılandırmadan okunur ve bozuk saatte (2010) zamanlayıcı
// yedek ALMAZ (yanlış tarihli yedek budamada ilk silinirdi).
func TestOtomatikYedekSaatDilimiVeBozukSaat(t *testing.T) {
	d, srv := newBackupTestDaemon(t, model.BackupPolicy{Auto: true, Schedule: "1d@04:00", Keep: 5})
	runs := recordRuns(d)
	d.mu.Lock()
	d.cfg.Timezone = "America/New_York"
	d.mu.Unlock()
	if loc := d.backupLocation(); loc.String() != "America/New_York" {
		t.Fatalf("saat dilimi %v", loc)
	}
	d.autoBackupTick(time.Date(2010, 1, 1, 0, 5, 0, 0, time.UTC))
	if len(*runs) != 0 {
		t.Fatalf("2010 tarihli saatle yedek alındı")
	}
	res := d.backupPolicyResult(srv, time.Date(2010, 1, 1, 0, 5, 0, 0, time.UTC))
	if !strings.Contains(res.Problem, "saati yanlış") || res.Next != nil {
		t.Fatalf("bozuk saat bildirilmedi: %+v", res)
	}

	ny, _ := time.LoadLocation("America/New_York")
	writeFakeBackup(t, d, srv.ID, "bak_0001", time.Date(2026, 9, 26, 4, 1, 0, 0, ny))
	res = d.backupPolicyResult(srv, time.Date(2026, 9, 26, 15, 0, 0, 0, ny))
	if res.Next == nil || res.Next.Format(time.RFC3339) != "2026-09-27T04:00:00-04:00" {
		t.Fatalf("sonraki yedek New York saatiyle 04:00 olmalı: %+v", res.Next)
	}
	if res.Timezone != "America/New_York" {
		t.Errorf("dilim %q", res.Timezone)
	}
}

// ── IPC doğrulama ───────────────────────────────────────────────────────────

func setPolicy(t *testing.T, d *Daemon, p ipc.BackupSetPolicyParams) (ipc.BackupPolicyResult, error) {
	t.Helper()
	raw, _ := json.Marshal(p)
	res, err := d.handleBackupSetPolicy(context.Background(), raw)
	if err != nil {
		return ipc.BackupPolicyResult{}, err
	}
	return res.(ipc.BackupPolicyResult), nil
}

func intp(n int) *int { return &n }

func TestYedekPlaniIPCDogrulama(t *testing.T) {
	d, srv := newBackupTestDaemon(t, model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5})
	for _, c := range []struct {
		p    ipc.BackupSetPolicyParams
		want string
	}{
		{ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true, Schedule: "5m"}, "en az 15 dakika"},
		{ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true, Schedule: "1d@25:00"}, "00:00–23:59"},
		{ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true, Schedule: "6h@04:00"}, "yalnızca gün aralığıyla"},
		{ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: false, Schedule: "her gün"}, "anlaşılamadı"},
		{ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true, Keep: intp(-1)}, "kopya"},
		{ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true, Keep: intp(101)}, "kopya"},
	} {
		_, err := setPolicy(t, d, c.p)
		var ie *ipc.Error
		if !errors.As(err, &ie) || ie.Code != ipc.CodeInvalidParams || !strings.Contains(ie.Message, c.want) {
			t.Errorf("%+v: hata %v, %q içeren geçersiz-parametre hatası bekleniyordu", c.p, err, c.want)
		}
	}
	// Reddedilen istek kaydı DEĞİŞTİRMEMELİ.
	got, _ := d.store.GetServer(srv.ID)
	if got.Backup != (model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}) {
		t.Fatalf("geçersiz istek planı değiştirdi: %+v", got.Backup)
	}

	if _, err := setPolicy(t, d, ipc.BackupSetPolicyParams{ServerID: "yok", Auto: true}); err == nil {
		t.Error("olmayan sunucu kabul edildi")
	} else if ie := (*ipc.Error)(nil); !errors.As(err, &ie) || ie.Code != ipc.CodeNotFound {
		t.Errorf("olmayan sunucu: %v", err)
	}
	if _, err := setPolicy(t, d, ipc.BackupSetPolicyParams{ServerID: "../x", Auto: true}); err == nil {
		t.Error("yol kaçışlı kimlik kabul edildi")
	}
}

func TestYedekPlaniIPCKaydeder(t *testing.T) {
	d, srv := newBackupTestDaemon(t, model.BackupPolicy{})
	// Hiç ayarlanmamış planı yalnızca "aç" ile açmak: sihirbaz varsayılanı.
	res, err := setPolicy(t, d, ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Policy != (model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}) || res.Summary != "Her 6 saatte bir · son 5 kopya" {
		t.Fatalf("varsayılan plan: %+v", res)
	}
	if !res.Due || res.Next == nil || res.Reason != "henüz yedek yok" {
		t.Errorf("hiç yedek yokken 'sonraki: şimdi' bekleniyordu: %+v", res)
	}

	res, err = setPolicy(t, d, ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true, Schedule: " 1D@4:00 ", Keep: intp(0)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := d.store.GetServer(srv.ID)
	if got.Backup != (model.BackupPolicy{Auto: true, Schedule: "1d@04:00", Keep: 0}) {
		t.Fatalf("kanonik biçimde saklanmadı: %+v", got.Backup)
	}
	if res.Summary != "Her gün 04:00 · sınırsız kopya" {
		t.Errorf("özet %q", res.Summary)
	}

	// Kapatmak planı SİLMEZ; yeniden açan eski seçimini bulur.
	res, err = setPolicy(t, d, ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: false})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "Kapalı" || res.Next != nil || res.Policy.Schedule != "1d@04:00" {
		t.Fatalf("kapatma: %+v", res)
	}
	res, _ = setPolicy(t, d, ipc.BackupSetPolicyParams{ServerID: srv.ID, Auto: true})
	if res.Policy != (model.BackupPolicy{Auto: true, Schedule: "1d@04:00", Keep: 0}) {
		t.Fatalf("yeniden açınca eski seçim kayboldu: %+v", res.Policy)
	}

	// backup.policy aynı sonucu okur.
	raw, _ := json.Marshal(ipc.BackupPolicyParams{ServerID: srv.ID})
	out, err := d.handleBackupPolicy(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if r := out.(ipc.BackupPolicyResult); r.Summary != "Her gün 04:00 · sınırsız kopya" {
		t.Errorf("backup.policy özeti %q", r.Summary)
	}
}
