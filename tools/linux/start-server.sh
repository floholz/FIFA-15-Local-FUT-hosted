#!/usr/bin/env bash
# Start the hosted FUT 15 server for play over the public internet.
#
#   tools/linux/start-server.sh            # foreground (e.g. inside tmux); Ctrl+C stops it
#   tools/linux/start-server.sh --detach   # background; output goes to $RT/server-console.log
#
# Environment (all optional):
#   PUBLIC_HOST   address players connect to (default: fut.fhun.site)
#   CODE_FILE     file holding the access code (default: ~/.fut15-access-code; created if missing)
#   FUT15_RUNTIME_ROOT  server state and logs (default: ~/.local/share/fifa15-local-fut-server)
#
# The match relay stays on (server-mode default), so players behind any router can play.
# Firewall ports: docs/HOSTING_VPS.md.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PUBLIC_HOST="${PUBLIC_HOST:-fut.fhun.site}"
CODE_FILE="${CODE_FILE:-$HOME/.fut15-access-code}"
export FUT15_RUNTIME_ROOT="${FUT15_RUNTIME_ROOT:-$HOME/.local/share/fifa15-local-fut-server}"

RUNNING_RE='^python3 (.*/)?payload/localfut15/server\.py --mode server'
if pid="$(pgrep -f "$RUNNING_RE" | head -1)" && [[ -n "$pid" ]]; then
  echo "The server is already running (pid $pid). Stop it first: kill $pid"
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
