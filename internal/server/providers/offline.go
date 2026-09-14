package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Bu dosya, MCOS'un İNTERNETSİZ çalışabilmesini sağlar.
//
// ── Sorun ───────────────────────────────────────────────────────────────────
// Sunucu oluşturmanın HER adımı ağa gidiyordu: sunucu jar'ı, Fabric yükleyici,
// modlar, hatta Java'nın kendisi. İnternet yoksa hiçbir sunucu kurulamıyordu.
// Ne bir önbellek, ne gömülü bir yedek, ne de "çevrimdışı kip" vardı.
//
// ── Çözüm ───────────────────────────────────────────────────────────────────
// Tek bir yerel ARTEFAKT DEPOSU. downloadTo() ağa çıkmadan ÖNCE buraya bakar;
// dosya varsa kopyalar ve internet hiç gerekmez. Depo iki kaynaktan dolar:
//
//   1. Kurulum sırasında mcos-install tarafından tohumlanır (imajla gelen
//      Fabric + ViaFabric + JRE paketi).
//   2. Başarılı her indirme buraya da yazılır — bir kez indirilen şey bir
//      daha indirilmez. Aynı sürümden ikinci sunucu kurmak artık ağ
//      gerektirmez.
//
// ── Neden URL'e göre anahtarlanıyor? ────────────────────────────────────────
// Depo, URL'in SHA-256 özetiyle adreslenir. Böylece sağlayıcıların (Paper,
// Fabric, Forge…) hiçbiri değişmek zorunda kalmaz: her biri kendi URL'ini
// üretmeye devam eder, önbellek araya şeffaf biçimde girer.

// CacheDir is where offline artifacts live.
//
// /data kalıcı bölümdedir: RAM'de çalışan canlı sistemde bile kurulum
// sırasında tohumlanan paket buradan okunur.
var CacheDir = "/data/artifacts"

// cachePath returns the on-disk name for a URL.
//
// Ad iki parçadır: okunabilir dosya adı + URL özetinin ilk 16 hanesi. Özet,
// aynı dosya adının farklı sürümlerini ayırır (ör. iki ayrı "server.jar");
// okunabilir kısım ise depoya bakan bir insanın ne olduğunu anlamasını sağlar.
func cachePath(url string) string {
	sum := sha256.Sum256([]byte(url))
	name := filepath.Base(url)
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	name = sanitizeName(name)
	if name == "" {
		name = "artifact"
	}
	return filepath.Join(CacheDir, hex.EncodeToString(sum[:8])+"-"+name)
}

// sanitizeName strips characters that are unsafe in a file name.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 96 {
		out = out[len(out)-96:]
	}
	return out
}

// cacheLookup copies a cached artifact to dst. Returns false if not cached.
func cacheLookup(url, dst string) bool {
	src := cachePath(url)
	fi, err := os.Stat(src)
	if err != nil || fi.Size() == 0 {
		return false
	}
	if err := copyFile(src, dst); err != nil {
		return false
	}
	return true
}

// cacheStore saves a freshly downloaded file for next time.
//
// Hata YOK SAYILIR: önbelleğe yazamamak (disk dolu, salt okunur /data)
// indirmeyi başarısız saymamalı — kullanıcı istediği dosyayı zaten aldı.
func cacheStore(url, src string) {
	dst := cachePath(url)
	if err := os.MkdirAll(CacheDir, 0o755); err != nil {
		return
	}
	// Geçici ada yaz, sonra taşı: yarım kalmış bir kopya bir sonraki
	// açılışta "önbellekte var" sanılıp bozuk jar olarak kullanılırdı.
	tmp := dst + ".tmp"
	if err := copyFile(src, tmp); err != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, dst)
}

// ErrOffline is returned when an artifact is needed but there is no network
// and no cached copy.
//
// Ayrı bir hata türü: arayüz bunu "indirilemedi" genel hatasından ayırıp
// kullanıcıya ne yapabileceğini söyleyebilsin.
var ErrOffline = errors.New("internet yok ve bu dosya çevrimdışı depoda bulunamadı")

// CachedArtifacts lists what is available offline, for the UI.
func CachedArtifacts() []string {
	ents, err := os.ReadDir(CacheDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		// Özet önekini at, insan tarafında okunabilir adı bırak.
		n := e.Name()
		if i := strings.Index(n, "-"); i == 16 {
			n = n[i+1:]
		}
		out = append(out, n)
	}
	return out
}
