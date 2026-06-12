#!/usr/bin/env bash
# Build a BIOS+UEFI hybrid bootable MCOS ISO from the Buildroot images.
# Deterministic (does not rely on grub-mkrescue auto-detection): builds an
# x86_64-efi standalone bootloader for UEFI and an i386-pc El Torito image for
# BIOS, then assembles an isohybrid ISO that boots both from optical and USB.
set -euo pipefail

BR_OUTPUT="${1:?usage: mkiso.sh <BR_OUTPUT> <ISO_OUT>}"
ISO_OUT="${2:?usage: mkiso.sh <BR_OUTPUT> <ISO_OUT>}"
GRUBLIB=/usr/lib/grub
D=dist/iso

command -v grub-mkstandalone >/dev/null || { echo "need grub-common"; exit 1; }
command -v mkfs.vfat >/dev/null || { echo "need dosfstools"; exit 1; }
command -v mcopy >/dev/null || { echo "need mtools"; exit 1; }
[ -d "$GRUBLIB/x86_64-efi" ] || { echo "need grub-efi-amd64-bin"; exit 1; }
[ -d "$GRUBLIB/i386-pc" ] || { echo "need grub-pc-bin"; exit 1; }

rm -rf "$D" "$ISO_OUT"
mkdir -p "$D/boot/grub" "$D/EFI/BOOT"
cp "$BR_OUTPUT/images/bzImage" "$D/boot/bzImage"
cp "$BR_OUTPUT/images/rootfs.cpio.gz" "$D/boot/initrd.img"

cat > "$D/boot/grub/grub.cfg" <<'EOF'
set timeout=5
set default=0
insmod all_video
insmod gfxterm
set gfxmode=auto
terminal_output gfxterm
menuentry "MCOS" {
  set gfxpayload=keep
  linux /boot/bzImage console=tty0 console=ttyS0,115200 video=1024x768 loglevel=3
  initrd /boot/initrd.img
}
menuentry "MCOS (safe / nomodeset)" {
  linux /boot/bzImage console=tty0 nomodeset loglevel=3
  initrd /boot/initrd.img
}
EOF

# Modules needed for ISO boot (keep minimal to stay under BIOS 480 KB limit).
# all_video + gfxterm give the kernel a real framebuffer console (so setfont +
# full colour work and the panel renders properly, esp. in legacy BIOS mode).
COMMON_MODS="normal linux search search_fs_file search_fs_uuid search_label configfile echo test all_video gfxterm"
EFI_MODS="$COMMON_MODS iso9660 fat part_gpt part_msdos efi_gop efi_uga"

# --- UEFI bootloader (BOOTX64.EFI) + FAT ESP image -------------------------
grub-mkstandalone -O x86_64-efi \
  --modules="$EFI_MODS" --install-modules="$EFI_MODS" \
  -o "$D/EFI/BOOT/BOOTX64.EFI" \
  "boot/grub/grub.cfg=$D/boot/grub/grub.cfg"
dd if=/dev/zero of="$D/boot/grub/efiboot.img" bs=1M count=8 status=none
mkfs.vfat -n MCOS-EFI "$D/boot/grub/efiboot.img" >/dev/null
mmd  -i "$D/boot/grub/efiboot.img" ::/EFI ::/EFI/BOOT
mcopy -i "$D/boot/grub/efiboot.img" "$D/EFI/BOOT/BOOTX64.EFI" ::/EFI/BOOT/

# --- BIOS bootloader (El Torito) -------------------------------------------
# Use grub-mkimage (not grub-mkstandalone) to avoid the memdisk that bloats
# the core image past the i386-pc 480 KB hard limit.
grub-mkimage -O i386-pc -p /boot/grub \
  -o "$D/boot/grub/core.img" \
  biosdisk iso9660 normal linux search search_fs_file search_label \
  configfile echo test part_msdos part_gpt all_video gfxterm vbe vga video_bochs video_cirrus
cat "$GRUBLIB/i386-pc/cdboot.img" "$D/boot/grub/core.img" > "$D/boot/grub/bios.img"

# --- assemble isohybrid (BIOS + UEFI, optical + USB) -----------------------
xorriso -as mkisofs -iso-level 3 -volid MCOS \
  -eltorito-boot boot/grub/bios.img \
    -no-emul-boot -boot-load-size 4 -boot-info-table \
    --eltorito-catalog boot/grub/boot.cat \
  -eltorito-alt-boot \
    -e boot/grub/efiboot.img -no-emul-boot \
    -isohybrid-gpt-basdat \
  -isohybrid-mbr "$GRUBLIB/i386-pc/boot_hybrid.img" \
  -o "$ISO_OUT" "$D"
echo ">> ISO ready: $ISO_OUT"
