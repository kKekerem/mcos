package fbpanel

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"

	_ "time/tzdata"
)

// ════════════════════════════════════════════════════════════════════════════
// SUNUCU DETAYI › YEDEKLER › OTOMATİK YEDEK PLANI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "yedek alma saat/gün aralığını da ayarlayalım".

var backupTestIST = func() *time.Location {
	l, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		panic(err)
	}
	return l
}()

func backupAt(mo time.Month, day, h, m int) time.Time {
	return time.Date(2026, mo, day, h, m, 0, 0, backupTestIST)
}

// fixNow pins the panel clock.
func fixNow(t *testing.T, now time.Time) {
	t.Helper()
	old := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = old })
}

func tp(t time.Time) *time.Time { return &t }

// "Sonraki" metni: bugün / yarın / tarih; kaçırılan, süren, sorunlu yedek.
func TestYedekSonrakiEtiketi(t *testing.T) {
	now := backupAt(9, 27, 12, 0) // Pazar
	pol := model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}
	ny, _ := time.LoadLocation("America/New_York")
	for _, c := range []struct {
		name string
		res  ipc.BackupPolicyResult
		now  time.Time
		want string
	}{
		{"bugün", ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(9, 27, 16, 0))}, now, "bugün 16:00"},
		{"yarın", ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(9, 28, 4, 0))}, now, "yarın 04:00"},
		{"hafta içinde", ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(10, 2, 3, 30))}, now, "2 Ekim Cuma 03:30"},
		{"yıl dönümü", ipc.BackupPolicyResult{Policy: pol, Next: tp(time.Date(2027, 1, 2, 4, 0, 0, 0, backupTestIST))},
			now, "2 Ocak 2027 04:00"},
		{"kaçırılan yedek", ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(9, 27, 4, 0)), Due: true,
			Reason: "planlanan yedek kaçırıldı (27.09 04:00)"}, now, "şimdi — planlanan yedek kaçırıldı (27.09 04:00)"},
		{"ilk yedek", ipc.BackupPolicyResult{Policy: pol, Next: tp(now), Due: true, Reason: "henüz yedek yok"},
			now, "şimdi — henüz yedek yok"},
		// Sekme saatlerce açık kaldı: geçmiş bir saati "bugün 16:00" diye
		// yazmak yedeğin kaçtığını sandırırdı.
		{"eskimiş yanıt", ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(9, 27, 11, 0))}, now, "şimdi"},
		{"sürüyor", ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(9, 27, 16, 0)), Running: true}, now, "şu an alınıyor…"},
		{"bozuk saat", ipc.BackupPolicyResult{Policy: pol,
			Problem: "sistem saati yanlış (2010-01-01) — NTP eşitlemesi bekleniyor"}, now,
			"bekliyor — sistem saati yanlış (2010-01-01) — NTP eşitlemesi bekleniyor"},
		{"kapalı", ipc.BackupPolicyResult{Policy: model.BackupPolicy{Schedule: "6h"}, Next: tp(now)}, now, ""},
		// Plan DAEMON'un diliminde: İstanbul'da 28'i 05:00 iken New York'ta
		// hâlâ 27'si 22:00 — 23:30 "bugün"dür, "27 Eylül" değil.
		{"daemon dilimi", ipc.BackupPolicyResult{Policy: pol, Next: tp(time.Date(2026, 9, 27, 23, 30, 0, 0, ny))},
			backupAt(9, 28, 5, 0), "bugün 23:30"},
	} {
		if got := backupNextLabel(c.res, c.now); got != c.want {
			t.Errorf("%s: %q, %q bekleniyordu", c.name, got, c.want)
		}
	}
}

// openBackupsTab opens the demo detail on the Yedekler tab.
func openBackupsTab(t *testing.T, a *App, pol model.BackupPolicy) (*ServerDetail, *model.Server) {
	t.Helper()
	d := DemoServerDetail(a, int(dtBackups))
	if d == nil {
		t.Fatal("detay açılmadı")
	}
	a.patchServerBackup(d.id, pol)
	s := a.detailServer(d)
	if s == nil || s.Backup != pol {
		t.Fatalf("sunucu kaydı yamalanmadı: %+v", s)
	}
	return d, s
}

