package linkjar

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// ── Yardımcılar ─────────────────────────────────────────────────────────────

// writeJar writes a small but REAL zip; fabric.mod.json is added when id != "".
func writeJar(t *testing.T, path, id, version string) {
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
		_, _ = w.Write([]byte(`{"schemaVersion":1,"id":"` + id + `","version":"` + version + `"}`))
	} else {
		w, _ := zw.Create("x.txt")
		_, _ = w.Write([]byte("x"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture builds a dist/mods/link-like folder: index lines + the jars they name.
func fixture(t *testing.T, dir string, lines ...string) {
	t.Helper()
	byLoader := map[string][]string{}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		byLoader[f[0]] = append(byLoader[f[0]], l)
		writeJar(t, filepath.Join(dir, f[2]), "mcos-link", "1.0.1")
		if len(f) > 3 && f[3] != "-" {
			mc := strings.TrimSuffix(f[1], "+")
			writeJar(t, filepath.Join(dir, f[3]), "fabric-api", "0.1.0+"+mc)
		}
	}
	for loader, ls := range byLoader {
		writeFile(t, filepath.Join(dir, "index-"+loader+".tsv"),
			"# sınama indeksi\n"+strings.Join(ls, "\n")+"\n")
	}
}

// fabricRange is the real shape: one line per exact version 1.20.5 … 26.3.
var fabricRange = []string{
	"fabric\t1.20.5\tmcos-link-fabric-1.20.5.jar\tfabric-api-0.97.8+1.20.5.jar",
	"fabric\t1.21.1\tmcos-link-fabric-1.21.1.jar\tfabric-api-0.116.7+1.21.1.jar",
	"fabric\t1.21.4\tmcos-link-fabric-1.21.4.jar\tfabric-api-0.119.2+1.21.4.jar",
	"fabric\t1.21.9\tmcos-link-fabric-1.21.9.jar\tfabric-api-0.133.14+1.21.9.jar",
	"fabric\t1.21.10\tmcos-link-fabric-1.21.10.jar\tfabric-api-0.138.3+1.21.10.jar",
	"fabric\t1.21.11\tmcos-link-fabric-1.21.11.jar\tfabric-api-0.141.6+1.21.11.jar",
	"fabric\t26.1\tmcos-link-fabric-26.1-26.3.jar\tfabric-api-0.145.0+26.1.jar",
	"fabric\t26.3\tmcos-link-fabric-26.1-26.3.jar\tfabric-api-0.150.0+26.3.jar",
}

// ── Sürüm karşılaştırma ─────────────────────────────────────────────────────

// Metin karşılaştırması iki GERÇEK sürüm çiftinde yanlış sonuç verir.
func TestCompareIsNumeric(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"26.1", "1.21.11", 1},   // takvim sürümü > eski şema
		{"1.21.10", "1.21.9", 1}, // metin olarak "1.21.10" < "1.21.9"
		{"26.1.2", "26.1", 1},
		{"1.21", "1.21.0", 0},
		{"26.3", "26.3", 0},
		{"1.20.5", "1.21.1", -1},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if !ok || got != c.want {
			t.Errorf("Compare(%q,%q) = %d,%v; %d bekleniyordu", c.a, c.b, got, ok, c.want)
		}
	}
	if _, ok := Compare("26.1-pre1", "26.1"); ok {
		t.Error("ön sürüm sayısal sayıldı; '+' satırına yanlışlıkla girerdi")
	}
}

// ── Seçim ───────────────────────────────────────────────────────────────────

