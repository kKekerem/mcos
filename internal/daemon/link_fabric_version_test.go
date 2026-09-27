package daemon

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/linkjar"
	"mcos/internal/log"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// SÜRÜME GÖRE ORTAK DÜNYA JAR'I
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının gerçek raporu: "PC eşleştirmede ortak dünyayı açınca 'mcos
// link kurulu değil' diyor." Mod yalnızca 1.21.11 için vardı (fabricLinkMC
// sabiti), çevrimiçi kurulan sunucu 26.3 alıyordu; link.enable hatayı
// yalnızca günlüğe yazıp "ortak dünya oldu" diyordu. Eskiden buradaki test
// sabiti mods/mcos-link/gradle.properties ile karşılaştırıyordu; artık tek
// kaynak indeks (dist/mods/link/index-*.tsv).

// linkFixture points the daemon at a fake dist/mods/link with the real shape
// (1.20.5 … 26.3, each Fabric line with its own fabric-api) and returns it.
func linkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	link := filepath.Join(root, "link")
	lines := []string{
		"# sınama: gerçek biçim",
		"fabric\t1.20.5\tmcos-link-fabric-1.20.5.jar\tfabric-api-0.97.8+1.20.5.jar",
		"fabric\t1.21.11\tmcos-link-fabric-1.21.11.jar\tfabric-api-0.141.6+1.21.11.jar",
		"fabric\t26.3\tmcos-link-fabric-26.1-26.3.jar\tfabric-api-0.150.0+26.3.jar",
	}
	for _, l := range lines[1:] {
		f := strings.Split(l, "\t")
		writeTestJar(t, filepath.Join(link, f[2]), "mcos-link", "1.0.1", f[1])
		writeTestJar(t, filepath.Join(link, f[3]), "fabric-api",
			strings.TrimSuffix(strings.TrimPrefix(f[3], "fabric-api-"), ".jar"), f[1])
	}
	writeLinkFile(t, filepath.Join(link, "index-fabric.tsv"), strings.Join(lines, "\n")+"\n")
	writeTestJar(t, filepath.Join(link, "mcos-link-paper.jar"), "", "", "paper")
	writeLinkFile(t, filepath.Join(link, "index-paper.tsv"),
		"paper\t1.21.1\tmcos-link-paper.jar\t-\npaper\t26.3+\tmcos-link-paper.jar\t-\n")

	oldDirs, oldLegacy, oldFetch := linkJarDirs, linkModSearchPaths, fetchFabricAPI
	linkJarDirs = []string{link}
	linkModSearchPaths = []string{filepath.Join(root, "eski")}
	// Sınama ağa ÇIKMAZ: indirme istenirse sınama bunu görür.
	fetchFabricAPI = func(*Daemon, string, string) (string, error) {
		return "", errors.New("sınamada indirme yok")
	}
	t.Cleanup(func() {
		linkJarDirs, linkModSearchPaths, fetchFabricAPI = oldDirs, oldLegacy, oldFetch
	})
	return link
}

