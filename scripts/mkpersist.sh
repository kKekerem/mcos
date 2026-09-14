#!/bin/sh
# mkpersist.sh — KALICI MCOS disk imajı üretir (USB veya SSD için).
#
# ════════════════════════════════════════════════════════════════════════════
# mkusb.sh'ten FARKI
# ════════════════════════════════════════════════════════════════════════════
#
# mkusb.sh iki bölüm yapar: boot (FAT32) + veri (ext4). Sistem yine
# initramfs'ten, yani RAM'den çalışır; yalnızca /data kalıcıdır.
#
# Bu betik ÜÇ bölüm yapar ve çekirdek komut satırına mcos.root= ekler:
#
#     p1  FAT32  MCOS-BOOT   çekirdek + initrd + GRUB
#     p2  ext4   MCOS-ROOT   KÖK DOSYA SİSTEMİ  ← sistem buradan çalışır
#     p3  ext4   MCOS-DATA   sunucular, Java, ayarlar
#
# Sonuç: yazılan her şey kalıcıdır. Kurulan paket, değişen ayar, oluşan
# dosya yeniden başlatmayı atlatır — çünkü sistem RAM'de değil DİSKTE.
#
# ── Neden root gerekmiyor ───────────────────────────────────────────────────
# Hiçbir yerde mount/losetup yok:
#   FAT32  → mtools (mformat/mcopy) doğrudan dosya üzerinde çalışır
#   ext4   → "mke2fs -d DIZIN" dizini bağlamadan gömer (e2fsprogs >= 1.43)
#   tablo  → sfdisk imaj dosyasına yazar
#
# Yalnızca imajı GERÇEK BİR AYGITA yazmak ayrıcalık ister; onu bu betik
# yapmaz (cmd/mcos-flash yapar).
#
# Kullanım:
#   scripts/mkpersist.sh --out dist/mcos-persist.img [--size 16G] [--mode uefi|bios|both]

set -eu

OUT="dist/mcos-persist.img"
TOTAL_SIZE="16G"
MODE="both"
BR_OUTPUT="${BR_OUTPUT:-os/buildroot/output}"

BOOT_MB=768        # çekirdek (~10MB) + initrd (~128MB) + GRUB + pay
ROOT_MB=4096       # kök dosya sistemi (açılmış rootfs ~300MB + büyüme payı)
ALIGN_MB=1

die() { printf '\033[31mHATA:\033[0m %s\n' "$1" >&2; exit 1; }
info() { printf '\033[36m>>\033[0m %s\n' "$1"; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 gerekli (paket: ${2:-$1})"; }

while [ $# -gt 0 ]; do
    case "$1" in
        --out)  OUT="${2:-}"; shift 2 ;;
        --size) TOTAL_SIZE="${2:-}"; shift 2 ;;
        --mode) MODE="${2:-}"; shift 2 ;;
        --br-output) BR_OUTPUT="${2:-}"; shift 2 ;;
        -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
        *) die "bilinmeyen seçenek: $1" ;;
    esac
done

case "$MODE" in
    uefi|bios|both) ;;
    *) die "--mode uefi, bios veya both olmalı" ;;
esac

need truncate coreutils
need sfdisk util-linux
need mformat mtools
need mcopy mtools
need mke2fs e2fsprogs
need dd coreutils

KERNEL="$BR_OUTPUT/images/bzImage"
INITRD="$BR_OUTPUT/images/rootfs.cpio.gz"
TARGET="$BR_OUTPUT/target"

[ -f "$KERNEL" ] || die "çekirdek yok: $KERNEL (önce 'make os')"
[ -f "$INITRD" ] || die "initramfs yok: $INITRD (önce 'make os')"
[ -d "$TARGET" ] || die "hedef rootfs yok: $TARGET (önce 'make os')"

# Boyutu MB'a çevir.
case "$TOTAL_SIZE" in
    *G|*g) TOTAL_MB=$(( ${TOTAL_SIZE%[Gg]} * 1024 )) ;;
    *M|*m) TOTAL_MB=${TOTAL_SIZE%[Mm]} ;;
    *)     TOTAL_MB="$TOTAL_SIZE" ;;
