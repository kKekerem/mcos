#!/bin/sh
# builtin-java.sh — Temurin JRE'yi hedef rootfs'e açar (post-build.sh çağırır).
#
# ── Neden ───────────────────────────────────────────────────────────────────
# QEMU'da 2026-09-25 imajıyla, ağ kapalıyken ölçüldü:
#   ls /usr/lib/jvm              -> No such file or directory
#   mcosctl java install 21      -> lookup api.adoptium.net ... CIKIS=1
# İlk açılışta hiç Java yoktu; Java 21 isteyen her sunucu önce internetten
# ~200 MB JDK indirmek zorundaydı. Çevrimdışı paketteki JRE /data/artifacts'ta
# dursa bile Java yöneticisi ona hiç bakmıyordu.
#
# Artık JRE derleme zamanında /usr/lib/jvm/temurin-<major>-jre altına açılır;
# internal/java/builtin.go o dizini "kurulu" sayar ve hiçbir şey indirmez.
#
# ── Neden ayrı dosya ────────────────────────────────────────────────────────
# post-build.sh bütün olarak ancak Buildroot içinde çalışır (grub, firmware…).
# Bu adım tek başına da çalışabilsin ki scripts/test-java-builtin.sh onu
# gerçek arşivle, sahte bir TARGET_DIR üzerinde sınayabilsin.
#
# Ortam: TARGET_DIR (zorunlu), BR2_CONFIG (varsa libc denetlenir),
#        MCOS_JAVA_CACHE (arşiv dizini; yoksa depo kökündeki dist/java),
#        MCOS_JAVA_CONF (tanım; yoksa bu dizindeki builtin-java.conf).
#
# Herhangi bir eksiklikte çıkış kodu 1: post-build derlemeyi DURDURUR.

set -eu

BOARD_DIR="$(cd "$(dirname "$0")" && pwd)"
CONF="${MCOS_JAVA_CONF:-$BOARD_DIR/builtin-java.conf}"

say() { echo "post-build: $*"; }
die() {
    echo "post-build: HATA - $*"
    echo "post-build: gomulu Java kurulamadi — derleme durduruluyor"
    exit 1
}

[ -n "${TARGET_DIR:-}" ] && [ -d "$TARGET_DIR" ] || die "TARGET_DIR yok (${TARGET_DIR:-bos})"
[ -f "$CONF" ] || die "gomulu Java tanimi yok: $CONF"
# shellcheck disable=SC1090
. "$CONF"
MAJOR="${JAVA_BUILTIN_MAJOR:?}"
URL="${JAVA_BUILTIN_URL:?}"
SHA="${JAVA_BUILTIN_SHA256:?}"

# ── 1. Hedef libc ───────────────────────────────────────────────────────────
# Temurin'in linux yapısı glibc'ye bağlı (release: LIBC="gnu"). musl/uClibc
# bir rootfs'e açılırsa derleme geçer ama java "not found" ile hiç başlamaz:
# yorumlayıcı /lib64/ld-linux-x86-64.so.2 orada yoktur.
if [ -n "${BR2_CONFIG:-}" ] && [ -f "$BR2_CONFIG" ]; then
    grep -q '^BR2_TOOLCHAIN_USES_GLIBC=y' "$BR2_CONFIG" ||
        die "arac zinciri glibc degil; Temurin linux yapisi calismaz (musl icin alpine-linux yapisi gerekir)"
fi
[ -e "$TARGET_DIR/lib64/ld-linux-x86-64.so.2" ] || [ -e "$TARGET_DIR/lib/ld-linux-x86-64.so.2" ] ||
    die "hedefte /lib64/ld-linux-x86-64.so.2 yok; glibc'siz rootfs'te java baslamaz"

# ── 2. Arşiv: önbellekte ve özeti doğru ─────────────────────────────────────
CACHE="${MCOS_JAVA_CACHE:-}"
if [ -z "$CACHE" ]; then
    d="$BOARD_DIR"
    while [ "$d" != "/" ]; do
        # Depo kökü go.mod ile tanınır; dist/ henüz olmayabilir (temiz klon).
        if [ -f "$d/go.mod" ]; then
            CACHE="$d/dist/java"
            break
        fi
        d="$(dirname "$d")"
    done
fi
FILE="${CACHE:-dist/java}/${URL##*/}"
if [ ! -s "$FILE" ]; then
    die "gomulu Java arsivi yok: $FILE
