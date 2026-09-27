package fbpanel

// ── Sunucu detayı › Yedekler › Otomatik yedek planı ─────────────────────────
//
// Kullanıcının isteği: "yedek alma saat/gün aralığını da ayarlayalım".
// Yedekler sekmesinin başında plan ("Her 6 saatte bir · son 5 kopya") ve bir
// sonraki yedeğin zamanı ("bugün 16:00") yazar; "Otomatik yedek planı…"
// satırı (ya da o tuşu) ardışık seçim pencerelerini açar:
//
//	Kapalı / Saat aralığıyla… / Gün aralığıyla…
//	  → saat: 1, 2, 3, 4, 6, 8, 12 saat
//	  → gün: her gün, 2 gün, 3 gün, her hafta → günün saati (ya da sabit değil)
//	→ saklanacak kopya: 3, 5, 10, 20, sınırsız
//
// "Sonraki" zamanı panel HESAPLAMAZ: daemon'un backup.policy yanıtından
// gelir, o da zamanlayıcının KENDİ kuralıyla hesaplanır. Panel aynı kuralı
// ikinci kez yazsaydı ekrandaki saat ile yedeğin gerçekten alındığı saat
// zamanla ayrışırdı (saat dilimi, kaçırılan yedek, yaz saati).
//
// Bu dosya ayrı, çünkü screen_server_detail*.go başka özelliklerle de
// düzenleniyor; oradaki değişiklikler birkaç satırlık kancadan ibaret.

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// detailBackupPolicy is the last backup.policy answer of the detail.
type detailBackupPolicy struct {
	res ipc.BackupPolicyResult
	// ok: yanıt geldi. Eski bir mcosd backup.policy'yi bilmez; o zaman
	// yalnızca sunucu kaydındaki plan yazılır, "Sonraki" satırı çıkmaz.
	ok bool
}

// fetchBackupPolicy reads the plan and the next due time into d.
//
// Yedek listesiyle aynı goroutine'de (detailLoadBackups) çağrılır: sekmenin
// yükleniyor göstergesi ikisini birden kapsar. Hata sekmeyi HATALI yapmaz:
// liste okunduysa kullanıcı yedeklerini görmeli, plan satırı eski daemon'da
// yalnızca özet olarak kalır.
func (a *App) fetchBackupPolicy(d *ServerDetail) {
	res, err := a.cl.BackupPolicy(d.id)
	d.mu.Lock()
	d.backupPol = detailBackupPolicy{res: res, ok: err == nil}
	d.mu.Unlock()
}

// backupPolicyRows are the info lines at the top of the Yedekler tab.
//
// Özet sunucu KAYDINDAN (s.Backup) yazılır: yoklama onu her saniye tazeler
// ve plan başka bir istemciden (telefon) değişse de satır hemen doğrulanır.
// "Sonraki" yalnızca daemon'un yanıtı bu planı anlatıyorsa gösterilir;
// başka bir plana ait saat, doğru görünen YANLIŞ bir bilgi olurdu.
func backupPolicyRows(s *model.Server, bp detailBackupPolicy, now time.Time) [][2]string {
	rows := [][2]string{{"Otomatik yedek", model.BackupPolicyLabel(s.Backup)}}
	if !s.Backup.Auto || !bp.ok || bp.res.Policy != s.Backup {
		return rows
	}
	if next := backupNextLabel(bp.res, now); next != "" {
		rows = append(rows, [2]string{"Sonraki yedek", next})
	}
	return rows
}

// backupNextLabel describes when the next automatic backup runs:
// "bugün 16:00", "yarın 04:00", "2 Ekim Cuma 03:30", "şimdi — …".
func backupNextLabel(res ipc.BackupPolicyResult, now time.Time) string {
	switch {
	case !res.Policy.Auto:
		return ""
	case res.Problem != "":
		return "bekliyor — " + res.Problem
	case res.Running:
		return "şu an alınıyor…"
	case res.Next == nil:
		return ""
	}
	next := *res.Next
	// Yanıt eskidiyse (sekme saatlerce açık kaldı) geçmiş bir saati "bugün
	// 04:00" diye yazmak yedeğin kaçtığını sandırır: zamanı gelen yedek
	// zamanlayıcının bir sonraki turunda (en geç bir dakika) başlar.
	if res.Due || !now.Before(next) {
		if res.Due && res.Reason != "" {
			return "şimdi — " + res.Reason
		}
		return "şimdi"
	}
	return backupWhenLabel(next, now)
}