esac

MIN_MB=$(( ALIGN_MB + BOOT_MB + ROOT_MB + 512 ))
[ "$TOTAL_MB" -ge "$MIN_MB" ] || die "en az ${MIN_MB}M gerekli (verilen: ${TOTAL_MB}M)"

DATA_MB=$(( TOTAL_MB - ALIGN_MB - BOOT_MB - ROOT_MB ))

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Çözünürlük ve komut satırının tek tanımı.
. "$(dirname "$0")/lib/display.sh"

# ════════════════════════════════════════════════════════════════════════════
# KALICILIK ANAHTARI
# ════════════════════════════════════════════════════════════════════════════
#
# Canlı ISO'nun komut satırında bu YOKTUR, bu yüzden ISO RAM'den çalışır.
# Burada VARDIR: initramfs'teki /init bunu görünce MCOS-ROOT bölümünü bağlar
# ve switch_root ile içine geçer. Ayrımı yapan tek şey bu parametredir.
CMDLINE="$MCOS_CMDLINE_BASE mcos.root=LABEL=MCOS-ROOT"

info "Kalıcı MCOS imajı: $OUT"
info "  toplam ${TOTAL_MB}M = boot ${BOOT_MB}M + kök ${ROOT_MB}M + veri ${DATA_MB}M"

# ── 1) FAT32 boot bölümü ────────────────────────────────────────────────────

BOOTIMG="$WORK/boot.img"
info "[1/5] FAT32 boot bölümü (${BOOT_MB}M)"
truncate -s "${BOOT_MB}M" "$BOOTIMG"
# -F: FAT32'yi ZORLA. Küçük bölümlerde mformat FAT16 seçer ve UEFI ESP'si
# FAT32 bekler; bu bayrak olmadan bazı firmware'ler bölümü hiç görmez.
mformat -i "$BOOTIMG" -F -v MCOS-BOOT ::

mmd -i "$BOOTIMG" ::/boot ::/boot/grub ::/EFI ::/EFI/BOOT >/dev/null 2>&1 || true
mcopy -i "$BOOTIMG" -o "$KERNEL" ::/boot/bzImage
mcopy -i "$BOOTIMG" -o "$INITRD" ::/boot/initrd.img

# grub.cfg — kalıcılık parametresiyle.
{
    mcos_grub_header 3
    cat <<EOF

menuentry "MCOS (kalıcı kurulum)" {
    set gfxpayload=keep
    linux /boot/bzImage $CMDLINE
    initrd /boot/initrd.img
}

menuentry "MCOS (RAM'den çalıştır — kurtarma)" {
    set gfxpayload=keep
    linux /boot/bzImage $MCOS_CMDLINE_BASE mcos.live
    initrd /boot/initrd.img
}

menuentry "MCOS (kurtarma — VGA metin kipi)" {
    set gfxpayload=text
    linux /boot/bzImage $MCOS_CMDLINE_RECOVERY mcos.live
    initrd /boot/initrd.img
}
EOF
} > "$WORK/grub.cfg"

mcopy -i "$BOOTIMG" -o "$WORK/grub.cfg" ::/boot/grub/grub.cfg
mcopy -i "$BOOTIMG" -o "$WORK/grub.cfg" ::/EFI/BOOT/grub.cfg

# ── 2) Önyükleyici imajları ─────────────────────────────────────────────────

info "[2/5] Önyükleyici üretiliyor (mod: $MODE)"

HAVE_BIOS=0
HAVE_UEFI=0

if [ "$MODE" != uefi ] && [ -d /usr/lib/grub/i386-pc ] && command -v grub-mkimage >/dev/null 2>&1; then
    # Gömülü ön-yapılandırma: bölümü ETİKETTEN bul. Sabit (hd0,msdos1)
    # yalnızca disk BIOS'ta ilk sırada olduğunda çalışır; USB genelde değildir.
    cat > "$WORK/early.cfg" <<'EARLY'
