#!/bin/bash
# Builds SecureSpeak.app (universal: Apple silicon + Intel), a .dmg to
# install it and a .zip for in-app updates. Runs on macOS (GitHub's
# macos runner): needs Xcode command line tools (clang, lipo, codesign,
# iconutil, sips, hdiutil, ditto).
#   VERSION=3.11.0 VERSION_NUM=3.11.0.0 installer/macos/build-app.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
VERSION=${VERSION:-$(tr -d ' \n' < VERSION)}
VERSION_NUM=${VERSION_NUM:-${VERSION%%[-+]*}}
OUT=dist/macos
APP="$OUT/SecureSpeak.app"
rm -rf "$OUT" && mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# cgo is needed for global push-to-talk (CoreGraphics); build both architectures
export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=11.0
LDFLAGS="-s -w -X main.version=$VERSION"
GOARCH=arm64 CC="clang -arch arm64" go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/securespeak-arm64" ./cmd/securespeak
GOARCH=amd64 CC="clang -arch x86_64" go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/securespeak-amd64" ./cmd/securespeak
lipo -create -output "$APP/Contents/MacOS/SecureSpeak" "$OUT/securespeak-arm64" "$OUT/securespeak-amd64"
rm "$OUT/securespeak-arm64" "$OUT/securespeak-amd64"
lipo -info "$APP/Contents/MacOS/SecureSpeak"

sed -e "s/@VERSION@/$VERSION/" -e "s/@VERSION_NUM@/$VERSION_NUM/" installer/macos/Info.plist > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"
cp LICENSE "$APP/Contents/Resources/LICENSE.txt"

# icon from the shield artwork
ICONSET="$OUT/SecureSpeak.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z $s $s assets/securespeak-icon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) assets/securespeak-icon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/SecureSpeak.icns"
rm -rf "$ICONSET"

# ad-hoc signature (no Apple Developer ID): required for Apple silicon,
# and keeps the bundle intact
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"

# zip for in-app updates (ditto keeps permissions and signatures)
ditto -c -k --keepParent "$APP" "$OUT/SecureSpeak-$VERSION-macos.zip"

# disk image: drag SecureSpeak to Applications
STAGE="$OUT/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "SecureSpeak $VERSION" -srcfolder "$STAGE" -ov -format UDZO "$OUT/SecureSpeak-$VERSION-macos.dmg" >/dev/null
rm -rf "$STAGE"
ls -l "$OUT"