// Yedekler sekmesinin başında plan ve sonraki yedek yazmalı; plan satırı
// seçilebilir olmalı.
func TestYedekSekmesiPlanSatiri(t *testing.T) {
	a, _ := newTestApp(t)
	fixNow(t, backupAt(9, 27, 12, 0))
	pol := model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}
	d, s := openBackupsTab(t, a, pol)
	d.mu.Lock()
	d.backupPol = detailBackupPolicy{ok: true, res: ipc.BackupPolicyResult{Policy: pol, Next: tp(backupAt(9, 27, 16, 0))}}
	d.mu.Unlock()

	v := a.detailViewFor(d, s)
	want := [][2]string{{"Otomatik yedek", "Her 6 saatte bir · son 5 kopya"}, {"Sonraki yedek", "bugün 16:00"}}
	if fmt.Sprint(v.info) != fmt.Sprint(want) {
		t.Errorf("bilgi satırları %q, %q bekleniyordu", v.info, want)
	}
	if len(v.items) < 2 || v.items[1].act != "backup-policy" || v.items[1].label != "Otomatik yedek planı…" {
		t.Fatalf("plan satırı yok: %+v", v.items)
	}

	// Plan başka bir istemciden değişti, yanıt eski planın: "Sonraki"
	// eski planın saatini göstermemeli.
	a.patchServerBackup(d.id, model.BackupPolicy{Auto: true, Schedule: "1d@04:00", Keep: 10})
	s = a.detailServer(d)
	v = a.detailViewFor(d, s)
	want = [][2]string{{"Otomatik yedek", "Her gün 04:00 · son 10 kopya"}}
	if fmt.Sprint(v.info) != fmt.Sprint(want) {
		t.Errorf("eski planın yanıtı gösterildi: %q", v.info)
	}

	// Kapalı: yalnızca "Kapalı", "Sonraki" yok.
	a.patchServerBackup(d.id, model.BackupPolicy{Schedule: "6h", Keep: 5})
	s = a.detailServer(d)
	v = a.detailViewFor(d, s)
	if fmt.Sprint(v.info) != fmt.Sprint([][2]string{{"Otomatik yedek", "Kapalı"}}) {
		t.Errorf("kapalı plan: %q", v.info)
	}
}

// ── Seçim pencereleri (sahte daemon) ────────────────────────────────────────

type sahteYedekDaemon struct {
	mu    sync.Mutex
	pol   model.BackupPolicy
	calls []ipc.BackupSetPolicyParams
	fail  string // doluysa setPolicy bu mesajla reddedilir
}

func (f *sahteYedekDaemon) last() (ipc.BackupSetPolicyParams, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ipc.BackupSetPolicyParams{}, 0
	}
	return f.calls[len(f.calls)-1], len(f.calls)
}

func sahteYedekBaslat(t *testing.T, f *sahteYedekDaemon) *ipcclient.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dinleme: %v", err)
	}
	srv := ipc.NewServer(ln, nil)
	srv.Handle(ipc.MethodBackupList, func(context.Context, json.RawMessage) (any, error) {
		return ipc.BackupListResult{}, nil
	})
	result := func(id string) ipc.BackupPolicyResult {
		return ipc.BackupPolicyResult{ServerID: id, Policy: f.pol, Summary: model.BackupPolicyLabel(f.pol)}
	}
	srv.Handle(ipc.MethodBackupPolicy, func(_ context.Context, raw json.RawMessage) (any, error) {
		var p ipc.BackupPolicyParams
		_ = json.Unmarshal(raw, &p)
		f.mu.Lock()
		defer f.mu.Unlock()
		return result(p.ServerID), nil
	})
	srv.Handle(ipc.MethodBackupSetPolicy, func(_ context.Context, raw json.RawMessage) (any, error) {
		var p ipc.BackupSetPolicyParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, p)
		if f.fail != "" {
			return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: f.fail}
		}
		f.pol.Auto = p.Auto
		if p.Schedule != "" {
			f.pol.Schedule = p.Schedule
		}
		if p.Keep != nil {
			f.pol.Keep = *p.Keep
		}
		return result(p.ServerID), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(cancel)
	cl, err := ipcclient.Dial("tcp://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("bağlantı: %v", err)
	}
	return cl
}

