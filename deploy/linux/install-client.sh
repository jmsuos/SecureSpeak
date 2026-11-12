#!/bin/sh
# Installs the SecureSpeak client for the current user (no root needed).
set -e
cd "$(dirname "$0")"
BIN=securespeak
[ "$(uname -m)" = "aarch64" ] && [ -f securespeak-arm64 ] && BIN=securespeak-arm64
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/scalable/apps"
install -m 0755 "$BIN" "$HOME/.local/bin/securespeak"
install -m 0644 securespeak.svg "$HOME/.local/share/icons/hicolor/scalable/apps/securespeak.svg"
sed "s|^Exec=.*|Exec=$HOME/.local/bin/securespeak|" securespeak.desktop > "$HOME/.local/share/applications/securespeak.desktop"
command -v update-desktop-database >/dev/null && update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
echo "Installed. Start SecureSpeak from your application menu."
if ! command -v google-chrome >/dev/null && ! command -v chromium >/dev/null && ! command -v chromium-browser >/dev/null && ! command -v microsoft-edge >/dev/null; then
  echo "Tip: SecureSpeak looks best in Chrome/Chromium/Edge (app window). Firefox works too."
fi
if ! id -nG | grep -qw input; then
  echo "Optional: for push-to-talk that works everywhere (also on Wayland): sudo usermod -aG input \$USER, then log out and in."
fi
