#!/bin/sh
# test-boot-logic.sh — önyükleme zincirinin tutarlılık testi.
#
# NEDEN VAR: Gerçek donanımda (Gigabyte Z390 D, çekirdek 6.6.32) UEFI'de
# kurulan disk şu panikle açıldı:
#
#     VFS: Cannot open root device "" or unknown-block(0,0)
#     Kernel panic - not syncing: VFS: Unable to mount root fs
#
# Sebebi: eski mcos-install, GRUB bulunamazsa "EFISTUB geri dönüşü" yapıp
# HAM ÇEKİRDEĞİ /EFI/BOOT/BOOTX64.EFI olarak kopyalıyordu. UEFI firmware'i
# bir EFI uygulamasını KOMUT SATIRI VERMEDEN çalıştırır; yanına yazılan
# startup.nsh dosyasını yalnızca UEFI Shell okur, normal firmware okumaz.
# Böylece çekirdek boş cmdline ile açılıyor, root= göremiyor ve panikliyordu.
#
# Bu test o yolun geri gelmesini ve prefix/etiket uyumsuzluklarını yakalar.
# Hiçbir diske YAZMAZ; yalnızca kaynak dosyaları okur ve grub-mkimage'ı
# geçici dizinde çalıştırır.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
INSTALL="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install"
POSTBUILD="$ROOT/os/buildroot/external/board/mcos/post-build.sh"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }
skip() { printf '  \033[33mATLA\033[0m %s\n' "$1"; }

[ -f "$INSTALL" ] || { echo "mcos-install bulunamadı: $INSTALL" >&2; exit 2; }
[ -f "$POSTBUILD" ] || { echo "post-build.sh bulunamadı: $POSTBUILD" >&2; exit 2; }

# Yorum satırlarını atarak yalnızca ÇALIŞAN kodu incele: bu dosyada panikten
# bahseden açıklama satırları var ve onlar eşleşmemeli.
code() { sed 's/[[:space:]]*#.*$//' "$1"; }

echo "== 1. EFISTUB geri dönüşü yok =="

if code "$INSTALL" | grep -qE 'cp[^|]*(bzImage|\$KERNEL)[^|]*BOOTX64\.EFI'; then
    fail "mcos-install ham çekirdeği BOOTX64.EFI olarak kopyalıyor — UEFI'de kernel panic verir"
else
    pass "çekirdek BOOTX64.EFI olarak kopyalanmıyor"
fi

if code "$INSTALL" | grep -q 'startup\.nsh'; then
    fail "mcos-install startup.nsh yazıyor — normal UEFI firmware'i onu OKUMAZ, yanıltıcı"
else
    pass "startup.nsh yazılmıyor"
fi

if code "$POSTBUILD" | grep -qiE 'EFISTUB'; then
    fail "post-build.sh hâlâ EFISTUB'dan bahsediyor — o yol kaldırıldı"
else
    pass "post-build.sh EFISTUB'a atıfta bulunmuyor"
fi

echo "== 2. BOOTX64.EFI yalnızca gömülü GRUB'dan gelir =="

if code "$INSTALL" | grep -q 'cp "\$BOOTLIB/BOOTX64.EFI"'; then
    pass "UEFI önyükleyici \$BOOTLIB'den kopyalanıyor"
else
    fail "mcos-install \$BOOTLIB/BOOTX64.EFI kopyalamıyor — UEFI kurulumu yapılamaz"
fi

if code "$INSTALL" | grep -q 'HAVE_UEFI=0.*&&.*HAVE_BIOS=0' ||
   code "$INSTALL" | grep -q '\[ "\$HAVE_BIOS" = 0 \] && \[ "\$HAVE_UEFI" = 0 \]'; then
    pass "ikisi de yoksa kurulum en başta durduruluyor"
else
    fail "önyükleyici yokken kurulum sessizce devam ediyor olabilir"
fi

echo "== 3. GRUB prefix'i grub.cfg'nin yazıldığı yerle aynı =="

# post-build.sh'ten UEFI prefix'ini oku.
efi_prefix="$(code "$POSTBUILD" |
    grep -A4 'grub-mkimage -O x86_64-efi' |
    grep -oE '^[[:space:]]*-p[[:space:]]+\S+' |
    awk '{print $2}' | tr -d "'\"" | head -1)"

