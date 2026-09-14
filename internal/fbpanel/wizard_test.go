package fbpanel

import (
	"strconv"
	"strings"
	"testing"
)

// Sunucu oluşturma sihirbazının testleri.
//
// ── Neden bu kadar önemli? ──────────────────────────────────────────────────
// Bu sihirbaz, daemon'a geçersiz veri gönderirse sunucu hiç kurulmaz ya da
// açılışta çöker — ve kullanıcı nedenini sihirbazda değil, sunucu
// günlüğünde aramak zorunda kalır. Doğrulama SAYFA BAŞINA yapılır ve
// gönderimden önce bir kez daha tekrarlanır; bu testler ikisini de kilitler.

func newTestWizard(t *testing.T) (*App, *Wizard) {
	t.Helper()
	a, _ := newTestApp(t)
	a.StartWizard()
	w := a.wizardState()
	if w == nil {
		t.Fatal("sihirbaz açılmadı")
	}
	// Daemon yok; sürüm listesini elle doldur.
	w.versions = []string{"1.21.11", "1.21.4", "1.20.6"}
	w.verLoaded = true
	return a, w
}

func TestEveryWizardPageDraws(t *testing.T) {
	a, w := newTestWizard(t)
	_, img := newTestApp(t)
	_ = img

	for page := wizStep(0); page < wizStepCount; page++ {
		w.step = page
		w.cursor = 0
		a.Draw() // panik etmemeli
		if len(w.rows(a)) == 0 {
			t.Errorf("sayfa %q hiç satır sunmuyor", wizTitles[page])
		}
	}
}

func TestWizardPagesAreLabelled(t *testing.T) {
	seen := map[string]bool{}
	for page := wizStep(0); page < wizStepCount; page++ {
		title := wizTitles[page]
		if strings.TrimSpace(title) == "" {
			t.Errorf("sayfa %d başlıksız", page)
			continue
		}
		if seen[title] {
			t.Errorf("başlık tekrar ediyor: %q", title)
		}
		seen[title] = true
		if strings.TrimSpace(wizSubtitle(page)) == "" {
			t.Errorf("sayfa %q açıklamasız", title)
		}
	}
}

// Kullanıcının bildirdiği mantık hatası: "sunucu kurarken secilmemli".
func TestWizardDoesNotOfferPeerSelection(t *testing.T) {
	a, w := newTestWizard(t)
	if w.hasPeerPicker(a) {
		t.Error("sihirbaz eş cihaz seçtiriyor — eşleştirme MAKİNEYE aittir")
	}

	// PC paylaşımı yalnızca bir ANAHTAR olmalı.
	w.step = wizResources
	found := false
	for _, r := range w.rows(a) {
		if r.key == "cluster" {
			found = true
			if r.kind != rowToggle {
				t.Errorf("PC paylaşımı bir anahtar değil: %v", r.kind)
			}
		}
	}
	if !found {
		t.Error("PC paylaşımı anahtarı yok")
	}
}

// İmleç her sayfada listenin içinde kalmalı.
func TestWizardCursorStaysInList(t *testing.T) {
	a, w := newTestWizard(t)
	for page := wizStep(0); page < wizStepCount; page++ {
		w.step = page
		w.cursor = 0
		n := len(w.rows(a))
		for i := 0; i < n*3; i++ {
			a.wizardKey("down")
			if w.cursor < 0 || w.cursor >= n {
				t.Fatalf("sayfa %d: imleç %d / %d", page, w.cursor, n)
			}
		}
		for i := 0; i < n*3; i++ {
			a.wizardKey("up")
			if w.cursor < 0 || w.cursor >= n {
				t.Fatalf("sayfa %d: imleç %d / %d", page, w.cursor, n)
			}
		}
	}
}

// Sol/sağ ile değer değiştirmek hiçbir dizini taşırmamalı.
func TestWizardAdjustWraps(t *testing.T) {
	a, w := newTestWizard(t)

	cases := []struct {
		step wizStep
		cur  int
		idx  *int
		n    int
		name string
	}{
		{wizTemplate, 0, &w.templateIdx, len(wizTemplates), "şablon"},
		{wizSoftware, 0, &w.softwareIdx, 10, "yazılım"},
		{wizGameplay, 1, &w.gamemodeIdx, len(wizGamemodes), "oyun modu"},
		{wizGameplay, 2, &w.difficultyIdx, len(wizDifficulties), "zorluk"},
		{wizResources, 0, &w.ramIdx, len(wizRAMChoices), "RAM"},
	}
	for _, c := range cases {
		w.step = c.step
		w.cursor = c.cur
		for i := 0; i < c.n*3; i++ {
			a.wizardAdjust(w, 1)
			if *c.idx < 0 || *c.idx >= c.n {
				t.Fatalf("%s dizini taştı: %d / %d", c.name, *c.idx, c.n)
			}
		}
		for i := 0; i < c.n*3; i++ {
			a.wizardAdjust(w, -1)
			if *c.idx < 0 || *c.idx >= c.n {
				t.Fatalf("%s dizini negatife düştü: %d", c.name, *c.idx)
			}
		}
	}
}

