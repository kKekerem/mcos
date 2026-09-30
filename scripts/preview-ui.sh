#!/bin/sh
# Paneli headless olarak çalıştırıp ekran görüntüsü basar.
# Geçici bir veri kökü kullanır; hiçbir şeye kurulum yapmaz.
set -eu

BIN=/tmp/mcosbin
ROOT="$(mktemp -d)"
PORT=17777
SECTION="${1:--1}"   # -1 = kurulum sihirbazı, 0.. = ana bölümler
W="${2:-100}"
H="${3:-34}"

cleanup() {
    [ -n "${DPID:-}" ] && kill "$DPID" 2>/dev/null || true
    rm -rf "$ROOT"
}
trap cleanup EXIT

GO="${GO:-$(command -v go || echo /usr/local/go/bin/go)}"
"$GO" build -o "$BIN/mcosd" ./cmd/mcosd
"$GO" build -o "$BIN/mcos-panel" ./cmd/mcos-panel

"$BIN/mcosd" --data-root "$ROOT" --listen "tcp://127.0.0.1:$PORT" >"$ROOT/daemon.log" 2>&1 &
DPID=$!

# Soket hazır olana kadar bekle.
i=0
while [ "$i" -lt 40 ]; do
    if "$BIN/mcos-panel" --connect "tcp://127.0.0.1:$PORT" --screenshot "$SECTION" -w "$W" -h "$H" 2>/dev/null; then
        exit 0
    fi
    i=$((i + 1))
    sleep 0.25
done

echo "panel baglanamadi; daemon gunlugu:" >&2
tail -20 "$ROOT/daemon.log" >&2
exit 1
