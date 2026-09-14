#!/bin/sh
# test-remote-logic.sh — uzaktan kontrol ve SSH'ın GÜVENLİK değişmezleri.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN BU TEST VAR
# ════════════════════════════════════════════════════════════════════════════
#
# Uzaktan kontrol köprüsü, sunucunun TAM DENETİMİNİ ağa açar: sunucu silme,
# dosya yazma, makineyi kapatma. Kullanıcı bunu playit üzerinden internete de
# açabilir.
#
# Bu yüzden buradaki bir gerileme, "bir özellik bozuldu"dan çok daha ağırdır:
# aynı Wi-Fi'deki (ya da tünel açıksa internetteki) herkes sistemi ele
# geçirebilir.
#
# Davranışın kendisi Go testlerinde sınanıyor (internal/remote/server_test.go,
# 12 test). Buradaki denetimler, o korumaların KODDAN KALDIRILMADIĞINI
# garanti eder — bir yeniden düzenleme sırasında sessizce kaybolmalarını
# engeller.
#
# Hiçbir ağ bağlantısı açmaz; yalnızca kaynak okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

SRV="$ROOT/internal/remote/server.go"
CRT="$ROOT/internal/remote/cert.go"
SSHD="$ROOT/internal/sshd/sshd.go"
SSHL="$ROOT/internal/sshd/sshd_linux.go"
HND="$ROOT/internal/daemon/handlers_remote.go"
CFG="$ROOT/internal/model/config.go"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$SRV" "$CRT" "$SSHD" "$SSHL" "$HND" "$CFG"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

# strip_comments, Go yorumlarını atar.
#
# NEDEN: bir kuralı ARAMAK, o kuralı ANLATAN yorumu bulmakla karıştırılmamalı.
# Yorumda "jeton denetleniyor" yazması, kodun gerçekten denetlediğini
# söylemez.
strip_comments() {
    sed 's://.*::' "$1"
}

echo "== 1. Jetonsuz dinleyici AÇILMAZ =="

# En önemli değişmez: jeton yoksa sunucu HİÇ kurulmamalı. Boş jeton sessizce
# "kimlik denetimi yok" anlamına gelseydi, bir yapılandırma hatası sistemi
# ağa açardı.
if strip_comments "$SRV" | grep -q 'TrimSpace(o.Token) == ""'; then
    if strip_comments "$SRV" | grep -A3 'TrimSpace(o.Token) == ""' | grep -q 'errors.New'; then
        pass "jeton boşsa sunucu kurulmuyor (hata dönüyor)"
    else
        fail "boş jeton denetleniyor ama HATA DÖNMÜYOR"
    fi
else
    fail "boş jeton denetimi YOK — jetonsuz dinleyici açılabilir"
fi

echo ""
echo "== 2. Kimlik denetimi sabit süreli =="

# Erken çıkan bir karşılaştırma, doğru jetonun önekini zamanlamadan sızdırır.
if strip_comments "$SRV" | grep -q 'subtle.ConstantTimeCompare'; then
    pass "jeton sabit süreli karşılaştırılıyor"
else
    fail "jeton düz == ile karşılaştırılıyor — zamanlama saldırısına açık"
fi

# Kimlik denetimi, isteği DAĞITMADAN ÖNCE olmalı.
auth_line="$(strip_comments "$SRV" | grep -n 's.authorized(r)' | head -1 | cut -d: -f1)"
disp_line="$(strip_comments "$SRV" | grep -n 's.disp.Dispatch' | head -1 | cut -d: -f1)"
if [ -n "$auth_line" ] && [ -n "$disp_line" ] && [ "$auth_line" -lt "$disp_line" ]; then
    pass "kimlik denetimi, RPC dağıtımından ÖNCE"
else
    fail "istek kimlik denetiminden önce dağıtılıyor"
fi

echo ""
echo "== 3. TLS zorunlu =="

if strip_comments "$SRV" | grep -q 'ServeTLS'; then
    pass "yalnızca TLS ile servis ediliyor"
else
    fail "düz HTTP ile servis ediliyor — jeton açık metin gider"
fi

if strip_comments "$SRV" | grep -q 'MinVersion: *tls.VersionTLS12'; then
    pass "TLS tabanı 1.2"
else
    fail "TLS asgari sürümü ayarlanmamış — kırık sürümler kabul edilir"
fi

echo ""
echo "== 4. Sertifika kalıcı ve parmak izi gösteriliyor =="

# Her açılışta yeni sertifika, telefondaki sabitlemeyi geçersiz kılar ve
# kullanıcıyı her seferinde "araya giren var" uyarısına alıştırır.
if strip_comments "$CRT" | grep -q 'loadCert(certPath, keyPath)'; then
    pass "var olan sertifika yeniden kullanılıyor"
