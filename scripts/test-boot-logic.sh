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
echo "== mcosd TEK ORNEK =="

LAUNCH_SH="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-launch"
if [ -f "$LAUNCH_SH" ]; then
    # pgrep BU IMAJDA YOK: ona dayanan bir kontrol her zaman "calismiyor"
    # der ve ikinci bir daemon baslatir. QEMU'da SSH ile iki mcosd sureci
    # gorulerek dogrulandi (ppid=1 ve ppid=mcos-launch).
    if grep -vE '^[[:space:]]*#' "$LAUNCH_SH" | grep -q 'pgrep'; then
        fail "mcos-launch hala pgrep kullaniyor — imajda yok, ikinci daemon acilir"
    else
        pass "mcos-launch pgrep kullanmiyor"
    fi
    if grep -q 'mcosd_running' "$LAUNCH_SH"; then
        pass "daemon denetimi /proc uzerinden yapiliyor"
    else
        fail "daemon calisiyor mu denetimi yok"
    fi
fi

echo
echo "== VENTOY / COKLU DISK UYUMU =="

MK="$ROOT/Makefile"

# ── Yakalanan gercek hata ──────────────────────────────────────────────────
# Kullanici: "ventoyda grub2 modunda bootlamiyor".
# Sebep: grub.cfg kosulsuz "search --set=root" yapiyordu ve Ventoy un
# chainload sirasinda kurdugu $root u EZIYORDU. Ventoy USB sinde baska ISO
# lar varsa yanlis aygita dusuyor, ya da esledigi sanal aygit taranamadigi
# icin hic bulamiyordu.
if grep -q 'if ! \[ -e /boot/bzImage \]' "$MK"; then
    pass "grub.cfg once \$root a bakiyor (Ventoy uyumlu)"
else
    fail "kosulsuz search \$root u eziyor - Ventoy grub2 kipinde acilmaz"
fi

# initramfs TAMAMEN RAM e aciliyor: buyuk bir initramfs dusuk bellekli
# makinelerde ve Ventoy altinda acilisi riske atar.
#
# SINIR 200 -> 260 MB (2026-09-26, BILINCLI): kullanicinin istegiyle Java 21
# JRE imaja gomuldu (+~50 MB sikistirilmis, 143 MB acik) ve gercek PC'lerde
# eksik olan Wi-Fi/GPU firmware'leri eklendi (amdgpu, radeon, Intel AX2xx,
# MediaTek, Realtek). Olculen: 237 MB. Acilista gecici tepe ~ sikistirilmis
# + acik = ~0,9 GB; 2 GB alti makinede acilis riskli. Kullanilmayan firmware
# acilistan sonra RAM'den silinir (mcos-fwprune) ve sunucu yigini bos bellege
# sigdirilir (internal/server/memfit.go). Bu sinir kazara sismeyi yakalamak
# icin: yeni bir buyuk ekleme bilincli karar olmali.
CPIO="${BR_OUTPUT:-$ROOT/os/buildroot/output}/images/rootfs.cpio.gz"
if [ -f "$CPIO" ]; then
    MB=$(( $(stat -c%s "$CPIO") / 1048576 ))
    if [ "$MB" -gt 260 ]; then
        fail "initramfs $MB MB - RAM e sigmayabilir (260 MB ustu)"
    else
        pass "initramfs boyutu makul ($MB MB)"
    fi
fi

echo
echo "== ETIKET ARAMA (USB KALICILIK) =="

# ── Yakalanan gercek hata ──────────────────────────────────────────────────
# Kullanici "USB'ye kalicilik yapilmiyor, persist hatasi veriyor" dedi.
# Calisan sistemde olculdu: etiket arama yollarinin HEPSI kirikti —
#   findfs imajda YOK, blkid iso9660 icin BOS donuyor,
#   /dev/disk/by-label yok, tarama yalnizca BOLUMLERE bakiyordu.
# Oysa etiket oradaydi (/dev/sr0'in 32808. bayti "MCOS").
FINDFS="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-findfs"

