#!/usr/bin/env sh
set -e

REPO="jhgundersen/magazine-builder"
BINARY="magazine-builder"
INSTALL_DIR="${PREFIX:-$HOME/.local}/bin"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

ASSET="${BINARY}-${OS}-${ARCH}"

# Resolve latest tag
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
if [ -z "$TAG" ]; then
  echo "Could not determine latest release" >&2
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

echo "Installing ${BINARY} ${TAG} (${OS}/${ARCH})..."
mkdir -p "$INSTALL_DIR"
curl -fsSL "$URL" -o "$INSTALL_DIR/$BINARY"
chmod +x "$INSTALL_DIR/$BINARY"
echo "Installed: $INSTALL_DIR/$BINARY"

echo ""
echo "Installing defapi-cli..."
curl -fsSL https://raw.githubusercontent.com/jhgundersen/defapi-cli/master/install.sh | PREFIX="${PREFIX:-$HOME/.local}" sh

echo ""
echo "Done. Make sure $INSTALL_DIR is in your PATH."
