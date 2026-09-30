// Package java manages multiple JDK runtimes and the mapping from a Minecraft
// version to the Java major version it requires. The download/registry/runtime
// binding parts are added in the Java-manager milestone; the version mapping is
// here because the install wizard needs it from the start.
package java

import (
	"strconv"
	"strings"

	"mcos/internal/model"
)

// RequiredJavaMajor returns the Java major version a given Minecraft release
// needs. Rules (vanilla baseline; modloaders follow the same floor):
//
//	<= 1.16.5            -> Java 8
//	1.17.x              -> Java 16 (17 also works)
//	1.18 .. 1.20.4      -> Java 17
//	>= 1.20.5 (incl 1.21)-> Java 21
//	26.x ve sonrası      -> Java 25
//
// Unknown/blank versions default to the modern LTS (21).
//
// ── Yakalanan gerçek hata: 26.x sunucuları hiç açılmıyordu ──────────────────
// Mojang 1.21.11'den sonra takvim sürümlemesine geçti (26.1, 26.2, 26.3).
// Burada "1.x dışındaki her şey Java 21" yazıyordu; oysa Mojang'ın sürüm
// manifesti 26.1–26.3 için javaVersion.majorVersion = 25 diyor (2026-09-26'da
// piston-meta'dan okundu). İnternetle kurulan bir sunucu en yeni sürümü
// (26.3) alıyor, Java 21 ile başlatılıyor ve UnsupportedClassVersionError ile
// düşüyordu.
func RequiredJavaMajor(mcVersion string) int {
	major, minor, patch, ok := parseMC(mcVersion)
	if !ok {
		return 21
	}
	if major >= 26 {
		// Takvim sürümleri (26.1+). Mojang sonraki bir yılda tabanı yeniden
		// yükseltirse buraya yeni bir satır eklenir; o güne kadar 25 doğru.
		return 25
	}
	if major != 1 {
		return 21
	}
	switch {
	case minor <= 16:
		return 8
	case minor == 17:
		return 17
	case minor < 20:
		return 17
	case minor == 20:
		if patch >= 5 {
			return 21
		}
		return 17
	default: // minor >= 21
		return 21
	}
}

// parseMC parses "1.20.4" / "1.21" / "1.8.8" into numeric components.
func parseMC(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimSpace(v)
	// Strip pre-release/snapshot suffixes like "1.21-rc1".
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, false
	}
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, false
	}
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return major, minor, patch, true
}

// RaiseToRequired yükseltir: srv.JavaMajor, sürümün istediğinden düşükse
// gereken ana sürüme çekilir. Değiştiyse true döner (çağıran kaydeder).
//
// Neden gerekli: JavaMajor sunucu OLUŞTURULURKEN bir kez yazılıp saklanıyor.
// 26.x eşlemesi düzeltilmeden önce oluşturulmuş bir 26.3 sunucusunun
// kaydında 21 duruyor; yalnızca eşlemeyi düzeltmek o sunucuyu kurtarmazdı —
// her açılışta yine Java 21 ile başlatılıp düşerdi. ASLA düşürmez: kullanıcı
// bilerek daha yeni bir Java seçmiş olabilir. Elle bir java yolu (JavaPath)
// verilmişse de dokunmaz; o kullanıcının kararıdır.
func RaiseToRequired(srv *model.Server) bool {
	if srv == nil || srv.JavaPath != "" {
		return false
	}
	need := RequiredJavaMajor(srv.MCVersion)
	if srv.JavaMajor >= need {
		return false
	}
	srv.JavaMajor = need
	return true
}
