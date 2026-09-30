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
for s in etc/init.d/S03mcosdata etc/init.d/S04splash etc/init.d/S99mcos \
         usr/bin/mcos-launch usr/bin/mcos-persist usr/bin/mcos-install \
         usr/bin/mcos-findfs usr/bin/mcos-display usr/bin/mcos-update; do
    normalize "$s"
    makeexec "$s"
done
normalize etc/inittab
normalize init
makeexec init

# ── openssh'in kendi acilis betigi KALDIRILIYOR ────────────────────────────
#
# Buildroot'un S50sshd'si her acilista kosulsuz "ssh-keygen -A" calistirir ve
# sshd'yi ayaga kaldirir. Uc ayri sorun cikariyordu:
#
#   1. HIZ. Olculdu (QEMU, donanim hizlandirmasiz): rcS'in toplam 7 saniyesinin
#      3.77 saniyesi yalnizca bu betikti — RSA/ECDSA/ED25519 anahtar uretimi.
#      Canli sistemde /etc RAM'de oldugu icin anahtarlar kalici da degil: ayni
#      bedel HER acilista yeniden odeniyordu.
#
#   2. DOGRULUK. SSH artik daemon'a ait (internal/sshd): kendi sshd_config'ini
#      yaziyor, host anahtarlarini kalici veri dizininde tutuyor, portu ve
#      authorized_keys'i kullanicinin ayarindan aliyor. Iki sshd ayni portu
#      dinleyemez; ikincisi sessizce basarisiz olurdu.
#
#   3. BEKLENTI. Panel "SSH: kapali" derken sistemde stok yapilandirmali bir
#      sshd calisiyordu. Kullanicinin gordugu durum ile gercek ayni olmali.
#
# SSH'i acan tek yer artik ayarlar ekranidir.
rm -f "${TARGET_DIR}/etc/init.d/S50sshd"

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

# ── Yakalanan gercek hata: gomulu onyukleyici HIC URETILMIYORDU ──────────────
#
# Olculdu (os/buildroot/output/build/mcos-grub-mkimage.log):
#   grub-mkimage: error: cannot open `.../output/host/lib/grub/i386-pc/moddep.lst'
# PATH'te ilk bulunan, Buildroot'un KENDI grub-mkimage'i (GRUB 2.12). -d
# verilmedigi icin modulleri derlendigi varsayilan dizinde ariyordu; o dizin
# yok. Bu betik yalnizca UYARI basip geciyordu, /usr/lib/mcos/boot BOS kaldi
# ve mcos-install ilk denetiminde "exit 1" verdi — kullanicinin "baska USB'ye
# kurarken hata kodu 1" sikayeti.
#
# Dogru kaynak HEDEFIN kendi modulleri: BR2_TARGET_GRUB2 onlari ayni GRUB
# surumuyle target/lib/grub altina kuruyor, yani grub-mkimage ile surum
# uyumlu. Host paketi (/usr/lib/grub) yalnizca yedek. Bulunan dizin
# grub-mkimage'a -d ile ACIKCA veriliyor.
grub_dir_for() {
    # $1 = hedef (i386-pc | x86_64-efi), stdout = bulunan dizin ya da bos.
    for d in "${TARGET_DIR:-}/lib/grub/$1" "${TARGET_DIR:-}/usr/lib/grub/$1" \
             "${HOST_DIR:-}/lib/grub/$1" "/usr/lib/grub/$1"; do
        [ -f "$d/normal.mod" ] && [ -f "$d/moddep.lst" ] && { echo "$d"; return; }
    done
}
MCOS_GRUB_HATA=0

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

    if grub-mkimage -O i386-pc -d "$GRUB_PC_DIR" -o "$MCOS_BOOTLIB/core.img" \
        -c "${BUILD_DIR:-/tmp}/mcos-grub-early.cfg" \
        -p '/boot/grub' \
        biosdisk part_msdos part_gpt fat ext2 normal configfile linux boot \
        search search_fs_file search_label search_fs_uuid echo test \
        all_video vbe vga gfxterm minicmd sleep reboot halt \
        >>"$MCOS_GRUB_LOG" 2>&1
    then
        echo "post-build: BIOS onyukleyici gomuldu ($(stat -c%s "$MCOS_BOOTLIB/core.img") bayt core.img)"
    else
        echo "post-build: HATA - BIOS core.img uretilemedi ($GRUB_PC_DIR)"
        echo "post-build:        ayrinti: $MCOS_GRUB_LOG"
        tail -3 "$MCOS_GRUB_LOG" 2>/dev/null | sed 's/^/post-build:        /'
        rm -f "$MCOS_BOOTLIB/core.img" "$MCOS_BOOTLIB/boot.img"
        MCOS_GRUB_HATA=1
    fi
