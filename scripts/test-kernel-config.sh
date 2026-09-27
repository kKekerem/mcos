#!/bin/sh
# test-kernel-config.sh — kernel.config'deki her sembol GERÇEKTEN çözülüyor mu?
#
# Kullanım:
#   sh scripts/test-kernel-config.sh                 # denetim
#   sh scripts/test-kernel-config.sh --karsi-sinama  # testin kendisini sına
#
# ── Yakalanan gerçek tuzak ──────────────────────────────────────────────────
#
# kernel.config'e bir satır yazmak, o seçeneğin AÇILDIĞI anlamına GELMEZ.
# Bağımlılığı karşılanmayan bir sembol kconfig tarafından SESSİZCE düşürülür:
# hata yok, uyarı yok, yalnızca özellik çalışmaz.
#
# Bu tam olarak başımıza geldi. Touchpad için I2C-HID yolu eklendi ve iki
# kritik sembol sessizce düştü:
#
#	CONFIG_I2C_DESIGNWARE_CORE
#	CONFIG_I2C_DESIGNWARE_PLATFORM
#
# Sebep: drivers/i2c/busses/Kconfig:558
#	depends on (ACPI && COMMON_CLK) || !ACPI
# ACPI açıktı, COMMON_CLK kapalıydı. COMMON_CLK eklenince ikisi de çözüldü.
# Aynı sınıftan: ATH11K (CRYPTO_MICHAEL_MIC), HYPERV_UTILS (CONNECTOR).
#
# ── Bu sürümde neler DEĞİŞTİ ve NEDEN ──────────────────────────────────────
#
# 1. Çözümleme Buildroot'un YAPTIĞI GİBİ yapılıyor: kernel.config .config
#    olarak alınıyor, linux.mk'nin LINUX_KCONFIG_FIXUP_CMDS düzeltmeleri
#    uygulanıyor, sonra olddefconfig. Eski sürüm fragmanı ESKİ .config'in
#    SONUNA ekliyordu; o zaman fragmandan silinen bir satırın değeri eski
#    .config'ten "miras" kalır ve test gerçekte olmayan bir şeyi geçer sayardı.
# 2. Eski düzenli ifade yalnızca [A-Z0-9_] ve "=y" arıyordu. CONFIG_MT76x0U,
#    MT76x2E gibi KÜÇÜK HARFLİ semboller ve "=1", "=\"rtc0\"" gibi değerler
#    hiç denetlenmiyordu. Artık her değer ve "is not set" satırı denetleniyor.
# 3. Aynı sembole ÇELİŞEN iki atama HATA: kconfig son satırı alır. vmwgfx
#    böyle bir tuzaktı - üstte =y, dosyanın sonunda "is not set".
# 4. Aileye göre rapor, SMBus/RMI4 birlikteliği ve firmware eşleşmesi
#    (firmware isteyen sürücü açıksa mcos_defconfig'te paketi de açık mı).
#
# ── Neden "derlenmiş çekirdek yoksa atla" ───────────────────────────────────
#
# Doğrulama GERÇEK kconfig ikilisini gerektiriyor; onu üretmek Buildroot
# derlemesi demek. Temiz bir ağaçta test bu yüzden ATLANIR ve NEDENİNİ söyler
# — sessizce "geçti" demek, hiç sınamamaktan daha kötü olurdu.
set -u

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FRAG="${KERNEL_FRAG:-$ROOT/os/buildroot/external/board/mcos/kernel.config}"
DEFC="${MCOS_DEFCONFIG:-$ROOT/os/buildroot/external/configs/mcos_defconfig}"
BR="${BR_OUTPUT:-$ROOT/os/buildroot/output}"

