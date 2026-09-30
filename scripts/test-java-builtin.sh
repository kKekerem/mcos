#!/bin/sh
# test-java-builtin.sh — Java 21'in imaja GÖMÜLDÜĞÜNÜ ve eksikse derlemenin
# DURDUĞUNU kilitleyen test.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN VAR
# ════════════════════════════════════════════════════════════════════════════
#
# Kullanıcının isteği: "OS'un içine Java'yı göm, hiç uğraşmayalım, direkt
# kurulu gelsin Java 21."
#
# 2026-09-25 imajıyla QEMU'da, ağ kapalıyken ölçülen:
#   ls /usr/lib/jvm            -> No such file or directory
#   mcosctl java install 21    -> lookup api.adoptium.net ... CIKIS=1
# Çevrimdışı paketteki JRE o sırada /data/artifacts'ta DURUYORDU ama hiçbir
# kod onu okumuyordu.
#
# Bu test:
#   1. Parçaların birbirine bağlı olduğunu denetler (post-build -> yardımcı
#      betik, Makefile -> indirme, Go'nun aradığı yol == betiğin açtığı yol).
#   2. builtin-java.sh'ı SAHTE bir TARGET_DIR üzerinde gerçekten çalıştırır:
#      arşiv yok / özet yanlış / musl / yorumlayıcı yok durumlarında çıkış
#      kodu 1 vermeli; gerçek arşivle açmalı, küçültmeli ve doğrulamalı.
#   3. Açılan GERÇEK JRE'yi Go tarafına verir (TestBuiltinRealJRE): tarama
#      Java 21'i ağsız "kurulu" saymalı, sürüm satırı gerçek java -version ile
#      aynı olmalı.
#
# Hiçbir diske dokunmaz, Buildroot çalıştırmaz; yalnızca geçici dizin kullanır.
# Gerçek arşiv dist/java'da yoksa 2. ve 3. bölümün gerçek-arşiv kısmı
# ATLANIR ve bunu yüksek sesle söyler (make builtin-java ile indirilir).

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BOARD="$ROOT/os/buildroot/external/board/mcos"
# Yollar ortamdan ezilebilir: karşı-sınama, paylaşılan dosyaları (post-build,
# Makefile) yerinde bozmak yerine bozulmuş KOPYALARLA koşar.
POSTBUILD="${POSTBUILD:-$BOARD/post-build.sh}"
HELPER="${HELPER:-$BOARD/builtin-java.sh}"
CONF="${CONF:-$BOARD/builtin-java.conf}"
MANIFEST="${MANIFEST:-$BOARD/offline-manifest.txt}"
MAKEFILE="${MAKEFILE:-$ROOT/Makefile}"
FETCH="${FETCH:-$ROOT/scripts/fetch-builtin-java.sh}"
GOSRC="${GOSRC:-$ROOT/internal/java/builtin.go}"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }
skip() { printf '  \033[33mATLANDI\033[0m %s\n' "$1"; }

for f in "$POSTBUILD" "$HELPER" "$CONF" "$MANIFEST" "$MAKEFILE" "$FETCH" "$GOSRC"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

# shellcheck disable=SC1090
. "$CONF"

echo "== 1. parcalar birbirine bagli =="

# Yorum satırları elenir: açıklamalar eski/örnek satırları alıntılayabilir.
code() { grep -v '^[[:space:]]*#' "$1"; }

if code "$POSTBUILD" | grep -q 'sh "\${BOARD_DIR}/builtin-java.sh" || exit 1'; then
    pass "post-build gomulu Java adimini cagiriyor ve hatada duruyor"
else
    fail "post-build builtin-java.sh'i cagirmiyor ya da hatasini yutuyor"
fi

if grep -Eq '^os:.* builtin-java( |$)' "$MAKEFILE" && grep -q 'scripts/fetch-builtin-java.sh' "$MAKEFILE"; then
    pass "'make os' once gomulu Java arsivini indiriyor"
else
    fail "'make os' zinciri builtin-java'ya bagli degil — arsiv eksik kalir"
fi

if printf '%s' "$JAVA_BUILTIN_SHA256" | grep -Eq '^[0-9a-f]{64}$'; then
    pass "tanimda 64 haneli SHA-256 var"
else
    fail "JAVA_BUILTIN_SHA256 gecersiz: '$JAVA_BUILTIN_SHA256'"
fi

# glibc yapısı olmalı: hedef BR2_TOOLCHAIN_BUILDROOT_GLIBC. alpine-linux (musl)
# yapısı burada "not found" ile hiç başlamaz.
case "${JAVA_BUILTIN_URL##*/}" in
    *jre_x64_linux_hotspot_*.tar.gz) pass "arsiv glibc (linux) x64 JRE yapisi" ;;
    *) fail "arsiv glibc x64 JRE degil: ${JAVA_BUILTIN_URL##*/}" ;;
esac
case "${JAVA_BUILTIN_URL##*/}" in
    OpenJDK${JAVA_BUILTIN_MAJOR}U-*) pass "arsiv adi tanimdaki ana surumle (Java $JAVA_BUILTIN_MAJOR) uyumlu" ;;
    *) fail "arsiv adi Java $JAVA_BUILTIN_MAJOR degil" ;;