else
    echo "post-build: UYARI - /usr/lib/grub/i386-pc yok (grub-pc-bin); BIOS boot DESTEKLENMEYECEK"
fi

# ── UEFI ────────────────────────────────────────────────────────────────────
if [ -n "$GRUB_EFI_DIR" ]; then
    # prefix=/EFI/BOOT: mcos-install grub.cfg'yi tam oraya yazar.
    # Bu ikisi BIRLIKTE degismelidir — scripts/test-boot-logic.sh dogrular.
    if grub-mkimage -O x86_64-efi -d "$GRUB_EFI_DIR" -o "$MCOS_BOOTLIB/BOOTX64.EFI" \
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
        echo "post-build: HATA - BOOTX64.EFI uretilemedi ($GRUB_EFI_DIR)"
        echo "post-build:        ayrinti: $MCOS_GRUB_LOG"
        tail -3 "$MCOS_GRUB_LOG" 2>/dev/null | sed 's/^/post-build:        /'
        rm -f "$MCOS_BOOTLIB/BOOTX64.EFI"
        MCOS_GRUB_HATA=1
    fi
else
    echo "post-build: UYARI - /usr/lib/grub/x86_64-efi yok (grub-efi-amd64-bin); UEFI boot DESTEKLENMEYECEK"
fi

# Modul dizini VAR ama uretim basarisizsa bu bir derleme hatasidir; sessizce
# gecilmez. (Bir kez gecildi ve hatayi kullanici sahada, kurulum ekraninda
# "hata kodu 1" olarak gordu.) mcos-install'in grub-install yedegi var, ama
# yedek yol yavas ve bu hata onun da gizlenmesine yol acmamali.
if [ "$MCOS_GRUB_HATA" = 1 ]; then
    echo "post-build: gomulu onyukleyici uretilemedi — derleme durduruluyor"
    exit 1
fi

# ── Qualcomm ath11k Wi-Fi firmware'i (2021 sonrasi dizustuler) ───────────────
#
# CONFIG_ATH11K_PCI cekirdekte acik, ama Buildroot 2024.02'nin linux-firmware
# paketinde ath11k icin bir secenek YOK: surucu probe ederken firmware bulamayip
# Wi-Fi kartini hic kaldirmiyordu (QCNFA765 / WCN6855 — Lenovo, Dell, HP'nin
# pek cok modeli). Paketin kaynak dizininden YALNIZCA dizustu yongalari
# kopyalaniyor: QCA6390 (4 MB) ve WCN6855 (12 MB). IPQ*/QCN9074/WCN6750 yonlendirici
# ve gomulu yongalar (28 MB daha) — initramfs RAM'de durdugu icin alinmiyor.
FW_SRC="$(ls -d "${BUILD_DIR:-}"/linux-firmware-* 2>/dev/null | head -1)"
if [ -n "$FW_SRC" ] && [ -d "$FW_SRC/ath11k" ]; then
    for y in QCA6390 WCN6855; do
        if [ -d "$FW_SRC/ath11k/$y" ]; then
            mkdir -p "${TARGET_DIR}/lib/firmware/ath11k"
            cp -a "$FW_SRC/ath11k/$y" "${TARGET_DIR}/lib/firmware/ath11k/"
        fi
    done
    echo "post-build: ath11k firmware'i eklendi (QCA6390, WCN6855)"
else
    echo "post-build: UYARI - linux-firmware kaynagi bulunamadi; ath11k Wi-Fi kartlari calismayacak"
fi