if [ -z "$efi_prefix" ]; then
    fail "post-build.sh içinde UEFI -p prefix'i bulunamadı"
else
    # mcos-install ESP'ye grub.cfg'yi nereye yazıyor?
    # "$TARGET_MNT/boot" FAT bölümünün kök dizinidir.
    esp_cfg="$(code "$INSTALL" |
        grep -oE '\$TARGET_MNT/boot/EFI/BOOT/grub\.cfg' | head -1)"
    if [ -z "$esp_cfg" ]; then
        fail "mcos-install ESP'ye grub.cfg yazmıyor — GRUB rescue kabuğuna düşer"
    elif [ "$efi_prefix" = "/EFI/BOOT" ]; then
        pass "UEFI prefix=$efi_prefix ile grub.cfg konumu uyuşuyor"
    else
        fail "UEFI prefix=$efi_prefix ama grub.cfg /EFI/BOOT altına yazılıyor"
    fi
fi

# BIOS tarafı: prefix bir disk numarasına SABİTLENMEMELİ.
bios_prefix="$(code "$POSTBUILD" |
    grep -A6 'grub-mkimage -O i386-pc' |
    grep -oE "^[[:space:]]*-p[[:space:]]+\S+" |
    awk '{print $2}' | tr -d "'\"" | head -1)"

case "$bios_prefix" in
    *'(hd'*)
        fail "BIOS prefix disk numarasına sabitlenmiş ($bios_prefix) — MCOS diski hd0 değilse GRUB rescue'ya düşer"
        ;;
    /boot/grub)
        pass "BIOS prefix=$bios_prefix (diskten bağımsız)"
        ;;
    "")
        fail "post-build.sh içinde BIOS -p prefix'i bulunamadı"
        ;;
    *)
        fail "beklenmeyen BIOS prefix: $bios_prefix"
        ;;
esac

echo "== 4. Bölüm etiketi ile GRUB search etiketi aynı =="

mkfs_label="$(code "$INSTALL" | grep -oE 'mkfs\.vfat[^|]*-n[[:space:]]+[A-Za-z0-9_-]+' |
    grep -oE '\-n[[:space:]]+[A-Za-z0-9_-]+' | awk '{print $2}' | head -1)"
search_label="$(code "$POSTBUILD" | grep -oE 'search --no-floppy --label --set=root[[:space:]]+\S+' |
    awk '{print $NF}' | head -1)"

if [ -z "$mkfs_label" ]; then
    fail "mcos-install boot bölümüne FAT etiketi vermiyor"
elif [ -z "$search_label" ]; then
    skip "post-build.sh GRUB search etiketi kullanmıyor (prefix doğrudan çözülüyor)"
elif [ "$mkfs_label" = "$search_label" ]; then
    pass "etiket uyuşuyor: $mkfs_label"
else
    fail "etiket uyuşmuyor: mkfs.vfat -n $mkfs_label ama GRUB '$search_label' arıyor"
fi

# FAT etiketi en fazla 11 karakter olabilir; uzunu mkfs sessizce kırpar ve
# GRUB search'ü bir daha asla eşleşmez.
if [ -n "$mkfs_label" ] && [ "${#mkfs_label}" -gt 11 ]; then
    fail "FAT etiketi 11 karakterden uzun (${#mkfs_label}): $mkfs_label — mkfs onu kırpar"
elif [ -n "$mkfs_label" ]; then
    pass "FAT etiketi uzunluğu uygun (${#mkfs_label} ≤ 11)"
fi

echo "== 5. Çekirdek komut satırında root= yok =="

# Sistem initramfs'ten çalışır. root= verilirse çekirdek olmayan bir aygıtı
# bağlamaya çalışır ve yine VFS paniği alırız.
#
# DİKKAT — "mcos.root=" BAŞKA BİR ŞEYDİR ve serbesttir: o bizim kendi
# parametremizdir. Çekirdek onu yok sayar; initramfs'teki /init okuyup diske
# switch_root yapar (kalıcılık). Bu yüzden yalnızca SÖZCÜK BAŞINDAKİ root=
# aranır, aksi halde "mcos.root=" de yanlışlıkla eşleşirdi.
cmdline="$(code "$INSTALL" | grep -E '^CMDLINE=' | head -1)"
if [ -z "$cmdline" ]; then
    fail "CMDLINE tanımı bulunamadı"
