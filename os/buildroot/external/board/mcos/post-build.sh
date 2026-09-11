#!/bin/sh
# Post-build script for MCOS Buildroot board.

set -e

BOARD_DIR="$(dirname "$0")"
OVERLAY="${BOARD_DIR}/rootfs-overlay"

normalize() {
    for base in "$OVERLAY" "$TARGET_DIR"; do
        f="$base/$1"
        [ -f "$f" ] && sed -i 's/\r$//' "$f" 2>/dev/null || true
    done
}
makeexec() {
    for base in "$OVERLAY" "$TARGET_DIR"; do
        f="$base/$1"
        [ -f "$f" ] && chmod 755 "$f" 2>/dev/null || true
    done
}

# Her overlay betiği CRLF'den arındırılmalı ve çalıştırılabilir yapılmalı.
# Overlay dosyaları git'te 0644 olarak durabildiği için (rsync -a modu aynen
# kopyalar) bu chmod olmadan "permission denied" alınır. mcos-install eskiden
# bu listede YOKTU: temiz bir derlemede kurulum betiği çalıştırılamıyordu.
for s in etc/init.d/S99mcos usr/bin/mcos-launch usr/bin/mcos-persist usr/bin/mcos-install usr/bin/mcos-findfs; do
    normalize "$s"
    makeexec "$s"
done
normalize etc/inittab

# Install keymaps and console fonts
KEYMAP_DST="${TARGET_DIR}/usr/share/keymaps/i386/qwerty"
FONT_DST="${TARGET_DIR}/usr/share/consolefonts"
if [ -n "${BUILD_DIR:-}" ]; then
    KEYMAP_SRC="$(ls "${BUILD_DIR}"/kbd-*/data/keymaps/i386/qwerty/trq.map.gz 2>/dev/null | head -1)"
    if [ -n "$KEYMAP_SRC" ] && [ -f "$KEYMAP_SRC" ]; then
        mkdir -p "$KEYMAP_DST"
        cp "$KEYMAP_SRC" "$KEYMAP_DST/"
    fi
    
    # NOT: eskiden burada ter-u16n (Terminus) kopyalanmaya calisiliyordu.
    # O font BR2_PACKAGE_TERMINUS_FONT paketine ait ve etkin degil; kbd
    # pakedinde hic yok. Kosul her zaman false donuyor, sonra S99mcos ve
    # mcos-launch "setfont ter-u16n" cagirip her acilista hata basiyordu.
    # kbd pakedi Lat2-Terminus16 ve iso09.16'yi ZATEN kuruyor, bu yuzden
    # burada ekstra kopyalama gerekmiyor. Varligini dogrula ve bildir.
    if [ -f "$FONT_DST/Lat2-Terminus16.psfu.gz" ]; then
        echo "post-build: konsol fontu Lat2-Terminus16 mevcut"
    else
        echo "post-build: UYARI - Lat2-Terminus16 bulunamadi; konsol varsayilan fontu kullanacak"
    fi
fi

# Create required directories
mkdir -p "${TARGET_DIR}/data"
mkdir -p "${TARGET_DIR}/boot"
mkdir -p "${TARGET_DIR}/run/mcos"
mkdir -p "${TARGET_DIR}/usr/lib"
mkdir -p "${TARGET_DIR}/lib"
mkdir -p "${TARGET_DIR}/root"
mkdir -p "${TARGET_DIR}/etc/skel"
mkdir -p "${TARGET_DIR}/usr/share/fonts"
mkdir -p "${TARGET_DIR}/etc/fonts"

# Flatten font directory so fontconfig finds DejaVu fonts at top-level /usr/share/fonts
if [ -d "${TARGET_DIR}/usr/share/fonts/dejavu" ]; then
    cp -a "${TARGET_DIR}/usr/share/fonts/dejavu/"*.ttf "${TARGET_DIR}/usr/share/fonts/" 2>/dev/null || true
