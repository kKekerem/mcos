package fbpanel

import (
	"strings"
	"testing"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
)

// Kurulum sihirbazının testleri.
//
// ── Neden bu kadar önemli? ──────────────────────────────────────────────────
// Sihirbaz, kullanıcının MCOS ile ilk teması. Burada takılan biri ürünü bir
// daha açmaz. Ayrıca sihirbazın bir hatası SESSİZDİR: kullanıcı her şeyi
// doldurur, "bitti" der, ve ayarlar hiç kaydedilmemiştir — tam olarak eski
// sihirbazda olan buydu (bkz. setup.go üstündeki not).

func TestEverySetupPageDraws(t *testing.T) {
	a, img := newTestApp(t)
	a.StartSetup()
	s := a.setupState()
	if s == nil {
		t.Fatal("sihirbaz başlamadı")
	}
	// Diske kurulum sayfası: disk listesi BOŞ olduğunda da dolu
	// çizilmeli (açıklama + "kurulum yapma" satırı).
	s.disks = []ipc.DiskTarget{}

	for page := setupStep(0); page < setupStepCount; page++ {
		s.step = page
		s.cursor = 0
		for i := range img.Pix {
			img.Pix[i] = 0
		}
		a.Draw()
		if ink := countInk(img); ink < 1500 {
			t.Errorf("sihirbaz sayfası %d (%q) neredeyse boş çizildi (%d piksel)",
				page, setupTitles[page], ink)
		}
	}
}

// Her sayfanın en az bir seçilebilir satırı olmalı: satırsız bir sayfada
// kullanıcı Enter'a bassa da hiçbir şey olmaz ve sihirbaz KİLİTLENİR.
func TestEverySetupPageHasRows(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	for page := setupStep(0); page < setupStepCount; page++ {
		s.step = page
		rows := s.rows(a)
		if len(rows) == 0 {
			t.Errorf("sayfa %d (%q) hiç satır sunmuyor — kullanıcı kilitlenir",
				page, setupTitles[page])
			continue
		}
		// Son sayfa dışında her sayfada ilerleme yolu olmalı.
		if page < stepSummary {
			found := false
			for _, r := range rows {
				if r.kind == rowContinue {
					found = true
				}
			}
			if !found {
				t.Errorf("sayfa %q 'devam' satırı sunmuyor", setupTitles[page])
			}
		}
	}
}

// Her sayfa başlığı ve açıklaması dolu olmalı: boş bir başlık, kullanıcının
// hangi adımda olduğunu bilmemesi demektir.
func TestSetupPagesAreLabelled(t *testing.T) {
	seen := map[string]bool{}
	for page := setupStep(0); page < setupStepCount; page++ {
		title := setupTitles[page]
		if strings.TrimSpace(title) == "" {
			t.Errorf("sayfa %d başlıksız", page)
			continue
		}
		if seen[title] {
			t.Errorf("başlık tekrar ediyor: %q", title)
		}
		seen[title] = true
		if strings.TrimSpace(setupSubtitle(page)) == "" {
			t.Errorf("sayfa %q açıklamasız", title)
		}
	}
}

// İleri-geri gezinme sihirbazı asla geçersiz bir duruma sokmamalı.
func TestSetupNavigationStaysInRange(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	// Sonuna kadar ilerle. stepSummary "kaydet" olduğu ve daemon yokken
	// kaydedemeyeceği için orada durur — bu DOĞRU davranıştır.
	for i := 0; i < int(setupStepCount)*2; i++ {
		a.setupKey("enter")
		if s.step < 0 || s.step >= setupStepCount {
			t.Fatalf("adım aralık dışına çıktı: %d", s.step)
		}
		if a.setupState() == nil {
			return // sihirbaz bitti, sorun yok
		}
	}

	// Sonuna kadar geri git.
	for i := 0; i < int(setupStepCount)*2; i++ {
		a.setupKey("esc")
		if s.step < 0 || s.step >= setupStepCount {
			t.Fatalf("geri giderken aralık dışına çıktı: %d", s.step)
		}
	}
	if s.step != stepWelcome {
		t.Errorf("geri gitme ilk sayfada durmadı: %d", s.step)
	}
}

// İmleç her sayfada listenin İÇİNDE kalmalı.
func TestSetupCursorStaysInList(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	for page := setupStep(0); page < setupStepCount; page++ {
		s.step = page
		s.cursor = 0
		n := len(s.rows(a))
		if n == 0 {
			continue
		}
		for i := 0; i < n*3; i++ {
			a.setupKey("down")
			if s.cursor < 0 || s.cursor >= n {
				t.Fatalf("sayfa %d: imleç %d, satır sayısı %d", page, s.cursor, n)
			}
		}
		for i := 0; i < n*3; i++ {
			a.setupKey("up")
			if s.cursor < 0 || s.cursor >= n {
				t.Fatalf("sayfa %d: imleç %d, satır sayısı %d", page, s.cursor, n)
			}
		}
	}
}

