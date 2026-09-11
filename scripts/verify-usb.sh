#!/bin/bash
# verify-usb.sh — mkusb.sh ile üretilen bir disk imajını BOOT ETMEDEN denetler.
#
# Amaç: "USB'den açılmıyor" hatasını donanıma gitmeden yakalamak. Kontroller:
#   1. Bölüm tablosu türü, bölüm tipleri, hizalama, bootable bayrağı
#   2. FAT bölümünde çekirdek + initramfs gerçekten var mı
#   3. Önyükleyici dosyaları yerinde mi (BOOTX64.EFI veya ldlinux.sys)
#   4. MBR bootstrap yazılmış mı (BIOS)
#   5. Veri bölümünün etiketi MCOS-DATA mı (kalıcılık buna bağlı)
#
# Kullanım: scripts/verify-usb.sh dist/mcos-uefi.img

set -uo pipefail

IMG="${1:-}"
[ -n "$IMG" ] || { echo "Kullanım: $0 <imaj>" >&2; exit 2; }
[ -f "$IMG" ] || { echo "imaj yok: $IMG" >&2; exit 2; }

FAIL=0
ok()   { printf '  \033[32mok  \033[0m %s\n' "$*"; }
bad()  { printf '  \033[31mHATA\033[0m %s\n' "$*"; FAIL=1; }
note() { printf '  ---- %s\n' "$*"; }

echo "== $IMG ($(du -h "$IMG" | cut -f1)) =="
echo

# ── 1. Bölüm tablosu ────────────────────────────────────────────────────────

echo "1) Bölüm tablosu"
DUMP="$(sfdisk --dump "$IMG" 2>/dev/null)" || { bad "bölüm tablosu okunamadı"; exit 1; }

LABEL="$(printf '%s\n' "$DUMP" | sed -n 's/^label: *//p')"
case "$LABEL" in
    gpt) ok "etiket türü: gpt (UEFI yolu)"; MODE=uefi ;;
    dos) ok "etiket türü: dos/MBR (BIOS yolu)"; MODE=bios ;;
    *)   bad "beklenmeyen etiket türü: '$LABEL'"; MODE=unknown ;;
esac

P1="$(printf '%s\n' "$DUMP" | grep -- '1 *:' | head -1)"
P2="$(printf '%s\n' "$DUMP" | grep -- '2 *:' | head -1)"
[ -n "$P1" ] && ok "1. bölüm var" || bad "1. bölüm YOK"
[ -n "$P2" ] && ok "2. bölüm var (kalıcı veri)" || bad "2. bölüm YOK — kalıcılık çalışmaz"

P1_START="$(printf '%s\n' "$P1" | sed -n 's/.*start= *\([0-9]*\).*/\1/p')"
if [ "${P1_START:-0}" = 2048 ]; then
    ok "1. bölüm 1 MiB'de başlıyor (hizalı)"
else
    bad "1. bölüm ${P1_START:-?} sektöründe başlıyor — 2048 (1 MiB) olmalı"
fi

if [ "$MODE" = uefi ]; then
    if printf '%s\n' "$P1" | grep -q 'C12A7328-F81F-11D2-BA4B-00A0C93EC93B'; then
        ok "1. bölüm tipi EFI System (firmware'in aradığı GUID)"
    else
        bad "1. bölüm EFI System değil — UEFI firmware onu ESP olarak görmez"
    fi
elif [ "$MODE" = bios ]; then
    if printf '%s\n' "$P1" | grep -qi 'type= *c\b'; then
        ok "1. bölüm tipi 0x0c (FAT32 LBA)"
    else
        bad "1. bölüm tipi 0x0c değil"
    fi
    if printf '%s\n' "$P1" | grep -q 'bootable'; then
        ok "1. bölüm ÖNYÜKLENEBİLİR işaretli"
    else
        bad "1. bölüm bootable değil — çoğu BIOS USB'den boot etmez"
    fi
fi

# ── 2. MBR bootstrap (yalnızca BIOS) ────────────────────────────────────────

if [ "$MODE" = bios ]; then
    echo
    echo "2) MBR bootstrap"
    FIRST="$(dd if="$IMG" bs=1 count=4 status=none 2>/dev/null | od -An -tx1 | tr -d ' \n')"
    if [ "$FIRST" = "00000000" ] || [ -z "$FIRST" ]; then
        bad "MBR'nin ilk 4 baytı boş — önyükleyici yazılmamış"
    else
        ok "MBR bootstrap var (ilk baytlar: $FIRST)"
    fi
    SIG="$(dd if="$IMG" bs=1 skip=510 count=2 status=none 2>/dev/null | od -An -tx1 | tr -d ' \n')"
    if [ "$SIG" = "55aa" ]; then
        ok "0x55AA boot imzası yerinde"
    else
        bad "0x55AA imzası yok (bulunan: $SIG) — BIOS diski boot edilebilir görmez"
    fi
