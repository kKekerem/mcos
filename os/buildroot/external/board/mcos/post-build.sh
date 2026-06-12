#!/bin/sh
# Post-build script for MCOS Buildroot board.
#
# Hardening for files authored on Windows/WSL: (1) strip CR line endings so
# busybox init/sh don't choke on a "\r" in the shebang, (2) set the exec bit
# (Windows/9p stages overlay files as 0644), (3) create runtime dirs. Without
# (1)+(2) init cannot exec mcos-launch and PID 1 panics. We touch BOTH the
# overlay source and the staged $TARGET_DIR copy so it works regardless of
# whether this script runs before or after the overlay is applied.

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

# Install the Turkish-Q (trq) console keymap so `loadkeys trq` works. The kbd
# package builds the keymaps but Buildroot does not stage them into the rootfs
# by default, so copy it out of the kbd build tree.
KEYMAP_DST="${TARGET_DIR}/usr/share/keymaps/i386/qwerty"
FONT_DST="${TARGET_DIR}/usr/share/consolefonts"
if [ -n "${BUILD_DIR:-}" ]; then
    KEYMAP_SRC="$(ls "${BUILD_DIR}"/kbd-*/data/keymaps/i386/qwerty/trq.map.gz 2>/dev/null | head -1)"
    if [ -n "$KEYMAP_SRC" ] && [ -f "$KEYMAP_SRC" ]; then
        mkdir -p "$KEYMAP_DST"
        cp "$KEYMAP_SRC" "$KEYMAP_DST/"
    fi
    
    # Also grab a good console font with Turkish support (ter-u16n supports iso8859-9)
    FONT_SRC="$(ls "${BUILD_DIR}"/kbd-*/data/consolefonts/ter-u16n.psf.gz 2>/dev/null | head -1)"
    if [ -n "$FONT_SRC" ] && [ -f "$FONT_SRC" ]; then
        mkdir -p "$FONT_DST"
        cp "$FONT_SRC" "$FONT_DST/"
    fi

    # Stage extlinux and MBR for the installer to use on the target machine
    EXTLINUX_SRC="$(find "${BUILD_DIR}"/syslinux-* -name extlinux -type f -executable | grep bios | head -1)"
    MBR_SRC="$(find "${BUILD_DIR}"/syslinux-* -name mbr.bin -type f | head -1)"
    if [ -n "$EXTLINUX_SRC" ]; then
        cp "$EXTLINUX_SRC" "${TARGET_DIR}/usr/sbin/"
    fi
    if [ -n "$MBR_SRC" ]; then
        mkdir -p "${TARGET_DIR}/usr/share/syslinux"
        cp "$MBR_SRC" "${TARGET_DIR}/usr/share/syslinux/"
    fi
fi

# Create required directories.
mkdir -p "${TARGET_DIR}/data"
mkdir -p "${TARGET_DIR}/boot"
mkdir -p "${TARGET_DIR}/run/mcos"
