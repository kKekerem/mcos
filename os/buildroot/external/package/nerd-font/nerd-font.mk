################################################################################
#
# nerd-font
#
################################################################################

NERD_FONT_VERSION = 3.1.1
NERD_FONT_SITE = https://github.com/ryanoasis/nerd-fonts/releases/download/v$(NERD_FONT_VERSION)
NERD_FONT_SOURCE = FiraCode.zip

define NERD_FONT_EXTRACT_CMDS
	unzip -d $(@D) $(NERD_FONT_DL_DIR)/$(NERD_FONT_SOURCE)
endef

define NERD_FONT_INSTALL_TARGET_CMDS
	mkdir -p $(TARGET_DIR)/usr/share/fonts/truetype/nerd-font
	cp $(@D)/FiraCodeNerdFont-Regular.ttf $(TARGET_DIR)/usr/share/fonts/truetype/nerd-font/
endef

$(eval $(generic-package))