# ── Buildroot seceneklerinin KAPSAMADIGI Wi-Fi firmware'leri ──────────────────
#
# Kullanici: "Ventoy ile normal kipte acinca kablosuz ile baglanamiyorum".
# Olculdu (2026-09-26): cekirdekteki gomulu Wi-Fi suruculerinin her
# MODULE_FIRMWARE satiri (modules.builtin.modinfo) imajdaki /lib/firmware ile
# karsilastirildi. Asagidakiler upstream linux-firmware-20240115'te VAR ama
# imajda YOKTU; Buildroot 2024.02'nin linux-firmware paketinde secenekleri
# yok (ornegin _IWLWIFI_6E yalnizca "so-a0-gf-a0" globunu kopyaliyor):
#   iwlwifi-ty-a0-gf-a0     Intel AX210 (Wi-Fi 6E) — 2020+ dizustu/masaustu, COK yaygin
#   iwlwifi-so-a0-hr-b0     Intel AX201/AX203 — 12./13. nesil Intel dizustuleri
#   iwlwifi-so-a0-jf-b0     Intel 9462/9560 — Alder Lake yongasetinde
#   iwlwifi-so-a0-gf4-a0    Intel AX411
#   iwlwifi-ma-b0-*         Intel AX211/AX201 (Meteor Lake)
#   iwlwifi-gl-c0-fm-c0     Intel BE200 (Wi-Fi 7)
#   iwlwifi-100..6050       2010-2012 Intel kartlari (eski dizustuler)
#   mt7615/mt7663, mt7650e, mt7610u   MediaTek PCIe ve USB (AC600 USB cubuklari)
#   rtl8723befw_36          Realtek RTL8723BE (ucuz dizustulerin cok yaygin karti)
#   rtl8192fu, rtl8710bu    Realtek USB cubuklari
#   ath10k QCA9887          Qualcomm
# Firmware yoksa surucu kartta YUKLENIR ama wlan arayuzu HIC olusmaz
# ("no suitable firmware found!"): panel ag goremez, baglanamaz.
#
# Surumler: iwlwifi yalnizca cekirdegin kabul ettigi EN YUKSEK API'li dosya
# (6.6'da AX210/BZ ailesi 83, so-a0-jf-b0 icin upstream'deki en yenisi 77)
# kopyalaniyor: daha yenisini cekirdek hic istemez, eskilerini de denemez
# (en yeniyi bulunca durur). Toplam ~19 MB acik / ~9 MB gzip; makinede Intel
# karti yoksa mcos-fwprune iwlwifi-* dosyalarini acilista RAM'den siler.
MCOS_WIFI_FW="
iwlwifi-ty-a0-gf-a0-83.ucode iwlwifi-ty-a0-gf-a0.pnvm
iwlwifi-so-a0-hr-b0-83.ucode
iwlwifi-so-a0-jf-b0-77.ucode
iwlwifi-so-a0-gf4-a0-83.ucode iwlwifi-so-a0-gf4-a0.pnvm
iwlwifi-ma-b0-gf-a0-83.ucode iwlwifi-ma-b0-gf-a0.pnvm
iwlwifi-ma-b0-gf4-a0-83.ucode iwlwifi-ma-b0-gf4-a0.pnvm
iwlwifi-ma-b0-hr-b0-83.ucode
iwlwifi-gl-c0-fm-c0-83.ucode iwlwifi-gl-c0-fm-c0.pnvm
iwlwifi-100-5.ucode iwlwifi-1000-5.ucode iwlwifi-105-6.ucode iwlwifi-135-6.ucode
iwlwifi-2000-6.ucode iwlwifi-2030-6.ucode iwlwifi-5150-2.ucode iwlwifi-6050-5.ucode
mediatek/mt7615_cr4.bin mediatek/mt7615_n9.bin mediatek/mt7615_rom_patch.bin
mediatek/mt7663_n9_rebb.bin mediatek/mt7663_n9_v3.bin mediatek/mt7663pr2h.bin
mediatek/mt7663pr2h_rebb.bin mediatek/mt7650e.bin mediatek/mt7610u.bin
rtlwifi/rtl8723befw_36.bin rtlwifi/rtl8192fufw.bin
rtlwifi/rtl8710bufw_SMIC.bin rtlwifi/rtl8710bufw_UMC.bin
ath10k/QCA9887/hw1.0/board.bin ath10k/QCA9887/hw1.0/firmware-5.bin
"
if [ -n "$FW_SRC" ]; then
    MCOS_WIFI_EKSIK=""
    for f in $MCOS_WIFI_FW; do
        if [ -f "$FW_SRC/$f" ]; then
            mkdir -p "${TARGET_DIR}/lib/firmware/$(dirname "$f")"
            cp -a "$FW_SRC/$f" "${TARGET_DIR}/lib/firmware/$f"
        else
            MCOS_WIFI_EKSIK="$MCOS_WIFI_EKSIK $f"
        fi
    done
    if [ -n "$MCOS_WIFI_EKSIK" ]; then
        # linux-firmware surumu degistiyse dosya adlari kayar: SESSIZCE
        # gecmesin, yoksa o kartlarda Wi-Fi yine hic olusmaz.
        echo "post-build: HATA - Wi-Fi firmware'i kaynakta yok:$MCOS_WIFI_EKSIK"
        echo "post-build: linux-firmware surumu degistiyse MCOS_WIFI_FW listesini guncelleyin"
        exit 1
    fi
    echo "post-build: ek Wi-Fi firmware'i eklendi (AX210/AX201/BE200, MediaTek, Realtek)"
