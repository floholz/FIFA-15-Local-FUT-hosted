#!/usr/bin/env bash
# One-machine joiner test with in-game ConnApi tracing.
#
#   tools/linux/trace_test.sh [LAN_IP]
#
# 1. installs the p2p-hook (tools/p2p-hook/build/dinput8.dll) into the game folder,
#    chain-loading the existing Local FUT dinput8.dll as ea-mitm.dll (backed up),
# 2. starts a local server in server mode (relay off) with its own runtime root,
# 3. launches FIFA 15 in client mode against it with FUT15_TRACE_CONNAPI=1,
# 4. arms the matchmaking bot, so YOUR client becomes the joiner.
# Then in the game: FUT -> Online -> Single Match (search). Afterwards quit the game;
# the script restores the original dinput8.dll and copies the logs next to it.
#
# Extra env is passed through, e.g. FUT15_CONF_OVERRIDES="Override_ConnApiAdapter_etnd=false".
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RT="${XDG_DATA_HOME:-$HOME/.local/share}/fifa15-local-fut"
GAME_DIR="${GAME_DIR:-$(cat "$RT/game-dir.txt")}"
LAN_IP="${1:-$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<NF;i++) if($i=="src") print $(i+1)}')}"
HOOK="$REPO/tools/p2p-hook/build/dinput8.dll"
CODE="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("access_code",""))' "$RT/hosted.json" 2>/dev/null || true)"
CODE="${CODE:-trace_test}"
OUT="$RT/logs/trace-$(date +%Y%m%d-%H%M%S)"; mkdir -p "$OUT"
[[ -f "$HOOK" ]] || { echo "build the hook first: tools/p2p-hook/build.sh (or build-zig.sh)"; exit 1; }

cp "$GAME_DIR/dinput8.dll" "$OUT/dinput8.original.dll"
restore() {
  echo "Restoring game folder..."
  [[ -n "${SRV:-}" ]] && kill "$SRV" 2>/dev/null || true
  cp "$GAME_DIR/p2p-hook.log" "$OUT/" 2>/dev/null || true
  cp "$OUT/dinput8.original.dll" "$GAME_DIR/dinput8.dll"
  rm -f "$GAME_DIR/ea-mitm.dll" "$GAME_DIR/p2p-hook.log"
  cp "$RT/../fifa15-local-fut-server/logs/localfut15-server.log" "$OUT/" 2>/dev/null || true
  echo "Logs: $OUT  (grep TRACE p2p-hook.log)"
}
trap restore EXIT
cp "$GAME_DIR/dinput8.dll" "$GAME_DIR/ea-mitm.dll"
cp "$HOOK" "$GAME_DIR/dinput8.dll"

FUT15_RUNTIME_ROOT="$RT/../fifa15-local-fut-server" FUT15_RELAY_ENABLED=0 FUT15_SERVER_ACCESS_CODE="$CODE" \
  python3 "$REPO/payload/localfut15/server.py" --mode server --public-host "$LAN_IP" > "$OUT/server-console.log" 2>&1 &
SRV=$!
for _ in $(seq 1 60); do grep -q "All services ready" "$OUT/server-console.log" && break; sleep 1; done
( for _ in $(seq 1 120); do
    curl -sf "http://127.0.0.1:8199/localfut/mmbot?code=$CODE" >/dev/null && { echo "mmbot armed"; break; }; sleep 2
  done ) &
FUT15_TRACE_CONNAPI=1 SYNC_PAYLOAD_FROM=auto SERVER_ARGS="--mode client --server-host $LAN_IP" \
  "$REPO/tools/linux/play.sh"
