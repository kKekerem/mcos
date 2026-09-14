#!/bin/sh
# ============================================================================
#  MCOS Flash — Linux/macOS başlatıcı
# ============================================================================
#
#  Ne yapar:
#    1. Kök değilse sudo ile kendini yeniden çalıştırır,
#    2. mcos-flash ikilisini bu klasörden çalıştırır.
#
#  NEDEN KÖK: bir blok aygıtına ham yazmak (/dev/sdX) kök hakkı ister.
#  Haksız da değil — bu işlem diskteki her şeyi siler.
#
#  NEDEN "sudo mcos-flash" DEMİYORUZ da betik yazıyoruz: kullanıcının
#  ikilinin tam yolunu bilmesi ve sudo'nun PATH'inde olması gerekirdi.
#  Betik, ikiliyi kendi yanında arar.
#
#  POSIX sh: bash olmayan sistemlerde de çalışsın (busybox ash, dash).
# ============================================================================

set -eu

# Betiğin bulunduğu klasör. $0 göreli olabilir; cd + pwd ile mutlaklaştır.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
EXE="$SCRIPT_DIR/mcos-flash"

if [ ! -x "$EXE" ]; then
    echo ""
    echo "  mcos-flash bu klasörde bulunamadı:"
    echo "    $SCRIPT_DIR"
    echo ""
    echo "  Derlemek için:  make flash"
    echo ""
    exit 1
fi

# ── Kök denetimi ────────────────────────────────────────────────────────────
if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        echo ""
        echo "  MCOS Flash kök hakkı istiyor (ham diske yazmak için)."
        echo ""
        # exec: yeni süreç bu kabuğun yerine geçer, böylece Ctrl+C doğrudan
        # programa gider ve iki katmanlı bir süreç ağacı kalmaz.
        exec sudo -- "$EXE" "$@"
    fi
    echo ""
    echo "  Kök hakkı gerekiyor ve sudo bulunamadı."
    echo "  Şunu deneyin:  su -c '$EXE'"
    echo ""
    exit 1
fi

exec "$EXE" "$@"