// Kullanıcının raporu: çevrimiçi kurulan sunucu 26.3 aldı, mod kurulmadı.
func TestFindExactFabric263(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, fabricRange...)
	l := Locator{Dirs: []string{dir}}

	r, err := l.Find(model.SoftwareFabric, "26.3")
	if err != nil {
		t.Fatalf("26.3 için jar bulunamadı: %v", err)
	}
	if filepath.Base(r.Jar) != "mcos-link-fabric-26.1-26.3.jar" || !filepath.IsAbs(r.Jar) {
		t.Errorf("jar = %q", r.Jar)
	}
	if r.Dep != "fabric-api-0.150.0+26.3.jar" {
		t.Errorf("bağımlılık = %q; 26.3'ün fabric-api'si bekleniyordu", r.Dep)
	}
	if r.DepPath != filepath.Join(dir, r.Dep) {
		t.Errorf("bağımlılık yolu = %q; indeksin klasöründeki kopya bekleniyordu", r.DepPath)
	}
	// 1.21.10 ile 1.21.9 metin olarak karışır: her biri KENDİ jar'ını almalı.
	for _, mc := range []string{"1.21.9", "1.21.10", "1.21.11"} {
		r, err := l.Find(model.SoftwareFabric, mc)
		if err != nil || filepath.Base(r.Jar) != "mcos-link-fabric-"+mc+".jar" {
			t.Errorf("%s: jar %q, hata %v", mc, r.Jar, err)
		}
	}
}

// "+" satırı o sürümü VE sonrasını kapsar; tam satır ona üstün gelir.
func TestFindPlusLineAndExactWins(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "mcos-link-paper.jar"), "", "")
	writeJar(t, filepath.Join(dir, "mcos-link-paper-26.5-ozel.jar"), "", "")
	writeFile(t, filepath.Join(dir, "index-paper.tsv"),
		"paper\t26.3+\tmcos-link-paper.jar\t-\n"+
			"paper\t1.21.1\tmcos-link-paper.jar\t-\n"+
			"paper\t26.5\tmcos-link-paper-26.5-ozel.jar\t-\n")
	l := Locator{Dirs: []string{dir}}

	cases := map[string]string{
		"26.3":   "mcos-link-paper.jar",
		"26.4":   "mcos-link-paper.jar",
		"27.1.2": "mcos-link-paper.jar",
		"1.21.1": "mcos-link-paper.jar",
		"26.5":   "mcos-link-paper-26.5-ozel.jar", // tam satır "+" satırını yener
	}
	for mc, want := range cases {
		r, err := l.Find(model.SoftwarePaper, mc)
		if err != nil {
			t.Errorf("%s: %v", mc, err)
			continue
		}
		if filepath.Base(r.Jar) != want {
			t.Errorf("%s: %s seçildi, %s bekleniyordu", mc, filepath.Base(r.Jar), want)
		}
		if r.Dep != "" {
			t.Errorf("%s: '-' bağımlılığı %q oldu", mc, r.Dep)
		}
	}
	// "+" satırı AŞAĞI doğru kapsamaz.
	if _, err := l.Find(model.SoftwarePaper, "26.2"); KindOf(err) != KindVersion {
		t.Errorf("26.2 kabul edildi ya da yanlış hata: %v", err)
	}
	// Purpur da Paper eklentisini alır.
	if r, err := l.Find(model.SoftwarePurpur, "26.4"); err != nil || r.Loader != Paper {
		t.Errorf("Purpur: %+v %v", r, err)
	}
}

// İki "+" satırı: sayısal olarak EN YÜKSEK uygun taban seçilmeli. Metin
// karşılaştırmasıyla "1.21.9+" > "1.21.10+" sanılır ve eski jar seçilir.
func TestFindPlusPicksHighestNumericBase(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "a.jar"), "", "")
	writeJar(t, filepath.Join(dir, "b.jar"), "", "")
	writeFile(t, filepath.Join(dir, "index-paper.tsv"),
		"paper\t1.21.9+\ta.jar\t-\npaper\t1.21.10+\tb.jar\t-\n")
	l := Locator{Dirs: []string{dir}}
	if r, err := l.Find(model.SoftwarePaper, "1.21.11"); err != nil || filepath.Base(r.Jar) != "b.jar" {
		t.Errorf("1.21.11: %q %v; b.jar (1.21.10+) bekleniyordu", r.Jar, err)
	}
	if r, err := l.Find(model.SoftwarePaper, "1.21.9"); err != nil || filepath.Base(r.Jar) != "a.jar" {
		t.Errorf("1.21.9: %q %v; a.jar bekleniyordu", r.Jar, err)
	}
	if r, err := l.Find(model.SoftwarePaper, "26.1"); err != nil || filepath.Base(r.Jar) != "b.jar" {
		t.Errorf("26.1: %q %v; b.jar bekleniyordu", r.Jar, err)
	}
}

