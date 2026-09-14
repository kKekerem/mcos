package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mcos/internal/java"
	mlog "mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/server"
	"mcos/internal/store"
	"mcos/internal/supervisor"
)

// ════════════════════════════════════════════════════════════════════════════
// DÜĞÜM, EŞTEN GELEN KURULUMU DOĞRU UYGULAMALI
// ════════════════════════════════════════════════════════════════════════════
//
// Bu program kullanıcının günlük kullandığı bilgisayarda çalışır ve MCOS'tan
// gelen bir istekle orada gerçek bir Minecraft sunucusu kurar. Yanlış bir alan
// (eksik Java sürümü, yanlış port) sunucunun hiç açılmamasına yol açar ve
// kullanıcı bunu ancak oyuna giremediğinde fark eder.
//
// Testler AĞA ÇIKMAZ: kurulum adımı değiştirilebilir bir alandır (installFn),
// böylece ApplyLinkSpec'in KARARLARI indirme yapılmadan sınanabilir.

func newTestHost(t *testing.T) (*nodeHost, *[]string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	lg := mlog.New(os.NewFile(0, os.DevNull), mlog.LevelError, 8)
	sup := supervisor.New(lg)
	jm := java.NewManager(st, lg)
	sm := server.NewManager(st, jm, sup, lg)

	var mu sync.Mutex
	installed := []string{}
	h := &nodeHost{
		st:          st,
		servers:     sm,
		log:         lg,
		budgetRAMMB: 4096,
		budgetCPU:   75,
	}
	h.installFn = func(srv *model.Server) {
		mu.Lock()
		installed = append(installed, srv.ID)
		mu.Unlock()
	}
	return h, &installed
}

func baseSpec() model.LinkSpec {
	return model.LinkSpec{
		Mode:       model.LinkSharedWorld,
		ServerName: "Ortak Dünya",
		Software:   "fabric",
		MCVersion:  "1.21.1",
		Seed:       "12345",
		Difficulty: model.LinkDifficulty("normal"),
		RAMMB:      8192, // bütçemizden BÜYÜK: kırpılmalı
		Port:       25565,
		SlabChunks: 32,
		Origin:     "mcos-host",
	}
}

// TestApplyLinkSpecCreatesUsableServer — eşten gelen kurulum çalışır olmalı.
func TestApplyLinkSpecCreatesUsableServer(t *testing.T) {
	h, _ := newTestHost(t)

	msg, port, err := h.ApplyLinkSpec(baseSpec())
	if err != nil {
		t.Fatalf("kurulum reddedildi: %v", err)
	}
	if msg == "" {
		t.Error("boş mesaj döndü")
	}
	if port != 25565 {
		t.Errorf("bildirilen port %d, 25565 bekleniyordu", port)
	}

	list, err := h.st.ListServers()
	if err != nil || len(list) != 1 {
		t.Fatalf("sunucu kaydı oluşmadı: %v (%d kayıt)", err, len(list))
	}
	srv := list[0]

	// ── JavaMajor ───────────────────────────────────────────────────────
	// Yakalanan gerçek hata: bu alan hiç atanmıyordu ve 0 kalıyordu. Sunucu
	// kuruluyor ama BAŞLATILAMIYORDU: java "0" diye bir sürüm aranıyordu.
	want := java.RequiredJavaMajor("1.21.1")
	if srv.JavaMajor != want {
		t.Errorf("JavaMajor %d, %d bekleniyordu — sunucu başlayamaz",
			srv.JavaMajor, want)
	}
	if srv.JavaMajor == 0 {
		t.Error("JavaMajor sıfır — bu sunucu hiç açılamaz")
	}

	if srv.MCVersion != "1.21.1" || srv.Software != model.Software("fabric") {
		t.Errorf("yazılım/sürüm yanlış: %s %s", srv.Software, srv.MCVersion)
	}
	if srv.LevelSeed != "12345" {
		t.Errorf("tohum taşınmadı: %q — dünya AYNI olmazdı", srv.LevelSeed)
	}
	if srv.Link.Mode != model.LinkSharedWorld {
		t.Errorf("ortak dünya kipi kurulmadı: %q", srv.Link.Mode)
	}
	if srv.Link.SlabChunks != 32 {
		t.Errorf("dilim genişliği taşınmadı: %d", srv.Link.SlabChunks)
	}

	// Bütçe: eşin önerisi bizim sınırımızı aşamaz.
	if srv.RAMMB > 4096 {
		t.Errorf("eşin önerdiği %d MB bütçeyi aştı (%d MB)", 8192, srv.RAMMB)
	}

	// Türetilmiş alanlar hesaplanmış olmalı.
	if !srv.SupportsMods {
		t.Error("fabric için SupportsMods yanlış")
	}
}

