#!/bin/sh
# test-link-mods.sh — ortak dünya jar'ları (sürüme göre) imaja, ISO'ya ve
# düğüm paketine DOĞRU giriyor mu.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN BU TEST VAR
# ════════════════════════════════════════════════════════════════════════════
#
# Kullanıcı (gerçek PC): "PC eşleştirmede ortak dünyayı açınca 'mcos link
# kurulu değil' diyor." İki kez yaşandı: önce jar'lar imaja HİÇ
# kopyalanmıyordu; sonra kopyalanan tek jar yalnızca Minecraft 1.21.11
# içindi ve 26.3 sunucusuna kurulmadı. Artık her sürümün jar'ı + indeks
# (dist/mods/link/index-*.tsv) gidiyor. Bu betik şunları denetler:
#
#   1. post-build'in bölümü daemon'un ARADIĞI yola kopyalıyor, düğüm de
#      programın yanındaki mods/link'e bakıyor (yerler sessizce ayrışmasın),
#   2. bölüm GERÇEKTEN çalışıyor: indeks + mod jar'ları rootfs'e (RAM), fabric-
#      api jar'ları rootfs'e DEĞİL önyükleme ortamına (ISO) gidiyor; indeks ya
#      da indeksin gösterdiği dosya eksik/bozuksa derleme DURUYOR,
#   3. düğüm paketinin (make node-*) adımı dist/mods/link'in tamamını
#      programın yanına koyuyor ve eksikse DURUYOR.
#
# Buildroot çalıştırmaz, ağa çıkmaz. Bölüm post-build.sh'tan işaretleriyle
# ("# >>> mcos-link" … "# <<< mcos-link") kesilip sahte bir TARGET_DIR /
# BINARIES_DIR'e karşı çalıştırılır.
#
# Karşı-sınama için yollar ortamdan ezilebilir:
#   POSTBUILD=<bozuk kopya> sh scripts/test-link-mods.sh   -> HATA vermeli
#   MAKEFILE=<bozuk kopya>  sh scripts/test-link-mods.sh   -> HATA vermeli

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
POSTBUILD="${POSTBUILD:-$ROOT/os/buildroot/external/board/mcos/post-build.sh}"
MAKEFILE="${MAKEFILE:-$ROOT/Makefile}"
DAEMON="$ROOT/internal/daemon/handlers_link.go"
NODEHOST="$ROOT/cmd/mcos-node/host.go"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

TAB="$(printf '\t')"
CR="$(printf '\r')"

# fakejar DOSYA ETİKET: zip imzası (PK) taşıyan küçük, birbirinden farklı dosya.
fakejar() { mkdir -p "$(dirname "$1")"; printf 'PK\003\004%s' "$2" > "$1"; }

# mklink KLASÖR: gerçek biçimde iki indeks + gösterdikleri jar'lar.
mklink() {
    mkdir -p "$1"
    {
        echo "# yükleyici${TAB}sürüm${TAB}jar${TAB}bağımlılık"
        echo ""
        echo "fabric${TAB}1.21.11${TAB}mcos-link-fabric-1.21.11.jar${TAB}fabric-api-0.141.6+1.21.11.jar"
        # CRLF satırı: Windows'ta düzenlenmiş bir indeks bozulmamalı.
        echo "fabric${TAB}26.2${TAB}mcos-link-fabric-26.1-26.3.jar${TAB}fabric-api-0.161.0+26.2.jar${CR}"
        echo "fabric${TAB}26.3${TAB}mcos-link-fabric-26.1-26.3.jar${TAB}fabric-api-0.161.0+26.3.jar"
    } > "$1/index-fabric.tsv"
    {
        echo "paper${TAB}1.21.1${TAB}mcos-link-paper.jar${TAB}-"
        echo "paper${TAB}26.3+${TAB}mcos-link-paper.jar${TAB}-"
    } > "$1/index-paper.tsv"
    for j in mcos-link-fabric-1.21.11.jar mcos-link-fabric-26.1-26.3.jar mcos-link-paper.jar \
        fabric-api-0.141.6+1.21.11.jar fabric-api-0.161.0+26.2.jar fabric-api-0.161.0+26.3.jar; do
        fakejar "$1/$j" "$j"
    done
    # İndekste OLMAYAN jar: imaja girmemeli (daemon onu hiçbir sürüme seçmez).
    fakejar "$1/mcos-link-fabric-kullanilmayan.jar" "x"
}