esac

# Go'nun aradığı yol ile betiğin açtığı yol AYNI olmalı; ayrışırsa imajda JRE
# durur ama panel "kurulu değil" der.
if grep -q '^const BuiltinRoot = "/usr/lib/jvm"$' "$GOSRC" &&
   grep -q '^const builtinPattern = "temurin-\*-jre"$' "$GOSRC" &&
   code "$HELPER" | grep -q 'REL="usr/lib/jvm/temurin-\${MAJOR}-jre"'; then
    pass "Go taramasi (/usr/lib/jvm/temurin-*-jre) == post-build yolu"
else
    fail "Go'nun aradigi yol ile builtin-java.sh'in actigi yol ayrismis"
fi

# Gomulu ana surumun (21) JRE'si manifestte OLMAMALI: ayni arsivin ikinci
# kopyasi olur. Java 25 ise BILEREK orada (26.x sunuculari; rootfs'e acmak
# canli ISO'da +144 MB RAM demekti) - ama yalnizca 25, baska ana surum degil.
if code "$MANIFEST" | grep -q "OpenJDK${JAVA_BUILTIN_MAJOR}U-jre"; then
    fail "cevrimdisi manifest gomulu Java ${JAVA_BUILTIN_MAJOR} JRE'sini de indiriyor — ikinci kopya"
else
    pass "cevrimdisi manifestte gomulu Java ${JAVA_BUILTIN_MAJOR} yok (rootfs'e gomulu)"
fi
if code "$MANIFEST" | grep -q 'OpenJDK25U-jre_x64_linux_hotspot_.*\.tar\.gz'; then
    pass "cevrimdisi manifestte Java 25 JRE var (Minecraft 26.x)"
else
    fail "cevrimdisi manifestte Java 25 JRE yok - 26.x sunuculari internetsiz kurulamaz"
fi

if code "$POSTBUILD" | grep -q "\*OpenJDK${JAVA_BUILTIN_MAJOR}U-jre_\*) continue"; then
    pass "eski paketteki Java ${JAVA_BUILTIN_MAJOR} JRE'si ISO'ya tasinmiyor"
else
    fail "eski cevrimdisi paketteki Java ${JAVA_BUILTIN_MAJOR} JRE'si hala ISO'ya kopyalaniyor"
fi
if code "$POSTBUILD" | grep -q '\*OpenJDK\*-jre_\*) continue'; then
    fail "post-build TUM JRE'leri atliyor - Java 25 arsivi ISO'ya girmez"
else
    pass "post-build Java 25 arsivini ISO'ya birakiyor"
fi

echo
echo "== 2. builtin-java.sh gercekten calisiyor =="

mktarget() {
    # $1 = dizin; glibc yorumlayıcısı olan asgari bir hedef.
    mkdir -p "$1/lib64" "$1/usr/bin"
    : > "$1/lib64/ld-linux-x86-64.so.2"
}
glibc_cfg="$TMP/glibc.config"
printf 'BR2_TOOLCHAIN_USES_GLIBC=y\n' > "$glibc_cfg"
musl_cfg="$TMP/musl.config"
printf 'BR2_TOOLCHAIN_USES_MUSL=y\n' > "$musl_cfg"

run_helper() {
    # $1 = hedef, $2 = BR2_CONFIG, $3 = önbellek; çıktı $TMP/out'a.
    TARGET_DIR="$1" BR2_CONFIG="$2" MCOS_JAVA_CACHE="$3" MCOS_JAVA_CONF="$CONF" \
        sh "$HELPER" > "$TMP/out" 2>&1
}

# 2a. Arşiv yok -> DUR.
mktarget "$TMP/t-yok"
mkdir -p "$TMP/bos"
if run_helper "$TMP/t-yok" "$glibc_cfg" "$TMP/bos"; then
    fail "arsiv yokken betik BASARILI dondu — Java'siz imaj sessizce uretilirdi"
elif grep -q 'arsivi yok' "$TMP/out" && grep -q 'make builtin-java' "$TMP/out"; then
    pass "arsiv yokken derleme duruyor ve cozumu soyluyor"
else
    fail "arsiv yokken duruyor ama nedenini/cozumu soylemiyor: $(tr '\n' ' ' < "$TMP/out")"
fi

# 2b. Özet yanlış -> DUR (arşiv açılmadan).
mktarget "$TMP/t-sha"
mkdir -p "$TMP/bozuk"
printf 'bu bir JRE degil\n' > "$TMP/bozuk/${JAVA_BUILTIN_URL##*/}"
if run_helper "$TMP/t-sha" "$glibc_cfg" "$TMP/bozuk"; then
    fail "SHA-256 uyusmazken betik BASARILI dondu"
elif grep -q 'SHA-256 uyusmuyor' "$TMP/out" && [ ! -e "$TMP/t-sha/usr/lib/jvm" ]; then
    pass "ozet tutmayan arsiv reddediliyor ve hicbir sey acilmiyor"