// Desteklenmeyen sürüm: NEDEN ve desteklenen aralık kullanıcıya gösterilir.
func TestUnsupportedVersionMessage(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, fabricRange...)
	l := Locator{Dirs: []string{dir}}

	_, err := l.Find(model.SoftwareFabric, "1.20.1")
	if KindOf(err) != KindVersion {
		t.Fatalf("1.20.1: %v", err)
	}
	want := "Fabric 1.20.1 için ortak dünya modu yok (desteklenen: 1.20.5–26.3)"
	if err.Error() != want {
		t.Errorf("ileti:\n  %q\nbeklenen:\n  %q", err.Error(), want)
	}
	// Aralığın İÇİNDEKİ boşluk: en yakın sürümler yazılmalı.
	_, err = l.Find(model.SoftwareFabric, "1.21.3")
	if KindOf(err) != KindVersion || !strings.Contains(err.Error(), "en yakın: 1.21.1, 1.21.4") {
		t.Errorf("boşluk iletisi: %v", err)
	}
	// Paper "+" satırında "ve sonrası".
	pdir := t.TempDir()
	fixture(t, pdir, "paper\t1.21.1\tmcos-link-paper.jar\t-", "paper\t26.3+\tmcos-link-paper.jar\t-")
	_, err = Locator{Dirs: []string{pdir}}.Find(model.SoftwarePaper, "1.20.4")
	if err == nil || err.Error() != "Paper 1.20.4 için ortak dünya eklentisi yok (desteklenen: 1.21.1–26.3 ve sonrası)" {
		t.Errorf("Paper iletisi: %v", err)
	}
	if _, err := l.Find(model.SoftwareFabric, ""); KindOf(err) != KindVersion {
		t.Errorf("boş sürüm: %v", err)
	}
}

// "+" satırı en yüksek satır DEĞİLSE de üst uç açıktır. Ölçüldü: "1.21.1+"
// ve tam "26.3" satırlı indekste 26.4 seçiliyordu ama ileti "desteklenen:
// 1.21.1–26.3" diyordu — kullanıcı 26.4'ün desteklenmediğini sanardı.
func TestSupportedTextOpenEndedWhenPlusIsNotHighest(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "a.jar"), "", "")
	writeJar(t, filepath.Join(dir, "b.jar"), "", "")
	writeFile(t, filepath.Join(dir, "index-paper.tsv"),
		"paper\t1.21.1+\ta.jar\t-\npaper\t26.3\tb.jar\t-\n")
	l := Locator{Dirs: []string{dir}}
	if r, err := l.Find(model.SoftwarePaper, "26.4"); err != nil || filepath.Base(r.Jar) != "a.jar" {
		t.Fatalf("26.4: %q %v; a.jar (1.21.1+) bekleniyordu", r.Jar, err)
	}
	_, err := l.Find(model.SoftwarePaper, "1.20.4")
	want := "Paper 1.20.4 için ortak dünya eklentisi yok (desteklenen: 1.21.1–26.3 ve sonrası)"
	if err == nil || err.Error() != want {
		t.Errorf("ileti:\n  %v\nbeklenen:\n  %s", err, want)
	}
}

func TestUnsupportedSoftwareMessage(t *testing.T) {
	l := Locator{}
	_, err := l.Find(model.SoftwareForge, "1.21.11")
	if err == nil || err.Error() != "Forge ortak dünyayı desteklemiyor; Fabric veya Paper seçin" {
		t.Errorf("Forge: %v", err)
	}
	for _, sw := range []model.Software{model.SoftwareNeoForge, model.SoftwareQuilt,
		model.SoftwareVanilla, ""} {
		if _, err := l.Find(sw, "1.21.11"); KindOf(err) != KindUnsupported {
			t.Errorf("%q: %v; KindUnsupported bekleniyordu", sw, err)
		}
	}
}

