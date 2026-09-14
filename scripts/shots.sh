#!/bin/sh
# shots.sh — panelin ve kurulum sihirbazının her ekranını PNG olarak üretir.
#
# Donanım gerekmez: panel --screenshot kipinde örnek veriyle çizer. Arayüz
# değişikliklerini gözden geçirmenin en hızlı yolu budur — bir ekranın boş
# hâlde iyi, dolu hâlde kötü görünmesi çok yaygın bir hatadır.
#
# Kullanım:
#   sh scripts/shots.sh [çıktı-klasörü] [panel-ikilisi]
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
OUT="${1:-dist/shots}"
BIN="${2:-}"

# İkili verilmediyse derle: betiğin, kullanıcının önceden bir şey
# derlemesini beklemesi gereksiz bir engel.
if [ -z "$BIN" ]; then
    BIN="$(mktemp -d)/mcos-panel-fb"
    GO="${GO:-go}"
    command -v "$GO" >/dev/null 2>&1 || GO=/usr/local/go/bin/go
    (cd "$ROOT" && "$GO" build -o "$BIN" ./cmd/mcos-panel-fb)
fi

mkdir -p "$OUT"
rm -f "$OUT"/*.png

shot() { # bolum_no dosya_adi [ek bayraklar…]
    n="$1"; name="$2"; shift 2
    "$BIN" --screenshot "$OUT/$name.png" --connect /yok.sock \
        --section "$n" "$@" >/dev/null 2>&1 || echo "HATA: $name"
}

setup_shot() { # sayfa_no dosya_adi
    "$BIN" --screenshot "$OUT/$2.png" --connect /yok.sock \
        --setup-page "$1" >/dev/null 2>&1 || echo "HATA: $2"
}

wizard_shot() { # sayfa_no dosya_adi
    "$BIN" --screenshot "$OUT/$2.png" --connect /yok.sock \
        --wizard-page "$1" >/dev/null 2>&1 || echo "HATA: $2"
}

echo "== panel =="
# Odak karşılaştırması: aynı bölüm, iki farklı odak.
shot 1 01-odak-menude
shot 1 02-odak-icerikte --content

shot 0  03-sistem-durumu --content
shot 2  04-usb           --content
shot 3  05-yazilim       --content
shot 4  06-performans    --content
shot 5  07-donanim       --content
shot 6  08-ag            --content
shot 7  09-ekran         --content
shot 8  10-tunel         --content
shot 9  11-paylasim      --content
shot 10 12-guc           --content
shot 11 13-ayarlar       --content

echo "== açılır pencereler (bulanık arka plan) =="
shot 6  20-modal-liste   --content --modal list
shot 6  21-modal-parola  --content --modal password
shot 10 22-modal-onay    --content --modal confirm
shot 11 23-modal-fare    --content --modal pointer
shot 9  24-modal-metin   --content --modal text
shot 0  25-kilit-ekrani  --modal lock

echo "== kurulum sihirbazı =="
setup_shot 0 30-oobe-hosgeldiniz
setup_shot 1 31-oobe-sistem
setup_shot 2 32-oobe-ad-ag
setup_shot 3 33-oobe-java
setup_shot 4 34-oobe-gorunum
setup_shot 5 35-oobe-kaynak
setup_shot 6 36-oobe-ozellikler
setup_shot 7 37-oobe-guvenlik
setup_shot 8 38-oobe-ozet
setup_shot 9 39-oobe-diske-kur

echo "== sunucu oluşturma sihirbazı =="
wizard_shot 0 50-sunucu-sablon
wizard_shot 1 51-sunucu-ad
wizard_shot 2 52-sunucu-surum
wizard_shot 3 53-sunucu-altyapi
wizard_shot 4 54-sunucu-ag
wizard_shot 5 55-sunucu-oyun
wizard_shot 6 56-sunucu-kaynak
wizard_shot 7 57-sunucu-yer
wizard_shot 8 58-sunucu-eula
wizard_shot 9 59-sunucu-ozet

echo "== açılış ekranı =="
SPLASH="$(dirname "$BIN")/mcos-splash"
if [ ! -x "$SPLASH" ]; then
    GO="${GO:-go}"
    command -v "$GO" >/dev/null 2>&1 || GO=/usr/local/go/bin/go
    (cd "$ROOT" && "$GO" build -o "$SPLASH" ./cmd/mcos-splash) || SPLASH=""
fi
if [ -n "$SPLASH" ] && [ -x "$SPLASH" ]; then
    "$SPLASH" --screenshot "$OUT/40-acilis.png" >/dev/null 2>&1 \
        || echo "HATA: 40-acilis"
fi

ls -1 "$OUT"
