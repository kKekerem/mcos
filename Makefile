# MCOS top-level Makefile.
#
# App layer (Go + Rust) builds anywhere with the toolchains installed.
# The OS/ISO targets (os, iso, qemu) require a Linux build host (WSL2/Docker)
# and are wired up in the Buildroot milestones — they intentionally fail fast
# with a hint until then.

GO            := $(shell which go 2>/dev/null || ls /usr/local/go/bin/go 2>/dev/null || ls "/mnt/c/Program Files/Go/bin/go.exe" 2>/dev/null || echo go)
CARGO         := $(shell which cargo 2>/dev/null || ls "/mnt/c/Users/*/scoop/shims/cargo.exe" 2>/dev/null || echo cargo)
GOOS_TARGET   ?= linux
GOARCH_TARGET ?= amd64
BIN           ?= bin
VERSION       := $(shell cat VERSION 2>/dev/null || echo 0.1.0)

# Host binaries (for dev/test on the current OS).
# Hedef imaja giren komutlar. mcos-flash BURADA DEGIL: o bir MASAUSTU
# aracidir (kullanicinin Windows/Linux makinesinde calisir) ve imaja
# koymanin anlami yok.
GO_CMDS := mcosd mcosctl mcos-detect mcos-panel mcos-panel-fb mcos-splash

# Masaustu araclari: imaja girmez, ayri derlenir (make flash / flash-windows).
GO_HOST_CMDS := mcos-flash

.PHONY: offline-bundle all app build test test-boot check-ui check-fonts preview-ui vet fmt run clean os iso qemu qemu-uefi lite help preflight uefi bios usb verify-usb boottest linux flash flash-windows flash-all mod mod-fabric mod-paper shots

all: app

## app: build all app-layer commands for the host OS
app: build lite

build:
	@mkdir -p $(BIN)
	@for cmd in $(GO_CMDS); do \
		echo ">> building $$cmd"; \
		"$(GO)" build -o $(BIN)/$$cmd ./cmd/$$cmd || exit 1; \
	done

## linux: cross-compile static Go binaries for the OS image (linux/amd64)
linux:
	@mkdir -p $(BIN)/linux
	@for cmd in $(GO_CMDS); do \
		echo ">> cross-building $$cmd ($(GOOS_TARGET)/$(GOARCH_TARGET))"; \
		CGO_ENABLED=0 GOOS=$(GOOS_TARGET) GOARCH=$(GOARCH_TARGET) \
			"$(GO)" build -ldflags "-s -w" -o $(BIN)/linux/$$cmd ./cmd/$$cmd || exit 1; \
	done

## lite: build the Rust lite panel (added in M5)
lite:
	@if [ -f lite-panel/Cargo.toml ]; then \
		cd lite-panel && "$(CARGO)" build --release; \
	else echo "lite-panel not present yet (M5)"; fi

# Go paket kapsamı. "./..." KULLANILMAZ: ISO bir kez derlendikten sonra
# os/buildroot/output/build/*/gcc/testsuite altında binlerce geçersiz .go
# dosyası oluşur (ör. bug257.go 20 000 satır, "relative import paths are not
# supported in module mode"). Go araçları .gitignore'a bakmadığı için "./..."
# bunları da tarar ve build/vet baştan kırılır.
GO_PKGS := ./cmd/... ./internal/... ./panel/...

test:
	"$(GO)" test $(GO_PKGS)

## test-boot: kurulum/önyükleme mantığını doğrular (diske dokunmaz)
test-boot:
	@sh scripts/test-disksig.sh
	@sh scripts/test-install-logic.sh
	@sh scripts/test-bootloader-embed.sh
	@sh scripts/test-boot-logic.sh
	@sh scripts/test-display-logic.sh
	@sh scripts/test-splash-logic.sh
	@sh scripts/test-panel-wiring.sh
	@sh scripts/test-diskroot-logic.sh
	@sh scripts/test-vbox-compat.sh
	@sh scripts/test-wifi-logic.sh
	@sh scripts/test-flash-logic.sh
	@sh scripts/test-ui-logic.sh
	@sh scripts/test-link-logic.sh
	@sh scripts/test-version.sh
	@sh scripts/test-remote-logic.sh
	@sh scripts/test-remote-e2e.sh

