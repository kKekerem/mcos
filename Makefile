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

# ── QEMU ayarları ───────────────────────────────────────────────────────────
#
# ── Kullanıcının şikâyeti ───────────────────────────────────────────────────
#
#	"qemu grafik hizlandirma kullanmiyor, grub menusunde bile kasiyor"
#
# ── Ölçülen sebep ───────────────────────────────────────────────────────────
#
# /dev/kvm VAR (crw-rw---- root:kvm) ve işlemcide vmx açık, yani WSL2'nin iç
# içe sanallaştırması çalışıyor. Ama kullanıcı `kvm` GRUBUNDA DEĞİL:
#
#	$ ls -la /dev/kvm   -> crw-rw---- 1 root kvm ...
#	$ id                -> ... groups=1000(kkekerem),4(adm),...  (kvm YOK)
#	$ qemu-system-x86_64 -accel kvm ...
#	  Could not access KVM kernel module: Permission denied
#
# Eski hedef bu durumda SESSİZCE TCG'ye (saf yazılım öykünmesi) düşüyordu.
# TCG'de 1920x1080x32 bir gfxterm'i GRUB'un yazılımla çizmesi, her menü
# tuşunda milyonlarca pikselin öykünmüş komutlarla kopyalanması demektir —
# "GRUB menüsünde bile kasıyor" şikâyetinin tam karşılığı budur.
#
# ── Çözüm (kullanıcının yapması gereken, TEK SEFER) ─────────────────────────
#
#	sudo usermod -aG kvm $USER
#	# sonra Windows tarafında PowerShell'de:  wsl --shutdown
#	# ve WSL'i yeniden açın. Doğrulama:
#	id | grep kvm && qemu-system-x86_64 -accel help
#
# Grup değişikliği ancak yeni bir oturumda geçerli olur; `wsl --shutdown`
# olmadan aynı terminalde görünmez.
#
# Artık hedefler KVM erişilemezse SESSİZCE yavaş moda düşmüyor, ne yapılacağını
# ekrana yazıyor.
QEMU_MEM ?= 2048
QEMU_SMP  ?= 4
# Ses: WSLg PulseAudio sunucusu sağlıyor (/mnt/wslg/PulseServer), yani MCOS'un
# ses efektleri sanal makinede gerçekten duyulur. PULSE_SERVER yoksa QEMU
# kendiliğinden sessize düşer.
QEMU_AUDIO ?= -audiodev pa,id=snd0 -device intel-hda -device hda-duplex,audiodev=snd0

# QEMU_ACCEL_CHECK, ACCEL değişkenini kurar ve KVM yoksa ÇÖZÜMÜ yazar.
define QEMU_ACCEL_CHECK
if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then \
	ACCEL="-accel kvm -cpu host"; \
	echo ">> KVM acik: donanim hizlandirmasi kullaniliyor"; \
else \
	ACCEL="-accel tcg,thread=multi -cpu max"; \
	echo ">> UYARI: /dev/kvm erisilemiyor -> yazilim oykunmesi (COK YAVAS)."; \
	echo ">>        GRUB menusu bile takilir. Tek seferlik cozum:"; \
	echo ">>            sudo usermod -aG kvm $$USER"; \
	echo ">>        sonra Windows PowerShell'de: wsl --shutdown"; \
	echo ">>        ve WSL'i yeniden acin (grup degisikligi yeni oturumda gecerli)."; \
fi;
endef

# Host binaries (for dev/test on the current OS).
# Hedef imaja giren komutlar. mcos-flash BURADA DEGIL: o bir MASAUSTU
# aracidir (kullanicinin Windows/Linux makinesinde calisir) ve imaja
# koymanin anlami yok.
GO_CMDS := mcosd mcosctl mcos-detect mcos-panel mcos-panel-fb mcos-splash

# Masaustu araclari: imaja girmez, ayri derlenir (make flash / flash-windows).
GO_HOST_CMDS := mcos-flash

.PHONY: offline-bundle all app build test test-boot check-ui check-fonts preview-ui vet fmt run clean os iso qemu qemu-uefi qemu-run qemu-run-uefi lite help preflight uefi bios usb usb-both windows-usb verify-usb boottest linux flash flash-windows flash-all node node-windows node-linux node-jars node-winres node-all mod mod-fabric mod-paper mod-velocity shots

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
# tools/ DAHİL: gözden geçirme araçları (ör. tools/wavdump, ses efektlerini
# WAV'a döker) derlenmeye devam etmeli. Dışarıda bırakılan bir paket, ilk
# imza değişikliğinde sessizce çürür.
GO_PKGS := ./cmd/... ./internal/... ./panel/... ./tools/...

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
	@sh scripts/test-kernel-config.sh
	@sh scripts/test-wifi-logic.sh
	@sh scripts/test-flash-logic.sh
	@sh scripts/test-ui-logic.sh
	@sh scripts/test-link-logic.sh
	@sh scripts/test-offline-logic.sh
	@sh scripts/test-vnc-logic.sh
	@sh scripts/test-version.sh
	@sh scripts/test-remote-logic.sh
	@sh scripts/test-remote-e2e.sh

