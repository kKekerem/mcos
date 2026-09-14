#!/bin/sh
# test-flash-logic.sh — masaüstü flaşlama aracının GÜVENLİK değişmezleri.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN BU TEST VAR
# ════════════════════════════════════════════════════════════════════════════
#
# mcos-flash bir diski geri dönüşsüz siler. Tek bir gerileme (regression) —
# sistem diskini listeye sokmak, onayı atlamak, boyut denetimini kaldırmak —
# kullanıcının Windows kurulumunu yok eder.
#
# Bu yüzden güvenlik davranışları KODDAN okunarak doğrulanır. Hiçbir aygıta
# dokunulmaz, hiçbir şey yazılmaz.
#
# Yazma yolunun kendisi (özet, doğrulama, bozuk bellek tespiti) Go testinde
# sahte bir aygıtla sınanır: internal/flash/write_test.go

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DEV="$ROOT/internal/flash/device.go"
ENUM_L="$ROOT/internal/flash/enum_linux.go"
ENUM_W="$ROOT/internal/flash/enum_windows.go"
WRITE="$ROOT/internal/flash/write.go"
MAIN="$ROOT/cmd/mcos-flash/main.go"
# Baslaticilarin KAYNAGI burada durur, dist/ icinde degil: dist/ butunuyle
# .gitignore'dadir, yani oradaki kopyalar temiz bir klonda YOKTUR. Bu test
# eskiden oraya bakiyordu ve dosyalari bulamayinca "eksik dosya" deyip
# cikiyordu — yani klonlanmis bir depoda hic calismiyordu.
LAUNCHER_DIR="$ROOT/cmd/mcos-flash/launcher"
BAT="$LAUNCHER_DIR/mcos-flash.bat"
SH="$LAUNCHER_DIR/mcos-flash.sh"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$DEV" "$ENUM_L" "$ENUM_W" "$WRITE" "$MAIN" "$BAT" "$SH"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

# strip_comments, Go yorumlarını atar.
#
# NEDEN: bir kuralı ARAMAK, o kuralı ANLATAN yorumu bulmakla karıştırılmamalı.
# Yorumda "sistem diski elenir" yazması, kodun gerçekten eleyip elemediğini
# söylemez.
strip_comments() {
    sed 's://.*::' "$1"
}

echo "== 1. Sistem diski HİÇBİR koşulda listelenmez =="

for f in "$ENUM_L" "$ENUM_W"; do
    name="$(basename "$f")"
    if strip_comments "$f" | grep -q 'if d.System {'; then
        if strip_comments "$f" | grep -A2 'if d.System {' | grep -q 'continue'; then
            pass "$name: sistem diski atlanıyor"
        else
            fail "$name: d.System denetleniyor ama atlanmıyor"
        fi
    else
        fail "$name: sistem diski denetimi YOK"
    fi

    # --all-disks yalnızca ÇIKARILABİLİRLİK filtresini gevşetmeli; sistem
    # diski denetimi ondan ÖNCE gelmeli.
    sys_line="$(strip_comments "$f" | grep -n 'if d.System {' | head -1 | cut -d: -f1)"
    # Kalip GEVSEK tutuluyor: filtre satiri "if !includeInternal" ya da
    # "if (!includeInternal || unknownSystem)" bicimindedir. Birebir yazima
    # bagli bir kalip, dogru bir duzeltmeyi hata sanar.
    inc_line="$(strip_comments "$f" | grep -n '!includeInternal' | head -1 | cut -d: -f1)"
    if [ -n "$sys_line" ] && [ -n "$inc_line" ] && [ "$sys_line" -lt "$inc_line" ]; then
        pass "$name: sistem denetimi, --all-disks filtresinden önce"
    else
        fail "$name: sistem denetimi --all-disks'ten sonra geliyor (sıra yanlış)"
    fi
done

echo "== 2. Doğrulama, yazma yolunda ZORUNLU =="

if strip_comments "$WRITE" | grep -q 'if err := Validate(w.Device); err != nil'; then
    pass "Writer.Write, Validate'i çağırıyor"
else
    fail "Writer.Write, Validate'i ÇAĞIRMIYOR — arayüz atlanırsa koruma kalmaz"
fi

if strip_comments "$DEV" | grep -q 'MinSizeBytes'; then
    pass "en küçük aygıt boyutu tanımlı"
else
    fail "MinSizeBytes yok"
fi

if strip_comments "$WRITE" | grep -q 'dropCachesFn(dst)'; then
    pass "doğrulama önce önbelleği boşaltıyor"
else
    fail "doğrulama önbelleği boşaltmıyor — bozuk bellek 'sağlam' görünür"
fi

if strip_comments "$WRITE" | grep -q 'readSum != sum'; then
    pass "geri okunan özet, yazılan özetle karşılaştırılıyor"
else
    fail "özet karşılaştırması yok"
fi

echo "== 3. Deneme kipi hiçbir aygıt açmaz =="

