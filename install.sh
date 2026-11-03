#!/bin/sh
# Print Agent installer for Linux, Raspberry Pi and macOS:
#   curl -fsSL https://raw.githubusercontent.com/vinayk-kumar/printer/main/install.sh | sh
# Non-interactive:
#   curl -fsSL .../install.sh | sh -s -- --printer-ip 192.168.1.50 --email shop@gmail.com --password "xxxx xxxx xxxx xxxx" --yes
set -eu

REPO="vinayk-kumar/printer"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *) echo "Unsupported system: $os (on Windows use install.ps1)"; exit 1 ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU: $arch (Raspberry Pi needs the 64-bit Raspberry Pi OS)"; exit 1 ;;
esac

# When piped from curl, stdin is this script, so read answers from the
# terminal (or from nothing when there is none, e.g. with --yes in a script).
tty=/dev/null
if (: </dev/tty) 2>/dev/null; then tty=/dev/tty; fi

if [ "$os" = linux ]; then
  # The CUPS printing system (missing on Raspberry Pi OS Lite and servers).
  if ! command -v lp >/dev/null 2>&1; then
    if ! command -v apt-get >/dev/null 2>&1; then
      echo "Install the CUPS printing system first, then run this again."
      exit 1
    fi
    echo "Installing the CUPS printing system (asks for your password)..."
    sudo apt-get update -qq <"$tty"
    sudo apt-get install -y -qq cups <"$tty"
    sudo usermod -aG lpadmin "$(id -un)" <"$tty"
  fi

  # An HP USB printer that isn't set up yet (e.g. LaserJet M1005): install
  # HP's driver and run HP's setup, which also fetches HP's plugin if needed.
  if command -v lsusb >/dev/null 2>&1 && lsusb | grep -qiE "hewlett|hp, inc" &&
    ! LC_ALL=C lpstat -v 2>/dev/null | grep -qE "usb:|hp:/usb"; then
    echo "An HP USB printer is connected but not set up yet."
    if ! command -v hp-setup >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then
      echo "Installing HP's printer driver (hplip)..."
      sudo apt-get install -y -qq hplip <"$tty"
    fi
    if command -v hp-setup >/dev/null 2>&1; then
      echo "Starting HP's printer setup. Choose the USB printer and accept the defaults."
      sudo hp-setup -i <"$tty" || echo "HP setup did not finish; you can run 'sudo hp-setup -i' later."
    fi
  fi
fi

if ! command -v lp >/dev/null 2>&1; then
  echo "The CUPS printing system is required but was not found."
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "Downloading Print Agent ($os/$arch)..."
curl -fsSL "https://github.com/$REPO/releases/latest/download/print-agent-$os-$arch" -o "$tmp/print-agent"
chmod +x "$tmp/print-agent"

"$tmp/print-agent" install "$@" <"$tty"