if grep -q 'etiket_oku()' "$FINDFS"; then
    pass "etiket dogrudan superbloktan okunuyor (dis araca bagimli degil)"
else
    fail "etiket okuma yok - findfs/blkid imajda olmayinca kalicilik cokuyor"
fi

for imza in 32769 1080 82 54; do
    if grep -q "skip=$imza" "$FINDFS"; then
        pass "dosya sistemi imzasi $imza taniniyor"
    else
        fail "imza $imza okunmuyor - o dosya sistemi bulunamaz"
    fi
done

# Optik ve TUM DISK taramasi sart: ham yazilmis bir USB'de etiket bolumde
# degil, aygitin KENDISINDE durur.
if grep -q '/dev/sr\[0-9\]' "$FINDFS"; then
    pass "optik aygitlar taraniyor"
else
    fail "/dev/sr* taranmiyor - canli ISO'da onyukleme aygiti bulunamaz"
fi
if grep -qE '/dev/sd\[a-z\][^0-9]' "$FINDFS"; then
    pass "tum diskler de taraniyor (yalnizca bolumler degil)"
else
    fail "yalnizca bolumler taraniyor - ham yazilmis USB bulunamaz"
fi

echo
echo "== ACILIS: EKRANDA YALNIZCA LOGO =="

# ── Kullanicinin istegi ────────────────────────────────────────────────────
#   "acinca direkt logo gelmeli. ilk basta mcos yukleniyor daemon aciliyor
#    felan var ya onlar olmasin. sonra linux loglari geliyo o da olmasin.
#    acilir acilmaz mcos acilis ekrani gelsin. ekrandaki aciklamalarda
#    gercek olsun"

MK="$ROOT/Makefile"

if grep -q "mcos-logo.png" "$MK"; then
    pass "GRUB arka plani (logo) ISO'ya uretiliyor"
else
    fail "GRUB logosu yok - acilista bos/metinsiz ekran kalir"
fi

if grep -q "background_image" "$MK"; then
    pass "grub.cfg logoyu arka plan olarak kuruyor"
else
    fail "background_image yok - logo gorunmez"
fi

# Menu GIZLI olmali: 2 saniyelik menu, "acilir acilmaz logo" degildir.
# ── OLCULEREK bulundu, tahmin DEGIL ───────────────────────────────────────
# timeout=0 + hidden -> GRUB menu ekranini hic cizmez -> background_image de
# cizilmez -> ekran 11,5 saniye KARA kalir. Arka plan ancak geri sayim
# gosterilirken boyaniyor. Bu yuzden timeout=1 + countdown SART.
if grep -q "set timeout=1" "$MK" && grep -q "timeout_style=countdown" "$MK"; then
    pass "GRUB geri sayim kipinde (logo boyaniyor)"
else
    fail "timeout=0/hidden ise logo HIC cizilmez - ekran kara kalir (olculdu)"
fi

# GRUB'un kendi metni kalkmali.
if grep -q 'MCOS baslatiliyor' "$MK"; then
    fail "grub.cfg hala 'MCOS baslatiliyor...' yaziyor - logo yerine metin"
else
    pass "GRUB acilis metni kaldirildi"
fi

# Cekirdek loglari susturulmus olmali - AMA PANIK GORUNMELI.
#
# ── Yakalanan gercek test kusuru ────────────────────────────────────────────
#
# Burada `grep -q 'loglevel=0' display.sh` yaziyordu. display.sh'nin YORUM
# satirlarinda "loglevel=0" gectigi icin bu iddia DOSYADAKI GERCEK DEGER NE
# OLURSA OLSUN geciyordu: loglevel=7 yazilsaydi bile test yesildi.
#
# Artik degerin kendisi cikariliyor. Beklenen 1'dir, 0 degil: loglevel=0
# KERN_EMERG'i de susturur, yani cekirdek panigi EKRANA HIC DUSMEZ. Kullanici
# tam olarak bunu bildirdi - "panic satiri gozukmuyor, ekran siyah, caps lock
# yanip sonuyo".
CMDLINE_BASE="$(sed -n "s/^MCOS_CMDLINE_BASE='\(.*\)'$/\1/p" \
    "$ROOT/scripts/lib/display.sh" | head -1)"
