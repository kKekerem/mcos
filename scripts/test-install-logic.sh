#!/bin/sh
# mcos-install ve boot mantığının değişmezlerini izole doğrular.
# Hiçbir gerçek diske / blok aygıtına dokunmaz.
set -eu

FAILED=0
check() {
    if [ "$2" = "$3" ]; then
        printf '  ok   %-46s = %s\n' "$1" "$2"
    else
        printf '  FAIL %-46s = %s (beklenen: %s)\n' "$1" "$2" "$3"
        FAILED=1
    fi
}

INSTALL="$(dirname "$0")/../os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install"

# ── 1) cp -a birleştirme davranışı ──────────────────────────────────────────
#
# Ölçüm sonucu:
#   cp -a SRC     DEST/       -> DEST/$(basename SRC), VARSA İÇERİĞİ BİRLEŞİR
#   cp -a SRC     DEST/same   -> DEST/same/$(basename SRC)   (İÇ İÇE!)
#   cp -a SRC/.   DEST/same/  -> DEST/same içeriği birleşir  (en açık biçim)
echo "1) cp -a birlestirme davranisi"

W="$(mktemp -d)"
mkdir -p "$W/src/etc"
echo icerik > "$W/src/etc/inittab"

mkdir -p "$W/a/etc"; echo eski > "$W/a/etc/onceden-vardi"
cp -a "$W/src/etc" "$W/a/"
check "cp -a SRC DEST/ : DEST/etc/inittab" \
      "$([ -f "$W/a/etc/inittab" ] && echo var || echo yok)" "var"
check "cp -a SRC DEST/ : ic ice DEST/etc/etc olusmaz" \
      "$([ -d "$W/a/etc/etc" ] && echo olustu || echo yok)" "yok"
check "cp -a SRC DEST/ : onceki dosya korunur" \
      "$([ -f "$W/a/etc/onceden-vardi" ] && echo var || echo yok)" "var"

mkdir -p "$W/b/etc"
cp -a "$W/src/etc" "$W/b/etc"
check "cp -a SRC DEST/etc : ic ice DEST/etc/etc olusur" \
      "$([ -d "$W/b/etc/etc" ] && echo olustu || echo yok)" "olustu"

mkdir -p "$W/c/etc"
cp -a "$W/src/etc/." "$W/c/etc/"
check "cp -a SRC/. DEST/etc/ : DEST/etc/inittab" \
      "$([ -f "$W/c/etc/inittab" ] && echo var || echo yok)" "var"
rm -rf "$W"

# ── 2) Bölüm adı türetme ────────────────────────────────────────────────────
echo
echo "2) Bolum adi turetme (case *[0-9] kurali)"
part_names() {
    case "$1" in
        *[0-9]) printf '%s %s' "${1}p1" "${1}p2" ;;
        *)      printf '%s %s' "${1}1"  "${1}2"  ;;
    esac
}
check "/dev/sda"     "$(part_names /dev/sda)"     "/dev/sda1 /dev/sda2"
check "/dev/sdb"     "$(part_names /dev/sdb)"     "/dev/sdb1 /dev/sdb2"
check "/dev/nvme0n1" "$(part_names /dev/nvme0n1)" "/dev/nvme0n1p1 /dev/nvme0n1p2"
check "/dev/mmcblk0" "$(part_names /dev/mmcblk0)" "/dev/mmcblk0p1 /dev/mmcblk0p2"
check "/dev/vda"     "$(part_names /dev/vda)"     "/dev/vda1 /dev/vda2"

