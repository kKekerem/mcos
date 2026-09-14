#!/bin/sh
# test-ui-logic.sh — fare/touchpad, animasyonlar, açılış ekranı ve kilit.
#
# ── Neden bu test var ───────────────────────────────────────────────────────
# Bu özelliklerin çoğu ancak GERÇEK DONANIMDA fark edilir: fare yoksa imleç
# görünmez, açılış ekranı yalnızca açılışta çalışır, kilit ekranı yalnızca
# parola kuruluysa gelir. Bir gerileme (regression) aylarca fark edilmez.
#
# Bu betik, özelliklerin gerçekten BAĞLI olduğunu koddan doğrular: bir
# fonksiyonun var olması yetmez, ÇAĞRILIYOR olması gerekir.
#
# Donanıma dokunmaz; yalnızca kaynak okur.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

need() {
    [ -f "$1" ] || { echo "eksik dosya: $1" >&2; exit 2; }
}

# has, yorumları atarak kaynakta bir kalıp arar.
#
# Yorumları atmak ŞART: bir özelliği ANLATAN yorum, o özelliğin ÇALIŞTIĞI
# anlamına gelmez.
has() { # dosya kalıp
    sed 's://.*::' "$1" | grep -q "$2"
}

EVDEV="$ROOT/internal/fbinput/evdev_linux.go"
ACT="$ROOT/internal/fbinput/activity_linux.go"
PTR="$ROOT/internal/fbpanel/pointer.go"
ANIM="$ROOT/internal/fbpanel/anim.go"
DRAW="$ROOT/internal/fbpanel/draw.go"
RUN="$ROOT/internal/fbpanel/run.go"
LOCK="$ROOT/internal/fbpanel/lock.go"
SETUP="$ROOT/internal/fbpanel/setup.go"
MAIN="$ROOT/cmd/mcos-panel-fb/main.go"
SPLASH="$ROOT/cmd/mcos-splash/main.go"
LAUNCH="$ROOT/os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-launch"
MK="$ROOT/os/buildroot/external/package/mcos/mcos.mk"
CFG="$ROOT/internal/model/config.go"

for f in "$EVDEV" "$ACT" "$PTR" "$ANIM" "$DRAW" "$RUN" "$LOCK" "$SETUP" \
         "$MAIN" "$SPLASH" "$LAUNCH" "$MK" "$CFG"; do
    need "$f"
done

echo "== 1. Fare / touchpad =="

if has "$EVDEV" 'eviocgbit'; then
    pass "aygıt yetenekleri çekirdeğe SORULUYOR (EVIOCGBIT)"
else
    fail "yetenek sorgusu yok — aygıt adına bakmak güvenilmez"
fi

if has "$EVDEV" 'absMTPosX' && has "$EVDEV" 'eviocgabs'; then
    pass "touchpad mutlak eksen aralığı okunuyor"
else
    fail "ABS eksen aralığı okunmuyor — touchpad hassasiyeti yanlış olur"
fi

if has "$ACT" 'func (a \*Activity) Pointer()'; then
    pass "imleç olayları tek okuyucudan yayımlanıyor"
else
    fail "Pointer kanalı yok"
fi

# Aynı evdev dosyasını iki kez açmamak: ayrı bir izleyici kurulmuş olsaydı
# olaylar ikiye bölünür ve uyku sayacı bozulurdu.
if [ "$(grep -c 'filepath.Glob("/dev/input/event\*")' "$ACT")" = "1" ] &&
   ! grep -rq 'filepath.Glob("/dev/input/event\*")' "$ROOT/internal/fbpanel" 2>/dev/null; then
    pass "evdev aygıtları TEK yerde açılıyor"
else
    fail "evdev birden çok yerde açılıyor"
fi

if has "$PTR" 'func (a \*App) Pointer(ev fbinput.PointerEvent)'; then
    pass "panel imleç olaylarını işliyor"
else
    fail "panel imleç olaylarını işlemiyor"
fi

if has "$RUN" 'case ev, ok := <-host.Pointer()'; then
    pass "ana döngü imleç kanalını dinliyor"
else
    fail "ana döngü imleç kanalını DİNLEMİYOR — fare hiç çalışmaz"
fi

