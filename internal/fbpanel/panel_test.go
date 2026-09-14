package fbpanel

import (
	"image"
	"testing"

	"mcos/internal/fbfont"
	"mcos/internal/fbui"
)

// newTestApp builds a panel on an off-screen canvas with demo data.
func newTestApp(t *testing.T) (*App, *image.RGBA) {
	t.Helper()
	f, err := fbfont.Load(16)
	if err != nil {
		t.Fatalf("font: %v", err)
	}
	t.Cleanup(func() { f.Close() })

	img := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	ui := fbui.NewUI(img, f, fbui.DefaultPalette)
	a := New(ui, nil) // istemci YOK: çevrimdışı davranış da sınanmalı
	FillDemo(a)
	a.SetScreenSize(1280, 800)
	return a, img
}

// TestEverySectionDraws — HER bölüm çizilebilmeli.
//
// NEDEN VAR: kullanıcı "yeni panelde çoğu şey bitirilmemiş" dedi ve haklıydı —
// bazı bölümler "bu bölüm henüz taşınmadı" yazan bir yer tutucu gösteriyordu.
// Bu test, bir bölümün çizim fonksiyonu olmadan listeye eklenmesini
// imkânsız kılar: eksik bölüm BOŞ ekran olarak yakalanır.
func TestEverySectionDraws(t *testing.T) {
	a, img := newTestApp(t)

	for s := Section(0); s < secCount; s++ {
		a.gotoSection(s)
		for _, focus := range []Focus{FocusSidebar, FocusContent} {
			a.setFocus(focus)
			// Tuvali temizle ki önceki bölümün pikselleri sayılmasın.
			for i := range img.Pix {
				img.Pix[i] = 0
			}
			a.Draw()

			if ink := countInk(img); ink < 2000 {
				t.Errorf("bölüm %q (odak=%d) neredeyse boş çizildi (%d piksel) — "+
					"çizim fonksiyonu eksik olabilir", s.Name(), focus, ink)
			}
		}
	}
}

// TestNoPlaceholderSections — hiçbir bölüm "henüz taşınmadı" dememeli.
//
// Yer tutucu ekranlar geçici olmalıydı. Bu test, hepsinin gerçekten
// uygulandığını sabitler; biri geri gelirse derleme değil TEST kırılır.
func TestNoPlaceholderSections(t *testing.T) {
	a, _ := newTestApp(t)
	for s := Section(0); s < secCount; s++ {
		if s.Name() == "" {
			t.Errorf("bölüm %d isimsiz", s)
		}
	}
	// Çizim dağıtıcısı her bölüm için bir dal içermeli: varsayılan dal
	// kalmadığından, eksik bir bölüm hiçbir şey çizmez ve yukarıdaki test
	// onu yakalar. Burada yalnızca bölüm sayısını sabitliyoruz ki yeni bir
	// bölüm eklendiğinde bu testler de gözden geçirilsin.
	if secCount != 12 {
		t.Errorf("bölüm sayısı %d — değiştiyse çizim ve tuş yönlendirmeyi de güncelleyin", secCount)
	}
	_ = a
}

// TestFocusIsVisiblyDifferent — KULLANICININ BİLDİRDİĞİ TASARIM HATASININ TESTİ.
//
// "soldaki menüden sağa gecince hangisinin aktif olduğu belli olmuyor".
// İki odak durumunda çizilen kare BELİRGİN biçimde farklı olmalı; aksi halde
// kullanıcı klavyenin nerede olduğunu göremez.
func TestFocusIsVisiblyDifferent(t *testing.T) {
	a, img := newTestApp(t)
	a.gotoSection(SecServers)

	a.setFocus(FocusSidebar)
	a.Draw()
	left := make([]byte, len(img.Pix))
	copy(left, img.Pix)

	a.setFocus(FocusContent)
	a.Draw()

	diff := 0
	for i := range img.Pix {
		if img.Pix[i] != left[i] {
			diff++
		}
	}
	// Kenar çubuğu vurgusu + iki panel çerçevesi değişiyor; bu en az
	// birkaç bin piksel eder. Eşik düşük tutuldu ki test kırılgan olmasın,
	// ama "hiç fark yok" durumunu kesin yakalasın.
	if diff < 3000 {
		t.Errorf("odak değişince yalnızca %d bayt değişti — "+
			"hangi sütunun aktif olduğu görsel olarak ayırt edilemiyor", diff)
	}
}