## offline-bundle: çevrimdışı sunucu paketini indirir (Fabric + Via + playit)
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

## offline-if-missing: paket yoksa indirmeyi dener, basarisizsa UYARIR
##
## NEDEN 'os' ZINCIRINDE: playit ikilileri bu paketten geliyor ve kullanici
## "bide playiti de gom" dedi. Hedef elle calistirilmaya birakilinca depoda
## dist/offline hic olusmadi ve uretilen ISO'da playit BULUNMADI.
##
## Basarisizlik derlemeyi KIRMAZ: internetsiz bir makinede de ISO uretilebilmeli.
.PHONY: offline-if-missing
offline-if-missing:
	@if [ ! -d dist/offline ] || [ -z "$$(ls -A dist/offline 2>/dev/null)" ]; then \
		echo ">> cevrimdisi paket yok, indiriliyor (playit dahil)..."; \
		sh scripts/fetch-offline-bundle.sh dist/offline || { \
			echo ">> UYARI: paket indirilemedi."; \
			echo ">>         playit imaja GOMULMEYECEK ve sunucu kurulumu internet isteyecek."; \
		}; \
	else \
		echo ">> cevrimdisi paket hazir ($$(du -sh dist/offline | cut -f1))"; \
	fi

## builtin-java: imaja GOMULECEK Temurin 21 JRE'yi indirir ve SHA-256 dogrular
##
## Arsiv dist/java/ altinda onbelleklenir; post-build.sh onu rootfs'e
## (/usr/lib/jvm/temurin-21-jre) acar. offline-if-missing'in AKSINE
## basarisizlik derlemeyi DURDURUR: Java'siz imaj, "Java 21 kurulu gelir"
## sozunu sessizce bozar. Tanim: os/buildroot/external/board/mcos/builtin-java.conf
.PHONY: builtin-java
builtin-java:
	@sh scripts/fetch-builtin-java.sh dist/java

