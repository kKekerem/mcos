package java

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"mcos/internal/model"
)

// Bu dosya, İMAJLA GELEN (gömülü) Java çalışma zamanlarını tanır.
//
// ── Ölçülen sorun ───────────────────────────────────────────────────────────
// QEMU'da, 2026-09-25 imajıyla, ağ kapalıyken ölçüldü:
//
//	ls /usr/lib/jvm                      -> No such file or directory
//	mcosctl java resolve 1.21.11         -> requires Java 21 (installed=false)
//	mcosctl java install 21              -> dial tcp: lookup api.adoptium.net ... CIKIS=1
//
// Yani ilk açılışta hiçbir Java yoktu; Java 21 isteyen her sunucu önce
// ~200 MB'lık bir JDK indirmek zorundaydı. İnternetsiz bir makinede hiçbir
// modern sunucu kurulamıyordu. Daha kötüsü: çevrimdışı paketteki JRE
// (/data/artifacts/...-OpenJDK21U-jre...tar.gz) o ölçümde DİSKTE DURUYORDU,
// ama bu paket onu hiç okumuyordu — kurulum api.adoptium.net'e gidiyor,
// çevrimdışı depoya (providers.cacheLookup) hiç bakmıyordu.
//
// ── Çözüm ───────────────────────────────────────────────────────────────────
// post-build.sh, SHA-256'sı doğrulanmış Temurin JRE'yi derleme zamanında
// rootfs'e açar: /usr/lib/jvm/temurin-<major>-jre. Bu dosya o dizini
// "KURULU" bir çalışma zamanı olarak tanır:
//
//   - Get/List/Resolve onu kayıt defterinde (index.json) olmasa da görür.
//   - Install/Ensure aynı ana sürüm için hiçbir şey İNDİRMEZ, onu döndürür.
//   - Kayıt defterine YAZILMAZ: /data kalıcıdır, rootfs ise her imaj
//     güncellemesinde değişir. Yazılsaydı eski imajın sürüm dizesi yeni
//     imajda "kurulu" diye görünmeye devam ederdi.

// BuiltinRoot, imajla gelen JRE'lerin bulunduğu dizindir.
//
// post-build.sh ile AYNI yol olmak ZORUNDA; scripts/test-java-builtin.sh
// ikisinin ayrışmadığını denetler.
const BuiltinRoot = "/usr/lib/jvm"

// builtinPattern, BuiltinRoot altında gömülü sayılan dizinlerin kalıbıdır.
//
// Neden /usr/lib/jvm altındaki HER dizin değil: mcosd geliştirici
// makinelerinde ve mcos-node (Linux) üzerinde de çalışıyor. Oradaki
// /usr/lib/jvm/java-17-openjdk-amd64 gibi dağıtım JDK'ları "imajla gelen"
// değildir; onları gömülü saymak Kaldır'ın reddedilmesi gibi yanlış
// davranışlar doğururdu. Adoptium'un apt paketi "temurin-21-jre-amd64" adını
// kullanır; bu kalıp onunla da EŞLEŞMEZ.
const builtinPattern = "temurin-*-jre"

// ErrBuiltinRemove, gömülü bir Java kaldırılmak istendiğinde döner.
//
// Sessizce "tamam" demek yanlış olurdu: dosyalar RAM'deki rootfs'te
// duruyor, silinse bile bir sonraki açılışta geri gelir.
var ErrBuiltinRemove = errors.New("bu Java sürümü sistemle gömülü geliyor, kaldırılamaz")

// defaultBuiltinRoot, bu platformda gömülü Java aranacak kökü verir.
//
// Windows'ta (mcos-node) imaj yok; orada boş dönmek taramayı tamamen kapatır.
func defaultBuiltinRoot() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	return BuiltinRoot
}

// SetBuiltinRoot, gömülü Java'nın aranacağı kökü değiştirir; "" taramayı
// kapatır. Varsayılan BuiltinRoot'tur. Başka paketlerin testleri (daemon,
// sunucu) gerçek /usr/lib/jvm'e dokunmadan gömülü Java'yı sınayabilsin diye
// dışa açık.
func (m *Manager) SetBuiltinRoot(root string) { m.builtinRoot = root }

// Builtin, imajla gelen Java çalışma zamanlarını ana sürüme göre sıralı
// döndürür. Kök yoksa (geliştirici makinesi) boş döner.
//
// Her çağrıda diske bakar: durum yoklaması saniyede bir çağırsa bile bu bir
// Glob + küçük bir "release" dosyası okumasıdır. Önbellek tutmamak, testlerin
// ve elle eklenmiş bir JRE'nin yeniden başlatma gerektirmemesini sağlar.
func (m *Manager) Builtin() []model.JavaRuntime {
	return scanBuiltin(m.builtinRoot)
}

// builtinFor, bir ana sürümün gömülü çalışma zamanını döndürür.
func (m *Manager) builtinFor(major int) *model.JavaRuntime {
	for _, rt := range m.Builtin() {
		if rt.Major == major {
			r := rt
			return &r
		}
	}
	return nil
}

