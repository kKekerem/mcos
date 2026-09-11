#!/bin/sh
# test-display-logic.sh — çözünürlük zincirinin tutarlılık testi.
#
# NEDEN VAR: gfxmode üç ayrı yerde yazılıyordu ve üçü de farklıydı —
#   mkiso.sh      : gfxmode=auto AMA komut satırında video=1024x768
#   mkusb.sh      : gfxmode=auto
#   mcos-install  : gfxmode=1024x768,800x600,auto
# Aynı makine ISO'dan, USB'den ve kurulu diskten ÜÇ AYRI çözünürlükte
# açılıyordu; ISO'daki video=1024x768 ise GRUB ne seçerse seçsin çözünürlüğü
# zorla düşürüyordu.
#
# Hiçbir şey derlemez, hiçbir diske dokunmaz: yalnızca kaynakları okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
LIB="$ROOT/scripts/lib/display.sh"
MKISO="$ROOT/scripts/mkiso.sh"
MKUSB="$ROOT/scripts/mkusb.sh"
INSTALL="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install"
POSTBUILD="$ROOT/os/buildroot/external/board/mcos/post-build.sh"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$LIB" "$MKISO" "$MKUSB" "$INSTALL" "$POSTBUILD"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

# shellcheck source=scripts/lib/display.sh
. "$LIB"

code() { sed 's/[[:space:]]*#.*$//' "$1"; }

echo "== 1. Tek kaynak kullanılıyor =="

# Makefile'ın iso hedefi KENDİ grub.cfg'sini satır içi üretiyor ve `make iso`
# gerçekte onu kullanıyor — mkiso.sh hiçbir yerden çağrılmıyor. Bu yüzden
# Makefile de denetlenmek ZORUNDA; aksi halde asıl üretilen ISO listeden sapar.
MK="$ROOT/Makefile"
if grep -q 'lib/display\.sh' "$MK"; then
    pass "Makefile iso hedefi lib/display.sh'ten okuyor"
else
    fail "Makefile iso hedefi kendi gfxmode'unu gömüyor — üretilen ISO sapar"
fi
if grep -qE "^[[:space:]]*'set gfxmode=[0-9]" "$MK"; then
    fail "Makefile içinde hâlâ sabit gfxmode var"
else
    pass "Makefile içinde sabit gfxmode yok"
fi

for f in "$MKISO" "$MKUSB"; do
    n="$(basename "$f")"
    if code "$f" | grep -q 'lib/display\.sh'; then
        pass "$n çözünürlük tanımını lib/display.sh'ten alıyor"
    else
        fail "$n kendi gfxmode'unu tanımlıyor — sapma kaçınılmaz"
    fi
done

if code "$INSTALL" | grep -q '/usr/lib/mcos/display\.sh'; then
    pass "mcos-install /usr/lib/mcos/display.sh okuyor"
else
    fail "mcos-install ortak tanımı okumuyor"
fi

if code "$POSTBUILD" | grep -q 'usr/lib/mcos/display\.sh'; then
    pass "post-build.sh tanımı hedef rootfs'e kopyalıyor"
else
    fail "post-build.sh display.sh'i kopyalamıyor — mcos-install yedeğe düşer"
fi

echo "== 2. video= çekirdek komut satırında YOK =="

# video=, GRUB'un seçtiği modu ezer. Framebuffer'ı firmware kurduğu için
# (gerçek GPU sürücüsü derlenmiyor) bu, çözünürlüğü sabitlemenin en kötü yolu.
for f in "$MKISO" "$MKUSB" "$INSTALL" "$ROOT/Makefile"; do
    n="$(basename "$f")"
    if code "$f" | grep -qE '(linux|APPEND|CMDLINE)[^#]*[[:space:]]video='; then
        fail "$n komut satırında video= var — GRUB'un seçtiği çözünürlüğü ezer"
    else
        pass "$n komut satırında video= yok"
    fi
done

echo "== 3. gfxmode listesi 1080p ile başlıyor =="