else
    echo "post-build: UYARI - linux-firmware kaynagi yok; AX210/AX201 vb. Wi-Fi kartlari calismayacak"
fi

# Clean up any leftover boot binaries in target rootfs so initrd doesn't recursively pack itself!
rm -f "${TARGET_DIR}/boot/initrd.img" "${TARGET_DIR}/boot/rootfs.cpio.gz" 2>/dev/null || true

# Copy bzImage into target boot directory for installed systems to use
if [ -n "${BINARIES_DIR:-}" ]; then
    [ -f "${BINARIES_DIR}/bzImage" ] && cp "${BINARIES_DIR}/bzImage" "${TARGET_DIR}/boot/bzImage" || true
fi

# ── Derleme kimliği (güncelleme sistemi) ────────────────────────────────────
#
# Kurulu sistemde kök (p2) initramfs'in bir KOPYASIDIR. USB'deki yeni ISO ile
# güncellemede yalnızca p1'deki bzImage/initrd.img değişir; /init açılışta
# RAM'deki kimlik ile p2'dekini karşılaştırır, farklıysa yeni sistemi p2'ye
# kopyalar (rootfs-overlay/init). Kimlik HER derlemede yenilenir: aynı sürüm
# numarasıyla yapılan iki derleme de birbirinden ayrılabilmeli, yoksa ikinci
# derlemenin düzeltmeleri kurulu diske hiç inmezdi.
#
# Aynı değer ISO'ya /mcos/build-id olarak konur (Makefile "iso" hedefi,
# scripts/mkiso.sh); panel USB'deki ISO'nun daha yeni mi eski mi olduğunu
# ona bakarak söyler. Biçim "YYYYAAGG-SSDD-hash": ilk kısım sıralanabilir.
MCOS_SURUM="$(cat "${BOARD_DIR}/../../../../VERSION" 2>/dev/null | tr -d ' \r\n' || true)"
[ -n "$MCOS_SURUM" ] || MCOS_SURUM="0.0.0"
MCOS_ZAMAN="$(date -u +%Y%m%d-%H%M)"
MCOS_OZET="$(printf '%s %s %s' "$MCOS_SURUM" "$(date -u +%s%N)" "$$" | sha256sum | cut -c1-8)"
printf '%s-%s\n' "$MCOS_ZAMAN" "$MCOS_OZET" > "${TARGET_DIR}/etc/mcos-build-id"
printf '%s\n' "$MCOS_SURUM" > "${TARGET_DIR}/etc/mcos-version"
echo "post-build: derleme kimliği $(cat "${TARGET_DIR}/etc/mcos-build-id") (sürüm $MCOS_SURUM)"

# ── Çevrimdışı sunucu paketi ────────────────────────────────────────────────
#
# Java, Fabric çalışma zamanı ve Via modları imaja gömülür. Böylece internet
# olmadan da sunucu kurulabilir.
#
# DİKKAT: Minecraft sunucu jar'ı BURADA YOK ve OLAMAZ — Mojang EULA'sı
# dağıtımını açıkça yasaklıyor (bkz. offline-manifest.txt). O tek dosya bir
# kez Mojang'dan indirilir ve /data/artifacts içine önbelleklenir; sonraki
# kurulumlar tamamen çevrimdışı çalışır.
#
# Paket ROOTFS'E DEĞİL ayrı bir dizine konur: initramfs tamamen RAM'e açılır,
# 65 MB'ı oraya koymak her açılışta 65 MB RAM demektir. mcos-install bunu
# kalıcı bölüme tohumlar.
MCOS_OFFLINE_SRC=""
d="$(cd "$(dirname "$0")" && pwd)"
while [ "$d" != "/" ]; do
    if [ -d "$d/dist/offline" ]; then
        MCOS_OFFLINE_SRC="$d/dist/offline"
        break
    fi
    d="$(dirname "$d")"
done

