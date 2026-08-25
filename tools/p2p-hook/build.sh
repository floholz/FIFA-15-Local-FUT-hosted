#!/usr/bin/env bash
# Build the Phase 0 P2P observation hook (dinput8.dll) with mingw-w64.
#
#   sudo pacman -S --needed mingw-w64-gcc cmake ninja   # one-time (CachyOS/Arch)
#   tools/p2p-hook/build.sh
#
# Output: tools/p2p-hook/build/dinput8.dll
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

command -v x86_64-w64-mingw32-gcc >/dev/null || {
  echo "mingw-w64 not found. Install it:  sudo pacman -S --needed mingw-w64-gcc cmake ninja"; exit 1; }
command -v cmake >/dev/null || { echo "cmake not found:  sudo pacman -S --needed cmake ninja"; exit 1; }

GEN="Unix Makefiles"; command -v ninja >/dev/null && GEN="Ninja"

cmake -S "$HERE" -B "$HERE/build" -G "$GEN" \
  -DCMAKE_TOOLCHAIN_FILE="$HERE/toolchain-mingw64.cmake" \
  -DCMAKE_BUILD_TYPE=Release
cmake --build "$HERE/build" --config Release

echo
echo "Built: $HERE/build/dinput8.dll"
echo "Install: see tools/p2p-hook/README.md (rename the existing EA-MITM dinput8.dll to ea-mitm.dll first)."