post-build:        Cozum: make builtin-java   (ya da: sh scripts/fetch-builtin-java.sh)"
fi
got="$(sha256sum "$FILE" | cut -d' ' -f1)"
[ "$got" = "$SHA" ] || die "SHA-256 uyusmuyor: $FILE
post-build:        beklenen $SHA
post-build:        gelen    $got"

# ── 3. Aç ───────────────────────────────────────────────────────────────────
REL="usr/lib/jvm/temurin-${MAJOR}-jre"
DEST="$TARGET_DIR/$REL"
rm -rf "$DEST"
mkdir -p "$DEST"
tar -xzf "$FILE" -C "$DEST" --strip-components=1 --no-same-owner ||
    die "arsiv acilamadi: $FILE"
acik_once="$(du -sk "$DEST" | cut -f1)"

# ── 4. Güvenli küçültme ─────────────────────────────────────────────────────
# rootfs initramfs'tir: her açılışta TAMAMEN RAM'e açılır, yani burada kalan
# her bayt kalıcı RAM'dir. Yalnızca hiçbir sunucu/eklentinin YÜKLEYEMEYECEĞİ
# ya da hiç KULLANMAYACAĞI dosyalar atılır; modül (lib/modules) ATILMAZ.
#
#   classes_nocoops.jsa  13,3 MB  CDS arşivi, YALNIZCA sıkıştırılmış oop'lar
#                                  kapalıyken (yığın >= 32 GB) kullanılır.
#                                  Yoksa JVM CDS'siz başlar, o kadar.
#                                  (classes.jsa — olağan durum — KALIR.)
#   libawt_xawt.so,      ~1 MB    libX11/libXext/libXrender/libXtst/libXi'ye
#   libsplashscreen.so,           bağlı; bu imajda X11 kütüphanesi yok, yani
#   libjawt.so                    dlopen zaten başarısız olurdu. Başsız
#                                 (headless) AWT libawt_headless.so kullanır.
#
# BİLEREK KALANLAR:
#   lib/modules (98,7 MB) — jlink ile modül atmak eklentileri kırabilir.
#   lib/server/libjvm.so sembolleri (4,6 MB) — spark/async-profiler gibi
#       profil eklentileri HotSpot iç sembollerini .symtab'dan okur.
#   legal/ — GPLv2 dağıtımı lisans metnini gerektirir; dosyaların çoğu bağ.
# src.zip, man/, demo/, jmods/ JRE arşivinde zaten YOK.
rm -f "$DEST/lib/server/classes_nocoops.jsa" \
      "$DEST/lib/libawt_xawt.so" \
      "$DEST/lib/libsplashscreen.so" \
      "$DEST/lib/libjawt.so"

# ── 5. Doğrula ──────────────────────────────────────────────────────────────
# internal/java/builtin.go'nun "gömülü" saymak için aradığı üç şeyin aynısı.
# Burada eksik çıkarsa panel Java 21'i "kurulu değil" gösterir; bunu sahada
# değil derlemede yakalamak gerekir.
[ -x "$DEST/bin/java" ] || die "$REL/bin/java yok ya da calistirilamaz"
[ -s "$DEST/lib/modules" ] || die "$REL/lib/modules yok"
[ -s "$DEST/lib/server/libjvm.so" ] || die "$REL/lib/server/libjvm.so yok"
[ -s "$DEST/lib/server/classes.jsa" ] || die "$REL/lib/server/classes.jsa yok (CDS)"
ver="$(sed -n 's/^JAVA_VERSION="\([^"]*\)"/\1/p' "$DEST/release" 2>/dev/null)"
case "$ver" in
    "$MAJOR"|"$MAJOR".*) ;;
    *) die "$REL/release JAVA_VERSION='$ver', Java $MAJOR bekleniyordu" ;;
esac

# Kabukta "java -version" doğrudan çalışsın. Başlatıcı JAVA_HOME'u
# /proc/self/exe'den çözdüğü için bağ güvenlidir.
mkdir -p "$TARGET_DIR/usr/bin"
ln -sf "../lib/jvm/temurin-${MAJOR}-jre/bin/java" "$TARGET_DIR/usr/bin/java"

acik_sonra="$(du -sk "$DEST" | cut -f1)"
say "gomulu Java $ver kuruldu -> /$REL"
say "         $((acik_sonra / 1024)) MB acik (kucultmeden once $((acik_once / 1024)) MB); rootfs RAM'de oldugu icin bu kadar RAM"
