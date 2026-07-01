#!/usr/bin/env bash
# Install qoe-server as a systemd service on a Linux VPS.
#
# Usage (run as root on the VPS, with the qoe-server binary in the current dir
# or already at /usr/local/bin/qoe-server):
#
#   SECRET=<long-random> PORT=7700 ./install-server.sh
#
# Idempotent: re-running updates the unit and restarts. Prints the listen port to
# open in your provider's firewall panel too (ufw is handled best-effort here).
set -euo pipefail

PORT="${PORT:-7700}"
SECRET="${SECRET:-}"
BIN_SRC="${BIN_SRC:-./qoe-server}"
BIN_DST="/usr/local/bin/qoe-server"

if [ -z "$SECRET" ]; then
  echo "ERROR: set SECRET to a long random string, e.g. SECRET=\$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')" >&2
  exit 1
fi
if [ "$(id -u)" != "0" ]; then
  echo "ERROR: run as root (sudo)." >&2
  exit 1
fi

# Install the binary if a fresh copy is present next to the script.
if [ -f "$BIN_SRC" ]; then
  install -m 0755 "$BIN_SRC" "$BIN_DST"
  echo "installed $BIN_DST"
elif [ ! -x "$BIN_DST" ]; then
  echo "ERROR: no $BIN_SRC here and no existing $BIN_DST. Copy the binary first." >&2
  exit 1
fi

cat >/etc/systemd/system/qoe-server.service <<UNIT
[Unit]
Description=QoE LLD/L4S test server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=${BIN_DST} -addr :${PORT} -secret ${SECRET}
Restart=always
RestartSec=2
User=nobody
AmbientCapabilities=
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now qoe-server
sleep 1
systemctl --no-pager --full status qoe-server | head -n 12 || true

# Best-effort firewall (also open ${PORT}/udp in your VPS provider's panel).
if command -v ufw >/dev/null 2>&1; then
  ufw allow "${PORT}/udp" || true
fi

echo
echo "qoe-server is running on UDP :${PORT}"
echo "Point the CLI at:  <VPS_PUBLIC_IP>:${PORT}"
echo "Reminder: open ${PORT}/udp in your Hostinger firewall panel if it isn't already."