// writeTestJar writes a real zip; tag makes jars of different versions differ.
func writeTestJar(t *testing.T, path, id, version, tag string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if id != "" {
		w, _ := zw.Create("fabric.mod.json")
		_, _ = w.Write([]byte(`{"id":"` + id + `","version":"` + version + `"}`))
	}
	w, _ := zw.Create("tag.txt")
	_, _ = w.Write([]byte(tag))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeLinkFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// linkArtifact (hedef ad/klasör) ile linkjar.LoaderFor (jar seçimi) aynı
// yazılımları kabul etmeli: ayrışırlarsa biri jar'ı kopyalar, öbürü "kurulu
// değil" der ya da Forge'a Fabric modu gider.
func TestLinkArtifactMatchesLinkjar(t *testing.T) {
	for _, sw := range model.AllSoftware {
		_, dir, ok := linkArtifact(sw)
		l, err := linkjar.LoaderFor(sw)
		if ok != (err == nil) {
			t.Errorf("%s: linkArtifact=%v, linkjar=%v", sw, ok, err)
			continue
		}
		if ok && (dir == "mods") != (l == linkjar.Fabric) {
			t.Errorf("%s: klasör %s ama yükleyici %s", sw, dir, l)
		}
	}
}

// Kullanıcının durumu: 26.3 Fabric sunucusu. mods/ içinde mcos-link.jar
// (26.3 yapısı) ve 26.3'ün fabric-api'si olmalı; sürüm değiştirilmiş
// sunucudan kalan 1.21.11 fabric-api'si kaldırılmalı (yoksa sunucu açılmaz).
func TestFabric263GetsModAndRightFabricAPI(t *testing.T) {
	link := linkFixture(t)
	d := newRemoteTestDaemon(t)
	dir := t.TempDir()
	srv := &model.Server{ID: "f263", Name: "Yeni", Software: model.SoftwareFabric,
		MCVersion: "26.3", DataDir: dir}
	oldAPI := filepath.Join(dir, "mods", "fabric-api-0.141.6+1.21.11.jar")
	writeTestJar(t, oldAPI, "fabric-api", "0.141.6+1.21.11", "eski")

	if err := d.installLinkMod(srv); err != nil {
		t.Fatalf("26.3'e mod kurulamadı: %v", err)
	}
	mod := filepath.Join(dir, "mods", linkModName)
	if !sameFile(t, mod, filepath.Join(link, "mcos-link-fabric-26.1-26.3.jar")) {
		t.Error("mods/mcos-link.jar 26.3 yapısı değil")
	}
	if !sameFile(t, filepath.Join(dir, "mods", "fabric-api-0.150.0+26.3.jar"),
		filepath.Join(link, "fabric-api-0.150.0+26.3.jar")) {
		t.Error("26.3'ün fabric-api'si mods/'a konmadı")
	}
	if _, err := os.Stat(oldAPI); err == nil {
		t.Error("1.21.11'in fabric-api'si mods/'da kaldı — 26.3 sunucusu açılmaz")
	}
	if !d.linkModInstalled(srv) {
		t.Error("linkModInstalled kurulu modu görmüyor (hedef ad değişmiş olabilir)")
	}
}

// fabric-api kök dosya sistemine girmez: indeksin klasöründe yoksa
// mcos-install'ın AYNI adla koyduğu /data/artifacts'tan alınır.
func TestFabricAPIFromArtifacts(t *testing.T) {
	link := linkFixture(t)
	art := t.TempDir()
	name := "fabric-api-0.150.0+26.3.jar"
	if err := os.Rename(filepath.Join(link, name), filepath.Join(art, name)); err != nil {
		t.Fatal(err)
	}
	linkModSearchPaths = []string{art}
	d := newRemoteTestDaemon(t)
	dir := t.TempDir()
	srv := &model.Server{ID: "fa", Name: "A", Software: model.SoftwareFabric, MCVersion: "26.3", DataDir: dir}
	if err := d.installLinkMod(srv); err != nil {
		t.Fatalf("artifacts'taki fabric-api kullanılmadı: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", name)); err != nil {
		t.Error("fabric-api /data/artifacts'tan kopyalanmadı")
	}
}

// fabric-api hiçbir yerde yoksa ve indirilemiyorsa mod KURULMAZ; eski
// kopyası da kaldırılır (fabric-api'siz mcos-link sunucuyu hiç açtırmaz) ve
// neden link.status'a ulaşır.
func TestFabricAPIMissingBlocksModWithReason(t *testing.T) {
	link := linkFixture(t)
	_ = os.Remove(filepath.Join(link, "fabric-api-0.150.0+26.3.jar"))
	d := newRemoteTestDaemon(t)
	dir := t.TempDir()
	srv := &model.Server{ID: "fm", Name: "B", Software: model.SoftwareFabric, MCVersion: "26.3", DataDir: dir}
	stale := filepath.Join(dir, "mods", linkModName)
	writeLinkFile(t, stale, "eski")
	err := d.installLinkMod(srv)
	if err == nil || !strings.Contains(err.Error(), "fabric-api-0.150.0+26.3.jar") {
		t.Fatalf("fabric-api yokken: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("fabric-api'siz mcos-link.jar bırakıldı — sunucu açılmaz")
	}
	if p := d.linkModProblem(srv); !strings.Contains(p, "fabric-api") {
		t.Errorf("ModProblem nedeni taşımıyor: %q", p)
	}
}

// Aynı sunucuya iki kurulum aynı anda (link.enable + ensureLinkArtifact, ya
// da eşin art arda iki ApplyLinkSpec'i): ikisi de başarmalı, fabric-api BİR
// kez indirilmeli ve mods/ içinde tek fabric-api kalmalı.
func TestConcurrentInstallLinkModFetchesOnce(t *testing.T) {
	link := linkFixture(t)
	_ = os.Remove(filepath.Join(link, "fabric-api-0.150.0+26.3.jar"))
	var mu sync.Mutex
	fetches := 0
	fetchFabricAPI = func(_ *Daemon, _ string, modsDir string) (string, error) {
		mu.Lock()
		fetches++
		mu.Unlock()
		time.Sleep(200 * time.Millisecond) // yavaş indirme
		p := filepath.Join(modsDir, "fabric-api-0.150.0+26.3.jar")
		writeTestJar(t, p, "fabric-api", "0.150.0+26.3", "indirilen")
		return p, nil
	}
	d := newRemoteTestDaemon(t)
	dir := t.TempDir()
	srv := &model.Server{ID: "es", Name: "E", Software: model.SoftwareFabric, MCVersion: "26.3", DataDir: dir}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- d.installLinkMod(srv.Clone()) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Errorf("eş zamanlı kurulum: %v", err)
		}
	}
	if fetches != 1 {
		t.Errorf("fabric-api %d kez indirildi; 1 bekleniyordu (kurulumlar yarıştı)", fetches)
	}
	if got := linkjar.FabricAPIsIn(filepath.Join(dir, "mods")); len(got) != 1 {
		t.Errorf("mods/ içinde %d fabric-api: %v", len(got), got)
	}
	if !d.linkModInstalled(srv) {
		t.Error("mod kurulmadı")
	}
}

