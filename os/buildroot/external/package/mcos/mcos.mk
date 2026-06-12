################################################################################
#
# mcos
#
################################################################################

MCOS_VERSION = 0.1.0
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
endef

$(eval $(generic-package))
