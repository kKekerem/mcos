#!/bin/sh
# start-qemu.sh — MCOS'u QEMU'da DONANIM HIZLANDIRMALI aç.
#
# Tek iş: çalıştır, MCOS açılsın.
#
#     sh start-qemu.sh
#
# ── Neden kökte ayrı bir dosya ──────────────────────────────────────────────
#
# Asıl iş scripts/qemu.sh'de (izin düzeltme, KVM doğrulama, ekran/ses/port
# ayarları hepsi orada ve tek yerde duruyor). Bu dosya yalnızca onu çağırıyor,
# çünkü kullanıcının isteği "açınca açılsın"dı: dosya kökte, adı belli ve
# hiçbir seçenek gerektirmiyor.
#
# ── Bu betik DERLEME YAPMAZ ─────────────────────────────────────────────────
#
# Var olan dist/mcos-x86_64.iso'yu açar. İmaj yoksa ne yapılacağını söyler.
# (make qemu ise "iso" -> "os" -> Buildroot zincirine bağlıdır ve saatlerce
# derleme başlatabilir; "hemen açılsın" isteğiyle bağdaşmaz.)
#
# HİÇBİR AYGITA YAZMAZ.
set -u

KOK="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

# Çift tıklayarak açıldıysa pencere hata mesajını gösteremeden kapanır.
# Hata varsa bekleyip okunmasını sağlıyoruz.
bekle_ve_cik() {
    kod=$1
    if [ "$kod" -ne 0 ] && [ -t 0 ]; then
        printf '\n>> Bir sorun çıktı. Kapatmak için Enter.\n'
        read -r _ || true
    fi
    exit "$kod"
}

sh "$KOK/scripts/qemu.sh" "$@"
bekle_ve_cik $?