# ── DÜZELTİLEN GERÇEK HATA: paket ROOTFS'E kopyalanıyordu ───────────────────
#
# Hemen yukarıdaki açıklama "Paket ROOTFS'E DEĞİL ayrı bir dizine konur"
# diyordu, ama kod tam tersini yapıyordu:
#
#     cp -a "$MCOS_OFFLINE_SRC"/. "${TARGET_DIR}/usr/lib/mcos/offline/"
#
# TARGET_DIR rootfs'tir ve bu rootfs BR2_TARGET_ROOTFS_CPIO ile initramfs'e
# paketlenip her açılışta TAMAMEN RAM'e açılır. Yani 76 MB'lık paket, canlı
# ISO'da hiç kullanılmasa bile her açılışta RAM'den 76 MB yiyordu — ve
# kullanıcının VirtualBox'ta ayırdığı 2 GB'ın içinden.
#
# Doğrusu: paket BINARIES_DIR altına konur (imaj yapımcıları oradan alır) ve
# ISO'ya /mcos/offline olarak yazılır. Kurulum sırasında mcos-install onu
# önyükleme ortamından okuyup kalıcı bölüme tohumlar.
#
# İSTİSNA: playit ikilileri. Onlar rootfs'e girer (aşağıda), çünkü tünel
# CANLI ISO'da da çalışabilmeli ve toplam 11 MB'dır.
MCOS_OFFLINE_STAGE=""
if [ -n "$MCOS_OFFLINE_SRC" ] && [ -n "$(ls -A "$MCOS_OFFLINE_SRC" 2>/dev/null)" ]; then
    if [ -n "${BINARIES_DIR:-}" ]; then
        MCOS_OFFLINE_STAGE="${BINARIES_DIR}/mcos-offline"
        rm -rf "$MCOS_OFFLINE_STAGE"
        mkdir -p "$MCOS_OFFLINE_STAGE"
        for f in "$MCOS_OFFLINE_SRC"/*; do
            [ -f "$f" ] || continue
            # playit ikilileri rootfs'e gidiyor; ortama İKİNCİ bir kopya
            # koymak 11 MB'ı iki kez taşımak olurdu.
            case "$(basename "$f")" in
                *playit-linux-amd64|*playit-cli-linux-amd64) continue ;;
                # Eski paketlerde kalan Java 21 JRE: 21 artık rootfs'e gömülü
                # (bkz. dosya sonu); ISO'ya ikinci kopyası 50 MB ölü ağırlıktı.
                # YALNIZCA 21 atlanır: Java 25 arşivi (26.x sunucuları için)
                # BİLEREK burada gelir — rootfs'e açılsaydı canlı ISO'da her
                # açılışta +144 MB RAM yerdi. internal/java onu kalıcı bölümden
                # (/data/artifacts) internetsiz kurar (LocalArchiveDirs).
                *OpenJDK21U-jre_*) continue ;;
            esac
            cp -a "$f" "$MCOS_OFFLINE_STAGE/"
        done
        sz="$(du -sh "$MCOS_OFFLINE_STAGE" 2>/dev/null | cut -f1)"
        echo "post-build: cevrimdisi paket ONYUKLEME ORTAMINA hazirlandi ($sz)"
        echo "post-build:          -> $MCOS_OFFLINE_STAGE (rootfs'e GIRMIYOR)"
    fi

    # Rootfs'e yalnızca KÜÇÜK bir bildirim dosyası: panel ve mcos-install
    # neyin var olduğunu buradan bilir, 76 MB taşımadan.
    mkdir -p "${TARGET_DIR}/usr/lib/mcos"
    (cd "$MCOS_OFFLINE_SRC" && ls -1) > "${TARGET_DIR}/usr/lib/mcos/offline-index.txt"
else
    echo "post-build: cevrimdisi paket yok - 'make offline-bundle' ile indirin."
    echo "post-build:          o olmadan sunucu kurulumu internet gerektirir."
fi

# ── playit tünel ajanı ──────────────────────────────────────────────────────
#
# İki ikili de statik-pie musl derlemesidir: hiçbir libc bağımlılığı yok,
# rootfs'e olduğu gibi düşüyor. CA sertifikası da gerekmiyor (rustls kök
# sertifikaları ikiliye gömülü).
#
# Bunlar çevrimdışı paketten AYRI olarak /usr/bin'e kurulur: tünel, kalıcı
# bölüm olmadan da (canlı ISO'da) çalışabilmeli.
#
# DİKKAT: "playit-linux-amd64" CLI DEĞİL, arka plan servisidir (playitd).
# playit 1.0 mimarisi servis + CLI olarak ikiye bölünmüştür; ikisi de gerekir.
#
# KAYNAK dizinden kuruluyor (rootfs'teki offline/ kopyası kaldırıldı; bkz.
# yukarıdaki düzeltme). Kullanıcının isteği netti: "bide playiti de gom" —
# yani tünel ajanı imajın PARÇASI olmalı, ilk kullanımda indirilen bir şey
# değil.
if [ -n "$MCOS_OFFLINE_SRC" ]; then
    playit_found=0
    for f in "$MCOS_OFFLINE_SRC"/*playit-linux-amd64; do
        [ -f "$f" ] || continue
        install -D -m 0755 "$f" "${TARGET_DIR}/usr/bin/playitd"
        echo "post-build: playitd gomuldu ($(du -h "$f" | cut -f1))"
        playit_found=$((playit_found + 1))
    done
    for f in "$MCOS_OFFLINE_SRC"/*playit-cli-linux-amd64; do
        [ -f "$f" ] || continue
        install -D -m 0755 "$f" "${TARGET_DIR}/usr/bin/playit-cli"
        echo "post-build: playit-cli gomuldu ($(du -h "$f" | cut -f1))"
        playit_found=$((playit_found + 1))
    done
    if [ "$playit_found" -lt 2 ]; then
        echo "post-build: UYARI - playit ikilileri bulunamadi; Tunel ekrani"
        echo "post-build:          'ajan kurulu degil' diyecek."
        echo "post-build:          Cozum: make offline-bundle"
    fi
else
    echo "post-build: UYARI - playit GOMULMEDI (cevrimdisi paket yok)."
    echo "post-build:          Cozum: make offline-bundle && make iso"
fi

# ── Seçili firmware paketleri GERÇEKTEN imajda mı? ───────────────────────────
#
# Yakalanan hata (2026-09-25 derlemesi): mcos_defconfig'te AMDGPU, RADEON ve
# QUALCOMM_6174 açıktı, Buildroot .config'i de onları gösteriyordu, ama
# target/lib/firmware/amdgpu YOKTU. Paket eski seçimle derlenmişti ve yalnızca
# yeniden KURULMUŞTU (br-firmware.tar eski listeyle kaldı). Derleme başarılı
# göründü; AMD ekran kartlı bir bilgisayarda panel kara ekranla açılacaktı.
# Artık seçili her paketin temsilci dosyası denetleniyor; yoksa derleme DURUR.
MCOS_FW_HATA=0
fw_gerekli() {
    # $1 = BR2 sembolü (LINUX_FIRMWARE_ sonrası), $2 = hedefte olması gereken yol
    if grep -q "^BR2_PACKAGE_LINUX_FIRMWARE_$1=y" "${BR2_CONFIG}" 2>/dev/null; then
        if [ -z "$(ls -A "${TARGET_DIR}/lib/firmware/$2" 2>/dev/null)" ]; then
            echo "post-build: HATA - $1 seçili ama /lib/firmware/$2 imajda yok"
            MCOS_FW_HATA=1
        fi
    fi
}
fw_gerekli AMDGPU amdgpu
fw_gerekli RADEON radeon
fw_gerekli QUALCOMM_6174 ath10k/QCA6174
fw_gerekli RTL_8169 rtl_nic
fw_gerekli IWLWIFI_22260 "iwlwifi-cc-a0-77.ucode"
if [ "$MCOS_FW_HATA" = 1 ]; then
    echo "post-build: Çözüm: make -C ${BASE_DIR} linux-firmware-rebuild && make iso"
    exit 1
fi
echo "post-build: seçili firmware paketleri imajda (amdgpu, radeon, QCA6174, ...)"

# ── Gömülü Java 21 (Temurin JRE) ────────────────────────────────────────────
#
# Kullanıcının isteği: "OS'un içine Java'yı göm, hiç uğraşmayalım, direkt
# kurulu gelsin Java 21." JRE, SHA-256'sı doğrulanarak
# /usr/lib/jvm/temurin-21-jre altına açılır; internal/java onu indirme
# gerektirmeyen KURULU bir çalışma zamanı olarak tanır.
#
# Yukarıdaki çevrimdışı paketin aksine bu bilerek ROOTFS'E (RAM'e) girer:
# kurulu diski olmayan canlı ISO'da da sunucu Java'sız kalmamalı. Bedeli
# ölçüldü: ~143 MB açık (kalıcı RAM), ~48 MB sıkıştırılmış.
#
# Arşiv yoksa ya da özeti tutmuyorsa derleme DURUR (firmware denetimi gibi):
# Java'sız bir imaj, kullanıcıya "kurulu gelir" sözünü sessizce bozardı.
# Arşivi "make builtin-java" indirir; "make os" onu kendiliğinden çağırır.
sh "${BOARD_DIR}/builtin-java.sh" || exit 1

# >>> mcos-link eklentileri (ortak dünya) ────────────────────────────────────
#
# Yakalanan hata (kullanıcı, gerçek PC): "PC eşleştirmede ortak dünyayı
# açınca 'mcos link kurulu değil' diyor." İki kez yaşandı:
#   1. 2026-09-26 imajında jar'lar imaja HİÇ kopyalanmıyordu (/usr/lib/mcos/mods
#      yoktu).
#   2. Kopyalanan tek jar YALNIZCA Minecraft 1.21.11 içindi; çevrimiçi kurulan
#      sunucu 26.3 alıyordu ve daemon modu kurmuyordu.
# Artık her sürümün jar'ı ve hangi sürüme hangisinin gittiğini söyleyen indeks
# (dist/mods/link/index-*.tsv; biçim internal/linkjar'da) imaja girer. Daemon
# onları /usr/lib/mcos/mods/link'te arar (internal/daemon/handlers_link.go
# linkJarDirs).
#
# NEREYE NE:
#   - mcos-link-*.jar + index-*.tsv -> ${TARGET_DIR}/usr/lib/mcos/mods/link
#     (rootfs = RAM; hepsi ~0,2 MB, ölçülemeyecek kadar ucuz). Yalnızca
#     İNDEKSİN gösterdiği jar'lar kopyalanır: indekste olmayan bir jar'ı daemon
#     hiçbir sürüme seçmez, RAM'de ölü ağırlık olurdu.
#   - mcos-link-velocity.jar (index-velocity.tsv, sürüm sütunu "*"): kurucunun
#     Velocity proxy'sinin eklentisi; aynı döngüyle kopyalanır, daemon onu
#     sabit adıyla bulup /data/proxy/plugins'e koyar (arka uç listesi
#     değişince proxy yeniden başlamasın, oyuncular düşmesin). İndeksi
#     ZORUNLU DEĞİL: yoksa proxy eski yolla (yeniden başlatarak) çalışır.
#   - fabric-api jar'ları (indeksin 4. sütunu) -> çevrimdışı paketin önyükleme
#     ortamı ($MCOS_OFFLINE_STAGE = ${BINARIES_DIR}/mcos-offline -> ISO'da
#     /mcos/offline). Rootfs'e GİRMEZ: her biri ~2,3 MB, 17 sürüm ~40 MB RAM
#     yerdi. mcos-install onları AYNI adla /data/artifacts'a kopyalar; daemon
#     orada arar (linkjar.FindDep), yoksa Modrinth'ten indirir.
#
# Eksik ya da bozuksa derleme DURUR (firmware denetimi gibi): iki indeksten
# (fabric, paper) biri yoksa ya da indeksin gösterdiği bir dosya yok/zip
# değilse. Eksik indeksli imaj "Ortak dünya" düğmesini gösterip her denemede
# "index-fabric.tsv yok" derdi. MCOS_MODS_SRC (dist/mods) ya da MCOS_LINK_SRC
# (dist/mods/link) ile kaynak ezilebilir (scripts/test-link-mods.sh).
#
# ESKİ düzen (/usr/lib/mcos/mods/mcos-link.jar, mcos-link-paper.jar): varsa
# ŞİMDİLİK yazılmaya devam eder, yoksa derleme durmaz. Daemon onları yalnızca
# 1.21.11 için ve indeks o sürümü kapsamıyorsa son çare olarak kullanır
# (linkjar.LegacyMC); ~85 KB. Geçiş bitince (dist/mods/*.jar üretilmez olunca)
# bu kısım kendiliğinden boşa düşer.
if [ -z "${MCOS_MODS_SRC:-}" ]; then
    d="$(cd "$(dirname "$0")" && pwd)"
    while [ "$d" != "/" ]; do
        if [ -d "$d/dist/mods" ]; then
            MCOS_MODS_SRC="$d/dist/mods"
            break
        fi
        d="$(dirname "$d")"
    done
fi
MCOS_LINK_SRC="${MCOS_LINK_SRC:-${MCOS_MODS_SRC:-/yok}/link}"
MCOS_LINK_DST="${TARGET_DIR}/usr/lib/mcos/mods/link"
# Çevrimdışı paket yoksa yukarıdaki blok MCOS_OFFLINE_STAGE'i boş bırakır;
# fabric-api yine de ISO'ya gitmeli (Makefile iso: images/mcos-offline).
MCOS_LINK_STAGE="${MCOS_OFFLINE_STAGE:-}"
if [ -z "$MCOS_LINK_STAGE" ] && [ -n "${BINARIES_DIR:-}" ]; then
    MCOS_LINK_STAGE="${BINARIES_DIR}/mcos-offline"
fi
# "PK": jar bir zip'tir. Boş ya da yarım bir dosya kopyalansaydı daemon onu
# "kurulu" sayar, Minecraft ise açılışta yükleyemeyip nedenini söylemezdi.
mcos_jar_ok() { [ -s "$1" ] && [ "$(head -c 2 "$1")" = "PK" ]; }
MCOS_MOD_HATA=0
for ld in fabric paper; do
    if [ ! -s "$MCOS_LINK_SRC/index-$ld.tsv" ]; then
        echo "post-build: HATA - $MCOS_LINK_SRC/index-$ld.tsv yok (ortak dünya jar dizini)"
        MCOS_MOD_HATA=1
    fi
done
mcos_mods=0
mcos_deps=0
if [ "$MCOS_MOD_HATA" = 0 ]; then
    rm -rf "$MCOS_LINK_DST"
    mkdir -p "$MCOS_LINK_DST"
    tab="$(printf '\t')"
    cr="$(printf '\r')"
    for idx in "$MCOS_LINK_SRC"/index-*.tsv; do
        install -m 0644 "$idx" "$MCOS_LINK_DST/"
        # "|| [ -n "$ld" ]": son satırın sonunda satır sonu YOKSA read o
        # satırı okur ama 1 döndürür ve döngü onu atlar. Sahte bir indeksle
        # (printf, son satır "fabric 26.3 …" \n'siz) ölçüldü: 26.3 jar'ı
        # imaja girmedi, eksik olduğunda da derleme DURMADI. Daemon'un
        # ayrıştırıcısı (linkjar) o satırı OKUR ve çalışırken "kayıtlı ama
        # yok" derdi. index-paper.tsv elle yazılıp aynen kopyalanıyor.
        while IFS="$tab" read -r ld mc jar dep rest || [ -n "$ld" ]; do
            case "$(printf '%s' "$ld" | tr -d ' \r')" in ''|'#'*) continue ;; esac
            jar="${jar%"$cr"}"
            dep="${dep%"$cr"}"
            # Yalnızca dosya ADI: "../x.jar" imajın dışından dosya alırdı
            # (daemon da böyle satırları reddeder).
            case "$jar" in ''|*/*|.|..)
                echo "post-build: HATA - $(basename "$idx"): bozuk satır ($ld $mc '$jar')"
                MCOS_MOD_HATA=1; continue ;;
            esac
            if ! mcos_jar_ok "$MCOS_LINK_SRC/$jar"; then
                echo "post-build: HATA - $(basename "$idx") $jar dosyasını gösteriyor ama $MCOS_LINK_SRC/$jar yok ya da bozuk"
                MCOS_MOD_HATA=1
                continue
            fi
            if [ ! -f "$MCOS_LINK_DST/$jar" ]; then
                install -m 0644 "$MCOS_LINK_SRC/$jar" "$MCOS_LINK_DST/$jar"
                mcos_mods=$((mcos_mods + 1))
            fi
            case "$dep" in ''|-) continue ;; esac
            case "$dep" in */*|.|..)
                echo "post-build: HATA - $(basename "$idx"): bozuk bağımlılık adı ($mc '$dep')"
                MCOS_MOD_HATA=1; continue ;;
            esac
            if ! mcos_jar_ok "$MCOS_LINK_SRC/$dep"; then
                echo "post-build: HATA - $(basename "$idx") $dep dosyasını gösteriyor ama $MCOS_LINK_SRC/$dep yok ya da bozuk"
                MCOS_MOD_HATA=1
                continue
            fi
            if [ -n "$MCOS_LINK_STAGE" ] && [ ! -f "$MCOS_LINK_STAGE/$dep" ]; then
                mkdir -p "$MCOS_LINK_STAGE"
                cp "$MCOS_LINK_SRC/$dep" "$MCOS_LINK_STAGE/$dep"
                mcos_deps=$((mcos_deps + 1))
            fi
        done < "$idx"
    done
fi
if [ "$MCOS_MOD_HATA" = 1 ]; then
    echo "post-build: Çözüm: make mod && make iso"
    exit 1
fi
if [ -z "$MCOS_LINK_STAGE" ]; then
    echo "post-build: UYARI - BINARIES_DIR yok; fabric-api jar'ları ISO'ya konmadı (Fabric ortak dünyası internetten indirir)"
fi
for j in mcos-link.jar mcos-link-paper.jar; do
    src="${MCOS_MODS_SRC:-/yok}/$j"
    if mcos_jar_ok "$src"; then
        install -D -m 0644 "$src" "${TARGET_DIR}/usr/lib/mcos/mods/$j"
    fi
done
echo "post-build: ortak dünya jar'ları imajda (/usr/lib/mcos/mods/link: $mcos_mods jar + indeks; fabric-api -> ${MCOS_LINK_STAGE:-yok}: $mcos_deps)"
# <<< mcos-link eklentileri
