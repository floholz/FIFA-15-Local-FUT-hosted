#!/usr/bin/env bash
# Two-machine match test, client side, with in-game ConnApi tracing.
#
#   tools/linux/match_client.sh <server-ip> [player-name] [access-code]
#
# 1. installs the p2p-hook (tools/p2p-hook/build/dinput8.dll) into the game folder,
#    chain-loading the stock Local FUT dinput8.dll (payload/dinput8.dll) as ea-mitm.dll,
# 2. points this client at <server-ip> (hosted_config.py client),
# 3. launches FIFA 15 in client mode with FUT15_TRACE_CONNAPI=1.
# Then in the game: FUT -> Online -> Single Match (search) on both machines.
# Afterwards quit the game; the script restores the stock dinput8.dll and copies the
# logs to $RT/logs/match-<timestamp>/.
#
# The server runs elsewhere, e.g. on the VPS:
#   FUT15_RUNTIME_ROOT=~/.local/share/fifa15-local-fut-server FUT15_RELAY_ENABLED=0 \
#     python3 payload/localfut15/server.py --mode server --host <ip> --public-host <ip>
#
# Extra env is passed through to play.sh (e.g. the NVIDIA PRIME vars on an Optimus laptop).
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RT="${XDG_DATA_HOME:-$HOME/.local/share}/fifa15-local-fut"
GAME_DIR="${GAME_DIR:-$(cat "$RT/game-dir.txt")}"
SERVER="${1:?usage: $0 <server-ip> [player-name] [access-code]}"
HOOK="$REPO/tools/p2p-hook/build/dinput8.dll"
STOCK="$REPO/payload/dinput8.dll"
OUT="$RT/logs/match-$(date +%Y%m%d-%H%M%S)"; mkdir -p "$OUT"
[[ -f "$HOOK" ]] || { echo "build the hook first: tools/p2p-hook/build.sh (or build-zig.sh)"; exit 1; }

python3 "$REPO/payload/localfut15/hosted_config.py" client "$SERVER" "${@:2}"

restore() {
  echo "Restoring game folder..."
  cp "$GAME_DIR/p2p-hook.log" "$OUT/" 2>/dev/null || true
  cp "$STOCK" "$GAME_DIR/dinput8.dll"
  rm -f "$GAME_DIR/ea-mitm.dll" "$GAME_DIR/p2p-hook.log"
  cp "$RT/logs/server-console.log" "$RT/logs/proton-console.log" "$OUT/" 2>/dev/null || true
  echo "Logs: $OUT  (grep TRACE p2p-hook.log)"
}
trap restore EXIT
cp "$STOCK" "$GAME_DIR/ea-mitm.dll"
cp "$HOOK" "$GAME_DIR/dinput8.dll"
rm -f "$GAME_DIR/p2p-hook.log"

FUT15_TRACE_CONNAPI=1 SYNC_PAYLOAD_FROM=auto SERVER_ARGS="--mode client --server-host $SERVER" \
  "$REPO/tools/linux/play.sh"