if has "$MAIN" 'func (c \*console) Pointer()'; then
    pass "konsol ana makinesi imleci sağlıyor"
else
    fail "konsol Pointer() sağlamıyor"
fi

if has "$PTR" 'func (a \*App) pointerAllowed'; then
    pass "fare/touchpad ayrı ayrı kapatılabiliyor"
else
    fail "aygıt sınıfı başına açma/kapama yok"
fi

echo "== 2. Ayarlar =="

for key in 'Mouse ' 'Touchpad ' 'TapToClick ' 'PointerSpeed ' 'Animations ' \
           'BootAnimation '; do
    if has "$CFG" "$key"; then
        pass "yapılandırmada $key alanı var"
    else
        fail "yapılandırmada $key alanı YOK"
    fi
done

if has "$ROOT/internal/fbpanel/screen_settings.go" 'setPointer'; then
    pass "Ayarlar ekranında fare satırı var"
else
    fail "Ayarlar ekranında fare satırı yok"
fi

echo "== 3. Animasyonlar ve geçişler =="

if has "$ANIM" 'func (a \*App) beginTransition'; then
    pass "geçiş motoru var"
else
    fail "geçiş motoru yok"
fi

if has "$ROOT/internal/fbpanel/app.go" 'a.beginTransition(kind)'; then
    pass "bölüm değişimi geçiş başlatıyor"
else
    fail "bölüm değişiminde geçiş yok"
fi

if has "$DRAW" 'a.applyTransition(main)'; then
    pass "çizim sonunda geçiş uygulanıyor"
else
    fail "geçiş uygulanmıyor — animasyon hiç görünmez"
fi

if has "$ANIM" 'if !a.animationsOn()'; then
    pass "animasyonlar kapatılabiliyor (bellek de ayrılmıyor)"
else
    fail "animasyonlar kapatılamıyor"
fi

if has "$ROOT/internal/fbdraw/effects.go" 'func ZoomBlurFade'; then
    pass "yakınlaş+bulanıklaş geçişi var"
else
    fail "zoom/blur geçişi yok"
fi

echo "== 4. Açılış ekranı =="

if has "$SPLASH" 'LogoPulse' && has "$SPLASH" 'ProgressRing'; then
    pass "açılış ekranı logo ve ilerleme halkası çiziyor"
else
    fail "açılış ekranı eksik"
fi

if has "$SPLASH" 'fbdev.SaveFrame(canvas, save)'; then
    pass "açılış ekranı son karesini kaydediyor"
else
    fail "son kare kaydedilmiyor — panel geçişi çalışmaz"
fi

if has "$MAIN" 'fbdev.LoadFrame(o.intro'; then
    pass "panel açılış karesinden geçiş yapıyor"
else
    fail "panel açılış karesini okumuyor"
fi

if has "$MAIN" 'app.BeginIntro(fr)'; then
    pass "panel yakınlaşma animasyonunu başlatıyor"
else
    fail "BeginIntro çağrılmıyor"
fi

if grep -q 'mcos-splash' "$LAUNCH"; then
    pass "başlatıcı açılış ekranını çalıştırıyor"
else
    fail "başlatıcı açılış ekranını çalıştırmıyor"
fi

if grep -q 'finish_splash' "$LAUNCH"; then
    pass "başlatıcı paneli açmadan önce splash'i bitiriyor"
else
    fail "splash ile panel aynı anda framebuffer'a yazabilir"
fi

if grep -q 'restore_console' "$LAUNCH"; then
    pass "panel açılmazsa konsol geri alınıyor"
else
    fail "hata yolunda konsol grafik kipinde kalır (kara ekran)"
fi

if grep -q 'mcos-splash' "$MK"; then
    pass "açılış ekranı imaja kuruluyor"
else
    fail "mcos-splash imaja kurulmuyor"
fi

echo "== 5. İsteğe bağlı parola =="

if has "$ROOT/internal/model/password.go" 'pbkdf2.Key'; then
    pass "parola PBKDF2 ile özetleniyor"
else
    fail "parola özeti PBKDF2 değil"
fi

if has "$ROOT/internal/model/password.go" 'subtle.ConstantTimeCompare'; then
    pass "karşılaştırma sabit süreli"
