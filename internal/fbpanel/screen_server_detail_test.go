package fbpanel

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// SUNUCU DETAYI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "sunucuya enter'e basınca o ekrana gitsin, böyle olmuyor".
// Enter eskiden sunucuyu başlatıp durduruyordu.

// newSizedApp builds a panel at w×h with the font size the real panel would
// pick for that screen (AutoFontSize) — sabit 16 px ile 4K'daki taşmalar
// hiç görünmezdi.
func newSizedApp(t *testing.T, w, h int) (*App, *image.RGBA) {
	t.Helper()
	f, err := fbfont.Load(AutoFontSize(h))
	if err != nil {
		t.Fatalf("font: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	a := New(fbui.NewUI(img, f, fbui.DefaultPalette), nil)
	a.SetHeadless(true)
	FillDemo(a)
	a.SetScreenSize(w, h)
	return a, img
}

// Enter DETAYI açmalı, sunucuyu başlatıp durdurmamalı.
func TestEnterOnServerListOpensDetail(t *testing.T) {
	a, _ := newTestApp(t)
	// Bir istemci ver ki başlat/durdur yapılsaydı olay satırına düşsün
	// (bağlantısız istemci ErrNoDaemon döner, ama önce "…ediliyor" yazar).
	a.cl = &ipcclient.Client{}
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(1)
	before := len(a.Events())

	a.Key("enter")

	d := a.detailState()
	if d == nil {
		t.Fatal("Enter sunucu listesinde detayı açmadı")
	}
	if d.id != "b" {
		t.Errorf("detay %q için açıldı, imleçteki sunucu b", d.id)
	}
	for _, e := range a.Events()[before:] {
		if strings.Contains(e.Text, "başlatılıyor") || strings.Contains(e.Text, "durduruluyor") {
			t.Errorf("Enter hâlâ sunucuyu başlatıp/durduruyor: %q", e.Text)
		}
	}
}

// Esc listeye dönmeli ve imleci ESKİ satıra koymalı; detay kendi imlecini
// tutmalı (detayda gezinmek liste imlecini kıpırdatmamalı).
func TestDetailEscRestoresListCursor(t *testing.T) {
	a, _ := newTestApp(t)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(1)
	a.Key("enter")
	d := a.detailState()
	if d == nil {
		t.Fatal("detay açılmadı")
	}

	a.Key("right") // Konsol
	a.Key("right") // Ayarlar
	a.Key("down")
	a.Key("down")
	a.wheel(-1) // fare tekerleği de detayda kalmalı
	if a.Cursor() != 1 {
		t.Errorf("detayda gezinmek liste imlecini %d yaptı", a.Cursor())
	}
	if d.cursor[dtSettings] != 3 {
		t.Errorf("detayın kendi imleci %d, 3 bekleniyordu", d.cursor[dtSettings])
	}

	a.Key("esc")
	if a.detailState() != nil {
		t.Fatal("Esc detayı kapatmadı")
	}
	if a.Section() != SecServers || a.Focus() != FocusContent {
		t.Errorf("Esc listeye dönmedi: bölüm=%v odak=%v", a.Section(), a.Focus())
	}
	if a.Cursor() != 1 {
		t.Errorf("imleç %d satırına döndü, 1 bekleniyordu", a.Cursor())
	}
}

// Detay açıkken liste yeniden sıralandıysa imleç SUNUCUYU izlemeli, eski
// dizini değil.
func TestDetailEscFollowsServerWhenListReorders(t *testing.T) {
	a, _ := newTestApp(t)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(1) // b
	a.Key("enter")

	_, servers, _ := a.Snapshot()
	rev := []*model.Server{servers[2], servers[0], servers[1]} // b artık 2. dizinde
	a.SetServers(rev)

	a.Key("esc")
	if a.Cursor() != 2 {
		t.Errorf("imleç %d; b sunucusu 2. satırda", a.Cursor())
	}
}

func TestDetailTabSwitching(t *testing.T) {
	a, _ := newTestApp(t)
	d := DemoServerDetail(a, 0)
	if d == nil {
		t.Fatal("detay açılmadı")
	}
	steps := []struct {
		key  string
		want detailTab
	}{
		{"right", dtConsole},
		{"tab", dtSettings},
		{"l", dtPlayers},
		{"left", dtSettings},
		{"shift+tab", dtConsole},
		{"[", dtGeneral},
		{"h", dtNetwork}, // başa sarar
		{"]", dtGeneral},
		{"detail-tab-4", dtSoftware}, // fare
	}
	for _, s := range steps {
		a.Key(s.key)
		if d.tab != s.want {
			t.Fatalf("%q sonrası sekme %q, %q bekleniyordu", s.key, d.tab.Name(), s.want.Name())
		}
	}
	if a.detailState() == nil {
		t.Fatal("sekme değişimi detayı kapattı")
	}
}

// Eski paneldeki on iki sekmenin hepsi, aynı sırayla.
func TestDetailHasEveryLegacyTab(t *testing.T) {
	legacy := []string{"Genel", "Konsol", "Ayarlar", "Oyuncular", "Yazılım",
		"Dosyalar", "Dünyalar", "Yedekler", "Erişim", "İnternete Aç", "Performans", "Ağ"}
	if int(dtCount) != len(legacy) {
		t.Fatalf("%d sekme var, eski panelde %d", dtCount, len(legacy))
	}
	for i, n := range legacy {
		if detailTab(i).Name() != n {
			t.Errorf("sekme %d = %q, eski panelde %q", i, detailTab(i).Name(), n)
		}
	}
}

// F12 eski panele acil çıkış: detay tuşları onu YUTMAMALI.
func TestDetailKeepsLegacyPanelKey(t *testing.T) {
	a, _ := newTestApp(t)
	DemoServerDetail(a, int(dtConsole))
	if got := a.Key("f12"); got != ActLegacyPanel {
		t.Errorf("detayda F12 = %v, ActLegacyPanel bekleniyordu", got)
	}
}

// Genel sekmesi istenen bilgilerin hepsini göstermeli.
func TestDetailGeneralRows(t *testing.T) {
	a, _ := newTestApp(t)
	d := DemoServerDetail(a, int(dtGeneral))
	s := a.detailServer(d)
	v := a.detailViewFor(d, s)
	got := map[string]string{}
	for _, kv := range v.info {
		got[kv[0]] = kv[1]
	}
	want := map[string]string{
		"Durum":          "Çalışıyor",
		"Yazılım":        "Paper",
		"Sürüm":          "Minecraft 1.21.1",
		"Port":           "25565",
		"RAM":            "4096 MB",
		"Oyuncular":      "3 / 20",
		"Çalışma süresi": "5sa 17dk",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("Genel %q = %q, %q bekleniyordu", k, got[k], w)
		}
	}
	if !strings.Contains(got["CPU"], "sistem %37") {
		t.Errorf("CPU satırı sistem kullanımını göstermiyor: %q", got["CPU"])
	}
	if !v.buttons {
		t.Error("Genel sekmesinde başlat/durdur düğmeleri yok")
	}
}

// Ayarlar satırları ve imleç tek kaynaktan: satır sayısı kadar gezinilir.
func TestDetailSettingsRowsAndCursorWrap(t *testing.T) {
	a, _ := newTestApp(t)
	d := DemoServerDetail(a, int(dtSettings))
	s := a.detailServer(d)
	v := a.detailViewFor(d, s)
	var labels []string
	for _, it := range v.items {
		labels = append(labels, it.label)
	}
	for _, need := range []string{"RAM", "Görüş uzaklığı", "Simülasyon uzaklığı", "Maksimum oyuncu",
		"Tam performans", "Otomatik başlat", "İnternete aç (WAN)"} {
		if !strings.Contains(strings.Join(labels, "|"), need) {
			t.Errorf("Ayarlar'da %q yok (eski panelde vardı)", need)
		}
	}
	for i := 0; i < len(v.items); i++ {
		a.Key("down")
	}
	if d.cursor[dtSettings] != 0 {
		t.Errorf("%d aşağıdan sonra imleç %d — sarmalı", len(v.items), d.cursor[dtSettings])
	}
}

// Vanilla eklenti yükleyemez: arama AÇILMAMALI ve nedeni söylenmeli.
func TestVanillaBlocksSearch(t *testing.T) {
	a, _ := newTestApp(t)
	a.cl = &ipcclient.Client{} // çevrimiçi gibi: engel çevrimdışılıktan gelmesin
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(2) // "Test" = vanilla
	a.Key("enter")
	d := a.detailState()
	if d == nil {
		t.Fatal("detay açılmadı")
	}
	a.Key("detail-tab-4")
	a.Key("/")

	if _, isText := a.ActiveModal().(*TextModal); isText {
		t.Fatal("Vanilla'da arama kutusu açıldı")
	}
	if _, isInfo := a.ActiveModal().(*InfoModal); !isInfo {
		t.Errorf("neden açıklanmadı: açık pencere %T", a.ActiveModal())
	}
	a.CloseModal()

	s := a.detailServer(d)
	a.detailSearch(d, s, "worldedit", false)
	d.mu.Lock()
	busy, q := d.search.busy, d.search.query
	d.mu.Unlock()
	if busy || q != "" || d.swView != swInstalled {
		t.Errorf("Vanilla'da arama başladı (busy=%v q=%q görünüm=%d)", busy, q, d.swView)
	}
	v := a.detailViewFor(d, s)
	if len(v.note) == 0 || !strings.Contains(strings.Join(v.note, " "), "eklenti ya da mod yüklemez") {
		t.Errorf("Yazılım sekmesi nedeni açıklamıyor: %v", v.note)
	}
}

// Rozet sunucunun yazılımından: Modrinth WorldEdit'e "mod" dese bile Paper
// sunucusunda "eklenti" yazmalı. Geri düşüşle bulunan sonuç "X uyumlu" demeli.
func TestSearchBadgeFromServerSoftware(t *testing.T) {
	a, _ := newTestApp(t)
	d := DemoServerDetail(a, int(dtSoftware))
	s := a.detailServer(d)
	v := a.detailViewFor(d, s)
	if v.header != "paper · 1.21.1 · 21 sonuç" {
		t.Errorf("başlık %q", v.header)
	}
	for _, it := range v.items {
		if it.act == "result" && it.badge != "eklenti" {
			t.Errorf("%q rozeti %q, eklenti bekleniyordu", it.label, it.badge)
		}
		if ci, ok := it.value.(ipc.CatalogItem); ok && ci.Loader == "bukkit" && !strings.Contains(it.detail, "bukkit uyumlu") {
			t.Errorf("geri düşüşlü sonuç yükleyiciyi söylemiyor: %q", it.detail)
		}
	}
}

// 0 sonuçta "sürüm süzgecini kaldır" seçeneği olmalı ve Enter onu yapmalı.
func TestZeroResultsOffersRemovingVersionFilter(t *testing.T) {
	a, _ := newTestApp(t)
	d := DemoServerDetail(a, int(dtSoftware))
	d.mu.Lock()
	d.search = detailSearch{query: "nadir", done: true,
		res: ipc.CatalogSearchResult{Loaders: []string{"paper"}, GameVersion: "1.21.1"}}
	d.mu.Unlock()
	s := a.detailServer(d)
	v := a.detailViewFor(d, s)
	if len(v.items) != 1 || v.items[0].act != "search-any" {
		t.Fatalf("0 sonuçta süzgeci kaldırma seçeneği yok: %+v", v.items)
	}
	if !strings.Contains(v.header, "0 sonuç") {
		t.Errorf("başlık %q", v.header)
	}
	d.cursor[dtSoftware] = 0
	a.Key("enter")
	d.mu.Lock()
	anyV, q := d.search.anyVersion, d.search.query
	d.mu.Unlock()
	if !anyV || q != "nadir" {
		t.Errorf("Enter süzgeçsiz aramayı başlatmadı (any=%v q=%q)", anyV, q)
	}
	if h := searchHeader(ipc.CatalogSearchResult{Loaders: []string{"paper"}, Total: 4}, 4); h != "paper · tüm sürümler · 4 sonuç" {
		t.Errorf("süzgeçsiz başlık %q", h)
	}
}

// Kurulumdan sonra çalışan sunucu için yeniden başlatma sorulmalı ve onay
// GERÇEKTEN yeniden başlatmalı; kapalı sunucuya sorulmamalı.
func TestInstallAsksToRestart(t *testing.T) {
	a, _ := newTestApp(t)
	d := DemoServerDetail(a, int(dtSoftware))
	a.cl = &ipcclient.Client{} // bağlantısız ama nil değil: eylem olay yazar

	a.detailAfterInstall(d, "a", "worldedit (paper uyumlu) kuruldu")
	m, ok := a.ActiveModal().(*ConfirmModal)
	if !ok {
		t.Fatalf("kurulumdan sonra onay penceresi açılmadı: %T", a.ActiveModal())
	}
	if !strings.Contains(strings.Join(m.lines, " "), "Eklentinin yüklenmesi için sunucu yeniden başlatılsın mı?") {
		t.Errorf("onay metni yanlış: %v", m.lines)
	}
	if last := a.Events(); !hasEvent(last, "worldedit (paper uyumlu) kuruldu") {
		t.Error("kurulum özeti olay satırına yazılmadı")
	}
	m.Key(a, "confirm")
	if !hasEvent(a.Events(), "Survival yeniden başlatılıyor") {
		t.Error("onay yeniden başlatmayı istemedi")
	}

	a.CloseModal()
	a.detailAfterInstall(d, "c", "x kuruldu") // "Test" kapalı
	if _, ok := a.ActiveModal().(*ConfirmModal); ok {
		t.Error("kapalı sunucuya yeniden başlatma soruldu")
	}
	if !hasEvent(a.Events(), "başlatıldığında yüklenecek") {
		t.Error("kapalı sunucu için bilgi verilmedi")
	}
}

func hasEvent(evs []fbui.Event, sub string) bool {
	for _, e := range evs {
		if strings.Contains(e.Text, sub) {
			return true
		}
	}
	return false
}

// r tuşu detayda YENİDEN BAŞLATIR (eski panelle aynı); serverAction("restart")
// artık bir tuşa bağlı.
func TestDetailRestartKey(t *testing.T) {
	a, _ := newTestApp(t)
	DemoServerDetail(a, int(dtGeneral))
	a.cl = &ipcclient.Client{}
	a.Key("r")
	if !hasEvent(a.Events(), "Survival yeniden başlatılıyor") {
		t.Error("r yeniden başlatmadı")
	}
}

// Modrinth başlıkları ASCII dışı olabilir: eksik glifler görünür bir yer
// tutucuya dönmeli, satır sığdırılmalı.
func TestFitTextHandlesMissingGlyphs(t *testing.T) {
	f, err := fbfont.Load(16)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := fitText(f, "WorldEditCUI 世界編集 🚀️ çok uzun", 20)
	for _, r := range got {
		if f.Glyph(r).Missing {
			t.Fatalf("sığdırılmış metinde hâlâ eksik glif var: %q (%q)", got, r)
		}
	}
	if !strings.Contains(got, "?") {
		t.Errorf("eksik glifler için yer tutucu yok: %q", got)
	}
	if n := len([]rune(got)); n > 20 {
		t.Errorf("%d kolon, en çok 20", n)
	}
	if fitText(f, "çok uzun Türkçe başlık", 100) != "çok uzun Türkçe başlık" {
		t.Error("Türkçe harfler bozuldu")
	}
}

// On iki sekme dar ekrana sığmaz: etkin sekme HER ZAMAN pencerede olmalı.
func TestTabWindowKeepsActiveVisible(t *testing.T) {
	widths := []int{50, 60, 70, 90, 70, 80, 80, 80, 60, 120, 100, 30}
	for act := range widths {
		s, e := tabWindow(widths, 5, 300, act)
		if act < s || act >= e {
			t.Errorf("etkin %d penceresi [%d,%d) dışında", act, s, e)
		}
		used := 0
		for i := s; i < e; i++ {
			used += widths[i]
			if i > s {
				used += 5
			}
		}
		if used > 300 {
			t.Errorf("pencere %d px, alan 300", used)
		}
	}
}

// ── Çizim: her sekme, her çözünürlükte ─────────────────────────────────────

// shotDir, PNG'lerin yazılacağı klasör (boşsa yazılmaz).
//
// Ortam değişkeniyle: normal test koşusu diske bir şey yazmamalı.
func shotDir() string { return os.Getenv("MCOS_DETAIL_SHOTS") }

func TestEveryDetailTabDraws(t *testing.T) {
	sizes := [][2]int{{1024, 768}, {1280, 800}, {2560, 1440}, {3840, 2160}}
	for _, sz := range sizes {
		for tab := detailTab(0); tab < dtCount; tab++ {
			a, img := newSizedApp(t, sz[0], sz[1])
			d := DemoServerDetail(a, int(tab))
			if d == nil {
				t.Fatal("detay açılmadı")
			}
			for i := range img.Pix {
				img.Pix[i] = 0
			}
			a.Draw()
			// Mürekkep YALNIZCA sekme gövdesinde sayılır (içerik sütunu,
			// sekme şeridinin altı): kenar çubuğu ve başlık her zaman
			// çizilir ve boş bir sekmeyi örterdi.
			// Panel çerçevesi de dışarıda kalır (tek başına ~1500 piksel).
			pad := a.ui.M.PadX
			body := image.Rect(a.sidebarWidth()+pad*3, sz[1]/5, sz[0]-pad*2, sz[1]-a.ui.StatusBarH()-pad*2)
			if ink := countForeground(img.SubImage(body).(*image.RGBA)); ink < 300 {
				t.Errorf("%dx%d %q sekme gövdesi neredeyse boş (%d piksel)", sz[0], sz[1], tab.Name(), ink)
			}
			if dir := shotDir(); dir != "" {
				writePNG(t, filepath.Join(dir, fmt.Sprintf("%dx%d-%02d-%s.png", sz[0], sz[1], int(tab), asciiName(tab.Name()))), img)
			}
		}
	}
}

// Çizim tıklama bölgelerini ekranın İÇİNDE kaydetmeli; sekme bölgesi yoksa
// fareyle sekme değiştirilemez.
func TestDetailRegistersTabZones(t *testing.T) {
	a, _ := newSizedApp(t, 1024, 768)
	DemoServerDetail(a, int(dtAccess))
	a.Draw()
	b := a.ui.Bounds()
	found := map[string]bool{}
	a.mu.Lock()
	for _, z := range a.zones {
		if z.kind == zoneShortcut && strings.HasPrefix(z.keyPress, "detail-") {
			found[z.keyPress] = true
			if !z.r.In(b) {
				t.Errorf("bölge %q ekran dışında: %v", z.keyPress, z.r)
			}
		}
	}
	a.mu.Unlock()
	if !found["detail-tab-8"] {
		t.Error("etkin sekmenin tıklama bölgesi yok")
	}
	if !found["detail-row-0"] {
		t.Error("satır tıklama bölgesi yok")
	}
}

// countForeground counts pixels that differ from the region's dominant colour.
//
// countInk burada işe yaramaz: panel yüzeyi (Surface) zaten "mürekkep"
// sayılıyor ve hiçbir şey çizmeyen bir sekme de testi geçiyordu (karşı
// sınamada görüldü). Baskın renk zemindir; ondan belirgin farklı her piksel
// metin, çizgi ya da şekildir.
func countForeground(img *image.RGBA) int {
	b := img.Bounds()
	hist := map[[3]uint8]int{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			hist[[3]uint8{c.R, c.G, c.B}]++
		}
	}
	var bg [3]uint8
	best := -1
	for k, n := range hist {
		if n > best {
			bg, best = k, n
		}
	}
	diff := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if diff(c.R, bg[0])+diff(c.G, bg[1])+diff(c.B, bg[2]) > 48 {
				n++
			}
		}
	}
	return n
}

func asciiName(s string) string {
	r := strings.NewReplacer("ı", "i", "İ", "I", "ş", "s", "ğ", "g", "ü", "u", "ö", "o", "ç", "c", " ", "-", "Ü", "U")
	return strings.ToLower(r.Replace(s))
}

func writePNG(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
