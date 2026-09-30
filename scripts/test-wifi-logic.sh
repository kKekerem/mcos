#!/bin/sh
# test-wifi-logic.sh — "Wi-Fi çalışıyor ama ağları görmüyor" hatasının testi.
#
# ── BULUNAN HATA ────────────────────────────────────────────────────────────
# Kablosuz düzenleyici veritabanı (wireless-regdb) imajda HİÇ YOKTU.
#
# Çekirdek CONFIG_CFG80211_REQUIRE_SIGNED_REGDB=y ile derleniyor, yani
# cfg80211 /lib/firmware/regulatory.db ve regulatory.db.p7s dosyalarını
# yüklemek ZORUNDA. Yoksa hiçbir düzenleyici alan yüklenemez ve çekirdek
# gömülü "00" (dünya dolaşımı) alanına düşer.
#
# "00" alanında kanalların çoğu NO-IR işaretlidir: kart aktif tarama yapamaz,
# yalnızca pasif dinler. 5 GHz'in tamamı bu durumdadır. Belirti tam olarak
# kullanıcının bildirdiğidir: Wi-Fi kartı çalışıyor, arayüz UP oluyor, ama
# tarama boş dönüyor.
#
# Hiçbir donanıma dokunmaz; yalnızca yapılandırmayı ve kodu okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DEFCONFIG="$ROOT/os/buildroot/external/configs/mcos_defconfig"
KCFG="$ROOT/os/buildroot/external/board/mcos/kernel.config"
NETCFG="$ROOT/internal/netcfg/netcfg_linux.go"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }
note() { printf '  \033[33mNOT\033[0m  %s\n' "$1"; }

for f in "$DEFCONFIG" "$KCFG" "$NETCFG"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

echo "== 1. Düzenleyici veritabanı (ASIL HATA) =="

if grep -q '^BR2_PACKAGE_WIRELESS_REGDB=y' "$DEFCONFIG"; then
    pass "wireless-regdb paketi etkin"
else
    fail "wireless-regdb YOK — çekirdek 00 alanına düşer, tarama boş döner"
fi

# Derlenmiş bir rootfs varsa dosyaların GERÇEKTEN kurulduğunu doğrula.
TGT="$ROOT/os/buildroot/output/target"
if [ -d "$TGT/lib/firmware" ]; then
    miss=""
    for f in regulatory.db regulatory.db.p7s; do
        [ -f "$TGT/lib/firmware/$f" ] || miss="$miss $f"
    done
    if [ -z "$miss" ]; then
        pass "regulatory.db ve imzası rootfs'te mevcut"
    elif [ "$DEFCONFIG" -nt "$TGT/lib/firmware" ]; then
        # Yapılandırma rootfs'ten YENİ: bu eski bir derleme, hata değil.
        # Ayrımı yapmazsak her yapılandırma değişikliği testi kırar ve
        # gerçek bir eksikliği fark edemez hale geliriz.
        note "rootfs eski (defconfig daha yeni) — 'make os' ile yeniden derleyin"
    else
        fail "rootfs'te eksik:$miss — çekirdek 00 alanına düşer, tarama boş döner"
    fi
else
    note "derlenmiş rootfs yok; yalnızca yapılandırma denetlendi"
fi

echo "== 2. Kablosuz araçlar =="

for p in BR2_PACKAGE_IW BR2_PACKAGE_WIRELESS_TOOLS BR2_PACKAGE_WPA_SUPPLICANT; do
    if grep -q "^$p=y" "$DEFCONFIG"; then
        pass "$p"
    else
        fail "$p yok — tarama yöntemlerinden biri çalışmaz"
    fi
done

# nl80211 arka ucu şart: wext eski ve çoğu modern sürücüde eksik.
if grep -q '^BR2_PACKAGE_WPA_SUPPLICANT_NL80211=y' "$DEFCONFIG"; then
    pass "wpa_supplicant nl80211 arka ucu etkin"
else
    fail "nl80211 arka ucu yok — modern kartlarda tarama başarısız olur"
fi

echo "== 3. Çekirdek kablosuz yığını =="

for opt in CONFIG_CFG80211 CONFIG_MAC80211 CONFIG_WLAN CONFIG_FW_LOADER; do
    if grep -q "^$opt=y" "$KCFG"; then
        pass "$opt"
    else
        fail "$opt kapalı"
    fi
done

# Sürücüler firmware'i çalışma anında yükler; yol yanlışsa kart hiç açılmaz.
if grep -q '^BR2_PACKAGE_LINUX_FIRMWARE=y' "$DEFCONFIG"; then
    pass "linux-firmware etkin"
else
    fail "linux-firmware yok — çoğu kart hiç başlamaz"
fi

echo "== 4. Tarama kodu sağlam mı =="

code() { sed 's|//.*$||' "$NETCFG"; }

# rfkill: kart yazılımdan engellenmişse tarama sessizce boş döner.
if code | grep -q 'rfkill'; then
    pass "tarama öncesi rfkill kaldırılıyor"
else
    fail "rfkill kaldırılmıyor — engelli kartta tarama sessizce boş döner"
fi

# Arayüz UP olmadan tarama yapılamaz.
if code | grep -q '"ip", "link", "set"'; then
    pass "arayüz taramadan önce UP yapılıyor"
else
    fail "arayüz UP yapılmıyor"
fi

# Tek bir yönteme güvenmek kırılgan: kartlar farklı arayüzleri destekler.
n=0
for m in iwlist wpa_cli 'iw", "dev'; do
    code | grep -q "$m" && n=$((n + 1))
done
if [ "$n" -ge 3 ]; then
    pass "üç ayrı tarama yöntemi deneniyor (iwlist / wpa_cli / iw)"
else
    fail "yalnızca $n tarama yöntemi var — bazı kartlarda hiç sonuç alınmaz"
fi

# Düzenleyici alanın UYGULANMASI: veritabanı olsa bile ülke ayarlanmazsa
# kart yine kısıtlı kalır.
if code | grep -q 'SetRegulatoryDomain\|"iw", "reg", "set"'; then
    pass "düzenleyici alan açıkça ayarlanıyor"
else
    fail "düzenleyici alan ayarlanmıyor — veritabanı yüklense de kanallar kısıtlı kalır"
fi

echo
if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mkablosuz yapılandırması doğrulandı\033[0m\n'
