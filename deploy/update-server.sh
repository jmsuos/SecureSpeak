#!/bin/sh
# Updates the SecureSpeak server to a release (default: the latest one).
# Installed as /usr/local/sbin/securespeak-update by install-server.sh.
# Run as root:  securespeak-update [VERSION]
#
# Safe to run automatically (the GitHub release pipeline does, via AWS
# Systems Manager): it only installs a release whose SHA256SUMS carries a
# valid signature by the SecureSpeak release key below, verifies every
# file against SHA256SUMS, never downgrades, and does nothing when the
# version is already installed.
set -eu

REPO=${SS_REPO:-jmsuos/SecureSpeak}
BIN_PATH=${SS_BIN_PATH:-/usr/local/bin/securespeak-server}
VERSION_FILE=${SS_VERSION_FILE:-/var/lib/securespeak/.installed-version}
RESTART=${SS_RESTART:-systemctl restart securespeak}

# The release signing key (same as internal/update/signing-keys.pem).
PUBKEY='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAPjE7Zxb/3dfdnv78Va10CnvPhpIZmIgcpm4wyHPvF84=
-----END PUBLIC KEY-----'

say() { echo "securespeak-update: $*"; }
die() { echo "securespeak-update: $*" >&2; exit 1; }

for tool in curl openssl sha256sum tar base64 sort; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is not installed"
done

# Which version?
VER=${1:-}
if [ -z "$VER" ]; then
  VER=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/v##')
fi
VER=${VER#v}
case "$VER" in
  *[!0-9.]* | "" | .* | *. | *..*) die "invalid version '$VER'" ;;
esac

CUR=$(cat "$VERSION_FILE" 2>/dev/null || echo 0)
if [ "$CUR" = "$VER" ]; then
  say "already up to date ($CUR)"
  exit 0
fi
if [ "$(printf '%s\n%s\n' "$CUR" "$VER" | sort -V | tail -n1)" = "$CUR" ]; then
  say "installed version $CUR is newer than $VER; not downgrading"
  exit 0
fi

BIN=securespeak-server
case "$(uname -m)" in aarch64|arm64) BIN=securespeak-server-arm64 ;; esac

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
URL="https://github.com/$REPO/releases/download/v$VER"
TARBALL="securespeak-server-$VER-linux.tar.gz"
say "downloading $VER"
for f in "$TARBALL" SHA256SUMS SHA256SUMS.sig; do
  curl -fsSL --retry 3 -o "$f" "$URL/$f" || die "cannot download $f"
done

printf '%s\n' "$PUBKEY" > key.pub
base64 -d SHA256SUMS.sig > sig.bin 2>/dev/null || die "SHA256SUMS.sig is malformed"
openssl pkeyutl -verify -pubin -inkey key.pub -rawin -in SHA256SUMS -sigfile sig.bin >/dev/null 2>&1 \
  || die "signature check FAILED; not installing $VER"
grep -q "  $TARBALL\$" SHA256SUMS || die "$TARBALL is not listed in SHA256SUMS"
sha256sum -c --ignore-missing --quiet SHA256SUMS || die "checksum mismatch; not installing $VER"
say "signature and checksums OK"

mkdir extract
tar xzf "$TARBALL" -C extract
[ -f "extract/$BIN" ] || die "$BIN missing from $TARBALL"
install -m 0755 "extract/$BIN" "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"
# keep this script current too
if [ -f extract/update-server.sh ] && [ -z "${SS_BIN_PATH:-}" ]; then
  install -m 0755 extract/update-server.sh /usr/local/sbin/securespeak-update
fi
$RESTART
mkdir -p "$(dirname "$VERSION_FILE")"
echo "$VER" > "$VERSION_FILE"
say "updated from $CUR to $VER"