// Yorum, boş satır, CRLF ve bozuk satırlar bütün indeksi bozmamalı.
func TestIndexCommentsBlankAndBadLines(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "ok.jar"), "", "")
	writeJar(t, filepath.Join(filepath.Dir(dir), "disari.jar"), "", "")
	writeFile(t, filepath.Join(dir, "index-fabric.tsv"),
		"# yükleyici\tsürüm\tjar\tbağımlılık\r\n"+
			"\r\n"+
			"   \n"+
			"fabric\t1.21.4\r\n"+ // eksik alan
			"fabric\t1.21.5\t../disari.jar\t-\n"+ // klasör dışına kaçış
			"fabric\t26.3\tok.jar\tfabric-api-0.150.0+26.3.jar\r\n")
	l := Locator{Dirs: []string{dir}}
	es := l.Entries()
	if len(es) != 1 || es[0].MC != "26.3" || es[0].Dep != "fabric-api-0.150.0+26.3.jar" {
		t.Fatalf("girdiler: %+v", es)
	}
	if _, err := l.Find(model.SoftwareFabric, "1.21.5"); err == nil {
		t.Error("indeks klasörü dışındaki jar kabul edildi")
	}
}

// İndeks bir jar'ı gösteriyor ama dosya yok (ya da boş): NEDEN söylenmeli.
func TestMissingJarFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index-fabric.tsv"),
		"fabric\t26.3\tmcos-link-fabric-26.1-26.3.jar\tfabric-api-0.150.0+26.3.jar\n"+
			"fabric\t26.2\tbos.jar\t-\n")
	writeFile(t, filepath.Join(dir, "bos.jar"), "")
	l := Locator{Dirs: []string{dir}}
	_, err := l.Find(model.SoftwareFabric, "26.3")
	if KindOf(err) != KindMissing || !strings.Contains(err.Error(), "mcos-link-fabric-26.1-26.3.jar yok") {
		t.Errorf("eksik jar: %v", err)
	}
	if _, err := l.Find(model.SoftwareFabric, "26.2"); KindOf(err) != KindMissing {
		t.Errorf("boş jar kabul edildi: %v", err)
	}
	// Hiç indeks yoksa: nerede arandığı yazılır.
	_, err = Locator{Dirs: []string{"/yok/mods/link"}}.Find(model.SoftwarePaper, "26.3")
	if KindOf(err) != KindMissing || !strings.Contains(err.Error(), "/yok/mods/link") {
		t.Errorf("indekssiz: %v", err)
	}
}

// Birinci klasördeki jar eksikse ikinci klasördeki kullanılır; ikisi de
// varsa ilki kazanır.
func TestDirPriority(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	fixture(t, a, "fabric\t26.3\tx.jar\t-")
	fixture(t, b, "fabric\t26.3\tx.jar\t-", "fabric\t26.2\ty.jar\t-")
	l := Locator{Dirs: []string{a, b}}
	if r, _ := l.Find(model.SoftwareFabric, "26.3"); filepath.Dir(r.Jar) != a {
		t.Errorf("26.3 %s'den seçildi, ilk klasör bekleniyordu", r.Jar)
	}
	if r, _ := l.Find(model.SoftwareFabric, "26.2"); filepath.Dir(r.Jar) != b {
		t.Errorf("26.2 %s'den seçildi", r.Jar)
	}
	_ = os.Remove(filepath.Join(a, "x.jar"))
	if r, err := l.Find(model.SoftwareFabric, "26.3"); err != nil || filepath.Dir(r.Jar) != b {
		t.Errorf("ilk klasörde jar yokken: %q %v", r.Jar, err)
	}
}