os: preflight fix-perms offline-if-missing builtin-java $(BR_DIR)/Makefile linux
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
	: '─ FIRMWARE: defconfig degistiyse linux-firmware YENIDEN DERLENIR ───'; \
	: '  Buildroot bir kez derlenmis paketi, secenekleri degisse bile yeniden'; \
	: '  derlemez. amdgpu/radeon/iwlwifi gibi yeni secilen firmware dosyalari'; \
	: '  bu olmadan imaja HIC girmezdi - ve amdgpu firmware siz kara ekran'; \
	: '  birakir (kernel.config GPU bolumu).'; \
	: '  YAKALANAN HATA: burada once "reinstall" vardi. reinstall yalnizca'; \
	: '  DERLEME adiminda uretilen br-firmware.tar i yeniden acar; arsiv eski'; \
	: '  secimle kaldigi icin amdgpu/radeon/QCA6174 yine girmedi (olculdu:'; \
	: '  target/lib/firmware/amdgpu yoktu). rebuild arsivi yeniden uretir.'; \
	: '  Damga da DERLEME damgasi: kurulum damgasi her reinstall da yenilenip'; \
	: '  karsilastirmayi yaniltiyordu.'; \
	DEFC="$(PWD)/os/buildroot/external/configs/mcos_defconfig"; \
	FWST=$$(ls -1 $(BR_OUTPUT)/build/linux-firmware-*/.stamp_built 2>/dev/null | head -1 || true); \
	if [ -n "$$FWST" ] && [ "$$DEFC" -nt "$$FWST" ]; then \
		echo ">> mcos_defconfig changed -> linux-firmware-rebuild"; \
		make -C $(BR_OUTPUT) linux-firmware-rebuild; \
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
	SPLASH="$$(mktemp -d)/mcos-splash"; \
	if "$(GO)" build -o "$$SPLASH" ./cmd/mcos-splash >/dev/null 2>&1 && \
	   "$$SPLASH" --screenshot "$$D/boot/grub/mcos-logo.png" --brand >/dev/null 2>&1; then \
		echo ">> GRUB arka plani uretildi: acilista logo ANINDA gorunur"; \
	else \
		echo ">> HATA: GRUB logosu uretilemedi."; \
		echo ">>       Acilista logo gorunmezdi ve bu SESSIZCE gecilemez:"; \
		echo ">>       bu uyari bir kez zaten fark edilmeden kayip gitti"; \
		echo ">>       (bayat bin/mcos-splash, --brand desteklemiyordu)."; \
		rm -rf "$$(dirname "$$SPLASH")"; \
		exit 1; \
	fi; \
	rm -rf "$$(dirname "$$SPLASH")"; \
	if [ -d "$(BR_OUTPUT)/images/mcos-offline" ]; then \
		mkdir -p "$$D/mcos/offline"; \
		cp -a "$(BR_OUTPUT)/images/mcos-offline"/. "$$D/mcos/offline/"; \
		echo ">> cevrimdisi paket ISO'ya eklendi ($$(du -sh "$$D/mcos/offline" | cut -f1)) - initramfs'e DEGIL"; \
	fi; \
	: '─ GUNCELLEME: derleme kimligi ISO ya yazilir ───────────────────'; \
	: '  Panel USB deki ISO nun daha yeni mi eski mi oldugunu bu dosyaya'; \
	: '  bakarak soyler; mcos-update kimligi olmayan ISO yu reddeder (yeni'; \
	: '  initrd ile eski kok karisirdi). Deger initrd nin ICINDEKI dosyadan'; \
	: '  okunur: ikisi ayni olmali, yoksa acilistaki esitleme yaniltir.'; \
	BID="$$(gzip -dc "$(BR_OUTPUT)/images/rootfs.cpio.gz" 2>/dev/null | \
		cpio -i --quiet --to-stdout etc/mcos-build-id ./etc/mcos-build-id 2>/dev/null | head -n 1)"; \
	[ -n "$$BID" ] || BID="$$(head -n 1 "$(BR_OUTPUT)/target/etc/mcos-build-id" 2>/dev/null)"; \
	if [ -z "$$BID" ]; then \
		echo ">> HATA: derleme kimligi yok (etc/mcos-build-id); post-build.sh calismamis."; \
		echo ">>       Bu ISO ile kurulu sistemler guncellenemezdi. Cozum: make os && make iso"; \
		exit 1; \
	fi; \
	mkdir -p "$$D/mcos"; \
	printf '%s\n' "$$BID" > "$$D/mcos/build-id"; \
	printf '%s\n' "$(VERSION)" > "$$D/mcos/version"; \
	echo ">> derleme kimligi ISO ya yazildi: $$BID (surum $(VERSION))"; \
	: '─ GRUB acilis ekrani: ONEMLI, OLCULEREK bulundu ────────────────────'; \
	: '  timeout=0 + timeout_style=hidden -> GRUB menu ekranini HIC cizmez,'; \
	: '  dolayisiyla background_image de cizilmez ve ekran 11,5 saniye KARA'; \
	: '  kalir (QEMU/KVM ile kare kare olculdu). Arka plani menu girdisinin'; \
	: '  ICINE tasimak da ise yaramadi - ayni sebep.'; \
	: '  Arka plan ancak geri sayim GERCEKTEN gosterilirken boyaniyor.'; \
	: '  timeout=1 + countdown: logo ~0,5 sn-de geliyor, sol ustte 1 saniye'; \
	: '  tek bir "1" karakteri kaliyor, sonra 11,5 sn boyunca TEMIZ logo.'; \
	: '  Esc ile kurtarma girdisine de hala ulasilabiliyor.'; \
	printf '%s\n' \
		'set timeout=1' \
		'set default=0' \
		'set timeout_style=countdown' \
		'insmod all_video' \
		'insmod png' \
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
		'if loadfont /boot/grub/fonts/unicode.pf2 ; then true ; fi' \
		'background_image -m stretch /boot/grub/mcos-logo.png' \
		'menuentry "MCOS" {' \
		'  set gfxpayload=keep' \
		'  # VENTOY VE COKLU DISK ICIN: $$root EZILMEZ.' \
		'  # Burada kosulsuz "search --set=root" vardi ve $$root u EZIYORDU.' \
		'  # Ventoy grub2 kipinde ISO yu chainload ederken $$root u KENDI' \
		'  # esledigi sanal aygita kurar. Kosulsuz search ise TUM aygitlari' \
		'  # tarar; Ventoy USB sinde baska ISO lar varsa YANLIS aygita duser.' \
		'  # Dogru sira: once bize verilen $$root a bak, DOSYA ORADAYSA onu' \
		'  # kullan; yalnizca yoksa aramaya dus.' \
		'  if ! [ -e /boot/bzImage ]; then' \
		'    search --no-floppy --set=root --file /boot/bzImage' \
		'  fi' \
		"  linux /boot/bzImage $$MCOS_CMDLINE_BASE" \
		'  initrd /boot/initrd.img' \
		'}' \
		'menuentry "MCOS (Guvenli kip - tum kayit ekranda)" {' \
		'  set gfxpayload=text' \
		'  if ! [ -e /boot/bzImage ]; then' \
		'    search --no-floppy --set=root --file /boot/bzImage' \
		'  fi' \
		'  echo "  Guvenli kip: grafik kipi hic denenmez, tum kayit ekrana basilir."' \
		"  linux /boot/bzImage $$MCOS_CMDLINE_RECOVERY" \
		'  initrd /boot/initrd.img' \
		'}' \
		> "$$D/boot/grub/grub.cfg"; \
	: '─ URETILEN grub.cfg DOGRULANIR ─────────────────────────────────────'; \
	: '  Bu dosya BIR KEZ SESSIZCE 0 BAYT uretildi: printf in argüman'; \
	: '  listesine ": yorum;" satirlari sokulmustu, noktali virgul printf i'; \
	: '  bitiriyor ve kalan satirlar "> grub.cfg" ile dosyayi SIFIRLIYORDU.'; \
	: '  Kabuk yalnizca stderr e "command not found" yazdi, make BASARILI'; \
	: '  dedi ve ISO menusuz cikti. Kullanicinin gordugu: Ventoy grub2'; \
	: '  kipinde menuye geri donus, normal kipte grub> terminali.'; \
	: '  Bir daha sessizce gecmemesi icin URUN denetleniyor, kaynak degil.'; \
	for gerekli in 'menuentry "MCOS"' 'linux /boot/bzImage' 'initrd /boot/initrd.img' 'background_image'; do \
		grep -qF "$$gerekli" "$$D/boot/grub/grub.cfg" || { \
			echo ">> HATA: uretilen grub.cfg eksik: $$gerekli"; \
			echo ">>       (boyut: $$(wc -c < "$$D/boot/grub/grub.cfg") bayt)"; \
			exit 1; }; \
	done; \
	echo ">> grub.cfg dogrulandi ($$(wc -c < "$$D/boot/grub/grub.cfg") bayt, $$(grep -c menuentry "$$D/boot/grub/grub.cfg") menu girdisi)"; \
	printf '\\EFI\\BOOT\\BOOTX64.EFI\r\n' > "$$D/startup.nsh"; \
	: '─ VENTOY ─────────────────────────────────────────────────────────────'; \
	: '  ISO BILEREK "VENTOY COMPATIBLE" olarak ISARETLENMIYOR. Denendi'; \
	: '  (Ventoy 1.1.17, QEMU): isaret normal kipi aciyor ama grub2 kipini'; \
	: '  BOZUYOR - Ventoy uyumlu ISO ya rdinit=/vtoy/vtoy verip kancayi'; \
	: '  yuklemiyor. Normal kipteki panigin asil sebebi cekirdekte 32 bit'; \
	: '  destegi (IA32_EMULATION) olmamasiydi: Ventoy kancasi 32 bit bir'; \
	: '  kabukla calisiyor. kernel.config a bakin; iki kip de boyle aciliyor.'; \
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

