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
MCOS_CMDLINE_BASE='console=tty0 consoleblank=0 loglevel=4 fbcon=nodefer vt.global_cursor_default=0'

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