// Eski düzen: indekssiz mcos-link.jar / mcos-link-paper.jar YALNIZCA 1.21.11.
func TestLegacyLayout(t *testing.T) {
	old := t.TempDir()
	writeJar(t, filepath.Join(old, LegacyFabricJar), "mcos-link", "1.0.1")
	writeJar(t, filepath.Join(old, LegacyPaperJar), "", "")
	l := Locator{Dirs: []string{filepath.Join(old, "link")}, LegacyDirs: []string{old}}

	r, err := l.Find(model.SoftwareFabric, "1.21.11")
	if err != nil || !r.Legacy || r.Jar != filepath.Join(old, LegacyFabricJar) {
		t.Fatalf("eski Fabric jar'ı 1.21.11'de seçilmedi: %+v %v", r, err)
	}
	if r.Dep != LegacyFabricDep {
		t.Errorf("eski Fabric jar'ının bağımlılığı %q", r.Dep)
	}
	if r, err := l.Find(model.SoftwarePaper, "1.21.11"); err != nil || !r.Legacy {
		t.Errorf("eski Paper jar'ı 1.21.11'de seçilmedi: %+v %v", r, err)
	}
	// 1.21.1'de eski Fabric jar'ı NoSuchFieldError ile sunucuyu düşürüyordu;
	// eski Paper jar'ı "Unsupported API version 1.21.11" ile reddediliyordu.
	for _, sw := range []model.Software{model.SoftwareFabric, model.SoftwarePaper} {
		for _, mc := range []string{"1.21.1", "26.3"} {
			_, err := l.Find(sw, mc)
			if KindOf(err) != KindVersion || !strings.Contains(err.Error(), "desteklenen: 1.21.11") {
				t.Errorf("%s %s: %v", sw, mc, err)
			}
		}
	}
	// İndeks varsa ve 1.21.11'i kapsıyorsa indeks kazanır.
	fixture(t, filepath.Join(old, "link"), fabricRange...)
	if r, err := l.Find(model.SoftwareFabric, "1.21.11"); err != nil || r.Legacy {
		t.Errorf("indeks varken eski jar seçildi: %+v %v", r, err)
	}
}

// ── Bağımlılık ──────────────────────────────────────────────────────────────

func TestFindDepLocations(t *testing.T) {
	near, art := t.TempDir(), t.TempDir()
	l := Locator{DepDirs: []string{art}}
	name := "fabric-api-0.141.6+1.21.11.jar"

	if got := l.FindDep(name, near); got != "" {
		t.Fatalf("olmayan dosya bulundu: %q", got)
	}
	// Eski çevrimdışı paketin önbellek adı: "<16 hane>-" + "+" -> "_2B".
	cached := filepath.Join(art, "0123456789abcdef-fabric-api-0.141.6_2B1.21.11.jar")
	writeJar(t, cached, "fabric-api", "0.141.6+1.21.11")
	if got := l.FindDep(name, near); got != cached {
		t.Errorf("önbellek adlı kopya bulunamadı: %q", got)
	}
	// mcos-install'ın AYNI adla koyduğu kopya önbellek adından önce gelir.
	same := filepath.Join(art, name)
	writeJar(t, same, "fabric-api", "0.141.6+1.21.11")
	if got := l.FindDep(name, near); got != same {
		t.Errorf("aynı adlı kopya seçilmedi: %q", got)
	}
	// İndeksin kendi klasörü en önce.
	n := filepath.Join(near, name)
	writeJar(t, n, "fabric-api", "0.141.6+1.21.11")
	if got := l.FindDep(name, near); got != n {
		t.Errorf("indeks klasörü öncelikli değil: %q", got)
	}
	// Boş dosya sayılmaz.
	empty := t.TempDir()
	writeFile(t, filepath.Join(empty, name), "")
	if got := (Locator{}).FindDep(name, empty); got != "" {
		t.Errorf("boş dosya kabul edildi: %q", got)
	}
}

// ── fabric-api hazırlığı ───────────────────────────────────────────────────

func fabricResult(t *testing.T, depDir, mc, dep string) Result {
	p := filepath.Join(depDir, dep)
	writeJar(t, p, "fabric-api", "0.150.0+"+mc)
	return Result{Loader: Fabric, MC: mc, Dep: dep, DepPath: p}
}

