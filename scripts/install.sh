#!/usr/bin/env bash
set -euo pipefail

REPO="shaiknoorullah/wtfrc"
INSTALL_DIR="${HOME}/.local/bin"

detect_os() {
    case "$(uname -s)" in
        Linux)  echo "linux" ;;
        Darwin) echo "darwin" ;;
        *)      echo "unsupported"; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             echo "unsupported"; exit 1 ;;
    esac
}

main() {
    local os arch version url tmpdir

    os=$(detect_os)
    arch=$(detect_arch)

    echo "Detecting system: ${os}/${arch}"

    # Get latest release version
    version=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        echo "Error: Could not determine latest version"
        exit 1
    fi

    echo "Installing wtfrc v${version}..."

    url="https://github.com/${REPO}/releases/download/v${version}/wtfrc_${os}_${arch}.tar.gz"

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    echo "Downloading from ${url}..."
    curl -sL "$url" | tar xz -C "$tmpdir"

    mkdir -p "$INSTALL_DIR"
    mv "$tmpdir/wtfrc" "$INSTALL_DIR/wtfrc"
    chmod +x "$INSTALL_DIR/wtfrc"

    echo "Installed wtfrc to ${INSTALL_DIR}/wtfrc"

    if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
        echo ""
        echo "Add ${INSTALL_DIR} to your PATH:"
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi

    echo ""
    echo "Get started:"
    echo "  wtfrc setup"
}

main