## offline-bundle: çevrimdışı sunucu paketini indirir (Java + Fabric + Via)
##
## İmaja gömülür; internet olmadan sunucu kurulabilmesini sağlar.
## Minecraft sunucu jar'ı BUNA DAHİL DEĞİLDİR — Mojang EULA'sı
## dağıtımını yasaklıyor; o tek dosya ilk kurulumda indirilip
## önbelleklenir.
offline-bundle:
	@sh scripts/fetch-offline-bundle.sh dist/offline

## probe-boot: HEDEF rootfs'te hangi önyükleyici araçlarının olduğunu gösterir
probe-boot:
	@sh scripts/probe-target-boot.sh $(BR_OUTPUT)/target

## check-ui: TUI'de çift genişlikli glif (emoji) kalmadığını doğrular
check-ui:
	@python3 scripts/check-tui-glyphs.py

## check-fonts: hedef rootfs'teki fbterm font yığınını denetler
check-fonts:
	@sh scripts/check-fonts.sh

## preview-ui: paneli headless çalıştırıp bir kare basar (SECTION=0 W=96 H=30)
preview-ui:
	@sh scripts/preview-ui.sh $(or $(SECTION),0) $(or $(W),96) $(or $(H),30)

vet:
	"$(GO)" vet $(GO_PKGS)

fmt:
	"$(GO)" fmt $(GO_PKGS)

## run: start the daemon against a local dev data root over TCP loopback
run: build
	$(BIN)/mcosd --data-root ./run --listen tcp://127.0.0.1:7777

clean:
	rm -rf $(BIN) dist

## os: build the Buildroot root filesystem + kernel (M9, needs Linux host)
BR_DIR := os/buildroot/buildroot
BR_VERSION := 2024.02
# Build output dir. Override (e.g. BR_OUTPUT=$HOME/mcos-output) to build on a
# native Linux filesystem if /mnt/c causes Buildroot fakeroot/symlink issues.
BR_OUTPUT ?= $(PWD)/os/buildroot/output

# Buildroot host-side tools that must exist before a build. The classic failure
# (dependencies.mk) is missing cpio/unzip; we check the full set up front.
HOST_DEPS := cpio unzip rsync bc wget gawk bison flex file

## preflight: ensure Buildroot host dependencies are installed (Linux only)
preflight:
	@if [ "$(shell uname -s 2>/dev/null)" != "Linux" ]; then \
		echo ">> preflight: skipped (not a Linux host)"; exit 0; \
	fi; \
	missing=""; \
	for tool in $(HOST_DEPS); do \
		command -v $$tool >/dev/null 2>&1 || missing="$$missing $$tool"; \
	done; \
	if [ -z "$$missing" ]; then \
		echo ">> preflight: all Buildroot host dependencies present"; exit 0; \
	fi; \
	echo ">> preflight: missing host tools:$$missing"; \
	if command -v apt-get >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
		echo ">> preflight: installing via apt-get (passwordless sudo) ..."; \
		sudo apt-get update && sudo apt-get install -y --no-install-recommends$$missing || { \
			echo "preflight: auto-install failed. Run: sudo apt-get install -y$$missing"; exit 1; }; \
	else \
		echo "preflight: cannot auto-install (sudo needs a password)."; \
		echo "preflight: run this once, then re-run the build:"; \
		echo "    sudo apt-get install -y$$missing"; \
		echo "preflight: or run the full setup: bash scripts/setup-wsl.sh"; exit 1; \
	fi

$(BR_DIR)/Makefile:
	@echo ">> Fetching Buildroot $(BR_VERSION) ..."
	@mkdir -p os/buildroot
	@git clone --depth 1 --branch $(BR_VERSION) https://github.com/buildroot/buildroot.git $(BR_DIR)

