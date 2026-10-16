#!/usr/bin/env bash
# Install the hosted FUT 15 server as a systemd service, so it starts on boot and restarts after a crash.
# Run once on the server, from your own account, with sudo:
#
#   sudo tools/linux/install-service.sh               # install, enable and start
#   sudo tools/linux/install-service.sh --uninstall   # stop, disable and remove
#
# The service runs tools/linux/start-server.sh as the user who ran sudo, with the same access code
# (~/.fut15-access-code) and state (~/.local/share/fifa15-local-fut-server) as a manual start.
# Environment (optional): PUBLIC_HOST (default fut.fhun.site).
#
# Afterwards: systemctl status fut15-server / journalctl -u fut15-server -f / systemctl restart fut15-server
set -euo pipefail
UNIT=fut15-server
DEST="/etc/systemd/system/$UNIT.service"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ $EUID -ne 0 ]]; then echo "Run with sudo: sudo $0 ${1:-}"; exit 1; fi

if [[ "${1:-}" == "--uninstall" ]]; then
  systemctl disable --now "$UNIT" 2>/dev/null || true
  rm -f "$DEST"
  systemctl daemon-reload
  echo "Removed $UNIT. State and access code were left in place."
  exit 0
elif [[ -n "${1:-}" ]]; then
  echo "usage: sudo $0 [--uninstall]"; exit 2
fi

SVC_USER="${SUDO_USER:-}"
if [[ -z "$SVC_USER" || "$SVC_USER" == root ]]; then
  echo "Run this through sudo from the account that should own the server (not as root directly)."; exit 1
fi
SVC_GROUP="$(id -gn "$SVC_USER")"
SVC_HOME="$(getent passwd "$SVC_USER" | cut -d: -f6)"
STATE="$SVC_HOME/.local/share/fifa15-local-fut-server"
PUBLIC_HOST="${PUBLIC_HOST:-fut.fhun.site}"

as_user() { sudo -u "$SVC_USER" env HOME="$SVC_HOME" "$@"; }

if as_user "$REPO/tools/linux/start-server.sh" --status | grep -q "is running"; then
  echo "A manually started server is running. Stop it first: tools/linux/start-server.sh --stop"; exit 1
fi

# The service sees the home directory read-only, so create the state dir and access code now.
as_user mkdir -p "$STATE"
if [[ ! -s "$SVC_HOME/.fut15-access-code" ]]; then
  as_user sh -c 'umask 077; python3 -c "import secrets; print(\"fut15-\" + secrets.token_urlsafe(12))" > "$HOME/.fut15-access-code"'
  echo "Created a new access code in $SVC_HOME/.fut15-access-code"
fi

sed -e "s|@USER@|$SVC_USER|g" -e "s|@GROUP@|$SVC_GROUP|g" -e "s|@HOME@|$SVC_HOME|g" \
    -e "s|@REPO@|$REPO|g" -e "s|@STATE@|$STATE|g" -e "s|@PUBLIC_HOST@|$PUBLIC_HOST|g" \
    "$REPO/tools/linux/fut15-server.service" > "$DEST"
chmod 644 "$DEST"
systemctl daemon-reload
systemctl enable --now "$UNIT"
sleep 2
systemctl --no-pager --lines=5 status "$UNIT" || true
echo
echo "Installed $DEST (user $SVC_USER, public host $PUBLIC_HOST)."
echo "Logs: journalctl -u $UNIT -f   or   $STATE/logs/localfut15-server.log"
