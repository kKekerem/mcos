#!/bin/bash
# mkusb.sh — MCOS için KALICI USB disk imajı üretir (BIOS veya UEFI).
#
# Neden ISO değil de disk imajı:
#   ISO bir salt-okunur CD dosya sistemidir; USB'ye yazıldığında hem kalıcı
#   veri alanı yoktur hem de birçok BIOS/UEFI firmware'i onu boot edilebilir
#   görmez (hibrit MBR olmadan). Gerçek bölüm tablolu bir disk imajı her iki
#   sorunu da çözer.
#
# Neden root gerekmiyor:
#   FAT bölümü mtools (mformat/mcopy) ile, ext4 bölümü "mke2fs -d" ile
#   doğrudan DOSYA üzerinde üretilir; hiçbir yerde mount/losetup yok.
#
# Kalıcılık nasıl çalışıyor:
#   İkinci bölüm "MCOS-DATA" etiketiyle ext4 olarak biçimlendirilir. Açılışta
#   S99mcos onu mcos-findfs ile bulup /data'ya bağlar. Java kurulumları ve
#   sunucular /data altında yaşadığı için reboot'ta korunur.
#
# Kullanım:
#   scripts/mkusb.sh --mode uefi --out dist/mcos-uefi.img
#   scripts/mkusb.sh --mode bios --out dist/mcos-bios.img --size 4G

set -euo pipefail

# ── Varsayılanlar ───────────────────────────────────────────────────────────

MODE=""
OUT=""
TOTAL_SIZE="4G"
BOOT_MB=768                 # kernel (~10MB) + initrd (~122MB) + yedek pay
BR_OUTPUT="${BR_OUTPUT:-os/buildroot/output}"
KERNEL=""
INITRD=""
CMDLINE_EXTRA=""

die() { printf 'mkusb: HATA: %s\n' "$*" >&2; exit 1; }
info() { printf '>> %s\n' "$*"; }

# ── Argümanlar ──────────────────────────────────────────────────────────────

while [ $# -gt 0 ]; do
    case "$1" in
        --mode)    MODE="${2:-}"; shift 2 ;;
        --out)     OUT="${2:-}"; shift 2 ;;
        --size)    TOTAL_SIZE="${2:-}"; shift 2 ;;
        --boot-mb) BOOT_MB="${2:-}"; shift 2 ;;
        --kernel)  KERNEL="${2:-}"; shift 2 ;;
        --initrd)  INITRD="${2:-}"; shift 2 ;;
        --cmdline) CMDLINE_EXTRA="${2:-}"; shift 2 ;;
        -h|--help)
            sed -n '2,25p' "$0"; exit 0 ;;
        *) die "bilinmeyen seçenek: $1" ;;
    esac
done

case "$MODE" in
    uefi|bios) ;;
    *) die "--mode uefi veya --mode bios olmalı" ;;
esac
[ -n "$OUT" ] || die "--out gerekli"

[ -n "$KERNEL" ] || KERNEL="$BR_OUTPUT/images/bzImage"
[ -n "$INITRD" ] || INITRD="$BR_OUTPUT/images/rootfs.cpio.gz"
[ -f "$KERNEL" ] || die "çekirdek bulunamadı: $KERNEL (önce 'make os' çalıştırın)"
[ -f "$INITRD" ] || die "initramfs bulunamadı: $INITRD (önce 'make os' çalıştırın)"

# ── Gerekli araçlar ─────────────────────────────────────────────────────────

need() { command -v "$1" >/dev/null 2>&1 || die "gerekli araç yok: $1"; }
need truncate
need dd
need sfdisk
need mformat
need mcopy
need mmd
need mke2fs

SYSLINUX_BIN="$BR_OUTPUT/build/syslinux-6.03/bios/mtools/syslinux"
SYSLINUX_LIB="$BR_OUTPUT/host/share/syslinux"

if [ "$MODE" = bios ]; then
    # ÖNEMLİ: syslinux kurucusu ile .c32 modülleri AYNI SÜRÜM olmalı, yoksa
    # önyükleyici "Failed to load libcom32.c32" ile durur. Bu yüzden host'un
    # syslinux'ü (6.04) DEĞİL, Buildroot'un derlediği 6.03 kullanılıyor.
    [ -x "$SYSLINUX_BIN" ] || die "syslinux kurucusu yok: $SYSLINUX_BIN (önce 'make os')"
    [ -f "$SYSLINUX_LIB/ldlinux.c32" ] || die "syslinux modülleri yok: $SYSLINUX_LIB"
    MBR_BIN="$SYSLINUX_LIB/mbr.bin"
    [ -f "$MBR_BIN" ] || MBR_BIN="/usr/lib/SYSLINUX/mbr.bin"
    [ -f "$MBR_BIN" ] || die "MBR önyükleyici bulunamadı (mbr.bin)"
else
    need grub-mkimage
    [ -d /usr/lib/grub/x86_64-efi ] || die "GRUB EFI platformu yok: /usr/lib/grub/x86_64-efi"
fi

