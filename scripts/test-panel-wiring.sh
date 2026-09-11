#!/bin/sh
# test-panel-wiring.sh — yeni framebuffer panelinin GERÇEKTEN bağlı olduğunu
# doğrular.
#
# ── NEDEN VAR ───────────────────────────────────────────────────────────────
# Yeni çizim motoru (internal/fbfont, fbdraw, fbui) yazıldı, testleri geçti ve
# ekran görüntüleri üretildi — ama HİÇBİR YERDEN ÇAĞRILMIYORDU. Kullanıcı ISO'yu
# derledi ve "hâlâ aynı menüler" dedi. Haklıydı: mcos-launch hâlâ fbterm + eski
# paneli başlatıyordu.
#
# Bu sınıftaki hata sessizdir: her şey derlenir, her test geçer, hiçbir uyarı
# çıkmaz — sadece yeni kod çalışmaz. Bu yüzden "bağlı mı?" sorusu ayrıca ve
# açıkça sorulmalıdır.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
LAUNCH="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-launch"
PKG="$ROOT/os/buildroot/external/package/mcos/mcos.mk"
MK="$ROOT/Makefile"
CMD="$ROOT/cmd/mcos-panel-fb/main.go"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$LAUNCH" "$PKG" "$MK" "$CMD"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

code() { sed 's/[[:space:]]*#.*$//' "$1"; }

echo "== 1. İkili derleniyor ve kuruluyor =="

if grep -q 'GO_CMDS :=.*mcos-panel-fb' "$MK"; then
    pass "Makefile mcos-panel-fb ikilisini derliyor"
else
    fail "Makefile GO_CMDS listesinde mcos-panel-fb yok — ikili hiç üretilmez"
fi

if grep -q 'mcos-panel-fb.*usr/bin/mcos-panel-fb' "$PKG"; then
    pass "Buildroot paketi ikiliyi rootfs'e kuruyor"
else
    fail "mcos.mk ikiliyi kurmuyor — imajda bulunmaz"
fi

echo "== 2. Açılış zinciri yeni paneli çağırıyor =="

if code "$LAUNCH" | grep -q '/usr/bin/mcos-panel-fb'; then
    pass "mcos-launch mcos-panel-fb çalıştırıyor"
else
    fail "mcos-launch yeni paneli HİÇ çağırmıyor — ISO eski arayüzü gösterir"
fi

# Yeni panel, eski panelden ÖNCE denenmeli.
new_line="$(code "$LAUNCH" | grep -n 'run_panel_fb$' | head -1 | cut -d: -f1)"
old_line="$(code "$LAUNCH" | grep -n 'run_panel_fbterm$' | tail -1 | cut -d: -f1)"
if [ -n "$new_line" ] && [ -n "$old_line" ] && [ "$new_line" -lt "$old_line" ]; then
    pass "yeni panel eski panelden önce deneniyor"
elif [ -z "$new_line" ]; then
    fail "ana döngüde run_panel_fb çağrısı yok"
else
    fail "eski panel yeni panelden önce çağrılıyor — yeni arayüz hiç görünmez"
fi

echo "== 3. Geri düşüş yolu duruyor =="

# Yeni panel çökerse kullanıcı kilitli kalmamalı.
if code "$LAUNCH" | grep -q 'run_panel_fbterm'; then
    pass "fbterm geri düşüşü korunmuş"
else
    fail "fbterm geri düşüşü kaldırılmış — yeni panel çökerse sistem kullanılamaz"
fi

if code "$LAUNCH" | grep -q 'MCOS_PANEL\|panel_mode'; then
    pass "kullanıcı eski panele geçebiliyor (MCOS_PANEL / panel.conf)"
else
    fail "eski panele dönüş yolu yok"
fi

# F12 ayrı bir çıkış koduyla bildirilmeli. Normal çıkışla aynı kodu
# döndürseydi mcos-launch farkı göremez, yeni paneli yeniden açar ve
# kullanıcı yeni arayüzde bir sorun olduğunda KİLİTLİ KALIRDI.
if grep -q 'exitLegacyPanel = 64' "$CMD"; then
    pass "F12 ayrı çıkış kodu (64) döndürüyor"
else
    fail "F12 için ayrı çıkış kodu yok — eski panele geçiş çalışmaz"
fi
if code "$LAUNCH" | grep -q 'rc" -eq 64'; then
    pass "mcos-launch 64 kodunu eski panel isteği olarak anlıyor"
else
    fail "mcos-launch 64 kodunu tanımıyor — F12 yeni paneli yeniden açar"
fi

# Panel HİÇ açılmıyorsa F12'ye basılamaz; menüden de geçiş şart.
if code "$LAUNCH" | grep -q 'mode=legacy' && code "$LAUNCH" | grep -q 'mode=auto'; then
    pass "kurtarma menüsünden iki yöne de geçiş var"
else
    fail "menüden arayüz değiştirilemiyor — panel açılmazsa çıkış yolu kalmaz"
fi

echo "== 4. Konsol geri veriliyor =="

# EN KRİTİK: grafik kipinde çıkılırsa makine kullanılamaz kalır.
if grep -q 'defer con.Restore()' "$CMD"; then
    pass "çıkışta konsol geri veriliyor (defer)"
else
    fail "defer con.Restore() yok — panel çıkınca konsol grafik kipinde kalır"
fi

if grep -q 'recover()' "$CMD"; then
    pass "panik durumunda da konsol geri veriliyor"
else
    fail "panik kurtarma yok — çökerse konsol kullanılamaz kalır"
fi

if grep -q 'signal.Notify' "$ROOT/internal/fbvt/console_linux.go"; then
    pass "sinyalde (SIGTERM/Ctrl+C) konsol geri veriliyor"
else
    fail "sinyal işleyicisi yok — öldürülen panel konsolu bozuk bırakır"
fi

echo "== 5. Yeni motor gerçekten kullanılıyor =="

# Panel kodu yeni çizim paketlerini içe aktarmalı; aksi halde ikili var ama
# eski görünümü çiziyor olabilir.
for pkg in fbui fbdraw fbfont fbvt fbinput; do
    if grep -rq "mcos/internal/$pkg" "$ROOT/cmd/mcos-panel-fb/" "$ROOT/internal/fbpanel/" 2>/dev/null; then
        pass "$pkg kullanılıyor"
    else
        fail "$pkg hiçbir yerden kullanılmıyor"
    fi
done

echo
if [ "$fails" -gt 0 ]; then
    printf '\033[31m%d test başarısız\033[0m\n' "$fails"
    exit 1
fi
printf '\033[32mpanel bağlantısı doğrulandı\033[0m\n'