else
    fail "parola karşılaştırması zamanlama sızdırıyor"
fi

if has "$LOCK" 'if !a.PasswordRequired()'; then
    pass "parola yoksa kilit ekranı hiç gelmiyor (isteğe bağlı)"
else
    fail "kilit isteğe bağlı değil"
fi

if has "$ROOT/internal/fbpanel/keys.go" 'if a.Locked()'; then
    pass "kilitliyken tuşlar panele ulaşmıyor"
else
    fail "kilitliyken kısayollar çalışıyor — kilit atlanabilir"
fi

if has "$RUN" 'a.lockAfterWake()'; then
    pass "uykudan uyanınca yeniden kilitlenebiliyor"
else
    fail "uyku kilidi atlamanın yolu olur"
fi

echo "== 6. Kurulum sihirbazı =="

if has "$SETUP" 'stepFeatures'; then
    pass "sihirbazda özellik açma/kapama adımı var"
else
    fail "özellik adımı yok"
fi

# Kullanıcının bildirdiği mantık hatası: OOBE'de eşleştirme OLMAMALI.
if has "$SETUP" 'ClusterPeers()' || has "$SETUP" 'ClusterScan()' ||
   has "$SETUP" 'ClusterPairManual('; then
    fail "sihirbaz hâlâ eşleştirme yapıyor — ilk kurulumda eş YOKTUR"
else
    pass "sihirbaz eşleştirme yapmıyor, yalnızca açıp kapatıyor"
fi

if has "$SETUP" 'next.SetupComplete = true'; then
    pass "sihirbaz kurulumu tamamlandı olarak işaretliyor"
else
    fail "SetupComplete yazılmıyor — sihirbaz her açılışta gelir"
fi

# Ayarlar ÖNCE kaydedilmeli, kurulum SONRA.
save_line="$(sed 's://.*::' "$SETUP" | grep -n 'a.cl.UpdateConfig(next)' | head -1 | cut -d: -f1)"
inst_line="$(sed 's://.*::' "$SETUP" | grep -n 'runHelper("mcos-install"' | head -1 | cut -d: -f1)"
if [ -n "$save_line" ] && [ -n "$inst_line" ] && [ "$save_line" -lt "$inst_line" ]; then
    pass "ayarlar diske kurulumdan ÖNCE kaydediliyor"
else
    fail "kurulum, ayarlar kaydedilmeden çalışıyor — girilenler kaybolur"
fi

if has "$RUN" 'if a.SetupNeeded()'; then
    pass "ilk açılışta sihirbaz kendiliğinden geliyor"
else
    fail "sihirbaz ilk açılışta gelmiyor"
fi


# ─────────────────────────────────────────────────────────────────────────────
# ANA DÖNGÜDE RPC OLMAMALI
# ─────────────────────────────────────────────────────────────────────────────
#
# ── Yakalanan gerçek hata ────────────────────────────────────────────────────
# ipc.Client bütün çağrıları TEK bir kilitle sıraya dizer. Bir ağ taraması
# sürerken o kilit 25 saniyeye kadar tutulu kalır. Bu yüzden ana döngüde
# yapılan HERHANGİ bir RPC, tarama sürerken paneli tamamen dondurur: çizim
# durur, tuşlar işlenmez, dönen tarama göstergesi tam da beklerken donar.
#
# Yedi yerde bu hata vardı: "r" tuşuyla yenileme, tema değiştirme, fare ve
# dokunmatik yüzey ayarları, parola, uykuda kilit, PC paylaşımı ve eş
# eşleştirme onayı. Hepsi kısa çağrılar oldukları için ancak tarama
# sırasında görünüyorlardı — yani panelin en çok kullanıldığı anda.
#
# Bu denetim, kuralın tek tek hatırlanmasına gerek bırakmaz.
#
# ana-donguda-rpc
echo ""
echo "ana döngüde RPC yok"

# Yalnızca ARKA PLANDAN çağrılan fonksiyonlar. İçlerinde doğrudan RPC
# olabilir, çünkü kendileri zaten bir goroutine içinde çalışır.
BG_FUNCS="loadSection refresh loadPeersSection loadTunnelSection pollPlayitClaim"

