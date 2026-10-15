#!/usr/bin/env bash
# Build the P2P hook (dinput8.dll) with Zig instead of mingw-w64 — no sudo needed.
#
#   ZIG=/path/to/zig tools/p2p-hook/build-zig.sh      # or have `zig` on PATH
#
# Zig bundles mingw-w64 headers/libs, so this works on machines without the
# mingw-w64-gcc package. Output: tools/p2p-hook/build/dinput8.dll
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ZIG="${ZIG:-$(command -v zig || true)}"
[[ -x "$ZIG" ]] || { echo "zig not found: download from https://ziglang.org/download/ and set ZIG=/path/to/zig"; exit 1; }
B="$HERE/build"; MH="$B/minhook"
mkdir -p "$B/obj"
[[ -d "$MH" ]] || git clone -q --depth 1 https://github.com/TsudaKageyu/minhook.git "$MH"
for f in "$MH"/src/*.c "$MH"/src/hde/hde64.c; do
  "$ZIG" cc -target x86_64-windows-gnu -O2 -c -I"$MH/include" "$f" -o "$B/obj/$(basename "$f" .c).o"
done
"$ZIG" c++ -target x86_64-windows-gnu -O2 -shared -std=c++17 -I"$MH/include" \
  "$HERE/src/dllmain.cpp" "$B"/obj/*.o -lws2_32 -o "$B/dinput8.dll"
echo "Built: $B/dinput8.dll"
