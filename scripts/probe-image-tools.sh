#!/bin/sh
# Kalıcı USB imajı üretmek için hangi araçların kullanılabilir olduğunu ölçer.
# Salt-okunur teşhis; hiçbir şey oluşturmaz veya değiştirmez.

echo "=== Araç varlığı ==="
for t in grub-mkrescue grub-install grub-mkimage grub-bios-setup \
         xorriso mformat mcopy mmd mdir \
         mkfs.vfat mkfs.ext4 mke2fs \
         sfdisk parted partx losetup \
         syslinux extlinux dd truncate blkid findfs; do
    p="$(command -v "$t" 2>/dev/null)"
    if [ -n "$p" ]; then
        printf '  %-18s %s\n' "$t" "$p"
    else
        printf '  %-18s YOK\n' "$t"
    fi
done

echo
echo "=== mke2fs -d destegi (rootsuz ext4 doldurma) ==="
mke2fs -V 2>&1 | head -2
if mke2fs 2>&1 | grep -q '\-d '; then
    echo "  -d destekleniyor -> rootsuz ext4 uretilebilir"
else
    echo "  -d YOK -> mount gerekir (root)"
fi

echo
echo "=== sudo parolasiz mi? ==="
if sudo -n true 2>/dev/null; then
    echo "  EVET (losetup/mount yolu kullanilabilir)"
else
    echo "  HAYIR -> rootsuz yol (mtools + mke2fs -d) sart"
fi

echo
echo "=== Buildroot host araclari ==="
BRH=os/buildroot/output/host/bin
if [ -d "$BRH" ]; then
    for t in mformat mcopy mmd mke2fs genimage mkfs.vfat; do
        if [ -x "$BRH/$t" ]; then
            printf '  %-12s %s\n' "$t" "$BRH/$t"
        else
            printf '  %-12s yok\n' "$t"
        fi
    done
else
    echo "  $BRH yok"
fi

echo
echo "=== GRUB platform dizinleri ==="
for d in /usr/lib/grub/i386-pc /usr/lib/grub/x86_64-efi; do
    if [ -d "$d" ]; then
        printf '  %-28s var (%s dosya)\n' "$d" "$(ls "$d" | wc -l)"
    else
        printf '  %-28s YOK\n' "$d"
    fi
done
[ -f /usr/lib/grub/i386-pc/boot.img ] && echo "  boot.img var (BIOS MBR icin)" || echo "  boot.img YOK"

echo
echo "=== isohybrid MBR sablonu (BIOS USB boot icin) ==="
for f in /usr/lib/ISOLINUX/isohdpfx.bin /usr/lib/syslinux/isohdpfx.bin \
         /usr/lib/syslinux/mbr/isohdpfx.bin; do
    [ -f "$f" ] && echo "  $f"
done
echo "  (bos = isohybrid sablonu yok)"
