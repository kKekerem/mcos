package java

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mcos/internal/model"
	"mcos/internal/store"
)

// Temurin 21.0.12.1 JRE'nin gerçek release dosyasından alınan alanlar
// (OpenJDK21U-jre_x64_linux_hotspot_21.0.12.1_1.tar.gz, SHA-256 2413149700df…).
// MODULES kısaltıldı; okuyucu uzun satırları da taşıyabilmeli.
const temurin21Release = `IMPLEMENTOR="Eclipse Adoptium"
IMPLEMENTOR_VERSION="Temurin-21.0.12.1+1"
JAVA_RUNTIME_VERSION="21.0.12.1+1-LTS"
JAVA_VERSION="21.0.12.1"
JAVA_VERSION_DATE="2026-08-18"
LIBC="gnu"
MODULES="java.base java.compiler java.datatransfer java.xml java.prefs java.desktop"
OS_ARCH="x86_64"
OS_NAME="Linux"
IMAGE_TYPE="JRE"
`

// fakeJRE, root/name altında gömülü JRE'nin asgari düzenini kurar:
// çalıştırılabilir bin/java, lib/modules ve release. bin/java gerçek bir
// JVM değildir; yalnızca Detect'in sürüm yoklamasına cevap veren bir betik.
func fakeJRE(t *testing.T, root, name, release string) string {
	t.Helper()
	home := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'openjdk version \"21.0.12.1\" 2026-08-18 LTS' >&2\n"
	if err := os.WriteFile(filepath.Join(home, "bin", "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "lib", "modules"), []byte("jimage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if release != "" {
		if err := os.WriteFile(filepath.Join(home, "release"), []byte(release), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// noNetwork, testi AĞA ÇIKAN her isteği kaydeden bir taşıyıcıdır. İstek
// gelirse hata döner; testler "hiç istek yapılmadı" diye bunu denetler.
type noNetwork struct {
	mu   sync.Mutex
	urls []string
}

func (n *noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.mu.Lock()
	n.urls = append(n.urls, r.URL.String())
	n.mu.Unlock()
	return nil, errors.New("test: ağ kapalı")
}

func (n *noNetwork) calls() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.urls...)
}

// newTestManager, geçici /data ve geçici gömülü kök ile bir yönetici kurar.
func newTestManager(t *testing.T) (*Manager, *noNetwork, string) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(st, nil)
	net := &noNetwork{}
	m.client = &http.Client{Transport: net}
	root := filepath.Join(t.TempDir(), "jvm")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	m.SetBuiltinRoot(root)
	// Geliştirme makinesindeki dist/java arşivi sınamaya karışmasın.
	old := LocalArchiveDirs
	LocalArchiveDirs = []string{t.TempDir()}
	t.Cleanup(func() { LocalArchiveDirs = old })
	return m, net, root
}

// Kullanıcının isteği: "OS'un içine Java'yı göm, direkt kurulu gelsin Java 21".
// Gömülü JRE, index.json'da hiçbir kayıt yokken (ilk açılış) KURULU sayılmalı.
func TestBuiltinJava21IsInstalledOnFirstBoot(t *testing.T) {
	m, net, root := newTestManager(t)
	home := fakeJRE(t, root, "temurin-21-jre", temurin21Release)

	rts, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(rts) != 1 || rts[0].Major != 21 || !rts[0].Builtin {
		t.Fatalf("List = %+v, gömülü Java 21 bekleniyordu", rts)
	}
	if rts[0].Path != home || rts[0].JavaBin != filepath.Join(home, "bin", "java") {
		t.Fatalf("yol yanlış: %+v", rts[0])
	}
	if rts[0].Vendor != "temurin" {
		t.Fatalf("üretici %q, temurin bekleniyordu", rts[0].Vendor)
	}

	major, installed, err := m.Resolve("1.21.11")
	if err != nil || major != 21 || !installed {
		t.Fatalf("Resolve(1.21.11) = %d, %v, %v; 21, true bekleniyordu", major, installed, err)
	}
	if c := net.calls(); len(c) != 0 {
		t.Fatalf("listeleme ağa çıktı: %v", c)
	}
}

// Sihirbazın "Gerekli Java'yı kur" adımı ve sunucu başlatma (Ensure →
// BindForServer), gömülü ana sürüm için AĞA HİÇ ÇIKMAMALI ve index.json'a
// yazmamalı. Ölçülen eski davranış: ağ kapalıyken "java install 21"
// api.adoptium.net'e gidip "lookup ... CIKIS=1" ile düşüyordu.
func TestBuiltinJava21NeedsNoDownload(t *testing.T) {
	m, net, root := newTestManager(t)
	home := fakeJRE(t, root, "temurin-21-jre", temurin21Release)

	rt, err := m.Install(21)
	if err != nil {
		t.Fatalf("Install(21) ağsız başarısız: %v", err)
	}
	if !rt.Builtin || rt.Path != home {
		t.Fatalf("Install(21) = %+v, gömülü JRE bekleniyordu", rt)
	}
	if _, err := m.Ensure(21); err != nil {
		t.Fatalf("Ensure(21): %v", err)
	}
	srv := &model.Server{JavaMajor: 21, RAMMB: 1024}
	bin, _, err := m.BindForServer(srv)
	if err != nil {
		t.Fatalf("BindForServer: %v", err)
	}
	if bin != filepath.Join(home, "bin", "java") {
		t.Fatalf("sunucu %q ile başlatılacaktı, gömülü java bekleniyordu", bin)
	}
	if c := net.calls(); len(c) != 0 {
		t.Fatalf("gömülü Java varken ağa çıkıldı: %v", c)
	}
	if len(m.ProgressMap()) != 0 {
		t.Fatalf("indirme ilerlemesi açıldı: %+v", m.ProgressMap())
	}
	if _, err := os.Stat(m.store.Paths.JavaIndex()); !os.IsNotExist(err) {
		t.Fatalf("gömülü Java index.json'a yazıldı (err=%v); imaj güncellenince eski kayıt kalır", err)
	}
}

// Eski Minecraft sürümleri için Java 8/17 indirme yolu çalışmaya DEVAM
// etmeli: gömülü 21, başka ana sürümlerin kurulumunu engellememeli.
func TestOtherMajorsStillDownload(t *testing.T) {
	m, net, root := newTestManager(t)
	fakeJRE(t, root, "temurin-21-jre", temurin21Release)

	for _, major := range []int{17, 8} {
		if _, err := m.Install(major); err == nil {
			t.Fatalf("Install(%d) ağ kapalıyken başarılı döndü", major)
		}
	}
	// Her ana sürüm için önce JRE, o olmazsa JDK denenir (bkz. temurinURL).
	c := net.calls()
	if len(c) != 4 || !strings.Contains(c[0], "/latest/17/") || !strings.Contains(c[0], "/jre/") ||
		!strings.Contains(c[1], "/jdk/") || !strings.Contains(c[2], "/latest/8/") {
		t.Fatalf("Java 17/8 için Adoptium'a (önce JRE) gidilmedi: %v", c)
	}
	if _, ok, _ := m.Get(17); ok {
		t.Fatal("başarısız indirme Java 17'yi kurulu gösterdi")
	}
}

// Önceki imajlarda indirilmiş /data/java/temurin-21 kopyası, gömülü olanı
// gölgelememeli; listede ana sürüm başına TEK satır olmalı.
func TestBuiltinShadowsRegisteredSameMajor(t *testing.T) {
	m, _, root := newTestManager(t)
	home := fakeJRE(t, root, "temurin-21-jre", temurin21Release)
	old := filepath.Join(m.store.Paths.JavaDir(), "temurin-21", "jdk-21.0.5+11")
	if err := m.register(model.JavaRuntime{Major: 21, Vendor: "temurin", Path: old,
		JavaBin: filepath.Join(old, "bin", "java"), Version: `openjdk version "21.0.5"`}); err != nil {
		t.Fatal(err)
	}
	if err := m.register(model.JavaRuntime{Major: 17, Vendor: "temurin", Path: "/data/java/temurin-17",
		JavaBin: "/data/java/temurin-17/bin/java"}); err != nil {
		t.Fatal(err)
	}

	rt, ok, err := m.Get(21)
	if err != nil || !ok || rt.Path != home {
		t.Fatalf("Get(21) = %+v, %v, %v; gömülü bekleniyordu", rt, ok, err)
	}
	rts, _ := m.List()
	if len(rts) != 2 || rts[0].Major != 17 || rts[1].Major != 21 || !rts[1].Builtin {
		t.Fatalf("List = %+v; [17 indirilmiş, 21 gömülü] bekleniyordu", rts)
	}
}

// Gömülü Java kaldırılamaz; ama aynı ana sürümün indirilmiş (gölgedeki)
// kopyası kaldırılabilmeli ki eski kurulumların diski geri alınabilsin.
func TestRemoveBuiltin(t *testing.T) {
	m, _, root := newTestManager(t)
	home := fakeJRE(t, root, "temurin-21-jre", temurin21Release)

	if err := m.Remove(21); !errors.Is(err, ErrBuiltinRemove) {
		t.Fatalf("Remove(21) = %v, ErrBuiltinRemove bekleniyordu", err)
	}

	old := filepath.Join(m.store.Paths.JavaDir(), "temurin-21")
	if err := os.MkdirAll(filepath.Join(old, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.register(model.JavaRuntime{Major: 21, Vendor: "temurin", Path: old,
		JavaBin: filepath.Join(old, "bin", "java")}); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(21); err != nil {
		t.Fatalf("gölgedeki kopya kaldırılamadı: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("indirilmiş kopya diskte kaldı (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "java")); err != nil {
		t.Fatalf("gömülü JRE silindi: %v", err)
	}
	if rt, ok, _ := m.Get(21); !ok || !rt.Builtin {
		t.Fatalf("kaldırmadan sonra gömülü Java 21 görünmüyor: %+v", rt)
	}
}

// Yarım açılmış ya da yanlış adlı dizinler gömülü SAYILMAMALI: yoksa
// "kurulu" denip sunucu başlatılırken JVM düşer.
func TestScanBuiltinRejectsIncomplete(t *testing.T) {
	root := t.TempDir()

	noModules := fakeJRE(t, root, "temurin-17-jre", strings.ReplaceAll(temurin21Release, `"21.0.12.1"`, `"17.0.9"`))
	if err := os.Remove(filepath.Join(noModules, "lib", "modules")); err != nil {
		t.Fatal(err)
	}
	notExec := fakeJRE(t, root, "temurin-11-jre", strings.ReplaceAll(temurin21Release, `"21.0.12.1"`, `"11.0.21"`))
	if err := os.Chmod(filepath.Join(notExec, "bin", "java"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeJRE(t, root, "temurin-8-jre", "") // release yok
	// Adoptium apt paketinin dizin adı; gömülü kalıbıyla eşleşmemeli.
	fakeJRE(t, root, "temurin-25-jre-amd64", strings.ReplaceAll(temurin21Release, `"21.0.12.1"`, `"25.0.1"`))

	if got := scanBuiltin(root); len(got) != 0 {
		t.Fatalf("eksik/yanlış dizinler gömülü sayıldı: %+v", got)
	}

	fakeJRE(t, root, "temurin-21-jre", temurin21Release)
	got := scanBuiltin(root)
	if len(got) != 1 || got[0].Major != 21 {
		t.Fatalf("scanBuiltin = %+v, yalnızca 21 bekleniyordu", got)
	}
	if scanBuiltin("") != nil || scanBuiltin(filepath.Join(root, "yok")) != nil {
		t.Fatal("boş/olmayan kök bir şey döndürdü")
	}
}

// Sürüm satırı, gerçek "java -version"ın ilk satırıyla AYNI olmalı (Temurin
// 21.0.12.1 ile ölçülen: openjdk version "21.0.12.1" 2026-08-18 LTS).
func TestReleaseVersionLineMatchesJavaVersion(t *testing.T) {
	root := t.TempDir()
	fakeJRE(t, root, "temurin-21-jre", temurin21Release)
	got := scanBuiltin(root)
	if len(got) != 1 {
		t.Fatalf("scanBuiltin = %+v", got)
	}
	const want = `openjdk version "21.0.12.1" 2026-08-18 LTS`
	if got[0].Version != want {
		t.Fatalf("Version = %q, %q bekleniyordu", got[0].Version, want)
	}
	if majorFromVersion(got[0].Version) != 21 {
		t.Fatalf("sürüm satırından ana sürüm çıkmıyor: %q", got[0].Version)
	}
}

// Detect, gömülü JRE'yi "host" olarak index.json'a YAZMAMALI. Yazsaydı
// kalıcı bölümde imaja bağlı bir kayıt kalır ve bir sonraki imajda eski
// sürüm dizesiyle görünürdü.
//
// Üretici BİLEREK Adoptium değil: Detect'in eski "Temurin kurulumunu ezme"
// denetimi (Vendor == "temurin") Adoptium yapısını tesadüfen koruyordu; bu
// test gömülü dizinin KENDİSİNİN atlandığını sınar, üreticiden bağımsız.
func TestDetectSkipsBuiltin(t *testing.T) {
	m, _, root := newTestManager(t)
	home := fakeJRE(t, root, "temurin-21-jre",
		strings.ReplaceAll(temurin21Release, `IMPLEMENTOR="Eclipse Adoptium"`, `IMPLEMENTOR="Baska Uretici"`))
	t.Setenv("JAVA_HOME", home)
	t.Setenv("PATH", "")

	if _, err := m.Detect(); err != nil {
		t.Fatal(err)
	}
	ix, err := m.store.LoadJavaIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Runtimes) != 0 {
		t.Fatalf("Detect gömülü JRE'yi kayıt defterine yazdı: %+v", ix.Runtimes)
	}
}

// TestBuiltinRealJRE, post-build'in GERÇEKTEN açtığı JRE üzerinde çalışır
// (scripts/test-java-builtin.sh MCOS_JAVA_REAL_ROOT=<hedef>/usr/lib/jvm ile
// çağırır). Sahte betik değil gerçek JVM: gömülü taramanın release'ten
// kurduğu sürüm satırı, "java -version"ın gerçek ilk satırıyla birebir
// aynı olmalı ve yönetici Java 21'i ağsız "kurulu" saymalı.
func TestBuiltinRealJRE(t *testing.T) {
	root := os.Getenv("MCOS_JAVA_REAL_ROOT")
	if root == "" {
		t.Skip("MCOS_JAVA_REAL_ROOT verilmedi (scripts/test-java-builtin.sh verir)")
	}
	m, net, _ := newTestManager(t)
	m.SetBuiltinRoot(root)

	rt, ok, err := m.Get(21)
	if err != nil || !ok || !rt.Builtin {
		t.Fatalf("gerçek JRE gömülü Java 21 sayılmadı: %+v, %v, %v", rt, ok, err)
	}
	if real := probeVersion(rt.JavaBin); real != rt.Version {
		t.Fatalf("release'ten kurulan sürüm %q, gerçek java -version %q", rt.Version, real)
	}
	if _, err := m.Install(21); err != nil {
		t.Fatalf("Install(21): %v", err)
	}
	if c := net.calls(); len(c) != 0 {
		t.Fatalf("gerçek gömülü JRE varken ağa çıkıldı: %v", c)
	}
}