if [ -z "$CMDLINE_BASE" ]; then
    fail "display.sh icinde MCOS_CMDLINE_BASE bulunamadi"
elif ! printf '%s' "$CMDLINE_BASE" | grep -q 'quiet'; then
    fail "cekirdek komut satirinda 'quiet' yok - Linux loglari gorunur: $CMDLINE_BASE"
elif ! printf '%s' "$CMDLINE_BASE" | grep -q 'loglevel=1'; then
    fail "loglevel=1 degil - panik ya gorunmez (0) ya da acilis gurultulu olur: $CMDLINE_BASE"
else
    pass "acilis sessiz ama panik gorunur (quiet loglevel=1)"
fi

# Turbo fanı: ThinkPad'de fan kademesi yalnızca thinkpad_acpi.fan_control=1
# ile yazılabilir (sürücü gömülü, modprobe seçeneği yok). Kullanıcı gerçek
# PC'de "turboda fan çalışmıyor" dedi. acpi_enforce_resources=lax BİLEREK
# yok (display.sh gerekçesi): biri eklerse test düşmeli.
if ! printf '%s' "$CMDLINE_BASE" | grep -qE '(^| )thinkpad_acpi\.fan_control=1( |$)'; then
    fail "komut satirinda thinkpad_acpi.fan_control=1 yok - ThinkPad fani turboda surulemez: $CMDLINE_BASE"
elif printf '%s' "$CMDLINE_BASE" | grep -q 'acpi_enforce_resources'; then
    fail "komut satirinda acpi_enforce_resources var - tum ACPI kaynak korumasi kalkar: $CMDLINE_BASE"
else
    pass "turbo fan komut satiri: thinkpad_acpi.fan_control=1 var, acpi_enforce_resources yok"
fi

# mcos-launch ekrana yazmamali.
if grep -vE '^[[:space:]]*#' "$LAUNCH_SH" | grep -q 'echo "MCOS hazirlaniyor'; then
    fail "mcos-launch hala ekrana 'MCOS hazirlaniyor' yaziyor"
else
    pass "mcos-launch acilis metni ekrana basmiyor"
fi
if grep -vE '^[[:space:]]*#' "$LAUNCH_SH" | grep -q 'echo ">> MCOS daemon baslatiliyor'; then
    fail "mcos-launch hala ekrana 'daemon baslatiliyor' yaziyor"
else
    pass "daemon mesaji ekrana degil gunluge gidiyor"
fi
if grep -q 'log_line()' "$LAUNCH_SH"; then
    pass "teshis bilgisi gunluge yaziliyor (atilmadi)"
else
    fail "log_line yok - teshis bilgisi tamamen kayboldu"
fi

# ── "Ekrandaki aciklamalar gercek olsun" ───────────────────────────────────
#
# GRUB karesi cekirdek yuklenmeden gorunuyor: o anda ilerleme YOK ve donanim
# OKUNMADI. --brand kipi tam olarak bunun icin var; ilerleme halkasi ve
# donanim satiri o karede CIZILMEZ.
if grep -q '"brand"' "$ROOT/cmd/mcos-splash/main.go"; then
    pass "marka karesi icin --brand kipi var"
else
    fail "--brand yok - GRUB karesinde uydurma ilerleme/donanim gosterilir"
fi
if grep -q 'func drawBrand' "$ROOT/cmd/mcos-splash/main.go"; then
    if awk '/func drawBrand/,/^}/' "$ROOT/cmd/mcos-splash/main.go" \
            | grep -qE 'ProgressRing|hardwareLine'; then
        fail "marka karesinde ilerleme/donanim ciziliyor - uydurma bilgi"
    else
        pass "marka karesinde uydurma ilerleme/donanim YOK"
    fi
fi

echo
echo "== KAPANIS SESSIZ =="

