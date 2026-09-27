package fbpanel

import "testing"

// "Atla" düğmesi ve tek-seçimde otomatik ilerlemenin testleri.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Kullanıcının iki net isteği var:
//
//	"eğer zorunlu bir soru değilse atla butonu olsun sağda, tab ile atlayabilelim"
//	"seçenek seçmek zorunluysa bir seçenek seçince devama basmak gerekmesin;
//	 birden fazlaysa seçip basalım"
//
// Bunların ikisi de SESSİZCE bozulabilir: bir sayfaya zorunlu bir alan
// eklendiğinde "Atla" düğmesi orada kalırsa kullanıcı doldurulmamış bir
// yapılandırmayla ilerler; otomatik ilerleme çok satırlı bir sayfaya sızarsa
// kullanıcı öbür alanları göremeden sayfa değişir.

// Atlanabilir her sayfa gerçekten bir "Atla" satırı sunmalı; zorunlu sayfalar
// SUNMAMALI.
func TestSetupSkipRowOnlyOnOptionalPages(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()
	if s == nil {
		t.Fatal("sihirbaz başlamadı")
	}

	for step := setupStep(0); step < setupStepCount; step++ {
		a.mu.Lock()
		s.step = step
		rows := s.rowsLocked()
		a.mu.Unlock()

		has := false
		for _, r := range rows {
			if r.kind == rowSkip {
				has = true
			}
		}
		if want := setupSkippable(step); has != want {
			t.Errorf("%s sayfası: Atla satırı=%v, beklenen=%v",
				setupTitles[step], has, want)
		}
	}
}

// Zorunlu sayfalarda Atla OLMAMALI — özellikle Özet: orada atlamak, ayarları
// hiç kaydetmeden kurulumu bitirmek olurdu.
func TestSetupSummaryIsNotSkippable(t *testing.T) {
	if setupSkippable(stepSummary) {
		t.Error("Özet sayfası atlanamamalı: ayarlar orada kaydediliyor")
	}
	if setupSkippable(stepInstall) {
		t.Error("Diske kur sayfasının kendi çıkış satırı var; Atla eklenmemeli")
	}
}

// Tab, atlanabilir sayfada SAYFAYI atlar.
func TestSetupTabSkipsOptionalPage(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	a.mu.Lock()
	s.step = stepLook // atlanabilir
	a.mu.Unlock()

	a.setupKey("tab")

	a.mu.Lock()
	got := s.step
	a.mu.Unlock()
	if got != stepBudget {
		t.Errorf("Tab sayfayı atlamalıydı: %v -> %v (beklenen %v)",
			stepLook, got, stepBudget)
	}
}

// Tab, zorunlu sayfada imleci indirir (eski davranış korunur): orada atlamak
// diye bir şey yok ve tuşun ölü kalması kötü olurdu.
func TestSetupTabMovesCursorOnRequiredPage(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	a.mu.Lock()
	s.step = stepSummary // zorunlu
	s.cursor = 0
	a.mu.Unlock()

	a.setupKey("tab")

	a.mu.Lock()
	step, cursor := s.step, s.cursor
	a.mu.Unlock()
	if step != stepSummary {
		t.Errorf("zorunlu sayfa Tab ile atlanmamalı, %v'e geçti", step)
	}
	if cursor != 1 {
		t.Errorf("Tab imleci indirmeliydi, imleç %d", cursor)
	}
}

// "Atla" satırını Enter ile çalıştırmak da sayfayı geçer (fare tıklaması bu
// yoldan gider).
func TestSetupSkipRowAdvances(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	a.mu.Lock()
	s.step = stepJava
	rows := s.rowsLocked()
	idx := -1
	for i, r := range rows {
		if r.kind == rowSkip {
			idx = i
		}
	}
	s.cursor = idx
	a.mu.Unlock()

	if idx < 0 {
		t.Fatal("Java sayfasında Atla satırı yok")
	}
	a.setupKey("enter")

	a.mu.Lock()
	got := s.step
	a.mu.Unlock()
	if got != stepLook {
		t.Errorf("Atla ilerletmeliydi: %v (beklenen %v)", got, stepLook)
	}
}

