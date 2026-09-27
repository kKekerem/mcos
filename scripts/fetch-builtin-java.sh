#!/bin/sh
# fetch-builtin-java.sh — imaja gömülecek Temurin JRE'yi DERLEMEDEN ÖNCE
# indirir ve SHA-256 ile doğrular.
#
# ── Neden ayrı bir betik (fetch-offline-bundle.sh değil)? ──────────────────
# Çevrimdışı paket "indirilemezse UYAR ve devam et" kuralıyla çalışır:
# internetsiz bir derleme makinesi de imaj üretebilsin diye. Gömülü Java için
# bu kural YANLIŞ olurdu — Java'sız bir imaj, kullanıcının açıkça istediği
# "direkt kurulu gelsin" sözünü sessizce bozar. Bu betik bu yüzden eksik ya
# da bozuk arşivde çıkış kodu 1 ile DURUR.
#
# ── Önbellek ────────────────────────────────────────────────────────────────
# Arşiv dist/java/<arşiv-adı> olarak saklanır. Doğru özetli bir dosya varsa
# hiçbir şey indirilmez. Önceki derlemelerin çevrimdışı paketinde (dist/offline)
# aynı arşiv duruyorsa ve özeti tutuyorsa oradan kopyalanır: 50 MB'ı ikinci
# kez indirmeye gerek yok.
#
# Kullanım:
#   scripts/fetch-builtin-java.sh [onbellek-dizini] [conf]

set -eu

OUT="${1:-dist/java}"
CONF="${2:-os/buildroot/external/board/mcos/builtin-java.conf}"

[ -f "$CONF" ] || { echo "gomulu Java tanimi yok: $CONF" >&2; exit 2; }
# shellcheck disable=SC1090
. "$CONF"
: "${JAVA_BUILTIN_URL:?JAVA_BUILTIN_URL tanimsiz ($CONF)}"
: "${JAVA_BUILTIN_SHA256:?JAVA_BUILTIN_SHA256 tanimsiz ($CONF)}"

command -v sha256sum >/dev/null 2>&1 || { echo "sha256sum gerekli" >&2; exit 2; }

name="${JAVA_BUILTIN_URL##*/}"
dst="$OUT/$name"
mkdir -p "$OUT"

sha_of() { sha256sum "$1" | cut -d' ' -f1; }

if [ -s "$dst" ] && [ "$(sha_of "$dst")" = "$JAVA_BUILTIN_SHA256" ]; then
    echo ">> gomulu Java hazir: $dst ($(du -h "$dst" | cut -f1), SHA-256 dogru)"
    exit 0
fi
if [ -e "$dst" ]; then
    # Yarım inmiş ya da eski sürümden kalmış bir dosya. Üzerine yazılacak;
    # ama bunu SÖYLE ki önbellekteki bozulma görünmez kalmasın.
    echo ">> UYARI: $dst ozeti tutmuyor; yeniden indiriliyor"
fi

# Çevrimdışı paketteki kopya (URL özeti önekli ad; bkz. fetch-offline-bundle.sh).
for f in dist/offline/*-"$name"; do
    [ -s "$f" ] || continue
    if [ "$(sha_of "$f")" = "$JAVA_BUILTIN_SHA256" ]; then
        cp "$f" "$dst.tmp" && mv "$dst.tmp" "$dst"
        echo ">> gomulu Java cevrimdisi paketten alindi: $dst"
        exit 0
    fi
done

if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fL --retry 3 --progress-bar -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -O "$2" "$1"; }
else
    echo "curl veya wget gerekli" >&2
    exit 2
fi

echo ">> gomulu Java indiriliyor: $name"
rm -f "$dst.tmp"
if ! fetch "$JAVA_BUILTIN_URL" "$dst.tmp"; then
    rm -f "$dst.tmp"
    echo "HATA: gomulu Java indirilemedi: $JAVA_BUILTIN_URL" >&2
    echo "      Internet baglantisini denetleyin ya da arsivi elle $dst yoluna koyun." >&2
    exit 1
fi
got="$(sha_of "$dst.tmp")"
if [ "$got" != "$JAVA_BUILTIN_SHA256" ]; then
    rm -f "$dst.tmp"
    echo "HATA: gomulu Java SHA-256 UYUSMUYOR" >&2
    echo "      beklenen: $JAVA_BUILTIN_SHA256" >&2
    echo "      gelen   : $got" >&2
    echo "      Arsiv bozuk ya da degistirilmis; imaja KONMAYACAK." >&2
    exit 1
fi
# Geçici addan taşı: yarım bir dosya bir sonraki çalıştırmada "var" sanılmasın.
mv "$dst.tmp" "$dst"
echo ">> gomulu Java indirildi ve dogrulandi: $dst ($(du -h "$dst" | cut -f1))"