// Port doluysa BAŞKA bir port seçilmeli VE bu geri bildirilmeli.
//
// ── Neden kritik ────────────────────────────────────────────────────────────
// Karşı taraf oyuncuyu aktarırken bu portu kullanır. Bildirilmezse oyuncu
// yanlış (ya da var olmayan) bir sunucuya gönderilir.
func TestRelocatedPortIsReportedBack(t *testing.T) {
	h, _ := newTestHost(t)

	// 25565'i başka bir sunucu tutsun.
	occupied := &model.Server{
		ID: "onceki", Name: "Baska Sunucu", Port: 25565,
		Software: model.Software("paper"), MCVersion: "1.20.1",
	}
	if err := h.st.SaveServer(occupied); err != nil {
		t.Fatal(err)
	}

	spec := baseSpec()
	spec.ServerName = "Ortak Dünya" // farklı ad: yeni sunucu oluşturulmalı
	_, port, err := h.ApplyLinkSpec(spec)
	if err != nil {
		t.Fatalf("kurulum reddedildi: %v", err)
	}
	if port == 25565 {
		t.Fatal("dolu port yeniden kullanıldı")
	}
	if port == 0 {
		t.Fatal("port bildirilmedi — eş oyuncuyu nereye göndereceğini bilemez")
	}

	// Bildirilen port GERÇEKTEN kaydedilmiş olmalı.
	list, _ := h.st.ListServers()
	found := false
	for _, s := range list {
		if s.Link.Mode == model.LinkSharedWorld {
			found = true
			if s.Port != port {
				t.Errorf("bildirilen port %d, kaydedilen %d — uyuşmuyor",
					port, s.Port)
			}
		}
	}
	if !found {
		t.Error("ortak dünya sunucusu bulunamadı")
	}
}

// Eş aynı kurulumu yeniden gönderirse dünya SİLİNMEMELİ.
func TestReapplyDoesNotRecreateWorld(t *testing.T) {
	h, _ := newTestHost(t)

	if _, _, err := h.ApplyLinkSpec(baseSpec()); err != nil {
		t.Fatal(err)
	}
	first, _ := h.st.ListServers()
	if len(first) != 1 {
		t.Fatalf("%d sunucu", len(first))
	}
	id := first[0].ID

	if _, _, err := h.ApplyLinkSpec(baseSpec()); err != nil {
		t.Fatal(err)
	}
	second, _ := h.st.ListServers()
	if len(second) != 1 {
		t.Fatalf("ikinci gönderim %d sunucu bıraktı — dünya ikizlendi", len(second))
	}
	if second[0].ID != id {
		t.Error("sunucu yeniden oluşturuldu — dünya verisi kaybolurdu")
	}
}

// Ortak dünya kapatıldığında sunucu SİLİNMEMELİ, yalnızca kip düşmeli.
func TestDisableKeepsTheWorld(t *testing.T) {
	h, _ := newTestHost(t)
	if _, _, err := h.ApplyLinkSpec(baseSpec()); err != nil {
		t.Fatal(err)
	}

	off := baseSpec()
	off.Mode = model.LinkOff
	if _, _, err := h.ApplyLinkSpec(off); err != nil {
		t.Fatalf("kapatma başarısız: %v", err)
	}

	list, _ := h.st.ListServers()
	if len(list) != 1 {
		t.Fatalf("kapatma sunucuyu sildi (%d kayıt) — dünya kullanıcının verisidir",
			len(list))
	}
	if list[0].Link.Mode == model.LinkSharedWorld {
		t.Error("kip düşmedi")
	}
}

// ── Bütçe ───────────────────────────────────────────────────────────────────

func TestBudgetClamp(t *testing.T) {
	h := &nodeHost{budgetRAMMB: 4096, budgetCPU: 75}
	cases := []struct{ in, want int }{
		{8192, 4096}, // bütçeyi aşan öneri kırpılır
		{2048, 2048}, // sığan öneri korunur
		{0, 4096},    // öneri yoksa bütçe
		{-5, 4096},   // saçma değer
	}
	for _, c := range cases {
		if got, _ := h.clamp(c.in, 0); got != c.want {
			t.Errorf("clamp(%d) = %d, %d bekleniyordu", c.in, got, c.want)
		}
	}
}

