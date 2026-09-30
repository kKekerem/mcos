#!/bin/sh
# qemu.sh — MCOS ISO'sunu QEMU'da DONANIM HIZLANDIRMALI açar.
#
# Kullanım:
#   sh scripts/qemu.sh                 # BIOS, mevcut ISO, hızlandırmalı
#   sh scripts/qemu.sh --mode uefi     # UEFI (OVMF)
#   sh scripts/qemu.sh --iso yol.iso   # başka bir imaj
#   sh scripts/qemu.sh --dry-run       # komutu YAZ, çalıştırma
#   sh scripts/qemu.sh --tcg           # bilerek yazılım öykünmesi
#
# ── Bu betik NE YAPMAZ ──────────────────────────────────────────────────────
#
# DERLEME YAPMAZ. "make qemu" hedefinin ön koşulu "iso", onunki de "os" ve o
# da Buildroot zinciri: yani hızlandırma isteyen biri "make qemu" yazdığında
# saatlerce derleme başlayabilir. Bu betik yalnızca VAR OLAN imajı açar; imaj
# yoksa ne yapılacağını söyleyip çıkar.
#
# HİÇBİR AYGITA YAZMAZ. dd yok, flash yok.
#
# POSIX sh ile yazıldı: belgelerde "sh scripts/qemu.sh" biçimi kullanılıyor ve
# dash "pipefail"i tanımaz. Bashism YOK — dizi, local, [[ ]] hiçbiri geçmiyor.
set -u

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
# shellcheck source=lib/kvm.sh
. "$ROOT/scripts/lib/kvm.sh"

MODE="bios"
ISO="$ROOT/dist/mcos-x86_64.iso"
DRY=""
FORCE_TCG=""

die() { printf 'qemu.sh: HATA: %s\n' "$*" >&2; exit 2; }

while [ $# -gt 0 ]; do
    case "$1" in
        --mode)    MODE="${2:-}"; shift 2 ;;
        --iso)     ISO="${2:-}"; shift 2 ;;
        --dry-run) DRY=1; shift ;;
        --tcg)     FORCE_TCG=1; shift ;;
        -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
        *) die "bilinmeyen seçenek: $1" ;;
    esac
done

command -v qemu-system-x86_64 >/dev/null 2>&1 \
    || die "qemu-system-x86_64 yok — kurun: sudo apt install qemu-system-x86"

[ -f "$ISO" ] || die "imaj yok: $ISO
       Önce üretin:  make iso
       (bu betik bilerek derleme yapmaz)"

# ════════════════════════════════════════════════════════════════════════════
# 1. HIZLANDIRMA
# ════════════════════════════════════════════════════════════════════════════

HW=0
if [ -n "$FORCE_TCG" ]; then
    echo ">> --tcg verildi: yazılım öykünmesi (bilerek)"
elif mcos_kvm_usable; then
    HW=1
elif mcos_kvm_missing; then
    mcos_kvm_explain_missing
    echo ">> Şimdilik yazılım öykünmesiyle devam edilecek (ÇOK YAVAŞ)."
else
    # İzin sorunu: düzeltilebilir.
    if mcos_kvm_grant; then
        HW=1
        echo ">> İzin düzeltildi."
    else
        echo ">> UYARI: /dev/kvm hâlâ erişilemiyor."
    fi
fi

# Aygıt açılabiliyor olması KVM'in çalışacağını KANITLAMAZ (bkz. lib/kvm.sh).
if [ "$HW" = "1" ] && [ -z "$DRY" ]; then
    if ! mcos_kvm_really_works; then
        echo ">> UYARI: /dev/kvm açılıyor ama KVM başlatılamıyor."
        echo ">>        (Windows'ta Hyper-V sanallaştırmayı tutuyor olabilir.)"
        HW=0
    fi
fi

# ACCEL ve CPU BİRLİKTE: "-cpu host" TCG altında ölümcüldür.
if [ "$HW" = "1" ]; then
    ACCEL="-accel kvm"
    CPU="-cpu host"
    echo ">> KVM AÇIK — donanım hızlandırması kullanılıyor."
else
    ACCEL="-accel tcg,thread=multi"
    CPU="-cpu max"
    echo ">> Yazılım öykünmesi (TCG): GRUB menüsü bile takılabilir."
fi

# ════════════════════════════════════════════════════════════════════════════
# 2. BELLEK VE ÇEKİRDEK
# ════════════════════════════════════════════════════════════════════════════
#
# KVM'de cömert olunabilir: konuk belleği host'tan yalnızca KULLANDIKÇA alınır.
# TCG'de her vCPU bir host iş parçacığını yakar, bu yüzden 4'te sınırlanır.

AVAIL_MB="$(awk '/MemAvailable/{print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 2048)"
if [ "$HW" = "1" ]; then
    MEM="${QEMU_MEM:-$(( AVAIL_MB * 40 / 100 ))}"
    [ "$MEM" -lt 2048 ] && MEM=2048
    [ "$MEM" -gt 8192 ] && MEM=8192
    CPUS="${QEMU_SMP:-$(( $(nproc) / 2 ))}"
    [ "$CPUS" -lt 2 ] && CPUS=2
    [ "$CPUS" -gt 8 ] && CPUS=8
else
    MEM="${QEMU_MEM:-2048}"
    CPUS="${QEMU_SMP:-4}"
fi