// planApp opens the Yedekler tab connected to a fake daemon.
func planApp(t *testing.T, pol model.BackupPolicy) (*App, *ServerDetail, *sahteYedekDaemon) {
	t.Helper()
	a, _ := newTestApp(t)
	d, _ := openBackupsTab(t, a, pol)
	f := &sahteYedekDaemon{pol: pol}
	a.cl = sahteYedekBaslat(t, f)
	return a, d, f
}

// activeList returns the open list dialog.
func activeList(t *testing.T, a *App) *ListModal {
	t.Helper()
	m, ok := a.ActiveModal().(*ListModal)
	if !ok {
		t.Fatalf("liste penceresi açık değil: %T", a.ActiveModal())
	}
	return m
}

// pick selects the row with the label and presses Enter.
func pick(t *testing.T, a *App, label string) {
	t.Helper()
	m := activeList(t, a)
	for i, it := range m.items {
		if it.Label == label {
			m.SetCursor(i)
			a.Key("enter")
			return
		}
	}
	var labels []string
	for _, it := range m.items {
		labels = append(labels, it.Label)
	}
	t.Fatalf("%q penceresinde %q yok: %q", m.title, label, labels)
}

// cursorLabel is the row under the cursor of the open list.
func cursorLabel(t *testing.T, a *App) string {
	t.Helper()
	m := activeList(t, a)
	return m.items[m.Cursor()].Label
}

func currentLabels(t *testing.T, a *App) []string {
	t.Helper()
	var out []string
	for _, it := range activeList(t, a).items {
		if it.Current {
			out = append(out, it.Label)
		}
	}
	return out
}