## fix-perms: derlemeyi kıran KAYIP ÇALIŞTIRMA BİTİNİ geri koyar
##
## ── Düzeltilen gerçek hata ─────────────────────────────────────────────────
## "make iso" şununla kırıldı:
##
##   >>>   Executing post-build script .../post-build.sh
##   /bin/bash: line 1: .../post-build.sh: Permission denied
##   make[2]: *** [Makefile:754: target-finalize] Error 126
##
## Sebep: Buildroot BR2_ROOTFS_POST_BUILD_SCRIPT'i DOĞRUDAN çalıştırır, yani
## dosyanın çalıştırma biti olmak ZORUNDA. Bu depo Windows'tan (WSL paylaşımı,
## NTFS üzerinde bir checkout, ya da bir Windows düzenleyicisi) da
## düzenlenebiliyor ve o yollardan yazılan her dosya 0644'e düşer. git de
## yalnızca TEK bir çalıştırma bitini takip eder; dosya yeni eklendiyse ya da
## mod değişikliği commit'lenmediyse bit sessizce kaybolur.
##
## Belirtisi sinsi: kaynak dosya doğru, derleme saatlerce sürüp EN SON adımda
## kırılıyor. Bu yüzden 'os' hedefi her çalıştığında bitler geri konuyor.
.PHONY: fix-perms
fix-perms:
	@chmod 755 \
		os/buildroot/external/board/mcos/post-build.sh \
		os/buildroot/external/board/mcos/rootfs-overlay/init \
		os/buildroot/external/board/mcos/rootfs-overlay/etc/init.d/S* \
		os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/* \
		2>/dev/null || true
	@chmod 755 scripts/*.sh 2>/dev/null || true

os: preflight fix-perms $(BR_DIR)/Makefile linux
	@if [ "$(shell uname -s 2>/dev/null)" != "Linux" ]; then \
		echo "os: Buildroot requires a Linux host (WSL2/Docker). Run from a Linux environment."; exit 1; \
	fi; \
	set -e; \
	CLEAN_PATH=$$(echo "$$PATH" | tr ':' '\n' | grep -v ' ' | paste -sd ':' -); \
	export PATH="$$CLEAN_PATH"; \
	make -C $(BR_DIR) O=$(BR_OUTPUT) BR2_EXTERNAL=$(PWD)/os/buildroot/external mcos_defconfig; \
	if ls -d $(BR_OUTPUT)/build/mcos-*/ >/dev/null 2>&1; then \
		echo ">> mcos: forcing reinstall of fresh linux binaries"; \
		make -C $(BR_OUTPUT) mcos-dirclean; \
	fi; \
	KCFG="$(PWD)/os/buildroot/external/board/mcos/kernel.config"; \
	BUILT=$$(ls -1 $(BR_OUTPUT)/build/linux-*/.config 2>/dev/null | head -1 || true); \
	if [ -n "$$BUILT" ] && [ "$$KCFG" -nt "$$BUILT" ]; then \
		echo ">> kernel.config changed -> linux-reconfigure (re-applying CONFIG_NET etc.)"; \
		make -C $(BR_OUTPUT) linux-reconfigure; \
	fi; \
	make -C $(BR_OUTPUT)

