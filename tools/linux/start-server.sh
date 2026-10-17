#!/usr/bin/env bash
# Start, stop or check the hosted FUT 15 server for play over the public internet.
#
#   tools/linux/start-server.sh            # foreground (e.g. inside tmux); Ctrl+C stops it
#   tools/linux/start-server.sh --detach   # background; output goes to $RT/server-console.log
#   tools/linux/start-server.sh --stop     # stop a running server (foreground or background)
#   tools/linux/start-server.sh --status   # show whether it is running
#
# To run it as a systemd service instead (starts on boot, restarts after a crash):
#   sudo tools/linux/install-service.sh    (the service runs this script in the foreground)
#
# Environment (all optional):
#   PUBLIC_HOST   address players connect to (default: fut15.fhun.site)
#   CODE_FILE     file holding the access code (default: ~/.fut15-access-code; created if missing)
#   FUT15_RUNTIME_ROOT  server state and logs (default: ~/.local/share/fifa15-local-fut-server)
#
# The match relay stays on (server-mode default), so players behind any router can play.
# Firewall ports: docs/HOSTING_VPS.md.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PUBLIC_HOST="${PUBLIC_HOST:-fut15.fhun.site}"
CODE_FILE="${CODE_FILE:-$HOME/.fut15-access-code}"

export FUT15_RUNTIME_ROOT="${FUT15_RUNTIME_ROOT:-$HOME/.local/share/fifa15-local-fut-server}"

RUNNING_RE='^python3 (.*/)?payload/localfut15/server\.py --mode server'
pid="$(pgrep -f "$RUNNING_RE" | head -1 || true)"
as_service=0
if [[ -z "${INVOCATION_ID:-}" ]] && systemctl is-active --quiet fut15-server 2>/dev/null; then as_service=1; fi

case "${1:-}" in
  --status)
    if [[ $as_service == 1 ]]; then echo "The server is running as the systemd service fut15-server (pid ${pid:-?}).";
    elif [[ -n "$pid" ]]; then echo "The server is running (pid $pid)."; else echo "The server is not running."; fi
    exit 0 ;;
  --stop)
    if [[ $as_service == 1 ]]; then echo "The server runs as a systemd service; stop it with: sudo systemctl stop fut15-server"; exit 1; fi
    if [[ -z "$pid" ]]; then echo "The server is not running."; exit 0; fi
    kill "$pid"
    for _ in $(seq 1 10); do kill -0 "$pid" 2>/dev/null || { echo "Server stopped (pid $pid)."; exit 0; }; sleep 1; done
    kill -9 "$pid" 2>/dev/null || true
    echo "Server did not exit within 10 s; force-stopped (pid $pid)."
    exit 0 ;;
  ""|--detach) ;;
  *) echo "usage: $0 [--detach | --stop | --status]"; exit 2 ;;
esac

if [[ $as_service == 1 ]]; then
  echo "The server already runs as the systemd service fut15-server. Use: sudo systemctl restart fut15-server"
  exit 1
fi
if [[ -n "$pid" ]]; then
  echo "The server is already running (pid $pid). Stop it first: $0 --stop"
  exit 1
fi

# The server resolves PUBLIC_HOST once and falls back to 127.0.0.1 if that fails, which would
# send every player to their own PC. Refuse to start instead.
if ! getent ahostsv4 "$PUBLIC_HOST" >/dev/null 2>&1; then
  echo "PUBLIC_HOST '$PUBLIC_HOST' does not resolve to an IPv4 address; not starting." >&2
  echo "Fix the DNS record or set PUBLIC_HOST to the server's IP." >&2
  exit 1
fi

if [[ ! -s "$CODE_FILE" ]]; then
  (umask 077; python3 -c 'import secrets; print("fut15-" + secrets.token_urlsafe(12))' > "$CODE_FILE")
  echo "Created a new access code in $CODE_FILE"
fi
export FUT15_SERVER_ACCESS_CODE="$(cat "$CODE_FILE")"

mkdir -p "$FUT15_RUNTIME_ROOT"
CMD=(python3 "$REPO/payload/localfut15/server.py" --mode server --host 0.0.0.0 --public-host "$PUBLIC_HOST")
echo "Public address: $PUBLIC_HOST   access code: $FUT15_SERVER_ACCESS_CODE"

if [[ "${1:-}" == "--detach" ]]; then
  setsid nohup "${CMD[@]}" > "$FUT15_RUNTIME_ROOT/server-console.log" 2>&1 < /dev/null &
  echo "Started in the background (pid $!). Log: $FUT15_RUNTIME_ROOT/logs/localfut15-server.log"
else
  exec "${CMD[@]}"
fi