fi

# Stage C++ runtime libraries (libstdc++.so.6 and libgcc_s.so.1) required by fbterm
if [ -n "${HOST_DIR:-}" ]; then
    TC_LIB="$(find "${HOST_DIR}" -name 'libstdc++.so.6*' -type f 2>/dev/null | head -1)"
    if [ -n "$TC_LIB" ] && [ -f "$TC_LIB" ]; then
        LIB_DIR="$(dirname "$TC_LIB")"
        cp -a "$LIB_DIR"/libstdc++.so* "${TARGET_DIR}/usr/lib/" 2>/dev/null || true
        cp -a "$LIB_DIR"/libgcc_s.so* "${TARGET_DIR}/usr/lib/" 2>/dev/null || true
        cp -a "$LIB_DIR"/libstdc++.so* "${TARGET_DIR}/lib/" 2>/dev/null || true
        cp -a "$LIB_DIR"/libgcc_s.so* "${TARGET_DIR}/lib/" 2>/dev/null || true
    fi
fi

# Purge any host fontconfig cache files so target regenerates clean target paths on first boot
rm -rf "${TARGET_DIR}/var/cache/fontconfig" "${TARGET_DIR}/var/lib/fontconfig"
mkdir -p "${TARGET_DIR}/var/cache/fontconfig"

# FONT SIRASI — KRİTİK.
#
# Panel fbterm altında çalışır ve fbterm, font listesinin İLK öğesini birincil
# font olarak kullanır; geri kalanlar yalnızca eksik glif için yedektir.
#
# Eskiden birincil font "Noto Color Emoji" idi. Ölçüldü (scripts/check-fonts.sh):
#
#   BIRINCIL Noto Color Emoji  -> KURULU DEGIL
#   yedek    Noto Emoji        -> KURULU  (ama spacing="" = monospace DEGIL,
#                                 Latin harf ve box-drawing glifi YOK)
#   yedek    FontAwesome       -> KURULU DEGIL
#   yedek    DejaVu Sans Mono  -> KURULU
#
# Yani birincil font, harf ve çerçeve glifi olmayan bir emoji fontuna düşüyordu.
# "Unicode karakterler ve yuvarlak kenarlar görünmüyor" şikâyetinin sebebi tam
# olarak buydu.
#
# Gönderilen tek uygun font — FiraCode Nerd Font (spacing=100, monospace;
# box-drawing + yuvarlak köşe + Türkçe glif + ikon içerir) — listede hiç
# geçmiyordu. Artık birincil o.
#
# Emoji EN SONA alındı: emoji glifleri çift genişliktedir ve öne alındığında
# TUI hizalamasını bozar.
cat > "${TARGET_DIR}/etc/fonts/local.conf" << 'EOF'
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <alias>
    <family>monospace</family>
    <prefer>
      <family>FiraCode Nerd Font</family>
      <family>DejaVu Sans Mono</family>
      <family>Liberation Mono</family>
      <family>Noto Emoji</family>
    </prefer>
  </alias>
  <match target="font">
    <edit name="rgba" mode="assign"><const>none</const></edit>
    <edit name="hinting" mode="assign"><bool>true</bool></edit>
    <edit name="autohint" mode="assign"><bool>false</bool></edit>
    <edit name="hintstyle" mode="assign"><const>hintslight</const></edit>
    <edit name="antialias" mode="assign"><bool>true</bool></edit>
  </match>
</fontconfig>
EOF

