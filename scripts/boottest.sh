#!/bin/bash
# boottest.sh — bir MCOS imajını QEMU'da GERÇEKTEN boot eder ve seri konsol
# çıktısından hangi aşamaya kadar geldiğini raporlar.
#
# "USB'den açılmıyor" hatası ancak boot ederek kanıtlanır; yapısal denetim
# (verify-usb.sh) bölüm tablosunu doğrular ama önyükleyicinin çekirdeği
# gerçekten yükleyip yükleyemediğini göstermez.
#
# Kullanım:
#   scripts/boottest.sh --img dist/mcos-bios.img --mode bios  [--seconds 45]
#   scripts/boottest.sh --img dist/mcos-uefi.img --mode uefi
#   scripts/boottest.sh --iso dist/mcos-x86_64.iso --mode bios
#
# NOT: imaj seri konsola yazmıyorsa çıktı boş kalır. mkusb.sh'ye
# "--cmdline console=ttyS0,115200" vererek test imajı üretin.

set -uo pipefail

IMG=""; ISO=""; MODE="bios"; SECONDS_LIMIT=45; MEM=2048

die() { printf 'boottest: HATA: %s\n' "$*" >&2; exit 2; }

while [ $# -gt 0 ]; do
    case "$1" in
        --img)     IMG="${2:-}"; shift 2 ;;
        --iso)     ISO="${2:-}"; shift 2 ;;
        --mode)    MODE="${2:-}"; shift 2 ;;
        --seconds) SECONDS_LIMIT="${2:-}"; shift 2 ;;
        --mem)     MEM="${2:-}"; shift 2 ;;
        *) die "bilinmeyen seçenek: $1" ;;
    esac
done

[ -n "$IMG$ISO" ] || die "--img veya --iso gerekli"
command -v qemu-system-x86_64 >/dev/null 2>&1 || die "qemu-system-x86_64 yok"

QEMU_ARGS=(-m "$MEM" -display none -no-reboot)

if [ "$MODE" = uefi ]; then
    OVMF=""
    for f in /usr/share/ovmf/OVMF.fd /usr/share/OVMF/OVMF_CODE.fd /usr/share/qemu/OVMF.fd; do
        [ -f "$f" ] && { OVMF="$f"; break; }
    done
    [ -n "$OVMF" ] || die "OVMF firmware bulunamadı (UEFI testi için gerekli)"
    QEMU_ARGS+=(-bios "$OVMF")
    echo ">> UEFI modu, firmware: $OVMF"
else
    echo ">> BIOS modu (SeaBIOS)"
fi

if [ -n "$ISO" ]; then
    [ -f "$ISO" ] || die "iso yok: $ISO"
    QEMU_ARGS+=(-cdrom "$ISO" -boot d)
    TARGET="$ISO"
else
    [ -f "$IMG" ] || die "imaj yok: $IMG"
    # USB'den boot senaryosunu taklit et: imaj bir USB depolama aygıtı olarak
    # takılır. Çoğu firmware bunu gerçek USB gibi ele alır.
    QEMU_ARGS+=(-drive "file=$IMG,format=raw,if=none,id=usbdisk"
                -device usb-ehci,id=ehci
                -device usb-storage,bus=ehci.0,drive=usbdisk
                -boot menu=off)
    TARGET="$IMG"
fi

LOG="$(mktemp)"
trap 'rm -f "$LOG"' EXIT

echo ">> $TARGET, $SECONDS_LIMIT sn boyunca boot ediliyor..."
timeout "$SECONDS_LIMIT" qemu-system-x86_64 "${QEMU_ARGS[@]}" \
    -serial "file:$LOG" >/dev/null 2>&1 || true

echo
echo "=== Seri konsol çıktısı (son 40 satır) ==="
if [ -s "$LOG" ]; then
    tail -40 "$LOG"
else
    echo "(boş — çekirdek seri konsola yazmadı)"
fi
echo

# ── Aşama tespiti ───────────────────────────────────────────────────────────

echo "=== Aşama tespiti ==="
stage() {
    if grep -qi -- "$1" "$LOG" 2>/dev/null; then
        printf '  \033[32mULASILDI\033[0m %s\n' "$2"
        return 0
    fi
    printf '  ----     %s\n' "$2"
    return 1
}

# NOT: cmdline'da loglevel=4 oldugu icin "Linux version" gibi KERN_INFO
# satirlari seri konsola yazilmaz. Bu yuzden "cekirdek yuklendi" kanitini
# userspace satirlarindan da kabul ediyoruz: init calistiysa cekirdek
# zorunlu olarak yuklenmis demektir.
BOOTED=0
stage "Linux version"              "cekirdek banner'i (loglevel=4 ile gizli olabilir)"
stage "Unpacking initramfs\|Trying to unpack rootfs" "initramfs aciliyor"
stage "Freeing unused kernel"      "cekirdek baslatma tamamlandi"
stage "Starting syslogd\|Starting network\|Running sysctl" "init (rcS) calisti" && BOOTED=1
stage "Starting MCOS"              "MCOS baslatma betigi (S99mcos) calisti" && BOOTED=1
stage "mcos-panel\|MCOS TUI\|hazirlaniyor" "panel baslatildi"

# Yukaridakilerden hicbiri yoksa ama log doluysa, en azindan bir sey calisti.
if [ "$BOOTED" = 0 ] && [ -s "$LOG" ]; then
    if grep -qiE "init|systemd|sh: |BusyBox" "$LOG" 2>/dev/null; then
        printf '  \033[32mULASILDI\033[0m userspace basladi (genel tespit)\n'
        BOOTED=1
    fi
fi

echo
echo "=== Bilinen hatalar ==="
PROBLEM=0
for pat in "Kernel panic" "not syncing" "Unable to mount root" \
           "No bootable device" "Boot failed" "Failed to load" \
           "Invalid partition" "unknown-block"; do
    if grep -qi -- "$pat" "$LOG" 2>/dev/null; then
        printf '  \033[31mBULUNDU\033[0m %s\n' "$pat"
        grep -i -m2 -- "$pat" "$LOG" | sed 's/^/            /'
        PROBLEM=1
    fi
done
[ "$PROBLEM" = 0 ] && echo "  (yok)"

echo
echo "=== Kritik olmayan uyarilar ==="
WARNED=0
for pat in "setfont: ERROR" "Unable to find file" "loadkeys" "fc-cache"; do
    if grep -qi -- "$pat" "$LOG" 2>/dev/null; then
        printf '  uyari: %s\n' "$(grep -i -m1 -- "$pat" "$LOG" | tr -d '\r')"
        WARNED=1
    fi
done
[ "$WARNED" = 0 ] && echo "  (yok)"

echo
if [ "$BOOTED" = 1 ] && [ "$PROBLEM" = 0 ]; then
    echo "SONUÇ: Önyükleyici çekirdeği yükledi ve panik yok — boot yolu ÇALIŞIYOR."
    exit 0
fi
if [ "$BOOTED" = 0 ]; then
    echo "SONUÇ: Çekirdek HİÇ yüklenmedi — önyükleyici/bölüm tablosu sorunu."
    exit 1
fi
echo "SONUÇ: Çekirdek yüklendi ama hata var (yukarıya bakın)."
exit 1