# ── Yakalanan gercek hata ──────────────────────────────────────────────────
# Kapatma animasyonu ekrani siyaha indirdikten hemen sonra mcos-launch'in
# kurtarma menusu o siyahin uzerine basiliyordu ve 15 saniye sonra paneli
# KAPANMAKTA OLAN sistemde yeniden aciyordu. QEMU'da kare kare olculdu.
#
# Sozlesme: panel 65 ile cikar (ErrPowerPending), mcos-launch bunu gorup
# power_wait'e girer; menu gosterilmez, panel yeniden acilmaz.
if [ -f "$LAUNCH_SH" ]; then
    if grep -q '"\$rc" -eq 65' "$LAUNCH_SH"; then
        pass "mcos-launch guc cikis kodunu (65) taniyor"
    else
        fail "mcos-launch 65'i tanimiyor — kurtarma menusu kapanis ekranina basar"
    fi
    if grep -q 'power_wait()' "$LAUNCH_SH"; then
        pass "kapanis beklemesi (power_wait) var"
    else
        fail "power_wait yok — panel kapanirken yeniden acilabilir"
    fi
    # Sonsuza kadar susulmamali: kapanma takilirsa kullanici kara ekranda
    # kalmamali.
    if grep -q 'POWER_GRACE' "$LAUNCH_SH"; then
        pass "takilan kapanis icin sure siniri var"
    else
        fail "kapanis sonsuza kadar bekliyor — takilirsa kurtarma yok"
    fi
fi

if grep -q 'exitPowerPending = 65' "$ROOT/cmd/mcos-panel-fb/main.go"; then
    pass "panel guc cikis kodunu 65 olarak veriyor"
else
    fail "panel 65 dondurmuyor — mcos-launch ile sozlesme kopuk"
fi

# Konsolun METIN ARABELLEGI, metin kipine donulmeden ONCE silinmeli. Aksi
# halde acilista yazilmis satirlar kapanis siyahinin ortasinda yanip soner
# (olculdu: iki kare, ~0,2 sn).
if awk '/func \(c \*console\) Power\(/,/^}/' "$ROOT/cmd/mcos-panel-fb/main.go" \
        | grep -n 'ClearText\|con.Restore' | head -2 \
        | awk 'NR==1 && /ClearText/ {ok=1} END {exit !ok}'; then
    pass "kapanista konsol metni Restore'dan ONCE siliniyor"
else
    fail "ClearText Restore'dan once cagrilmiyor — kapanista eski metin yanip soner"
fi

if [ -f "$ROOT/cmd/mcosd/singleton.go" ]; then
    pass "mcosd tek ornek kilidi tutuyor (veri koku basina)"
else
    fail "mcosd tek ornek kilidi yok — iki daemon ayni dunyaya yazabilir"
fi

echo
# ════════════════════════════════════════════════════════════════════════════
# KURULUM YEDEK KOMUT SATIRI
# ════════════════════════════════════════════════════════════════════════════
#
# mcos-install, display.sh'yi okuyamazsa gömülü yedek değerleri kullanır.
# Bu ikisi bir kez sessizce ayrıştı: display.sh loglevel=1'e geçti, yedek
# loglevel=4'te kaldı. Yedek yol yalnızca eski imajlarda çalıştığı için
# ayrışma kimsenin gözüne çarpmaz — bu yüzden sınanıyor.
echo ""
echo "== KURULUM YEDEK KOMUT SATIRI =="

INS="$INSTALL"
for ad in MCOS_GFXMODE MCOS_CMDLINE_BASE MCOS_CMDLINE_RECOVERY; do
    kaynak="$(sed -n "s/^${ad}='\(.*\)'$/\1/p" "$ROOT/scripts/lib/display.sh" | head -1)"
    yedek="$(sed -n "s/^    ${ad}='\(.*\)'$/\1/p" "$INS" | head -1)"
    if [ "$kaynak" = "$yedek" ]; then
        pass "$ad yedegi display.sh ile ayni"
    else
        fail "$ad ayrismis: display.sh='$kaynak' mcos-install='$yedek'"
    fi
done

