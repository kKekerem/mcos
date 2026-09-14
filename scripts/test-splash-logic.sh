#!/bin/sh
# test-splash-logic.sh — açılış ekranının ZAMANLAMASINI koruyan test.
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN VAR
# ════════════════════════════════════════════════════════════════════════════
#
# Kullanıcının bildirdiği arıza: "VirtualBox'ta GRUB menüsü geliyor, Enter'a
# basıyorum, sonra siyah ekran."
#
# QEMU'da (donanım hızlandırması KAPALI — kullanıcının sanal makinesiyle aynı
# koşul) gerçek ISO ölçüldü:
#
#     t=2..6 sn    GRUB menüsü görünüyor
#     t=8..20 sn   TAMAMEN SİYAH EKRAN        <-- şikâyet edilen yer
#     t=25 sn      açılış animasyonu nihayet başlıyor
#     t=42 sn      panel
#
# Siyahlığın iki bağımsız sebebi vardı ve ikisi de bu testle korunuyor:
#
#  1. AÇILIŞ ANİMASYONU ÇOK GEÇ BAŞLIYORDU. Onu yalnızca mcos-launch
#     başlatıyordu; o da inittab'deki "tty2::respawn" girdisinden gelir ve
#     busybox init respawn girdilerini BÜTÜN sysinit girdileri (yani rcS'in
#     tamamı) bittikten sonra çalıştırır. Ölçülen dağılım:
#
#         S01seedrng 0.14  S01syslogd 0.14  S02klogd 0.14  S02sysctl 0.23
#         S40network 0.62  S50sshd 3.77     S99mcos 1.69
#
#     Artık etc/init.d/S04splash animasyonu rcS'in BAŞINDA başlatıyor.
#
#  2. ÇEKİRDEK HİÇBİR ŞEY YAZMIYORDU. loglevel=4 yalnızca KERN_ERR ve üstünü
#     basar; normal bir açılışta o şiddette tek mesaj yoktur. Çekirdek
#     çerçeve arabelleği konsoluna geçtiği an ekranı temizler ve userspace'e
#     kadar boş bırakır. Yani siyahlık bir arıza değil TASARIM GEREĞİ
#     sessizlikti — ama kullanıcı bunu ayırt edemez.
#
# Ölçülen sonuç: siyah pencere 12 saniyeden SIFIRA indi, panel 42 saniyeden
# ~13 saniyeye.
#
# Bu test hiçbir şey ÇALIŞTIRMAZ ve hiçbir diske dokunmaz; yalnızca kaynak
# dosyaları okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
OVERLAY="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay"
SPLASH="$OVERLAY/etc/init.d/S04splash"
DATASH="$OVERLAY/etc/init.d/S03mcosdata"
MCOSSH="$OVERLAY/etc/init.d/S99mcos"
LAUNCH="$OVERLAY/usr/bin/mcos-launch"
POSTBUILD="$ROOT/os/buildroot/external/board/mcos/post-build.sh"
DISPLAY_LIB="$ROOT/scripts/lib/display.sh"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

for f in "$SPLASH" "$DATASH" "$MCOSSH" "$LAUNCH" "$POSTBUILD" "$DISPLAY_LIB"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

# Yorumları at: bu dosyalarda eski davranışı ANLATAN açıklama satırları var ve
# onlar eşleşmemeli.
code() { sed 's/[[:space:]]*#.*$//' "$1"; }

echo "== 1. Açılış animasyonu rcS'in BAŞINDA =="

# Sayı önemli: rcS betikleri S??* kalıbıyla SIRALI çalışır. Animasyon,
# ağ (S40) ve MCOS (S99) servislerinden ÖNCE gelmeli, yoksa gecikme geri
# döner.
case "$(basename "$SPLASH")" in
    S0*) pass "animasyon betiği S0x (ağdan ve MCOS'tan önce)" ;;
    *) fail "animasyon betiği rcS'te geç sırada: $(basename "$SPLASH")" ;;
esac

if [ -x "$SPLASH" ] || grep -q 'S04splash' "$POSTBUILD"; then
    pass "S04splash post-build.sh'te çalıştırılabilir yapılıyor"
else
    fail "S04splash post-build.sh listesinde yok — imajda çalıştırılamaz olur"
fi

if grep -q 'S03mcosdata' "$POSTBUILD"; then
    pass "S03mcosdata post-build.sh listesinde"
else
    fail "S03mcosdata post-build.sh listesinde yok"
fi

echo "== 2. Animasyon ARKA PLANDA başlatılıyor =="

# Ön planda başlatılırsa sysinit orada takılır ve MCOS hiç açılmaz.
if code "$SPLASH" | grep -qE '^\s*"\$SPLASH"|mcos-splash' && \
   code "$SPLASH" | grep -q '&\s*$'; then
    pass "mcos-splash arka plana atılıyor (& ile)"
else
    fail "mcos-splash arka plana atılmıyor — rcS burada bloklanır"
fi

if code "$SPLASH" | grep -q 'chvt'; then
    pass "panel VT'si ön plana alınıyor (chvt)"