# ── Karşı-sınama ────────────────────────────────────────────────────────────
#
# NEDEN: "her şey geçti" diyen bir test, hiçbir şeyi yakalayamayan bir test
# de olabilir. Burada kopyalar kasten bozuluyor ve testin HER birinde
# BAŞARISIZ olduğu, üstelik DOĞRU sebeple başarısız olduğu doğrulanıyor.
if [ "${1:-}" = "--karsi-sinama" ]; then
    W="$(mktemp -d)"
    trap 'rm -rf "$W"' EXIT
    bad=0
    # $1 ad, $2 beklenen çıkış (0/1), $3 çıktıda aranacak metin, $4 fragman, $5 defconfig
    dene() {
        out="$(KERNEL_FRAG="$4" MCOS_DEFCONFIG="$5" sh "$0" 2>&1)"; rc=$?
        [ "$rc" -ne 0 ] && rc=1
        if [ "$rc" = "$2" ] && printf '%s' "$out" | grep -q -- "$3"; then
            printf '  \033[32mOK\033[0m   %s\n' "$1"
        else
            printf '  \033[31mHATA\033[0m %s (cikis=%s, beklenen=%s, aranan="%s")\n' "$1" "$rc" "$2" "$3"
            printf '%s\n' "$out" | grep -E 'HATA|ATLA' | head -5
            bad=$((bad + 1))
        fi
    }
    echo "== KARSI-SINAMA: bozulan kopyalarda test BASARISIZ olmali =="
    dene "bozulmamis kopya GECER" 0 "tutarli" "$FRAG" "$DEFC"

    # Gizli ön koşulu sil: designware sessizce düşmeli. Yalnız COMMON_CLK
    # satırını silmek ARTIK YETMİYOR - X86_INTEL_LPSS, X86_AMD_PLATFORM_DEVICE
    # (arch/x86/Kconfig:641/662) ve MFD_INTEL_LPSS (drivers/mfd/Kconfig:682)
    # onu "select" ediyor. İlk karşı-sınama tam bunu gösterdi: tuzak artık
    # kendiliğinden kapanıyor. Sınamak için seçenleri de silmek gerekiyor.
    grep -v -e '^CONFIG_COMMON_CLK=y$' -e '^CONFIG_X86_INTEL_LPSS=y$' \
            -e '^CONFIG_X86_AMD_PLATFORM_DEVICE=y$' \
            -e '^CONFIG_MFD_INTEL_LPSS_PCI=y$' -e '^CONFIG_MFD_INTEL_LPSS_ACPI=y$' \
            "$FRAG" > "$W/f1"
    dene "COMMON_CLK (ve secenleri) silinince I2C_DESIGNWARE_PLATFORM dusmesi yakalanir" 1 \
        "CONFIG_I2C_DESIGNWARE_PLATFORM SESSIZCE DUSUYOR" "$W/f1" "$DEFC"

    grep -v '^CONFIG_CRYPTO_MICHAEL_MIC=y$' "$FRAG" > "$W/f1b"
    dene "CRYPTO_MICHAEL_MIC silinince ATH11K dusmesi yakalanir" 1 \
        "CONFIG_ATH11K SESSIZCE DUSUYOR" "$W/f1b" "$DEFC"

    grep -v '^CONFIG_CONNECTOR=y$' "$FRAG" > "$W/f2"
    dene "CONNECTOR silinince HYPERV_UTILS dusmesi yakalanir" 1 \
        "CONFIG_HYPERV_UTILS SESSIZCE DUSUYOR" "$W/f2" "$DEFC"

    # RMI4_F3A başka hiçbir şey tarafından seçilmiyor; silinince yalnızca
    # birliktelik denetimi yakalayabilir (artık "istenmiyor" çünkü).
    grep -v '^CONFIG_RMI4_F3A=y$' "$FRAG" > "$W/f3"
    dene "SMBus acikken RMI4_F3A eksikligi yakalanir" 1 \
        "RMI4 BIRLIKTELIGI BOZUK.*CONFIG_RMI4_F3A" "$W/f3" "$DEFC"

    grep -v '^BR2_PACKAGE_LINUX_FIRMWARE_AMDGPU=y$' "$DEFC" > "$W/d1"
    dene "amdgpu acik + firmware paketi kapali yakalanir" 1 \
        "CONFIG_DRM_AMDGPU acik ama firmware paketi yok" "$FRAG" "$W/d1"

    { cat "$FRAG"; echo '# CONFIG_DRM_AMDGPU is not set'; } > "$W/f4"
    dene "ayni sembole celisen atama yakalanir" 1 \
        "CELISEN ATAMA: CONFIG_DRM_AMDGPU" "$W/f4" "$DEFC"

    # Yorum satırındaki sembol DENETLENMEMELİ; gerçek satırdaki denetlenmeli.
    { cat "$FRAG"; echo '# CONFIG_MCOS_OLMAYAN_SEMBOL=y   <- yorum'; } > "$W/f5"
    dene "yorumdaki sembol yok sayilir" 0 "tutarli" "$W/f5" "$DEFC"
    { cat "$FRAG"; echo 'CONFIG_MCOS_OLMAYAN_SEMBOL=y'; } > "$W/f6"
    dene "gercek satirdaki olmayan sembol yakalanir" 1 \
        "CONFIG_MCOS_OLMAYAN_SEMBOL SESSIZCE DUSUYOR" "$W/f6" "$DEFC"

    { cat "$FRAG"; echo '# CONFIG_DRM_VMWGFX is not set'; } > "$W/f7"
    dene "vmwgfx (VMware/VirtualBox) kapanirsa yakalanir" 1 \
        "CONFIG_DRM_VMWGFX KAPALI" "$W/f7" "$DEFC"

    { cat "$FRAG"; echo '# CONFIG_EXFAT_FS is not set'; } > "$W/f8"
    dene "exFAT kapanirsa (Ventoy kaliciligi) yakalanir" 1 \
        "CONFIG_EXFAT_FS KAPALI" "$W/f8" "$DEFC"

    # Turbo: RAPL kapanırsa ve menü sembolü silinince HP_WMI sessizce
    # düşerse (gizli ön koşul) yakalanmalı.
    grep -v '^CONFIG_POWERCAP=y' "$FRAG" > "$W/f9"
    dene "POWERCAP silinirse (turbo PL1) yakalanir" 1 \
        "CONFIG_INTEL_RAPL KAPALI - turbo" "$W/f9" "$DEFC"
    grep -v '^CONFIG_X86_PLATFORM_DRIVERS_HP=y' "$FRAG" > "$W/f10"
    dene "HP menusu silinince HP_WMI dusmesi yakalanir" 1 \
        "CONFIG_HP_WMI KAPALI - turbo" "$W/f10" "$DEFC"

    echo
    if [ "$bad" -gt 0 ]; then
        printf '\033[31m%d karsi-sinama basarisiz: test bir bozulmayi YAKALAYAMIYOR\033[0m\n' "$bad"
        exit 1
    fi
    printf '\033[32mkarsi-sinama tamam: test her bozulmayi dogru sebeple yakaliyor\033[0m\n'
    exit 0
