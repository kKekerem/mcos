package fbpanel

import (
	"fmt"
	"sort"
	"time"

	"mcos/internal/fbui"
)

// ════════════════════════════════════════════════════════════════════════════
// ARKA PLAN İŞLERİ: uzun işlemler ekrandan bağımsız sürer ve hep görünür
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Kullanıcı (gerçek PC): "Java kur'a bastım, arkada birkaç şey yapıyorum,
// gidiyor o işlem; bir daha aynı ekranda kalmak zorunda kalıyorum."
//
// İş aslında bir goroutine'de sürüyordu; ama TEK göstergesi durum çubuğundaki
// "son olay"dı (EventBusy "Java 17 indiriliyor…"). O satır iki yoldan
// kayboluyordu:
//   - başka bir pencere açılınca clearBusyLocked son Busy olayını siliyordu,
//   - başka herhangi bir eylemin mesajı onu eziyordu.
// Java satırı da "kuruluyor" göstermiyordu: kullanıcı işlemin iptal olduğunu
// sanıp tekrar basıyor ve İKİNCİ bir kurulum başlıyordu.
//
// Artık uzun işler bu kayıtta tutulur: durum çubuğu (bkz. statusEvent) süren
// işi ve geçen süreyi her ekranda gösterir, aynı iş ikinci kez başlamaz,
// sonuç kullanıcı neredeyse orada bildirilir.

// jobMsgHold: bir iş sürerken yeni bir mesaj (başarı, uyarı) bu kadar süre
// öne çıkar, sonra durum çubuğu yeniden süren işi gösterir.
const jobMsgHold = 4 * time.Second

type bgJob struct {
	title   string
	started time.Time
}

// runJob starts work in the background unless a job with the same id is
// already running. work döner: başarıda kullanıcıya gösterilecek mesaj,
// hatada mesajın bağlamı (a.Fail'e gider). after yalnızca başarıda çağrılır.
func (a *App) runJob(id, title string, work func() (string, error), after func()) bool {
	return a.runJobKind(id, title, func() (fbui.EventKind, string, error) {
		msg, err := work()
		return fbui.EventOK, msg, err
	}, after)
}

// runJobKind is runJob with a result kind: bazı işlerin başarı ile hata
// dışında üçüncü bir sonucu var (ör. kalıcılık canlı DVD'de "geçerli değil"
// bilgisi — yeşil "tamam" göstermek yanlış olurdu).
func (a *App) runJobKind(id, title string, work func() (fbui.EventKind, string, error), after func()) bool {
	a.mu.Lock()
	if a.jobs == nil {
		a.jobs = map[string]*bgJob{}
	}
	if j, ok := a.jobs[id]; ok {
		a.mu.Unlock()
		a.Emit(fbui.EventInfo, j.title+" zaten sürüyor — bitince haber verilecek")
		return false
	}
	a.jobs[id] = &bgJob{title: title, started: nowFunc()}
	a.dirty = true
	a.mu.Unlock()

	go func() {
		kind, msg, err := work()
		a.mu.Lock()
		delete(a.jobs, id)
		a.dirty = true
		a.mu.Unlock()
		if err != nil {
			a.Fail(msg, err)
			return
		}
		a.Emit(kind, msg)
		if after != nil {
			after()
		}
	}()
	return true
}

// runningJobs returns the ids of running jobs (kilit ALMADAN çağrılır).
func (a *App) runningJobs() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]bool, len(a.jobs))
	for id := range a.jobs {
		out[id] = true
	}
	return out
}

// statusEvent is what the status bar shows: süren bir iş varsa o (yeni bir
// mesaj kısa süre öne çıkabilir), yoksa son olay.
func (a *App) statusEvent() *fbui.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	var son *fbui.Event
	if n := len(a.events); n > 0 {
		e := a.events[n-1]
		son = &e
	}
	if len(a.jobs) == 0 {
		return son
	}
	now := nowFunc()
	if son != nil && son.Kind != fbui.EventBusy && now.Sub(son.At) < jobMsgHold {
		return son
	}
	// En eski iş: kullanıcı en uzun süredir onu bekliyor.
	ids := make([]string, 0, len(a.jobs))
	for id := range a.jobs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, k int) bool {
		return a.jobs[ids[i]].started.Before(a.jobs[ids[k]].started)
	})
	j := a.jobs[ids[0]]
	gecen := now.Sub(j.started)
	if gecen < 0 {
		gecen = 0
	}
	sn := int(gecen.Seconds())
	text := fmt.Sprintf("%s · %d:%02d", j.title, sn/60, sn%60)
	if len(ids) > 1 {
		text += fmt.Sprintf(" · +%d iş", len(ids)-1)
	}
	return &fbui.Event{Kind: fbui.EventBusy, Text: text, At: j.started}
}
