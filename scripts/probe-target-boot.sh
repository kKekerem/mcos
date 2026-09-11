#!/bin/sh
# probe-target-boot.sh — HEDEF rootfs'te (imajın içinde) hangi önyükleyici
# araçlarının bulunduğunu ölçer.
#
# Neden önemli: mcos-install, OOBE'den USB'ye kalıcı kurulum yaparken
# önyükleyiciyi HEDEF SİSTEMİN İÇİNDEN kurar. Host'ta grub-install olması
# hiçbir şey ifade etmez — imajın içinde olması gerekir. Eksikse kurulum
# "başarılı" görünür ama disk boot edilemez (BIOS bootable aygıt görmez).

set -u
T="${1:-os/buildroot/output/target}"

if [ ! -d "$T" ]; then
    echo "HATA: hedef rootfs yok: $T" >&2
    echo "Once 'make os' calistirin." >&2
    exit 2
fi

echo "== Hedef rootfs: $T =="
echo

have() {
    # $1 = aciklama, $2.. = aranacak yollar (hedefe gore)
    _desc="$1"; shift
    for p in "$@"; do
        if [ -e "$T/$p" ]; then
            printf '  VAR    %-34s -> %s\n' "$_desc" "$p"
            return 0
        fi
    done
    printf '  YOK    %-34s (%s)\n' "$_desc" "$*"
    return 1
}

echo "1) GRUB (mcos-install su an bunu kullaniyor)"
GRUB_OK=1
have "grub-install ikilisi"    usr/sbin/grub-install sbin/grub-install usr/bin/grub-install || GRUB_OK=0
have "grub-mkimage ikilisi"    usr/bin/grub-mkimage bin/grub-mkimage || GRUB_OK=0
have "i386-pc platform dizini" usr/lib/grub/i386-pc boot/grub/i386-pc || GRUB_OK=0
have "i386-pc/boot.img"        usr/lib/grub/i386-pc/boot.img boot/grub/i386-pc/boot.img || GRUB_OK=0
have "i386-pc/*.mod modulleri" usr/lib/grub/i386-pc/normal.mod boot/grub/i386-pc/normal.mod || GRUB_OK=0
have "x86_64-efi platformu"    usr/lib/grub/x86_64-efi boot/grub/x86_64-efi || GRUB_OK=0

echo
echo "2) syslinux (mkusb.sh bunu kullaniyor; QEMU'da BIOS boot dogrulandi)"
SYS_OK=1
have "syslinux ikilisi"  usr/bin/syslinux bin/syslinux || SYS_OK=0
have "mbr.bin"           usr/share/syslinux/mbr.bin usr/lib/syslinux/mbr.bin || SYS_OK=0
have "ldlinux.c32"       usr/share/syslinux/ldlinux.c32 usr/lib/syslinux/ldlinux.c32 || SYS_OK=0
have "menu.c32"          usr/share/syslinux/menu.c32 usr/lib/syslinux/menu.c32 || SYS_OK=0
have "libcom32.c32"      usr/share/syslinux/libcom32.c32 usr/lib/syslinux/libcom32.c32 || SYS_OK=0

echo
echo "3) Bolumleme / dosya sistemi araclari"
have "parted"     usr/sbin/parted sbin/parted
have "sfdisk"     usr/sbin/sfdisk sbin/sfdisk
have "mkfs.vfat"  usr/sbin/mkfs.vfat sbin/mkfs.vfat
have "mkfs.ext4"  usr/sbin/mkfs.ext4 sbin/mkfs.ext4
have "blkid (util-linux mi busybox mu?)" usr/sbin/blkid sbin/blkid
have "findfs"     usr/sbin/findfs sbin/findfs
have "dd"         bin/dd usr/bin/dd
have "partprobe"  usr/sbin/partprobe sbin/partprobe

echo
echo "== SONUC =="
if [ "$GRUB_OK" = 1 ]; then
    echo "  GRUB yolu KULLANILABILIR"
else
    echo "  GRUB yolu EKSIK -> mcos-install'in grub-install cagrisi BASARISIZ olur."
    echo "  Mevcut kodda o cagri '|| log UYARI' ile susturuldugu icin kurulum"
    echo "  'basarili' gorunur ama diskte onyukleyici YOKTUR: BIOS bootable"
    echo "  aygit gormez. Kullanicinin bildirdigi hata tam olarak bu."
fi
if [ "$SYS_OK" = 1 ]; then
    echo "  syslinux yolu KULLANILABILIR (onerilen: mkusb.sh ile ayni, boot testi gecti)"
else
    echo "  syslinux yolu EKSIK -> hedefe kurulmasi gerekir (mcos.mk / defconfig)"
fi