## iso: produce mcos-x86_64.iso — BIOS+UEFI hybrid via grub-mkrescue
iso: os
	@if [ "$(shell uname -s 2>/dev/null)" != "Linux" ]; then \
		echo "iso: ISO generation requires a Linux host (WSL2/Docker)."; exit 1; \
	fi; \
	command -v grub-mkrescue >/dev/null 2>&1 || { echo "iso: need grub-common + grub-pc-bin + grub-efi-amd64-bin + mtools"; exit 1; }; \
	command -v xorriso >/dev/null 2>&1 || { echo "iso: need xorriso"; exit 1; }; \
	. scripts/lib/display.sh; \
	D=dist/iso; \
	rm -rf "$$D" dist/mcos-x86_64.iso; \
	mkdir -p "$$D/boot/grub" "$$D/EFI/BOOT"; \
	cp "$(BR_OUTPUT)/images/bzImage"        "$$D/boot/bzImage"; \
	cp "$(BR_OUTPUT)/images/rootfs.cpio.gz" "$$D/boot/initrd.img"; \
	printf '%s\n' \
		'set timeout=3' \
		'set default=0' \
		'insmod all_video' \
		'insmod vbe' \
		'insmod vga' \
		'insmod gfxterm' \
		'insmod part_msdos' \
		'insmod part_gpt' \
		'insmod fat' \
		'insmod ext2' \
		'insmod iso9660' \
		'insmod search' \
		'insmod search_fs_file' \
		'insmod search_label' \
		"set gfxmode=$$MCOS_GFXMODE" \
		'set gfxpayload=keep' \
		'terminal_input console' \
		'terminal_output console gfxterm' \
		'menuentry "MCOS Live Installer" {' \
		'  set gfxpayload=keep' \
		'  search --no-floppy --set=root --file /boot/bzImage' \
		"  linux /boot/bzImage $$MCOS_CMDLINE_BASE" \
		'  clear' \
		'  echo "  MCOS baslatiliyor. Bellek diski yukleniyor (~140 MB)..."' \
		'  initrd /boot/initrd.img' \
		'  echo "  Hazir, cekirdek calistiriliyor."' \
		'}' \
		'menuentry "MCOS Live Installer (Safe / VGA Text Mode)" {' \
		'  set gfxpayload=text' \
		'  search --no-floppy --set=root --file /boot/bzImage' \
		'  echo "  Guvenli kip: grafik kipi hic denenmez, tum kayit ekrana basilir."' \
		"  linux /boot/bzImage $$MCOS_CMDLINE_RECOVERY" \
		'  initrd /boot/initrd.img' \
		'}' \
		> "$$D/boot/grub/grub.cfg"; \
	printf '\\EFI\\BOOT\\BOOTX64.EFI\r\n' > "$$D/startup.nsh"; \
	grub-mkrescue -o dist/mcos-x86_64.iso "$$D" \
		-- -volid MCOS -joliet on -rockridge on && \
	echo ">> ISO ready: dist/mcos-x86_64.iso ($$(du -h dist/mcos-x86_64.iso | cut -f1))"

# ── Yukaridaki grub-mkrescue cagrisinda DUZELTILEN GERCEK HATALAR ──────────
#
# 1) "$$GRUB_MODS" KALDIRILDI.
#    grub-mkrescue, "--" oncesindeki her secenek-olmayan argumani ISO KOKUNE
#    EKLENECEK KAYNAK DIZIN sayar. "-d/--directory" ile karistirilmisti.
#    Olculen sonuc: uretilen ISO kokunde 291 basibos *.MOD dosyasi, BOOT.IMG,
#    CDBOOT.IMG, EFIEMU32.O (~1 MB) -- ve daha kotusu, "EFI" dizin adi
#    cakisip "EFI0/" ve "EFI1/" olarak bozulmustu, ikisi de BOS.
#    grub-mkrescue iki platformu da zaten kendisi buluyor; bu argumana hic
#    gerek yoktu.
#
# 2) startup.nsh EKLENDI.
#    VirtualBox'in EDK2 tabanli EFI'si /EFI/BOOT/BOOTX64.EFI yolunu bazen
#    bulamayip "UEFI Interactive Shell"e duser -- VBox'ta en cok bildirilen
#    EFI belirtisi budur. O kabuk kokteki startup.nsh'i OTOMATIK calistirir,
#    yani bu tek satirlik dosya makineyi kurtarir.
#    (DIKKAT: bu, eskiden kernel panic'e yol acan EFISTUB numarasi DEGILDIR.
#     Orada ham cekirdek BOOTX64.EFI yapiliyor ve komut satirsiz aciliyordu.
#     Burada BOOTX64.EFI gercek GRUB'dir ve komut satirini kendisi verir.)
#
# 3) -joliet on -rockridge on EKLENDI.
#    DIKKAT: "--" sonrasi xorriso YEREL kipte calisir, mkisofs oykunmesinde
#    DEGIL. mkisofs adi olan "-rational-rock" burada GECERSIZDIR ve
#    derlemeyi kirar; yerel karsiligi "-rockridge on".
#
#    Joliet olmadan bir firmware kabugu yalnizca bozuk 8.3 adlar gorur
#    (BZIMAGE.;1). startup.nsh'in dogru adla gorunebilmesi icin gerekli.

