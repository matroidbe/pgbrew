#!/bin/bash
set -e

# pgbrew installer/upgrader
# Usage: curl -fsSL https://raw.githubusercontent.com/matroidbe/pgbrew/main/install.sh | bash
# Run again to upgrade to the latest version.

REPO="github.com/matroidbe/pgbrew"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CLONE_DIR=$(mktemp -d)

cleanup() {
    rm -rf "$CLONE_DIR"
}
trap cleanup EXIT

echo "Installing pgbrew..."

# Check for Go
if ! command -v go &> /dev/null; then
    echo "Error: Go is required but not installed."
    echo "Install Go from https://go.dev/dl/"
    exit 1
fi

# Check for Git
if ! command -v git &> /dev/null; then
    echo "Error: Git is required but not installed."
    exit 1
fi

# Clone repository
echo "Cloning $REPO..."
git clone --depth 1 "https://$REPO.git" "$CLONE_DIR" 2>/dev/null

# Build
echo "Building pgx..."
cd "$CLONE_DIR"
go build -o pgx ./cmd/pgx

# Install
echo "Installing to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
mv pgx "$INSTALL_DIR/"

# Check PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo "Add $INSTALL_DIR to your PATH:"
    echo ""
    if [[ -f "$HOME/.zshrc" ]]; then
        echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
    else
        echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
    fi
    echo ""
fi

echo "✓ pgbrew installed successfully!"
echo ""
echo "Run 'pgx doctor' to verify your setup."