// Atla düğmesi SAĞ tarafta ve Devam düğmesiyle AYNI satırda çizilmeli,
// gövdenin dışına taşmamalı.
func TestSetupSkipButtonIsRightAlignedOnSameRow(t *testing.T) {
	a, img := newTestApp(t)
	a.StartSetup()
	s := a.setupState()

	a.mu.Lock()
	s.step = stepFeatures
	rows := s.rowsLocked()
	contIdx, skipIdx := -1, -1
	for i, r := range rows {
		switch r.kind {
		case rowContinue:
			contIdx = i
		case rowSkip:
			skipIdx = i
		}
	}
	a.mu.Unlock()
	if contIdx < 0 || skipIdx < 0 {
		t.Fatal("Devam ve Atla satırları birlikte bulunmalı")
	}

	a.Draw()

	a.mu.Lock()
	defer a.mu.Unlock()
	var cont, skip = -1, -1
	var contRect, skipRect = zone{}, zone{}
	for _, z := range a.zones {
		if z.kind != zoneRow {
			continue
		}
		if z.idx == contIdx {
			cont = z.r.Min.Y
			contRect = z
		}
		if z.idx == skipIdx {
			skip = z.r.Min.Y
			skipRect = z
		}
	}
	if cont < 0 || skip < 0 {
		t.Fatal("düğmelerin tıklama bölgeleri kaydedilmemiş")
	}
	if cont != skip {
		t.Errorf("Devam ve Atla aynı satırda olmalı: y=%d ve y=%d", cont, skip)
	}
	if skipRect.r.Min.X <= contRect.r.Max.X {
		t.Errorf("Atla, Devam'ın SAĞINDA olmalı: atla.x=%d devam.maxX=%d",
			skipRect.r.Min.X, contRect.r.Max.X)
	}
	if !skipRect.r.In(img.Bounds()) {
		t.Errorf("Atla düğmesi ekran dışına taşıyor: %v", skipRect.r)
	}
}

// ── Sunucu sihirbazı ────────────────────────────────────────────────────────

// Tek kararlı sayfalarda (şablon, yazılım) seçim satırı autoNext işaretli
// olmalı; çok alanlı sayfalarda OLMAMALI.
func TestWizardAutoNextOnlyOnSingleChoicePages(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartWizard()
	w := a.wizardState()
	if w == nil {
		t.Fatal("sihirbaz başlamadı")
	}

	want := map[wizStep]bool{
		wizTemplate: true,
		wizSoftware: true,
		wizVersion:  false, // sürüm + elle sürüm + ViaVersion
		wizGameplay: false, // sekiz alan
		wizNetwork:  false, // üç alan
	}

	for step, expect := range want {
		a.mu.Lock()
		w.step = step
		a.mu.Unlock()
		// DİKKAT: pageRows a.mu TUTULURKEN çağrılamaz — drainWizard kendi
		// kilidini alır ve sonuç kendi kendine kilitlenmedir.
		rows := w.pageRows(a)

		got := false
		for _, r := range rows {
			if r.autoNext {
				got = true
			}
		}
		if got != expect {
			t.Errorf("%s sayfası: autoNext=%v, beklenen=%v",
				wizTitles[step], got, expect)
		}
	}
}

// Şablon seçilince sayfa KENDİLİĞİNDEN ilerlemeli (kullanıcı ayrıca Devam'a
// basmak zorunda kalmamalı).
func TestWizardTemplatePickAdvances(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartWizard()
	w := a.wizardState()

	a.mu.Lock()
	w.step = wizTemplate
	w.cursor = 0
	a.mu.Unlock()
	rows := w.pageRows(a)
	if len(rows) == 0 || !rows[0].autoNext {
		t.Fatal("şablon satırı autoNext olmalı")
	}

	// Satırı çalıştır: seçim penceresi açılır ve autoNext kurulur.
	a.wizardActivate(w, rows[0])
	m := a.ActiveModal()
	if m == nil {
		t.Fatal("şablon seçim penceresi açılmadı")
	}

	// Pencerede bir şablon seç.
	if done := m.Key(a, "enter"); done {
		a.CloseModal()
	}

	a.mu.Lock()
	got := w.step
	a.mu.Unlock()
	if got != wizIdentity {
		t.Errorf("şablon seçimi ilerletmeliydi: %v (beklenen %v)",
			got, wizIdentity)
	}
}

// Seçim yapılmadan (Esc ile) kapatılan pencere sayfayı İLERLETMEMELİ:
// kullanıcı vazgeçmiştir.
func TestWizardAutoNextDoesNotFireOnCancel(t *testing.T) {
	a, _ := newTestApp(t)
	a.StartWizard()
	w := a.wizardState()

	a.mu.Lock()
	w.step = wizTemplate
	a.mu.Unlock()
	rows := w.pageRows(a)

	a.wizardActivate(w, rows[0])
	m := a.ActiveModal()
	if m == nil {
		t.Fatal("pencere açılmadı")
	}
	if done := m.Key(a, "esc"); done {
		a.CloseModal()
	}

	a.mu.Lock()
	got := w.step
	a.mu.Unlock()
	if got != wizTemplate {
		t.Errorf("vazgeçilen seçim ilerletmemeli, %v'e geçti", got)
	}
}

// Sunucu sihirbazında da Atla yalnızca varsayılanı olan sayfalarda olmalı.
// Özellikle EULA ve Özet ASLA atlanmamalı.
func TestWizardSkipNeverOnEULAOrConfirm(t *testing.T) {
	if wizSkippable(wizEULA) {
		t.Error("EULA atlanamaz: kabul zorunlu")
	}
	if wizSkippable(wizConfirm) {
		t.Error("Özet atlanamaz: sunucu orada kuruluyor")
	}
	if wizSkippable(wizIdentity) {
		t.Error("Sunucu adı zorunlu; kimlik sayfası atlanamaz")
	}
	if !wizSkippable(wizGameplay) {
		t.Error("Oyun ayarlarının hepsi varsayılanlı; atlanabilmeli")
	}
}
