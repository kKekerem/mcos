################################################################################
#
# nerd-font
#
################################################################################

NERD_FONT_SITE = $(BR2_EXTERNAL_MCOS_PATH)/board/mcos/rootfs-overlay/usr/share/fonts/truetype
NERD_FONT_SITE_METHOD = local
NERD_FONT_LICENSE = OFL-1.1

define NERD_FONT_INSTALL_TARGET_CMDS
	mkdir -p $(TARGET_DIR)/usr/share/fonts/truetype/nerd-font
	cp $(@D)/FiraCodeNerdFont-Regular.ttf $(TARGET_DIR)/usr/share/fonts/truetype/nerd-font/
	cp $(@D)/NotoEmoji-Regular.ttf $(TARGET_DIR)/usr/share/fonts/truetype/nerd-font/
endef

$(eval $(generic-package))