# DryRun denetimi, openDeviceFn çağrısından ÖNCE olmalı.
dry_line="$(strip_comments "$WRITE" | grep -n 'if w.DryRun {' | head -1 | cut -d: -f1)"
open_line="$(strip_comments "$WRITE" | grep -n 'openDeviceFn(' | head -1 | cut -d: -f1)"
if [ -n "$dry_line" ] && [ -n "$open_line" ] && [ "$dry_line" -lt "$open_line" ]; then
    pass "DryRun denetimi, aygıt açılmadan önce"
else
    fail "DryRun denetimi aygıt açıldıktan SONRA — deneme kipi yazabilir"
fi

echo "== 4. Onay: kullanıcı aygıt yolunu ELLE yazar =="

if strip_comments "$MAIN" | grep -q 'strings.TrimSpace(line) != d.Path'; then
    pass "onay, yazılan yolun aygıtla eşleşmesini istiyor"
else
    fail "onay yol eşleşmesi istemiyor — 'e/h' sorusu yeterli koruma değil"
fi

if strip_comments "$MAIN" | grep -q 'func reverify('; then
    pass "yazmadan hemen önce aygıt yeniden doğrulanıyor"
else
    fail "aygıt yeniden doğrulanmıyor — kullanıcı belleği değiştirmiş olabilir"
fi

echo "== 5. Sunucu yok, başlatıcılar var =="

if strip_comments "$MAIN" | grep -qE 'net/http|http\.ListenAndServe'; then
    fail "mcos-flash hâlâ bir HTTP sunucusu içeriyor (sunucusuz olmalıydı)"
else
    pass "HTTP sunucusu yok"
fi

if [ -f "$ROOT/cmd/mcos-flash/ui.html" ]; then
    fail "eski tarayıcı arayüzü (ui.html) hâlâ duruyor"
else
    pass "tarayıcı arayüzü kaldırılmış"
fi

if grep -q 'net session' "$BAT" && grep -q 'RunAs' "$BAT"; then
    pass ".bat yönetici hakkını kendisi istiyor"
else
    fail ".bat yönetici yükseltmesi yapmıyor"
fi

if grep -q 'id -u' "$SH" && grep -q 'sudo' "$SH"; then
    pass ".sh kök hakkını kendisi istiyor"
else
    fail ".sh kök yükseltmesi yapmıyor"
fi

if [ -x "$SH" ]; then
    pass ".sh çalıştırılabilir"
else
    fail ".sh çalıştırma izni yok (chmod +x)"
fi

echo "== 6. Windows ham yazma: birimler kilitlenir =="

RAW_W="$ROOT/internal/flash/raw_windows.go"
if strip_comments "$RAW_W" | grep -q 'fsctlLockVolume' &&
   strip_comments "$RAW_W" | grep -q 'fsctlDismountVolume'; then
    pass "birimler kilitleniyor ve ayrılıyor"
else
    fail "birim kilitleme/ayırma yok — Windows yazmayı reddeder"
fi

# Kilit, disk tanıtıcısı açılmadan ÖNCE alınmalı.
lock_line="$(strip_comments "$RAW_W" | grep -n 'lockVolume(letter)' | head -1 | cut -d: -f1)"
crt_line="$(strip_comments "$RAW_W" | grep -n 'windows.CreateFile(p,' | head -1 | cut -d: -f1)"
if [ -n "$lock_line" ] && [ -n "$crt_line" ] && [ "$lock_line" -lt "$crt_line" ]; then
    pass "kilit, disk açılmadan önce alınıyor"
else
    fail "disk, birimler kilitlenmeden açılıyor (yarış durumu)"
fi


# ─────────────────────────────────────────────────────────────────────────────
# BAŞLATICILAR GİT'İN GÖREBİLECEĞİ YERDE OLMALI
# ─────────────────────────────────────────────────────────────────────────────
#
# ── Yakalanan gerçek hata ────────────────────────────────────────────────────
# .bat ve .sh başlatıcıları bir süre doğrudan dist/flash/ içinde duruyordu.
# dist/ ise bütünüyle .gitignore'da (ISO ve .img orada üretiliyor), yani
# başlatıcılar HİÇ İZLENMİYORDU. İki sonucu vardı:
#
#   1. Depoyu temiz klonlayan biri "make flash" çalıştırdığında ikiliyi
#      alıyor, ama çift tıklayacağı dosya hiç oluşmuyordu.
#   2. Bu testin kendisi o yola bakıyordu ve dosyayı bulamayınca
#      "eksik dosya" deyip çıkıyordu — make test-boot klonlanmış bir
#      depoda hiç çalışmıyordu.
#
# Dosyalar yerel diskte durduğu için hata GÖRÜNMÜYORDU; yalnızca başka bir
# makinede ortaya çıkardı.
#
# baslatici-izlenebilir
echo ""
echo "başlatıcı konumu"