// isBuiltinHome, verilen JAVA_HOME'un gömülü bir dizin olup olmadığını söyler.
func (m *Manager) isBuiltinHome(home string) bool {
	home = filepath.Clean(home)
	for _, rt := range m.Builtin() {
		if filepath.Clean(rt.Path) == home {
			return true
		}
	}
	return false
}

// scanBuiltin, root altındaki gömülü JRE'leri bulur.
//
// Ana sürüm "java -version" ÇALIŞTIRILMADAN, JDK'nın kendi "release"
// dosyasından okunur: bu yol durum yoklamasında her saniye çağrılıyor ve
// her seferinde bir JVM başlatmak (ölçülen ~40 MB, ~0,1 sn) kabul edilemez.
// Aynı ana sürümden iki dizin varsa yalnızca biri (sözlük sırasında son
// gelen) döner: çağıranlar ana sürüm başına TEK çalışma zamanı bekliyor.
func scanBuiltin(root string) []model.JavaRuntime {
	if root == "" {
		return nil
	}
	dirs, err := filepath.Glob(filepath.Join(root, builtinPattern))
	if err != nil || len(dirs) == 0 {
		return nil
	}
	sort.Strings(dirs)
	byMajor := map[int]model.JavaRuntime{}
	for _, home := range dirs {
		rt, ok := builtinRuntime(home)
		if !ok {
			continue
		}
		byMajor[rt.Major] = rt
	}
	out := make([]model.JavaRuntime, 0, len(byMajor))
	for _, rt := range byMajor {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Major < out[j].Major })
	return out
}

// builtinRuntime, tek bir gömülü dizini doğrular ve tanımlar.
//
// Üç şart birden aranır, çünkü her biri ayrı bir yarım-kopya hatasını yakalar:
// çalıştırılabilir bin/java (izinleri bozuk açılmış arşiv), lib/modules
// (sınıf görüntüsü olmadan JVM "Error occurred during initialization of boot
// layer" ile düşer) ve okunabilir bir ana sürüm (release dosyası).
func builtinRuntime(home string) (model.JavaRuntime, bool) {
	javaBin := filepath.Join(home, "bin", javaExe())
	fi, err := os.Stat(javaBin)
	if err != nil || fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
		return model.JavaRuntime{}, false
	}
	if _, err := os.Stat(filepath.Join(home, "lib", "modules")); err != nil {
		return model.JavaRuntime{}, false
	}
	relPath := filepath.Join(home, "release")
	rel, err := readRelease(relPath)
	if err != nil {
		return model.JavaRuntime{}, false
	}
	major := releaseMajor(rel["JAVA_VERSION"])
	if major == 0 {
		return model.JavaRuntime{}, false
	}
	rt := model.JavaRuntime{
		Major:   major,
		Version: releaseVersionLine(rel),
		Vendor:  releaseVendor(rel),
		Path:    home,
		JavaBin: javaBin,
		Builtin: true,
	}
	if st, err := os.Stat(relPath); err == nil {
		rt.InstalledAt = st.ModTime()
	}
	return rt, true
}

// readRelease, JDK'nın KEY="değer" biçimli release dosyasını okur.
func readRelease(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // MODULES satırı uzun olabilir
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[k] = strings.Trim(v, `"`)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// releaseMajor, "21.0.12.1" / "1.8.0_402" biçimlerinden ana sürümü çıkarır.
func releaseMajor(v string) int {
	if v == "" {
		return 0
	}
	return majorFromVersion(fmt.Sprintf("version %q", v))
}

// releaseVersionLine, release dosyasından "java -version"ın İLK satırını
// yeniden kurar.
//
// İndirilen çalışma zamanlarının Version alanı probeVersion ile o satırdan
// alınıyor; gömülü olan da aynı biçimde görünmeli ki Yazılım ekranı ve
// mcosctl iki kaynağı tutarlı listelesin. Temurin 21.0.12.1 ile ölçüldü:
//
//	java -version  -> openjdk version "21.0.12.1" 2026-08-18 LTS
//	release        -> JAVA_VERSION="21.0.12.1" JAVA_VERSION_DATE="2026-08-18"
//	                  JAVA_RUNTIME_VERSION="21.0.12.1+1-LTS"
func releaseVersionLine(rel map[string]string) string {
	line := fmt.Sprintf("openjdk version %q", rel["JAVA_VERSION"])
	if d := rel["JAVA_VERSION_DATE"]; d != "" {
		line += " " + d
	}
	if strings.HasSuffix(rel["JAVA_RUNTIME_VERSION"], "-LTS") {
		line += " LTS"
	}
	return line
}

// releaseVendor, IMPLEMENTOR alanını kısa üretici adına çevirir.
func releaseVendor(rel map[string]string) string {
	impl := strings.ToLower(rel["IMPLEMENTOR"])
	switch {
	case strings.Contains(impl, "adoptium"):
		return "temurin" // indirilen kurulumlarla AYNI ad
	case impl == "":
		return "gömülü"
	default:
		return rel["IMPLEMENTOR"]
	}
}
