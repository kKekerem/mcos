#!/bin/sh
# Post-build script for MCOS Buildroot board.

set -e

BOARD_DIR="$(dirname "$0")"
OVERLAY="${BOARD_DIR}/rootfs-overlay"

normalize() {
    for base in "$OVERLAY" "$TARGET_DIR"; do
        f="$base/$1"
        [ -f "$f" ] && sed -i 's/\r$//' "$f" 2>/dev/null || true
    done
}
makeexec() {
    for base in "$OVERLAY" "$TARGET_DIR"; do
        f="$base/$1"
        [ -f "$f" ] && chmod 755 "$f" 2>/dev/null || true
    done
}

for s in etc/init.d/S99mcos usr/bin/mcos-launch usr/bin/mcos-persist; do
    normalize "$s"
    makeexec "$s"
done
normalize etc/inittab

# Install keymaps and console fonts
KEYMAP_DST="${TARGET_DIR}/usr/share/keymaps/i386/qwerty"
FONT_DST="${TARGET_DIR}/usr/share/consolefonts"
if [ -n "${BUILD_DIR:-}" ]; then
    KEYMAP_SRC="$(ls "${BUILD_DIR}"/kbd-*/data/keymaps/i386/qwerty/trq.map.gz 2>/dev/null | head -1)"
    if [ -n "$KEYMAP_SRC" ] && [ -f "$KEYMAP_SRC" ]; then
        mkdir -p "$KEYMAP_DST"
        cp "$KEYMAP_SRC" "$KEYMAP_DST/"
    fi
    
    FONT_SRC="$(ls "${BUILD_DIR}"/kbd-*/data/consolefonts/ter-u16n.psf.gz 2>/dev/null | head -1)"
    if [ -n "$FONT_SRC" ] && [ -f "$FONT_SRC" ]; then
        mkdir -p "$FONT_DST"
        cp "$FONT_SRC" "$FONT_DST/"
    fi
fi

# Create required directories
mkdir -p "${TARGET_DIR}/data"
mkdir -p "${TARGET_DIR}/boot"
mkdir -p "${TARGET_DIR}/run/mcos"
mkdir -p "${TARGET_DIR}/usr/lib"
mkdir -p "${TARGET_DIR}/lib"
mkdir -p "${TARGET_DIR}/root"
mkdir -p "${TARGET_DIR}/etc/skel"
mkdir -p "${TARGET_DIR}/usr/share/fonts"
mkdir -p "${TARGET_DIR}/etc/fonts"

# Flatten font directory so fontconfig finds DejaVu fonts at top-level /usr/share/fonts
if [ -d "${TARGET_DIR}/usr/share/fonts/dejavu" ]; then
    cp -a "${TARGET_DIR}/usr/share/fonts/dejavu/"*.ttf "${TARGET_DIR}/usr/share/fonts/" 2>/dev/null || true
fi

# Stage C++ runtime libraries (libstdc++.so.6 and libgcc_s.so.1) required by fbterm
if [ -n "${HOST_DIR:-}" ]; then
    TC_LIB="$(find "${HOST_DIR}" -name 'libstdc++.so.6*' -type f 2>/dev/null | head -1)"
    if [ -n "$TC_LIB" ] && [ -f "$TC_LIB" ]; then
        LIB_DIR="$(dirname "$TC_LIB")"
        cp -a "$LIB_DIR"/libstdc++.so* "${TARGET_DIR}/usr/lib/" 2>/dev/null || true
        cp -a "$LIB_DIR"/libgcc_s.so* "${TARGET_DIR}/usr/lib/" 2>/dev/null || true
        cp -a "$LIB_DIR"/libstdc++.so* "${TARGET_DIR}/lib/" 2>/dev/null || true
        cp -a "$LIB_DIR"/libgcc_s.so* "${TARGET_DIR}/lib/" 2>/dev/null || true
    fi
fi

# Purge any host fontconfig cache files so target regenerates clean target paths on first boot
rm -rf "${TARGET_DIR}/var/cache/fontconfig" "${TARGET_DIR}/var/lib/fontconfig"
mkdir -p "${TARGET_DIR}/var/cache/fontconfig"

# Prefer a stable mono face for seamless box-drawing. Noto Emoji remains the
# fallback for emoji, followed by Liberation Mono for broad text coverage.
cat > "${TARGET_DIR}/etc/fonts/local.conf" << 'EOF'
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <alias>
    <family>monospace</family>
    <prefer>
      <family>DejaVu Sans Mono</family>
      <family>Liberation Mono</family>
      <family>FontAwesome</family>
    </prefer>
  </alias>
  <match target="font">
    <edit name="rgba" mode="assign"><const>none</const></edit>
    <edit name="hinting" mode="assign"><bool>true</bool></edit>
    <edit name="autohint" mode="assign"><bool>false</bool></edit>
    <edit name="hintstyle" mode="assign"><const>hintslight</const></edit>
    <edit name="antialias" mode="assign"><bool>true</bool></edit>
  </match>
</fontconfig>
EOF

# Generate fbterm configuration (.fbtermrc) with font fallback and correct color palette
cat > "${TARGET_DIR}/root/.fbtermrc" << 'EOF'
font-names=DejaVu Sans Mono,Liberation Mono,FontAwesome,mono
font-size=14
font-height=0
font-width=0
font-space=0
font-space-adjust=0
font-color=7
color-0=0B0D10
color-1=FF6B7A
color-2=75E0A3
color-3=FFD166
color-4=15191F
color-5=8A98A5
color-6=7EE7D0
color-7=EFF3F0
color-8=8A7F88
color-9=FF6B6B
color-10=5DD39E
color-11=FFC857
color-12=7AA2F7
color-13=C8A6FF
color-14=5DD39E
color-15=FFFFFF
ambiguous-wide=0
screen-rotate=0
vesa-mode=0
cursor-shape=0
cursor-interval=500
input-method=
EOF
cp "${TARGET_DIR}/root/.fbtermrc" "${TARGET_DIR}/etc/skel/.fbtermrc" 2>/dev/null || true

# Clean up any leftover boot binaries in target rootfs so initrd doesn't recursively pack itself!
rm -f "${TARGET_DIR}/boot/initrd.img" "${TARGET_DIR}/boot/rootfs.cpio.gz" 2>/dev/null || true

# Copy bzImage into target boot directory for installed systems to use
if [ -n "${BINARIES_DIR:-}" ]; then
    [ -f "${BINARIES_DIR}/bzImage" ] && cp "${BINARIES_DIR}/bzImage" "${TARGET_DIR}/boot/bzImage" || true
fi