fi

skip() { printf '  \033[33mATLA\033[0m %s\n' "$1"; }

echo "== CEKIRDEK YAPILANDIRMASI: SEMBOLLER GERCEKTEN COZULUYOR MU =="

[ -f "$FRAG" ] || { printf '  \033[31mHATA\033[0m kernel.config yok: %s\n' "$FRAG"; exit 1; }
[ -f "$DEFC" ] || { printf '  \033[31mHATA\033[0m defconfig yok: %s\n' "$DEFC"; exit 1; }
command -v python3 >/dev/null 2>&1 || { skip "python3 yok"; exit 0; }

KDIR="$(ls -d "$BR"/build/linux-*/ 2>/dev/null | grep -v firmware | grep -v headers | head -1)"
if [ -z "$KDIR" ] || [ ! -x "$KDIR/scripts/kconfig/conf" ]; then
    skip "derlenmis cekirdek yok - once 'make os' (kconfig ikilisi gerekiyor)"
    exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Buildroot'un linux paketinin yaptığı: BR2_LINUX_KERNEL_USE_CUSTOM_CONFIG
# dosyası .config olarak kopyalanır, sonra LINUX_KCONFIG_FIXUP_CMDS
# (buildroot/linux/linux.mk:384) uygulanır. Bu defconfig için etkin olanlar:
# gzip sıkıştırma, cpio için BLK_DEV_INITRD, devtmpfs, GCC eklentileri kapalı.
cp "$FRAG" "$TMP/c"
ayarla() { sed -i -e "/^$1=/d" -e "/^# $1 is not set/d" "$TMP/c"; echo "$1=$2" >> "$TMP/c"; }
kapat()  { sed -i -e "/^$1=/d" -e "/^# $1 is not set/d" "$TMP/c"; echo "# $1 is not set" >> "$TMP/c"; }
ayarla CONFIG_KERNEL_GZIP y
for o in CONFIG_KERNEL_LZ4 CONFIG_KERNEL_LZMA CONFIG_KERNEL_LZO CONFIG_KERNEL_XZ \
         CONFIG_KERNEL_ZSTD CONFIG_KERNEL_UNCOMPRESSED CONFIG_GCC_PLUGINS; do kapat $o; done