// Sol/sağ ile değer değiştirmek hiçbir dizini taşırmamalı.
func TestSetupAdjustWraps(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()
	s.step = stepLook

	for i := 0; i < 40; i++ {
		a.setupAdjust(s, 1)
		if s.themeIdx < 0 || s.themeIdx >= len(fbui.ThemeOrder) {
			t.Fatalf("tema dizini taştı: %d", s.themeIdx)
		}
	}
	for i := 0; i < 40; i++ {
		a.setupAdjust(s, -1)
		if s.themeIdx < 0 {
			t.Fatalf("tema dizini negatife düştü: %d", s.themeIdx)
		}
	}

	s.step = stepBudget
	s.cursor = 0
	for i := 0; i < 40; i++ {
		a.setupAdjust(s, 1)
		if s.ramIdx < 0 || s.ramIdx >= len(budgetRAM) {
			t.Fatalf("RAM dizini taştı: %d", s.ramIdx)
		}
	}
	s.cursor = 1
	for i := 0; i < 40; i++ {
		a.setupAdjust(s, -1)
		if s.cpuIdx < 0 || s.cpuIdx >= len(budgetCPU) {
			t.Fatalf("CPU dizini taştı: %d", s.cpuIdx)
		}
	}
}

// Daemon YOKKEN hiçbir tuş paniklememeli.
//
// Sihirbaz, daemon henüz açılmadan da çizilebiliyor olmalı: açılışta panel
// daemon'dan önce hazır olabilir.
func TestSetupOfflineNeverPanics(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	keys := []string{"up", "down", "left", "right", "enter", "esc", "tab",
		"g", "t", "n", "w", "e", "r", "s", "x", "f1", "backspace", "space"}

	for page := setupStep(0); page < setupStepCount; page++ {
		s.step = page
		for _, k := range keys {
			for i := 0; i < 3; i++ {
				a.setupKey(k)
				if m := a.ActiveModal(); m != nil {
					m.Key(a, "esc")
					a.CloseModal()
				}
			}
		}
		a.Draw()
	}
}

// Toggle'lar gerçekten değişmeli ve özet satırına yansımalı.
func TestSetupTogglesAffectSummary(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	// Başlangıç değerleri YAPILANDIRMADAN gelir (demo verisinde paylaşım
	// açık). Bu yüzden "açıldı mı?" diye değil, "TERS ÇEVRİLDİ mi?" diye
	// soruyoruz — test, varsayılanlar değişince kırılmamalı.
	wantSharing := !s.sharing
	wantMouse := !s.mouse
	wantAnim := !s.animations

	before := s.summaryLine()
	a.setupToggle(s, "sharing")
	a.setupToggle(s, "mouse")
	a.setupToggle(s, "anim")
	after := s.summaryLine()

	if before == after {
		t.Fatal("anahtarlar değişmedi — özet aynı kaldı")
	}
	if s.sharing != wantSharing {
		t.Errorf("paylaşım ters çevrilmedi: %s", after)
	}
	if s.mouse != wantMouse {
		t.Errorf("fare ters çevrilmedi: %s", after)
	}
	if s.animations != wantAnim {
		t.Errorf("animasyon ters çevrilmedi: %s", after)
	}
	// Özet satırı her anahtarı GERÇEKTEN yansıtmalı; yoksa kullanıcı
	// kaydetmeden önce yanlış bilgi görür.
	for _, key := range []string{"paylasim=", "fare=", "animasyon=", "playit="} {
		if !strings.Contains(after, key) {
			t.Errorf("özet satırında %q yok: %s", key, after)
		}
	}
}

// Kullanıcının bildirdiği mantık hatası: OOBE cihaz eşleştirmemeli.
func TestSetupDoesNotPair(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	for page := setupStep(0); page < setupStepCount; page++ {
		s.step = page
		for _, r := range s.rows(a) {
			label := strings.ToLower(r.label + " " + r.hint)
			// "eşleştir" fiili yalnızca MCOS Paylaşım ekranına aittir.
			if strings.Contains(label, "eşleştirebilir") ||
				strings.Contains(label, "cihaz seç") {
				t.Errorf("sayfa %q eşleştirme sunuyor: %q",
					setupTitles[page], r.label)
			}
		}
	}
}

// Bilgisayar adı doğrulaması, ağ yığınının kullanamayacağı adları reddetmeli.
func TestHostnameValidation(t *testing.T) {
	ok := []string{"mcos-1", "salon", "PC2", "a"}
	bad := []string{"", "   ", "-bas", "son-", "boşluk var", "türkçe_ğ",
		strings.Repeat("x", 40)}

	for _, s := range ok {
		if msg := validHostname(s); msg != "" {
			t.Errorf("geçerli ad reddedildi %q: %s", s, msg)
		}
	}
	for _, s := range bad {
		if validHostname(s) == "" {
			t.Errorf("geçersiz ad kabul edildi: %q", s)
		}
	}
}

// Eş adresi doğrulaması, elle girişte yazım hatalarını yakalamalı.
func TestPeerAddressValidation(t *testing.T) {
	ok := []string{"192.168.1.50", "mcos-2.local", "10.0.0.3:27890"}
	bad := []string{"", "   ", "192.168.1.50:", "192.168.1.50:abc", "a b"}

	for _, s := range ok {
		if msg := validatePeerAddress(s); msg != "" {
			t.Errorf("geçerli adres reddedildi %q: %s", s, msg)
		}
	}
	for _, s := range bad {
		if validatePeerAddress(s) == "" {
			t.Errorf("geçersiz adres kabul edildi: %q", s)
		}
	}
}

// Sihirbaz penceresi (liste/metin) açıkken panelin kısayolları çalışmamalı.
func TestSetupSwallowsPanelShortcuts(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()

	// "g" panelde Güç bölümüne gider. Sihirbazda hiçbir şey yapmamalı.
	before := a.Section()
	a.Key("g")
	if a.Section() != before {
		t.Error("sihirbazda 'g' tuşu panel bölümünü değiştirdi")
	}
	if a.setupState() == nil {
		t.Error("sihirbaz kapandı")
	}
}