// Uyumsuz Fabric sürümüne mod KURULMAMALI ve eski kopya KALDIRILMALI.
func TestUyumsuzFabricSurumuneModKurulmaz(t *testing.T) {
	linkFixture(t)
	d := newRemoteTestDaemon(t) // tam bir daemon; yalnızca installLinkMod kullanılır
	dir := t.TempDir()
	srv := &model.Server{ID: "f1", Name: "Fabric", Software: model.SoftwareFabric, MCVersion: "1.20.1", DataDir: dir}
	eski := filepath.Join(dir, "mods", linkModName)
	writeLinkFile(t, eski, "eski uyumsuz mod")
	err := d.installLinkMod(srv)
	if err == nil {
		t.Fatal("1.20.1 Fabric sunucusuna mod kuruldu")
	}
	if want := "Fabric 1.20.1 için ortak dünya modu yok (desteklenen: 1.20.5–26.3)"; err.Error() != want {
		t.Errorf("ileti %q, %q bekleniyordu", err, want)
	}
	if _, err := os.Stat(eski); err == nil {
		t.Fatal("sunucuyu düşüren eski mod kopyası kaldırılmadı")
	}
}

// Paper'da "+" satırı: 26.4 sunucusu 26.3+ eklentisini alır.
func TestPaperPlusLine(t *testing.T) {
	link := linkFixture(t)
	d := newRemoteTestDaemon(t)
	dir := t.TempDir()
	srv := &model.Server{ID: "p1", Name: "P", Software: model.SoftwarePurpur, MCVersion: "26.4", DataDir: dir}
	if err := d.installLinkMod(srv); err != nil {
		t.Fatalf("Purpur 26.4: %v", err)
	}
	if !sameFile(t, filepath.Join(dir, "plugins", linkPluginName), filepath.Join(link, "mcos-link-paper.jar")) {
		t.Error("plugins/mcos-link-paper.jar kurulmadı")
	}
}

// newPairedLinkDaemon is a daemon with one paired (offline) peer: link.enable
// "önce bir PC eşleştirin" demesin.
func newPairedLinkDaemon(t *testing.T) *Daemon {
	t.Helper()
	root := t.TempDir()
	peers := `[{"id":"id:es","name":"ikinci-pc","ip":"127.0.0.1","port":1}]`
	writeLinkFile(t, filepath.Join(root, "cluster", "peers.json"), peers)
	d, err := New(filepath.Join(root, "config.json"), root, log.New(io.Discard, log.LevelError, 16))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.stopRemote)
	return d
}

func enableLink(d *Daemon, id string) (any, error) {
	raw, _ := json.Marshal(map[string]any{"serverId": id, "difficulty": "normal"})
	return d.handleLinkEnable(context.Background(), raw)
}