ayarla CONFIG_BLK_DEV_INITRD y
ayarla CONFIG_DEVTMPFS y
ayarla CONFIG_DEVTMPFS_MOUNT y

CC="x86_64-buildroot-linux-gnu-gcc"
LD="x86_64-buildroot-linux-gnu-ld"
( cd "$KDIR" && PATH="$BR/host/bin:$PATH" srctree=. ARCH=x86_64 SRCARCH=x86 \
    KERNELVERSION=6.6.32 CC="$CC" LD="$LD" HOSTCC=gcc \
    CC_VERSION_TEXT="$(PATH="$BR/host/bin:$PATH" $CC --version 2>/dev/null | head -1)" \
    KCONFIG_CONFIG="$TMP/c" ./scripts/kconfig/conf --olddefconfig Kconfig \
    >"$TMP/kconf.log" 2>&1 ) || { skip "kconfig cozumlemesi calistirilamadi"; exit 0; }

python3 - "$FRAG" "$TMP/c" "$DEFC" "$KDIR" "$BR" <<'PYEOF'
import io, os, re, sys
frag_p, res_p, defc_p, kdir, br = sys.argv[1:6]
fails = 0
def ok(m):   print('  \033[32mOK\033[0m   ' + m)
def hata(m):
    global fails; fails += 1; print('  \033[31mHATA\033[0m ' + m)
def uyari(m): print('  \033[33mUYARI\033[0m ' + m)
def bilgi(m): print('  BILGI ' + m)

# ── Fragman: YALNIZCA gerçek atama satırları ──────────────────────────────
# Yorum satırları (# ile başlayan; "# CONFIG_X is not set" hariç) ayıklanır:
# dosyada "# CONFIG_DRM_I915=y  <- ..." gibi açıklama satırları var ve
# onlar istek DEĞİL.
want, where = {}, {}
for n, line in enumerate(io.open(frag_p, encoding='utf-8'), 1):
    s = line.strip()
    m = re.match(r'^(CONFIG_[A-Za-z0-9_]+)=(.*)$', s)
    if m:
        k, v = m.group(1), m.group(2)
    else:
        m = re.match(r'^# (CONFIG_[A-Za-z0-9_]+) is not set$', s)
        if not m:
            continue
        k, v = m.group(1), 'n'
    if k in want and want[k] != v:
        hata('CELISEN ATAMA: %s satir %d "%s", satir %d "%s" - kconfig SONUNCUYU alir'
             % (k, where[k], want[k], n, v))
    want[k], where[k] = v, n

res = {}
for line in io.open(res_p, encoding='utf-8'):
    m = re.match(r'^(CONFIG_[A-Za-z0-9_]+)=(.*)$', line.strip())
    if m:
        res[m.group(1)] = m.group(2)
def on(k): return res.get(k) in ('y', 'm')

# ── Aileler ───────────────────────────────────────────────────────────────
AILE = [
    ('Ekran (GPU/DRM/FB)', r'^CONFIG_(DRM|FB|SYSFB|FRAMEBUFFER|LOGO|VGA_CONSOLE)'),
    ('Touchpad/girdi', r'^CONFIG_(PINCTRL|GPIO|I2C|MFD_INTEL_LPSS|X86_INTEL_LPSS|X86_AMD_PLATFORM|COMMON_CLK|ACPI_I2C|HID|MOUSE|RMI4|SERIO|INPUT|KEYBOARD|USB_HID)'),
    ('Ses', r'^CONFIG_(SND|SOUND)'),
    ('Wi-Fi', r'^CONFIG_(WLAN|IWL|RTW|RTL8|RTLWIFI|RTL8XXXU|ATH|CARL|MT7|MT76|RT2|RT28|B43|BRCM|CFG80211|MAC80211|WIRELESS|RFKILL|CRYPTO_MICHAEL)'),
    ('Kablolu ag', r'^CONFIG_(NET|ETHERNET|E1000|IGB|IGC|R8169|ALX|ATL1|TIGON3|BNX2|SKY2|AQTION|FORCEDETH|VIRTIO_NET|USB_NET|USB_USBNET|USB_RTL8152|HYPERV_NET)'),
    ('Depolama', r'^CONFIG_(ATA|SATA|PATA|NVME|BLK_DEV|SCSI|MMC|MISC_RTSX|VMD|USB_STORAGE|USB_UAS|FUSION|HYPERV_STORAGE|ISO9660|JOLIET|ZISOFS|EXT4|VFAT|MSDOS|EFI_PARTITION|PARTITION)'),
    ('USB', r'^CONFIG_USB'),
    ('Sanal makine', r'^CONFIG_(HYPERV|VBOX|VIRT|VIRTIO|PARAVIRT|HYPERVISOR)'),
]
def aile(k):
    for ad, rx in AILE:
        if re.match(rx, k):
            return ad
    return 'Cekirdek/diger'