# ── Boyut hesabı ────────────────────────────────────────────────────────────

to_mb() {
    case "$1" in
        *G|*g) echo $(( ${1%[Gg]} * 1024 )) ;;
        *M|*m) echo "${1%[Mm]}" ;;
        *)     echo "$1" ;;
    esac
}

TOTAL_MB="$(to_mb "$TOTAL_SIZE")"
ALIGN_MB=1                                   # p1 1 MiB'de başlar (hizalama)
DATA_MB=$(( TOTAL_MB - BOOT_MB - ALIGN_MB - 1 ))
[ "$DATA_MB" -ge 64 ] || die "toplam boyut çok küçük: boot=${BOOT_MB}M için en az $(( BOOT_MB + 66 ))M gerekli"

KSIZE_MB=$(( ( $(stat -c%s "$KERNEL") + $(stat -c%s "$INITRD") ) / 1048576 + 8 ))
[ "$BOOT_MB" -ge "$KSIZE_MB" ] || die "boot bölümü küçük: çekirdek+initramfs ${KSIZE_MB}M, --boot-mb $BOOT_MB"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

info "mod=$MODE toplam=${TOTAL_MB}M boot=${BOOT_MB}M veri=${DATA_MB}M"
info "çekirdek=$KERNEL ($(( $(stat -c%s "$KERNEL") / 1048576 ))M)"
info "initramfs=$INITRD ($(( $(stat -c%s "$INITRD") / 1048576 ))M)"

# ── Çekirdek komut satırı ───────────────────────────────────────────────────
#
# root= YOK ve olmamalı: sistem tamamen initramfs'ten çalışır (bkz.
# mcos-install baş yorumu — cpio içinde /init olduğu için çekirdek root='u
# yok sayar). Kalıcılık, açılışta S99mcos'un MCOS-DATA bölümünü /data'ya
# bağlamasıyla sağlanır.
#
# "quiet" BİLEREK yok: bir şey ters giderse kullanıcı gerçek çekirdek
# mesajını görmeli.
# Çözünürlük ve komut satırının TEK tanımı (mkiso.sh ve mcos-install ile
# aynı olmak zorunda; scripts/test-display-logic.sh doğrular).
. "$(dirname "$0")/lib/display.sh"

CMDLINE="${MCOS_CMDLINE_BASE} ${CMDLINE_EXTRA}"

# ── 1) FAT32 boot bölümünü üret (mount YOK) ─────────────────────────────────

BOOTIMG="$WORK/boot.img"
info "[1/5] FAT32 boot bölümü oluşturuluyor (${BOOT_MB}M)"
truncate -s "${BOOT_MB}M" "$BOOTIMG"
# -F: FAT32'yi zorla. Küçük bölümlerde mformat FAT16 seçebilir; UEFI ESP için
# FAT32 (veya FAT16) kabul edilir ama tutarlılık için sabitliyoruz.
mformat -i "$BOOTIMG" -F -v MCOS-BOOT ::

mmd -i "$BOOTIMG" ::/EFI ::/EFI/BOOT >/dev/null 2>&1 || true
mcopy -i "$BOOTIMG" -o "$KERNEL" ::/bzImage
mcopy -i "$BOOTIMG" -o "$INITRD" ::/initrd.img

if [ "$MODE" = uefi ]; then
    info "[2/5] GRUB EFI önyükleyicisi üretiliyor"
    # Standalone GRUB EFI: prefix (hd0,gpt1)/EFI/BOOT olarak gömülür, böylece
    # grub.cfg'yi ESP üzerinde kendi dizininde arar.
    grub-mkimage -O x86_64-efi -o "$WORK/BOOTX64.EFI" \
        -p /EFI/BOOT \
        part_gpt part_msdos fat ext2 normal boot linux configfile \
        search search_label search_fs_uuid echo test all_video gfxterm \
        >/dev/null

    {
        mcos_grub_header 3
        cat <<EOF

menuentry "MCOS (Minecraft Server OS)" {
    set gfxpayload=keep
    linux /bzImage $CMDLINE
    initrd /initrd.img
}

menuentry "MCOS (kurtarma - VGA metin modu, ayrintili gunluk)" {
    set gfxpayload=text
    linux /bzImage ${MCOS_CMDLINE_RECOVERY}
    initrd /initrd.img
}
EOF
    } > "$WORK/grub.cfg"
    mcopy -i "$BOOTIMG" -o "$WORK/BOOTX64.EFI" ::/EFI/BOOT/BOOTX64.EFI
    mcopy -i "$BOOTIMG" -o "$WORK/grub.cfg"    ::/EFI/BOOT/grub.cfg
else
    info "[2/5] syslinux 6.03 önyükleyicisi hazırlanıyor"
    mmd -i "$BOOTIMG" ::/syslinux >/dev/null 2>&1 || true
    for m in ldlinux.c32 libutil.c32 menu.c32 libcom32.c32; do
        [ -f "$SYSLINUX_LIB/$m" ] && mcopy -i "$BOOTIMG" -o "$SYSLINUX_LIB/$m" "::/syslinux/$m"
    done
    cat > "$WORK/syslinux.cfg" <<EOF
