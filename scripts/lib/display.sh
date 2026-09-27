# display.sh — ekran çözünürlüğünün TEK tanımı.
#
# Bu dosya `.` ile kaynak alınır; çalıştırılabilir değildir.
#
# ── Neden tek yerde? ────────────────────────────────────────────────────────
# gfxmode üç ayrı yerde yazılıyordu (mkiso.sh, mkusb.sh, mcos-install) ve
# üçü de FARKLIYDI. Sonuç: ISO'dan 1024x768, USB'den "auto", kurulu diskten
# 1024x768 açılıyordu. Aynı makine, üç ayrı çözünürlük.
#
# ── Bu sistemde çözünürlük NASIL belirlenir? ────────────────────────────────
# Çekirdek yapılandırmasında gerçek GPU sürücüsü YOK (i915/amdgpu/nouveau
# derlenmiyor; yalnızca simpledrm, efifb, vesafb ve sanal makine sürücüleri
# var). Yani framebuffer'ı FIRMWARE kurar, çekirdek değil:
#
#   UEFI  -> GRUB, GOP (Graphics Output Protocol) modunu seçer ve
#            gfxpayload=keep sayesinde çekirdek onu OLDUĞU GİBİ devralır.
#   BIOS  -> GRUB, VESA/VBE modunu seçer; aynı devralma geçerlidir.
#
# Bunun iki sonucu var:
#   1. gfxmode listesi çözünürlüğü belirleyen ASIL ayardır.
#   2. Çalışırken çözünürlük DEĞİŞTİRİLEMEZ (modesetting sürücüsü yok).
#      Ekran ayarları ekranı bu yüzden grub.cfg'yi yazıp yeniden başlatır.
#
# ── Neden liste, tek değer değil? ───────────────────────────────────────────
# GRUB listeyi soldan sağa dener ve firmware'in GERÇEKTEN sunduğu ilk modu
# kullanır. Tek bir 1920x1080 yazsaydık, o modu sunmayan bir firmware'de
# GRUB metin moduna düşerdi. Liste, geniş ekranları önce deneyip her zaman
# "auto" ile biter.
#
# ── video= NEDEN YOK? ───────────────────────────────────────────────────────
# Eski mkiso.sh çekirdek komut satırına `video=1024x768` koyuyordu. Bu,
# GRUB 1920x1080 seçmiş olsa bile çözünürlüğü zorla 1024x768'e düşürüyordu:
# panelin düşük çözünürlükte açılmasının doğrudan sebebi buydu. Komut
# satırına video= KOYMAYIN.

# Kanonik mod listesi. Soldan sağa denenir.
MCOS_GFXMODE='1920x1080x32,1920x1080,1680x1050,1600x900,1440x900,1366x768,1280x1024,1280x800,1280x720,1024x768,auto'

# Ekran ayarları ekranında kullanıcıya sunulan modlar (grub.cfg'ye yazılır).
# Her girdi geçerli bir GRUB gfxmode olmalı; en sona her zaman auto eklenir.
MCOS_MODES='1920x1080 1680x1050 1600x900 1440x900 1366x768 1280x1024 1280x800 1280x720 1024x768'