// Sürümü değiştirilen sunucu: mods/ içinde 1.21.11'in fabric-api'si kalmış.
// Eski kod "fabric-api var" deyip geçiyordu ve 26.3 sunucusu açılmazdı.
func TestPrepareFabricAPIReplacesWrongVersion(t *testing.T) {
	mods, src := t.TempDir(), t.TempDir()
	oldAPI := filepath.Join(mods, "fabric-api-0.141.6+1.21.11.jar")
	writeJar(t, oldAPI, "fabric-api", "0.141.6+1.21.11")
	// Adı kalıba uyan ama BAŞKA bir mod: dokunulmamalı.
	other := filepath.Join(mods, "harika-fabric-api-eklentisi-1.0.jar")
	writeJar(t, other, "harika", "1.0")

	r := fabricResult(t, src, "26.3", "fabric-api-0.150.0+26.3.jar")
	note, err := PrepareFabricAPI(mods, r, "26.3", nil)
	if err != nil {
		t.Fatalf("hazırlanamadı: %v", err)
	}
	if !usable(filepath.Join(mods, r.Dep)) {
		t.Error("26.3'ün fabric-api'si kopyalanmadı")
	}
	if usable(oldAPI) {
		t.Error("1.21.11'in fabric-api'si kaldırılmadı — 26.3 sunucusu açılmaz")
	}
	if !usable(other) {
		t.Error("fabric-api OLMAYAN bir mod silindi")
	}
	if got := FabricAPIsIn(mods); len(got) != 1 {
		t.Errorf("mods/ içinde %d fabric-api kaldı: %v (not: %s)", len(got), got, note)
	}
}

// Aynı Minecraft sürümü için zaten bir fabric-api varsa (kullanıcı
// güncellemiş olabilir) dokunulmaz ve ağa çıkılmaz.
func TestPrepareFabricAPIKeepsFittingOne(t *testing.T) {
	mods := t.TempDir()
	mine := filepath.Join(mods, "fabric-api-0.151.0+26.3.jar")
	writeJar(t, mine, "fabric-api", "0.151.0+26.3")
	r := Result{Loader: Fabric, MC: "26.3", Dep: "fabric-api-0.150.0+26.3.jar"}
	_, err := PrepareFabricAPI(mods, r, "26.3", func() (string, error) {
		t.Error("uygun fabric-api varken indirme denendi")
		return "", nil
	})
	if err != nil || !usable(mine) || usable(filepath.Join(mods, r.Dep)) {
		t.Errorf("uygun kopya korunmadı: %v", err)
	}
}

// Doğrusu yerelde de yok, indirme de olmuyor: HATA, yanlış sürüm yerinde.
func TestPrepareFabricAPIFailsWithReason(t *testing.T) {
	mods := t.TempDir()
	oldAPI := filepath.Join(mods, "fabric-api.jar") // eski daemon'un koyduğu ad
	writeJar(t, oldAPI, "fabric-api", "0.141.6+1.21.11")
	r := Result{Loader: Fabric, MC: "26.3", Dep: "fabric-api-0.150.0+26.3.jar"}
	_, err := PrepareFabricAPI(mods, r, "26.3", func() (string, error) {
		return "", os.ErrDeadlineExceeded
	})
	if err == nil {
		t.Fatal("doğru fabric-api yokken başarı döndü")
	}
	if !strings.Contains(err.Error(), "fabric-api-0.150.0+26.3.jar") ||
		!strings.Contains(err.Error(), "1.21.11") {
		t.Errorf("ileti nedeni söylemiyor: %v", err)
	}
	if !usable(oldAPI) {
		t.Error("yerine konacak kopya yokken kullanıcının dosyası silindi")
	}
}

// Sürümü okunamayan (fabric.mod.json'suz) bir fabric-api: yerine konacak
// doğru kopya varsa değiştirilir, yoksa olduğu gibi bırakılır.
func TestPrepareFabricAPIUnknownVersion(t *testing.T) {
	mods, src := t.TempDir(), t.TempDir()
	unk := filepath.Join(mods, "fabric-api-test.jar")
	writeFile(t, unk, "PK sahte")
	if _, err := PrepareFabricAPI(mods, Result{Loader: Fabric, Dep: "fabric-api-9+26.3.jar"}, "26.3",
		func() (string, error) { return "", os.ErrNotExist }); err != nil || !usable(unk) {
		t.Fatalf("yerine konacak yokken: %v, dosya duruyor=%v", err, usable(unk))
	}
	r := fabricResult(t, src, "26.3", "fabric-api-9+26.3.jar")
	if _, err := PrepareFabricAPI(mods, r, "26.3", nil); err != nil {
		t.Fatal(err)
	}
	if usable(unk) || !usable(filepath.Join(mods, r.Dep)) {
		t.Error("sürümü bilinmeyen kopya doğru kopyayla değiştirilmedi")
	}
}