elif echo "$cmdline" | grep -qE '(^|[[:space:]"])root='; then
    fail "CMDLINE içinde ÇEKİRDEK root= var — initramfs sisteminde VFS paniği: $cmdline"
else
    pass "CMDLINE çekirdek root= içermiyor"
fi

echo "== 6. grub-mkimage gerçekten bu argümanlarla çalışıyor mu =="

if ! command -v grub-mkimage >/dev/null 2>&1; then
    skip "grub-mkimage yok — dinamik doğrulama atlandı"
else
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT

    if [ -d /usr/lib/grub/x86_64-efi ]; then
        cat >"$tmp/early.cfg" <<'EOF'
search --no-floppy --label --set=root MCOS-BOOT
set prefix=($root)/boot/grub
EOF
        if grub-mkimage -O x86_64-efi -o "$tmp/BOOTX64.EFI" -p /EFI/BOOT \
            part_gpt part_msdos fat ext2 normal configfile linux boot \
            search search_fs_file search_label search_fs_uuid echo test \
            all_video gfxterm efi_gop efi_uga minicmd sleep reboot halt \
            2>"$tmp/efi.err"
        then
            sz=$(stat -c%s "$tmp/BOOTX64.EFI")
            if [ "$sz" -lt 20000 ]; then
                fail "BOOTX64.EFI şüpheli derecede küçük ($sz bayt)"
            elif grep -aq '/EFI/BOOT' "$tmp/BOOTX64.EFI"; then
                pass "BOOTX64.EFI üretildi ($sz bayt) ve prefix gömülü"
            else
                fail "BOOTX64.EFI içinde /EFI/BOOT prefix'i yok"
            fi
        else
            fail "grub-mkimage -O x86_64-efi başarısız: $(head -2 "$tmp/efi.err" | tr '\n' ' ')"
        fi
    else
        skip "/usr/lib/grub/x86_64-efi yok — UEFI imajı denenmedi"
    fi

    if [ -d /usr/lib/grub/i386-pc ]; then
        if grub-mkimage -O i386-pc -o "$tmp/core.img" \
            -c "$tmp/early.cfg" -p '/boot/grub' \
            biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot \
            search search_fs_file search_label search_fs_uuid echo test \
            all_video vbe vga gfxterm minicmd sleep reboot halt \
            2>"$tmp/pc.err"
        then
            sz=$(stat -c%s "$tmp/core.img")
            sectors=$(( (sz + 511) / 512 ))
            if [ "$sectors" -ge 2047 ]; then
                fail "core.img MBR boşluğuna sığmıyor ($sectors sektör ≥ 2047)"
            else
                pass "core.img üretildi ($sz bayt = $sectors sektör, sınır 2047)"
            fi
            # Gömülü yapılandırma i386-pc'de lzma ile SIKIŞTIRILIR; düz metin
            # aranamaz. Bunun yerine -c'siz bir imaj üretip boyut farkına
            # bakıyoruz: config gömülmüşse imaj büyür.
            grub-mkimage -O i386-pc -o "$tmp/core-nocfg.img" -p '/boot/grub' \
                biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot \
                search search_fs_file search_label search_fs_uuid echo test \
                all_video vbe vga gfxterm minicmd sleep reboot halt 2>/dev/null
            if [ -f "$tmp/core-nocfg.img" ] &&
               [ "$sz" -gt "$(stat -c%s "$tmp/core-nocfg.img")" ]; then
                pass "core.img gömülü ön-yapılandırmayı içeriyor (+$(( sz - $(stat -c%s "$tmp/core-nocfg.img") )) bayt)"
            else
                fail "core.img -c ile üretilmemiş gibi — etiket araması gömülü değil"
            fi
        else
            fail "grub-mkimage -O i386-pc başarısız: $(head -2 "$tmp/pc.err" | tr '\n' ' ')"
        fi
    else
        skip "/usr/lib/grub/i386-pc yok — BIOS imajı denenmedi"
    fi
fi

echo
if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mtüm önyükleme testleri geçti\033[0m\n'
