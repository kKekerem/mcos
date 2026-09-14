#!/bin/sh
# test-diskroot-logic.sh — kalıcılık mantığının testi.
#
# ── NE DOĞRULUYOR ───────────────────────────────────────────────────────────
# Kullanıcının şartı: "usb ye kurulan ana iso ramdisk olsun ama ssd ye kurulan
# sistem kalıcı olsun" ve "direkt iso yu değiştirme".
#
# Ayrımı yapan tek şey çekirdek komut satırıdır:
#   ISO / USB  -> mcos.root= YOK      -> /init hemen normal init'e devreder
#   Kurulu disk-> mcos.root=LABEL=... -> /init o bölüme switch_root yapar
#
# Bu test, o ayrımın her iki yönde de korunduğunu ve geçişin güvenli
# olduğunu sabitler. Hiçbir diske DOKUNMAZ.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
INIT="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/init"
INSTALL="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install"
MK="$ROOT/Makefile"
MKUSB="$ROOT/scripts/mkusb.sh"
KCFG="$ROOT/os/buildroot/external/board/mcos/kernel.config"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$INIT" "$INSTALL" "$MK" "$MKUSB" "$KCFG"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

code() { sed 's/[[:space:]]*#.*$//' "$1"; }

echo "== 1. Canlı ISO/USB HÂLÂ ramdisk =="

# ISO'nun komut satırında mcos.root OLMAMALI. Olursa canlı sistem de diske
# geçmeye çalışır ve ISO'nun anlamı kalmaz.
if code "$MK" | grep -q 'mcos\.root='; then
    fail "Makefile (ISO) komut satırında mcos.root var — ISO artık ramdisk değil"
else
    pass "ISO komut satırında mcos.root yok"
fi
if code "$MKUSB" | grep -q 'mcos\.root='; then
    fail "mkusb.sh komut satırında mcos.root var — USB artık ramdisk değil"
else
    pass "USB imajı komut satırında mcos.root yok"
fi

echo "== 2. Kurulu disk KALICI =="

if code "$INSTALL" | grep -q 'mcos\.root=LABEL=MCOS-ROOT'; then
    pass "kurulum mcos.root=LABEL=MCOS-ROOT yazıyor"
else
    fail "kurulum kalıcılık parametresini yazmıyor — disk yine RAM'den açılır"
fi

if code "$INSTALL" | grep -q 'mkfs.ext4 .*-L MCOS-ROOT'; then
    pass "kök bölümü MCOS-ROOT etiketiyle biçimlendiriliyor"
else
    fail "MCOS-ROOT etiketli kök bölümü yok"
fi

# Üç bölüm olmalı: boot(FAT) + root(ext4) + data(ext4).
n="$(code "$INSTALL" | grep -c 'parted -s "\$TARGET" mkpart' || true)"
if [ "$n" -eq 3 ]; then
    pass "üç bölüm oluşturuluyor (boot + kök + veri)"
else
    fail "bölüm sayısı $n — boot/kök/veri için 3 olmalı"
fi

if code "$INSTALL" | grep -q 'ROOT_PART'; then
    pass "kök bölümü ayrı değişkende izleniyor"
else
    fail "ROOT_PART tanımlı değil"
fi

# Kök gerçekten DOLDURULMALI; boş bir bölüme geçmek açılışı kilitler.
if code "$INSTALL" | grep -q 'cp -a "/\$d" "\$ROOT_MNT/"'; then
    pass "canlı sistem kök bölümüne kopyalanıyor"
else
    fail "kök bölümü doldurulmuyor — switch_root boş diske geçerdi"
fi

if code "$INSTALL" | grep -q 'etc/mcos-root'; then
    pass "kök işaret dosyası yazılıyor"
else
    fail "işaret dosyası yok — /init geçerli kökü doğrulayamaz"
fi

echo "== 3. /init güvenli =="

if code "$INIT" | grep -q 'switch_root'; then
    pass "/init switch_root yapıyor"
else
    fail "/init switch_root yapmıyor — kalıcılık çalışmaz"
fi

# EN KRİTİK: her hata yolu canlı sisteme dönmeli. Yarım kalmış bir geçiş,
# kullanıcıyı AÇILMAYAN bir makineyle bırakır.
if code "$INIT" | grep -q 'live_boot'; then
    pass "hata yollarında canlı sisteme dönüş var"
else
    fail "geri dönüş yolu yok — bir hata makineyi açılamaz yapar"
fi

n="$(code "$INIT" | grep -c 'live_boot$' || true)"
if [ "$n" -ge 4 ]; then
    pass "canlı sisteme dönüş $n ayrı hata noktasında çağrılıyor"
else
    fail "yalnızca $n hata noktası geri dönüyor — bazı hatalar açılışı keser"
fi

# Kök doğrulaması ŞART: rastgele bir ext4 bölümüne geçmek PID 1'i kaybettirir
# ve çekirdek panik verir.
if code "$INIT" | grep -q 'etc/mcos-root' && code "$INIT" | grep -q 'sbin/init'; then
    pass "geçiş öncesi kök doğrulanıyor (init + işaret dosyası)"
else
    fail "kök doğrulanmadan switch_root yapılıyor — yanlış bölümde kernel panic"
fi

# Bağlı dosya sistemleri taşınmalı; taşınmazsa yeni sistemde /proc eksik olur.
if code "$INIT" | grep -q 'mount -o move'; then
    pass "/proc ve /sys yeni köke taşınıyor"
else
    fail "dosya sistemleri taşınmıyor — yeni kökte /proc eksik kalır"
fi

if [ -x "$INIT" ]; then
    pass "/init çalıştırılabilir"
else
    fail "/init çalıştırılabilir değil — çekirdek onu çalıştıramaz"
fi

# Buildroot /init'i YALNIZCA yoksa oluşturur; bizimki hayatta kalmalı.
CPIO="$ROOT/os/buildroot/buildroot/fs/cpio/cpio.mk"
if [ -f "$CPIO" ]; then
    if grep -q 'if \[ ! -e $(TARGET_DIR)/init \]' "$CPIO"; then
        pass "Buildroot bizim /init dosyamızın üzerine yazmıyor"
    else
        fail "Buildroot /init'i ezebilir — cpio.mk değişmiş"
    fi
fi

echo "== 4. Çekirdek desteği =="

for opt in CONFIG_EXT4_FS CONFIG_BLK_DEV_INITRD CONFIG_DEVTMPFS_MOUNT; do
    if grep -q "^$opt=y" "$KCFG"; then
        pass "$opt açık"
    else
        fail "$opt kapalı — kalıcı kök çalışmaz"
    fi
done

echo "== 5. Açık 'canlı çalış' kaçışı =="

# Kurulu diskte bile kullanıcı RAM'den açabilmeli (kök bozulursa kurtarma).
if code "$INIT" | grep -q 'mcos\.live'; then
    pass "mcos.live parametresi ile RAM'den açılabiliyor"
else
    fail "kurtarma için canlı açılış parametresi yok"
fi

echo
if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mkalıcılık mantığı doğrulandı\033[0m\n'
