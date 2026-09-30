#!/bin/sh
# preview-oobe.sh — OOBE (ilk kurulum) sihirbazının her adımını headless olarak
# basar. Tasarım düzeltmeleri ancak ekranı görerek yapılabilir.
#
# Kullanım:
#   sh scripts/preview-oobe.sh            # tüm adımlar
#   sh scripts/preview-oobe.sh 4          # yalnızca 4. adım
#   sh scripts/preview-oobe.sh 4 100 30   # boyut vererek
set -eu

STEP="${1:-all}"
W="${2:-96}"
H="${3:-30}"

BIN=/tmp/mcosbin
ROOT="$(mktemp -d)"
PORT=17791
GO="${GO:-$(command -v go || echo /usr/local/go/bin/go)}"

cleanup() {
    [ -n "${DPID:-}" ] && kill "$DPID" 2>/dev/null || true
    rm -rf "$ROOT"
}
trap cleanup EXIT

mkdir -p "$BIN"
"$GO" build -o "$BIN/mcosd" ./cmd/mcosd
"$GO" build -o "$BIN/mcos-panel" ./cmd/mcos-panel

"$BIN/mcosd" --data-root "$ROOT" --listen "tcp://127.0.0.1:$PORT" >"$ROOT/daemon.log" 2>&1 &
DPID=$!

# Soket hazır olana kadar bekle.
i=0
while [ "$i" -lt 40 ]; do
    if "$BIN/mcos-panel" --connect "tcp://127.0.0.1:$PORT" --screenshot -1 -w 20 -h 5 >/dev/null 2>&1; then
        break
    fi
    i=$((i + 1))
    sleep 0.25
done

# OOBE adım adları (view_setup.go içindeki setupStep sırasıyla).
step_name() {
    case "$1" in
        0) echo "Tanitim" ;;
        1) echo "Sistem Kontrolu" ;;
        2) echo "Kimlik + WiFi" ;;
        3) echo "Java" ;;
        4) echo "Disk Kurulumu" ;;
        5) echo "Tema + Saat Dilimi" ;;
        6) echo "Kaynak Butcesi" ;;
        7) echo "PC Eslestirme" ;;
        8) echo "Ilk Sunucu" ;;
        9) echo "Onay" ;;
        *) echo "Adim $1" ;;
    esac
}

shot() {
    _s="$1"
    printf '\n'
    printf '################ OOBE adim %s — %s ################\n' "$_s" "$(step_name "$_s")"
    # section = -10 - adim  (bkz. panel/screenshot.go setupSectionBase)
    "$BIN/mcos-panel" --connect "tcp://127.0.0.1:$PORT" \
        --screenshot "$(( -10 - _s ))" -w "$W" -h "$H" 2>/dev/null \
        | sed 's/\x1b\[[0-9;]*m//g'
}

if [ "$STEP" = all ]; then
    s=0
    while [ "$s" -lt 10 ]; do
        shot "$s"
        s=$((s + 1))
    done
else
    shot "$STEP"
fi