## qemu-run: MEVCUT ISO'yu donanim hizlandirmali acar (DERLEME YAPMAZ)
#
# "qemu" hedefinin on kosulu "iso", onunki de "os": yani yalnizca sanal makineyi
# acmak isteyen biri saatlerce Buildroot derlemesi baslatabiliyordu. Bu hedefin
# HICBIR on kosulu yok ve betik de derleme yapmaz.
qemu-run:
	@QEMU_MEM=$(QEMU_MEM) QEMU_SMP=$(QEMU_SMP) sh scripts/qemu.sh

## qemu-run-uefi: ayni sey, UEFI (OVMF) ile
qemu-run-uefi:
	@QEMU_MEM=$(QEMU_MEM) QEMU_SMP=$(QEMU_SMP) sh scripts/qemu.sh --mode uefi

## qemu: boot the ISO in QEMU, BIOS mode, graphical window
qemu: iso
	@command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "qemu: qemu-system-x86_64 not found"; exit 1; }; \
	$(QEMU_ACCEL_CHECK) \
	qemu-system-x86_64 -m $(QEMU_MEM) -smp $(QEMU_SMP) $$ACCEL \
		-cdrom dist/mcos-x86_64.iso -boot d \
		-vga std -display gtk $(QEMU_AUDIO)

