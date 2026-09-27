#!/bin/sh
# ============================================================================
#  MCOS Düğüm — Linux kurulumu (kök hakkı GEREKMEZ)
# ============================================================================
#
#  Ne yapar:
#    1. programı ve ortak dünya eklentilerini ~/.local/lib/mcos-node/ altına,
#       komutu ~/.local/bin/mcos-node olarak kurar,
#    2. (isteğe bağlı) --key ile verilen eşleştirme anahtarını kaydeder;
#       verilmezse SORMAZ: eşleştirme kodla yapılır (MCOS'ta "Eşleştir",
#       burada "mcos-node --kabul KOD"),
#    3. systemd kullanıcı birimini kurar, etkinleştirir ve başlatır.
#
#  Kullanım:
#    sh install.sh                 # kurar; eşleştirme kodla
#    sh install.sh --key ANAHTAR   # (gelişmiş) anahtarı elle ver
#    sh install.sh --kaldir        # servisi ve programı kaldırır (dünyalar kalır)
#
#  NEDEN KÖK YOK: düğüm yalnızca kendi klasörüne yazar ve 1024'ün üstündeki
#  portları dinler. sudo istemek, kullanıcıya her betikte "evet"e basmayı
#  öğretir.
#
#  POSIX sh: dash / busybox ash üzerinde de çalışır.
# ============================================================================
set -eu

HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
LIB="$HOME/.local/lib/mcos-node"
BIN="$HOME/.local/bin"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT=mcos-node.service

KEY=""
NAME=""
REMOVE=0
while [ $# -gt 0 ]; do
    case "$1" in
        --key) KEY="${2:-}"; shift 2 ;;
        --name) NAME="${2:-}"; shift 2 ;;
        --kaldir) REMOVE=1; shift ;;
        -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
        *) echo "bilinmeyen seçenek: $1" >&2; exit 2 ;;
    esac
done

have_systemd() {
    command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1
}

if [ "$REMOVE" = 1 ]; then
    if have_systemd; then
        systemctl --user disable --now "$UNIT" 2>/dev/null || true
        rm -f "$UNIT_DIR/$UNIT"
        systemctl --user daemon-reload || true
    elif [ -x "$LIB/mcos-node" ]; then
        "$LIB/mcos-node" --durdur || true
    fi
    rm -f "$BIN/mcos-node"
    rm -rf "$LIB"
    echo "  MCOS Düğüm kaldırıldı. Dünyalar ve ayarlar yerinde:"
    echo "    ${XDG_DATA_HOME:-$HOME/.local/share}/mcos-node"
    exit 0
fi

if [ ! -x "$HERE/mcos-node" ]; then
    echo "  mcos-node bu klasörde yok: $HERE" >&2
    exit 1
fi

# ── 1. Program ──────────────────────────────────────────────────────────────
# Önce servisi durdur: çalışan bir ikilinin üzerine yazmak "Text file busy"
# ile düşer.
if have_systemd && systemctl --user is-active --quiet "$UNIT" 2>/dev/null; then
    systemctl --user stop "$UNIT"
fi
mkdir -p "$LIB" "$BIN"
# Geçici ad + taşıma: yarım kopyalanmış bir program kalmasın.
cp "$HERE/mcos-node" "$LIB/.mcos-node.tmp" && mv -f "$LIB/.mcos-node.tmp" "$LIB/mcos-node"
chmod 755 "$LIB/mcos-node"
# Eklentiler programın YANINDAKİ mods/link'te aranır (sürüme göre jar'lar +
# index-*.tsv); ~/.local/bin'deki bağlantı gerçek yola çözüldüğü için buraya
# konmaları yeter. Eski klasör SİLİNİP yeniden kopyalanır: önceki paketin
# jar'ı kalırsa indeksin göstermediği bir dosya olarak durur, zararsız ama
# kafa karıştırır.
if [ -d "$HERE/mods/link" ]; then
    rm -rf "$LIB/mods/link"
    mkdir -p "$LIB/mods/link"
    cp "$HERE/mods/link/"* "$LIB/mods/link/"
fi
# Eski paketler (indekssiz mcos-link.jar programın yanında) de çalışsın.
for j in "$HERE"/*.jar; do
    [ -f "$j" ] && cp "$j" "$LIB/"
done
cp "$HERE/README.txt" "$LIB/" 2>/dev/null || true
cp "$0" "$LIB/install.sh" 2>/dev/null || true
ln -sf "$LIB/mcos-node" "$BIN/mcos-node"

# ── 2. Anahtar ──────────────────────────────────────────────────────────────
DATA="${XDG_DATA_HOME:-$HOME/.local/share}/mcos-node"
# Anahtar artık SORULMAZ: düğüm anahtarsız başlar, MCOS taramada bulur ve
# kodla eşleştirir (cmd/mcos-node/pairing.go). Eskiden burada 32 haneli
# anahtar elle yapıştırılıyordu; yarım kopyalanan anahtar "anahtar yanlış"
# ile bitiyordu.
if [ -n "$KEY" ] || [ -n "$NAME" ]; then
    set --
    [ -n "$KEY" ] && set -- "$@" --key "$KEY"
    [ -n "$NAME" ] && set -- "$@" --name "$NAME"
    "$LIB/mcos-node" "$@" --yalniz-ayar
fi

# ── 3. Servis ───────────────────────────────────────────────────────────────
if have_systemd; then
    mkdir -p "$UNIT_DIR"
    cp "$HERE/$UNIT" "$UNIT_DIR/$UNIT"
    systemctl --user daemon-reload
    systemctl --user enable --now "$UNIT"
    echo ""
    echo "  Kuruldu: oturum açıldığında kendiliğinden başlar."
    echo "  Eşleştirme: MCOS'ta MCOS Paylaşım → Ağı tara → bu PC → Eşleştir;"
    echo "              burada 'mcos-node' kodu gösterir → K + Enter (ya da mcos-node --kabul KOD)."
    echo "  Durum:   mcos-node          Günlük: $DATA/mcos-node.log"
    echo "  Durdur:  systemctl --user stop $UNIT"
else
    echo ""
    echo "  systemd kullanıcı oturumu yok; servis kurulmadı."
    echo "  Elle çalıştırmak için:  $BIN/mcos-node"
fi

case ":$PATH:" in
    *":$BIN:"*) ;;
    *) echo "  Not: $BIN PATH'te değil; 'mcos-node' yerine $BIN/mcos-node yazın." ;;
esac
echo ""
echo "  Güvenlik duvarı açıksa (ör. ufw) bir kez:"
echo "    sudo ufw allow from 192.168.0.0/16 to any port 2222,25565:25600,27893:27899 proto tcp"
echo "    sudo ufw allow from 192.168.0.0/16 to any port 27891 proto udp"