search --no-floppy --label --set=root MCOS-BOOT
set prefix=($root)/boot/grub
EARLY
    if grub-mkimage -O i386-pc -o "$WORK/core.img" \
        -c "$WORK/early.cfg" -p '/boot/grub' \
        biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot \
        search search_fs_file search_label search_fs_uuid echo test \
        all_video vbe vga gfxterm minicmd sleep reboot halt 2>/dev/null
    then
        cp /usr/lib/grub/i386-pc/boot.img "$WORK/boot_mbr.img"
        HAVE_BIOS=1
        info "     BIOS önyükleyici hazır ($(stat -c%s "$WORK/core.img") bayt)"
    fi
fi

if [ "$MODE" != bios ] && [ -d /usr/lib/grub/x86_64-efi ] && command -v grub-mkimage >/dev/null 2>&1; then
    if grub-mkimage -O x86_64-efi -o "$WORK/BOOTX64.EFI" -p /EFI/BOOT \
        part_gpt part_msdos fat ext2 normal configfile linux boot \
        search search_fs_file search_label search_fs_uuid echo test \
        all_video gfxterm efi_gop efi_uga minicmd sleep reboot halt 2>/dev/null
    then
        mcopy -i "$BOOTIMG" -o "$WORK/BOOTX64.EFI" ::/EFI/BOOT/BOOTX64.EFI
        HAVE_UEFI=1
        info "     UEFI önyükleyici hazır ($(stat -c%s "$WORK/BOOTX64.EFI") bayt)"
    fi
fi

[ "$HAVE_BIOS" = 1 ] || [ "$HAVE_UEFI" = 1 ] || \
    die "hiçbir önyükleyici üretilemedi (grub-pc-bin / grub-efi-amd64-bin kurun)"

# ── 3) ext4 KÖK bölümü — kalıcılığın kalbi ──────────────────────────────────

ROOTIMG="$WORK/root.img"
info "[3/5] ext4 kök bölümü (${ROOT_MB}M, etiket MCOS-ROOT)"

ROOTSEED="$WORK/rootseed"
mkdir -p "$ROOTSEED"

# Canlı rootfs'in TAMAMINI kopyala.
#
# Hariç tutulanlar çekirdek tarafından sağlanır veya ayrı bölüme gider:
#   proc sys dev run tmp  → çekirdek bağlar
#   data                  → p3
#   boot                  → p1
for d in bin etc lib lib32 lib64 opt root sbin usr var init; do
    [ -e "$TARGET/$d" ] || continue
    cp -a "$TARGET/$d" "$ROOTSEED/" 2>/dev/null || true
done
mkdir -p "$ROOTSEED/proc" "$ROOTSEED/sys" "$ROOTSEED/dev" "$ROOTSEED/run" \
         "$ROOTSEED/tmp" "$ROOTSEED/mnt" "$ROOTSEED/data" "$ROOTSEED/boot"
chmod 1777 "$ROOTSEED/tmp"

# İŞARET DOSYASI — /init switch_root yapmadan ÖNCE bunu arar.
#
# Rastgele bir ext4 bölümüne geçmek PID 1'i kaybettirir ve çekirdek panik
# verir. Bu dosya, bölümün gerçekten bir MCOS kökü olduğunun kanıtıdır.
{
    echo "MCOS kalıcı kök dosya sistemi"
    echo "üretildi: $(date 2>/dev/null || echo bilinmiyor)"
    echo "araç: mkpersist.sh"
} > "$ROOTSEED/etc/mcos-root"

mke2fs -q -t ext4 -L MCOS-ROOT -d "$ROOTSEED" -F "$ROOTIMG" "${ROOT_MB}M"
info "     kök dosya sistemi hazır ($(du -sh "$ROOTSEED" | cut -f1) içerik)"

# ── 4) ext4 VERİ bölümü ─────────────────────────────────────────────────────

DATAIMG="$WORK/data.img"
info "[4/5] ext4 veri bölümü (${DATA_MB}M, etiket MCOS-DATA)"

DATASEED="$WORK/dataseed"
mkdir -p "$DATASEED/servers" "$DATASEED/java" "$DATASEED/log" "$DATASEED/mcos"

