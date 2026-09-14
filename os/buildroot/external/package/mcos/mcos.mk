################################################################################
#
# mcos
#
################################################################################

MCOS_VERSION = 1.0.1
# Sync ONLY the cross-compiled linux binaries (built by 'make linux'); pointing
# at the repo root makes the local rsync drag in os/buildroot/buildroot (the
# whole Buildroot source, hundreds of MB) — that stalls the build and bloats
# the output dir.
MCOS_SITE = $(BR2_EXTERNAL_MCOS_PATH)/../../../bin/linux
MCOS_SITE_METHOD = local
MCOS_LICENSE = MIT

# The Go binaries must already be cross-compiled via 'make linux' from the
# project root. This package only installs them into the target rootfs.
define MCOS_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/mcosd $(TARGET_DIR)/usr/bin/mcosd
	$(INSTALL) -D -m 0755 $(@D)/mcosctl $(TARGET_DIR)/usr/bin/mcosctl
	$(INSTALL) -D -m 0755 $(@D)/mcos-panel $(TARGET_DIR)/usr/bin/mcos-panel
	$(INSTALL) -D -m 0755 $(@D)/mcos-detect $(TARGET_DIR)/usr/bin/mcos-detect
	$(INSTALL) -D -m 0755 $(@D)/mcos-panel-fb $(TARGET_DIR)/usr/bin/mcos-panel-fb
	# Acilis animasyonu. Panelden AYRI bir ikili: servisler baslarken
	# calisir ve panel hazir olunca son karesini birakip cikar.
	$(INSTALL) -D -m 0755 $(@D)/mcos-splash $(TARGET_DIR)/usr/bin/mcos-splash
endef

$(eval $(generic-package))