# Etkileşim dosyaları: tuş, fare ve modal geri çağrıları BURADA çalışır ve
# hepsi ana döngüdedir. (run.go dışarıda: Run() olay döngüsü BAŞLAMADAN önce
# bilerek senkron refresh yapar — o noktada donacak bir döngü yoktur.)
UI_FILES="keys.go pointer.go actions.go modal.go screen_settings.go
          screen_peers.go screen_tunnel.go screens.go wizard.go setup.go"

# NOT: aşağıdaki awk programları KAÇIŞ DİZİSİ KULLANMAZ. Kabuk katmanından
# geçerken "\." gibi diziler düz noktaya dönüşüyordu ve ".loadSection()"
# kalıbı "loadSectionAsync()" ile de eşleşiyordu — yani denetim, düzeltilmiş
# kodu hatalı sanıyordu. Onun yerine tam metin araması (index) ve köşeli
# parantez ([{]) kullanılıyor: ikisi de kaçış istemez.

# goroutine dışında kalan .cl. çağrılarını kapsayan fonksiyon adıyla listele.
offenders=""
for f in $UI_FILES; do
    p="$ROOT/internal/fbpanel/$f"
    [ -f "$p" ] || continue
    out=$(awk '
        /^func / { fn = $0
                   sub(/^func [(][^)]*[)] /, "", fn)
                   sub(/[(].*/, "", fn) }
        {
            line = $0
            sub(/[/][/].*/, "", line)
            if (index(line, "go func(") > 0) { gd[++gi] = depth }
            if (index(line, ".cl.") > 0 && gi == 0) print fn ":" FNR
            n = gsub(/[{]/, "&", line); m = gsub(/[}]/, "&", line)
            depth += n - m
            while (gi > 0 && depth <= gd[gi]) gi--
        }
    ' "$p")
    for hit in $out; do
        fn=${hit%%:*}
        ln=${hit##*:}
        case " $BG_FUNCS " in
            *" $fn "*) ;;
            *) offenders="$offenders $f:$ln($fn)" ;;
        esac
    done
done

if [ -n "$offenders" ]; then
    fail "ana döngüde doğrudan RPC:$offenders"
else
    pass "etkileşim yollarında doğrudan RPC yok"
fi

# Arka plan fonksiyonları da etkileşim yollarından SENKRON çağrılmamalı;
# yoksa RPC'yi bir katman aşağı taşıyıp aynı donmayı yaşardık.
bad=""
for f in $UI_FILES; do
    p="$ROOT/internal/fbpanel/$f"
    [ -f "$p" ] || continue
    for fn in $BG_FUNCS; do
        out=$(awk -v want="$fn" '
            /^func / { self = $0
                       sub(/^func [(][^)]*[)] /, "", self)
                       sub(/[(].*/, "", self) }
            {
                line = $0
                sub(/[/][/].*/, "", line)
                if (index(line, "go func(") > 0) { gd[++gi] = depth }
                if (index(line, "." want "()") > 0 && gi == 0 && self != want)
                    print self ":" FNR
                n = gsub(/[{]/, "&", line); m = gsub(/[}]/, "&", line)
                depth += n - m
                while (gi > 0 && depth <= gd[gi]) gi--
            }
        ' "$p")
        for hit in $out; do
            caller=${hit%%:*}
            ln=${hit##*:}
            case " $BG_FUNCS " in
                *" $caller "*) ;;
                *) bad="$bad $f:$ln($fn<-$caller)" ;;
            esac
        done
    done
done

if [ -n "$bad" ]; then
    fail "arka plan fonksiyonu ana döngüden senkron çağrılıyor:$bad"
else
    pass "arka plan fonksiyonları yalnızca goroutine'den çağrılıyor"
fi

# Merkezi yardımcı durmalı: ayar yazan altı yer ona bağlı.
if has "$RUN" "func (a \*App) saveConfigAsync"; then
    pass "yapılandırma yazımı için merkezi asenkron yardımcı var"
else
    fail "saveConfigAsync yok — ayar kaydetme ana döngüyü kilitler"
fi

echo ""
if [ "$fails" -eq 0 ]; then
    printf '\033[32mARAYÜZ MANTIĞI TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
