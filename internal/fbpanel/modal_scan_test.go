package fbpanel

import (
	"testing"
	"time"
)

// Canlı tarama penceresinin testleri.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Bu pencerenin tamamı bir YARIŞ üzerine kurulu: arka plandaki yoklama
// goroutine'i satırları değiştirirken çizim goroutine'i onları okuyor.
// Gözle bakarak doğrulanamaz; kırılması da sessiz olur (yanlış ağa bağlanmak
// gibi). Testler sözleşmeyi kilitliyor.

func newScanModal() *ScanModal {
	return NewScanModal("Tarama", "aranıyor…", func(*App, int, ListItem) bool { return true })
}

func items(labels ...string) []ListItem {
	out := make([]ListItem, 0, len(labels))
	for _, l := range labels {
		out = append(out, ListItem{Label: l})
	}
	return out
}

// Pencere BOŞ ve "taranıyor" hâlinde açılmalı: kullanıcının gördüğü ilk şey
// beklemek değil, dönen göstergedir.
func TestScanModalStartsScanningAndEmpty(t *testing.T) {
	m := newScanModal()
	if !m.Scanning() {
		t.Error("yeni pencere tarama hâlinde olmalı")
	}
	if m.Items() != 0 {
		t.Errorf("yeni pencere boş açılmalı, %d satır var", m.Items())
	}
	if m.Closed() {
		t.Error("yeni pencere kapalı olmamalı")
	}
}

// İmleç ADA göre korunmalı. İndise göre korunsaydı, araya yeni bir ağ
// düştüğünde imleç başka bir satıra kayar ve kullanıcı yanlış ağa bağlanırdı.
func TestScanModalKeepsCursorOnSameNetwork(t *testing.T) {
	m := newScanModal()
	m.Replace(items("EvWiFi", "Komsu"))
	m.SetCursor(1) // "Komsu"

	// Araya alfabetik olarak önce gelen bir ağ düşüyor.
	m.Replace(items("AAA_Misafir", "EvWiFi", "Komsu"))

	if got := m.Cursor(); got != 2 {
		t.Errorf("imleç 'Komsu' satırında kalmalıydı (2), %d bulundu", got)
	}
}

// Listeden kaybolan ağ seçiliyse imleç listenin dışına TAŞMAMALI.
func TestScanModalCursorStaysInRangeWhenRowVanishes(t *testing.T) {
	m := newScanModal()
	m.Replace(items("A", "B", "C"))
	m.SetCursor(2)

	m.Replace(items("A"))
	if got := m.Cursor(); got < 0 || got >= m.Items() {
		t.Fatalf("imleç aralık dışı: %d (satır: %d)", got, m.Items())
	}
}

// Eski satırın belirme animasyonu YENİDEN oynamamalı; yalnızca yeni satır
// animasyon ister. Aksi hâlde her yoklamada (400 ms'de bir) tüm liste
// titrerdi.
func TestScanModalOnlyNewRowsAnimate(t *testing.T) {
	m := newScanModal()
	m.Replace(items("A"))

	m.mu.Lock()
	first := m.seenAt[0]
	m.mu.Unlock()

	time.Sleep(2 * time.Millisecond)
	m.Replace(items("A", "B"))

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.seenAt[0].Equal(first) {
		t.Error("var olan satırın zaman damgası değişmemeli")
	}
	if !m.seenAt[1].After(first) {
		t.Error("yeni satır daha yeni damgalanmalı")
	}
}

// Tarama sürerken 60 kare/sn istenmeli (gösterge ve radar takılmasın);
// bittikten ve animasyonlar oturduktan sonra istenmemeli — boşta duran bir
// pencere için saniyede 60 kare çizmek, üstünde sunucu koşan bir makinede
// boşuna CPU demektir.
func TestScanModalAnimatesOnlyWhenBusy(t *testing.T) {
	m := newScanModal()
	if !m.Animating(true) {
		t.Error("tarama sürerken yeniden çizim istenmeli")
	}

	m.Replace(items("A"))
	m.SetScanning(false)
	if !m.Animating(true) {
		t.Error("yeni satırın belirme animasyonu sürerken çizim istenmeli")
	}

	time.Sleep(rowAppearDur + 20*time.Millisecond)
	if m.Animating(true) {
		t.Error("her şey durduktan sonra yeniden çizim İSTENMEMELİ")
	}
}

