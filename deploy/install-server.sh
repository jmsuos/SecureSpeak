#!/bin/sh
# Installs the SecureSpeak server as a systemd service. Run as root from the
# folder that contains securespeak-server (or securespeak-server-arm64).
set -e

BIN=securespeak-server
case "$(uname -m)" in
  aarch64|arm64) [ -f securespeak-server-arm64 ] && BIN=securespeak-server-arm64 ;;
esac

install -m 0755 "$BIN" /usr/local/bin/securespeak-server
id securespeak >/dev/null 2>&1 || useradd --system --home /var/lib/securespeak --shell /usr/sbin/nologin securespeak
install -d -o securespeak -g securespeak -m 0700 /var/lib/securespeak

# First run creates config, certificate, default roles and a one-time owner key.
runuser -u securespeak -- /usr/local/bin/securespeak-server -data /var/lib/securespeak -fingerprint >/dev/null

install -m 0644 securespeak.service /etc/systemd/system/securespeak.service
systemctl daemon-reload
systemctl enable --now securespeak

# open the firewall if ufw or firewalld is in use
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q active; then
  ufw allow 9987/tcp && ufw allow 9987/udp
elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port=9987/tcp --add-port=9987/udp && firewall-cmd --reload
fi

echo
echo "Certificate fingerprint (users compare this on first connect):"
runuser -u securespeak -- /usr/local/bin/securespeak-server -data /var/lib/securespeak -fingerprint
echo
TOKEN=$(runuser -u securespeak -- /usr/local/bin/securespeak-server -data /var/lib/securespeak -owner-token)
case "$TOKEN" in
  SS-*) echo "One-time OWNER key (redeem it in the client: Admin -> Use privilege key):"
        echo
        echo "    $TOKEN"
        echo ;;
esac
echo "SecureSpeak is running. Logs: journalctl -u securespeak -f"
