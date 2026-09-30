#!/bin/bash
# exp-grub-dd.sh — DENEY: mcos-install'in kullanacağı "hedefte önyükleyici
# aracı GEREKTİRMEYEN" BIOS boot yöntemini kanıtlar.
#
# Yöntem:
#   1. Build zamanında (host'ta) kendine yeten bir GRUB core.img üret:
#      grub-mkimage -O i386-pc  → gereken TÜM modüller gömülü
#   2. Hedef diske yalnızca dd ve cp:
#        boot.img  → sektör 0 (ilk 440 bayt; bölüm tablosu korunur)
#        core.img  → sektör 1.. (MBR boşluğu, p1 1 MiB'de başladığı için 2047
#                    sektör yer var)
#        grub.cfg  → FAT bölümünde /boot/grub/grub.cfg
#
# Neden bu yöntem: hedef rootfs'te grub-install, grub-mkimage ve
# /usr/lib/grub/i386-pc HİÇ YOK (bkz. scripts/probe-target-boot.sh). Mevcut
# mcos-install "command -v grub-install" kontrolüne takılıp SESSİZCE hiçbir
# önyükleyici yazmıyor; BIOS da diski bootable görmüyor.
#
# boot.img'in 0x5c ofsetindeki kernel_sector alanı zaten LBA 1'i gösteriyor
# (hexdump ile doğrulandı), bu yüzden elle yama gerekmiyor.

set -euo pipefail

OUT="${1:-/tmp/exp-grub.img}"
BR_OUTPUT="${BR_OUTPUT:-os/buildroot/output}"
KERNEL="$BR_OUTPUT/images/bzImage"
INITRD="$BR_OUTPUT/images/rootfs.cpio.gz"
GRUB_PC=/usr/lib/grub/i386-pc

BOOT_MB=768
TOTAL_MB=1024
DATA_MB=$(( TOTAL_MB - BOOT_MB - 2 ))

for f in "$KERNEL" "$INITRD" "$GRUB_PC/boot.img"; do
    [ -f "$f" ] || { echo "yok: $f" >&2; exit 2; }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo ">> 1) Kendine yeten core.img uretiliyor (grub-mkimage, host)"
# prefix: core.img grub.cfg'yi NEREDE arayacak. p1 FAT olduğu için
# (hd0,msdos1)/boot/grub.
# Modüller GÖMÜLÜ olmalı: hedef diskte .mod dosyası aramasın.
grub-mkimage -O i386-pc -o "$WORK/core.img" \
    -p '(hd0,msdos1)/boot/grub' \
    biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot \
    search search_fs_file search_label echo test all_video vbe vga gfxterm \
    minicmd sleep reboot halt
echo "   core.img: $(stat -c%s "$WORK/core.img") bayt"

if [ "$(stat -c%s "$WORK/core.img")" -gt $(( 2047 * 512 )) ]; then
    echo "HATA: core.img MBR bosluguna sigmiyor ($(( 2047 * 512 )) bayt)" >&2
    exit 1
fi

echo ">> 2) FAT32 boot bolumu"
truncate -s "${BOOT_MB}M" "$WORK/boot.part"
mformat -i "$WORK/boot.part" -F -v MCOS-BOOT ::
mmd -i "$WORK/boot.part" ::/boot ::/boot/grub >/dev/null 2>&1 || true
mcopy -i "$WORK/boot.part" -o "$KERNEL" ::/boot/bzImage
mcopy -i "$WORK/boot.part" -o "$INITRD" ::/boot/initrd.img

cat > "$WORK/grub.cfg" <<'EOF'
set timeout=3
set default=0
insmod all_video
insmod gfxterm
terminal_output console

menuentry "MCOS (Minecraft Server OS)" {
    linux /boot/bzImage console=tty0 console=ttyS0,115200 consoleblank=0 loglevel=4 fbcon=nodefer
    initrd /boot/initrd.img
}
EOF
mcopy -i "$WORK/boot.part" -o "$WORK/grub.cfg" ::/boot/grub/grub.cfg

echo ">> 3) ext4 kalici veri bolumu"
mkdir -p "$WORK/seed"
mke2fs -q -t ext4 -L MCOS-DATA -d "$WORK/seed" -F "$WORK/data.part" "${DATA_MB}M"

echo ">> 4) Disk imaji + MBR bolum tablosu"
rm -f "$OUT"
truncate -s "${TOTAL_MB}M" "$OUT"
BOOT_START=2048
BOOT_SECT=$(( BOOT_MB * 2048 ))
DATA_START=$(( BOOT_START + BOOT_SECT ))
DATA_SECT=$(( DATA_MB * 2048 ))

sfdisk --quiet --wipe always "$OUT" >/dev/null <<EOF
label: dos
${OUT}1 : start=$BOOT_START, size=$BOOT_SECT, type=c, bootable
${OUT}2 : start=$DATA_START, size=$DATA_SECT, type=83
EOF

dd if="$WORK/boot.part" of="$OUT" bs=512 seek="$BOOT_START" conv=notrunc status=none
dd if="$WORK/data.part" of="$OUT" bs=512 seek="$DATA_START" conv=notrunc status=none

echo ">> 5) Onyukleyici: SADECE dd (hedefte arac gerekmez)"
# boot.img'in ilk 440 bayti = MBR bootstrap. 440..511 bolum tablosu ve imza,
# onlara DOKUNULMAZ.
dd if="$GRUB_PC/boot.img" of="$OUT" bs=440 count=1 conv=notrunc status=none
# core.img sektor 1'den itibaren MBR bosluguna.
dd if="$WORK/core.img" of="$OUT" bs=512 seek=1 conv=notrunc status=none

sync
echo
echo ">> HAZIR: $OUT"
echo ">> Dogrulama:"
sfdisk --list "$OUT" | sed 's/^/    /'
echo "    MBR ilk baytlar: $(dd if="$OUT" bs=1 count=4 status=none | od -An -tx1 | tr -d ' \n')"
echo "    0x55AA imzasi  : $(dd if="$OUT" bs=1 skip=510 count=2 status=none | od -An -tx1 | tr -d ' \n')"
echo "    sektor 1 (core): $(dd if="$OUT" bs=512 skip=1 count=1 status=none | od -An -tx1 -N4 | tr -d ' \n')"