## qemu: boot the ISO in QEMU, BIOS mode, graphical window
qemu: iso
	@command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "qemu: qemu-system-x86_64 not found"; exit 1; }; \
	KVM=""; if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then KVM="-enable-kvm"; else echo ">> /dev/kvm not accessible -> using TCG software emulation (slower, no KVM needed)"; fi; \
	qemu-system-x86_64 -m 2048 -cdrom dist/mcos-x86_64.iso -vga std -boot d $$KVM

## qemu-uefi: boot the ISO in QEMU under UEFI (OVMF)
qemu-uefi: iso
	@command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "qemu: qemu-system-x86_64 not found"; exit 1; }; \
	OVMF=""; \
	for f in /usr/share/ovmf/OVMF.fd /usr/share/OVMF/OVMF.fd /usr/share/OVMF/OVMF_CODE.fd /usr/share/qemu/OVMF.fd; do \
		[ -f "$$f" ] && { OVMF="$$f"; break; }; \
	done; \
	[ -n "$$OVMF" ] || { echo "qemu-uefi: OVMF firmware not found — install 'ovmf'"; exit 1; }; \
	KVM=""; if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then KVM="-enable-kvm"; else echo ">> /dev/kvm not accessible -> using TCG software emulation (slower, no KVM needed)"; fi; \
	qemu-system-x86_64 -m 2048 -bios "$$OVMF" -cdrom dist/mcos-x86_64.iso -vga std -boot d $$KVM


# ── Kalici USB disk imajlari ────────────────────────────────────────────────
#
# NEDEN ISO YETERLI DEGIL:
#   ISO salt-okunur bir CD dosya sistemidir. USB'ye yazildiginda (a) kalici
#   veri alani yoktur, (b) hibrit MBR olmadan bircok BIOS/UEFI firmware'i onu
#   boot edilebilir GORMEZ — kullanicinin "USB'ye kurunca PC gormuyor"
#   sikayetinin sebebi tam olarak bu. Asagidaki hedefler GERCEK bolum tablolu
#   disk imajlari uretir ve ikisi de QEMU'da USB aygiti olarak boot ederek
#   dogrulandi (scripts/boottest.sh).
#
# KALICILIK NASIL CALISIYOR:
#   2. bolum "MCOS-DATA" etiketiyle ext4'tur. Acilista S99mcos onu
#   mcos-findfs ile bulup /data'ya baglar. Java kurulumlari ve olusturulan
#   sunucular /data altinda yasadigi icin reboot'ta KORUNUR.
#
# ROOT GEREKMEZ: FAT bolumu mtools, ext4 bolumu "mke2fs -d" ile dogrudan
# dosya uzerinde uretilir; mount/losetup yoktur.

# USB imaj toplam boyutu. Yazilacak USB bellekten kucuk veya esit olmali.
USB_SIZE    ?= 4G
# Boot bolumu boyutu (cekirdek ~10M + initramfs ~122M + pay).
USB_BOOT_MB ?= 768

## uefi: kalici UEFI USB disk imaji -> dist/mcos-uefi.img
uefi: os
	@bash scripts/mkusb.sh --mode uefi --out dist/mcos-uefi.img \
		--size $(USB_SIZE) --boot-mb $(USB_BOOT_MB)
	@bash scripts/verify-usb.sh dist/mcos-uefi.img

## bios: kalici BIOS (Legacy) USB disk imaji -> dist/mcos-bios.img
bios: os
	@bash scripts/mkusb.sh --mode bios --out dist/mcos-bios.img \
		--size $(USB_SIZE) --boot-mb $(USB_BOOT_MB)
	@bash scripts/verify-usb.sh dist/mcos-bios.img

## usb: hem UEFI hem BIOS kalici imajlarini uret
usb: uefi bios

