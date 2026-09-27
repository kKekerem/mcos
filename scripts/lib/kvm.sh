# kvm.sh — QEMU donanım hızlandırmasının TEK tanımı.
#
# Bu dosya `.` ile kaynak alınır; çalıştırılabilir DEĞİLDİR (display.sh deseni).
#
# ── Neden tek yerde ─────────────────────────────────────────────────────────
#
# Aynı "kvm grubunda değilsin" metni üç ayrı yerde yazılıydı: Makefile'daki
# QEMU_ACCEL_CHECK makrosu, README ve GECIS.md. Üç kopya, bir gün üç ayrı
# gerçek demektir. Karar da metin de artık burada.
#
# ── Ölçülen gerçekler (varsayım değil) ──────────────────────────────────────
#
#   1. `-cpu host` TCG altında ÖLÜMCÜLDÜR:
#          "CPU model 'host' requires KVM or HVF"
#      Yani "-accel kvm -accel tcg" biçimindeki tek satırlık geri düşüş
#      zinciri İŞE YARAMAZ: hızlandırıcı geri düşer ama CPU modeli düşmez ve
#      QEMU yine çöker. ACCEL ve CPU bu yüzden HER ZAMAN birlikte atanır.
#
#   2. `sg kvm` / `newgrp kvm` kullanıcı gruba EKLENMEDEN önce grup parolası
#      sorar ve "Invalid password" ile düşer. Ekledikten sonra çalışır ama
#      komutu /bin/sh (dash) ile çalıştırır — yani bashism'ler patlar. Gereği
#      yok: setfacl aynı sonucu yeniden çalıştırmadan verir.
#
#   3. `sudo setfacl -m u:$USER:rw /dev/kvm` ANINDA etkilidir (/dev devtmpfs
#      ve ACL destekli) ama WSL yeniden başlayınca kaybolur — çünkü /dev her
#      açılışta sıfırdan kurulur. Kalıcılık ayrıca `usermod -aG kvm` ister.
#      İkisi TEK sudo isteminde yapılır: kullanıcı parolayı bir kez yazar.
#
#   4. /dev/kvm'in HİÇ OLMAMASI bir izin sorunu DEĞİLDİR: WSL2'de iç içe
#      sanallaştırma kapalıdır ve bu yalnızca Windows tarafından düzeltilir.
#      O durumda sudo istemek kullanıcıyı boşuna yorar.

# mcos_kvm_usable, KVM'in şu an KULLANILABİLİR olup olmadığını söyler.
mcos_kvm_usable() {
    [ -r /dev/kvm ] && [ -w /dev/kvm ]
}

# mcos_kvm_missing, aygıtın hiç bulunmadığını söyler (izin değil, yokluk).
mcos_kvm_missing() {
    [ ! -e /dev/kvm ]
}

# mcos_kvm_explain_missing, iç içe sanallaştırma kapalıyken ne yapılacağını yazar.
mcos_kvm_explain_missing() {
    cat <<'EOF'
>> /dev/kvm YOK — WSL2 iç içe sanallaştırma (nested virtualization) kapalı.
>>
>> Bu bir izin sorunu DEĞİL; Linux tarafından düzeltilemez. Windows'ta
>> %USERPROFILE%\.wslconfig dosyasına şunu ekleyin:
>>
>>     [wsl2]
>>     nestedVirtualization=true
>>
>> sonra PowerShell'de:  wsl --shutdown
>> ve WSL'i yeniden açın.
EOF
}

# mcos_kvm_grant, izni düzeltmeyi dener. Başarılıysa 0 döner.
#
# TEK sudo istemi: anlık düzeltme (setfacl) ve kalıcı düzeltme (usermod)
# birlikte çalışır. setfacl başarısız olsa bile usermod denenir — bazı
# çekirdek derlemelerinde devtmpfs POSIX ACL desteklemez.
mcos_kvm_grant() {
    user="$(id -un)"

    echo ">> /dev/kvm erişilemiyor — QEMU yazılım öykünmesine düşerdi."
    echo ">> Tek seferlik izin düzeltmesi için sudo parolası gerekebilir:"
    echo ">>     setfacl -m u:$user:rw /dev/kvm   (bu oturum için ANINDA)"
    echo ">>     usermod -aG kvm $user            (kalıcı, bir daha sorulmaz)"

    if sudo -n true 2>/dev/null; then
        SUDO="sudo -n"
    elif [ -t 0 ]; then
        SUDO="sudo"
    else
        # Uçbirim YOK (betik bir başka betikten ya da arka planda çalışıyor):
        # sudo parolayı okuyamaz ve anlaşılmaz bir hata basar. Sessizce
        # vazgeçip çağırana "olmadı" demek daha dürüst.
        echo ">> (uçbirim yok — izin düzeltmesi atlandı)"
        return 1
    fi

    # İkisi tek kabukta: parola bir kez sorulur.
    $SUDO sh -c "setfacl -m 'u:$user:rw' /dev/kvm 2>/dev/null || true
                 usermod -aG kvm '$user' 2>/dev/null || true" || true

    mcos_kvm_usable
}

# mcos_kvm_really_works, KVM'in GERÇEKTEN çalıştığını kanıtlar.
#
# ── Neden yetmez: [ -w /dev/kvm ] ───────────────────────────────────────────
#
# O test yalnızca open() iznini kanıtlar, KVM_CREATE_VM'in başarılı olacağını
# DEĞİL. Windows tarafında Hyper-V VMX'i tutuyorsa aygıt açılır ama sanal
# makine kurulamaz. 225 MB'lık ISO'yu yavaşça açmadan önce beş saniyede
# gerçeği söyleyen sınama budur.
#
# Çıkış kodu 124 (timeout) = QEMU hâlâ çalışıyordu = KVM başlatma BAŞARILI.
mcos_kvm_really_works() {
    timeout 5 qemu-system-x86_64 -accel kvm -m 64 \
        -display none -nodefaults -no-user-config -serial none -monitor none -S \
        </dev/null >/dev/null 2>&1
    [ $? -eq 124 ]
}

# mcos_kvm_accel, ACCEL ve CPU bayraklarını BİRLİKTE yazdırır.
#
# Çıktı tek satır: "-accel kvm -cpu host" ya da "-accel tcg,thread=multi -cpu max".
# İkisini ayrı ayrı kurmak, bir gün birini değiştirip ötekini unutmak demektir
# (bkz. yukarıdaki 1. ölçüm).
mcos_kvm_accel() {
    if mcos_kvm_usable; then
        printf -- '-accel kvm -cpu host'
    else
        printf -- '-accel tcg,thread=multi -cpu max'
    fi
}