# Ortak çekirdek komut satırı.
#
#   consoleblank=0            ekran koruyucu kapalı (sunucu paneli sürekli açık)
#   fbcon=nodefer             framebuffer konsolu hemen devral, boot mesajları görünsün
#   vt.global_cursor_default=0 imleç yanıp sönmesin (kendi arayüzümüzü çiziyoruz)
#   root= YOK                 sistem initramfs'ten çalışır; root= verilirse
#                             "VFS: Cannot open root device" paniği alınır
#
# ── loglevel NEDEN ARTIK 0 (quiet)? ────────────────────────────────────────
#
# Bu deger iki kez degisti ve ikisinin de sebebi ayni olcumdu.
#
# ONCE loglevel=4 vardi. Olculdu (QEMU, donanim hizlandirmasiz — kullanicinin
# VirtualBox'i da oyle):
#
#   t=2..6 sn    GRUB menusu
#   t=8..20 sn   TAMAMEN SIYAH EKRAN   <-- "enter'a basiyorum sonra siyah ekran"
#   t=25 sn      acilis animasyonu
#
# loglevel=4 normal bir acilista hicbir sey basmaz; siyahlik tasarim geregi
# sessizlikti ama kullanici bunu makinenin donmasindan ayirt edemiyordu.
# Cozum olarak loglevel=6 yapildi: ekranda cekirdek kaydi akiyordu, yani
# "makine calisiyor" gorunuyordu.
#
# SIMDI o bandaja gerek kalmadi. Kullanicinin yeni istegi:
#
#   "acılırken linux logları felan gözüküyo o da gözükmesin direkt acılırken
#    ilk animasyon baslasın"
#
# Animasyon artik initramfs'in /init'inden, yani userspace'in ILK aninda
# basliyor (rootfs-overlay/init: start_early_splash). Doldurulacak bir bosluk
# yok: cekirdek cerceve arabellegini kurar kurmaz animasyon ekrani devraliyor.
#
# Bu yuzden "quiet loglevel=0": ekrana hicbir cekirdek mesaji dusmez.
# KAYIT KAYBOLMAZ — mesajlar gunluk tamponuna yazilmaya devam eder ve
# "dmesg" ile okunur. Kurtarma girdisinde (asagida) loglevel=7 durur; bir sey
# ters giderse tum kayit yine ekranda.
# ── Düzeltilen gerçek hata: PANİK MESAJI GÖRÜNMÜYORDU ──────────────────────
#
# Burada "loglevel=0" yazıyordu. O değer çekirdek konsoluna HİÇBİR ŞEY
# bastırmaz — KERN_EMERG dahil. Yani makine panikleyince ekran SİYAH kalıyor,
# yalnızca Caps Lock ışığı yanıp sönüyordu.
#
# Kullanıcının bildirdiği belirti tam olarak buydu:
#   "panic satırı gözükmüyor, ekran siyah, caps lock yanıp sönüyor"
#
# Caps Lock'un yanıp sönmesi çekirdeğin panik işaretidir; mesajın kendisi
# bastırıldığı için teşhis İMKÂNSIZ hâle geliyordu.
#
# loglevel=1: yalnızca KERN_EMERG basılır. Açılış yine SESSİZ (normal sürücü
# mesajları level 3-7'dir ve görünmez), ama panik GÖRÜNÜR. Sessiz açılış
# uğruna arıza teşhisini kaybetmek kabul edilemez.
#
# ── thinkpad_acpi.fan_control=1 NEDEN VAR? ──────────────────────────────────
#
# Kullanıcı gerçek PC'de "turboda fan da çalışmıyor" dedi. Dizüstülerde fan
# EC'dedir; ThinkPad'de Linux'un tek yolu /proc/acpi/ibm/fan ve sürücü elle
# kademe komutlarını bu parametre OLMADAN reddeder (thinkpad-acpi.rst, "Fan
# control and monitoring"). Sürücü çekirdeğe gömülü (MODULES=n): modprobe
# seçeneği yok, parametre yalnızca komut satırından verilir. ThinkPad olmayan
# makinede sürücü hiç bağlanmaz, parametrenin etkisi olmaz. Turbo fanı
# yalnızca YÜKSELTİR (7. kademe) ve kapanınca "auto"ya döndürür
# (internal/turbo/fans.go); fanı durduran bir komut hiç gönderilmez.
#
# acpi_enforce_resources=lax BİLEREK YOK: masaüstü Super I/O fan sürücüsünü
# (nct6775/it87) ACPI çakışmasına rağmen yükletirdi ama çekirdeğin TÜM ACPI
# kaynak korumasını kaldırır (i801 SMBus dahil) ve BIOS'la aynı yongaya
# eşzamanlı erişime izin verir. Masaüstü fanı zaten BIOS eğrisinde döner;
# çakışma turbo tanısında çekirdek günlüğünden gösterilir.
MCOS_CMDLINE_BASE='console=tty0 consoleblank=0 quiet loglevel=1 fbcon=nodefer vt.global_cursor_default=0 thinkpad_acpi.fan_control=1'

# Kurtarma girdisinin komut satırı: grafik kipi hiç denenmez.
MCOS_CMDLINE_RECOVERY='console=tty0 nomodeset vga=normal loglevel=7'

# mcos_grub_header, grub.cfg'nin ortak başlığını stdout'a yazar.
# $1 = timeout saniyesi
mcos_grub_header() {
    cat <<EOF
set timeout=${1:-5}
set default=0
insmod all_video
insmod gfxterm
# ÇÖZÜNÜRLÜK: scripts/lib/display.sh tarafından üretildi. Elle değiştirmeyin;
# ekran ayarları ekranı yalnızca aşağıdaki satırı yeniden yazar.
set gfxmode=${MCOS_GFXMODE}
set gfxpayload=keep
terminal_output gfxterm
EOF
}