// budgetRAM hiçbir zaman makineyi kullanılamaz hâle getirecek kadar
// büyük ya da sunucuyu çalıştıramayacak kadar küçük olmamalı.
func TestBudgetRAMStaysSane(t *testing.T) {
	if got := budgetRAM(2048); got != 2048 {
		t.Errorf("kullanıcının verdiği değer ezildi: %d", got)
	}
	auto := budgetRAM(0)
	if auto < 1024 {
		t.Errorf("otomatik bütçe %d MB — sunucu açılamaz", auto)
	}
	if auto > 8192 {
		t.Errorf("otomatik bütçe %d MB — kullanıcının makinesi kilitlenir", auto)
	}
}

// ── Ayarlar ─────────────────────────────────────────────────────────────────

// Anahtar bir sırdır: dosya başka kullanıcılar tarafından okunamamalı.
func TestSettingsAreWrittenPrivately(t *testing.T) {
	dir := t.TempDir()
	want := nodeSettings{Key: "0123456789abcdef", Name: "salon-pc", RAMMB: 2048}
	if err := saveSettings(dir, want); err != nil {
		t.Fatal(err)
	}

	got, err := loadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ayarlar değişti: %+v -> %+v", want, got)
	}

	info, err := os.Stat(settingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("ayar dosyası izinleri %o — eşleştirme anahtarı okunabilir", perm)
	}
}

// Bozuk bir ayar dosyası programı ENGELLEMEMELİ: kullanıcı anahtarı yeniden
// girer, ama program açılmalı.
func TestCorruptSettingsDoNotBlockStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(settingsPath(dir), []byte("{bozuk"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(dir)
	if err != nil {
		t.Fatalf("bozuk dosya hata verdi: %v", err)
	}
	if got.Key != "" {
		t.Error("bozuk dosyadan anahtar üretildi")
	}
}

// Vanilla ne mod ne eklenti yükler: ortak dünya orada ÇALIŞAMAZ ve bunu
// sessizce geçmek yerine açıkça söylemeliyiz.
func TestVanillaCannotTakeTheMod(t *testing.T) {
	if _, _, ok := linkArtifact(model.Software("vanilla")); ok {
		t.Error("vanilla için bir yükleme yeri bildirildi — böyle bir yer yok")
	}
	for _, sw := range []string{"fabric", "quilt", "paper", "purpur"} {
		if _, _, ok := linkArtifact(model.Software(sw)); !ok {
			t.Errorf("%s desteklenmiyor sayıldı", sw)
		}
	}
}

// ── Mod ─────────────────────────────────────────────────────────────────────

// Mod, yazılıma göre DOĞRU klasöre kurulmalı: Fabric mods/, Paper plugins/.
func TestLinkModGoesToTheRightFolder(t *testing.T) {
	h, _ := newTestHost(t)

	tmp := t.TempDir()
	modJar := filepath.Join(tmp, linkModName)
	pluginJar := filepath.Join(tmp, linkPluginName)
	for _, p := range []string{modJar, pluginJar} {
		if err := os.WriteFile(p, []byte("PK\x03\x04 sahte jar"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MCOS_NODE_MOD", modJar)
	t.Setenv("MCOS_NODE_PLUGIN", pluginJar)

	cases := []struct {
		software string
		wantDir  string
		wantFile string
	}{
		{"fabric", "mods", linkModName},
		{"paper", "plugins", linkPluginName},
	}
	for _, c := range cases {
		dataDir := t.TempDir()
		srv := &model.Server{
			ID: "x" + c.software, Name: "s", DataDir: dataDir,
			Software: model.Software(c.software), MCVersion: "1.21.1",
		}
		if err := h.installLinkMod(srv); err != nil {
			t.Fatalf("%s: mod kurulamadı: %v", c.software, err)
		}
		want := filepath.Join(dataDir, c.wantDir, c.wantFile)
		if _, err := os.Stat(want); err != nil {
			t.Errorf("%s: %s dosyası %s içinde değil — yükleyici onu bulamaz",
				c.software, c.wantFile, c.wantDir)
		}
	}
}
