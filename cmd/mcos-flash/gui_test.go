package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/deskgui"
	"mcos/internal/flash"
)

// ════════════════════════════════════════════════════════════════════════════
// PENCERE, KONSOLUN GÜVENLİK KURALLARINI AYNEN UYGULAMALI
// ════════════════════════════════════════════════════════════════════════════
//
// Pencere disk silen bir uç (/api/yaz) sunuyor. Sınamalar HİÇBİR aygıta
// dokunmaz: aygıt listesi ve yazıcı sahtedir. Sahte liste BİLEREK sistem
// diskini ve dahili diski de içerir (gerçek Enumerate(false) onları zaten
// eler): pencerenin kendi süzgeci de çalışmalı, tek katmana güvenilmemeli.

const gb = uint64(1) << 30

var fakeDevices = []flash.Device{
	{Path: "/dev/sda", Name: "sda", Model: "Sistem SSD", SizeBytes: 500 * gb, System: true},
	{Path: "/dev/sdb", Name: "sdb", Model: "Yedek HDD", SizeBytes: 1000 * gb},
	{Path: "/dev/sdc", Name: "sdc", Model: "SanDisk Ultra", SizeBytes: 16 * gb, Removable: true, Bus: "usb"},
	{Path: "/dev/sdd", Name: "sdd", Model: "Eski bellek", SizeBytes: 2 * gb, Removable: true, Bus: "usb"},
}

type flashRig struct {
	g       *flashGUI
	base    string
	token   string
	imgNew  string
	imgOld  string
	mu      sync.Mutex
	enumArg []bool
	writes  []flash.Writer
	devs    []flash.Device
	block   chan struct{} // nil: yazıcı hemen biter
}

func newFlashRig(t *testing.T, cfg guiConfig, elevated bool) *flashRig {
	t.Helper()
	dir := t.TempDir()
	mk := func(name string, size int64, age time.Duration) string {
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		f.Close()
		m := time.Now().Add(-age)
		_ = os.Chtimes(p, m, m)
		return p
	}
	r := &flashRig{devs: append([]flash.Device(nil), fakeDevices...)}
	r.imgNew = mk("mcos-usb.img", 20<<20, 0)
	r.imgOld = mk("mcos-eski.img", 30<<20, 80*time.Hour)

	g := newFlashGUI(cfg)
	g.imageDirs = func() []string { return []string{dir} }
	g.elevated = func() bool { return elevated }
	g.relaunch = func([]string) error { return errors.New("sınamada yükseltme yok") }
	g.enumerate = func(internal bool) ([]flash.Device, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.enumArg = append(r.enumArg, internal)
		return append([]flash.Device(nil), r.devs...), nil
	}
	g.write = func(ctx context.Context, w flash.Writer, p func(flash.Progress)) error {
		r.mu.Lock()
		r.writes = append(r.writes, w)
		block := r.block
		r.mu.Unlock()
		p(flash.Progress{Stage: "yazılıyor", BytesDone: 10 << 20, BytesTotal: 20 << 20})
		if block != nil {
			select {
			case <-block:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		p(flash.Progress{Stage: "doğrulanıyor", BytesDone: 20 << 20, BytesTotal: 20 << 20, Done: true})
		return nil
	}
	r.g = g

	srv, err := deskgui.New(deskgui.Page{Title: "sınama"})
	if err != nil {
		t.Fatal(err)
	}
	g.register(srv)
	srv.Start()
	t.Cleanup(srv.Close)
	r.base = strings.SplitN(srv.URL(), "/?", 2)[0]
	r.token = srv.Token()
	return r
}

func (r *flashRig) call(t *testing.T, method, path string, body any, withToken bool) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, r.base+path, rd)
	if withToken {
		req.Header.Set(deskgui.TokenHeader, r.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func (r *flashRig) writeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.writes)
}

func writeReq(img, dev string, size uint64, confirm string) map[string]any {
	return map[string]any{"imaj": img, "aygit": dev, "boyut": size, "onay": confirm}
}

func (r *flashRig) waitDone(t *testing.T) writeProgress {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.g.mu.Lock()
		p, w := r.g.prog, r.g.writing
		r.g.mu.Unlock()
		if !w && !p.Active {
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("yazma bitmedi")
	return writeProgress{}
}

func TestFlashGUIAygitlarYalnizCikarilabilir(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	code, v := r.call(t, http.MethodGet, "/api/aygitlar", nil, true)
	if code != http.StatusOK {
		t.Fatalf("kod %d", code)
	}
	var got []string
	for _, d := range v["aygitlar"].([]any) {
		got = append(got, d.(map[string]any)["yol"].(string))
	}
	if !reflect.DeepEqual(got, []string{"/dev/sdc", "/dev/sdd"}) {
		t.Fatalf("listelenen aygıtlar %v; yalnızca çıkarılabilirler (sdc, sdd) olmalıydı", got)
	}
	for _, a := range r.enumArg {
		if a {
			t.Fatal("pencere dahili diskleri istedi (Enumerate(true))")
		}
	}
}

func TestFlashGUIBelirtecsizYazilamaz(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	code, _ := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, "/dev/sdc"), false)
	if code != http.StatusForbidden || r.writeCount() != 0 {
		t.Fatalf("belirteçsiz yazma: kod %d, yazma %d", code, r.writeCount())
	}
}