sayac, dusen = {}, {}
for k, v in want.items():
    a = aile(k)
    sayac[a] = sayac.get(a, 0) + 1
    olan = res.get(k, 'n')
    if v == 'n':
        bozuk = olan in ('y', 'm')
    else:
        bozuk = olan != v
    if bozuk:
        dusen.setdefault(a, []).append((k, v, olan))

print('-- aileye gore (istenen / dusen)')
for a in sorted(sayac):
    d = dusen.get(a, [])
    (hata if d else ok)('%-20s %3d istendi, %d dustu' % (a, sayac[a], len(d)))
for a in sorted(dusen):
    for k, v, olan in dusen[a]:
        if v == 'n':
            hata('%s KAPALI istendi ama ACIK (%s) - baska bir sembol seciyor (satir %d)'
                 % (k, olan, where[k]))
        else:
            hata('%s SESSIZCE DUSUYOR - istenen "%s", olan "%s" (satir %d); '
                 'bagimliligi karsilanmiyor, ozellik CALISMAZ' % (k, v, olan, where[k]))

# ── Kritik halkalar ───────────────────────────────────────────────────────
# Genel denetim "istenen düştü mü" sorar; bir satır fragmandan SİLİNİRSE
# hiçbir şey demez. Bu adlar o yüzden açıkça yazılı.
KRITIK = {
    'touchpad (I2C-HID)': 'I2C_HID_ACPI I2C_DESIGNWARE_PLATFORM COMMON_CLK MFD_INTEL_LPSS_PCI '
                          'X86_INTEL_LPSS X86_AMD_PLATFORM_DEVICE PINCTRL_INTEL PINCTRL_AMD '
                          'GPIOLIB HID_MULTITOUCH HID_GENERIC INPUT_EVDEV',
    'touchpad (PS/2)': 'SERIO_I8042 MOUSE_PS2 MOUSE_PS2_SYNAPTICS MOUSE_PS2_ELANTECH '
                       'MOUSE_PS2_ALPS MOUSE_PS2_TRACKPOINT MOUSE_PS2_VMMOUSE',
    'ekran': 'DRM_SIMPLEDRM DRM_FBDEV_EMULATION DRM_I915 DRM_AMDGPU DRM_AMD_DC DRM_RADEON '
             'DRM_NOUVEAU DRM_BOCHS DRM_VIRTIO_GPU FW_LOADER',
    'depolama': 'BLK_DEV_NVME VMD SATA_AHCI USB_STORAGE USB_UAS MMC_SDHCI_PCI MMC_BLOCK',
    'ses': 'SND_HDA_INTEL SND_HDA_GENERIC SND_HDA_CODEC_REALTEK SND_ENS1370 SND_ENS1371',
}
print('-- kritik halkalar')
for grup, adlar in KRITIK.items():
    eksik = [a for a in adlar.split() if res.get('CONFIG_' + a) != 'y']
    if eksik:
        for a in eksik:
            hata('CONFIG_%s KAPALI - %s calismaz' % (a, grup))
    else:
        ok('%s: %d halka acik' % (grup, len(adlar.split())))

# vmwgfx: VMware ve VirtualBox VMSVGA'da çalışırken çözünürlük/tazeleme
# değişiminin TEK yolu. Kara ekran yalnızca QEMU'nun eksik -vga vmware
# öykünmesinde ölçüldü (PITCHLOCK/DISPLAY_TOPOLOGY bildirmiyor); gerçek VMware
# ikisini de bildiriyor. Kapanırsa kullanıcının test ettiği VMware'de mod
# listesi tek elemanlı kalır.
if not on('CONFIG_DRM_VMWGFX'):
    hata('CONFIG_DRM_VMWGFX KAPALI - VMware/VirtualBox\'ta calisirken cozunurluk '
         'degisemez (kernel.config GPU bolumu)')