// Şablon seçmek gerçekten alanları doldurmalı.
func TestWizardTemplateFillsFields(t *testing.T) {
	_, w := newTestWizard(t)

	// "Yaratıcı — Fabric" şablonu.
	for i, tpl := range wizTemplates {
		if !strings.Contains(tpl.label, "Fabric") {
			continue
		}
		w.templateIdx = i
		w.applyTemplate()
		if string(w.software()) != "fabric" {
			t.Errorf("şablon yazılımı uygulamadı: %s", w.software())
		}
		if wizGamemodes[w.gamemodeIdx] != "creative" {
			t.Errorf("şablon oyun modunu uygulamadı: %s",
				wizGamemodes[w.gamemodeIdx])
		}
		return
	}
	t.Fatal("Fabric şablonu bulunamadı")
}

// "Özel" şablonu HİÇBİR alanı değiştirmemeli.
func TestCustomTemplateChangesNothing(t *testing.T) {
	_, w := newTestWizard(t)
	w.softwareIdx = 5
	w.gamemodeIdx = 2
	w.hardcore = true

	w.templateIdx = 0 // "Özel"
	w.applyTemplate()

	if w.softwareIdx != 5 || w.gamemodeIdx != 2 || !w.hardcore {
		t.Error("'Özel' şablonu alanları değiştirdi")
	}
}

// Doğrulama her sayfada çalışmalı.
func TestWizardValidation(t *testing.T) {
	_, w := newTestWizard(t)

	w.step = wizIdentity
	w.name = "   "
	if w.validateStep() == "" {
		t.Error("boş ad kabul edildi")
	}
	w.name = "Survival"
	if msg := w.validateStep(); msg != "" {
		t.Errorf("geçerli ad reddedildi: %s", msg)
	}

	w.step = wizNetwork
	for _, bad := range []string{"abc", "-1", "70000"} {
		w.port = bad
		if w.validateStep() == "" {
			t.Errorf("geçersiz port kabul edildi: %q", bad)
		}
	}
	w.port = "25565"
	if msg := w.validateStep(); msg != "" {
		t.Errorf("geçerli port reddedildi: %s", msg)
	}

	w.step = wizGameplay
	w.maxPlayers = "0"
	if w.validateStep() == "" {
		t.Error("0 oyuncu kabul edildi")
	}
	w.maxPlayers = "20"
	if msg := w.validateStep(); msg != "" {
		t.Errorf("geçerli oyuncu sayısı reddedildi: %s", msg)
	}

	w.step = wizResources
	w.cpuQuota = "500"
	if w.validateStep() == "" {
		t.Error("%500 CPU kabul edildi")
	}
	w.cpuQuota = "" // boş = sınırsız
	if msg := w.validateStep(); msg != "" {
		t.Errorf("boş CPU payı reddedildi: %s", msg)
	}

	// EULA kabul edilmeden geçilememeli — bu bir LİSANS şartı.
	w.step = wizEULA
	w.eula = false
	if w.validateStep() == "" {
		t.Error("EULA kabul edilmeden ilerlenebiliyor")
	}
	w.eula = true
	if msg := w.validateStep(); msg != "" {
		t.Errorf("EULA kabul edilmesine rağmen engellendi: %s", msg)
	}
}

// Gönderimden önceki son denetim, atlanan sayfaları da yakalamalı.
func TestWizardValidateAllCatchesSkippedPages(t *testing.T) {
	_, w := newTestWizard(t)
	w.name = "Test"
	w.eula = true
	w.port = "25565"
	w.maxPlayers = "20"
	if msg := w.validateAll(); msg != "" {
		t.Fatalf("geçerli sihirbaz reddedildi: %s", msg)
	}

	// Fare ile doğrudan özete atlanmış gibi: EULA işaretsiz.
	w.eula = false
	if w.validateAll() == "" {
		t.Error("EULA'sız gönderim engellenmedi")
	}
}