for f in mcos-flash.sh mcos-flash.bat; do
    rel="cmd/mcos-flash/launcher/$f"
    if git -C "$ROOT" check-ignore -q "$rel" 2>/dev/null; then
        fail "$rel .gitignore tarafından yok sayılıyor — temiz bir klonda başlatıcı oluşmaz"
    else
        pass "$rel git tarafından izlenebilir"
    fi
done

# Makefile başlatıcıları kaynaktan KOPYALAMALI. Yalnızca chmod etmek,
# dosyanın oraya başka bir yoldan geldiğini varsaymak demektir.
if grep -q 'FLASH_LAUNCHER_SRC)/mcos-flash.sh" dist/flash/' "$ROOT/Makefile"; then
    pass "make flash başlatıcıyı kaynaktan kopyalıyor"
else
    fail "make flash başlatıcıyı kopyalamıyor — dist/flash eksik kalır"
fi

if grep -q 'FLASH_LAUNCHER_SRC)/mcos-flash.bat" dist/flash/' "$ROOT/Makefile"; then
    pass "make flash-windows .bat'i kaynaktan kopyalıyor"
else
    fail "make flash-windows .bat'i kopyalamıyor"
fi


# ─────────────────────────────────────────────────────────────────────────────
# SİSTEM DİSKİ LVM/LUKS/RAID ARKASINDAN DA GÖRÜLMELİ
# ─────────────────────────────────────────────────────────────────────────────
#
# ── Yakalanan gerçek hata ────────────────────────────────────────────────────
# systemDisk() kökü yalnızca düz bir bölümse çözebiliyordu. Sıradan bir
# Ubuntu/Fedora/Debian kurulumu kökü LVM ya da LUKS üzerine koyar; orada
# /proc/mounts "/dev/mapper/ubuntu--vg-ubuntu--lv" der ve eski kod bundan
# hiçbir /sys/block girdisiyle eşleşmeyen bir ad üretiyordu. Sonuç: HİÇBİR
# disk sistem diski sayılmıyordu ve --all-disks ile makinenin açılış diski
# silinebilir hedefler arasında listeleniyordu.
#
# Davranışın kendisi Go testinde sahte bir sysfs ağacıyla doğrulanıyor
# (internal/flash/enum_linux_test.go). Buradaki denetim, o korumaların
# KODDAN KALDIRILMADIĞINI garanti eder.
#
# lvm-emniyet
echo ""
echo "sistem diski çözümlemesi"

EL="$ROOT/internal/flash/enum_linux.go"

if strip_comments "$EL" | grep -q 'slaves'; then
    pass "dm/md üyeleri (slaves) izleniyor — LVM/LUKS/RAID kökü çözülür"
else
    fail "slaves izlenmiyor — LVM/LUKS kökünde sistem diski görünmez olur"
fi

if strip_comments "$EL" | grep -q 'EvalSymlinks'; then
    pass "/dev/mapper sembolik bağı çözülüyor"
else
    fail "/dev/mapper çözülmüyor — kök aygıt adı /sys ile eşleşmez"
fi

if strip_comments "$EL" | grep -q 'unknownSystem'; then
    pass "sistem diski bilinmiyorsa iç diskler gizleniyor (emniyet yedeği)"
else
    fail "emniyet yedeği yok — sistem diski çözülemezse iç diskler listelenir"
fi

# Go testleri bu korumayı gerçekten sınamalı.
if [ -f "$ROOT/internal/flash/enum_linux_test.go" ] &&
   grep -q 'TestSystemDiskSeenThroughLVM' "$ROOT/internal/flash/enum_linux_test.go"; then
    pass "LVM senaryosu Go testinde sınanıyor"
else
    fail "LVM senaryosunu sınayan Go testi yok"
fi

# Linux yazma yolu bağlı bir aygıta yazmamalı.
RL="$ROOT/internal/flash/raw_linux.go"
if strip_comments "$RL" | grep -q 'O_EXCL'; then
    pass "aygıt O_EXCL ile açılıyor — bağlıysa çekirdek reddeder"
else
    fail "O_EXCL yok — bağlı bir belleğe yazıp sessizce bozabilir"
fi

if strip_comments "$RL" | grep -q 'unmountPartitions'; then
    pass "hedef diskin bölümleri yazmadan önce ayrılıyor"
else
    fail "bölümler ayrılmıyor — eski dosya sistemi imajın üstüne yazar"
fi

# Windows boyut sorgusu erişim maskesi istemeyen koddan yapılmalı.
EW="$ROOT/internal/flash/enum_windows.go"
if strip_comments "$EW" | grep -q 'ioctlDiskGetDriveGeometryEx'; then
    pass "Windows boyut sorgusu FILE_ANY_ACCESS koduyla yapılıyor"
else
    fail "boyut sorgusu yalnızca FILE_READ_ACCESS koduyla — aygıt listesi hep boş döner"
fi

echo ""
if [ "$fails" -eq 0 ]; then
    printf '\033[32mFLAŞ GÜVENLİĞİ TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