# 32 bit program desteği: Ventoy normal kipte enjekte ettiği kancayı 32 bitlik
# bir kabukla çalıştırıyor. Kapalıyken "Failed to execute /init (error -8)"
# -> "No working init found" paniği (Ventoy 1.1.17 ile QEMU'da ölçüldü).
if not on('CONFIG_IA32_EMULATION'):
    hata('CONFIG_IA32_EMULATION KAPALI - Ventoy normal kipte kernel panic verir')
for s in ('CONFIG_EXFAT_FS', 'CONFIG_NTFS3_FS'):
    if not on(s):
        hata(s + ' KAPALI - Ventoy bolumu baglanamaz, Ventoy kaliciligi (mcos-vtoydata) calismaz')
# Turbo: gerçek PC'de "4,4 GHz yerine 2,4 GHz, fan dönmüyor" şikâyetinin
# çekirdek tarafı. Bunlar kapalıyken PL1 yükseltilemez, AMD'de frekans
# sürücüsü olmaz, dizüstü/masaüstü fanına yol kalmaz, tanı ölçemez
# (kernel.config "Turbo" bölümü). Menü sembolü (X86_PLATFORM_DRIVERS_HP)
# silinirse HP_WMI SESSİZCE düşer — tam da bu testin yakaladığı tuzak.
turbo = {
    'CONFIG_POWERCAP': 'PL1/PL2 okunamaz', 'CONFIG_INTEL_RAPL': 'PL1 yukseltilemez',
    'CONFIG_PROC_THERMAL_MMIO_RAPL': 'dizustu MMIO PL1 yukseltilemez',
    'CONFIG_X86_AMD_PSTATE': 'AMD\'de frekans surucusu yok', 'CONFIG_X86_ACPI_CPUFREQ': 'CPPC\'siz AMD\'de frekans surucusu yok',
    'CONFIG_INTEL_IDLE': 'tek cekirdek turbosu icin derin uyku yok', 'CONFIG_X86_MSR': 'turbo tanisi olcemez',
    'CONFIG_SENSORS_CORETEMP': 'Intel sicakligi yok', 'CONFIG_SENSORS_K10TEMP': 'AMD sicakligi yok',
    'CONFIG_THINKPAD_ACPI': 'ThinkPad fani surulemez', 'CONFIG_SENSORS_DELL_SMM': 'Dell fani surulemez',
    'CONFIG_HP_WMI': 'HP fani surulemez', 'CONFIG_ASUS_WMI': 'ASUS fani surulemez',
    'CONFIG_IDEAPAD_LAPTOP': 'IdeaPad fani/profili yok', 'CONFIG_SENSORS_NCT6775': 'masaustu Nuvoton fani yok',
    'CONFIG_SENSORS_IT87': 'masaustu ITE fani yok',
}
eksik = [k for k in turbo if not on(k)]
for k in eksik:
    hata(k + ' KAPALI - turbo: ' + turbo[k])
if not eksik:
    ok('turbo: %d cekirdek secenegi acik (RAPL, frekans, sicaklik, fan, MSR)' % len(turbo))
# Sunucu performansı (kernel.config "Minecraft sunucu performansı" bölümü):
# kullanıcı SD karta kurulu sistemde "10 saniyedir yanıt yok" donmaları ve
# genel yavaşlık bildirdi. Bunlar sessizce düşerse o sorunlar geri gelir.
perf = {
    'CONFIG_NO_HZ_IDLE': 'bosta cekirdekler uyumaz, turbo payi azalir',
    'CONFIG_PREEMPT_VOLUNTARY': 'uzun cekirdek isleri ana is parcacigini bekletir',
    'CONFIG_TRANSPARENT_HUGEPAGE': 'JVM buyuk sayfa kullanamaz',
    'CONFIG_MQ_IOSCHED_DEADLINE': 'SD kartta okumalar yazmalarin arkasinda bekler',
    'CONFIG_BLK_WBT': 'yazma geri donusu okuma gecikmesini bogar',
    'CONFIG_ZRAM': 'RAM deki kok dosya sistemi sikistirilamaz',
    'CONFIG_ZRAM_DEF_COMP_LZ4': 'zram yavas sikistirici kullanir',
}
peksik = [k for k in perf if not on(k)]
for k in peksik:
    hata(k + ' KAPALI - performans: ' + perf[k])
if not peksik:
    ok('performans: %d cekirdek secenegi acik (NO_HZ, THP, G/C, zram)' % len(perf))
