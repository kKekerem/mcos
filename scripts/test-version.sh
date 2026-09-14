#!/bin/sh
# test-version.sh — sürüm numarasının TEK bir kaynaktan geldiğini doğrular.
#
# ── Neden bu test var ───────────────────────────────────────────────────────
# Sürüm numarası eskiden SEKİZ ayrı yerde elle yazılıydı: daemon sabiti,
# model varsayılanları, panel kenar çubuğu, katalog User-Agent, demo verisi,
# Buildroot paketi, VERSION dosyası ve testler. Bir yükseltmede bazıları
# güncelleniyor, bazıları unutuluyordu — kullanıcı panelde "v0.1.0" görürken
# daemon kendini "1.0.0" diye tanıtıyordu.
#
# Artık tek kaynak internal/version/version.go. Bu test, kimsenin yeniden
# elle sürüm yazmadığını garanti eder.
#
# Hiçbir şey derlemez, hiçbir donanıma dokunmaz: yalnızca dosya okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VER_FILE="$ROOT/VERSION"
VER_GO="$ROOT/internal/version/version.go"
MK="$ROOT/os/buildroot/external/package/mcos/mcos.mk"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$VER_FILE" "$VER_GO" "$MK"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

echo "== 1. Tek kaynak =="

WANT="$(tr -d ' \t\r\n' < "$VER_FILE")"
if [ -z "$WANT" ]; then
    fail "VERSION dosyası boş"
    WANT="?"
else
    pass "VERSION = $WANT"
fi

GOT="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' "$VER_GO" | head -1)"
if [ "$GOT" = "$WANT" ]; then
    pass "internal/version aynı sürümü bildiriyor"
else
    fail "internal/version = '$GOT', VERSION = '$WANT' — ikisi ayrışmış"
fi

MKVER="$(sed -n 's/^MCOS_VERSION = \(.*\)$/\1/p' "$MK" | tr -d ' \r' | head -1)"
if [ "$MKVER" = "$WANT" ]; then
    pass "Buildroot paketi aynı sürümü bildiriyor"
else
    fail "mcos.mk = '$MKVER', VERSION = '$WANT'"
fi

echo "== 2. Elle yazılmış sürüm kalmamış =="

# Eski sürümün kod içinde kalmış olması, birinin sabiti geri yazdığını
# gösterir. Testleri ve bu betiği hariç tutuyoruz.
stray="$(grep -rn '"0\.1\.0"' \
    "$ROOT/cmd" "$ROOT/internal" "$ROOT/panel" 2>/dev/null \
    | grep -v '_test\.go' || true)"
if [ -z "$stray" ]; then
    pass "kodda elle yazılmış eski sürüm yok"
else
    fail "elle yazılmış sürüm bulundu:"
    echo "$stray" | sed 's/^/        /'
fi

# version paketinin kendisi dışında hiçbir yerde "const Version =" olmamalı;
# daemon'unki internal/version'a atıf yapıyor olmalı.
#
# internal/ipc HARİÇ: oradaki Version, MCOS'un sürümü değil JSON-RPC
# PROTOKOL sürümüdür ("2.0") ve MCOS yükseltildiğinde değişmez. İkisini
# karıştırmak, protokol sürümünü her sürümde bozmak demek olurdu.
dup="$(grep -rn '^const Version = "' "$ROOT/cmd" "$ROOT/internal" "$ROOT/panel" \
    2>/dev/null \
    | grep -v 'internal/version/version.go' \
    | grep -v 'internal/ipc/protocol.go' || true)"
if [ -z "$dup" ]; then
    pass "ikinci bir Version sabiti yok"
else
    fail "ayrı bir Version sabiti var:"
    echo "$dup" | sed 's/^/        /'
fi

echo "== 3. Mod sürümü =="

MODPROPS="$ROOT/mods/mcos-link/gradle.properties"
if [ -f "$MODPROPS" ]; then
    MODVER="$(sed -n 's/^mod_version=\(.*\)$/\1/p' "$MODPROPS" | tr -d ' \r' | head -1)"
    if [ "$MODVER" = "$WANT" ]; then
        pass "mcos-link modu aynı sürümü taşıyor"
    else
        fail "mcos-link mod_version = '$MODVER', VERSION = '$WANT'"
    fi
else
    fail "mods/mcos-link/gradle.properties yok"
fi

echo ""
if [ "$fails" -eq 0 ]; then
    printf '\033[32mSÜRÜM TUTARLI\033[0m (%s)\n' "$WANT"
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
