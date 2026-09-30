#!/usr/bin/env bash
# setup-wsl.sh — prepare a WSL2/Ubuntu (or Debian) host to build MCOS.
#
# Installs:
#   - Go toolchain (for mcosd / mcos-panel / mcos-detect / mcosctl)
#   - Rust toolchain + linux-gnu target (for mcos-panel-lite)
#   - Buildroot build dependencies (for the OS image / ISO, M9-M10)
#   - QEMU + OVMF (for BIOS/UEFI boot testing, M10)
#
# Usage:  bash scripts/setup-wsl.sh
set -euo pipefail

GO_VERSION="${GO_VERSION:-1.26.3}"

log() { printf '\033[1;33m>> %s\033[0m\n' "$*"; }

if ! command -v apt-get >/dev/null 2>&1; then
  echo "This script targets Debian/Ubuntu (apt). Adapt for your distro." >&2
  exit 1
fi

log "Updating apt and installing Buildroot + ISO toolchain dependencies"
sudo apt-get update
sudo apt-get install -y --no-install-recommends \
  build-essential gcc g++ make file wget cpio unzip rsync bc \
  python3 git libncurses-dev flex bison gawk libelf-dev libssl-dev \
  xorriso syslinux isolinux dosfstools mtools \
  qemu-system-x86 ovmf ca-certificates curl

# --- Go -------------------------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
  log "Installing Go ${GO_VERSION}"
  tmp="$(mktemp -d)"
  wget -qO "$tmp/go.tgz" "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "$tmp/go.tgz"
  rm -rf "$tmp"
  if ! grep -q '/usr/local/go/bin' "$HOME/.profile" 2>/dev/null; then
    echo 'export PATH=$PATH:/usr/local/go/bin' >> "$HOME/.profile"
  fi
  export PATH=$PATH:/usr/local/go/bin
fi
log "Go: $(/usr/local/go/bin/go version 2>/dev/null || go version)"

# --- Rust -----------------------------------------------------------------
if ! command -v cargo >/dev/null 2>&1; then
  log "Installing Rust via rustup"
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  # shellcheck disable=SC1090
  source "$HOME/.cargo/env"
fi
log "Ensuring x86_64-unknown-linux-gnu Rust target"
rustup target add x86_64-unknown-linux-gnu || true
log "Rust: $(rustc --version)"

log "Done. Open a new shell (or 'source ~/.profile') so PATH picks up Go."
log "Next: 'make app && make test' to build and test the app layer."