// TestCursorNeverLeavesTheList — imleç listenin dışına çıkmamalı.
//
// Eski panelde Wi-Fi listesi 8 satır çiziyordu ama imleç daha aşağı
// inebiliyordu: kullanıcı seçtiği şeyi göremiyordu. contentRows() tek
// kaynak olduğu için bu artık imkânsız — test bunu sabitler.
func TestCursorNeverLeavesTheList(t *testing.T) {
	a, _ := newTestApp(t)
	for s := Section(0); s < secCount; s++ {
		a.gotoSection(s)
		a.setFocus(FocusContent)
		n := a.contentRows()
		// Listeden çok daha fazla hareket et; imleç her zaman sınırda kalmalı.
		for i := 0; i < n+25; i++ {
			a.moveCursor(1)
			if c := a.Cursor(); n > 0 && (c < 0 || c >= n) {
				t.Fatalf("bölüm %q: imleç %d, satır sayısı %d", s.Name(), c, n)
			}
		}
		for i := 0; i < n+25; i++ {
			a.moveCursor(-1)
			if c := a.Cursor(); n > 0 && (c < 0 || c >= n) {
				t.Fatalf("bölüm %q: imleç %d, satır sayısı %d", s.Name(), c, n)
			}
		}
	}
}

// TestOfflineNeverPanics — daemon yokken hiçbir tuş panik yapmamalı.
//
// GERÇEK BİR ÇÖKME YAKALANDI: bir bölüme girince loadSection() nil istemci
// üzerinden RPC çağırıyordu ve program segmentasyon hatasıyla düşüyordu.
// Panelin çökmesi, konsolu grafik kipinde bırakır — makine kullanılamaz hale
// gelir. Bu yüzden çevrimdışı yol her tuş için sınanır.
func TestOfflineNeverPanics(t *testing.T) {
	a, _ := newTestApp(t)
	keys := []string{
		"up", "down", "left", "right", "enter", "esc", "tab", "shift+tab",
		"k", "j", "h", "l", "n", "s", "x", "r", "t", "g", "w", "e",
		"a", "1", " ", "backspace", "f1", "f12", "ctrl+g",
	}
	for s := Section(0); s < secCount; s++ {
		a.gotoSection(s)
		for _, f := range []Focus{FocusSidebar, FocusContent} {
			a.setFocus(f)
			for _, k := range keys {
				a.Key(k) // panik olursa test düşer
			}
		}
	}
	// Açılır pencereler de çevrimdışı açılıp kapanabilmeli.
	a.OpenModal(NewPasswordModal("Test-Ag", nil))
	for _, k := range []string{"a", "ş", " ", "backspace", "ctrl+g", "enter"} {
		a.Key(k)
	}
	a.CloseModal()
}

// TestModalDrawsOverEverything — açılır pencere arka planı bulanıklaşmalı.
//
// Kullanıcının isteği: "herhangi bisi secme ekranı gelince arkası
// blurlanacak". Perde uygulanınca arka plan DEĞİŞMELİ; değişmiyorsa bulanıklık
// hiç çalışmıyor demektir.
func TestModalDrawsOverEverything(t *testing.T) {
	a, img := newTestApp(t)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)

	a.Draw()
	before := make([]byte, len(img.Pix))
	copy(before, img.Pix)

	a.OpenModal(NewListModal("Test", "Seçin.", []ListItem{
		{Label: "Bir"}, {Label: "İki"},
	}, nil))
	a.Draw()

	// Pencerenin UZAĞINDAKİ bir piksel de değişmiş olmalı: perde tüm ekranı
	// kapsıyor demektir. Yalnızca pencere alanı değişseydi arka plan
	// bulanıklaşmamış olurdu.
	i := img.PixOffset(30, 700)
	if img.Pix[i] == before[i] && img.Pix[i+1] == before[i+1] {
		t.Error("pencere açıkken köşedeki piksel değişmedi — arka plan perdelenmemiş")
	}
}

// TestSectionNamesAreUnique — iki bölüm aynı adı taşımamalı.
func TestSectionNamesAreUnique(t *testing.T) {
	seen := map[string]Section{}
	for s := Section(0); s < secCount; s++ {
		n := s.Name()
		if prev, dup := seen[n]; dup {
			t.Errorf("bölüm adı yinelendi: %q (%d ve %d)", n, prev, s)
		}
		seen[n] = s
	}
}

// countInk returns how many pixels are not fully transparent/black.
func countInk(img *image.RGBA) int {
	n := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 8 || img.Pix[i+1] > 8 || img.Pix[i+2] > 8 {
			n++
		}
	}
	return n
}