# fbterm yapılandırması.
#
# Font sırası yukarıdaki local.conf ile AYNI olmalı: birincil font monospace
# bir METİN fontu olmalı, emoji en sonda.
#
# RENKLER BURADA AYARLANMAZ. fbterm 1.7.0 yalnızca "color-foreground" ve
# "color-background" anahtarlarını okur (src/fbconfig.cpp); "color-0=..."
# biçimindeki 16 satır yıllardır HİÇ OKUNMADI, yani kullanıcı fbterm'in gömülü
# VGA paletini görüyordu — örneğin kenarlık yuvası #aa00aa magenta.
#
# Palet artık panel/theme/palette.go içinde tanımlıdır ve panel açılışında
# fbterm'e escape dizisiyle yüklenir (ESC[3;i;r;g;b}). Tek doğruluk kaynağı
# orasıdır; buraya renk yazmayın.
cat > "${TARGET_DIR}/root/.fbtermrc" << 'EOF'
font-names=FiraCode Nerd Font,DejaVu Sans Mono,Liberation Mono,Noto Emoji
font-size=14
font-height=0
font-width=0
font-space=0
font-space-adjust=0
color-foreground=7
color-background=0
ambiguous-wide=0
screen-rotate=0
vesa-mode=0
cursor-shape=0
cursor-interval=500
history-lines=2000
input-method=
EOF
cp "${TARGET_DIR}/root/.fbtermrc" "${TARGET_DIR}/etc/skel/.fbtermrc" 2>/dev/null || true

# fbterm terminfo girdisini HEDEFE kur.
#
# fbterm'in kendi terminfo/Makefile.am'ı sadece "tic fbterm" çalıştırır — çıktı
# dizini (-o) VERMEZ. Çapraz derlemede bu, terminfo'yu derleme HOST'una yazar;
# hedef rootfs'te fbterm girdisi hiç oluşmaz. Oysa mcos-launch TERM=fbterm
# ihraç ediyor. Burada host'un tic'ini açık çıktı diziniyle çağırıyoruz.
if [ -n "${BUILD_DIR:-}" ] && command -v tic >/dev/null 2>&1; then
    TI_SRC="$(ls "${BUILD_DIR}"/fbterm-*/terminfo/fbterm 2>/dev/null | head -1)"
    if [ -n "$TI_SRC" ] && [ -f "$TI_SRC" ]; then
        mkdir -p "${TARGET_DIR}/usr/share/terminfo"
        if tic -x -o "${TARGET_DIR}/usr/share/terminfo" "$TI_SRC" 2>/dev/null; then
            echo "post-build: fbterm terminfo hedefe kuruldu"
        else
            echo "post-build: UYARI - fbterm terminfo derlenemedi (tic hatasi)"
        fi
    fi
fi


# ── Onyukleyici dosyalarini ROOTFS'E GOM ────────────────────────────────────
#
# NEDEN: mcos-install (OOBE'den USB'ye kalici kurulum) onyukleyiciyi hedef
# diske yazmak zorunda. Ama hedef rootfs'te grub-install / grub-mkimage /
# /usr/lib/grub/i386-pc HIC YOK (olculdu: scripts/probe-target-boot.sh).
# Eski kod "command -v grub-install" kontrolune takilip SESSIZCE hicbir
# onyukleyici yazmiyordu: kurulum "basarili" gorunup disk boot etmiyordu,
# BIOS bootable aygit gormuyordu.
#
# COZUM: onyukleyici parcalarini BURADA (build zamaninda, host araclariyla)
# uretip rootfs'e gomuyoruz. mcos-install yalnizca dd ve cp yapar; hedefte
# hicbir onyukleyici aracina ihtiyac duymaz.
#
# Bu yontem QEMU'da BIOS boot ederek dogrulandi (scripts/exp-grub-dd.sh).
MCOS_BOOTLIB="${TARGET_DIR}/usr/lib/mcos/boot"
mkdir -p "$MCOS_BOOTLIB"

