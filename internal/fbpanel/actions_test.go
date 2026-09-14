package fbpanel

import (
	"strings"
	"testing"
)

// Adlandırılmış eylemlerin testi.
//
// ── Neden gerekli? ──────────────────────────────────────────────────────────
// Fare tıklaması bir DİKDÖRTGENE gelir; o dikdörtgene iliştirilen AD ile
// eylem eşleşmezse hiçbir şey olmaz — ve sessizce olmaz. Kullanıcı tıklar,
// bekler, tekrar tıklar; hata mesajı yoktur.
//
// Bu testler, çizim tarafında kaydedilen her adın runAction'da bir karşılığı
// olduğunu (ve tersini) doğrular.

// knownActions lists every name runAction understands.
//
// Elle tutulur: yeni bir eylem eklendiğinde buraya da eklenmeli, yoksa test
// "tanımsız eylem" diye uyarır. Bu bilinçli bir sürtünme — sessiz bir
// eşleşmemeden iyidir.
var knownActions = []string{
	"peers-scan", "peers-manual", "peers-key", "peers-shared-world",
	"playit-claim", "playit-claim-info", "playit-start",
	"settings-theme", "settings-pointer", "settings-password",
	"sleep", "reboot", "poweroff",
}

// TestEveryRegisteredActionIsHandled — çizimde kaydedilen her ad işlenmeli.
func TestEveryRegisteredActionIsHandled(t *testing.T) {
	a, _ := newTestApp(t)

	// Her bölümü çiz ve kaydedilen eylem adlarını topla.
	registered := map[string]bool{}
	for s := Section(0); s < secCount; s++ {
		a.gotoSection(s)
		a.setFocus(FocusContent)
		a.Draw()
		a.mu.Lock()
		for _, z := range a.zones {
			if z.kind == zoneAction && z.key != "" {
				registered[z.key] = true
			}
		}
		a.mu.Unlock()
	}

	if len(registered) == 0 {
		t.Fatal("hiçbir ekran adlandırılmış eylem kaydetmiyor — " +
			"zoneAction yolu ölü demektir")
	}

	known := map[string]bool{}
	for _, k := range knownActions {
		known[k] = true
	}
	for key := range registered {
		if !known[key] {
			t.Errorf("çizimde kaydedilen %q eylemi runAction'da yok — "+
				"tıklama sessizce hiçbir şey yapmaz", key)
		}
	}
}

// Bilinen her eylem paniklemeden çalışmalı (daemon yokken de).
func TestKnownActionsNeverPanic(t *testing.T) {
	for _, key := range knownActions {
		a, _ := newTestApp(t)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%q eylemi panikledi: %v", key, r)
				}
			}()
			a.runAction(key)
			if m := a.ActiveModal(); m != nil {
				m.Key(a, "esc")
				a.CloseModal()
			}
		}()
	}
}

// Tanımsız bir ad SESSİZCE yutulmamalı: kullanıcı en azından bir satır görmeli.
func TestUnknownActionIsReported(t *testing.T) {
	a, _ := newTestApp(t)
	a.runAction("boyle-bir-sey-yok")

	ev := a.LastEvent()
	if ev == nil || !strings.Contains(ev.Text, "Tanımsız eylem") {
		t.Errorf("tanımsız eylem bildirilmedi: %+v", ev)
	}
}

// Güç eylemleri, ana döngünün işlemesi için doğru Action döndürmeli.
func TestPowerActionsReturnActions(t *testing.T) {
	a, _ := newTestApp(t)
	if got := a.runAction("sleep"); got != ActSleep {
		t.Errorf("sleep %v döndürdü, ActSleep bekleniyordu", got)
	}
	// reboot/poweroff önce ONAY penceresi açar: yıkıcı bir işlem tek
	// tıklamayla çalışmamalı.
	if got := a.runAction("reboot"); got != ActNone {
		t.Errorf("reboot onaysız çalıştı: %v", got)
	}
	if a.ActiveModal() == nil {
		t.Error("reboot onay penceresi açmadı")
	}
}