# ════════════════════════════════════════════════════════════════════════════
# 3. EKRAN
# ════════════════════════════════════════════════════════════════════════════
#
# ── Yakalanan gerçek tuzak ──────────────────────────────────────────────────
#
# MCOS çekirdeğinde CONFIG_DRM_BOCHS=y var ve QEMU'nun stdvga aygıtı PCI
# 1234:1111 olarak bochs-drm'e bağlanır. bochs-drm, GRUB'un kurduğu firmware
# framebuffer'ını DEVRALIR ve fb0'ı EDID'in TERCİH EDİLEN moduyla yeniden
# kurar. VGA aygıtının xres/yres varsayılanı 0 olduğu için GRUB'un seçtiği
# 1920x1080 SESSİZCE 1024x768'e düşer — kullanıcı bunu "çözünürlük ayarı
# çalışmıyor" diye görür.
#
# EDID'i sabitlemek bunu kapatır. vgamem_mb=32 de gerekli: varsayılan 16 MiB,
# iki adet 1920x1080x32 tampon (2 x 7.9 = 15.8 MiB) için sınırda ve hizalama
# payıyla taşabilir.
XRES="${QEMU_XRES:-1920}"
YRES="${QEMU_YRES:-1080}"
VIDEO="-vga none -device VGA,vgamem_mb=32,xres=$XRES,yres=$YRES"

# WSLg'de WAYLAND_DISPLAY ayarlıdır ama soket XDG_RUNTIME_DIR'de görünmez
# (systemd=true WSLg'nin dizinini gölgeler). GTK önce Wayland'ı dener,
# başarısız olur ve pencere hiç açılmaz. X11 yolu sağlam: ona zorluyoruz.
if [ -S /tmp/.X11-unix/X0 ]; then
    export GDK_BACKEND=x11
    unset WAYLAND_DISPLAY
fi
[ -n "${DISPLAY:-}" ] || echo ">> UYARI: DISPLAY boş — WSLg çalışmıyor olabilir."

DISPLAY_ARG="-display gtk,zoom-to-fit=on,grab-on-hover=off"

# ════════════════════════════════════════════════════════════════════════════
# 4. SES
# ════════════════════════════════════════════════════════════════════════════
#
# PULSE_SERVER ortam değişkenine GÜVENİLMEZ: boşsa QEMU sessizce sessize
# düşer ve MCOS'un ses efektleri hiç duyulmaz. Soketin varlığı kontrol edilip
# yol açıkça veriliyor.
if [ -S /mnt/wslg/PulseServer ]; then
    AUDIO="-audiodev pa,id=snd0,server=unix:/mnt/wslg/PulseServer"
elif [ -n "${PULSE_SERVER:-}" ]; then
    AUDIO="-audiodev pa,id=snd0"
else
    echo ">> Ses sunucusu bulunamadı — sanal makine sessiz açılacak."
    AUDIO="-audiodev none,id=snd0"
fi
AUDIO="$AUDIO -device intel-hda -device hda-duplex,audiodev=snd0"

# ════════════════════════════════════════════════════════════════════════════
# 5. AĞ VE PORT YÖNLENDİRME
# ════════════════════════════════════════════════════════════════════════════
#
# ── Yakalanan gerçek tuzak ──────────────────────────────────────────────────
#
# hostfwd'de host portu DOLUYSA QEMU hiç başlamaz: exit 1, pencere açılmaz.
# Yani betik ikinci kez çalıştırıldığında (ilk sanal makine hâlâ açıkken)
# HİÇBİR ŞEY olmaz ve sebep görünmez. Her port önce yoklanıyor.
#
# 127.0.0.1'e bağlanıyor, 0.0.0.0'a değil: yönlendirilen portlar yerel ağa
# açılmamalı — konuk sistemin SSH'ı ve VNC'si oradadır.
port_bos() { ! ss -tln 2>/dev/null | grep -q ":$1 "; }

HOSTFWD=""
add_fwd() { # $1=host portu  $2=konuk portu  $3=ad
    if port_bos "$1"; then
        HOSTFWD="$HOSTFWD,hostfwd=tcp:127.0.0.1:$1-:$2"
        printf '   %-18s localhost:%s -> konuk:%s\n' "$3" "$1" "$2"
    else
        printf '   %-18s ATLANDI (host portu %s dolu)\n' "$3" "$1"
    fi
}
echo ">> Port yönlendirme:"
add_fwd 25565 25565 "Minecraft"
add_fwd 12222 22    "SSH"
add_fwd 12223 2223  "Uzaktan kontrol"
add_fwd 15900 5900  "VNC (RealVNC)"

NET="-netdev user,id=n0${HOSTFWD} -device e1000,netdev=n0"

# ════════════════════════════════════════════════════════════════════════════
# 6. ÜRET VE ÇALIŞTIR
# ════════════════════════════════════════════════════════════════════════════

set -- -m "$MEM" -smp "$CPUS" $ACCEL $CPU -machine pc \
    -cdrom "$ISO" -boot d \
    $VIDEO $DISPLAY_ARG $AUDIO $NET \
    -rtc base=utc,driftfix=slew -name "MCOS"

if [ "$MODE" = "uefi" ]; then
    OVMF=""
    for f in /usr/share/ovmf/OVMF.fd /usr/share/OVMF/OVMF.fd \
             /usr/share/OVMF/OVMF_CODE.fd /usr/share/qemu/OVMF.fd; do
        [ -f "$f" ] && { OVMF="$f"; break; }
    done
    [ -n "$OVMF" ] || die "OVMF bulunamadı — kurun: sudo apt install ovmf"
    set -- -bios "$OVMF" "$@"
fi

echo ">> $MEM MB bellek, $CPUS çekirdek, ${XRES}x${YRES}"
echo ">> İmaj: $ISO"

if [ -n "$DRY" ]; then
    echo ">> (--dry-run) çalıştırılacak komut:"
    echo "qemu-system-x86_64 $*"
    exit 0
fi

exec qemu-system-x86_64 "$@"