else
    fail "chvt yok — animasyon çizilir ama etkin konsol üstüne yazar"
fi

echo "== 3. mcos-launch ikinci bir animasyon BAŞLATMIYOR =="

if grep -q 'adopt_early_splash' "$LAUNCH"; then
    pass "mcos-launch erken animasyonu devralıyor (adopt_early_splash)"
else
    fail "mcos-launch erken animasyonu devralmıyor — iki animasyon çakışır"
fi

if grep -q 'SPLASH_PIDFILE' "$LAUNCH" && grep -q 'mcos-splash.pid' "$SPLASH"; then
    pass "PID dosyası iki tarafta da aynı (kesin kapatma)"
else
    fail "PID dosyası uyuşmuyor — kapatma pkill'e düşer"
fi

# finish_splash HER yolda çağrılmalı: panel açılsa da açılmasa da animasyon
# kapatılmazsa VT grafik kipinde kalır ve kurtarma menüsü GÖRÜNMEZ.
n="$(grep -c '^\s*finish_splash\s*$' "$LAUNCH" || true)"
if [ "${n:-0}" -ge 3 ]; then
    pass "finish_splash her çıkış yolunda çağrılıyor ($n yer)"
else
    fail "finish_splash yalnızca $n yerde — bir yol animasyonu açık bırakır"
fi

echo "== 4. Zaman aşımı, yavaş bir sanal makineyi kaldırıyor =="

# Ölçüldü: donanım hızlandırması olmayan bir makinede panel 42. saniyede
# açılabiliyordu. 45 saniyelik eski zaman aşımı dolduğunda animasyon çıkar,
# konsolu metin kipine döndürür ve kullanıcı boş bir konsola bakar.
if code "$LAUNCH" | grep -qE -- '--timeout 45s'; then
    fail "mcos-launch hâlâ 45 sn zaman aşımı kullanıyor (yavaş VM'de dolar)"
else
    pass "mcos-launch zaman aşımı 45 sn değil"
fi

if code "$SPLASH" | grep -qE 'SPLASH_TIMEOUT="1[0-9][0-9]s"|SPLASH_TIMEOUT="[2-9][0-9][0-9]s"'; then
    pass "S04splash zaman aşımı >= 100 sn"
else
    fail "S04splash zaman aşımı çok kısa"
fi

echo "== 5. Çekirdek açılışı GÖZLEMLENEBİLİR =="

if grep -qE '^MCOS_CMDLINE_BASE=.*loglevel=[6-7]' "$DISPLAY_LIB"; then
    pass "loglevel >= 6: açılış kaydı ekranda akıyor (siyah ekran değil)"
else
    fail "loglevel < 6: çekirdek hiçbir şey yazmaz, ekran siyah kalır"
fi

echo "== 6. Her açılışta SSH anahtarı üretilmiyor =="

# Ölçüldü: S50sshd her açılışta "ssh-keygen -A" ile 3.77 saniye yiyordu ve
# canlı sistemde /etc RAM'de olduğu için anahtarlar kalıcı bile değildi.
# Ayrıca SSH artık daemon'a ait (internal/sshd): iki sshd aynı portu
# dinleyemez ve panel "SSH kapalı" derken sistemde stok bir sshd çalışırdı.
if grep -q 'rm -f "${TARGET_DIR}/etc/init.d/S50sshd"' "$POSTBUILD"; then
    pass "openssh'in kendi açılış betiği imajdan kaldırılıyor"
else
    fail "S50sshd imajda kalıyor — her açılışta ~3.8 sn ve daemon ile çakışma"
fi

echo "== 7. Kalıcı depolama animasyondan ÖNCE bağlanıyor =="

# Animasyonun "kapalı mı" ayarı /data/config.json içindedir; /data bağlı
# değilken okunamaz ve kullanıcının kapattığı animasyon yine çalışırdı.
if [ "$(basename "$DATASH")" \< "$(basename "$SPLASH")" ]; then
    pass "S03mcosdata, S04splash'ten önce çalışıyor"
else
    fail "kalıcı depolama animasyondan sonra bağlanıyor"
fi

if grep -q '/data/config.json' "$SPLASH" && grep -q '/data/config.json' "$LAUNCH"; then
    pass "iki taraf da aynı yapılandırma yolunu okuyor"
else
    fail "animasyon ayarının yolu iki dosyada farklı"
fi

echo "== 8. S99mcos'taki boşuna bekleme kaldırıldı =="

if code "$MCOSSH" | grep -qE '^\s*sleep 1\s*$'; then
    fail "S99mcos hâlâ 'sleep 1' ile bekliyor (her açılışta bir saniye)"
else
    pass "S99mcos gereksiz beklemeden arınmış"
fi

echo
if [ "$fails" -eq 0 ]; then
    printf '\033[32mTüm açılış ekranı testleri geçti.\033[0m\n'
else
    printf '\033[31m%d test başarısız.\033[0m\n' "$fails"
    exit 1
fi
