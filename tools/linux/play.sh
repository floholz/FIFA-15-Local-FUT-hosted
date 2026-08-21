#!/usr/bin/env bash
# Start the Local FUT services and launch FIFA 15 through Proton (umu-launcher).
#
#   tools/linux/play.sh [--game DIR] [--no-server] [-- extra umu/game args]
#
# Environment (all optional):
#   GAME_DIR     folder with fifa15.exe (default: the one install_payload.py recorded)
#   WINEPREFIX   Proton prefix (default: $GAME_DIR/../prefix)
#   PROTONPATH   Proton build for umu (default: GE-Proton = latest GE, auto-downloaded)
#   SERVER_ARGS  extra args for server.py, e.g. "--mode client --server-host 100.64.0.5"
#
# The server runs in whatever mode hosted.json says (local unless CONNECT_TO_SERVER
# equivalent: python3 payload/localfut15/hosted_config.py client <host> <name>).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RT="${FUT15_RUNTIME_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/fifa15-local-fut}"
GAME_DIR="${GAME_DIR:-}"
START_SERVER=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --game) GAME_DIR="$2"; shift 2 ;;
    --no-server) START_SERVER=0; shift ;;
    --) shift; break ;;
    *) break ;;
  esac
done
if [[ -z "$GAME_DIR" && -f "$RT/game-dir.txt" ]]; then
  GAME_DIR="$(cat "$RT/game-dir.txt")"
fi
[[ -f "$GAME_DIR/fifa15.exe" ]] || { echo "fifa15.exe not found in '$GAME_DIR' (use --game DIR or run install_payload.py)"; exit 1; }
[[ -f "$GAME_DIR/localfut15/server.py" ]] || { echo "payload not installed in $GAME_DIR - run tools/linux/install_payload.py"; exit 1; }
command -v umu-run >/dev/null || { echo "umu-run not found: sudo pacman -S umu-launcher"; exit 1; }

# Dev convenience: refresh the server code in the game folder from a repo checkout
# before launching, so edits to payload/localfut15 don't get left behind. Set
# SYNC_PAYLOAD_FROM=/path/to/repo (or "auto" to use this script's own repo).
if [[ -n "${SYNC_PAYLOAD_FROM:-}" ]]; then
  src="$SYNC_PAYLOAD_FROM"; [[ "$src" == "auto" ]] && src="$REPO"
  if [[ -d "$src/payload/localfut15" ]]; then
    cp "$src/payload/localfut15"/*.py "$GAME_DIR/localfut15/" && echo "Synced server code from $src"
  else
    echo "SYNC_PAYLOAD_FROM=$src has no payload/localfut15 — skipping sync" >&2
  fi
fi

export WINEPREFIX="${WINEPREFIX:-$(dirname "$GAME_DIR")/prefix}"
export PROTONPATH="${PROTONPATH:-GE-Proton}"
export GAMEID="${GAMEID:-umu-fifa15}"
export STORE="${STORE:-none}"
# The Local FUT hook is a dinput8.dll wrapper: Proton must load the game's copy.
export WINEDLLOVERRIDES="dinput8=n,b${WINEDLLOVERRIDES:+;$WINEDLLOVERRIDES}"
mkdir -p "$RT/logs"

SERVER_PID=""
cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "Stopping Local FUT server (pid $SERVER_PID)"
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

if [[ "$START_SERVER" == 1 ]]; then
  rm -f "$RT/runtime_ports.json"
  echo "Starting Local FUT server: python3 $GAME_DIR/localfut15/server.py ${SERVER_ARGS:-}"
  # shellcheck disable=SC2086
  python3 "$GAME_DIR/localfut15/server.py" ${SERVER_ARGS:-} > "$RT/logs/server-console.log" 2>&1 &
  SERVER_PID=$!
  echo "Waiting for the FUT service..."
  for _ in $(seq 1 60); do
    if [[ -f "$RT/runtime_ports.json" ]]; then
      host="$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(d.get("host","127.0.0.1"))' "$RT/runtime_ports.json")"
      port="$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(d["fut_port"])' "$RT/runtime_ports.json")"
      if python3 -c 'import socket,sys;socket.create_connection((sys.argv[1],int(sys.argv[2])),timeout=0.5).close()' "$host" "$port" 2>/dev/null; then
        echo "Local FUT ready at $host:$port (mode: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("mode"))' "$RT/runtime_ports.json"))"
        break
      fi
    fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      echo "Server exited early:"; tail -20 "$RT/logs/server-console.log"; exit 2
    fi
    sleep 1
  done
  [[ -f "$RT/runtime_ports.json" ]] || { echo "Server never became ready:"; tail -20 "$RT/logs/server-console.log"; exit 2; }
fi

echo "Launching FIFA 15 via umu-run (prefix: $WINEPREFIX, proton: $PROTONPATH)"
cd "$GAME_DIR"
umu-run ./fifa15.exe "$@" 2>&1 | tee "$RT/logs/proton-console.log"