else
    fail "sertifika her açılışta yeniden üretiliyor olabilir"
fi

if strip_comments "$CRT" | grep -q 'sha256.Sum256'; then
    pass "parmak izi SHA-256"
else
    fail "parmak izi hesaplanmıyor"
fi

# Özel anahtar yalnızca kök tarafından okunabilmeli.
if strip_comments "$CRT" | grep -q '0o600'; then
    pass "özel anahtar 0600 ile yazılıyor"
else
    fail "özel anahtar gevşek izinle yazılıyor"
fi

echo ""
echo "== 5. /health hiçbir sır sızdırmıyor =="

if strip_comments "$SRV" | grep -A10 'func (s \*Server) handleHealth' | grep -q 'Token'; then
    fail "/health yanıtında jeton geçiyor"
else
    pass "/health jeton içermiyor"
fi

echo ""
echo "== 6. Varsayılan KAPALI =="

# Kullanıcı açıkça istemeden ağa açılmamalı.
if strip_comments "$CFG" | grep -A6 'type RemoteConfig struct' | grep -q 'Enabled bool'; then
    pass "uzaktan erişim yapılandırmada bir anahtarla korunuyor"
else
    fail "RemoteConfig.Enabled yok"
fi

if strip_comments "$HND" | grep -q 'if !rc.Enabled'; then
    pass "kapalıyken köprü başlatılmıyor"
else
    fail "köprü, Enabled denetlenmeden başlatılıyor"
fi

echo ""
echo "== 7. Jeton üretimi güçlü =="

if strip_comments "$HND" | grep -q 'crypto/rand\|rand.Read'; then
    pass "jeton kriptografik rastgelelikten üretiliyor"
else
    fail "jeton zayıf bir kaynaktan üretiliyor"
fi

# 16 bayt = 128 bit.
if strip_comments "$HND" | grep -q 'make(\[\]byte, 16)'; then
    pass "jeton 128 bit"
else
    fail "jeton uzunluğu 128 bit değil"
fi

echo ""
echo "== 8. SSH: parola yapılandırmaya YAZILMIYOR =="

# config.json yedeklenip kopyalanabilen bir dosya; içine giriş parolası
# koymak, o yedeği alan herkese kabuk erişimi vermek olurdu.
if strip_comments "$CFG" | grep -A10 'type SSHConfig struct' | grep -qi 'password *string'; then
    fail "SSH parolası config.json'a yazılıyor"
else
    pass "SSH parolası yapılandırmada tutulmuyor (yalnızca PasswordSet)"
fi

if strip_comments "$SSHL" | grep -q 'cmd.Stdin = strings.NewReader'; then
    pass "parola chpasswd'ye stdin ile veriliyor (komut satırında değil)"
else
    fail "parola komut satırında geçiyor olabilir — /proc'tan görünür"
fi

echo ""
echo "== 9. SSH: parolasız/anahtarsız açılmıyor =="

if strip_comments "$SSHD" | grep -q '!cfg.PasswordSet && len(cfg.AuthorizedKeys) == 0'; then
    pass "parola da anahtar da yoksa SSH açılmıyor"
else
    fail "SSH parolasız açılabiliyor"
fi

if strip_comments "$SSHL" | grep -q 'PermitEmptyPasswords no'; then
    pass "boş parolayla giriş kapalı"
else
    fail "PermitEmptyPasswords ayarlanmamış"
fi

echo ""
echo "== 10. SSH sunucu anahtarları kalıcı =="

# Her açılışta yeni anahtar, istemcide "REMOTE HOST IDENTIFICATION HAS
# CHANGED" uyarısı üretir. O uyarıyı rutin hâline getirmek, gerçeğini fark
# edilemez kılar.
if strip_comments "$SSHD" | grep -q 'func (m \*Manager) hostKeyDir'; then
    pass "sunucu anahtarları kalıcı veri klasöründe"
else
    fail "sunucu anahtarı klasörü yok"
fi

echo ""
echo "== 11. Go testleri gerçekten var =="

T="$ROOT/internal/remote/server_test.go"
if [ -f "$T" ]; then
    for want in TestNoTokenNoAccess TestEmptyTokenRefusesToStart \
                TestPlainHTTPIsNotServed TestCertificateIsStable; do
        if grep -q "$want" "$T"; then
            pass "$want var"
        else
            fail "$want testi yok"
        fi
    done
else
    fail "internal/remote/server_test.go yok"
fi

echo ""
if [ "$fails" -eq 0 ]; then
    printf '\033[32mUZAKTAN ERİŞİM GÜVENLİĞİ TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
