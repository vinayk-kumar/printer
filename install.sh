#!/bin/sh
# Print Agent installer for Linux and macOS:
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
  *) echo "Unsupported CPU: $arch"; exit 1 ;;
esac

if ! command -v lp >/dev/null 2>&1; then
  echo "The CUPS printing system is required. Install it first (Ubuntu/Debian: sudo apt install cups), then run this again."
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "Downloading Print Agent ($os/$arch)..."
curl -fsSL "https://github.com/$REPO/releases/latest/download/print-agent-$os-$arch" -o "$tmp/print-agent"
chmod +x "$tmp/print-agent"

# When piped from curl, stdin is this script, so read answers from the terminal.
if (: </dev/tty) 2>/dev/null; then
  "$tmp/print-agent" install "$@" </dev/tty
else
  "$tmp/print-agent" install "$@"
fi