# ── 3) Çekirdek root= biçimi bilgisi ────────────────────────────────────────
#
# Bu bölüm neden duruyor: kurulu sistem artık root= HİÇ kullanmıyor
# (initramfs modeli), ama biri ileride root= eklemek isterse hangi biçimlerin
# çekirdek tarafından çözülebildiğini bilmesi gerekir. name_to_dev_t() yalnızca
# /dev/xxx, maj:min, PARTUUID= ve PARTLABEL= biçimlerini bilir; "LABEL=" ve
# "UUID=" YALNIZCA userspace/initramfs tarafından çözülür.
echo
echo "3) root= bicim bilgisi (cekirdek tarafindan cozulebilirlik)"
kernel_resolvable() {
    case "$1" in
        PARTUUID=*|PARTLABEL=*|/dev/*) echo evet ;;
        [0-9]*:[0-9]*)                 echo evet ;;
        *)                             echo hayir ;;
    esac
}
check "PARTUUID=4d3c2b1a-02"          "$(kernel_resolvable 'PARTUUID=4d3c2b1a-02')" "evet"
check "LABEL=MCOS-ROOT (cozulemez)"   "$(kernel_resolvable 'LABEL=MCOS-ROOT')"      "hayir"
check "/dev/sda2"                     "$(kernel_resolvable '/dev/sda2')"            "evet"
check "UUID=... (cozulemez)"          "$(kernel_resolvable 'UUID=abcd')"            "hayir"

# ── 4) mcos-install: initramfs + kalıcı /data modelinin değişmezleri ────────
#
# Model (bkz. mcos-install baş yorumu): sistem initramfs'ten çalışır, kalıcılık
# "MCOS-DATA" etiketli ext4 bölümdedir. Bu model QEMU'da BIOS ve UEFI ile boot
# ederek doğrulandı (scripts/exp-grub-dd.sh, scripts/boottest.sh).
echo
echo "4) mcos-install: boot modeli degismezleri"

if [ ! -f "$INSTALL" ]; then
    echo "  FAIL mcos-install bulunamadi: $INSTALL"
    FAILED=1
else
    # Yorum satırlarını çıkar: tasarım notları "root=LABEL= kullanılmaz" gibi
    # ifadeler içerdiği için ham grep yanlış pozitif verir.
    CODE="$(grep -v '^[[:space:]]*#' "$INSTALL")"

    # 4a. ÇEKİRDEĞİN root= parametresi HİÇ verilmemeli.
    #
    #     Sistem initramfs'ten açılır; oradaki /init PID 1 olur ve çekirdek
    #     root='u zaten yok sayar. Yazmak yanlış beklenti yaratır ve eski
    #     "root=LABEL=" panic'inin geri gelmesine kapı açar.
    #
    #     DİKKAT — "mcos.root=" BAŞKA BİR ŞEYDİR ve serbesttir: o bizim kendi
    #     parametremizdir, /init onu okuyup diske switch_root yapar
    #     (kalıcılık). Bu yüzden yalnızca SÖZCÜK BAŞINDAKİ root= aranır;
    #     aksi halde "mcos.root=" de yanlışlıkla eşleşirdi.
    check "cekirdek root= parametresi kullanilmiyor" \
          "$(printf '%s\n' "$CODE" | grep -cE '(^|[[:space:]"])root=' || true)" "0"

    # 4a-2. Kendi kalıcılık parametremiz ise VAR olmalı.
    check "mcos.root= kalicilik parametresi var" \
          "$(printf '%s\n' "$CODE" | grep -c 'mcos\.root=' || true)" "1"

    # 4b. grub.cfg initrd YÜKLEMELİ (modelin çekirdeği). Normal + kurtarma
    #     girdisi = 2 satır.
    check "grub.cfg initrd yukluyor (2 girdi)" \
          "$(printf '%s\n' "$CODE" | grep -c 'initrd /boot/initrd.img' || true)" "2"

    # 4c. Kalıcılık MCOS-DATA etiketine bağlı (S99mcos bunu arar).
    check "kalici bolum MCOS-DATA etiketli" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c 'MCOS-DATA')" -gt 0 ] && echo evet || echo hayir)" "evet"

    # 4d. Hedefte grub-install/grub-mkimage ÇAĞRILMAMALI: o araçlar imajda yok
    #     (ölçüldü: scripts/probe-target-boot.sh). Eski kod onlara güvenip
    #     sessizce hiçbir önyükleyici yazmıyordu -> BIOS bootable görmüyordu.
    check "grub-install cagrilmiyor" \
          "$(printf '%s\n' "$CODE" | grep -c 'grub-install' || true)" "0"
    check "grub-mkimage cagrilmiyor" \
          "$(printf '%s\n' "$CODE" | grep -c 'grub-mkimage' || true)" "0"

    # 4e. Önyükleyici build zamanında gömülen dosyalardan gelmeli.
    check "gomulu onyukleyici dizini kullaniliyor" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c '/usr/lib/mcos/boot')" -gt 0 ] && echo evet || echo hayir)" "evet"
    check "MBR bootstrap dd ile yaziliyor" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c 'boot.img')" -gt 0 ] && echo evet || echo hayir)" "evet"
    check "core.img dd ile yaziliyor" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c 'core.img')" -gt 0 ] && echo evet || echo hayir)" "evet"

    # 4f. Sessiz başarısızlık olmamalı: önyükleyici kurulamazsa fail() çağrılır.
    check "onyukleyici yoksa kurulum basarisiz oluyor" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c 'INSTALLED_ANY')" -gt 0 ] && echo evet || echo hayir)" "evet"

    # 4g. Kurulum sonunda gerçekten doğrulama yapılmalı.
    check "kurulum sonunda dogrulama var" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c 'verify_fail')" -gt 0 ] && echo evet || echo hayir)" "evet"

    # 4h. OOBE ayarları kalıcı bölüme taşınmalı, yoksa kullanıcı sihirbazı
    #     baştan doldurur.
    check "OOBE ayarlari (/data) tasiniyor" \
          "$([ "$(printf '%s\n' "$CODE" | grep -c 'cp -a /data/\.')" -gt 0 ] && echo evet || echo hayir)" "evet"

    check "calistirilabilir" \
          "$([ -x "$INSTALL" ] && echo evet || echo hayir)" "evet"
fi

# ── 5) mcos-findfs: etiket çözümleme yolları ────────────────────────────────
#
# Imajda udev YOK, bu yüzden /dev/disk/by-label/* hiç oluşmaz ve busybox blkid
# "-L" seçeneğini bilmez. mcos-findfs dört yolu sırayla denemeli.
echo
echo "5) mcos-findfs: etiket cozumleme"
FINDFS="$(dirname "$0")/../os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-findfs"
if [ ! -f "$FINDFS" ]; then
    echo "  FAIL mcos-findfs bulunamadi"
    FAILED=1
else
    for m in findfs 'blkid -L' by-label '/proc/mounts\|blkid "\$dev"'; do
        check "yontem denenmis: $m" \
              "$([ "$(grep -c -- "$m" "$FINDFS")" -gt 0 ] && echo evet || echo hayir)" "evet"
    done
    check "calistirilabilir" \
          "$([ -x "$FINDFS" ] && echo evet || echo hayir)" "evet"
fi

echo
if [ "$FAILED" -ne 0 ]; then
    echo "SONUC: BASARISIZ"
    exit 1
fi
echo "SONUC: TUM TESTLER GECTI"
