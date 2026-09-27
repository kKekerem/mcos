package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/cluster"
)

// ════════════════════════════════════════════════════════════════════════════
// KODLA EŞLEŞTİRME: TEKLİF DURUMA, KARAR DOSYAYLA DÜĞÜME
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "eşleştirme anahtarını elle girme gerekmesin". Teklif arka
// plandaki düğüm sürecinde, "Kabul et" düğmesi ise pencere sürecinde; ikisi
// arasındaki köprü durum.json (teklifler) ve karar dosyasıdır. Ağ tarafı
// internal/cluster/pairoffer_test.go'da; burada köprü sınanır.

// fakeOffers stands in for cluster.Manager (Offers/DecideOffer).
type fakeOffers struct {
	mu        sync.Mutex
	list      []cluster.InOffer
	decisions []string // "id=kabul" / "id=red"
}

func (f *fakeOffers) Offers() []cluster.InOffer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cluster.InOffer(nil), f.list...)
}

func (f *fakeOffers) DecideOffer(id string, accept bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ID == id {
			f.list[i].Accepted, f.list[i].Rejected = accept, !accept
			if accept {
				f.decisions = append(f.decisions, id+"=kabul")
			} else {
				f.decisions = append(f.decisions, id+"=red")
			}
			return nil
		}
	}
	return os.ErrNotExist
}

func (f *fakeOffers) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.decisions...)
}

func (f *fakeOffers) drop(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ID == id {
			f.list = append(f.list[:i], f.list[i+1:]...)
			return
		}
	}
}

func testOffer() cluster.InOffer {
	return cluster.InOffer{ID: "Ab3_-x9Qz", Name: "mcos-kutu", IP: "192.168.1.20",
		Code: "482 913", At: time.Now()}
}

