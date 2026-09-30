package proxy

import (
	"bytes"
	"os"
	"path/filepath"
)

// PluginJar, MCOS'un Velocity eklentisinin (mods/mcos-link-velocity) sabit
// adıdır: imajda /usr/lib/mcos/mods/link, geliştirmede dist/mods/link altında
// bu adla durur, proxy'nin plugins/ klasörüne de bu adla kopyalanır.
//
// Eklenti neden var: arka uç listesi değişince Velocity'yi yeniden başlatmak
// bağlı bütün oyuncuları düşürüyordu. Eklenti listeyi koordinatörden
// (/link/proxy) okuyup çalışan proxy'ye kendisi ekler/çıkarır.
const PluginJar = "mcos-link-velocity.jar"

// InstallPlugin puts src into <dir>/plugins/PluginJar; true = eklenti yerinde.
//
// src boşsa (imajda eklenti yok) yalnızca daha önce kopyalanmış bir eklenti
// olup olmadığına bakılır. Dosya zaten aynıysa HİÇBİR ŞEY yazılmaz: bu işlev
// her uzlaştırma turunda çağrılır.
//
// Yazma geçici dosya + yeniden adlandırmayla yapılır: çalışan Velocity eski
// jar'ı açık tutuyor olabilir; yerinde üzerine yazmak, henüz yüklenmemiş
// sınıfları bozuk okutup proxy'yi çalışırken düşürebilirdi. Yeni sürüm bir
// sonraki açılışta devreye girer.
func InstallPlugin(dir, src string) (bool, error) {
	dst := filepath.Join(dir, "plugins", PluginJar)
	if src == "" {
		return isJar(dst), nil
	}
	want, err := os.ReadFile(src)
	if err != nil || !jarBytes(want) {
		// Kaynak bozuk: varsa eski kopya kullanılmaya devam eder.
		return isJar(dst), err
	}
	if have, err := os.ReadFile(dst); err == nil && bytes.Equal(have, want) {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return isJar(dst), err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, want, 0o644); err != nil {
		_ = os.Remove(tmp)
		return isJar(dst), err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return isJar(dst), err
	}
	return true, nil
}

// isJar: boş ya da yarım bir dosya eklenti SAYILMAZ. Sayılsaydı MCOS
// yeniden başlatmayı bırakır, Velocity ise eklentiyi yükleyemez ve yeni
// arka uçlar hiç eklenmezdi.
func isJar(p string) bool {
	b, err := os.ReadFile(p)
	return err == nil && jarBytes(b)
}

// jarBytes: jar bir zip'tir ("PK" ile başlar).
func jarBytes(b []byte) bool {
	return len(b) > 2 && b[0] == 'P' && b[1] == 'K'
}