func TestFlashGUIOnayEslesmezseYazmaz(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	for _, onay := range []string{"", "evet", "/dev/sd", "/dev/sdd", "/DEV/SDC"} {
		code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, onay), true)
		if code != http.StatusBadRequest || !strings.Contains(v["hata"].(string), "Onay") {
			t.Errorf("onay %q: kod %d yanıt %v", onay, code, v)
		}
	}
	if r.writeCount() != 0 {
		t.Fatalf("onaysız yazma başladı (%d)", r.writeCount())
	}
}

// Sistem diski ve dahili disk, istemci yolunu elle gönderse bile yazılamaz.
func TestFlashGUISistemVeDahiliDiskeYazilamaz(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	for _, d := range fakeDevices[:2] {
		code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, d.Path, d.SizeBytes, d.Path), true)
		if code == http.StatusOK {
			t.Errorf("%s (%s) için yazma kabul edildi: %v", d.Path, d.Model, v)
		}
	}
	if r.writeCount() != 0 {
		t.Fatalf("sistem/dahili diske yazma başladı (%d)", r.writeCount())
	}
}

// Kullanıcı seçtikten sonra bellek değişti: aynı yol, farklı boyut.
func TestFlashGUIAygitDegistiyseReddeder(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	r.mu.Lock()
	r.devs[2].SizeBytes = 32 * gb // yerine 32 GB'lık başka bir bellek takıldı
	r.mu.Unlock()
	code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, "/dev/sdc"), true)
	if code != http.StatusConflict || !strings.Contains(v["hata"].(string), "değişmiş") {
		t.Fatalf("değişen aygıt: kod %d yanıt %v", code, v)
	}
	r.mu.Lock()
	r.devs = r.devs[:2] // bellek çıkarıldı
	r.mu.Unlock()
	code, v = r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, "/dev/sdc"), true)
	if code != http.StatusConflict || !strings.Contains(v["hata"].(string), "listede yok") {
		t.Fatalf("çıkarılan aygıt: kod %d yanıt %v", code, v)
	}
	if r.writeCount() != 0 {
		t.Fatal("değişen/çıkarılan aygıta yazma başladı")
	}
}

func TestFlashGUIListedeOlmayanImajYazilamaz(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	for _, img := range []string{"/etc/passwd", r.imgNew + ".yok", ""} {
		code, _ := r.call(t, http.MethodPost, "/api/yaz", writeReq(img, "/dev/sdc", 16*gb, "/dev/sdc"), true)
		if code != http.StatusBadRequest {
			t.Errorf("imaj %q: kod %d, 400 bekleniyordu", img, code)
		}
	}
	if r.writeCount() != 0 {
		t.Fatal("listede olmayan imaj yazıldı")
	}
}

func TestFlashGUIKucukAygitReddedilir(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdd", 2*gb, "/dev/sdd"), true)
	if code != http.StatusBadRequest || !strings.Contains(v["hata"].(string), "küçük") || r.writeCount() != 0 {
		t.Fatalf("2 GB bellek: kod %d yanıt %v yazma %d", code, v, r.writeCount())
	}
}

func TestFlashGUIYoneticiGerekli(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, false)
	code, _ := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, "/dev/sdc"), true)
	if code != http.StatusForbidden || r.writeCount() != 0 {
		t.Fatalf("yöneticisiz yazma: kod %d yazma %d", code, r.writeCount())
	}
	// Deneme kipi aygıt açmaz; yönetici istemez.
	r = newFlashRig(t, guiConfig{verify: true, dryRun: true}, false)
	if code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, "/dev/sdc"), true); code != http.StatusOK {
		t.Fatalf("deneme kipi reddedildi: %d %v", code, v)
	}
	r.waitDone(t)
	if w := r.writes[0]; !w.DryRun || w.Verify {
		t.Fatalf("deneme kipi yazıcıya geçmedi: DryRun=%v Verify=%v", w.DryRun, w.Verify)
	}
}