## verify-usb: uretilmis imajlari BOOT ETMEDEN denetle (bolum tablosu, onyukleyici, etiket)
verify-usb:
	@# -- Yakalanan gercek hata --------------------------------------------
	@# Bu hedef eskiden imaj YOKKEN hicbir sey yazmadan 0 ile cikiyordu.
	@# Yani "make verify-usb" sessizce basarili gorunuyor, ama hicbir sey
	@# dogrulanmiyordu. Dogrulama komutunun bos gecmesi, en kotu hata
	@# turudur: kullanici imajin denetlendigini sanip USB'ye yazar.
	@found=0; rc=0; \
	for f in dist/mcos-uefi.img dist/mcos-bios.img; do \
		if [ -f "$$f" ]; then \
			found=1; \
			bash scripts/verify-usb.sh "$$f" || rc=1; \
		fi; \
	done; \
	if [ "$$found" -eq 0 ]; then \
		echo "verify-usb: hicbir imaj bulunamadi (dist/mcos-uefi.img, dist/mcos-bios.img)"; \
		echo "            once 'make usb' calistirin"; \
		exit 1; \
	fi; \
	exit $$rc

## boottest: imajlari QEMU'da GERCEKTEN boot edip hangi asamaya geldigini raporla
boottest:
	@bash scripts/mkusb.sh --mode bios --out dist/boottest-bios.img \
		--size 1G --cmdline "console=ttyS0,115200" >/dev/null
	@bash scripts/boottest.sh --img dist/boottest-bios.img --mode bios --seconds 45
	@bash scripts/mkusb.sh --mode uefi --out dist/boottest-uefi.img \
		--size 1G --cmdline "console=ttyS0,115200" >/dev/null
	@bash scripts/boottest.sh --img dist/boottest-uefi.img --mode uefi --seconds 60

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'

## flash: masaustu flaslama aracini bu makine icin derler
##
## Sonuc dist/flash/ altina konur; yanindaki mcos-flash.sh ile calistirilir
## (kok hakkini kendisi ister).
# Baslaticilar ELLE YAZILMIS KAYNAKTIR, uretilen dosya degil.
#
# NEDEN AYRI BIR DEGISKEN: dist/ butunuyle .gitignore'da (ISO ve .img orada
# uretiliyor). Baslaticilar bir zamanlar dogrudan dist/flash/ icinde duruyordu
# ve bu yuzden GIT TARAFINDAN HIC IZLENMIYORLARDI: temiz bir klonda "make
# flash" ikiliyi uretiyor ama kullanicinin cift tiklayacagi .bat/.sh hic
# olusmuyordu. Artik kaynak izlenen bir klasorde duruyor ve buraya kopyalaniyor.
FLASH_LAUNCHER_SRC := cmd/mcos-flash/launcher

flash:
	@mkdir -p dist/flash
	@echo ">> building mcos-flash (host)"
	@CGO_ENABLED=0 "$(GO)" build -ldflags "-s -w" -o dist/flash/mcos-flash ./cmd/mcos-flash
	@cp "$(FLASH_LAUNCHER_SRC)/mcos-flash.sh" dist/flash/
	@cp "$(FLASH_LAUNCHER_SRC)/mcos-flash.bat" dist/flash/
	@chmod +x dist/flash/mcos-flash.sh
	@echo "   dist/flash/mcos-flash  (baslatici: dist/flash/mcos-flash.sh)"

## flash-windows: masaustu flaslama aracini Windows icin derler
##
## .bat baslaticisi yonetici hakkini kendisi ister; ikisi de ayni klasorde
## olmali.
flash-windows:
	@mkdir -p dist/flash
	@echo ">> building mcos-flash.exe (windows/amd64)"
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$(GO)" build -ldflags "-s -w" \
		-o dist/flash/mcos-flash.exe ./cmd/mcos-flash
	@cp "$(FLASH_LAUNCHER_SRC)/mcos-flash.bat" dist/flash/
	@echo "   dist/flash/mcos-flash.exe  (baslatici: dist/flash/mcos-flash.bat)"