fi

# ── 3. FAT bölümü içeriği ───────────────────────────────────────────────────

echo
echo "3) Boot bölümü içeriği"
OFF=$(( ${P1_START:-2048} * 512 ))
MT="$IMG@@$OFF"

FATLIST="$(mdir -i "$MT" -a -/ :: 2>/dev/null)"
if [ -z "$FATLIST" ]; then
    bad "FAT bölümü okunamadı (ofset $OFF) — biçimlendirme başarısız olabilir"
else
    VOL="$(printf '%s\n' "$FATLIST" | sed -n 's/.*Volume in drive [^ ]* is *//p' | head -1 | tr -d ' ')"
    if [ "$VOL" = "MCOS-BOOT" ]; then
        ok "FAT birim etiketi MCOS-BOOT"
    else
        note "FAT birim etiketi: '${VOL:-yok}' (beklenen MCOS-BOOT)"
    fi

    check_file() {
        if printf '%s\n' "$FATLIST" | grep -qi "$1"; then
            ok "$2"
        else
            bad "$2 — BULUNAMADI"
        fi
    }
    check_file 'bzImage\|BZIMAGE' "çekirdek (/bzImage)"
    check_file 'initrd'           "initramfs (/initrd.img)"

    if [ "$MODE" = uefi ]; then
        check_file 'BOOTX64'  "UEFI önyükleyici (/EFI/BOOT/BOOTX64.EFI)"
        check_file 'grub *cfg' "GRUB yapılandırması (/EFI/BOOT/grub.cfg)"
    else
        check_file 'ldlinux *sys' "syslinux çekirdeği (ldlinux.sys)"
        check_file 'ldlinux *c32' "syslinux modülü ldlinux.c32"
        check_file 'menu *c32'    "syslinux modülü menu.c32"
        check_file 'libcom32'     "syslinux modülü libcom32.c32"
        check_file 'syslinux *cfg' "syslinux yapılandırması"
    fi
fi

# ── 4. Veri bölümü ──────────────────────────────────────────────────────────

echo
echo "4) Kalıcı veri bölümü"
P2_START="$(printf '%s\n' "$P2" | sed -n 's/.*start= *\([0-9]*\).*/\1/p')"
if [ -n "${P2_START:-}" ]; then
    # Etiketi DOGRUDAN superbloktan oku. dumpe2fs bir dosya parcasindan
    # guvenilir okumuyordu (grup tanimlayicilarina da bakiyor), bu yuzden
    # yapiyi kendimiz cozuyoruz:
    #   ext4 superblok  : bolum basindan +1024 bayt
    #   s_volume_name   : superblok icinde +0x78, 16 bayt
    #   s_magic (0xEF53): superblok icinde +0x38, 2 bayt (little-endian)
    SB=$(( P2_START * 512 + 1024 ))

    MAGIC="$(dd if="$IMG" bs=1 skip=$(( SB + 0x38 )) count=2 status=none 2>/dev/null \
             | od -An -tx1 | tr -d ' \n')"
    if [ "$MAGIC" = "53ef" ]; then
        ok "ext4 superblok imzasi dogru (0xEF53)"
    else
        bad "ext4 superblok imzasi yok (bulunan: ${MAGIC:-yok}) — bolum bicimlendirilmemis"
    fi

    VOLNAME="$(dd if="$IMG" bs=1 skip=$(( SB + 0x78 )) count=16 status=none 2>/dev/null \
               | tr -d '\000')"
    if [ "$VOLNAME" = "MCOS-DATA" ]; then
        ok "ext4 etiketi MCOS-DATA (S99mcos bunu arıyor)"
    else
        bad "veri bolumu etiketi '${VOLNAME:-yok}' — MCOS-DATA olmali, yoksa Java/sunucular kalici OLMAZ"
    fi

fi

echo
if [ "$FAIL" -ne 0 ]; then
    echo "SONUÇ: BAŞARISIZ — yukarıdaki HATA satırlarına bakın."
    exit 1
fi
echo "SONUÇ: TÜM KONTROLLER GEÇTİ"
