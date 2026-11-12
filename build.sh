#!/bin/sh
# Builds everything into ./dist. Requires Go 1.24+ (no cgo, no third-party
# modules), plus dpkg-deb for the .deb packages (optional).
set -e
cd "$(dirname "$0")"
# Usage: ./build.sh [all|linux|windows]   (default: all)
# The Windows installer is built by CI (.github/workflows/build.yml) or on
# Windows with Inno Setup: ISCC installer\securespeak.iss
TARGET=${1:-all}
VERSION=${VERSION:-$(tr -d ' \n' < VERSION)}
LDFLAGS="-s -w -X main.version=$VERSION"
export CGO_ENABLED=0
rm -rf dist && mkdir -p dist/windows dist/linux-client dist/linux-server

if [ "$TARGET" = all ] || [ "$TARGET" = windows ]; then
  # Windows client (portable exe; CI additionally embeds icon + version info)
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS -H windowsgui" -o dist/windows/SecureSpeak.exe ./cmd/securespeak
fi
[ "$TARGET" = windows ] && exit 0

# Linux client
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o dist/linux-client/securespeak ./cmd/securespeak
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o dist/linux-client/securespeak-arm64 ./cmd/securespeak
cp deploy/linux/securespeak.desktop deploy/linux/securespeak.png deploy/linux/install-client.sh LICENSE dist/linux-client/

# Linux server
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/linux-server/securespeak-server ./cmd/securespeak-server
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/linux-server/securespeak-server-arm64 ./cmd/securespeak-server
cp deploy/securespeak.service deploy/install-server.sh LICENSE dist/linux-server/

# .deb packages for Ubuntu / Linux Mint / Debian
if command -v dpkg-deb >/dev/null; then
  for arch in amd64 arm64; do
    root=$(mktemp -d)
    mkdir -p "$root/DEBIAN" "$root/usr/bin" "$root/usr/share/applications" \
      "$root/usr/share/icons/hicolor/512x512/apps" "$root/usr/share/pixmaps" "$root/usr/share/doc/securespeak"
    bin=dist/linux-client/securespeak
    [ $arch = arm64 ] && bin=dist/linux-client/securespeak-arm64
    install -m 0755 "$bin" "$root/usr/bin/securespeak"
    install -m 0644 deploy/linux/securespeak.desktop "$root/usr/share/applications/"
    install -m 0644 deploy/linux/securespeak.png "$root/usr/share/icons/hicolor/512x512/apps/"
    install -m 0644 deploy/linux/securespeak.png "$root/usr/share/pixmaps/"
    install -m 0644 README.md "$root/usr/share/doc/securespeak/"
    install -m 0644 LICENSE "$root/usr/share/doc/securespeak/copyright"
    cat > "$root/DEBIAN/control" <<EOF
Package: securespeak
Version: $VERSION
Section: net
Priority: optional
Architecture: $arch
Maintainer: SecureSpeak <noreply@localhost>
Homepage: https://github.com/jmsuos/SecureSpeak
Recommends: chromium | chromium-browser | google-chrome-stable | microsoft-edge-stable | firefox, xdg-utils
Description: End-to-end encrypted voice and text chat (client)
 TeamSpeak-style voice chat with Opus audio, noise suppression, whisper,
 file transfer and avatars. Everything is end-to-end encrypted with
 AES-256-GCM; the server only relays ciphertext. No accounts.
EOF
    chmod 0755 "$root" "$root/DEBIAN"
    dpkg-deb --root-owner-group --build "$root" "dist/linux-client/securespeak_${VERSION}_${arch}.deb" >/dev/null
    rm -rf "$root"
  done
fi

cp README.md dist/
(cd dist && find . -type f ! -name SHA256SUMS ! -name '*.md' | sort | xargs sha256sum > SHA256SUMS)
find dist -type f | sort | xargs ls -lh | awk '{print $5, $9}'
