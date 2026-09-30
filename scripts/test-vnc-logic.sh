#!/bin/sh
# test-vnc-logic.sh — ekran paylaşımının (VNC) GÜVENLİK ve BAĞLANTI
# sözleşmesini kilitleyen test.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN VAR
# ════════════════════════════════════════════════════════════════════════════
#
# VNC, panelin TAM DENETİMİNİ ağa açar: uzaktaki kullanıcı ekranı görür,
# klavyeyi ve fareyi kullanır. Üstelik RFB trafiği ŞİFRESİZDİR.
#
# Bu yüzden üç kural pazarlığa kapalıdır ve burada kilitleniyor:
#
#   1. Parolasız sunucu AÇILMAZ.
#   2. "None" güvenlik türü HİÇ SUNULMAZ (istemci onu seçebilseydi parola
#      devre dışı kalırdı).
#   3. Varsayılan KAPALI.
#
# Ayrıca bağlantının çalışması için gereken iki çekirdek/kod ayarı da
# denetleniyor: uinput (girdi) ve çerçeve arabelleği okuma (ekran).
#
# Bu test hiçbir şey çalıştırmaz, hiçbir porta bağlanmaz; kaynak okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VNCDIR="$ROOT/internal/vnc"
KCFG="$ROOT/os/buildroot/external/board/mcos/kernel.config"
MODEL="$ROOT/internal/model/config.go"
DAEMON="$ROOT/internal/daemon/handlers_vnc.go"
PANEL="$ROOT/internal/fbpanel/screen_vnc.go"
CAPTURE="$ROOT/internal/fbdev/capture_linux.go"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$VNCDIR/server.go" "$VNCDIR/auth.go" "$VNCDIR/session.go" \
         "$KCFG" "$MODEL" "$DAEMON" "$PANEL" "$CAPTURE"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

echo "== 1. Parola ZORUNLU =="

if grep -q 'parola yok — ekran paylaşımı açılamaz' "$VNCDIR/server.go"; then
    pass "parolasiz sunucu kurulamiyor (New hata donuyor)"
else
    fail "parolasiz sunucu kurulabiliyor — agdaki herkes baglanabilir"
fi

# "None" (güvenlik türü 1) listeye HİÇ konmamalı.
if grep -q 'bw.Write(\[\]byte{1, secTypeVNCAuth})' "$VNCDIR/server.go"; then
    pass "yalnizca VNC kimlik dogrulamasi sunuluyor"
else
    fail "guvenlik turu listesi degismis — 'None' sizabilir"
fi
if grep -vE '^[[:space:]]*(//|#)' "$VNCDIR/server.go" | grep -q 'secTypeNone'; then
    fail "kodda 'None' guvenlik turu geciyor"
else
    pass "kodda 'None' guvenlik turu yok"
fi

if grep -q 'subtle.ConstantTimeCompare' "$VNCDIR/server.go"; then
    pass "parola SABIT SUREDE karsilastiriliyor"
else
    fail "parola karsilastirmasi zamanlama sizdiriyor"
fi

echo
echo "== 2. Varsayilan KAPALI =="

if grep -q 'Enabled bool `json:"enabled,omitempty"`' "$MODEL"; then
    pass "VNCConfig.Enabled varsayilan false"
else
    fail "VNC yapilandirmasi varsayilan acik olabilir"
fi
if grep -q 'vncConfigured(d.Config())' "$ROOT/internal/daemon/daemon.go"; then
    pass "acilista yalnizca ACIKSA baslatiliyor"
else
    fail "acilista kosulsuz baslatiliyor olabilir"
fi

echo
echo "== 3. Parola uretimi =="

if grep -q 'const vncPasswordAlphabet' "$DAEMON"; then
    pass "parola alfabesi tanimli (okunakli karakterler)"
else
    fail "parola alfabesi yok"
fi
# RFB parolayı 8 bayta kırpar; daha uzun üretmek "yazdım ama kabul etmiyor"
# demekti.
if grep -q 'b := make(\[\]byte, 8)' "$DAEMON"; then
    pass "parola TAM 8 karakter (RFB siniri)"
else
    fail "parola uzunlugu 8 degil — RFB kirpar ve parola calismaz"
fi
if grep -q 'crypto/rand' "$DAEMON"; then
    pass "parola kriptografik rastgelelikten"
else
    fail "parola tahmin edilebilir kaynaktan uretiliyor"
fi

echo
echo "== 4. Baglantinin calismasi icin gerekenler =="

if grep -q '^CONFIG_INPUT_UINPUT=y' "$KCFG"; then
    pass "cekirdekte uinput acik (klavye/fare iletilebilir)"
else
    fail "uinput kapali — VNC yalnizca izleme kipinde kalir"
fi
if grep -q 'func (d \*Device) Snapshot' "$CAPTURE"; then
    pass "cerceve arabellegi OKUNABILIYOR (ekran yayinlanabilir)"
else
    fail "ekran okuma yolu yok"
fi
if grep -q 'DefaultPort = 5900' "$VNCDIR/server.go"; then
    pass "standart VNC portu (5900) — istemciye yalnizca IP yeter"
else
    fail "port standart degil; RealVNC'de elle port girmek gerekir"
fi

# Piksel biçimi: istemci düşük renk isterse UYULMALI, yoksa görüntü bozulur.
if grep -q 'format = want' "$VNCDIR/session.go"; then
    pass "istemcinin istedigi piksel bicimi uygulaniyor"
else
    fail "SetPixelFormat yok sayiliyor — RealVNC'de bozuk goruntu riski"
fi

echo
echo "== 5. Panel yuzeyi =="

if grep -q 'setVNC' "$ROOT/internal/fbpanel/screen_settings.go"; then
    pass "Ayarlar'da ekran paylasimi satiri var"
else
    fail "Ayarlar'da VNC satiri yok — kullanici acamaz"
fi
if grep -q 'ssh -L 5900:localhost:5900' "$PANEL"; then
    pass "sifresiz trafik uyarisi ve SSH tuneli onerisi ekranda"
else
    fail "kullaniciya sifreleme uyarisi verilmiyor"
fi
if grep -q 'Yalnızca izlemeye al' "$PANEL"; then
    pass "yalnizca-izleme kipi panelden acilabiliyor"
else
    fail "yalnizca-izleme kipi arayuzde yok"
fi

echo
if [ "$fails" -eq 0 ]; then
    printf '\033[32mEKRAN PAYLASIMI (VNC) TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
