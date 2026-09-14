#!/bin/sh
# test-vbox-compat.sh — VirtualBox (ve genel sanal makine) uyumluluğu testi.
#
# ── NE DOĞRULUYOR ───────────────────────────────────────────────────────────
# Kullanıcı "virtualbox uyumlu yap, açılsın" dedi. Aşağıdaki kontrollerin
# hepsi, kaynak ağacında ÖLÇÜLEREK bulunmuş gerçek eksiklere karşılık gelir —
# hiçbiri varsayım değil.
#
# Hiçbir şey derlemez, hiçbir makine başlatmaz: yalnızca yapılandırmayı okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
KCFG="$ROOT/os/buildroot/external/board/mcos/kernel.config"
MK="$ROOT/Makefile"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }
note() { printf '  \033[33mNOT\033[0m  %s\n' "$1"; }

for f in "$KCFG" "$MK"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

has() { grep -q "^$1=y" "$KCFG"; }

echo "== 1. Ekran: /dev/fb0 GERÇEKTEN oluşmalı =="

# Panel /dev/fb0 olmadan hiç açılmaz (mcos-launch has_framebuffer kontrolü).
if has CONFIG_FB_DEVICE; then
    pass "CONFIG_FB_DEVICE sabitlendi (/dev/fb0 düğümü)"
else
    fail "CONFIG_FB_DEVICE yok — /dev/fb0 şansa kalıyor, panel açılmayabilir"
fi

# vmwgfx, VirtualBox'ta firmware framebuffer'ını YOK EDER: probe'un ilk işi
# fb'yi devre dışı bırakmak, uyumluluk kontrolü SONRA geliyor ve VBox'ta
# her zaman başarısız oluyor. Sonuç: fb yok, panel yok.
if grep -q '^CONFIG_DRM_VMWGFX=y' "$KCFG"; then
    fail "CONFIG_DRM_VMWGFX açık — VirtualBox VMSVGA'da /dev/fb0 kaybolur"
else
    pass "vmwgfx kapalı (firmware framebuffer korunuyor)"
fi

if has CONFIG_DRM_SIMPLEDRM; then
    pass "simpledrm açık — firmware framebuffer'ını devralır"
else
    fail "simpledrm yok — firmware framebuffer sürücüsüz kalır"
fi

echo "== 2. Hypervisor konuk desteği =="

# Bu kapalıyken hypervisor_is_type() her zaman "native" döner ve
# hypervisor'e özgü sürücüler yanlış karar verir.
if has CONFIG_HYPERVISOR_GUEST; then
    pass "CONFIG_HYPERVISOR_GUEST açık"
else
    fail "CONFIG_HYPERVISOR_GUEST kapalı — sanal makine tespiti çalışmaz"
fi

# VBOXGUEST tek başına yetmez: VIRT_DRIVERS bloğunun içindedir.
if has CONFIG_VBOXGUEST; then
    if has CONFIG_VIRT_DRIVERS; then
        pass "VBOXGUEST + VIRT_DRIVERS birlikte açık"
    else
        fail "VBOXGUEST var ama VIRT_DRIVERS yok — satır sessizce atılır"
    fi
else
    note "VBOXGUEST kapalı (zorunlu değil)"
fi

echo "== 3. Depolama: VirtualBox'ın sunduğu her denetleyici =="

for opt in CONFIG_SATA_AHCI CONFIG_ATA_PIIX CONFIG_BLK_DEV_NVME \
           CONFIG_SCSI_BUSLOGIC CONFIG_VIRTIO_BLK CONFIG_USB_STORAGE; do
    if has "$opt"; then
        pass "$opt"
    else
        fail "$opt yok"
    fi
done

# Bunlar EKSİKTİ: kullanıcı LsiLogic veya virtio-scsi seçerse disk görünmüyordu.
for opt in CONFIG_SCSI_SYM53C8XX_2 CONFIG_FUSION_SAS CONFIG_SCSI_VIRTIO; do
    if has "$opt"; then
        pass "$opt (eskiden eksikti)"
    else
        fail "$opt yok — o denetleyicide disk görünmez"
    fi
done

echo "== 4. Ölü yapılandırma satırı kalmamalı =="

# Bunlar Linux Kconfig'de HİÇ YOK; olddefconfig onları sessizce atar, yani
# "destek var" sanılıp aslında hiçbir şey yapmıyorlardı.
for dead in CONFIG_SCSI_LSILOGIC CONFIG_MAC80211_USB CONFIG_MT76 CONFIG_MT7922 \
            CONFIG_FB_SIMPLE; do
    if grep -q "^$dead=y" "$KCFG"; then
        fail "$dead hâlâ etkin — böyle bir Kconfig sembolü YOK (sessizce atılır)"
    else
        pass "$dead temizlendi"
    fi
