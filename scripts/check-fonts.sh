#!/bin/sh
# Gönderilen fontların GERÇEK fontconfig aile adlarını ve .fbtermrc'deki
# birincil font seçimini karşılaştırır. Salt-okunur teşhis aracı.
set -u

T="${1:-os/buildroot/output/target}"

echo "=== .fbtermrc birinci satiri (birincil font listesi) ==="
if [ -f "$T/root/.fbtermrc" ]; then
    grep '^font-names=' "$T/root/.fbtermrc" || echo "(font-names satiri yok)"
else
    echo "(.fbtermrc bulunamadi: $T/root/.fbtermrc)"
fi

echo
echo "=== Kurulu .ttf dosyalari ve GERCEK aile adlari ==="
if command -v fc-query >/dev/null 2>&1; then
    find "$T/usr/share/fonts" -name '*.ttf' 2>/dev/null | sort | while read -r f; do
        fam="$(fc-query -f '%{family[0]}' "$f" 2>/dev/null)"
        spacing="$(fc-query -f '%{spacing}' "$f" 2>/dev/null)"
        printf '  %-52s aile="%s" spacing=%s\n' "${f#"$T"/usr/share/fonts/}" "$fam" "$spacing"
    done
else
    echo "  (fc-query yok — yalnizca dosya adlari)"
    find "$T/usr/share/fonts" -name '*.ttf' 2>/dev/null | sort | sed 's/^/  /'
fi

echo
echo "=== Kritik kontrol: .fbtermrc'de adi gecen her aile kurulu mu? ==="
NAMES="$(grep '^font-names=' "$T/root/.fbtermrc" 2>/dev/null | cut -d= -f2)"
if [ -z "$NAMES" ]; then
    echo "  (font-names okunamadi)"
    exit 0
fi

# Kurulu aile adlarini topla.
INSTALLED="$(find "$T/usr/share/fonts" -name '*.ttf' 2>/dev/null | while read -r f; do
    fc-query -f '%{family[0]}\n' "$f" 2>/dev/null
done | sort -u)"

i=0
echo "$NAMES" | tr ',' '\n' | while read -r name; do
    i=$((i + 1))
    label="yedek"
    [ "$i" = "1" ] && label="BIRINCIL"
    if [ "$name" = "mono" ]; then
        printf '  %-8s %-22s -> fontconfig takma adi\n' "$label" "$name"
        continue
    fi
    if printf '%s\n' "$INSTALLED" | grep -qxF "$name"; then
        printf '  %-8s %-22s -> KURULU\n' "$label" "$name"
    else
        printf '  %-8s %-22s -> ** KURULU DEGIL **\n' "$label" "$name"
    fi
done

echo
echo "=== Sonuc ==="
echo "Birincil font kurulu degilse fbterm tum metni ve kenarlari yanlis"
echo "fontla cizer; box-drawing (- | + karakterleri) ve Turkce glifler bozulur."