# Cozunurluk tanimini hedef rootfs'e kopyala: mcos-install ve ekran ayarlari
# ekrani onu okur. Boylece gfxmode listesi TEK yerde tanimli kalir
# (scripts/lib/display.sh) ve ISO/USB/kurulu disk birbirinden sapmaz.
MCOS_DISPLAY_SRC="${BR2_EXTERNAL_MCOS_PATH:-$(dirname "$0")/../../..}/scripts/lib/display.sh"
if [ ! -f "$MCOS_DISPLAY_SRC" ]; then
    # BR2_EXTERNAL yolu farkli kurulmus olabilir; depo kokunu yukari dogru ara.
    d="$(cd "$(dirname "$0")" && pwd)"
    while [ "$d" != "/" ]; do
        [ -f "$d/scripts/lib/display.sh" ] && { MCOS_DISPLAY_SRC="$d/scripts/lib/display.sh"; break; }
        d="$(dirname "$d")"
    done
fi
if [ -f "$MCOS_DISPLAY_SRC" ]; then
    install -D -m 0644 "$MCOS_DISPLAY_SRC" "${TARGET_DIR}/usr/lib/mcos/display.sh"
    echo "post-build: ekran tanimi kopyalandi (/usr/lib/mcos/display.sh)"
else
    echo "post-build: UYARI - scripts/lib/display.sh bulunamadi;"
    echo "post-build:          mcos-install gomulu yedek cozunurluk listesini kullanacak"
fi

# BIOS ve UEFI BAGIMSIZ uretilir.
#
# Eskiden UEFI imaji BIOS blogunun ICINDE uretiliyordu: host'ta yalnizca
# grub-efi-amd64-bin kuruluysa (i386-pc dizini yoksa) HICBIRI uretilmiyordu
# ve UEFI makinede kurulan disk boot etmiyordu. Artik ikisi ayri.
#
# Ayrica hata ciktisi ARTIK YUTULMUYOR (eski kodda 2>/dev/null vardi):
# grub-mkimage neden basarisiz oldugunu soylemeden kaybolmasin.
MCOS_GRUB_LOG="${BUILD_DIR:-/tmp}/mcos-grub-mkimage.log"
: >"$MCOS_GRUB_LOG" 2>/dev/null || MCOS_GRUB_LOG=/dev/stderr

grub_dir_for() {
    # $1 = hedef (i386-pc | x86_64-efi), stdout = bulunan dizin ya da bos.
    for d in "/usr/lib/grub/$1" "${HOST_DIR:-}/lib/grub/$1"; do
        [ -f "$d/normal.mod" ] && { echo "$d"; return; }
    done
}

GRUB_PC_DIR="$(grub_dir_for i386-pc)"
GRUB_EFI_DIR="$(grub_dir_for x86_64-efi)"

if ! command -v grub-mkimage >/dev/null 2>&1; then
    echo "post-build: UYARI - host'ta grub-mkimage YOK."
    echo "post-build:          'sudo apt-get install grub-pc-bin grub-efi-amd64-bin' gerekir,"
    echo "post-build:          aksi halde mcos-install ile kurulan disk BOOT ETMEZ."
    GRUB_PC_DIR=""
    GRUB_EFI_DIR=""
fi

# ── BIOS ────────────────────────────────────────────────────────────────────
if [ -n "$GRUB_PC_DIR" ] && [ -f "$GRUB_PC_DIR/boot.img" ]; then
    # MBR bootstrap (ilk 440 bayti kullanilir). 0x5c ofsetindeki
    # kernel_sector alani zaten LBA 1'i gosteriyor, elle yama gerekmez.
    cp "$GRUB_PC_DIR/boot.img" "$MCOS_BOOTLIB/boot.img"

    # prefix ARTIK (hd0,msdos1) DEGIL.
    #
    # Sabit (hd0,msdos1) yalnizca MCOS diski BIOS'ta ILK disk olarak
    # numaralandigi zaman calisir. Kullanicinin dahili diski varsa USB
    # cogu zaman hd1'dir ve GRUB yanlis diskte grub.cfg arayip
    # "error: file not found" ile rescue kabuguna duser.
    #
    # Cozum: prefix'i bos birakip gomulu on-yapilandirma ile bolumu
    # ETIKETTEN buldurmak. search_label modulu zaten gomulu.
    cat >"${BUILD_DIR:-/tmp}/mcos-grub-early.cfg" <<'EARLYCFG'
