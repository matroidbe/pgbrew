#!/bin/bash
set -euo pipefail

# pgbrew installer/upgrader
# Usage: curl -fsSL https://raw.githubusercontent.com/matroidbe/pgbrew/main/install.sh | bash
# Run again to upgrade to the latest version.
#
# Downloads the prebuilt pgx binary for this platform from the GitHub release
# and checks it against the release's sha256 checksums. No Go toolchain needed.
#
# Environment:
#   PGX_VERSION   release tag to install (default: latest), e.g. v0.2.0
#   INSTALL_DIR   where pgx goes (default: ~/.local/bin)

REPO="matroidbe/pgbrew"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${PGX_VERSION:-latest}"
WORK_DIR=$(mktemp -d)

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

die() {
    echo "Error: $*" >&2
    exit 1
}

echo "Installing pgbrew..."

command -v curl &> /dev/null || die "curl is required but not installed."
command -v tar &> /dev/null || die "tar is required but not installed."

# Detect platform
case "$(uname -s)" in
    Darwin) OS=darwin ;;
    Linux) OS=linux ;;
    *) die "unsupported OS $(uname -s); pgx ships for macOS and Linux." ;;
esac
case "$(uname -m)" in
    arm64 | aarch64) ARCH=arm64 ;;
    x86_64 | amd64) ARCH=amd64 ;;
    *) die "unsupported architecture $(uname -m); pgx ships for arm64 and amd64." ;;
esac

# Resolve "latest" through the release redirect (no API call, no rate limit)
if [[ "$VERSION" == "latest" ]]; then
    latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") \
        || die "could not resolve the latest pgbrew release."
    VERSION="${latest_url##*/}"
    [[ "$VERSION" == v* ]] || die "could not resolve the latest pgbrew release (got '$VERSION')."
fi

ASSET="pgx-$VERSION-$OS-$ARCH.tar.gz"
CHECKSUMS="pgx-$VERSION-checksums.txt"
BASE_URL="https://github.com/$REPO/releases/download/$VERSION"

echo "Downloading pgx $VERSION for $OS-$ARCH..."
curl -fsSL -o "$WORK_DIR/$ASSET" "$BASE_URL/$ASSET" \
    || die "no prebuilt pgx $VERSION for $OS-$ARCH at $BASE_URL/$ASSET"
curl -fsSL -o "$WORK_DIR/$CHECKSUMS" "$BASE_URL/$CHECKSUMS" \
    || die "release $VERSION has no $CHECKSUMS; refusing to install an unverified binary."

# Verify checksum
expected=$(awk -v f="$ASSET" '$2 == f || $2 == "*"f { print $1 }' "$WORK_DIR/$CHECKSUMS")
[[ -n "$expected" ]] || die "$ASSET is not listed in $CHECKSUMS."
if command -v sha256sum &> /dev/null; then
    actual=$(sha256sum "$WORK_DIR/$ASSET" | awk '{ print $1 }')
else
    actual=$(shasum -a 256 "$WORK_DIR/$ASSET" | awk '{ print $1 }')
fi
[[ "$actual" == "$expected" ]] || die "checksum mismatch for $ASSET (expected $expected, got $actual)."

# Install
tar -xzf "$WORK_DIR/$ASSET" -C "$WORK_DIR" pgx
echo "Installing to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
# Write next to the target and rename, so a running pgx is never half-written
install -m 0755 "$WORK_DIR/pgx" "$INSTALL_DIR/.pgx.new"
mv -f "$INSTALL_DIR/.pgx.new" "$INSTALL_DIR/pgx"

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

echo "✓ pgx $VERSION installed successfully!"
echo ""
echo "Run 'pgx doctor' to verify your setup."
