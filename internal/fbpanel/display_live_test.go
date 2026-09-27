package fbpanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcos/internal/drm"
)

// sahteEkran is a multi-mode display (bochs/virtio/i915 gibi).
type sahteEkran struct {
	modlar  []drm.Mode
	cur     drm.Mode
	saved   string
	setler  []string
	checked int
}

func (s *sahteEkran) Size() (int, int) { return s.cur.Width, s.cur.Height }
func (s *sahteEkran) FramePeriod() time.Duration {
	return time.Second * 1000 / time.Duration(s.cur.MilliHz)
}
func (s *sahteEkran) Check() bool       { s.checked++; return false }
func (s *sahteEkran) SetSaved(k string) { s.saved = k }
func (s *sahteEkran) SetMode(k string) error {
	m, ok := drm.Find(s.modlar, k)
	if !ok {
		return os.ErrNotExist
	}
	s.cur = m
	s.setler = append(s.setler, k)
	return nil
}
func (s *sahteEkran) Info() drm.Info {
	return drm.Info{Backend: "drm", Driver: "i915", Connector: "HDMI-A-1", ConnType: 11,
		Current: s.cur, Modes: s.modlar, Changeable: len(s.modlar) > 1,
		Method: "sayfa çevirme (dikey boşluğa kilitli)"}
}

func yeniSahteEkran() *sahteEkran {
	ms := []drm.Mode{
		{Width: 2560, Height: 1440, MilliHz: 143912, Refresh: 143},
		{Width: 2560, Height: 1440, MilliHz: 59951, Refresh: 59, Preferred: true},
		{Width: 1920, Height: 1080, MilliHz: 60000, Refresh: 60},
		{Width: 1280, Height: 720, MilliHz: 60000, Refresh: 60},
	}
	return &sahteEkran{modlar: ms, cur: ms[0]}
}

func gecelDosya(t *testing.T) string {
	t.Helper()
	eski := displayConfPath
	displayConfPath = filepath.Join(t.TempDir(), "display.conf")
	t.Cleanup(func() { displayConfPath = eski })
	return displayConfPath
}

func sahteSaat(t *testing.T) *time.Time {
	t.Helper()
	simdi := time.Unix(1_700_000_000, 0)
	eski := nowFunc
	nowFunc = func() time.Time { return simdi }
	t.Cleanup(func() { nowFunc = eski })
	return &simdi
}

// Ekran bölümü gerçek modları listelemeli: "Otomatik" + her mod.
func TestEkranBolumuGercekModlariListeler(t *testing.T) {
	a, _ := newTestApp(t)
	e := yeniSahteEkran()
	a.SetLiveDisplay(e)
	if got := a.displayRowCount(); got != 1+len(e.modlar) {
		t.Fatalf("satır sayısı %d, %d bekleniyordu", got, 1+len(e.modlar))
	}
}

// Seçim ANINDA uygulanır, onay gelmezse 15 sn sonra ESKİ moda dönülür.
func TestModOnaysizGeriAlinir(t *testing.T) {
	a, _ := newTestApp(t)
	gecelDosya(t)
	saat := sahteSaat(t)
	e := yeniSahteEkran()
	a.SetLiveDisplay(e)

	a.applyDisplayRow(3) // 1920x1080@60 (0: Otomatik)
	if e.cur.Width != 1920 {
		t.Fatalf("mod anında uygulanmadı: %s", e.cur)
	}
	if _, ok := a.ActiveModal().(*modeConfirmModal); !ok {
		t.Fatal("onay penceresi açılmadı")
	}
	*saat = saat.Add(14 * time.Second)
	a.displayTick()
	if e.cur.Width != 1920 {
		t.Fatal("süre dolmadan geri alındı")
	}
	*saat = saat.Add(2 * time.Second)
	a.displayTick()
	if e.cur.Width != 2560 || e.cur.MilliHz != 143912 {
		t.Fatalf("süre dolunca önceki moda dönülmedi: %s", e.cur)
	}
	if a.ActiveModal() != nil {
		t.Fatal("geri almadan sonra pencere açık kaldı")
	}
}

// "Koru" modu kalıcı yapar: display.conf'a yazılır, diğer satırlar korunur.
func TestKorunanModKaydedilir(t *testing.T) {
	a, _ := newTestApp(t)
	yol := gecelDosya(t)
	sahteSaat(t)
	if err := os.WriteFile(yol, []byte("gfxmode=1920x1080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := yeniSahteEkran()
	a.SetLiveDisplay(e)
	a.applyDisplayRow(3)
	a.Key("enter") // odak "Koru"da
	b, _ := os.ReadFile(yol)
	if !strings.Contains(string(b), "mode=1920x1080@60000") {
		t.Fatalf("mod kaydedilmedi: %q", b)
	}
	if !strings.Contains(string(b), "gfxmode=1920x1080") {
		t.Fatalf("mcos-display'in satırı silindi: %q", b)
	}
	if e.saved != "1920x1080@60000" {
		t.Fatalf("ekrana tercih bildirilmedi: %q", e.saved)
	}
	if got := ReadDisplayMode(); got != "1920x1080@60000" {
		t.Fatalf("ReadDisplayMode = %q", got)
	}
}

// Mod değişince panel yeni boyutta kurulmalı ve HER bölüm çizilebilmeli.
func TestModDegisinceHerBolumCizilir(t *testing.T) {
	a, _ := newTestApp(t)
	for _, boy := range [][2]int{{1024, 768}, {2560, 1440}, {3840, 2160}} {
		c := a.resizeCanvas(boy[0], boy[1])
		if c.Bounds().Dx() != boy[0] || a.ui.Bounds().Dx() != boy[0] {
			t.Fatalf("%dx%d: tuval/arayüz yeni boyutta değil", boy[0], boy[1])
		}
		for sec := Section(0); sec < secCount; sec++ {
			a.gotoSection(sec)
			a.Draw()
		}
		// Çizim gerçekten tuvale inmiş olmalı (boş tuval = kurulum bozuk).
		dolu := false
		for _, px := range c.Pix[:len(c.Pix)/2] {
			if px != 0 {
				dolu = true
				break
			}
		}
		if !dolu {
			t.Fatalf("%dx%d: yeni tuvale hiçbir şey çizilmedi", boy[0], boy[1])
		}
	}
}

// Ekran kartı tek mod sunuyorsa (simpledrm) canlı liste DEĞİL, GRUB yolu.
func TestTekModdaGrubYolu(t *testing.T) {
	a, _ := newTestApp(t)
	e := yeniSahteEkran()
	e.modlar = e.modlar[:1]
	a.SetLiveDisplay(e)
	if _, ok := a.liveSelectable(); ok {
		t.Fatal("tek modlu ekranda canlı liste seçilebilir göründü")
	}
	if got := a.displayRowCount(); got != len(displayModes) {
		t.Fatalf("GRUB listesi bekleniyordu, %d satır", got)
	}
	a.gotoSection(SecDisplay)
	a.Draw() // panik olmamalı
}