var (
	trMonths   = [...]string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
	trWeekdays = [...]string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}
)

// backupWhenLabel writes t relative to now, in t's time zone.
//
// Zaman DAEMON'un dilimindedir (yanıtın ofsetiyle çözülür): "04:00" planı
// sistemin saatinde 04:00 demek. now aynı dilime çevrilmeden "bugün/yarın"
// kararı verilirse gece yarısına yakın saatlerde bir gün kayardı.
func backupWhenLabel(t, now time.Time) string {
	now = now.In(t.Location())
	clock := t.Format("15:04")
	y, m, d := now.Date()
	ty, tm, td := t.Date()
	switch {
	case ty == y && tm == m && td == d:
		return "bugün " + clock
	case sameDay(t, time.Date(y, m, d+1, 12, 0, 0, 0, t.Location())):
		return "yarın " + clock
	}
	s := fmt.Sprintf("%d %s %s", td, trMonths[tm-1], trWeekdays[t.Weekday()])
	if ty != y {
		s = fmt.Sprintf("%d %s %d", td, trMonths[tm-1], ty)
	}
	return s + " " + clock
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// ── Seçim pencereleri ───────────────────────────────────────────────────────

// Adım değerleri: pencereler arasında taşınan seçim.
const (
	bpOff    = "off"
	bpResume = "resume"
	bpHours  = "hours"
	bpDays   = "days"
	bpCustom = "custom"
)

// detailBackupPolicyMenu opens the first step: off / hours / days.
func (a *App) detailBackupPolicyMenu(d *ServerDetail, s *model.Server) {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	id, pol := s.ID, s.Backup
	cur, curErr := pol.Plan()
	auto := pol.Auto && curErr == nil
	items := []ListItem{
		{Label: "Kapalı", Detail: "otomatik yedek alınmaz", Current: !pol.Auto, Value: bpOff},
	}
	if !pol.Auto && curErr == nil {
		// Kapatılan plan saklanır (bkz. daemon mergeBackupPolicy): yeniden
		// açan kullanıcı saat ve kopya seçimlerini baştan yapmasın.
		items = append(items, ListItem{Label: "Eski planla yeniden aç",
			Detail: cur.Label() + " · " + model.BackupKeepLabel(pol.Keep), Value: bpResume})
	}
	items = append(items,
		ListItem{Label: "Saat aralığıyla…", Detail: "1 – 12 saatte bir", Current: auto && cur.Days == 0, Value: bpHours},
		ListItem{Label: "Gün aralığıyla…", Detail: "her gün – her hafta", Current: auto && cur.Days > 0, Value: bpDays},
	)
	a.OpenModal(NewListModal("Otomatik yedek", "Şu an: "+model.BackupPolicyLabel(pol), items,
		func(app *App, _ int, it ListItem) bool {
			switch it.Value.(string) {
			case bpOff:
				app.detailBackupPolicyApply(d, ipc.BackupSetPolicyParams{ServerID: id, Auto: false})
			case bpResume:
				app.detailBackupPolicyApply(d, ipc.BackupSetPolicyParams{ServerID: id, Auto: true})
			case bpHours:
				app.detailBackupHoursMenu(d, id, pol)
			case bpDays:
				app.detailBackupDaysMenu(d, id, pol)
			}
			return true
		}))
}

func (a *App) detailBackupHoursMenu(d *ServerDetail, id string, pol model.BackupPolicy) {
	cur, err := pol.Plan()
	items := make([]ListItem, 0, len(model.BackupHourChoices))
	for _, h := range model.BackupHourChoices {
		sc := model.BackupSchedule{Every: time.Duration(h) * time.Hour}
		items = append(items, ListItem{Label: sc.Label(), Value: sc.String(),
			Current: err == nil && cur.Days == 0 && cur.Every == sc.Every})
	}
	a.OpenModal(withDefault(NewListModal("Kaç saatte bir?", "Süre son yedekten itibaren sayılır.", items,
		func(app *App, _ int, it ListItem) bool {
			app.detailBackupKeepMenu(d, id, pol, it.Value.(string))
			return true
		}), model.DefaultBackupSchedule))
}

func (a *App) detailBackupDaysMenu(d *ServerDetail, id string, pol model.BackupPolicy) {
	cur, err := pol.Plan()
	items := make([]ListItem, 0, len(model.BackupDayChoices))
	for _, n := range model.BackupDayChoices {
		sc := model.BackupSchedule{Every: time.Duration(n) * 24 * time.Hour, Days: n}
		items = append(items, ListItem{Label: sc.Label(), Value: n,
			Current: err == nil && cur.Days == n})
	}
	a.OpenModal(NewListModal("Kaç günde bir?", "Sonra günün hangi saatinde alınacağını seçersiniz.", items,
		func(app *App, _ int, it ListItem) bool {
			app.detailBackupClockMenu(d, id, pol, it.Value.(int))
			return true
		}))
}

// detailBackupClockMenu asks the time of day for a day-based plan.
//
// 04:00 "önerilen": yedek alınırken dünya sıkıştırılır ve sunucu yavaşlar;
// gecenin en sessiz saati oyuncuların en az hissettiğidir.
func (a *App) detailBackupClockMenu(d *ServerDetail, id string, pol model.BackupPolicy, days int) {
	cur, err := pol.Plan()
	sameDays := err == nil && cur.Days == days
	base := model.BackupSchedule{Every: time.Duration(days) * 24 * time.Hour, Days: days}
	items := []ListItem{
		{Label: "Saat sabit değil", Detail: fmt.Sprintf("son yedekten %d saat sonra", days*24),
			Current: sameDays && !cur.HasAt, Value: base.String()},
		{Label: "Başka bir saat…", Detail: "SS:DD", Value: bpCustom},
	}
	// Elle (CLI/telefon) verilmiş buçuklu bir saat listede yoksa en üstte
	// "şu anki" olarak durur; yoksa imleç onu gösteremez.
	if sameDays && cur.HasAt && cur.AtMin%60 != 0 {
		items = append(items, ListItem{Label: cur.Clock(), Detail: "şu anki", Current: true, Value: cur.String()})
	}
	for h := 0; h < 24; h++ {
		sc := base
		sc.HasAt, sc.AtMin = true, h*60
		it := ListItem{Label: sc.Clock(), Value: sc.String(),
			Current: sameDays && cur.HasAt && cur.AtMin == sc.AtMin}
		if h == 4 {
			it.Detail = "önerilen"
		}
		items = append(items, it)
	}
	def := base
	def.HasAt, def.AtMin = true, 4*60
	a.OpenModal(withDefault(NewListModal(base.Label()+" — saat kaçta?",
		"Cihaz o saatte kapalıysa yedek açılınca hemen alınır.", items,
		func(app *App, _ int, it ListItem) bool {
			v := it.Value.(string)
			if v == bpCustom {
				app.detailBackupCustomClock(d, id, pol, base)
				return true
			}
			app.detailBackupKeepMenu(d, id, pol, v)
			return true
		}), def.String()))
}

// detailBackupCustomClock asks for an exact HH:MM ("03:30").
func (a *App) detailBackupCustomClock(d *ServerDetail, id string, pol model.BackupPolicy, base model.BackupSchedule) {
	value := "04:00"
	if cur, err := pol.Plan(); err == nil && cur.HasAt {
		value = cur.Clock()
	}
	a.OpenModal(NewTextModal("Yedek saati", base.Label()+", saat (SS:DD)",
		func(app *App, text string) {
			sc, err := model.ParseBackupSchedule(base.String() + "@" + strings.TrimSpace(text))
			if err != nil {
				return // WithValidate zaten reddetti
			}
			app.detailBackupKeepMenu(d, id, pol, sc.String())
		}).WithValue(value).WithPlaceholder("04:00").WithMaxLen(5).WithOK("Devam").
		WithHint("Sistem saat dilimine göre.").
		WithValidate(func(t string) string {
			// Doğrulama daemon'la AYNI ayrıştırıcıdan: panelin kabul edip
			// daemon'un reddettiği bir saat olmasın.
			if _, err := model.ParseBackupSchedule(base.String() + "@" + strings.TrimSpace(t)); err != nil {
				return upperFirst(err.Error())
			}
			return ""
		}))
}

// detailBackupKeepMenu asks how many copies to keep, then saves.
func (a *App) detailBackupKeepMenu(d *ServerDetail, id string, pol model.BackupPolicy, schedule string) {
	sc, err := model.ParseBackupSchedule(schedule)
	if err != nil {
		a.Emit(fbui.EventError, "Yedek planı: "+err.Error())
		return
	}
	// Hiç ayarlanmamış planın Keep'i 0'dır ama bu bir "sınırsız" SEÇİMİ
	// değildir; yalnızca bir kez kaydedilmiş plan "şu anki" işaretini alır.
	stored := strings.TrimSpace(pol.Schedule) != ""
	items := make([]ListItem, 0, len(model.BackupKeepChoices)+1)
	listed := false
	for _, k := range model.BackupKeepChoices {
		listed = listed || pol.Keep == k
		items = append(items, ListItem{Label: upperFirst(model.BackupKeepLabel(k)),
			Current: stored && pol.Keep == k, Value: k})
	}
	if !listed && pol.Keep > 0 {
		// Listede olmayan bir sayı (CLI ile 7) sessizce kaybolmasın.
		items = append([]ListItem{{Label: upperFirst(model.BackupKeepLabel(pol.Keep)), Detail: "şu anki",
			Current: true, Value: pol.Keep}}, items...)
	}
	a.OpenModal(withDefault(NewListModal(sc.Label()+" — kaç kopya?",
		"Sayı aşılınca en eski yedek silinir (elle alınanlar dahil).", items,
		func(app *App, _ int, it ListItem) bool {
			k := it.Value.(int)
			app.detailBackupPolicyApply(d, ipc.BackupSetPolicyParams{ServerID: id, Auto: true,
				Schedule: sc.String(), Keep: &k})
			return true
		}), model.DefaultBackupKeep))
}

// detailBackupPolicyApply saves the plan in the background.
func (a *App) detailBackupPolicyApply(d *ServerDetail, p ipc.BackupSetPolicyParams) {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	a.Emit(fbui.EventBusy, "Otomatik yedek planı kaydediliyor…")
	go func() {
		res, err := a.cl.SetBackupPolicy(p)
		if err != nil {
			a.Fail("otomatik yedek planı kaydedilemedi", err)
			return
		}
		d.mu.Lock()
		d.backupPol = detailBackupPolicy{res: res, ok: true}
		d.mu.Unlock()
		a.patchServerBackup(res.ServerID, res.Policy)
		a.Emit(fbui.EventOK, "Otomatik yedek: "+res.Summary)
	}()
}

// patchServerBackup updates the local server copy right away.
//
// detailUpdate ile aynı neden: yoklama bir saniye sonra aynı planı getirir,
// ama o saniye boyunca eski planı göstermek "kaydedilmedi mi?" dedirtir.
// Kayıt KOPYALANIR: aynı işaretçiyi çizim goroutine'i okuyor olabilir.
func (a *App) patchServerBackup(id string, pol model.BackupPolicy) {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := make([]*model.Server, len(a.servers))
	copy(list, a.servers)
	for i, s := range list {
		if s.ID == id {
			cp := s.Clone()
			cp.Backup = pol
			list[i] = cp
		}
	}
	a.servers = list
	a.dirty = true
}

// withDefault puts the cursor on the suggested value when no row is the
// current one.
//
// NewListModal imleci "şu anki" satıra, yoksa İLK satıra koyar. İlk satır
// burada en uç seçenek (her saat, 00:00, son 3 kopya); Enter'a basıp geçen
// kullanıcı farkında olmadan en sık yedeği ya da gece yarısını seçerdi.
func withDefault(m *ListModal, def any) *ListModal {
	for _, it := range m.items {
		if it.Current {
			return m
		}
	}
	for i, it := range m.items {
		if it.Value == def {
			m.SetCursor(i)
			break
		}
	}
	return m
}

// upperFirst capitalises the first letter ("son 5 kopya" → "Son 5 kopya").
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
