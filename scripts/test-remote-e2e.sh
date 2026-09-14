#!/bin/sh
# test-remote-e2e.sh — uzaktan kontrolü GERÇEKTEN çalıştırarak sınar.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN STATİK DENETİM YETMİYOR
# ════════════════════════════════════════════════════════════════════════════
#
# test-remote-logic.sh kaynağı okuyup "koruma kodda duruyor mu" diye bakar.
# Ama bir koruma kodda durup da BAĞLANMAMIŞ olabilir: işleyici kaydedilmemiş,
# köprü başlatılmamış, jeton yanlış yere yazılmış olabilir.
#
# Bu betik gerçek mcosd'yi başlatır, uzaktan erişimi açar ve telefonun
# yapacağı çağrıların AYNISINI yapar:
#
#   1. /health  — kimliksiz, "bu adreste MCOS var mı"
#   2. /rpc     — JETONSUZ, 401 dönmeli
#   3. /rpc     — doğru jetonla, gerçek sistem durumu dönmeli
#
# Hiçbir aygıta dokunmaz, hiçbir şey kurmaz; geçici bir klasörde çalışır ve
# arkasını temizler.

set -u

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
GO="${GO:-go}"

# Yüksek portlar: geliştirme makinesinde çalışan gerçek bir mcosd ya da
# başka bir servisle çakışmasın.
IPC_PORT=17777
REMOTE_PORT=12223

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

command -v curl >/dev/null 2>&1 || {
    echo "  curl yok — uçtan uca sınama atlandı"
    exit 0
}
command -v python3 >/dev/null 2>&1 || {
    echo "  python3 yok — uçtan uca sınama atlandı"
    exit 0
}

WORK="$(mktemp -d)"
DAEMON=""

cleanup() {
    if [ -n "$DAEMON" ]; then
        kill "$DAEMON" 2>/dev/null
        wait "$DAEMON" 2>/dev/null
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

echo "== uzaktan kontrol: uçtan uca =="

if ! (cd "$ROOT" && "$GO" build -o "$WORK/mcosd" ./cmd/mcosd) 2>"$WORK/build.err"; then
    fail "mcosd derlenemedi"
    sed 's/^/    /' "$WORK/build.err"
    exit 1
fi

"$WORK/mcosd" --data-root "$WORK/data" --config "$WORK/data/config.json" \
    --listen "tcp://127.0.0.1:$IPC_PORT" >"$WORK/daemon.log" 2>&1 &
DAEMON=$!

# Daemon'un dinlemeye başlamasını bekle. Sabit bir "sleep 3" yerine
# yoklamak, yavaş bir makinede yanlış hata vermeyi önler.
i=0
while [ "$i" -lt 40 ]; do
    if python3 - "$IPC_PORT" <<'PY' 2>/dev/null
import socket, sys
s = socket.socket()
s.settimeout(0.5)
sys.exit(0 if s.connect_ex(("127.0.0.1", int(sys.argv[1]))) == 0 else 1)
PY
    then
        break
    fi
    i=$((i + 1))
    sleep 0.25
done
if [ "$i" -ge 40 ]; then
    fail "mcosd dinlemeye başlamadı"
    tail -5 "$WORK/daemon.log" | sed 's/^/    /'
    exit 1
fi

# ── Uzaktan erişimi aç (panelin yaptığı çağrı) ─────────────────────────────
TOKEN="$(python3 - "$IPC_PORT" "$REMOTE_PORT" <<'PY'
import json, socket, sys
ipc_port, remote_port = int(sys.argv[1]), int(sys.argv[2])
s = socket.create_connection(("127.0.0.1", ipc_port), timeout=20)
s.sendall((json.dumps({"jsonrpc": "2.0", "id": 1, "method": "remote.enable",
                       "params": {"port": remote_port}}) + "\n").encode())
buf = b""
while not buf.endswith(b"\n"):
    chunk = s.recv(65536)
    if not chunk:
        break
    buf += chunk
s.close()
r = json.loads(buf.decode())
print((r.get("result") or {}).get("token", ""))
PY
)"