// Tarama sürerken ve liste boşken Enter pencereyi KAPATMAMALI: kullanıcı
// "seç" demek istiyor ama seçecek bir şey yok. Kapatmak, aramayı kullanıcının
// isteğinin tam tersi şekilde iptal ederdi.
func TestScanModalEnterOnEmptyListKeepsDialogOpen(t *testing.T) {
	a, _ := newTestApp(t)
	m := newScanModal()

	if done := m.Key(a, "enter"); done {
		t.Error("boş listede Enter pencereyi kapatmamalı")
	}
	if m.Closed() {
		t.Error("boş listede Enter pencereyi kapalı işaretlememeli")
	}
}

// Esc pencereyi kapatmalı VE kapalı olarak işaretlemeli: yoklama
// goroutine'i bunu görüp durmazsa, kullanıcı kapattıktan sonra da saniyede
// iki buçuk kez daemon'a istek gider.
func TestScanModalEscMarksClosed(t *testing.T) {
	a, _ := newTestApp(t)
	m := newScanModal()

	if done := m.Key(a, "esc"); !done {
		t.Error("Esc pencereyi kapatmalı")
	}
	if !m.Closed() {
		t.Error("Esc kapalı bayrağını kurmalı — yoklama durmalı")
	}
}

// Satır seçilince pencere kapanır ve kapalı işaretlenir.
func TestScanModalPickMarksClosed(t *testing.T) {
	a, _ := newTestApp(t)
	picked := ""
	m := NewScanModal("T", "h", func(_ *App, _ int, it ListItem) bool {
		picked = it.Label
		return true
	})
	m.Replace(items("EvWiFi"))

	if done := m.Key(a, "enter"); !done {
		t.Fatal("seçim pencereyi kapatmalı")
	}
	if picked != "EvWiFi" {
		t.Errorf("seçilen satır yanlış: %q", picked)
	}
	if !m.Closed() {
		t.Error("seçimden sonra kapalı işaretlenmeli")
	}
}

// Pencere HER durumda çizilebilmeli: boş+tarıyor, dolu+tarıyor, dolu+bitti,
// hata. Panel tek arayüz olduğu için bir çizim paniği kullanıcıyı ekransız
// bırakır.
func TestScanModalDrawsInEveryState(t *testing.T) {
	a, img := newTestApp(t)

	states := []func(m *ScanModal){
		func(m *ScanModal) {},                                              // boş + tarıyor
		func(m *ScanModal) { m.Replace(items("A", "B")) },                  // dolu + tarıyor
		func(m *ScanModal) { m.Replace(items("A")); m.SetScanning(false) }, // bitti
		func(m *ScanModal) { m.SetError("kart yok") },                      // hata
	}

	for i, st := range states {
		m := newScanModal()
		st(m)
		a.OpenModal(m)
		a.Draw()
		if img.Bounds().Empty() {
			t.Fatalf("durum %d: tuval boş", i)
		}
		a.CloseModal()
	}
}

// Çok uzun liste pencereyi taşırmamalı ve imleç her zaman görünür kalmalı.
func TestScanModalScrollsAndKeepsCursorVisible(t *testing.T) {
	m := newScanModal()
	var labels []string
	for i := 0; i < 40; i++ {
		labels = append(labels, "ag-"+itoa(i))
	}
	m.Replace(items(labels...))
	m.SetCursor(39)

	start, end := m.visibleRows(8)
	if 39 < start || 39 >= end {
		t.Fatalf("imleç görünür aralıkta değil: [%d,%d) imleç 39", start, end)
	}
	if end-start != 8 {
		t.Errorf("görünür satır sayısı 8 olmalı, %d", end-start)
	}
}

// Tıklama bölgeleri ekranın içinde kalmalı (pointer_test.go'daki sözleşme).
func TestScanModalZonesStayOnScreen(t *testing.T) {
	a, img := newTestApp(t)
	m := newScanModal()
	m.Replace(items("A", "B", "C"))
	a.OpenModal(m)
	a.Draw()

	a.mu.Lock()
	defer a.mu.Unlock()
	screen := img.Bounds()
	for _, z := range a.zones {
		if z.r.Empty() {
			t.Errorf("boş tıklama bölgesi: %v", z)
		}
		if !z.r.In(screen) {
			t.Errorf("bölge ekran dışında: %v", z.r)
		}
	}
}