func TestFlashGUIYazmaAkisi(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	r.block = make(chan struct{})
	code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, " /dev/sdc "), true)
	if code != http.StatusOK {
		t.Fatalf("kod %d: %v", code, v)
	}
	// Sürerken: ilerleme görünür, ikinci yazma reddedilir.
	deadline := time.Now().Add(2 * time.Second)
	for r.writeCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	_, st := r.call(t, http.MethodGet, "/api/durum", nil, true)
	p := st["ilerleme"].(map[string]any)
	if p["etkin"] != true || p["asama"] != "yazılıyor" || p["yuzde"] != float64(50) {
		t.Fatalf("ilerleme görünmüyor: %v", p)
	}
	if code, _ := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgNew, "/dev/sdc", 16*gb, "/dev/sdc"), true); code != http.StatusConflict {
		t.Fatalf("yazma sürerken ikinci yazma: kod %d, 409 bekleniyordu", code)
	}
	close(r.block)
	fin := r.waitDone(t)
	if !fin.OK || fin.Percent != 100 || fin.Err != "" {
		t.Fatalf("bitiş: %+v", fin)
	}
	w := r.writes[0]
	if w.ImagePath != r.imgNew || w.Device.Path != "/dev/sdc" || !w.Verify || w.DryRun {
		t.Fatalf("yazıcıya giden: %+v", w)
	}
}

func TestFlashGUIIptal(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	r.block = make(chan struct{})
	if code, v := r.call(t, http.MethodPost, "/api/yaz", writeReq(r.imgOld, "/dev/sdc", 16*gb, "/dev/sdc"), true); code != http.StatusOK {
		t.Fatalf("kod %d: %v", code, v)
	}
	for r.writeCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if code, _ := r.call(t, http.MethodPost, "/api/iptal", map[string]any{}, true); code != http.StatusOK {
		t.Fatalf("iptal kodu %d", code)
	}
	fin := r.waitDone(t)
	if fin.OK || !strings.Contains(fin.Err, "Durduruldu") {
		t.Fatalf("iptal sonrası: %+v", fin)
	}
}

func TestFlashGUIImajlarEnYeniOnce(t *testing.T) {
	r := newFlashRig(t, guiConfig{verify: true}, true)
	_, v := r.call(t, http.MethodGet, "/api/imajlar", nil, true)
	list := v["imajlar"].([]any)
	if len(list) != 2 {
		t.Fatalf("%d imaj", len(list))
	}
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if first["yol"] != r.imgNew || first["enYeni"] != true {
		t.Fatalf("ilk sırada en yeni imaj olmalı: %v", first)
	}
	if second["bayat"] != "3 gün" {
		t.Fatalf("eski imaj bayat işaretlenmedi: %v", second)
	}
}

func TestFlashWantGUI(t *testing.T) {
	for _, c := range []struct {
		ad   string
		set  []string
		goos string
		want bool
	}{
		{"Windows çift tıklama", nil, "windows", true},
		{"Windows --dry-run", []string{"dry-run"}, "windows", true},
		{"Windows --image", []string{"image"}, "windows", true},
		{"Windows --konsol", []string{"konsol"}, "windows", false},
		{"Windows --list", []string{"list"}, "windows", false},
		{"Windows betik (--device --yes)", []string{"device", "yes", "image"}, "windows", false},
		{"Windows --all-disks (uzman)", []string{"all-disks"}, "windows", false},
		{"Linux varsayılan", nil, "linux", false},
		{"Linux --gui", []string{"gui"}, "linux", true},
		{"Linux --gui --list", []string{"gui", "list"}, "linux", false},
	} {
		set := map[string]bool{}
		for _, s := range c.set {
			set[s] = true
		}
		if got := wantGUI(set, c.goos); got != c.want {
			t.Errorf("%s: %v, beklenen %v", c.ad, got, c.want)
		}
	}
}

// Yönetici olarak yeniden açılış deneme kipini DÜŞÜRMEMELİ (mcos-flash.bat'ta
// yakalanan hatanın pencere karşılığı).
func TestFlashRelaunchArgsKorunur(t *testing.T) {
	g := newFlashGUI(guiConfig{dryRun: true, verify: false, image: `C:\x\mcos.img`, imageDir: `D:\imajlar`})
	want := []string{"--gui", "--dry-run", "--no-verify", "--image", `C:\x\mcos.img`, "--images", `D:\imajlar`}
	if got := g.relaunchArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("yükseltme bayrakları %v, beklenen %v", got, want)
	}
}