## qemu-uefi: boot the ISO in QEMU under UEFI (OVMF)
qemu-uefi: iso
	@command -v qemu-system-x86_64 >/dev/null 2>&1 || { echo "qemu: qemu-system-x86_64 not found"; exit 1; }; \
	OVMF=""; \
	for f in /usr/share/ovmf/OVMF.fd /usr/share/OVMF/OVMF.fd /usr/share/OVMF/OVMF_CODE.fd /usr/share/qemu/OVMF.fd; do \
		[ -f "$$f" ] && { OVMF="$$f"; break; }; \
	done; \
	[ -n "$$OVMF" ] || { echo "qemu-uefi: OVMF firmware not found — install 'ovmf'"; exit 1; }; \
	$(QEMU_ACCEL_CHECK) \
	qemu-system-x86_64 -m $(QEMU_MEM) -smp $(QEMU_SMP) $$ACCEL \
		-bios "$$OVMF" -cdrom dist/mcos-x86_64.iso -boot d \
		-vga std -display gtk $(QEMU_AUDIO)


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
#
# 2G yeterli: veri bolumu ilk acilista USB'nin sonuna kadar buyutulur
# (rootfs-overlay/usr/bin/mcos-growdata). Eskiden 4G'ydi ve buyutme yoktu;
# 32 GB'lik bir USB'de /data 3 GB'ta kaliyordu. Kucuk imaj ayrica yazma ve
# geri-okuyup-dogrulama suresini yariya indirir (sifirlar da yaziliyor).
USB_SIZE    ?= 2G
# Boot bolumu boyutu (cekirdek ~10M + initramfs ~122M + pay).
USB_BOOT_MB ?= 768

## uefi: kalici UEFI USB disk imaji -> dist/mcos-uefi.img
uefi: os
	@bash scripts/mkusb.sh --mode uefi --out dist/mcos-uefi.img \
		--size $(USB_SIZE) --boot-mb $(USB_BOOT_MB)
	@bash scripts/verify-usb.sh dist/mcos-uefi.img uefi

## bios: kalici BIOS (Legacy) USB disk imaji -> dist/mcos-bios.img
bios: os
	@bash scripts/mkusb.sh --mode bios --out dist/mcos-bios.img \
		--size $(USB_SIZE) --boot-mb $(USB_BOOT_MB)
	@bash scripts/verify-usb.sh dist/mcos-bios.img bios

## usb: hem UEFI hem BIOS kalici imajlarini uret
usb: uefi bios

## usb-both: TEK kalici imaj; hem BIOS hem UEFI bilgisayarda acilir -> dist/mcos-usb.img
##
## Windows'tan USB hazirlayan kullanici bilgisayarinin UEFI mi BIOS mu
## oldugunu bilmez; yanlis imaji secerse USB "acilmiyor" gorunur. Bu imaj
## MBR + FAT32 uzerinde iki onyukleyiciyi birden tasir (bkz. mkusb.sh).
usb-both: os
	@bash scripts/mkusb.sh --mode both --out dist/mcos-usb.img \
		--size $(USB_SIZE) --boot-mb $(USB_BOOT_MB)
	@bash scripts/verify-usb.sh dist/mcos-usb.img both

## verify-usb: uretilmis imajlari BOOT ETMEDEN denetle (bolum tablosu, onyukleyici, etiket)
verify-usb:
	@# -- Yakalanan gercek hata --------------------------------------------
	@# Bu hedef eskiden imaj YOKKEN hicbir sey yazmadan 0 ile cikiyordu.
	@# Yani "make verify-usb" sessizce basarili gorunuyor, ama hicbir sey
	@# dogrulanmiyordu. Dogrulama komutunun bos gecmesi, en kotu hata
	@# turudur: kullanici imajin denetlendigini sanip USB'ye yazar.
	@found=0; rc=0; \
	for f in dist/mcos-uefi.img dist/mcos-bios.img dist/mcos-usb.img; do \
		if [ -f "$$f" ]; then \
			found=1; \
			case "$$f" in *uefi*) k=uefi ;; *bios*) k=bios ;; *) k=both ;; esac; \
			bash scripts/verify-usb.sh "$$f" "$$k" || rc=1; \
		fi; \
	done; \
	if [ "$$found" -eq 0 ]; then \
		echo "verify-usb: hicbir imaj bulunamadi (dist/mcos-uefi.img, dist/mcos-bios.img, dist/mcos-usb.img)"; \
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

