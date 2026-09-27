#!/bin/sh
# test-offline-logic.sh — playit'in imaja gömüldüğünü ve çevrimdışı paketin
# RAM'e taşınmadığını kilitleyen test.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN VAR
# ════════════════════════════════════════════════════════════════════════════
#
# İki ayrı gerçek arıza:
#
#  1. PLAYIT HİÇ GÖMÜLMÜYORDU. post-build.sh, playit ikililerini
#     "${TARGET_DIR}/usr/lib/mcos/offline" içinden kopyalıyordu — yani ancak
#     çevrimdışı paket ROOTFS'e kopyalandıysa. Paket ise yalnızca
#     "make offline-bundle" elle çalıştırılmışsa indiriliyordu ve o hedef
#     "make os" / "make iso" zincirine BAĞLI DEĞİLDİ. Depoda dist/offline
#     hiç oluşmamıştı; sonuç: üretilen 154 MB'lık ISO'da playitd YOKTU ve
#     Tünel ekranı "ajan kurulu değil" diyordu.
#
#     Kullanıcının isteği netti: "bide playiti de gom".
#
#  2. PAKET RAM'E AÇILIYORDU. post-build.sh'in kendi açıklaması "Paket
#     ROOTFS'E DEĞİL ayrı bir dizine konur" diyordu ama kod paketi
#     TARGET_DIR'e kopyalıyordu. TARGET_DIR initramfs'e paketlenir ve her
#     açılışta tamamen RAM'e açılır: 76 MB, canlı ISO'da hiç kullanılmasa
#     bile.
#
# Bu test hiçbir şey çalıştırmaz, hiçbir diske dokunmaz; yalnızca kaynak
# dosyaları okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
POSTBUILD="$ROOT/os/buildroot/external/board/mcos/post-build.sh"
INSTALL="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install"
MANIFEST="$ROOT/os/buildroot/external/board/mcos/offline-manifest.txt"
MAKEFILE="$ROOT/Makefile"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$POSTBUILD" "$INSTALL" "$MANIFEST" "$MAKEFILE"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

echo "== 1. playit imaja gomuluyor =="

if grep -q 'install -D -m 0755 "\$f" "\${TARGET_DIR}/usr/bin/playitd"' "$POSTBUILD"; then
    pass "playitd /usr/bin'e kuruluyor"
else
    fail "playitd /usr/bin'e kurulmuyor — Tunel ekrani 'ajan yok' der"
fi

if grep -q 'install -D -m 0755 "\$f" "\${TARGET_DIR}/usr/bin/playit-cli"' "$POSTBUILD"; then
    pass "playit-cli /usr/bin'e kuruluyor"
else
    fail "playit-cli kurulmuyor — hesap baglama (claim) calismaz"
fi

# Kurulum KAYNAK dizinden yapılmalı. Eskiden rootfs'teki kopyadan yapılıyordu
# ve o kopya kaldırıldığı için playit sessizce gömülmez olurdu.
if grep -q 'for f in "\$MCOS_OFFLINE_SRC"/\*playit-linux-amd64' "$POSTBUILD"; then
    pass "playit KAYNAK dizinden kuruluyor (rootfs kopyasina bagli degil)"
else
    fail "playit hala rootfs'teki offline/ kopyasina bagli olabilir"
fi

if grep -q 'playitd|https://' "$MANIFEST" && grep -q 'playit-cli|https://' "$MANIFEST"; then
    pass "manifest iki playit ikilisini de listeliyor"
else
    fail "manifest'te playitd veya playit-cli eksik"
fi

echo
echo "== 2. cevrimdisi paket initramfs'e (RAM'e) GIRMIYOR =="

# En önemli denetim: paketin tamamını TARGET_DIR'e kopyalayan satır
# KALMAMALI. (playit ikilileri ayrı ayrı install ediliyor; onlar 11 MB.)
# AÇIKLAMA SATIRLARI ELENİR: post-build.sh eski hatalı satırı bilerek
# alıntılıyor (neden değiştirildiğini anlatmak için). Yorumları saymak, bu
# testi kendi belgelendirmesine karşı çalıştırmak olurdu.
if grep -v '^[[:space:]]*#' "$POSTBUILD" |
        grep -q 'cp -a "\$MCOS_OFFLINE_SRC"/\. "\${TARGET_DIR}'; then
    fail "paket hala rootfs'e kopyalaniyor — her acilista 76 MB RAM"
else
    pass "paket rootfs'e kopyalanmiyor"
fi

if grep -q 'MCOS_OFFLINE_STAGE="\${BINARIES_DIR}/mcos-offline"' "$POSTBUILD"; then
    pass "paket BINARIES_DIR altina hazirlaniyor"
else
    fail "paket onyukleme ortami icin hazirlanmiyor"
fi

if grep -q 'mcos-offline' "$MAKEFILE"; then
    pass "ISO hedefi paketi ISO'ya kopyaliyor"
else
    fail "ISO hedefi paketi ISO'ya koymuyor — kurulumda tohumlanamaz"
fi

echo
echo "== 3. kurulum paketi onyukleme ortamindan tohumluyor =="

if grep -q 'seed_offline_from' "$INSTALL"; then
    pass "mcos-install tohumlama yordamina sahip"
else
    fail "mcos-install tohumlama yordami yok"
fi

if grep -q 'mcos/offline' "$INSTALL"; then
    pass "onyukleme ortamindaki /mcos/offline araniyor"
else
    fail "onyukleme ortami hic aranmiyor"
fi

if grep -q 'mount -o ro' "$INSTALL"; then
    pass "ortam SALT OKUNUR baglaniyor"
else
    fail "ortam salt okunur baglanmiyor — kurulum medyasina yazma riski"
fi

# Kurulum HEDEFİ asla ortam olarak taranmamalı: az önce biçimlendirildi.
if grep -q 'case "\$dev" in "\$TARGET"\*) continue ;; esac' "$INSTALL"; then
    pass "kurulum hedefi ortam taramasindan haric"
else
    fail "kurulum hedefi de taraniyor — yeni yazilan diski bagliyor olabilir"
fi

echo
if [ "$fails" -eq 0 ]; then
    printf '\033[32mCEVRIMDISI PAKET VE PLAYIT TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
