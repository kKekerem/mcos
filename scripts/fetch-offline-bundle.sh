#!/bin/sh
# fetch-offline-bundle.sh — çevrimdışı sunucu paketini DERLEME ZAMANINDA indirir.
#
# ── Neden derleme zamanında? ────────────────────────────────────────────────
# Kullanıcının isteği: internet yokken bile Minecraft sunucusu kurulabilsin.
# Bunun tek yolu, gereken dosyaların imajla BİRLİKTE gelmesidir. Çalışma
# anında indirmek zaten mevcut davranış ve tam olarak çözmeye çalıştığımız
# sorun.
#
# ── Nereye konuyor? ─────────────────────────────────────────────────────────
# İndirilen dosyalar rootfs'e DEĞİL, ayrı bir dizine konur ve mcos-install
# tarafından kalıcı bölüme (/data/artifacts) tohumlanır.
#
# SEBEP: initramfs tamamen RAM'e açılır. 150 MB'lık bir paketi oraya koymak,
# her açılışta 150 MB RAM demektir — üstelik canlı ISO'da hiç gerekmeyebilir.
# Kalıcı bölüme tohumlamak bu maliyeti tamamen ortadan kaldırır.
#
# ── Dosya adları neden URL özetine göre? ────────────────────────────────────
# internal/server/providers/offline.go deposu URL'in SHA-256 özetiyle
# adreslenir. Aynı adlandırmayı burada da kullanıyoruz ki indirme kodu
# değişmeden dosyayı bulsun.
#
# Kullanım:
#   scripts/fetch-offline-bundle.sh [cikti-dizini] [manifest]

set -eu

OUT="${1:-dist/offline}"
MANIFEST="${2:-os/buildroot/external/board/mcos/offline-manifest.txt}"

[ -f "$MANIFEST" ] || { echo "manifest yok: $MANIFEST" >&2; exit 2; }

command -v sha256sum >/dev/null 2>&1 || { echo "sha256sum gerekli" >&2; exit 2; }
if command -v curl >/dev/null 2>&1; then
    FETCH="curl -fsSL --retry 3 -o"
elif command -v wget >/dev/null 2>&1; then
    FETCH="wget -q -O"
else
    echo "curl veya wget gerekli" >&2
    exit 2
fi

mkdir -p "$OUT"

# cache_name, bir URL icin depo dosya adini uretir.
#
# offline.go ile AYNI kural: ozetin ilk 8 baytinin onaltilik gosterimi (16
# hane) + "-" + temizlenmis dosya adi. Bu iki tarafin ayni ada varmasi SART;
# yoksa tohumlanan dosya calisma aninda bulunamaz.
cache_name() {
    url="$1"
    sum="$(printf '%s' "$url" | sha256sum | cut -c1-16)"
    base="${url##*/}"
    base="${base%%\?*}"
    base="${base%%#*}"
    # Guvenli olmayan karakterleri alt cizgiye cevir.
    base="$(printf '%s' "$base" | tr -c 'A-Za-z0-9._-' '_')"
    [ -n "$base" ] || base="artifact"
    printf '%s-%s' "$sum" "$base"
}

total=0
ok=0
skipped=0
failed=0

while IFS= read -r line; do
    # Yorum ve bos satirlari atla.
    case "$line" in
        ''|\#*) continue ;;
    esac

    # Bicim:  <etiket>|<url>
    label="${line%%|*}"
    url="${line#*|}"
    [ "$label" != "$line" ] || { echo "gecersiz satir: $line" >&2; continue; }

    total=$((total + 1))
    name="$(cache_name "$url")"
    dst="$OUT/$name"

    if [ -s "$dst" ]; then
        echo "  var      $label"
        skipped=$((skipped + 1))
        continue
    fi

    printf '  indiriliyor %s ... ' "$label"
    # Gecici ada indir, sonra tasi: yarim kalmis bir dosya bir sonraki
    # calistirmada "zaten var" sanilir ve bozuk jar olarak tohumlanirdi.
    if $FETCH "$dst.tmp" "$url" 2>/dev/null && [ -s "$dst.tmp" ]; then
        mv "$dst.tmp" "$dst"
        echo "tamam ($(du -h "$dst" | cut -f1))"
        ok=$((ok + 1))
    else
        rm -f "$dst.tmp"
        echo "BASARISIZ"
        echo "    $url" >&2
        failed=$((failed + 1))
    fi
done < "$MANIFEST"

# Etiket -> dosya esleme listesi. mcos-install bunu okuyup ne tohumladigini
# kullaniciya soyleyebilir.
: > "$OUT/INDEX"
while IFS= read -r line; do
    case "$line" in
        ''|\#*) continue ;;
    esac
    label="${line%%|*}"
    url="${line#*|}"
    name="$(cache_name "$url")"
    [ -s "$OUT/$name" ] && printf '%s|%s\n' "$label" "$name" >> "$OUT/INDEX"
done < "$MANIFEST"

echo
echo "cevrimdisi paket: $OUT"
echo "  toplam $total, indirilen $ok, zaten vardi $skipped, basarisiz $failed"
[ -d "$OUT" ] && echo "  boyut: $(du -sh "$OUT" | cut -f1)"

# Basarisizlik DERLEMEYI DURDURMAZ: internet olmayan bir derleme makinesinde
# de imaj uretilebilmeli. Paket eksikse sistem eskisi gibi calisma aninda
# indirmeye devam eder.
if [ "$failed" -gt 0 ]; then
    echo
    echo "UYARI: $failed dosya indirilemedi. Imaj yine uretilir, ancak o"
    echo "       parcalar icin calisma aninda internet gerekir."
fi
exit 0