## windows-usb: Windows kullanicisina verilecek HAZIR klasor + zip
##   dist/windows-usb/  mcos-flash.exe, mcos-flash.bat, mcos-usb.img, BENIOKU.txt
##   dist/mcos-windows-usb.zip
##
## Imaj "both" kipindedir (usb-both): kullanici bilgisayarinin UEFI mi BIOS
## mu oldugunu bilmek zorunda degil. Klasorde TEK imaj bulunur; kurucu
## imajlari kendi klasorunde arar, eski bir imaji yanlislikla one cikarmaz.
## zip: imajin cogu sifir, ~2 GB -> ~150 MB; Windows Gezgini zip64'u acar.
windows-usb: flash-windows usb-both
	@rm -rf dist/windows-usb dist/mcos-windows-usb.zip
	@mkdir -p dist/windows-usb
	@cp dist/flash/mcos-flash.exe "$(FLASH_LAUNCHER_SRC)/mcos-flash.bat" \
		"$(FLASH_LAUNCHER_SRC)/BENIOKU.txt" dist/windows-usb/
	@cp --sparse=always dist/mcos-usb.img dist/windows-usb/mcos-usb.img
	@python3 -c 'import sys, zipfile, os; \
z = zipfile.ZipFile(sys.argv[1], "w", zipfile.ZIP_DEFLATED, allowZip64=True); \
[z.write(os.path.join(sys.argv[2], f), "mcos-usb/" + f) for f in sorted(os.listdir(sys.argv[2]))]; \
z.close()' dist/mcos-windows-usb.zip dist/windows-usb
	@ls -la dist/windows-usb dist/mcos-windows-usb.zip

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
	@$(MAKE) --no-print-directory node-jars NODE_JAR_DIR=dist/node
	@echo "   dist/node/mcos-node  (baslatici: dist/node/mcos-node.sh)"

## node-windows: Windows düğüm paketi -> dist/mcos-node-windows/
##
## CGO'suz, penceresiz (GUI alt sistemi) mcos-node.exe + simge/sürüm bilgisi
## + ortak dünya eklentileri + README.txt. Kullanıcı exe'ye çift tıklar;
## ilk açılışta kendini kurar (güvenlik duvarı için bir kez UAC).
#
# -H windowsgui: arka planda (oturum açılışında) konsol penceresi AÇILMASIN.
# Çift tıklamada konsolu program kendisi açar (platform_windows.go).
# Simge ve sürüm bilgisi cmd/mcos-node/rsrc_windows_amd64.syso'dan gelir;
# sürüm değişince: make node-winres
NODE_PKG_SRC := cmd/mcos-node/packaging
NODE_WIN_DIR := dist/mcos-node-windows
NODE_LINUX_DIR := dist/mcos-node-linux

node-windows:
	@rm -rf "$(NODE_WIN_DIR)" && mkdir -p "$(NODE_WIN_DIR)"
	@echo ">> building mcos-node.exe (windows/amd64, GUI alt sistemi)"
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$(GO)" build -trimpath \
		-ldflags "-s -w -H windowsgui" -o "$(NODE_WIN_DIR)/mcos-node.exe" ./cmd/mcos-node
	@sed 's/$$/\r/' "$(NODE_PKG_SRC)/windows/README.txt" > "$(NODE_WIN_DIR)/README.txt"
	@$(MAKE) --no-print-directory node-jars NODE_JAR_DIR="$(NODE_WIN_DIR)"
	@ls -la "$(NODE_WIN_DIR)"

## node-linux: Linux düğüm paketi -> dist/mcos-node-linux/
##
## Statik ikili + systemd kullanıcı birimi + install.sh + eklentiler.
node-linux:
	@rm -rf "$(NODE_LINUX_DIR)" && mkdir -p "$(NODE_LINUX_DIR)"
	@echo ">> building mcos-node (linux/amd64, statik)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$(GO)" build -trimpath \
		-ldflags "-s -w" -o "$(NODE_LINUX_DIR)/mcos-node" ./cmd/mcos-node
	@cp "$(NODE_PKG_SRC)/linux/install.sh" "$(NODE_PKG_SRC)/linux/mcos-node.service" \
		"$(NODE_PKG_SRC)/linux/README.txt" "$(NODE_LINUX_DIR)/"
	@chmod 755 "$(NODE_LINUX_DIR)/install.sh" "$(NODE_LINUX_DIR)/mcos-node"
	@$(MAKE) --no-print-directory node-jars NODE_JAR_DIR="$(NODE_LINUX_DIR)"
	@ls -la "$(NODE_LINUX_DIR)"