done

echo "== 5. Saat ve PCI =="

# RTC olmadan duvar saati her açılışta 1970'te başlar: TLS doğrulaması
# bozulur (indirmeler başarısız) ve günlük zaman damgaları anlamsızlaşır.
if has CONFIG_RTC_DRV_CMOS && has CONFIG_RTC_HCTOSYS; then
    pass "gerçek zaman saati açık (TLS ve günlükler için şart)"
else
    fail "RTC yok — saat 1970'te başlar, TLS doğrulaması bozulur"
fi

if has CONFIG_PCI_MSI; then
    pass "CONFIG_PCI_MSI açık"
else
    fail "CONFIG_PCI_MSI yok — tüm aygıtlar eski INTx kesmesine düşer"
fi

echo "== 6. ISO üretimi =="

# grub-mkrescue, "--" öncesindeki her argümanı ISO KÖKÜNE eklenecek kaynak
# dizin sayar. "$GRUB_MODS" geçmek, ISO köküne 291 başıboş .MOD dosyası
# ekliyor ve "EFI" dizin adını "EFI0/"+"EFI1/" olarak BOZUYORDU.
if grep -q 'grub-mkrescue.*GRUB_MODS' "$MK"; then
    fail "grub-mkrescue'ya GRUB_MODS geçiliyor — ISO kökü kirlenir, EFI dizini bozulur"
else
    pass "grub-mkrescue temiz çağrılıyor"
fi

# VirtualBox EFI'si sık sık UEFI kabuğuna düşer; startup.nsh onu kurtarır.
if grep -q 'startup.nsh' "$MK"; then
    pass "startup.nsh yazılıyor (VBox EFI kabuğundan kurtarma)"
else
    fail "startup.nsh yok — VBox EFI kabuğa düşerse kurtuluş yok"
fi

if grep -q 'joliet on' "$MK"; then
    pass "Joliet açık (firmware kabuğu gerçek dosya adlarını görür)"
else
    fail "Joliet yok — kabukta yalnızca bozuk 8.3 adlar görünür"
fi

# xorriso SEÇENEK ADLARI — bu derlemeyi bir kez kırdı.
#
# grub-mkrescue'da "--" sonrası xorriso YEREL kipte çalışır, mkisofs
# öykünmesinde DEĞİL. "-rational-rock" mkisofs adıdır ve yerel kipte
# geçersizdir; sonuç:
#   xorriso : FAILURE : Not a known command: '-rational-rock'
# Yerel karşılığı "-rockridge on".
# Yalnızca ÇALIŞAN kodu denetle: yorumlar bu seçenek adını açıklamak için
# anmak zorunda ve yanlış pozitif vermemeli.
if sed 's/[[:space:]]*#.*$//' "$MK" | grep -q 'rational-rock'; then
    fail "xorriso'ya -rational-rock geçiliyor — yerel kipte geçersiz, ISO üretimi kırılır"
else
    pass "xorriso seçenekleri yerel kip adlarıyla yazılmış"
fi

# Seçenekleri GERÇEKTEN dene: ad değişiklikleri sessizce kırar.
if command -v xorriso >/dev/null 2>&1; then
    opts="$(sed -n 's/.*-- -volid MCOS \(.*\) && .*/\1/p' "$MK" | head -1)"
    if [ -n "$opts" ]; then
        bad=""
        for tok in $opts; do
            case "$tok" in
                -*)
                    xorriso -help 2>&1 | grep -q -- "$tok " || bad="$bad $tok"
                    ;;
            esac
        done
        if [ -z "$bad" ]; then
            pass "xorriso seçenekleri bu sürümde tanınıyor"
        else
            fail "xorriso bu seçenekleri TANIMIYOR:$bad"
        fi
    fi
fi

echo "== 7. Bellek uyarısı =="

# initramfs sıkıştırılmış ~128 MiB, açılmış ~286 MiB. Tepe kullanım init
# çalışmadan ÖNCE ~414 MiB. VirtualBox'ın "Other Linux (64-bit)" varsayılanı
# 512 MB'dir ve bu "Initramfs unpacking failed" ile ÖLÜR.
#
# Bu bir kod hatası değil, kullanıcının bilmesi gereken bir gereksinim —
# README ve GECIS.md'de yazılı olmalı.
if grep -rq '2048 MB\|2 GB\|2048MB' "$ROOT/README.md" 2>/dev/null; then
    pass "bellek gereksinimi belgelenmiş"
else
    fail "README'de asgari bellek (2048 MB) yazmıyor — VBox varsayılanı 512 MB ve ÖLÜR"
fi

echo
if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mVirtualBox uyumluluğu doğrulandı\033[0m\n'