search --no-floppy --label --set=root MCOS-BOOT
set prefix=($root)/boot/grub
EARLYCFG

    if grub-mkimage -O i386-pc -o "$MCOS_BOOTLIB/core.img" \
        -c "${BUILD_DIR:-/tmp}/mcos-grub-early.cfg" \
        -p '/boot/grub' \
        biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot \
        search search_fs_file search_label search_fs_uuid echo test \
        all_video vbe vga gfxterm minicmd sleep reboot halt \
        >>"$MCOS_GRUB_LOG" 2>&1
    then
        echo "post-build: BIOS onyukleyici gomuldu ($(stat -c%s "$MCOS_BOOTLIB/core.img") bayt core.img)"
    else
        echo "post-build: UYARI - BIOS core.img uretilemedi; USB'ye kurulum BIOS'ta boot etmez"
        echo "post-build:          ayrinti: $MCOS_GRUB_LOG"
        rm -f "$MCOS_BOOTLIB/core.img" "$MCOS_BOOTLIB/boot.img"
    fi
else
    echo "post-build: UYARI - /usr/lib/grub/i386-pc yok (grub-pc-bin); BIOS boot DESTEKLENMEYECEK"
fi

# ── UEFI ────────────────────────────────────────────────────────────────────
if [ -n "$GRUB_EFI_DIR" ]; then
    # prefix=/EFI/BOOT: mcos-install grub.cfg'yi tam oraya yazar.
    # Bu ikisi BIRLIKTE degismelidir — scripts/test-boot-logic.sh dogrular.
    if grub-mkimage -O x86_64-efi -o "$MCOS_BOOTLIB/BOOTX64.EFI" \
        -p /EFI/BOOT \
        part_gpt part_msdos fat ext2 normal configfile linux boot \
        search search_fs_file search_label search_fs_uuid echo test \
        all_video gfxterm efi_gop efi_uga minicmd sleep reboot halt \
        >>"$MCOS_GRUB_LOG" 2>&1
    then
        echo "post-build: UEFI onyukleyici gomuldu ($(stat -c%s "$MCOS_BOOTLIB/BOOTX64.EFI") bayt)"
    else
        # EFISTUB'a DUSULMEZ. Cekirdegi dogrudan BOOTX64.EFI yapmak, UEFI'nin
        # komut satiri VERMEMESI yuzunden "VFS: Cannot open root device" ile
        # kernel panic verir (sahada goruldu). Onyukleyici yoksa UEFI kurulumu
        # denenmez; mcos-install HAVE_UEFI=0 gorup BIOS'a duser.
        echo "post-build: UYARI - BOOTX64.EFI uretilemedi; UEFI makinelerde kurulan disk BOOT ETMEZ"
        echo "post-build:          ayrinti: $MCOS_GRUB_LOG"
        rm -f "$MCOS_BOOTLIB/BOOTX64.EFI"
    fi
else
    echo "post-build: UYARI - /usr/lib/grub/x86_64-efi yok (grub-efi-amd64-bin); UEFI boot DESTEKLENMEYECEK"
fi

# Clean up any leftover boot binaries in target rootfs so initrd doesn't recursively pack itself!
rm -f "${TARGET_DIR}/boot/initrd.img" "${TARGET_DIR}/boot/rootfs.cpio.gz" 2>/dev/null || true

# Copy bzImage into target boot directory for installed systems to use
if [ -n "${BINARIES_DIR:-}" ]; then
    [ -f "${BINARIES_DIR}/bzImage" ] && cp "${BINARIES_DIR}/bzImage" "${TARGET_DIR}/boot/bzImage" || true
fi
