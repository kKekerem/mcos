#!/bin/sh
# mcos-install içindeki MBR disk imzası → PARTUUID dönüşümünü izole doğrular.
# Hiçbir gerçek diske dokunmaz; /tmp içinde 512 baytlık sahte bir MBR kullanır.
set -eu

T="$(mktemp)"
trap 'rm -f "$T"' EXIT

dd if=/dev/zero of="$T" bs=512 count=1 status=none
# Diskteki bayt sırası: 1a 2b 3c 4d  →  le32 = 0x4d3c2b1a
printf '\032\053\074\115' | dd of="$T" bs=1 seek=440 conv=notrunc status=none

# mcos-install'daki fonksiyonun birebir kopyası.
read_disksig() {
    _raw="$(dd if="$1" bs=1 skip=440 count=4 2>/dev/null | od -An -tx1 | tr -d ' \n')"
    [ ${#_raw} -eq 8 ] || return 1
    printf '%s%s%s%s' \
        "$(printf '%s' "$_raw" | cut -c7-8)" \
        "$(printf '%s' "$_raw" | cut -c5-6)" \
        "$(printf '%s' "$_raw" | cut -c3-4)" \
        "$(printf '%s' "$_raw" | cut -c1-2)"
}

GOT="$(read_disksig "$T")"
EXPECT="4d3c2b1a"

echo "diskteki bayt sirasi  : 1a 2b 3c 4d"
echo "beklenen (le32, %08x) : $EXPECT"
echo "fonksiyon dondu       : $GOT"

if [ "$GOT" != "$EXPECT" ]; then
    echo "SONUC: BASARISIZ"
    exit 1
fi
echo "SONUC: GECTI  ->  PARTUUID=$GOT-02"

# Bağımsız çapraz doğrulama (çekirdeğin le32_to_cpup davranışı).
if command -v python3 >/dev/null 2>&1; then
    python3 - "$T" <<'PY'
import struct, sys
d = open(sys.argv[1], "rb").read()
sig = struct.unpack_from("<I", d, 0x1b8)[0]
print("capraz dogrulama (python le32) : %08x-02" % sig)
PY
fi

# Rastgele imzayla da tutarlı mı? 20 tur.
i=0
while [ "$i" -lt 20 ]; do
    dd if=/dev/urandom of="$T" bs=1 seek=440 count=4 conv=notrunc status=none
    A="$(read_disksig "$T")"
    B="$(python3 - "$T" <<'PY'
import struct, sys
d = open(sys.argv[1], "rb").read()
print("%08x" % struct.unpack_from("<I", d, 0x1b8)[0])
PY
)"
    if [ "$A" != "$B" ]; then
        echo "TUTARSIZLIK: shell=$A python=$B"
        exit 1
    fi
    i=$((i + 1))
done
echo "20 rastgele imzada shell ve python ayni sonucu verdi."