else
    fail "ozet tutmayan arsivde yanlis davranis: $(tr '\n' ' ' < "$TMP/out")"
fi

REAL_CACHE="$ROOT/dist/java"
REAL="$REAL_CACHE/${JAVA_BUILTIN_URL##*/}"

# 2c. musl araç zinciri -> DUR. Arşiv denetiminden ÖNCE bakılır, yani
# gerçek arşiv olmadan da sınanabilir.
mktarget "$TMP/t-musl"
if run_helper "$TMP/t-musl" "$musl_cfg" "$TMP/bos"; then
    fail "musl araç zincirinde betik BASARILI dondu"
elif grep -q 'glibc degil' "$TMP/out"; then
    pass "glibc olmayan arac zinciri reddediliyor"
else
    fail "musl'de duruyor ama nedeni yanlis: $(tr '\n' ' ' < "$TMP/out")"
fi

# 2d. Hedefte glibc yorumlayıcısı yok -> DUR.
mkdir -p "$TMP/t-ld/usr"
if run_helper "$TMP/t-ld" "$glibc_cfg" "$TMP/bos"; then
    fail "yorumlayicisiz hedefte betik BASARILI dondu"
elif grep -q 'ld-linux-x86-64.so.2 yok' "$TMP/out"; then
    pass "glibc yorumlayicisi olmayan hedef reddediliyor"
else
    fail "yorumlayicisiz hedefte nedeni yanlis: $(tr '\n' ' ' < "$TMP/out")"
fi

# 2e. Gerçek arşiv: aç, küçült, doğrula.
T="$TMP/t-gercek"
J="$T/usr/lib/jvm/temurin-${JAVA_BUILTIN_MAJOR}-jre"
if [ -s "$REAL" ]; then
    mktarget "$T"
    if run_helper "$T" "$glibc_cfg" "$REAL_CACHE"; then
        pass "gercek arsiv acildi ($(grep 'MB acik' "$TMP/out" | sed 's/^post-build: *//'))"
    else
        fail "gercek arsiv acilamadi: $(tr '\n' ' ' < "$TMP/out")"
    fi
    for f in bin/java lib/modules lib/server/libjvm.so lib/server/classes.jsa \
             lib/libawt_headless.so lib/libfontmanager.so lib/libzip.so \
             lib/security/cacerts release legal/java.base; do
        [ -e "$J/$f" ] || fail "gerekli dosya eksik: $f"
    done
    for f in lib/server/classes_nocoops.jsa lib/libawt_xawt.so lib/libsplashscreen.so lib/libjawt.so; do
        [ -e "$J/$f" ] && fail "kucultme yapilmamis: $f hala var"
    done
    # jlink ile modül ATILMAMALI: release'teki modül listesi tam kalmalı.
    if grep -q 'jdk.compiler' "$J/release" && grep -q 'java.desktop' "$J/release" &&
       grep -q 'jdk.unsupported ' "$J/release"; then
        pass "modul listesi tam (jdk.compiler, java.desktop, jdk.unsupported)"
    else
        fail "release modul listesi eksik — modul atilmis olabilir"
    fi
    if [ "$(readlink "$T/usr/bin/java")" = "../lib/jvm/temurin-${JAVA_BUILTIN_MAJOR}-jre/bin/java" ]; then
        pass "/usr/bin/java gomulu JRE'yi gosteriyor"
    else
        fail "/usr/bin/java bagi yok ya da yanlis"
    fi

    # Aynı mimarideki derleme makinesinde gerçek JVM'i çalıştır.
    if [ "$(uname -m)" = x86_64 ]; then
        if v="$("$T/usr/bin/java" -XX:-UsePerfData -version 2>&1)"; then
            first="$(printf '%s\n' "$v" | head -1)"
            case "$first" in
                *"version \"${JAVA_BUILTIN_MAJOR}"*) pass "java -version: $first" ;;
                *) fail "java -version beklenmeyen: $first" ;;
            esac
        else
            fail "acilan JRE derleme makinesinde calismadi: $v"
        fi
    else
        skip "derleme makinesi x86_64 degil; JVM burada calistirilamaz"
    fi

    echo
    echo "== 3. Go tarafi gercek JRE'yi taniyor =="
    if (cd "$ROOT" && MCOS_JAVA_REAL_ROOT="$T/usr/lib/jvm" \
            go test -count=1 -run 'TestBuiltinRealJRE' ./internal/java/ > "$TMP/gotest" 2>&1) &&
       ! grep -q 'no tests to run\|SKIP' "$TMP/gotest"; then
        pass "TestBuiltinRealJRE: Java 21 agsiz kurulu, surum satiri gercekle ayni"
    else
        fail "TestBuiltinRealJRE basarisiz: $(tail -5 "$TMP/gotest" | tr '\n' ' ')"
    fi
else
    skip "gercek arsiv yok ($REAL) — 'make builtin-java' ile indirip yeniden kosun"
fi

echo
if [ "$fails" -eq 0 ]; then
    printf '\033[32mGOMULU JAVA TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