// waitCall waits for the n-th setPolicy call.
func waitCall(t *testing.T, f *sahteYedekDaemon, n int) ipc.BackupSetPolicyParams {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, got := f.last()
		if got >= n {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("backup.setPolicy çağrılmadı (%d çağrı)", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitBackup waits until the local server copy carries pol.
func waitBackup(t *testing.T, a *App, d *ServerDetail, pol model.BackupPolicy) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s := a.detailServer(d); s != nil && s.Backup == pol {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("yerel sunucu kaydı %+v olmadı: %+v", pol, a.detailServer(d).Backup)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func keepOf(p ipc.BackupSetPolicyParams) string {
	if p.Keep == nil {
		return "nil"
	}
	return fmt.Sprint(*p.Keep)
}

// Hiç ayarlanmamış plan → gün aralığı → 04:00 → 10 kopya. İmleç her adımda
// önerilen değerde başlar (ilk satır en uç seçenek olurdu).
func TestYedekPlaniGunlukSaatli(t *testing.T) {
	a, d, f := planApp(t, model.BackupPolicy{})
	a.Key("o")
	if got := activeList(t, a).title; got != "Otomatik yedek" {
		t.Fatalf("o tuşu plan penceresini açmadı: %q", got)
	}
	if cur := currentLabels(t, a); fmt.Sprint(cur) != "[Kapalı]" {
		t.Errorf("şu anki mod %q, [Kapalı] bekleniyordu", cur)
	}
	pick(t, a, "Gün aralığıyla…")
	pick(t, a, "Her gün")
	if got := cursorLabel(t, a); got != "04:00" {
		t.Errorf("saat penceresi %q üstünde açıldı, 04:00 önerilmeli", got)
	}
	pick(t, a, "04:00")
	if got := cursorLabel(t, a); got != "Son 5 kopya" {
		t.Errorf("kopya penceresi %q üstünde açıldı, son 5 önerilmeli", got)
	}
	pick(t, a, "Son 10 kopya")

	p := waitCall(t, f, 1)
	if p.ServerID != d.id || !p.Auto || p.Schedule != "1d@04:00" || keepOf(p) != "10" {
		t.Fatalf("gönderilen plan %+v (keep %s)", p, keepOf(p))
	}
	pol := model.BackupPolicy{Auto: true, Schedule: "1d@04:00", Keep: 10}
	waitBackup(t, a, d, pol)
	if !hasEvent(a.Events(), "Otomatik yedek: Her gün 04:00 · son 10 kopya") {
		t.Errorf("onay olayı yok: %+v", a.Events())
	}
	if a.ActiveModal() != nil {
		t.Errorf("kayıttan sonra pencere açık kaldı: %T", a.ActiveModal())
	}

	// Yeniden açınca kayıtlı seçimler "şu anki" işaretli.
	a.Key("o")
	if cur := currentLabels(t, a); fmt.Sprint(cur) != "[Gün aralığıyla…]" {
		t.Errorf("şu anki mod %q", cur)
	}
	pick(t, a, "Gün aralığıyla…")
	if cur := currentLabels(t, a); fmt.Sprint(cur) != "[Her gün]" {
		t.Errorf("şu anki aralık %q", cur)
	}
	pick(t, a, "Her gün")
	if got := cursorLabel(t, a); got != "04:00" {
		t.Errorf("imleç kayıtlı saatte değil: %q", got)
	}
	pick(t, a, "04:00")
	if cur := currentLabels(t, a); fmt.Sprint(cur) != "[Son 10 kopya]" {
		t.Errorf("şu anki kopya %q", cur)
	}
	a.Key("esc")
	if _, n := f.last(); n != 1 {
		t.Errorf("Esc planı kaydetti (%d çağrı)", n)
	}
}

// Saat aralığı → 12 saat → sınırsız.
func TestYedekPlaniSaatlik(t *testing.T) {
	a, d, f := planApp(t, model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5})
	a.Key("o")
	pick(t, a, "Saat aralığıyla…")
	if cur := currentLabels(t, a); fmt.Sprint(cur) != "[Her 6 saatte bir]" {
		t.Errorf("şu anki aralık %q", cur)
	}
	var labels []string
	for _, it := range activeList(t, a).items {
		labels = append(labels, it.Label)
	}
	wantLabels := "[Her saat Her 2 saatte bir Her 3 saatte bir Her 4 saatte bir Her 6 saatte bir Her 8 saatte bir Her 12 saatte bir]"
	if fmt.Sprint(labels) != wantLabels {
		t.Errorf("saat seçenekleri %q", labels)
	}
	pick(t, a, "Her 12 saatte bir")
	pick(t, a, "Sınırsız kopya")
	p := waitCall(t, f, 1)
	if !p.Auto || p.Schedule != "12h" || keepOf(p) != "0" {
		t.Fatalf("gönderilen plan %+v (keep %s)", p, keepOf(p))
	}
	waitBackup(t, a, d, model.BackupPolicy{Auto: true, Schedule: "12h", Keep: 0})
}

// Kapatmak planı SİLMEZ (Schedule/Keep gönderilmez) ve eski planla yeniden
// açmak tek adım.
func TestYedekPlaniKapatVeYenidenAc(t *testing.T) {
	a, d, f := planApp(t, model.BackupPolicy{Auto: true, Schedule: "2d@03:00", Keep: 3})
	a.Key("o")
	pick(t, a, "Kapalı")
	p := waitCall(t, f, 1)
	if p.Auto || p.Schedule != "" || p.Keep != nil {
		t.Fatalf("kapatma isteği planı değiştiriyor: %+v", p)
	}
	waitBackup(t, a, d, model.BackupPolicy{Schedule: "2d@03:00", Keep: 3})

	a.Key("o")
	m := activeList(t, a)
	var resume *ListItem
	for i := range m.items {
		if m.items[i].Value == bpResume {
			resume = &m.items[i]
		}
	}
	if resume == nil || resume.Detail != "Her 2 günde bir 03:00 · son 3 kopya" {
		t.Fatalf("eski planla yeniden açma satırı yok ya da yanlış: %+v", resume)
	}
	pick(t, a, "Eski planla yeniden aç")
	p = waitCall(t, f, 2)
	if !p.Auto || p.Schedule != "" || p.Keep != nil {
		t.Fatalf("yeniden açma isteği %+v", p)
	}
	waitBackup(t, a, d, model.BackupPolicy{Auto: true, Schedule: "2d@03:00", Keep: 3})
}

// "Başka bir saat…": geçersiz saat pencereyi kapatmaz; 03:30 kabul edilir.
func TestYedekPlaniOzelSaat(t *testing.T) {
	a, d, f := planApp(t, model.BackupPolicy{})
	a.Key("o")
	pick(t, a, "Gün aralığıyla…")
	pick(t, a, "Her hafta")
	pick(t, a, "Başka bir saat…")
	tm, ok := a.ActiveModal().(*TextModal)
	if !ok {
		t.Fatalf("saat girişi açılmadı: %T", a.ActiveModal())
	}
	typeInto := func(s string) {
		a.Key("ctrl+u")
		for _, r := range s {
			a.Key(string(r))
		}
		a.Key("enter")
	}
	typeInto("25:00")
	if a.ActiveModal() != Modal(tm) || !strings.Contains(tm.errText, "00:00–23:59") {
		t.Fatalf("25:00 kabul edildi ya da hata yok: %T %q", a.ActiveModal(), tm.errText)
	}
	typeInto("3:30")
	pick(t, a, "Son 5 kopya")
	p := waitCall(t, f, 1)
	if p.Schedule != "7d@03:30" || keepOf(p) != "5" {
		t.Fatalf("gönderilen plan %+v (keep %s)", p, keepOf(p))
	}
	waitBackup(t, a, d, model.BackupPolicy{Auto: true, Schedule: "7d@03:30", Keep: 5})

	// Buçuklu saat listede "şu anki" olarak görünür.
	a.Key("o")
	pick(t, a, "Gün aralığıyla…")
	pick(t, a, "Her hafta")
	if got := cursorLabel(t, a); got != "03:30" {
		t.Errorf("imleç kayıtlı 03:30'da değil: %q", got)
	}
}

// Daemon reddederse Türkçe nedeni olay satırında görünür ve yerel kayıt
// DEĞİŞMEZ.
func TestYedekPlaniDaemonHatasi(t *testing.T) {
	pol := model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}
	a, d, f := planApp(t, pol)
	f.mu.Lock()
	f.fail = "saklanacak kopya sayısı 0 (sınırsız) ile 100 arasında olmalı (verilen: 200)"
	f.mu.Unlock()
	a.Key("o")
	pick(t, a, "Saat aralığıyla…")
	pick(t, a, "Her 2 saatte bir")
	pick(t, a, "Son 3 kopya")
	waitCall(t, f, 1)
	deadline := time.Now().Add(5 * time.Second)
	for !hasEvent(a.Events(), "otomatik yedek planı kaydedilemedi: saklanacak kopya sayısı") {
		if time.Now().After(deadline) {
			t.Fatalf("hata olayı yok: %+v", a.Events())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := a.detailServer(d); s.Backup != pol {
		t.Errorf("reddedilen plan yerel kayda yazıldı: %+v", s.Backup)
	}
}

// ── Ekran görüntüleri ve sığma ──────────────────────────────────────────────

func backupShotDir(t *testing.T) string {
	if dir := os.Getenv("MCOS_BACKUP_SHOTS"); dir != "" {
		return dir
	}
	return t.TempDir()
}

// kvValueFits reports whether value fits the value column of the tab's info
// lines UNTRUNCATED. Ölçüler çizimle AYNI: draw.go ana sütunu (kenar çubuğu +
// üç boşluk), Panel iç boşluğu (iki yan) ve drawKV'nin anahtar sütunu.
func kvValueFits(a *App, screenW int, keys []string, value string) bool {
	u := a.ui
	inW := screenW - u.M.PadX*5 - a.sidebarWidth()
	keyCols := 0
	for _, k := range keys {
		if n := len([]rune(k)); n > keyCols {
			keyCols = n
		}
	}
	keyW := (keyCols + 2) * u.F.CellW
	if keyW > inW/2 {
		keyW = inW / 2
	}
	return a.fit(value, inW-keyW) == value
}

// Yedekler sekmesi planla ve seçim pencereleriyle, üç çözünürlükte. En uzun
// metinler en dar ekranda KIRPILMADAN sığmalı; pencereler ListModal'ın 90
// sütunluk tavanına dayanmamalı (dayanan pencerenin metni taşar).
func TestYedekPlaniEkranGoruntusu(t *testing.T) {
	fixNow(t, backupAt(9, 27, 13, 40))
	longest := []string{
		model.BackupPolicyLabel(model.BackupPolicy{Auto: true, Schedule: "2d@23:59", Keep: 0}),
		model.BackupPolicyLabel(model.BackupPolicy{Auto: true, Schedule: "12h", Keep: 20}),
		"şimdi — planlanan yedek kaçırıldı (27.09 04:00)",
		"30 Eylül Çarşamba 04:00",
		"bekliyor — sistem saati yanlış (2010-01-01) — NTP eşitlemesi bekleniyor",
	}
	keys := []string{"Otomatik yedek", "Sonraki yedek"}
	for _, sz := range [][2]int{{1024, 768}, {1280, 800}, {3840, 2160}} {
		a, img := newSizedApp(t, sz[0], sz[1])
		pol := model.BackupPolicy{Auto: true, Schedule: "2d@04:00", Keep: 10}
		d, _ := openBackupsTab(t, a, pol)
		d.mu.Lock()
		d.backupPol = detailBackupPolicy{ok: true, res: ipc.BackupPolicyResult{Policy: pol,
			Next: tp(backupAt(9, 28, 4, 0))}}
		d.mu.Unlock()
		a.Draw()
		for _, v := range longest {
			if !kvValueFits(a, sz[0], keys, v) {
				t.Errorf("%dx%d: %q bilgi satırına sığmıyor (kırpılıyor)", sz[0], sz[1], v)
			}
		}
		pad := a.ui.M.PadX
		body := image.Rect(a.sidebarWidth()+pad*3, sz[1]/5, sz[0]-pad*2, sz[1]-a.ui.StatusBarH()-pad*2)
		if ink := countForeground(img.SubImage(body).(*image.RGBA)); ink < 300 {
			t.Errorf("%dx%d yedek sekmesi neredeyse boş (%d piksel)", sz[0], sz[1], ink)
		}
		writePNG(t, filepath.Join(backupShotDir(t), fmt.Sprintf("%dx%d-yedekler-plan.png", sz[0], sz[1])), img)
		if sz[0] != 1024 {
			continue
		}

		// Her pencereyi aç, genişliğini denetle; en dar ekranda PNG'leri yaz.
		a.cl = &ipcclient.Client{} // pencere açılabilsin (çevrimdışı değil)
		settle := func(name string) {
			if w, _ := activeList(t, a).Size(); w >= 90 {
				t.Errorf("%q penceresi genişlik tavanına dayandı (%d sütun): metin taşar", activeList(t, a).title, w)
			}
			for i := 0; i < 10; i++ {
				a.Draw()
				time.Sleep(40 * time.Millisecond)
			}
			a.Draw()
			writePNG(t, filepath.Join(backupShotDir(t), name), img)
		}
		a.Key("o")
		settle("1024x768-yedekler-ilk-pencere.png")
		pick(t, a, "Saat aralığıyla…")
		settle("1024x768-yedekler-saat-araligi.png")
		a.Key("esc")
		a.Key("o")
		pick(t, a, "Gün aralığıyla…")
		settle("1024x768-yedekler-gun-araligi.png")
		pick(t, a, "Her 2 günde bir")
		settle("1024x768-yedekler-saat-penceresi.png")
		pick(t, a, "04:00")
		settle("1024x768-yedekler-kopya-penceresi.png")
		a.Key("esc")
	}
}