# Çevrimdışı paketi tohumla: Java + Fabric + Via modları + playit.
# Böylece ilk sunucu kurulumunda yalnızca Mojang jar'ı indirilir.
if [ -d dist/offline ] && [ -n "$(ls -A dist/offline 2>/dev/null)" ]; then
    mkdir -p "$DATASEED/artifacts"
    cp dist/offline/* "$DATASEED/artifacts/" 2>/dev/null || true
    info "     çevrimdışı paket tohumlandı ($(du -sh dist/offline | cut -f1))"
else
    info "     çevrimdışı paket yok ('make offline-bundle' ile indirilir)"
fi

mke2fs -q -t ext4 -L MCOS-DATA -d "$DATASEED" -F "$DATAIMG" "${DATA_MB}M"

# ── 5) Bölüm tablosu ve birleştirme ─────────────────────────────────────────

info "[5/5] Disk imajı birleştiriliyor"
mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
truncate -s "${TOTAL_MB}M" "$OUT"

BOOT_START=$(( ALIGN_MB * 2048 ))
BOOT_SECTORS=$(( BOOT_MB * 2048 ))
ROOT_START=$(( BOOT_START + BOOT_SECTORS ))
ROOT_SECTORS=$(( ROOT_MB * 2048 ))
DATA_START=$(( ROOT_START + ROOT_SECTORS ))
DATA_SECTORS=$(( DATA_MB * 2048 ))

# MBR (dos) tablosu seçildi, GPT değil.
#
# MBR'li bir FAT32 bölümünde /EFI/BOOT/BOOTX64.EFI bulunan disk HEM Legacy
# BIOS HEM de çoğu UEFI firmware'i tarafından boot edilir. Böylece TEK imaj
# iki firmware türünde de çalışır ve kullanıcıya "hangisini seçeyim"
# sorulmaz.
sfdisk --quiet --wipe always "$OUT" >/dev/null <<EOF
label: dos
${OUT}1 : start=$BOOT_START, size=$BOOT_SECTORS, type=c, bootable
${OUT}2 : start=$ROOT_START, size=$ROOT_SECTORS, type=83
${OUT}3 : start=$DATA_START, size=$DATA_SECTORS, type=83
EOF

dd if="$BOOTIMG" of="$OUT" bs=512 seek="$BOOT_START" conv=notrunc status=none
dd if="$ROOTIMG" of="$OUT" bs=512 seek="$ROOT_START" conv=notrunc status=none
dd if="$DATAIMG" of="$OUT" bs=512 seek="$DATA_START" conv=notrunc status=none

if [ "$HAVE_BIOS" = 1 ]; then
    # boot.img'in ilk 440 baytı MBR bootstrap'tır; 440..511 bölüm tablosu ve
    # 0x55AA imzasıdır — onlara DOKUNULMAZ.
    dd if="$WORK/boot_mbr.img" of="$OUT" bs=440 count=1 conv=notrunc status=none
    CORE_SECTORS=$(( ( $(stat -c%s "$WORK/core.img") + 511 ) / 512 ))
    if [ "$CORE_SECTORS" -ge "$BOOT_START" ]; then
        die "core.img MBR boşluğuna sığmıyor ($CORE_SECTORS sektör)"
    fi
    dd if="$WORK/core.img" of="$OUT" bs=512 seek=1 conv=notrunc status=none
fi

sync

# ── Doğrulama ───────────────────────────────────────────────────────────────

echo
info "Doğrulama:"
sfdisk --list "$OUT" 2>/dev/null | sed 's/^/    /'
echo
info "Kök bölümü etiketi: $(dumpe2fs -h "$ROOTIMG" 2>/dev/null | sed -n 's/Filesystem volume name:[[:space:]]*//p')"
info "Veri bölümü etiketi: $(dumpe2fs -h "$DATAIMG" 2>/dev/null | sed -n 's/Filesystem volume name:[[:space:]]*//p')"
info "Önyükleyici: BIOS=$HAVE_BIOS UEFI=$HAVE_UEFI"
echo
info "HAZIR: $OUT ($(du -h "$OUT" | cut -f1))"
info "Bu imaj KALICIDIR — sistem RAM'den değil diskten çalışır."
echo