# Ortak dünya jar'ları programın yanındaki mods/link'te aranır
# (cmd/mcos-node/host.go linkLocator; seçim kuralı internal/linkjar).
#
# Eskiden yalnızca iki sabit adlı jar (1.21.11 için derlenmiş) kopyalanıyordu
# ve düğüm onu sürüm sormadan her Fabric sunucusuna kuruyordu: MCOS'un 26.3
# ortak dünyasında o PC'nin yarısı modsuz kalıyordu. Artık dist/mods/link'in
# TAMAMI gider: index-*.tsv + her sürümün jar'ı + fabric-api'ler (düğümün
# internetsiz de kurabilmesi için; paketi ~45 MB büyütür).
#
# İki indeksten (Fabric, Paper) biri yoksa ya da indeksin gösterdiği bir
# dosya eksik/bozuksa paket OLUŞMAZ (post-build'deki imaj denetimiyle aynı
# kural): o PC'de ortak dünya "index-fabric.tsv yok" ya da "kayıtlı ama yok"
# ile düşerdi.
#
# Denetim döngüsünde "|| [ -n ld ]": satır sonu olmayan SON satırı read atlar
# (post-build'de ölçüldü: o satırın eksik jar'ı sessizce geçiyordu).
NODE_LINK_SRC ?= dist/mods/link
node-jars:
	@src="$(NODE_LINK_SRC)"; dst="$(NODE_JAR_DIR)/mods/link"; tab="$$(printf '\t')"; \
	for ld in fabric paper; do \
		if [ ! -s "$$src/index-$$ld.tsv" ]; then \
			echo "   HATA: $$src/index-$$ld.tsv yok - once 'make mod' calistirin"; exit 1; fi; \
	done; \
	for idx in "$$src"/index-*.tsv; do \
		tr -d '\r' < "$$idx" | { hata=0; \
		while IFS="$$tab" read -r ld mc jar dep rest || [ -n "$$ld" ]; do \
			case "$$ld" in ''|'#'*) continue ;; esac; \
			for f in "$$jar" "$$dep"; do \
				if [ -z "$$f" ] || [ "$$f" = "-" ]; then continue; fi; \
				if [ ! -s "$$src/$$f" ] || [ "$$(head -c 2 "$$src/$$f")" != "PK" ]; then \
					echo "   HATA: $$idx $$f dosyasini gosteriyor ama $$src/$$f yok ya da jar degil"; hata=1; fi; \
			done; \
		done; exit $$hata; } || exit 1; \
	done; \
	rm -rf "$$dst" && mkdir -p "$$dst" && \
	cp "$$src"/index-*.tsv "$$src"/*.jar "$$dst"/ || exit 1; \
	echo "   ortak dunya jar'lari -> $$dst ($$(ls "$$dst" | wc -l) dosya)"

## node-winres: mcos-node.exe simge + sürüm kaynağını yeniden üretir
node-winres:
	@VER=$$(sed -n 's/^const Version = "\(.*\)"/\1/p' internal/version/version.go); \
	"$(GO)" run ./cmd/mcos-icon -o /tmp/mcos-node-icon.png -size 256 && \
	cd cmd/mcos-node && GOFLAGS= "$(GO)" run github.com/tc-hib/go-winres@v0.3.3 simply \
		--arch amd64 --out rsrc --icon /tmp/mcos-node-icon.png --manifest gui \
		--product-name "MCOS Düğüm" \
		--file-description "MCOS Düğüm — bu PC'yi MCOS'a ikinci makine olarak ekler" \
		--product-version "$$VER.0" --file-version "$$VER.0" \
		--original-filename mcos-node.exe --copyright "MCOS"
	@rm -f /tmp/mcos-node-icon.png

## node-all: iki düğüm paketi birden
node-all: node-windows node-linux


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
mod: mod-fabric mod-paper mod-velocity
	@ls -l dist/mods/ 2>/dev/null || true

## mod-fabric: ortak dunya modu (Fabric, 1.20.5 .. 26.3) -> dist/mods/link/
##             mcos-link-fabric-<etiket>.jar + surum basina Fabric API jar'i
##             + index-fabric.tsv (daemon hangi surume hangi jar'i kuracagini
##             buradan okur)
#
# Eskiden tek bir 1.21.11 jar'i uretiliyordu ve 1.21.1 gibi baska bir surumde
# sunucu acilista NoSuchFieldError ile dusuyordu. Artik her surum grubu icin
# ayri jar var; gruplar ve dogrulanan surumler mods/mcos-link/targets/ altinda.
# Eski dist/mods/mcos-link.jar'a DOKUNULMAZ (Go tarafi dizine gecene kadar).
#
# Gradle'in KENDISI Java 25 ile calismali: 26.x Minecraft'i Java 25 ister ve
# Loom oyunu Gradle'in JVM'inde acar (Fabric'in 26.1 notu). Eski surumlerin
# jar'lari yine Java 21 bayt kodu olur (options.release=21). Java 25 sirayla
# MOD_JAVA_HOME, JAVA_HOME, ~/toolchains/jdk25 ve /usr/lib/jvm altinda aranir;
# yoksa derleme 26.x'e gelip anlasilmaz bir hatayla kirilmasin diye EN BASTA
# durur.
MOD_JAVA_HOME ?=
mod-fabric:
	@if [ ! -f mods/mcos-link/build.gradle ]; then \
		echo "mods/mcos-link bulunamadi"; exit 1; \
	fi
	@mkdir -p dist/mods/link
	@jh=""; \
	for c in "$(MOD_JAVA_HOME)" "$$JAVA_HOME" "$$HOME/toolchains/jdk25" /usr/lib/jvm/*25*; do \
		[ -n "$$c" ] && [ -x "$$c/bin/java" ] || continue; \
		v=$$("$$c/bin/java" -XshowSettings:properties -version 2>&1 | sed -n 's/^ *java.specification.version = //p'); \
		if [ "$${v%%.*}" -ge 25 ] 2>/dev/null; then jh="$$c"; break; fi; \
	done; \
	if [ -z "$$jh" ]; then \
		echo "HATA: Java 25 bulunamadi (26.x derlemesi icin gerekli)."; \
		echo "  make mod-fabric MOD_JAVA_HOME=/jdk25/yolu   ya da   ~/toolchains/jdk25"; \
		exit 1; \
	fi; \
	echo "mod-fabric: Java $$jh"; \
	cd mods/mcos-link && \
	if [ -x ./gradlew ]; then JAVA_HOME="$$jh" ./gradlew build --no-daemon; \
	else JAVA_HOME="$$jh" gradle build --no-daemon; fi

## mod-paper: ortak dunya eklentisi (Paper/Purpur) -> dist/mods/mcos-link-paper.jar
mod-paper:
	@if [ ! -f mods/mcos-link-paper/build.gradle ]; then \
		echo "mods/mcos-link-paper bulunamadi"; exit 1; \
	fi
	@mkdir -p dist/mods
	@cd mods/mcos-link-paper && (./gradlew build --no-daemon || gradle build --no-daemon)

## mod-velocity: kurucunun Velocity proxy'si icin eklenti -> dist/mods/link/
##               mcos-link-velocity.jar + index-velocity.tsv
#
# Arka uc listesi degisince (yeni PC, IP/port degisimi, kopya acilip kapandi)
# Velocity YENIDEN BASLATILIYORDU ve proxy'deki herkes dusuyordu. Eklenti
# listeyi MCOS'tan (/link/proxy) okuyup calisan proxy'ye kendisi uygular.
# velocity-api 4.2.0 Java 25 bayt kodudur; Java 25 mod-fabric'teki sirayla
# aranir (MOD_JAVA_HOME, JAVA_HOME, ~/toolchains/jdk25, /usr/lib/jvm).
mod-velocity:
	@if [ ! -f mods/mcos-link-velocity/build.gradle ]; then \
		echo "mods/mcos-link-velocity bulunamadi"; exit 1; \
	fi
	@mkdir -p dist/mods/link
	@jh=""; \
	for c in "$(MOD_JAVA_HOME)" "$$JAVA_HOME" "$$HOME/toolchains/jdk25" /usr/lib/jvm/*25*; do \
		[ -n "$$c" ] && [ -x "$$c/bin/java" ] || continue; \
		v=$$("$$c/bin/java" -XshowSettings:properties -version 2>&1 | sed -n 's/^ *java.specification.version = //p'); \
		if [ "$${v%%.*}" -ge 25 ] 2>/dev/null; then jh="$$c"; break; fi; \
	done; \
	if [ -z "$$jh" ]; then \
		echo "HATA: Java 25 bulunamadi (velocity-api 4.2.0 icin gerekli)."; \
		echo "  make mod-velocity MOD_JAVA_HOME=/jdk25/yolu   ya da   ~/toolchains/jdk25"; \
		exit 1; \
	fi; \
	cd mods/mcos-link-velocity && \
	if [ -x ./gradlew ]; then JAVA_HOME="$$jh" ./gradlew build --no-daemon -q; \
	else JAVA_HOME="$$jh" gradle build --no-daemon -q; fi

## shots: arayuzun her ekranini PNG olarak basar (donanim gerekmez)
shots:
	@sh scripts/shots.sh