UI menu.c32
PROMPT 0
TIMEOUT 30
DEFAULT mcos

LABEL mcos
  MENU LABEL MCOS (Minecraft Server OS)
  LINUX /bzImage
  INITRD /initrd.img
  APPEND $CMDLINE

LABEL recovery
  MENU LABEL MCOS (kurtarma - VGA metin modu)
  LINUX /bzImage
  INITRD /initrd.img
  APPEND ${MCOS_CMDLINE_RECOVERY}
EOF
    mcopy -i "$BOOTIMG" -o "$WORK/syslinux.cfg" ::/syslinux/syslinux.cfg
fi

# ── 3) ext4 kalıcı veri bölümünü üret (mount YOK) ───────────────────────────

DATAIMG="$WORK/data.img"
SEED="$WORK/seed"
info "[3/5] ext4 kalıcı veri bölümü oluşturuluyor (${DATA_MB}M, etiket MCOS-DATA)"
mkdir -p "$SEED"
# Boş bir /data iskeleti: daemon ilk açılışta config.json'u kendi yazar.
mkdir -p "$SEED/servers" "$SEED/java" "$SEED/log"
# mke2fs -d: dizini mount etmeden dosya sistemine gömer (e2fsprogs >= 1.43).
mke2fs -q -t ext4 -L MCOS-DATA -d "$SEED" -F "$DATAIMG" "${DATA_MB}M"

# ── 4) Bölüm tablosunu yaz ──────────────────────────────────────────────────

info "[4/5] Disk imajı ve bölüm tablosu yazılıyor: $OUT"
mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
truncate -s "${TOTAL_MB}M" "$OUT"

BOOT_START=$(( ALIGN_MB * 2048 ))                  # sektör (512B)
BOOT_SECTORS=$(( BOOT_MB * 2048 ))
DATA_START=$(( BOOT_START + BOOT_SECTORS ))
DATA_SECTORS=$(( DATA_MB * 2048 ))

if [ "$MODE" = uefi ]; then
    # GPT + EFI System Partition. sfdisk'te "U" tipi = EFI System.
    sfdisk --quiet --wipe always "$OUT" >/dev/null <<EOF
label: gpt
first-lba: 34
${OUT}1 : start=$BOOT_START, size=$BOOT_SECTORS, type=U, name="MCOS-BOOT"
${OUT}2 : start=$DATA_START, size=$DATA_SECTORS, type=linux, name="MCOS-DATA"
EOF
else
    # MBR. p1 FAT32-LBA (tip c) ve ÖNYÜKLENEBİLİR işaretli olmalı; birçok BIOS
    # aktif bayrak olmadan USB'den boot etmez.
    sfdisk --quiet --wipe always "$OUT" >/dev/null <<EOF
label: dos
${OUT}1 : start=$BOOT_START, size=$BOOT_SECTORS, type=c, bootable
${OUT}2 : start=$DATA_START, size=$DATA_SECTORS, type=83
EOF
fi

# Bölüm içeriklerini yerleştir.
dd if="$BOOTIMG" of="$OUT" bs=512 seek="$BOOT_START" conv=notrunc status=none
dd if="$DATAIMG" of="$OUT" bs=512 seek="$DATA_START" conv=notrunc status=none

# ── 5) Önyükleyiciyi diske gömme ────────────────────────────────────────────

if [ "$MODE" = bios ]; then
    info "[5/5] MBR önyükleyici ve syslinux kuruluyor"
    # MBR bootstrap: ilk 440 bayt. Bölüm tablosu (446+) korunur.
    dd if="$MBR_BIN" of="$OUT" bs=440 count=1 conv=notrunc status=none
    # syslinux'u FAT bölümünün İÇİNE kur. --offset bayt cinsindendir.
    "$SYSLINUX_BIN" --offset $(( BOOT_START * 512 )) --install --directory /syslinux "$OUT"
else
    info "[5/5] UEFI için ek gömme gerekmiyor (ESP + BOOTX64.EFI yeterli)"
fi

sync

# ── Doğrulama ───────────────────────────────────────────────────────────────

echo
info "Doğrulama:"
sfdisk --list "$OUT" 2>/dev/null | sed 's/^/    /'
echo
info "Boot bölümü içeriği:"
mdir -i "$BOOTIMG" -/ :: 2>/dev/null | sed 's/^/    /' || true
echo
info "Veri bölümü etiketi: $(dumpe2fs -h "$DATAIMG" 2>/dev/null | grep -i 'volume name' || echo '?')"
echo
info "HAZIR: $OUT ($(du -h "$OUT" | cut -f1))"
echo
cat <<EOF
USB'ye yazmak için (AYGITI DOĞRU SEÇTİĞİNİZDEN EMİN OLUN — veri silinir):

    sudo dd if=$OUT of=/dev/sdX bs=4M status=progress conv=fsync

Windows'ta: Rufus'ta "DD Image" modu, veya BalenaEtcher.
EOF
