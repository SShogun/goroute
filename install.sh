#!/usr/bin/env bash
set -euo pipefail

# GoRoute 1-Line Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/ziuus/goroute/main/install.sh | bash

REPO="ziuus/goroute"
INSTALL_DIR="${HOME}/.local/bin"
BINARY_NAME="goroute"

echo "⚡ Installing GoRoute - High-Performance Multi-Provider LLM Gateway..."

# Detect OS & Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64) ARCH="x86_64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        echo "❌ Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

case "$OS" in
    linux|darwin) ;;
    *)
        echo "❌ Unsupported operating system: $OS"
        exit 1
        ;;
esac

mkdir -p "$INSTALL_DIR"

# Check if go is available for instant compilation or fetch latest GitHub release
INSTALLED=false

if command -v go >/dev/null 2>&1; then
    echo "📦 Go toolchain detected. Compiling latest GoRoute binary..."
    GOBIN="$INSTALL_DIR" go install "github.com/${REPO}/cmd/goroute@latest" || true
    if [ -x "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        INSTALLED=true
    fi
fi

if [ "$INSTALLED" = false ]; then
    echo "🌐 Downloading latest pre-compiled binary from GitHub Releases..."
    LATEST_TAG=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep -Po '"tag_name": "\K.*?(?=")' || echo "v0.1.0")
    RELEASE_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/goroute_${OS}_${ARCH}.tar.gz"

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    if curl -sL --fail "$RELEASE_URL" -o "${TMP_DIR}/goroute.tar.gz"; then
        tar -xzf "${TMP_DIR}/goroute.tar.gz" -C "$TMP_DIR"
        mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
        chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
        INSTALLED=true
    fi
fi

if [ "$INSTALLED" = false ]; then
    echo "❌ Installation failed. Please ensure Go is installed or check https://github.com/${REPO}/releases"
    exit 1
fi

# Ensure install directory is in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo "ℹ️  Adding $INSTALL_DIR to PATH in shell profile..."
    SHELL_PROFILE="${HOME}/.bashrc"
    if [ -f "${HOME}/.zshrc" ]; then
        SHELL_PROFILE="${HOME}/.zshrc"
    fi
    echo "export PATH=\"\$PATH:${INSTALL_DIR}\"" >> "$SHELL_PROFILE"
fi

echo ""
echo "🎉 GoRoute successfully installed to: ${INSTALL_DIR}/${BINARY_NAME}"
echo ""
echo "Next steps:"
echo "  1. Authenticate Antigravity (or other providers):"
echo "     goroute auth login antigravity"
echo ""
echo "  2. Configure your coding agents in 1 click:"
echo "     goroute setup all"
echo ""
echo "  3. Start the gateway:"
echo "     goroute serve"
echo "     (or enable background service: goroute autostart enable)"
echo ""