# SOF firmware'siz: intel-dsp-config makineyi SOF'a yönlendirir, SOF düşer,
# bugün eski HDA yoluyla çalışan hoparlör de susar.
if on('CONFIG_SND_SOC_SOF_TOPLEVEL') or on('CONFIG_SND_SOC_SOF_PCI'):
    hata('SOF ACIK ama Buildroot 2024.02 SOF firmware paketi vermiyor - DSP\'li '
         'Intel dizustulerde ses TAMAMEN gider')

# ── SMBus / RMI4 birlikteliği ─────────────────────────────────────────────
# SMBus denetleyicisi varken psmouse beyaz listedeki Synaptics touchpad'i
# RMI4'e devreder ve PS/2 tarafını bırakır. F03 yoksa trackpoint, F3A/F30
# yoksa düğmeler, F11/F12 yoksa hareket ölür.
print('-- SMBus/RMI4 birlikteligi')
smbus = [k for k in ('CONFIG_I2C_I801', 'CONFIG_I2C_PIIX4') if on(k)]
if smbus and (on('CONFIG_MOUSE_PS2_SYNAPTICS_SMBUS') or on('CONFIG_RMI4_SMB')):
    gerek = ['CONFIG_RMI4_CORE', 'CONFIG_RMI4_SMB', 'CONFIG_RMI4_I2C', 'CONFIG_RMI4_F03',
             'CONFIG_RMI4_F03_SERIO', 'CONFIG_RMI4_F3A', 'CONFIG_RMI4_F11',
             'CONFIG_RMI4_F12', 'CONFIG_RMI4_F30', 'CONFIG_HID_RMI', 'CONFIG_I2C_SMBUS']
    eksik = [k for k in gerek if not on(k)]
    if eksik:
        hata('RMI4 BIRLIKTELIGI BOZUK: %s acik ama %s kapali - Synaptics touchpad '
             'dugmeleri/trackpoint OLUR' % (' '.join(smbus), ' '.join(eksik)))
    else:
        ok('%s acik ve RMI4 zinciri tam (%d sembol)' % (' + '.join(smbus), len(gerek)))
else:
    bilgi('SMBus denetleyicisi kapali - RMI4 devri devre disi')

# ── Firmware eşleşmesi ────────────────────────────────────────────────────
# Sürücü açık + firmware paketi kapalı = kart görünür ama çalışmaz. GPU'da
# daha kötüsü: devraldıktan SONRA düşer ve KARA EKRAN bırakır.
defc = set()
for line in io.open(defc_p, encoding='utf-8'):
    m = re.match(r'^(BR2_[A-Za-z0-9_]+)=y\s*$', line.strip())
    if m:
        defc.add(m.group(1))
