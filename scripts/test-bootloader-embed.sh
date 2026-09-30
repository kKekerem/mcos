#!/bin/sh
# test-bootloader-embed.sh — post-build.sh'nin gömdüğü önyükleyici parçalarının
# ÜRETİLEBİLİR ve KULLANILABİLİR olduğunu doğrular.
#
# Neden ayrı bir test: post-build.sh içindeki grub-mkimage çağrıları
# "2>/dev/null" ile susturulmuş ve başarısızlıkta yalnızca UYARI basıyor.
# Modül listesinde tek bir geçersiz ad olsa önyükleyici üretilmez, kurulum
# sessizce boot etmeyen bir disk üretir — düzeltmeye çalıştığımız hatanın
# aynısı. Bu test o listeleri build'den BAĞIMSIZ olarak sınar.
#
# Kullanım: sh scripts/test-bootloader-embed.sh

set -eu

FAILED=0
ok()   { printf '  ok   %s\n' "$*"; }
bad()  { printf '  FAIL %s\n' "$*"; FAILED=1; }

# post-build.sh ile AYNI modül listeleri. Değiştirirseniz orayı da güncelleyin.
MODULES_PC="biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot
            search search_fs_file search_label echo test all_video vbe vga
            gfxterm minicmd sleep reboot halt"
MODULES_EFI="part_gpt part_msdos fat ext2 normal configfile linux boot
             search search_fs_file search_label echo test all_video gfxterm
             minicmd sleep reboot halt"

# MBR boşluğu: p1 1 MiB'de (sektör 2048) başladığı için sektör 1..2047 kullanılabilir.
MAX_CORE_BYTES=$(( 2047 * 512 ))

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

echo "1) Host araclari"
if command -v grub-mkimage >/dev/null 2>&1; then
    ok "grub-mkimage var"
else
    bad "grub-mkimage YOK — 'sudo apt-get install grub-common' gerekir"
    echo; echo "SONUC: BASARISIZ (host araci eksik)"; exit 1
fi
if [ -f /usr/lib/grub/i386-pc/boot.img ]; then
    ok "i386-pc/boot.img var ($(stat -c%s /usr/lib/grub/i386-pc/boot.img) bayt)"
else
    bad "/usr/lib/grub/i386-pc/boot.img YOK — 'grub-pc-bin' paketi gerekir"
fi
if [ -d /usr/lib/grub/x86_64-efi ]; then
    ok "x86_64-efi platformu var"
else
    bad "/usr/lib/grub/x86_64-efi YOK — 'grub-efi-amd64-bin' paketi gerekir"
fi

echo
echo "2) BIOS core.img uretimi"
# shellcheck disable=SC2086
if grub-mkimage -O i386-pc -o "$W/core.img" -p '(hd0,msdos1)/boot/grub' \
        $MODULES_PC 2>"$W/err-pc"; then
    SZ="$(stat -c%s "$W/core.img")"
    ok "uretildi ($SZ bayt, $(( (SZ + 511) / 512 )) sektor)"
    if [ "$SZ" -le "$MAX_CORE_BYTES" ]; then
        ok "MBR bosluguna sigiyor (sinir $MAX_CORE_BYTES bayt)"
    else
        bad "MBR bosluguna SIGMIYOR: $SZ > $MAX_CORE_BYTES"
    fi
    if [ "$SZ" -gt 0 ]; then
        ok "bos degil"
    else
        bad "0 bayt"
    fi
else
    bad "grub-mkimage BASARISIZ (modul adi hatali olabilir):"
    sed 's/^/         /' "$W/err-pc"
fi

echo
echo "3) UEFI BOOTX64.EFI uretimi"
# shellcheck disable=SC2086
if grub-mkimage -O x86_64-efi -o "$W/BOOTX64.EFI" -p /EFI/BOOT \
        $MODULES_EFI 2>"$W/err-efi"; then
    SZ="$(stat -c%s "$W/BOOTX64.EFI")"
    ok "uretildi ($SZ bayt)"
    # PE/COFF imzasi: gecerli bir EFI uygulamasi "MZ" ile baslar.
    MZ="$(dd if="$W/BOOTX64.EFI" bs=2 count=1 status=none | od -An -c | tr -d ' \n')"
    case "$MZ" in
        *M*Z*) ok "gecerli PE/COFF imzasi (MZ)" ;;
        *)     bad "PE/COFF imzasi yok — firmware bu dosyayi calistirmaz" ;;
    esac
else
    bad "grub-mkimage BASARISIZ (modul adi hatali olabilir):"
    sed 's/^/         /' "$W/err-efi"
fi

echo
echo "4) boot.img kernel_sector alani"
# GRUB'un boot.img'inde 0x5c ofsetinde 8 baytlik kernel_sector (core.img'in
# LBA'si) durur. Varsayilan 1 olmali, yoksa mcos-install'in "core.img'i sektor
# 1'e dd et" yaklasimi calismaz ve elle yama gerekir.
if [ -f /usr/lib/grub/i386-pc/boot.img ]; then
    KS="$(dd if=/usr/lib/grub/i386-pc/boot.img bs=1 skip=92 count=8 status=none \
          | od -An -tu1 | tr -s ' ' | sed 's/^ //')"
    FIRST="$(printf '%s' "$KS" | cut -d' ' -f1)"
    if [ "$FIRST" = "1" ]; then
        ok "kernel_sector = 1 (core.img sektor 1'e dd edilebilir)"
    else
        bad "kernel_sector = $FIRST (1 bekleniyordu) — boot.img elle yamalanmali"
    fi
fi

echo
if [ "$FAILED" -ne 0 ]; then
    echo "SONUC: BASARISIZ — bu haliyle 'mcos-install' ile kurulan disk BOOT ETMEZ."
    exit 1
fi
echo "SONUC: TUM KONTROLLER GECTI"