# Kurulum betiği hedefi hem konumsal hem --device ile kabul etmeli: panel ve
# betik ayrı ayrı güncellenebiliyor ve bir kez ayrıştıklarında kurulum
# "hata kodu 1" verip HİÇ BAŞLAMIYORDU.
echo ""
echo "== KURULUM ARGUMANLARI =="
for arg in "/dev/mcos-yok" "--device /dev/mcos-yok --yes" "--device=/dev/mcos-yok"; do
    # shellcheck disable=SC2086
    cikti="$(sh "$INS" $arg 2>&1 || true)"
    if printf '%s' "$cikti" | grep -q '/dev/mcos-yok'; then
        pass "arguman bicimi kabul edildi: $arg"
    else
        fail "arguman bicimi reddedildi ($arg): $cikti"
    fi
done

echo ""
echo "== GRUB YAPILANDIRMASI URETIMI =="

# ── Yakalanan gercek hata: grub.cfg 0 BAYT uretildi ─────────────────────────
#
# Makefile'daki `printf '%s\n' ...` listesine ": yorum;" satirlari sokulmustu.
# Noktali virgul printf i BITIRIYOR; kalan tirnakli satirlar yeni bir komut
# olup `> grub.cfg` yonlendirmesiyle dosyayi SIFIRLIYORDU. Kabuk yalnizca
# stderr e "command not found" yazdi, make BASARILI dedi.
#
# Kullanicinin gordugu:
#   Ventoy grub2 kipi -> bos yapilandirma -> menuye geri donus
#   Ventoy normal kipi -> menu girdisi yok -> grub> terminali
#
# Iki ayri sinama: (1) kaynak bir daha bu bicimi almasin, (2) urun bossa yakala.

# Blogun sinirlari: "set timeout=1" satirindan grub.cfg yonlendirmesine kadar.
blok_son="$(grep -n 'boot/grub/grub.cfg"; \\$' "$ROOT/Makefile" | head -1 | cut -d: -f1)"
blok_bas="$(grep -n "'set timeout=1'" "$ROOT/Makefile" | head -1 | cut -d: -f1)"
if [ -z "$blok_bas" ] || [ -z "$blok_son" ] || [ "$blok_bas" -ge "$blok_son" ]; then
    fail "Makefile icinde grub.cfg ureten printf blogu bulunamadi"
else
    kirik="$(sed -n "${blok_bas},${blok_son}p" "$ROOT/Makefile" | grep -c "'; *\\\\$" || true)"
    if [ "$kirik" -gt 0 ]; then
        fail "printf argüman listesinde $kirik adet ';' var - grub.cfg BOS uretilir"
    else
        pass "grub.cfg printf listesi butun ($(( blok_son - blok_bas + 1 )) satir, noktali virgulsuz)"
    fi
fi

# Makefile urunu kendi dogrulamali: sessiz basarisizlik bir kez zaten gecti.
if grep -q 'uretilen grub.cfg eksik' "$ROOT/Makefile"; then
    pass "ISO hedefi uretilen grub.cfg yi dogruluyor"
else
    fail "ISO hedefi uretilen grub.cfg yi dogrulamiyor - bos dosya yine gecer"
fi

# Urun varsa dogrudan bak: en kesin kanit budur.
URETILEN="$ROOT/dist/iso/boot/grub/grub.cfg"
if [ ! -f "$URETILEN" ]; then
    skip "uretilmis grub.cfg yok (once 'make iso')"
else
    eksik=""
    for gerekli in 'menuentry "MCOS"' 'linux /boot/bzImage' 'initrd /boot/initrd.img'; do
        grep -qF "$gerekli" "$URETILEN" || eksik="$eksik [$gerekli]"
    done
    if [ -n "$eksik" ]; then
        fail "uretilen grub.cfg eksik:$eksik ($(wc -c < "$URETILEN") bayt)"
    else
        pass "uretilen grub.cfg tam ($(wc -c < "$URETILEN") bayt)"
    fi
fi

if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mtüm önyükleme testleri geçti\033[0m\n'