if [ -z "$TOKEN" ]; then
    fail "remote.enable jeton döndürmedi"
    tail -5 "$WORK/daemon.log" | sed 's/^/    /'
    exit 1
fi
pass "remote.enable jeton üretti (${#TOKEN} karakter)"

# TLS dinleyicisinin ayağa kalkması için kısa bir yoklama.
i=0
while [ "$i" -lt 40 ]; do
    if curl -sk --max-time 2 "https://127.0.0.1:$REMOTE_PORT/health" >/dev/null 2>&1; then
        break
    fi
    i=$((i + 1))
    sleep 0.25
done

# ── 1. /health ─────────────────────────────────────────────────────────────
HEALTH="$(curl -sk --max-time 5 "https://127.0.0.1:$REMOTE_PORT/health")"
case "$HEALTH" in
    *'"service":"mcos"'*) pass "/health MCOS olduğunu bildiriyor" ;;
    *) fail "/health beklenmeyen yanıt: $HEALTH" ;;
esac
case "$HEALTH" in
    *"$TOKEN"*) fail "/health JETONU SIZDIRIYOR" ;;
    *) pass "/health jeton sızdırmıyor" ;;
esac
case "$HEALTH" in
    *'"fingerprint":"'*) pass "/health sertifika parmak izini veriyor" ;;
    *) fail "/health parmak izi vermiyor — telefon sabitleyemez" ;;
esac

# ── 2. Jetonsuz istek reddedilmeli ─────────────────────────────────────────
CODE="$(curl -sk --max-time 5 -o /dev/null -w '%{http_code}' \
    -X POST "https://127.0.0.1:$REMOTE_PORT/rpc" \
    -d '{"jsonrpc":"2.0","id":1,"method":"system.status"}')"
if [ "$CODE" = "401" ]; then
    pass "jetonsuz istek 401 ile reddedildi"
else
    fail "jetonsuz istek $CODE döndü — YETKİSİZ ERİŞİM"
fi

# Yanlış jeton da reddedilmeli.
CODE="$(curl -sk --max-time 5 -o /dev/null -w '%{http_code}' \
    -X POST "https://127.0.0.1:$REMOTE_PORT/rpc" \
    -H "Authorization: Bearer yanlis-jeton" \
    -d '{"jsonrpc":"2.0","id":1,"method":"system.status"}')"
if [ "$CODE" = "401" ]; then
    pass "yanlış jeton 401 ile reddedildi"
else
    fail "yanlış jeton $CODE döndü"
fi

# ── 3. Doğru jetonla gerçek çağrı ──────────────────────────────────────────
BODY="$(curl -sk --max-time 10 -X POST "https://127.0.0.1:$REMOTE_PORT/rpc" \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":5,"method":"system.status"}')"
case "$BODY" in
    *'"systemName"'*) pass "doğru jetonla gerçek sistem durumu döndü" ;;
    *) fail "beklenmeyen yanıt: $(echo "$BODY" | head -c 200)" ;;
esac

# Telefonun ana ekranı sunucu listesini çeker.
BODY="$(curl -sk --max-time 10 -X POST "https://127.0.0.1:$REMOTE_PORT/rpc" \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":6,"method":"server.list"}')"
case "$BODY" in
    *'"result"'*) pass "server.list telefon üzerinden çalışıyor" ;;
    *) fail "server.list başarısız: $(echo "$BODY" | head -c 200)" ;;
esac

# ── 4. Düz HTTP servis edilmemeli ──────────────────────────────────────────
PLAIN="$(curl -s --max-time 5 "http://127.0.0.1:$REMOTE_PORT/health" 2>&1)"
case "$PLAIN" in
    *'"service":"mcos"'*) fail "düz HTTP ile servis edildi — jeton açık gider" ;;
    *) pass "düz HTTP servis edilmiyor" ;;
esac

echo ""
if [ "$fails" -eq 0 ]; then
    printf '\033[32mUZAKTAN KONTROL UÇTAN UCA ÇALIŞIYOR\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