P = 'BR2_PACKAGE_LINUX_FIRMWARE_'
FW = [
    # (sürücü, [paketlerden biri], düzey, not)
    ('DRM_AMDGPU', ['AMDGPU'], 'zorunlu', 'yoksa SI/CIK/VI (Polaris) KARA EKRAN'),
    ('DRM_RADEON', ['RADEON'], 'zorunlu', 'devraldiktan sonra microcode ister: KARA EKRAN'),
    ('DRM_I915', ['I915'], 'istege', 'ekran calisir; GuC/HuC/DMC yok (GPU wedged, guc tasarrufu kapali)'),
    ('IWLMVM', ['IWLWIFI_9XXX', 'IWLWIFI_22000', 'IWLWIFI_QUZ', 'IWLWIFI_6E'], 'zorunlu', 'Intel Wi-Fi'),
    ('IWLDVM', ['IWLWIFI_5000', 'IWLWIFI_6000G2A', 'IWLWIFI_6000G2B'], 'zorunlu', 'eski Intel Wi-Fi'),
    ('RTW88_CORE', ['RTL_RTW88'], 'zorunlu', 'Realtek rtw88'),
    ('RTW89_CORE', ['RTL_RTW89'], 'zorunlu', 'Realtek rtw89'),
    ('RTLWIFI', ['RTL_81XX', 'RTL_87XX', 'RTL_88XX'], 'zorunlu', 'Realtek rtlwifi'),
    ('ATH10K_PCI', ['ATHEROS_10K_QCA9377', 'QUALCOMM_6174'], 'zorunlu', 'ath10k'),
    ('ATH9K_HTC', ['ATHEROS_7010', 'ATHEROS_9271'], 'zorunlu', 'ath9k USB'),
    ('CARL9170', ['ATHEROS_9170'], 'zorunlu', 'carl9170'),
    ('MT7921E', ['MEDIATEK_MT7921', 'MEDIATEK_MT7922'], 'zorunlu', 'MediaTek MT792x'),
    ('MT76x2E', ['MEDIATEK_MT76X2E'], 'zorunlu', 'MediaTek MT76x2'),
    ('MT7601U', ['MEDIATEK_MT7601U'], 'zorunlu', 'MediaTek MT7601U'),
    ('MT76x0E', ['MEDIATEK_MT7610E'], 'zorunlu', 'MediaTek MT76x0E'),
    ('RT2800PCI', ['RALINK_RT2XX'], 'zorunlu', 'Ralink'),
    ('BRCMFMAC', ['BRCM_BCM43XXX'], 'zorunlu', 'Broadcom FullMAC'),
    ('BRCMSMAC', ['BRCM_BCM43XX'], 'zorunlu', 'Broadcom SoftMAC'),
    ('BNX2', ['BNX2'], 'zorunlu', 'Broadcom NetXtreme II'),
    ('R8169', ['RTL_8169'], 'zorunlu', 'Realtek 8168/8111 yamalari'),
    ('USB_RTL8152', ['RTL_815X'], 'istege', 'RTL8153/8156 yamalari'),
    ('TIGON3', ['BROADCOM_TIGON3'], 'istege', 'tg3 5701/57766'),
    # Buildroot 2024.02'de paketi OLMAYANLAR: yapılandırmayla çözülemez.
    ('ATH11K_PCI', [], 'yok', 'ath11k firmware Buildroot 2024.02 linux-firmware\'da secenek degil'),
    ('B43', [], 'yok', 'b43 firmware ayri (b43-firmware) paket, secili degil'),
    ('DRM_NOUVEAU', [], 'yok', 'nvidia firmware paketi yok; goruntu calisir, hizlandirma yok'),
]
print('-- firmware eslesmesi (surucu acik -> paket acik mi)')
for drv, pk, duzey, notu in FW:
    if not on('CONFIG_' + drv):
        continue
    var = [p for p in pk if P + p in defc]
    if var:
        ok('CONFIG_%s -> %s' % (drv, ', '.join(var)))
    elif duzey == 'zorunlu':
        hata('CONFIG_%s acik ama firmware paketi yok (%s) - %s'
             % (drv, ' | '.join(P + p for p in pk), notu))
    elif duzey == 'istege':
        bilgi('CONFIG_%s: firmware paketi bilerek yok - %s' % (drv, notu))
    else:
        uyari('CONFIG_%s: %s' % (drv, notu))

# ── Derlenmiş imajla çapraz denetim (yalnızca 'make os' sonrası anlamlı) ──
# modules.builtin.modinfo, gömülü her sürücünün istediği firmware adlarını
# listeler. GPU'larda HİÇBİRİ imajda yoksa bu, defconfig ile derlenmiş imaj
# arasında kopukluk demektir (örn. çekirdek yeniden derlendi ama rootfs değil).
mi = os.path.join(kdir, 'modules.builtin.modinfo')
tfw = os.path.join(br, 'target', 'lib', 'firmware')
if os.path.isfile(mi) and os.path.isdir(tfw):
    istek = {}
    for ent in open(mi, 'rb').read().split(b'\0'):
        m = re.match(rb'^([a-z0-9_]+)\.firmware=(.+)$', ent)
        if m:
            istek.setdefault(m.group(1).decode(), []).append(m.group(2).decode())
    for mod in ('amdgpu', 'radeon'):
        if mod not in istek:
            continue
        var = sum(1 for f in istek[mod] if os.path.exists(os.path.join(tfw, f)))
        if var == 0:
            hata('%s gomulu ama derlenmis imajda firmware\'i YOK (%d dosyanin 0\'i) - '
                 'rootfs yeniden uretilmeli' % (mod, len(istek[mod])))
        else:
            ok('%s: derlenmis imajda %d/%d firmware dosyasi' % (mod, var, len(istek[mod])))

print()
if fails:
    print('\033[31m%d cekirdek yapilandirma sorunu\033[0m' % fails)
    sys.exit(1)
print('\033[32mcekirdek yapilandirmasi tutarli\033[0m')
PYEOF
