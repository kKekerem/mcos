package sshd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bu dosya /etc/shadow'daki TEK bir kullanıcının parola alanını değiştirir.
//
// ── Neden elle ve neden bu kadar dikkatli ───────────────────────────────────
// chpasswd imajda yok (bkz. shacrypt.go). Onun yaptığı işi burada yapıyoruz ve
// shadow dosyası hataya çok az tolerans gösterir:
//
//   - Diğer satırlar AYNEN korunmalı: sshd kullanıcısının "*" alanı bozulursa
//     OpenSSH ayrıcalık ayırmasıyla başlayamaz.
//   - Her satır 9 alandır (ad:özet:sondeğişim:min:max:uyarı:pasif:bitiş:ayrık).
//   - Yazım ATOMİK olmalı: yarım yazılmış bir shadow kök girişini tamamen
//     kilitler. Geçici dosyaya yazıp rename ediyoruz.
//   - "sondeğişim" alanı 0 OLMAMALI: OpenSSH (auth-shadow.c) sp_lstchg == 0'ı
//     "kök parolayı zorla değiştirtiyor" diye yorumlar ve girişte parola
//     değiştirme ister; toplu (ssh host komut) girişler başarısız olur.

// shadowFields is the fixed field count of an /etc/shadow line.
const shadowFields = 9

// daysSinceEpoch is the value of the "last change" field for now.
//
// Saat 1 Ocak 1970'te duruyorsa (RTC pili bitmiş bir makine) sonuç 0 olur ve
// yukarıdaki OpenSSH kuralı yüzünden zorunlu parola değişimi tetiklenirdi.
// O durumda alan BOŞ bırakılır; boş alan "yaşlanma yok" demektir.
func daysSinceEpoch(now time.Time) string {
	d := now.Unix() / 86400
	if d < 1 {
		return ""
	}
	return strconv.FormatInt(d, 10)
}

// replaceShadowEntry returns shadow content with user's hash replaced.
//
// Kullanıcının satırı yoksa sona eklenir. Kullanıcının diğer alanları
// (min/max/uyarı/...) korunur; yalnızca özet ve son değişim tarihi değişir —
// chpasswd'ın davranışı da budur.
func replaceShadowEntry(content, user, hash string, now time.Time) (string, error) {
	if user == "" || strings.ContainsAny(user, ":\n") {
		return "", fmt.Errorf("geçersiz kullanıcı adı: %q", user)
	}
	if hash == "" || strings.ContainsAny(hash, ":\n") {
		return "", errors.New("geçersiz parola özeti")
	}

	lines := strings.Split(content, "\n")
	// Sondaki satır sonu Split'te boş bir öğe üretir; onu ayrı tutuyoruz ki
	// dosyanın sonuna fazladan boş satır eklenmesin.
	trailingNL := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNL {
		lines = lines[:len(lines)-1]
	}

	found := false
	for i, ln := range lines {
		f := strings.Split(ln, ":")
		if f[0] != user {
			continue
		}
		if found {
			// Aynı kullanıcının ikinci satırı: getspnam İLKİNİ okur, yani
			// geçerli olan yukarıda değiştirdiğimizdir. İkincisine
			// dokunmuyoruz: bilmediğimiz bir satırı silmek, düzeltmekten
			// çok bozma riski taşır.
			continue
		}
		for len(f) < shadowFields {
			f = append(f, "")
		}
		f[1] = hash
		f[2] = daysSinceEpoch(now)
		lines[i] = strings.Join(f, ":")
		found = true
	}
	if !found {
		f := make([]string, shadowFields)
		f[0], f[1], f[2] = user, hash, daysSinceEpoch(now)
		lines = append(lines, strings.Join(f, ":"))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// writeShadowHash sets user's hash in the shadow file at path, atomically.
//
// Dosya yoksa oluşturulur (0600). Varsa izni korunur: Buildroot'un shadow'u
// 0600'dür ve daha gevşek bir izin parola özetlerini herkese okuturdu.
func writeShadowHash(path, user, hash string, now time.Time) error {
	var old []byte
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s okunamadı: %w", path, err)
		}
		old = b
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s okunamadı: %w", path, err)
	}

	body, err := replaceShadowEntry(string(old), user, hash, now)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(body), mode)
}

// writeFileAtomic writes via a temp file in the same directory + rename.
//
// fsync: kalıcı veri bölümünde (ext4) elektrik kesilirse rename'den sonra
// SIFIR baytlık bir dosya kalabilir; önce veriyi diske indiriyoruz.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("%s yazılamadı: %w", path, err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return fmt.Errorf("%s yazılamadı: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("%s yazılamadı: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("%s diske yazılamadı: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s yazılamadı: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("%s yazılamadı: %w", path, err)
	}
	ok = true
	return nil
}

// volatileMount reports whether dir lives on a RAM filesystem according to
// the given /proc/mounts content.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// MCOS'un kökü initramfs'tir; kalıcı bölüm (MCOS-DATA) bulunamazsa /data da
// RAM'de kalır. O durumda parola ve sunucu anahtarları yeniden başlatınca
// kaybolur. Bunu sessizce yapmak "dün koyduğum parola çalışmıyor" şikâyetine
// döner; panel kullanıcıya önceden söylesin diye tespit ediyoruz.
func volatileMount(mounts, dir string) bool {
	dir = filepath.Clean(dir)
	best, bestType := "", ""
	for _, ln := range strings.Split(mounts, "\n") {
		f := strings.Fields(ln)
		if len(f) < 3 {
			continue
		}
		mp := unescapeMount(f[1])
		if mp != "/" && dir != mp && !strings.HasPrefix(dir, mp+"/") {
			continue
		}
		// En uzun eşleşen bağlama noktası kazanır; eşitlikte SONRAKİ satır
		// (aynı noktaya üst üste bağlama) geçerlidir.
		if len(mp) >= len(best) {
			best, bestType = mp, f[2]
		}
	}
	switch bestType {
	case "rootfs", "tmpfs", "ramfs":
		return true
	}
	return false
}

// unescapeMount decodes the octal escapes (\040 = space) of /proc/mounts.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
