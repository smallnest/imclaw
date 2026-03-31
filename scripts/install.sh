#!/bin/bash
#
# IMClaw CLI Installer
# Automatically downloads and installs imclaw-cli from GitHub Releases
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/smallnest/imclaw/main/scripts/install.sh | bash
#
# Environment variables:
#   INSTALL_DIR - Directory to install imclaw-cli (default: ~/bin or ~/.local/bin)
#   VERSION     - Version to install (default: latest)
#

set -e

REPO="smallnest/imclaw"
BINARY="imclaw-cli"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Detect OS
detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        darwin*) OS="darwin" ;;
        linux*)  OS="linux" ;;
        mingw*|msys*|cygwin*) OS="windows" ;;
        *) log_error "Unsupported OS: $OS"; exit 1 ;;
    esac
    echo "$OS"
}

# Detect architecture
detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        i386|i686) ARCH="386" ;;
        *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
    esac
    echo "$ARCH"
}

# Get latest release version
get_latest_version() {
    local version
    version=$(curl -sf https://api.github.com/repos/${REPO}/releases/latest | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        log_error "Failed to get latest version"
        exit 1
    fi
    echo "$version"
}

# Determine install directory
get_install_dir() {
    if [ -n "$INSTALL_DIR" ]; then
        echo "$INSTALL_DIR"
        return
    fi

    # Check common paths
    for dir in "$HOME/bin" "$HOME/.local/bin"; do
        if [ -d "$dir" ] && echo "$PATH" | grep -q "$dir"; then
            echo "$dir"
            return
        fi
    done

    # Default to ~/bin
    echo "$HOME/bin"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Main installation
main() {
    log_info "IMClaw CLI Installer"

    # Check if already installed
    if command_exists "$BINARY"; then
        log_warn "$BINARY is already installed at: $(command -v $BINARY)"
        if [ -z "$FORCE_INSTALL" ]; then
            log_info "Set FORCE_INSTALL=1 to reinstall"
            exit 0
        fi
    fi

    # Detect platform
    OS=$(detect_os)
    ARCH=$(detect_arch)
    log_info "Detected platform: ${OS}/${ARCH}"

    # Get version
    if [ -n "$VERSION" ]; then
        VERSION="${VERSION}"
    else
        VERSION=$(get_latest_version)
    fi
    log_info "Installing version: ${VERSION}"

    # Determine download file name
    if [ "$OS" = "windows" ]; then
        ARCHIVE_NAME="imclaw_${OS}_${ARCH}.zip"
    else
        ARCHIVE_NAME="imclaw_${OS}_${ARCH}.tar.gz"
    fi

    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"
    log_info "Download URL: ${DOWNLOAD_URL}"

    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap "rm -rf $TMP_DIR" EXIT

    # Download
    log_info "Downloading..."
    if ! curl -sfL "$DOWNLOAD_URL" -o "$TMP_DIR/$ARCHIVE_NAME"; then
        log_error "Failed to download from $DOWNLOAD_URL"
        log_info "Please check if the release exists at: https://github.com/${REPO}/releases"
        exit 1
    fi

    # Extract
    log_info "Extracting..."
    cd "$TMP_DIR"
    if [ "$OS" = "windows" ]; then
        unzip -q "$ARCHIVE_NAME"
    else
        tar xzf "$ARCHIVE_NAME"
    fi

    # Find the binary
    if [ ! -f "$BINARY" ]; then
        # Might be in a subdirectory
        BINARY_PATH=$(find . -name "$BINARY" -type f | head -1)
        if [ -z "$BINARY_PATH" ]; then
            log_error "Binary $BINARY not found in archive"
            exit 1
        fi
        BINARY="$BINARY_PATH"
    fi

    # Install
    INSTALL_DIR=$(get_install_dir)
    mkdir -p "$INSTALL_DIR"

    mv "$BINARY" "$INSTALL_DIR/$BINARY"
    chmod +x "$INSTALL_DIR/$BINARY"

    log_success "Installed $BINARY to $INSTALL_DIR/$BINARY"

    # Check if in PATH
    if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
        log_warn "$INSTALL_DIR is not in your PATH"
        echo ""
        echo "Add the following to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo ""
        echo "    export PATH=\"\$PATH:$INSTALL_DIR\""
        echo ""
        echo "Then run: source ~/.bashrc  # or source ~/.zshrc"
    fi

    # Verify
    log_info "Verifying installation..."
    "$INSTALL_DIR/$BINARY" --help > /dev/null 2>&1 || true

    log_success "Installation complete!"
    echo ""
    echo "Run '$BINARY --help' to get started."
}

main "$@"
