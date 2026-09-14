#!/bin/sh
# ============================================================================
#  MCOS Düğüm — Linux/macOS başlatıcı
# ============================================================================
#
#  Ne yapar:
#    Bu bilgisayarı MCOS'a "ikinci PC" olarak ekleyen programı çalıştırır.
#
#  NEDEN KÖK HAKKI İSTEMİYOR: mcos-flash'in aksine bu program hiçbir diske ham
#  yazma yapmaz. Yalnızca kendi klasörüne yazar ve bir TCP portu dinler.
#  Gereksiz yere kök istemek, kullanıcıya sudo'yu refleksle yazmayı öğretir.
#
#  NOT: 2222 ayrıcalıklı bir port DEĞİLDİR (1024'ün üstü), bu yüzden sıradan
#  bir kullanıcı da dinleyebilir. Eşleştirme portunun 2222 seçilmesinin bir
#  nedeni de budur.
#
#  POSIX sh: bash olmayan sistemlerde de çalışsın (dash, busybox ash).
# ============================================================================

set -eu

# Betiğin bulunduğu klasör. $0 göreli olabilir; cd + pwd ile mutlaklaştır.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
EXE="$SCRIPT_DIR/mcos-node"

if [ ! -x "$EXE" ]; then
    echo ""
    echo "  mcos-node bu klasörde bulunamadı:"
    echo "    $SCRIPT_DIR"
    echo ""
    echo "  Derlemek için:  make node"
    echo ""
    exit 1
fi

# exec: yeni süreç bu kabuğun yerine geçer, böylece Ctrl+C doğrudan programa
# gider ve iki katmanlı bir süreç ağacı kalmaz.
#
# "$@": kullanıcının verdiği tüm argümanlar olduğu gibi aktarılır.
exec "$EXE" "$@"
