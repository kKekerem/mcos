package fbpanel

import (
	"strings"
	"testing"

	"mcos/internal/fbui"
	"mcos/internal/model"
)

// Java 21 imajla GÖMÜLÜ geliyor (internal/java/builtin.go). Sihirbaz bunu
// "kurulu değil" ya da yalnızca "kurulu: 21" diye değil, gömülü olarak
// söylemeli; diğer (indirilmiş) sürümler ayrıca listelenmeli.
func TestJavaSetupNoteBuiltin(t *testing.T) {
	cases := []struct {
		name    string
		rt      []model.JavaRuntime
		builtin bool
		note    string
	}{
		{"yalniz-gomulu", []model.JavaRuntime{{Major: 21, Builtin: true}},
			true, "Java 21 gömülü, kurulu"},
		{"gomulu-ve-indirilmis", []model.JavaRuntime{{Major: 17}, {Major: 21, Builtin: true}},
			true, "Java 21 gömülü, kurulu · kurulu: 17"},
		{"indirilmis-21", []model.JavaRuntime{{Major: 21}},
			false, "kurulu: 21"},
		{"gomulu-ama-baska-surum", []model.JavaRuntime{{Major: 17, Builtin: true}},
			false, "Java 17 gömülü, kurulu"},
	}
	for _, c := range cases {
		b, note := javaSetupNote(c.rt)
		if b != c.builtin || note != c.note {
			t.Errorf("%s: javaSetupNote = (%v, %q), (%v, %q) bekleniyordu",
				c.name, b, note, c.builtin, c.note)
		}
	}
}

// Gömülü Java varken "Gerekli Java'yı kur" İNTERNET İSTEMEMELİ ve hiçbir şey
// indirmeye kalkmamalı. Eski davranış: adım koşulsuz ağ durumuna bakıp
// "İnternet yok — önce ağ adımına dönüp bağlanın" diyordu.
func TestSetupInstallJavaBuiltinIsNoop(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	a.mu.Lock()
	s.step = stepJava
	s.javaOK = true
	s.javaBuiltin = true
	a.mu.Unlock()

	a.setupInstallJava(s)

	ev := a.LastEvent()
	if ev == nil {
		t.Fatal("gömülü Java için hiçbir geri bildirim verilmedi")
	}
	if ev.Kind != fbui.EventOK || !strings.Contains(ev.Text, "gömülü") {
		t.Fatalf("olay = %v %q; 'gömülü' diyen bir tamam bekleniyordu", ev.Kind, ev.Text)
	}
	for _, e := range a.Events() {
		if strings.Contains(e.Text, "İnternet") || strings.Contains(e.Text, "indiriliyor") {
			t.Fatalf("gömülü Java varken ağ/indirme mesajı çıktı: %q", e.Text)
		}
	}
}

// Java sayfasının satır SAYISI gömülü durumda değişmemeli: denetim arka
// planda bittiğinde imleç başka bir satıra (örn. Devam) kaymasın. Kur
// satırı ise "hazır / gömülü" demeli.
func TestJavaStepRowsWhenBuiltin(t *testing.T) {
	s := &Setup{step: stepJava}
	before := s.rowsLocked()
	s.javaBuiltin = true
	after := s.rowsLocked()

	if len(before) != len(after) {
		t.Fatalf("satır sayısı %d -> %d değişti", len(before), len(after))
	}
	var install *setupRow
	for i := range after {
		if after[i].key == "java-install" {
			install = &after[i]
		}
	}
	if install == nil {
		t.Fatal("java-install satırı yok")
	}
	if install.value != "gömülü" || !strings.Contains(install.label, "hazır") {
		t.Fatalf("kur satırı = %q / %q; 'Java 21 hazır' / 'gömülü' bekleniyordu", install.label, install.value)
	}
	if strings.Contains(install.hint, "İnternet gerekir") {
		t.Fatalf("gömülü durumda ipucu hâlâ internet istiyor: %q", install.hint)
	}
}