func TestKararDosyasiKabulEder(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	tr := newPairTracker(dir, src, nil)
	if !tr.tick() {
		t.Fatal("yeni teklif 'değişti' saymadı (durum hemen yazılmazdı)")
	}
	if err := writePairDecision(dir, "Ab3_-x9Qz", "482913", true); err != nil {
		t.Fatal(err)
	}
	tr.tick()
	if got := src.got(); len(got) != 1 || got[0] != "Ab3_-x9Qz=kabul" {
		t.Fatalf("karar uygulanmadı: %v", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.karar")); len(left) != 0 {
		t.Fatalf("karar dosyası silinmedi: %v", left)
	}
	if e := tr.lastEvent(); e == nil || !e.OK || !strings.Contains(e.Msg, "Kodlar aynı") {
		t.Fatalf("olay iletisi MCOS'taki adımı söylemiyor: %+v", e)
	}
	offs := tr.offers()
	if len(offs) != 1 || !offs[0].Accepted || offs[0].LeftSec < 170 {
		t.Fatalf("durumdaki teklif: %+v", offs)
	}
}

// Kullanıcı ekranda eski bir kodu onayladıysa (teklif bu arada yenilendi)
// karar UYGULANMAMALI: kodu karşılaştırmanın bütün anlamı bu.
func TestKararEskiKodlaUygulanmaz(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	tr := newPairTracker(dir, src, nil)
	if err := writePairDecision(dir, "Ab3_-x9Qz", "111 222", true); err != nil {
		t.Fatal(err)
	}
	tr.tick()
	if got := src.got(); len(got) != 0 {
		t.Fatalf("eski kodlu karar uygulandı: %v", got)
	}
	if e := tr.lastEvent(); e == nil || e.OK {
		t.Fatalf("kullanıcıya kodun değiştiği söylenmedi: %+v", e)
	}
}

// Kimlik dosya adına girer: yol ayırıcılı bir kimlik klasör dışına
// yazdırmamalı; adı ile içeriği tutmayan bir dosya da uygulanmamalı.
func TestKararKimligiDenetlenir(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", "../dışarı", `a\b`, "a/b", strings.Repeat("x", 40)} {
		if err := writePairDecision(dir, id, "482913", true); err == nil {
			t.Fatalf("geçersiz kimlik kabul edildi: %q", id)
		}
	}
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	tr := newPairTracker(dir, src, nil)
	// Başka bir teklifin dosyasına bu teklifin kararı yazılmış.
	if err := os.WriteFile(filepath.Join(dir, "esleme-baska.karar"),
		[]byte(`{"id":"Ab3_-x9Qz","kod":"482913","kabul":true,"zaman":"`+
			time.Now().Format(time.RFC3339Nano)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tr.tick()
	if got := src.got(); len(got) != 0 {
		t.Fatalf("adı tutmayan karar dosyası uygulandı: %v", got)
	}
}

// Düğüm kapalıyken kalmış eski bir karar, sonradan gelen bir teklifi
// kabul etmemeli.
func TestEskiKararAtilir(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	tr := newPairTracker(dir, src, nil)
	b := []byte(`{"id":"Ab3_-x9Qz","kod":"482913","kabul":true,"zaman":"` +
		time.Now().Add(-cluster.PairOfferTTL-time.Minute).Format(time.RFC3339Nano) + `"}`)
	if err := os.WriteFile(filepath.Join(dir, decisionFileName("Ab3_-x9Qz")), b, 0o600); err != nil {
		t.Fatal(err)
	}
	tr.tick()
	if got := src.got(); len(got) != 0 {
		t.Fatalf("süresi geçmiş karar uygulandı: %v", got)
	}
}

// Gelen anahtar KALICI olmalı: düğüm yeniden açıldığında (oturum açılışı)
// aynı anahtarla başlamazsa eşleşme her yeniden başlatmada kaybolurdu.
func TestGelenAnahtarKaydedilir(t *testing.T) {
	dir := t.TempDir()
	_ = saveSettings(dir, nodeSettings{Name: "Salon PC", RAMMB: 4096})
	tr := newPairTracker(dir, &fakeOffers{}, nil)
	if err := tr.keyReceived("kume-anahtari-0123456789", testOffer()); err != nil {
		t.Fatal(err)
	}
	s, _ := loadSettings(dir)
	if s.Key != "kume-anahtari-0123456789" || s.Name != "Salon PC" || s.RAMMB != 4096 {
		t.Fatalf("ayarlar: %+v (anahtar yazılmadı ya da diğer ayarlar ezildi)", s)
	}
	if st, err := os.Stat(settingsPath(dir)); err == nil && st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("anahtar dosyası başkalarınca okunabilir: %v", st.Mode())
	}
	if e := tr.lastEvent(); e == nil || !e.OK || !strings.Contains(e.Msg, "mcos-kutu ile eşleşildi") {
		t.Fatalf("olay: %+v", e)
	}
}

// fakeRunningNode writes durum.json from a tracker, like serve() does.
func fakeRunningNode(t *testing.T, dir string, src *fakeOffers, keySet func() bool) *pairTracker {
	t.Helper()
	tr := newPairTracker(dir, src, nil)
	ctx, cancel := context.WithCancel(context.Background())
	kick := make(chan struct{}, 1)
	done := make(chan struct{})
	go tr.loop(ctx, kick)
	go func() {
		defer close(done)
		statusLoop(ctx, dir, func() nodeStatusFile {
			return nodeStatusFile{PID: 1, Name: "sınama", Updated: time.Now(),
				Offers: tr.offers(), PairEvent: tr.lastEvent(), KeySet: keySet()}
		}, kick)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return tr
}

func waitUntil(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", what)
}

// "mcos-node --kabul 482913": penceresiz (systemd, SSH) düğümde kabul yolu.
func TestKomutSatirindanKabul(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	var mu sync.Mutex
	key := ""
	tr := fakeRunningNode(t, dir, src, func() bool { mu.Lock(); defer mu.Unlock(); return key != "" })
	waitUntil(t, "teklif durum.json'a yazılsın", func() bool {
		st, ok := readStatus(dir)
		return ok && len(st.Offers) == 1
	})

	// MCOS tarafı: kabul görülünce anahtarı gönderir (cluster handlePairKey
	// SetSecret + onKey çağırır ve teklifi siler).
	go func() {
		for i := 0; i < 100; i++ {
			if len(src.got()) > 0 {
				time.Sleep(200 * time.Millisecond)
				mu.Lock()
				key = "kume-anahtari-0123456789"
				mu.Unlock()
				_ = tr.keyReceived(key, testOffer())
				src.drop("Ab3_-x9Qz")
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	var out bytes.Buffer
	if rc := cliDecide(dir, "482913", true, &out, 10*time.Second); rc != 0 {
		t.Fatalf("--kabul çıkış kodu %d:\n%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "Kodlar aynı, onayla") || !strings.Contains(out.String(), "eşleşildi") {
		t.Fatalf("--kabul çıktısı adımı ve sonucu söylemiyor:\n%s", out.String())
	}
	if s, _ := loadSettings(dir); s.Key != "kume-anahtari-0123456789" {
		t.Fatalf("anahtar kaydedilmedi: %+v", s)
	}
}

func TestKomutSatirindanYanlisKod(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	fakeRunningNode(t, dir, src, func() bool { return false })
	waitUntil(t, "teklif durum.json'a yazılsın", func() bool {
		st, ok := readStatus(dir)
		return ok && len(st.Offers) == 1
	})
	var out bytes.Buffer
	if rc := cliDecide(dir, "000000", true, &out, time.Second); rc == 0 {
		t.Fatal("yanlış kodla --kabul başarılı döndü")
	}
	if !strings.Contains(out.String(), "482 913") {
		t.Fatalf("bekleyen gerçek istek listelenmedi:\n%s", out.String())
	}
	if got := src.got(); len(got) != 0 {
		t.Fatalf("yanlış kodla karar uygulandı: %v", got)
	}
	out.Reset()
	if rc := cliDecide(dir, "482 913", false, &out, 0); rc != 0 {
		t.Fatalf("--reddet çıkış kodu %d:\n%s", rc, out.String())
	}
	if got := src.got(); len(got) != 1 || got[0] != "Ab3_-x9Qz=red" {
		t.Fatalf("red uygulanmadı: %v", got)
	}
}

// Konsol ekranında "K" + Enter en yeni isteği kabul eder.
func TestKonsoldanKabul(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	fakeRunningNode(t, dir, src, func() bool { return false })
	waitUntil(t, "teklif durum.json'a yazılsın", func() bool {
		st, ok := readStatus(dir)
		return ok && len(st.Offers) == 1
	})
	var said []string
	consoleKeys(dir, strings.NewReader("merhaba\nk\n"), func(s string) { said = append(said, s) })
	waitUntil(t, "K kararı uygulansın", func() bool { return len(src.got()) == 1 })
	if len(said) != 1 || !strings.Contains(said[0], "kabul edildi") {
		t.Fatalf("konsol iletisi: %v", said)
	}
}

// ── Pencere: "Kabul et" düğmesi ─────────────────────────────────────────────

func TestGUIKabulDugmesi(t *testing.T) {
	r := newGUIRig(t, true)
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	fakeRunningNode(t, r.o.dataRoot, src, func() bool { return false })
	waitUntil(t, "pencere teklifi görsün", func() bool {
		st := r.g.state()
		return st.Node != nil && len(st.Node.Offers) == 1
	})

	// Belirteçsiz: ağdaki ya da tarayıcıdaki bir sayfa eşleşmeyi kabul
	// ettiremez.
	if code, _, _ := r.call(t, http.MethodPost, "/api/karar", `{"id":"Ab3_-x9Qz","kod":"482 913","kabul":true}`, false); code != http.StatusForbidden {
		t.Fatalf("belirteçsiz karar: kod %d", code)
	}
	// Ekrandaki kod eski: uygulanmaz.
	if code, v, _ := r.call(t, http.MethodPost, "/api/karar", `{"id":"Ab3_-x9Qz","kod":"111 222","kabul":true}`, true); code != http.StatusConflict {
		t.Fatalf("eski kodlu karar: kod %d %v", code, v)
	}
	if code, v, _ := r.call(t, http.MethodPost, "/api/karar", `{"id":"yok","kod":"482 913","kabul":true}`, true); code != http.StatusGone {
		t.Fatalf("olmayan teklif: kod %d %v", code, v)
	}
	if got := src.got(); len(got) != 0 {
		t.Fatalf("reddedilmesi gereken istekler karar uyguladı: %v", got)
	}
	code, v, _ := r.call(t, http.MethodPost, "/api/karar", `{"id":"Ab3_-x9Qz","kod":"482 913","kabul":true}`, true)
	if code != http.StatusOK || !strings.Contains(v["ileti"].(string), "Kodlar aynı") {
		t.Fatalf("kabul: kod %d %v", code, v)
	}
	if got := src.got(); len(got) != 1 || got[0] != "Ab3_-x9Qz=kabul" {
		t.Fatalf("kabul düğüme ulaşmadı: %v", got)
	}
}

// Anahtarsız pencere düğümü BAŞLATIR: eskiden "önce anahtarı girin" deyip
// hiçbir şey başlatmıyordu ve MCOS taramada bu PC'yi bulamıyordu.
func TestGUIAnahtarsizBaslatir(t *testing.T) {
	r := newGUIRig(t, true)
	code, v, _ := r.call(t, http.MethodPost, "/api/baslat", `{}`, true)
	if code != http.StatusOK {
		t.Fatalf("anahtarsız başlat: kod %d %v", code, v)
	}
	st := r.idle(t)
	if st.Err != "" || !st.Running {
		t.Fatalf("anahtarsız düğüm başlamadı: hata=%q çalışıyor=%v", st.Err, st.Running)
	}
	r.f.mu.Lock()
	n := len(r.f.startExe)
	r.f.mu.Unlock()
	if n != 1 {
		t.Fatalf("arka plan başlatması %d kez çağrıldı", n)
	}
}

// Reddedilen teklif, MCOS reddi öğrenene kadar düğümün belleğinde durur;
// ekranda "Kabul et" düğmesiyle KALMAMALI (kullanıcı yanlışlıkla kabul
// edebilirdi).
func TestReddedilenTeklifGizlenir(t *testing.T) {
	dir := t.TempDir()
	src := &fakeOffers{list: []cluster.InOffer{testOffer()}}
	tr := newPairTracker(dir, src, nil)
	if err := writePairDecision(dir, "Ab3_-x9Qz", "482 913", false); err != nil {
		t.Fatal(err)
	}
	tr.tick()
	if len(src.Offers()) != 1 || !src.Offers()[0].Rejected {
		t.Fatal("red uygulanmadı")
	}
	if offs := tr.offers(); len(offs) != 0 {
		t.Fatalf("reddedilen teklif ekranda: %+v", offs)
	}
}
