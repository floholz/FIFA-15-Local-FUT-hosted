#!/usr/bin/env bash
# Build the OUFT Online (Open Ultimate Football Team) player launcher zip: Windows + Linux binaries and the client payload.
#
#   tools/build-launcher.sh [version]      -> dist/OUFT-Online-<version>.zip
#
# The payload is the setup every working online match used (match_client.sh):
#   dinput8.dll   = the p2p hook (tools/p2p-hook/build/dinput8.dll; build it first)
#   ea-mitm.dll   = the EA-MITM hook (payload/dinput8.dll), chain-loaded by the p2p hook
#   + ItsAMe_Origin.dll, CardsDLLzf.dll, cards0.big/.bh, data_patch.big/.bh from payload/
# The .big files are git-lfs objects: they come from the LFS cache that
# tools/linux/install_payload.py fills (or a real checkout with git-lfs).
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(git -C "$REPO" describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="$REPO/dist/OUFT-Online"
LFS_CACHE="${FUT15_RUNTIME_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/fifa15-local-fut}/lfs-cache"
# sha256 of the fifa15.exe builds the hook's fixed addresses fit (the CPY repack).
KNOWN_EXES=(3da97d0a568475e5714e06f4871b814842a705ddc62207c2b9b66b5fc085bffb)

HOOK="$REPO/tools/p2p-hook/build/dinput8.dll"
[[ -f "$HOOK" ]] || { echo "missing $HOOK: build it with tools/p2p-hook/build.sh"; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT/payload"

echo "Building launcher $VERSION"
LDFLAGS="-s -w -X main.version=$VERSION"
( cd "$REPO" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/OUFT-Online.exe" ./cmd/ouft-launcher )
( cd "$REPO" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/ouft-online" ./cmd/ouft-launcher )

# Copy one payload file, resolving git-lfs pointers from the cache.
put() {
  local src="$1" name="$2"
  if head -c 64 "$src" | grep -q '^version https://git-lfs'; then
    local oid; oid="$(sed -n 's/^oid sha256://p' "$src")"
    local real="$LFS_CACHE/$oid"
    [[ -f "$real" ]] || { echo "LFS object for $name not in $LFS_CACHE (run tools/linux/install_payload.py once)"; exit 1; }
    echo "$oid  $real" | sha256sum -c --quiet - || { echo "LFS cache copy of $name is damaged"; exit 1; }
    src="$real"
  fi
  cp "$src" "$OUT/payload/$name"
}
put "$HOOK" dinput8.dll
put "$REPO/payload/dinput8.dll" ea-mitm.dll
for f in ItsAMe_Origin.dll CardsDLLzf.dll cards0.big cards0.bh data_patch.big data_patch.bh; do
  put "$REPO/payload/$f" "$f"
done

python3 - "$OUT/payload" "$VERSION" "${KNOWN_EXES[@]}" <<'EOF'
import hashlib, json, sys
from pathlib import Path
payload, version, exes = Path(sys.argv[1]), sys.argv[2], sys.argv[3:]
def entry(src, dst, **extra):
    data = (payload / src).read_bytes()
    return {"src": src, "dst": dst, "sha256": hashlib.sha256(data).hexdigest(), "size": len(data), **extra}
# dinput8.dll / ea-mitm.dll are ours; the game never ships them (restore may delete them).
files = [entry(n, n, added=True) for n in ("dinput8.dll", "ea-mitm.dll")]
files += [entry(n, n) for n in ("ItsAMe_Origin.dll", "CardsDLLzf.dll",
                                "cards0.big", "cards0.bh", "data_patch.big", "data_patch.bh")]
files.append(entry("CardsDLLzf.dll", "dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll", if_dir_exists=True))
manifest = {"version": version, "known_exes": exes, "files": files,
            "generated": ["cl.ini", "EA-MITM.ini", "p2p-hook.ini"]}
(payload / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
EOF

cat > "$OUT/README.txt" <<EOF
OUFT Online $VERSION
==================

You need: the same game copy as your friends, and this folder (keep the
"payload" folder next to the launcher).

Windows
  1. Double-click OUFT-Online.exe.
     If Windows says "Windows protected your PC": click "More info", then "Run anyway".
  2. Check the game folder (Settings, if it was not found), type your player name and
     the access code you got from the host.
  3. Click Play. The first time it copies ~210 MB of online files into the game
     (it backs up the originals first).
  Keep the black launcher console open while you play.
  If the game folder is in "Program Files" and installing fails, right-click
  OUFT-Online.exe and pick "Run as administrator".

Linux
  Needs umu-launcher (Arch: pacman -S umu-launcher) and ideally a GE-Proton in
  ~/.local/share/Steam/compatibilitytools.d. Run ./ouft-online from a terminal.
  White or black screen? Tick "Use WineD3D" in the Proton section and play again.

In the game: Ultimate Team -> Online -> Single Match. Both players search at
the same time and get matched.

To undo everything: "Online files" -> "Undo" -> "Restore original files".
Logs: %LOCALAPPDATA%\\FIFA15LocalFUT\\logs (Windows), ~/.local/share/fifa15-local-fut/logs (Linux).
EOF

ZIP="$REPO/dist/OUFT-Online-$VERSION.zip"
rm -f "$ZIP"
( cd "$REPO/dist" && bsdtar -a -cf "$ZIP" OUFT-Online )
echo "Built $ZIP ($(du -h "$ZIP" | cut -f1))"