echo "== 1. Yol ve adlar her yerde AYNI =="

dpath="$(sed -n '/^var linkJarDirs/,/^}/p' "$DAEMON" | sed -n 's/^[[:space:]]*"\(\/usr\/[^"]*\)",$/\1/p' | head -1)"
if [ "$dpath" = "/usr/lib/mcos/mods/link" ] &&
    sed -n '/# >>> mcos-link/,/# <<< mcos-link/p' "$POSTBUILD" | grep -q 'TARGET_DIR}/usr/lib/mcos/mods/link"'; then
    pass "post-build daemon'un ilk aradığı yola kopyalıyor ($dpath)"
else
    fail "post-build daemon'un yoluna ('$dpath') kopyalamıyor"
fi
if grep -q '"mods", "link"' "$NODEHOST" &&
    sed -n '/^node-jars:/,/^$/p' "$MAKEFILE" | grep -q 'NODE_JAR_DIR)/mods/link'; then
    pass "düğüm <program>/mods/link'e bakıyor, node-jars oraya koyuyor"
else
    fail "düğümün aradığı yer ile node-jars'ın koyduğu yer ayrışmış"
fi
for name in mcos-link.jar mcos-link-paper.jar; do
    if grep -q "\"$name\"" "$DAEMON" && grep -q "\"$name\"" "$NODEHOST"; then
        pass "$name: daemon ve düğüm aynı HEDEF adı kullanıyor"
    else
        fail "$name hedef adı daemon ya da düğümde farklı"
    fi
done

echo "== 2. post-build ortak dünya bölümü çalışıyor =="

sed -n '/^# >>> mcos-link/,/^# <<< mcos-link/p' "$POSTBUILD" > "$TMP/bolum.sh"
if [ ! -s "$TMP/bolum.sh" ]; then
    fail "post-build'de '# >>> mcos-link' bölümü yok"
else
    mklink "$TMP/mods/link"
    fakejar "$TMP/mods/mcos-link.jar" "eski-fabric"
    fakejar "$TMP/mods/mcos-link-paper.jar" "eski-paper"

    # run_part KAYNAK(dist/mods) HEDEF BINARIES [MCOS_OFFLINE_STAGE]
    run_part() {
        rm -rf "$2" "$3"
        mkdir -p "$2"
        if [ -n "$3" ]; then mkdir -p "$3"; fi
        MCOS_MODS_SRC="$1" TARGET_DIR="$2" BINARIES_DIR="$3" \
            MCOS_OFFLINE_STAGE="${4:-}" sh "$TMP/bolum.sh" > "$TMP/out" 2>&1
    }
    L="usr/lib/mcos/mods/link"

    if run_part "$TMP/mods" "$TMP/t1" "$TMP/b1"; then
        ok=1
        for f in index-fabric.tsv index-paper.tsv mcos-link-fabric-1.21.11.jar \
            mcos-link-fabric-26.1-26.3.jar mcos-link-paper.jar; do
            cmp -s "$TMP/mods/link/$f" "$TMP/t1/$L/$f" || { ok=0; echo "     eksik: $f"; }
        done
        [ "$ok" = 1 ] && pass "indeks + mod jar'ları /$L'e kopyalandı" ||
            fail "mod jar'ları eksik: $(cat "$TMP/out")"
        if ls "$TMP/t1/$L" | grep -q '^fabric-api'; then
            fail "fabric-api rootfs'e (RAM) girdi"
        else
            pass "fabric-api rootfs'e girmedi (RAM)"
        fi
        ok=1
        for f in fabric-api-0.141.6+1.21.11.jar fabric-api-0.161.0+26.2.jar fabric-api-0.161.0+26.3.jar; do
            cmp -s "$TMP/mods/link/$f" "$TMP/b1/mcos-offline/$f" || { ok=0; echo "     eksik: $f"; }
        done
        [ "$ok" = 1 ] && pass "fabric-api'ler AYNI adla önyükleme ortamına (images/mcos-offline) kondu" ||
            fail "fabric-api önyükleme ortamına kopyalanmadı: $(cat "$TMP/out")"
        if [ -e "$TMP/t1/$L/mcos-link-fabric-kullanilmayan.jar" ]; then
            fail "indekste olmayan jar imaja girdi"
        else
            pass "indekste olmayan jar imaja girmedi"
        fi
        if cmp -s "$TMP/mods/mcos-link.jar" "$TMP/t1/usr/lib/mcos/mods/mcos-link.jar" &&
            cmp -s "$TMP/mods/mcos-link-paper.jar" "$TMP/t1/usr/lib/mcos/mods/mcos-link-paper.jar"; then
            pass "eski düzen (/usr/lib/mcos/mods/mcos-link*.jar) varken yazılmaya devam ediyor"
        else
            fail "eski düzen jar'ları yazılmadı"
        fi
    else
        fail "bölüm tam kaynakla başarısız: $(cat "$TMP/out")"
    fi

    # Çevrimdışı paketin hazırladığı dizin varsa fabric-api ORAYA gider.
    if run_part "$TMP/mods" "$TMP/t1s" "$TMP/b1s" "$TMP/b1s/sahne" &&
        [ -s "$TMP/b1s/sahne/fabric-api-0.161.0+26.3.jar" ]; then
        pass "MCOS_OFFLINE_STAGE tanımlıysa fabric-api oraya gidiyor"
    else
        fail "MCOS_OFFLINE_STAGE'e yazılmadı: $(cat "$TMP/out")"
    fi

    # BINARIES_DIR yoksa (yalnızca bu sınamada olur) uyarır ama durmaz.
    if run_part "$TMP/mods" "$TMP/t1n" "" && grep -q "UYARI - BINARIES_DIR yok" "$TMP/out"; then
        pass "BINARIES_DIR yokken uyarıyor, rootfs kısmı yine yazılıyor"
    else
        fail "BINARIES_DIR yokken: $(cat "$TMP/out")"
    fi

    # Eski düzen jar'ları YOKSA derleme durmamalı (yalnızca geçiş için).
    mkdir -p "$TMP/yeni"
    cp -R "$TMP/mods/link" "$TMP/yeni/link"
    if run_part "$TMP/yeni" "$TMP/t1y" "$TMP/b1y" && [ ! -e "$TMP/t1y/usr/lib/mcos/mods/mcos-link.jar" ]; then
        pass "eski düzen jar'ları yokken derleme sürüyor"
    else
        fail "eski jar'lar yokken: $(cat "$TMP/out")"
    fi

    # ── Durması gereken durumlar ──────────────────────────────────────────
    # expect_stop AD MESAJ-PARÇASI AÇIKLAMA
    expect_stop() {
        if run_part "$TMP/$1" "$TMP/t-$1" "$TMP/b-$1"; then
            fail "$3: derleme DURMADI"
        elif grep -q -- "$2" "$TMP/out"; then
            pass "$3: derleme duruyor ve nedeni söylüyor"
        else
            fail "$3: beklenmedik çıktı: $(cat "$TMP/out")"
        fi
    }
    mklink "$TMP/indekssiz/link"; rm "$TMP/indekssiz/link/index-paper.tsv"
    expect_stop indekssiz "index-paper.tsv yok" "Paper indeksi eksik"

    mklink "$TMP/jarsiz/link"; rm "$TMP/jarsiz/link/mcos-link-fabric-26.1-26.3.jar"
    expect_stop jarsiz "mcos-link-fabric-26.1-26.3.jar dosyasını gösteriyor" "indeksin gösterdiği mod jar'ı yok"

    mklink "$TMP/apisiz/link"; rm "$TMP/apisiz/link/fabric-api-0.161.0+26.3.jar"
    expect_stop apisiz "fabric-api-0.161.0+26.3.jar dosyasını gösteriyor" "indeksin gösterdiği fabric-api yok"

    mklink "$TMP/bozuk/link"; printf '<html>404</html>' > "$TMP/bozuk/link/mcos-link-paper.jar"
    expect_stop bozuk "mcos-link-paper.jar dosyasını gösteriyor" "zip olmayan jar"

    mklink "$TMP/kacis/link"; fakejar "$TMP/kacis/disari.jar" "x"
    echo "paper${TAB}1.20.6${TAB}../disari.jar${TAB}-" >> "$TMP/kacis/link/index-paper.tsv"
    expect_stop kacis "bozuk satır" "klasör dışına kaçan jar adı"

    # Son satırın sonunda satır sonu YOK (elle yazılıp aynen kopyalanan
    # index-paper.tsv'de olağan). read o satırı okuyup 1 döndürüyordu ve döngü
    # onu ATLIYORDU — ölçüldü: jar'ı imaja girmedi, eksikken derleme durmadı.
    # Daemon'un ayrıştırıcısı (linkjar) o satırı okur ve çalışırken "kayıtlı
    # ama yok" derdi.
    mklink "$TMP/nlsiz/link"
    printf 'paper\t26.5\tmcos-link-paper-26.5.jar\t-' >> "$TMP/nlsiz/link/index-paper.tsv"
    fakejar "$TMP/nlsiz/link/mcos-link-paper-26.5.jar" "p265"
    printf '%s' "$(cat "$TMP/nlsiz/link/index-fabric.tsv")" > "$TMP/nlsiz/f.tsv"
    mv "$TMP/nlsiz/f.tsv" "$TMP/nlsiz/link/index-fabric.tsv"
    if run_part "$TMP/nlsiz" "$TMP/t-nl" "$TMP/b-nl" &&
        [ -s "$TMP/t-nl/$L/mcos-link-paper-26.5.jar" ] &&
        [ -s "$TMP/b-nl/mcos-offline/fabric-api-0.161.0+26.3.jar" ]; then
        pass "satır sonu olmayan son satırın jar'ı da imaja, fabric-api'si ISO'ya giriyor"
    else
        fail "satır sonu olmayan son satır atlandı: $(cat "$TMP/out")"
    fi
    rm "$TMP/nlsiz/link/mcos-link-paper-26.5.jar"
    expect_stop nlsiz "mcos-link-paper-26.5.jar dosyasını gösteriyor" "satır sonu olmayan son satırın jar'ı yok"

    # Kaynak verilmezse depo kökündeki dist/mods/link yukarı doğru aranır
    # (Buildroot betiği board dizininden çalıştırır).
    mkdir -p "$TMP/depo/os/buildroot/external/board/mcos"
    mklink "$TMP/depo/dist/mods/link"
    cp "$TMP/bolum.sh" "$TMP/depo/os/buildroot/external/board/mcos/bolum.sh"
    mkdir -p "$TMP/t4"
    if (unset MCOS_MODS_SRC MCOS_LINK_SRC; TARGET_DIR="$TMP/t4" BINARIES_DIR="$TMP/b4" \
        sh "$TMP/depo/os/buildroot/external/board/mcos/bolum.sh" > "$TMP/out" 2>&1) &&
        [ -s "$TMP/t4/$L/index-fabric.tsv" ]; then
        pass "kaynak verilmeden depo kökündeki dist/mods/link bulunuyor"
    else
        fail "dist/mods/link yukarı doğru bulunamadı: $(cat "$TMP/out")"
    fi

    # Gerçek dist/mods/link (mod derleyen işler üretir): tam ise bölüm onunla
    # da geçmeli; yarımsa (işler sürüyor) atlanır.
    REAL="$ROOT/dist/mods/link"
    if [ -s "$REAL/index-fabric.tsv" ] && [ -s "$REAL/index-paper.tsv" ]; then
        if MCOS_MODS_SRC="$ROOT/dist/mods" TARGET_DIR="$TMP/tr" BINARIES_DIR="$TMP/br" \
            sh "$TMP/bolum.sh" > "$TMP/out" 2>&1; then
            pass "gerçek dist/mods/link imaja hazırlanabiliyor ($(ls "$TMP/tr/$L" | wc -l) dosya rootfs'te, $(du -sk "$TMP/tr/$L" | cut -f1) KB)"
        else
            fail "gerçek dist/mods/link eksik ya da bozuk: $(cat "$TMP/out")"
        fi
    else
        echo "  --   gerçek dist/mods/link henüz iki indeksi taşımıyor (make mod); denetim atlandı"
    fi
fi

echo "== 3. Düğüm paketi dist/mods/link'i programın yanına koyuyor =="

if command -v make >/dev/null 2>&1; then
    mklink "$TMP/sahte-kok/dist/mods/link"
    mkdir -p "$TMP/paket"
    if (cd "$TMP/sahte-kok" && make -s -f "$MAKEFILE" node-jars NODE_JAR_DIR="$TMP/paket" > "$TMP/out" 2>&1); then
        ok=1
        for f in index-fabric.tsv index-paper.tsv mcos-link-fabric-26.1-26.3.jar \
            mcos-link-paper.jar fabric-api-0.161.0+26.3.jar; do
            cmp -s "$TMP/sahte-kok/dist/mods/link/$f" "$TMP/paket/mods/link/$f" || { ok=0; echo "     eksik: $f"; }
        done
        [ "$ok" = 1 ] && pass "make node-jars indeks + jar'ları paket/mods/link'e koydu" ||
            fail "make node-jars eksik kopyaladı: $(cat "$TMP/out")"
    else
        fail "make node-jars başarısız: $(cat "$TMP/out")"
    fi
    mklink "$TMP/sahte-kok2/dist/mods/link"; rm "$TMP/sahte-kok2/dist/mods/link/index-paper.tsv"
    mkdir -p "$TMP/paket2"
    if (cd "$TMP/sahte-kok2" && make -s -f "$MAKEFILE" node-jars NODE_JAR_DIR="$TMP/paket2" > "$TMP/out" 2>&1); then
        fail "Paper indeksi eksikken düğüm paketi yine oluştu"
    elif grep -q "index-paper.tsv yok" "$TMP/out"; then
        pass "indeks eksikse düğüm paketi oluşmuyor ve nedeni söylüyor"
    else
        fail "indeks eksikken beklenmedik çıktı: $(cat "$TMP/out")"
    fi
    mklink "$TMP/sahte-kok3/dist/mods/link"; rm "$TMP/sahte-kok3/dist/mods/link/fabric-api-0.161.0+26.2.jar"
    mkdir -p "$TMP/paket3"
    if (cd "$TMP/sahte-kok3" && make -s -f "$MAKEFILE" node-jars NODE_JAR_DIR="$TMP/paket3" > "$TMP/out" 2>&1); then
        fail "indeksin gösterdiği fabric-api eksikken düğüm paketi yine oluştu"
    elif grep -q "fabric-api-0.161.0+26.2.jar" "$TMP/out"; then
        pass "indeksin gösterdiği dosya eksikse paket oluşmuyor ve hangisi olduğunu söylüyor"
    else
        fail "eksik dosyada beklenmedik çıktı: $(cat "$TMP/out")"
    fi
    # Satır sonu olmayan son satır da denetlenmeli (bkz. bölüm 2).
    mklink "$TMP/sahte-kok4/dist/mods/link"
    printf 'paper\t26.5\tmcos-link-paper-26.5.jar\t-' >> "$TMP/sahte-kok4/dist/mods/link/index-paper.tsv"
    mkdir -p "$TMP/paket4"
    if (cd "$TMP/sahte-kok4" && make -s -f "$MAKEFILE" node-jars NODE_JAR_DIR="$TMP/paket4" > "$TMP/out" 2>&1); then
        fail "satır sonu olmayan son satırın jar'ı eksikken düğüm paketi yine oluştu"
    elif grep -q "mcos-link-paper-26.5.jar" "$TMP/out"; then
        pass "satır sonu olmayan son satırın eksik jar'ı da paketi durduruyor"
    else
        fail "satır sonu olmayan son satırda beklenmedik çıktı: $(cat "$TMP/out")"
    fi
else
    echo "  --   make yok; paket denetimi atlandı"
fi

echo ""
if [ "$fails" -gt 0 ]; then
    echo "$fails denetim başarısız."
    exit 1
fi
echo "Tüm denetimler geçti."