case "$MCOS_GFXMODE" in
    1920x1080*) pass "gfxmode listesi 1920x1080 ile başlıyor" ;;
    *)          fail "gfxmode listesi 1080p ile başlamıyor: $MCOS_GFXMODE" ;;
esac

case "$MCOS_GFXMODE" in
    *,auto) pass "liste 'auto' ile bitiyor (firmware hiçbirini sunmazsa geri dönüş var)" ;;
    *)      fail "liste 'auto' ile bitmiyor — desteklenmeyen modda kara ekran riski: $MCOS_GFXMODE" ;;
esac

# Liste içindeki her girdi WxH ya da WxHxD biçiminde olmalı.
bad=""
for m in $(echo "$MCOS_GFXMODE" | tr ',' ' '); do
    case "$m" in
        auto) ;;
        [0-9]*x[0-9]*) ;;
        *) bad="$bad $m" ;;
    esac
done
if [ -n "$bad" ]; then
    fail "geçersiz gfxmode girdisi:$bad"
else
    pass "tüm gfxmode girdileri geçerli biçimde"
fi

echo "== 4. gfxpayload=keep var =="

# Bu olmadan çekirdek GRUB'un kurduğu modu DEVRALMAZ ve VGA metin moduna döner.
if echo "$(mcos_grub_header 5)" | grep -q 'set gfxpayload=keep'; then
    pass "ortak grub başlığı gfxpayload=keep içeriyor"
else
    fail "gfxpayload=keep yok — çekirdek GRUB'un modunu devralmaz"
fi

if code "$INSTALL" | grep -q 'set gfxpayload=keep'; then
    pass "mcos-install grub.cfg'si gfxpayload=keep içeriyor"
else
    fail "mcos-install grub.cfg'sinde gfxpayload=keep yok"
fi

echo "== 5. Komut satırında root= yok =="

case "$MCOS_CMDLINE_BASE" in
    *root=*) fail "MCOS_CMDLINE_BASE içinde root= var — initramfs sisteminde VFS paniği" ;;
    *)       pass "MCOS_CMDLINE_BASE root= içermiyor" ;;
esac

echo "== 6. Kullanıcıya sunulan modlar listede var =="

for m in $MCOS_MODES; do
    case ",$MCOS_GFXMODE," in
        *",$m,"*|*",${m}x32,"*) ;;
        *) fail "ekran ayarlarında sunulan $m, gfxmode listesinde yok" ;;
    esac
done
pass "sunulan $(echo "$MCOS_MODES" | wc -w) modun tamamı listede"

echo "== 7. Çekirdekte gerçek modesetting sürücüsü var mı? =="

KCFG="$ROOT/os/buildroot/external/board/mcos/kernel.config"
if [ -f "$KCFG" ]; then
    if grep -qE '^CONFIG_DRM_(I915|AMDGPU|RADEON|NOUVEAU)=' "$KCFG"; then
        pass "gerçek GPU sürücüsü var — çalışırken mod değiştirme mümkün"
    else
        # Bu bir HATA DEĞİL, bilinçli bir tasarım kararı: initramfs'i şişirmemek
        # için GPU sürücüsü ve firmware blob'ları derlenmiyor. Testin görevi,
        # bu gerçeğin ekran ayarları ekranının davranışıyla tutarlı kalmasını
        # sağlamak: çözünürlük DEĞİŞİKLİĞİ YENİDEN BAŞLATMA GEREKTİRİR.
        if grep -q 'yeniden başlat' "$LIB" 2>/dev/null ||
           grep -q 'yeniden başlatır' "$LIB" 2>/dev/null; then
            pass "GPU sürücüsü yok; display.sh bunu ve yeniden başlatma gereğini belgeliyor"
        else
            fail "GPU sürücüsü yok ama display.sh yeniden başlatma gereğini açıklamıyor"
        fi
    fi
else
    fail "kernel.config bulunamadı"
fi

echo
if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mtüm ekran testleri geçti\033[0m\n'