// Üretilen parametreler alanlarla birebir eşleşmeli.
func TestWizardParams(t *testing.T) {
	_, w := newTestWizard(t)
	w.name = "  Survival  "
	w.desc = "açıklama"
	w.softwareIdx = 1
	w.verIdx = 0
	w.port = "25577"
	w.render = "12"
	w.sim = "8"
	w.maxPlayers = "30"
	w.motd = "merhaba"
	w.ramIdx = 3
	w.cpuQuota = "75"
	w.clusterShare = true
	w.whitelist = true
	w.hardcore = true
	w.onlineMode = false
	w.pvp = false
	w.via = true
	w.autostart = true
	w.dataDir = " /data/x "

	p := w.params()
	if p.Name != "Survival" {
		t.Errorf("ad kırpılmadı: %q", p.Name)
	}
	if p.DataDir != "/data/x" {
		t.Errorf("klasör kırpılmadı: %q", p.DataDir)
	}
	if p.Port != 25577 || p.ViewDistance != 12 || p.SimDistance != 8 ||
		p.MaxPlayers != 30 || p.CPUQuota != 75 {
		t.Errorf("sayısal alanlar yanlış: %+v", p)
	}
	if p.RAMMB != wizRAMChoices[3] {
		t.Errorf("RAM yanlış: %d", p.RAMMB)
	}
	if p.MCVersion != "1.21.11" {
		t.Errorf("sürüm yanlış: %q", p.MCVersion)
	}
	// OnlineMode ve PVP İŞARETÇİDİR: "ayarlanmadı" ile "false" ayrılmalı.
	if p.OnlineMode == nil || *p.OnlineMode {
		t.Error("online mode false olarak gönderilmedi")
	}
	if p.PVP == nil || *p.PVP {
		t.Error("pvp false olarak gönderilmedi")
	}
	if !p.ClusterShare || !p.Whitelist || !p.Hardcore || !p.AllowOldVersions ||
		!p.Autostart {
		t.Errorf("anahtarlar taşınmadı: %+v", p)
	}
}

// Elle yazılan sürüm, listeden seçileni geçersiz kılmalı.
func TestManualVersionWins(t *testing.T) {
	_, w := newTestWizard(t)
	w.verIdx = 0
	if w.version() != "1.21.11" {
		t.Fatalf("liste sürümü alınmadı: %q", w.version())
	}
	w.manualVer = "1.8.8"
	if w.version() != "1.8.8" {
		t.Errorf("elle sürüm geçersiz kılmadı: %q", w.version())
	}
}

// Daemon YOKKEN hiçbir tuş paniklememeli.
func TestWizardOfflineNeverPanics(t *testing.T) {
	a, w := newTestWizard(t)
	keys := []string{"up", "down", "left", "right", "enter", "esc", "tab",
		"backspace", "space", "g", "q", "n"}

	for page := wizStep(0); page < wizStepCount; page++ {
		w.step = page
		for _, k := range keys {
			for i := 0; i < 3; i++ {
				a.wizardKey(k)
				if m := a.ActiveModal(); m != nil {
					m.Key(a, "esc")
					a.CloseModal()
				}
				if a.wizardState() == nil {
					// Esc ilk sayfada sihirbazı kapatır: yeniden aç.
					a.StartWizard()
					w = a.wizardState()
					w.step = page
				}
			}
		}
		a.Draw()
	}
}

// "n" tuşu artık uyarı değil, sihirbaz açmalı.
func TestNKeyOpensWizard(t *testing.T) {
	a, _ := newTestApp(t)
	if a.InWizard() {
		t.Fatal("sihirbaz zaten açık")
	}
	a.Key("n")
	if !a.InWizard() {
		t.Fatal("'n' tuşu sihirbazı açmadı")
	}
	// Sihirbaz açıkken panel kısayolları çalışmamalı.
	before := a.Section()
	a.Key("g")
	if a.Section() != before {
		t.Error("sihirbaz açıkken 'g' panel bölümünü değiştirdi")
	}
}

// Özet satırı her alanı yansıtmalı.
func TestWizardSummaryLine(t *testing.T) {
	_, w := newTestWizard(t)
	w.name = "Test"
	w.clusterShare = true
	w.eula = true
	line := w.summaryLine()
	for _, want := range []string{"ad=Test", "paylasim=açık", "eula=açık",
		"ram=" + strconv.Itoa(wizRAMChoices[w.ramIdx])} {
		if !strings.Contains(line, want) {
			t.Errorf("özet satırında %q yok: %s", want, line)
		}
	}
}