// 1.20.1 için mod yok: link.enable HATA döndürmeli ve sunucu ortak dünya
// olarak İŞARETLENMEMELİ. Eskiden "ortak dünya oldu" deniyordu.
func TestLinkEnableUnsupportedVersionFails(t *testing.T) {
	linkFixture(t)
	d := newPairedLinkDaemon(t)
	srv := &model.Server{ID: "e1", Name: "Eski", Software: model.SoftwareFabric,
		MCVersion: "1.20.1", DataDir: t.TempDir()}
	if err := d.store.SaveServer(srv); err != nil {
		t.Fatal(err)
	}
	_, err := enableLink(d, srv.ID)
	var ie *ipc.Error
	if !errors.As(err, &ie) {
		t.Fatalf("hata dönmedi: %v", err)
	}
	if !strings.Contains(ie.Message, "desteklenen: 1.20.5–26.3") {
		t.Errorf("hata nedeni söylemiyor: %q", ie.Message)
	}
	got, _ := d.store.GetServer(srv.ID)
	if got.Link.Mode == model.LinkSharedWorld {
		t.Error("mod kurulamadığı halde sunucu ortak dünya olarak işaretlendi")
	}
}

// 26.3 için mod var: link.enable başarır, sunucu işaretlenir, mod yerinde.
func TestLinkEnable263Succeeds(t *testing.T) {
	linkFixture(t)
	d := newPairedLinkDaemon(t)
	dir := t.TempDir()
	srv := &model.Server{ID: "e2", Name: "Yeni", Software: model.SoftwareFabric, MCVersion: "26.3", DataDir: dir}
	if err := d.store.SaveServer(srv); err != nil {
		t.Fatal(err)
	}
	if _, err := enableLink(d, srv.ID); err != nil {
		t.Fatalf("26.3 ortak dünya açılamadı: %v", err)
	}
	got, _ := d.store.GetServer(srv.ID)
	if got.Link.Mode != model.LinkSharedWorld {
		t.Error("sunucu ortak dünya olarak işaretlenmedi")
	}
	if !d.linkModInstalled(got) {
		t.Error("mod kurulmadı")
	}
}

// link.status mod yokken NEDENİ taşımalı (panel onu gösterir).
func TestLinkStatusCarriesModProblem(t *testing.T) {
	linkFixture(t)
	d := newRemoteTestDaemon(t)
	srv := &model.Server{ID: "s1", Name: "Eski", Software: model.SoftwareFabric,
		MCVersion: "1.20.1", DataDir: t.TempDir(),
		Link: model.LinkConfig{Mode: model.LinkSharedWorld}}
	if err := d.store.SaveServer(srv); err != nil {
		t.Fatal(err)
	}
	res, err := d.handleLinkStatus(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	st := res.(model.LinkStatus)
	if st.ModInstalled || !strings.HasPrefix(st.ModProblem, "Fabric 1.20.1 için ortak dünya modu yok") {
		t.Errorf("ModProblem = %q (kurulu=%v)", st.ModProblem, st.ModInstalled)
	}
}

// Gerçek dist/mods/link varsa (mod derleyen işler üretir) indeksin
// gösterdiği her dosya orada ve geçerli bir jar olmalı: eksik bir satır
// imajda "kayıtlı ama yok" hatasına dönüşür.
func TestDistLinkIndexIsComplete(t *testing.T) {
	dist := filepath.Join("..", "..", "dist", "mods", "link")
	l := linkjar.Locator{Dirs: []string{dist}}
	es := l.Entries()
	if len(es) == 0 {
		t.Skip("dist/mods/link yok ya da indekssiz (make mod henüz çalışmadı)")
	}
	isJar := func(p string) bool {
		b, err := os.ReadFile(p)
		return err == nil && len(b) > 2 && string(b[:2]) == "PK"
	}
	for _, e := range es {
		if !isJar(e.Jar) {
			t.Errorf("%s %s: %s yok ya da jar değil", e.Loader, e.MC, filepath.Base(e.Jar))
		}
		if e.Loader != linkjar.Fabric {
			continue
		}
		if e.Dep == "" {
			t.Errorf("fabric %s: fabric-api bağımlılığı yazılmamış", e.MC)
			continue
		}
		dep := filepath.Join(filepath.Dir(e.Jar), e.Dep)
		if !isJar(dep) {
			t.Errorf("fabric %s: %s yok ya da jar değil", e.MC, e.Dep)
			continue
		}
		// fabric-api'nin kendi söylediği sürüm adıyla aynı olmalı: kurulum
		// "uygun fabric-api var mı" kararını buna göre veriyor.
		if got := linkjar.FabricAPIMC(dep); got == "" || !strings.HasSuffix(e.Dep, "+"+got+".jar") {
			t.Errorf("fabric %s: %s içindeki sürüm %q, adıyla uyuşmuyor", e.MC, e.Dep, got)
		}
	}
}