## flash-all: hem Linux hem Windows araclarini derler
flash-all: flash flash-windows

# ─────────────────────────────────────────────────────────────────────────────
# MCOS DUGUM (masaustu uygulamasi)
# ─────────────────────────────────────────────────────────────────────────────
#
# Siradan bir Windows/Linux bilgisayari MCOS'a "ikinci PC" olarak ekleyen
# program. MCOS kurmaya gerek yok: calistirmak yeterli.
#
# Baslaticilar mcos-flash'taki ile AYNI mantikla izlenen bir klasorde durur;
# dist/ butunuyle .gitignore'da oldugu icin oraya konulsalardi temiz bir
# klonda hic olusmazlardi.
NODE_LAUNCHER_SRC := cmd/mcos-node/launcher

node:
	@mkdir -p dist/node
	@echo ">> building mcos-node (host)"
	@CGO_ENABLED=0 "$(GO)" build -ldflags "-s -w" -o dist/node/mcos-node ./cmd/mcos-node
	@cp "$(NODE_LAUNCHER_SRC)/mcos-node.sh" dist/node/
	@cp "$(NODE_LAUNCHER_SRC)/mcos-node.bat" dist/node/
	@chmod +x dist/node/mcos-node.sh
	@echo "   dist/node/mcos-node  (baslatici: dist/node/mcos-node.sh)"

node-windows:
	@mkdir -p dist/node
	@echo ">> building mcos-node.exe (windows/amd64)"
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$(GO)" build -ldflags "-s -w" \
		-o dist/node/mcos-node.exe ./cmd/mcos-node
	@cp "$(NODE_LAUNCHER_SRC)/mcos-node.bat" dist/node/
	@echo "   dist/node/mcos-node.exe  (baslatici: dist/node/mcos-node.bat)"

# Modu da yanina koy: dugum, ortak dunya modunu programin YANINDA arar.
node-all: node node-windows
	@if [ -f dist/mods/mcos-link.jar ]; then \
		cp dist/mods/mcos-link.jar dist/node/; \
		echo "   dist/node/mcos-link.jar (ortak dunya modu)"; \
	else \
		echo "   UYARI: dist/mods/mcos-link.jar yok - once 'make mod' calistirin"; \
	fi


## mod: MCOS Link Minecraft modunu derler (gradle, bir kez internet ister)
##
## Sonuc dist/mods/mcos-link.jar; daemon oradan alip sunucunun mods/
## klasorune kurar.
# Ortak dunya eklentisinin IKI yapisi vardir.
#
# Fabric modlari mods/ altindan, Paper eklentileri plugins/ altindan yuklenir
# ve ikisi birbirinin dosyasini TANIMAZ. Tek bir jar ile ikisini birden
# beslemek mumkun degil. Ikisi de AYNI tel protokolunu konusur, yani bir Paper
# dugumu ile bir Fabric dugumu ayni ortak dunyayi paylasabilir.
mod: mod-fabric mod-paper
	@ls -l dist/mods/ 2>/dev/null || true

## mod-fabric: ortak dunya modu (Fabric/Quilt) -> dist/mods/mcos-link.jar
mod-fabric:
	@if [ ! -f mods/mcos-link/build.gradle ]; then \
		echo "mods/mcos-link bulunamadi"; exit 1; \
	fi
	@mkdir -p dist/mods
	@cd mods/mcos-link && (./gradlew build --no-daemon || gradle build --no-daemon)

## mod-paper: ortak dunya eklentisi (Paper/Purpur) -> dist/mods/mcos-link-paper.jar
mod-paper:
	@if [ ! -f mods/mcos-link-paper/build.gradle ]; then \
		echo "mods/mcos-link-paper bulunamadi"; exit 1; \
	fi
	@mkdir -p dist/mods
	@cd mods/mcos-link-paper && (./gradlew build --no-daemon || gradle build --no-daemon)

## shots: arayuzun her ekranini PNG olarak basar (donanim gerekmez)
shots:
	@sh scripts/shots.sh
