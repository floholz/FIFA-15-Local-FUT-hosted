#!/usr/bin/env bash
# Build the OUFT Online (Open Ultimate Football Team) player launcher zip: Windows + Linux binaries and the client payload.
#
#   tools/build-launcher.sh [version]      -> dist/OUFT-Online-<version>.zip
#
# Versions are 15.<minor>.<patch>: 15 = the game, minor 0 = beta.
#
# The payload is the setup every working online match used (match_client.sh). Whole files,
# only our own or third-party code:
#   dinput8.dll   = the p2p hook (tools/p2p-hook/build/dinput8.dll; build it first)
#   ea-mitm.dll   = the EA-MITM hook (payload/dinput8.dll, GPL), chain-loaded by the p2p hook
#   fifa15.bmp    = the startup splash, rendered from the OUFT logo
# The modded game files (ItsAMe_Origin.dll, CardsDLLzf.dll, cards0.big/.bh, data_patch.big/.bh
# from payload/) ship as zstd patches against the repack originals, so the zip contains no
# EA or repack file. The originals come from OUFT_ORIGINALS (default: this PC's install
# backup, i.e. the files the first install replaced). The launcher also shortens the boot
# intro movie (manifest "shorten_movies").
# The .big files are git-lfs objects: they come from the LFS cache that
# tools/linux/install_payload.py fills (or a real checkout with git-lfs).
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-15.4.0}"
OUT="$REPO/dist/OUFT-Online"
RT="${FUT15_RUNTIME_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/fifa15-local-fut}"
LFS_CACHE="$RT/lfs-cache"
ORIGINALS="${OUFT_ORIGINALS:-$RT/install-backup}"
PATCHED=(ItsAMe_Origin.dll CardsDLLzf.dll cards0.big cards0.bh data_patch.big data_patch.bh)
# sha256 of the fifa15.exe builds the hook's fixed addresses fit (the CPY repack).
KNOWN_EXES=(3da97d0a568475e5714e06f4871b814842a705ddc62207c2b9b66b5fc085bffb)

HOOK="$REPO/tools/p2p-hook/build/dinput8.dll"
[[ -f "$HOOK" ]] || { echo "missing $HOOK: build it with tools/p2p-hook/build.sh"; exit 1; }

command -v zstd >/dev/null || { echo "need zstd (for the patches)"; exit 1; }
for f in "${PATCHED[@]}"; do
  [[ -f "$ORIGINALS/$f" ]] || { echo "missing repack original $ORIGINALS/$f (set OUFT_ORIGINALS)"; exit 1; }
done

rm -rf "$OUT"
mkdir -p "$OUT/payload"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/modded"

echo "Building launcher $VERSION"
LDFLAGS="-s -w -X main.version=$VERSION"
( cd "$REPO" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/OUFT-Online.exe" ./cmd/ouft-launcher )
( cd "$REPO" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/ouft-online" ./cmd/ouft-launcher )

# Copy one file (to $3, default the payload folder), resolving git-lfs pointers from the cache.
put() {
  local src="$1" name="$2" dest="${3:-$OUT/payload}"
  if head -c 64 "$src" | grep -q '^version https://git-lfs'; then
    local oid; oid="$(sed -n 's/^oid sha256://p' "$src")"
    local real="$LFS_CACHE/$oid"
    [[ -f "$real" ]] || { echo "LFS object for $name not in $LFS_CACHE (run tools/linux/install_payload.py once)"; exit 1; }
    echo "$oid  $real" | sha256sum -c --quiet - || { echo "LFS cache copy of $name is damaged"; exit 1; }
    src="$real"
  fi
  cp "$src" "$dest/$name"
}
put "$HOOK" dinput8.dll
put "$REPO/payload/dinput8.dll" ea-mitm.dll

# Modded game files -> patches against the originals.
for f in "${PATCHED[@]}"; do
  put "$REPO/payload/$f" "$f" "$TMP/modded"
  if cmp -s "$ORIGINALS/$f" "$TMP/modded/$f"; then
    echo "$ORIGINALS/$f is the modded file, not the original (check OUFT_ORIGINALS)"; exit 1
  fi
  zstd -q -19 --long=28 --patch-from="$ORIGINALS/$f" "$TMP/modded/$f" -o "$OUT/payload/$f.zst"
  echo "  patch $f: $(stat -c %s "$OUT/payload/$f.zst") bytes"
done

# Startup splash: the game shows fifa15.bmp (500x500, 24-bit) as a shaped window,
# pure green (#00ff00) is transparent. Render the OUFT crest with a hard edge so
# no half-green pixels show as a fringe.
command -v rsvg-convert >/dev/null && command -v magick >/dev/null \
  || { echo "need rsvg-convert (librsvg) and magick (ImageMagick) for the splash"; exit 1; }
rsvg-convert -w 500 -h 500 "$REPO/cmd/ouft-launcher/web/logo.svg" -o "$TMP/logo.png"
magick "$TMP/logo.png" -background "#111111" -alpha remove -alpha off "$TMP/color.png"
magick "$TMP/logo.png" -alpha extract -threshold 50% "$TMP/mask.png"
magick -size 500x500 xc:"#00ff00" "$TMP/color.png" "$TMP/mask.png" -composite -alpha off -type TrueColor \
  "BMP3:$OUT/payload/fifa15.bmp"

python3 - "$OUT/payload" "$TMP/modded" "$ORIGINALS" "$VERSION" "${KNOWN_EXES[@]}" <<'EOF'
import hashlib, json, sys
from pathlib import Path
payload, modded, originals, version, exes = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3]), sys.argv[4], sys.argv[5:]
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
def entry(src, dst, **extra):
    data = (payload / src).read_bytes()
    return {"src": src, "dst": dst, "sha256": hashlib.sha256(data).hexdigest(), "size": len(data), **extra}
def patched(name, dst=None, **extra):
    target = modded / name
    return {"dst": dst or name, "patch": name + ".zst", "source_sha256": sha(originals / name),
            "sha256": sha(target), "size": target.stat().st_size, **extra}
# dinput8.dll / ea-mitm.dll are ours; the game never ships them (restore may delete them).
files = [entry(n, n, added=True) for n in ("dinput8.dll", "ea-mitm.dll")]
files.append(entry("fifa15.bmp", "fifa15.bmp"))
files += [patched(n) for n in ("ItsAMe_Origin.dll", "CardsDLLzf.dll", "cards0.big", "cards0.bh",
                               "data_patch.big", "data_patch.bh")]
files.append(patched("CardsDLLzf.dll", "dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll", if_dir_exists=True))
# The boot intro gets cut to ~1.5 s on the player's PC (the game waits for it to
# finish, so it cannot simply be removed); restore brings the originals back. A cut
# to 0.1 s left the game stuck on the legal screen now and then (a race in its boot
# sequence, not the French file as first suspected).
manifest = {"version": version, "known_exes": exes, "files": files,
            "generated": ["cl.ini", "EA-MITM.ini", "p2p-hook.ini"],
            "shorten_movies": ["data/movies/bootflowintro_MARKER.vp6", "data/movies/bootflowintro_MARKER_fre.vp6"]}
(payload / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
EOF

cat > "$OUT/README.txt" <<EOF
OUFT Online $VERSION
==================

You need: the same game copy as your friends, this folder (keep the "payload"
folder next to the launcher), and an OUFT account. To create one, open the invite
link you got from an admin (https://ouft.fhun.site/register?invite=...).

Windows
  1. Double-click OUFT-Online.exe.
     If Windows says "Windows protected your PC": click "More info", then "Run anyway".
  2. Log in with your OUFT username and password. Check that the game was found
     (if not: Settings -> game folder).
  3. Click Play. The first time it patches a few game files with the online changes
     (it backs up the originals first). Your club follows your account to any PC.
  Keep the black launcher console open while you play.
  If the game folder is in "Program Files" and installing fails, right-click
  OUFT-Online.exe and pick "Run as administrator".

Linux
  Needs umu-launcher (Arch: pacman -S umu-launcher) and ideally a GE-Proton in
  ~/.local/share/Steam/compatibilitytools.d. Run ./ouft-online from a terminal.
  White or black screen? Settings -> "Use WineD3D instead of DXVK", then play again.

In the game: Ultimate Team -> Online -> Single Match. Both players search at
the same time and get matched.

To undo everything: Settings -> "Restore original files".
Logs: %LOCALAPPDATA%\\FIFA15LocalFUT\\logs (Windows), ~/.local/share/fifa15-local-fut/logs (Linux).
EOF

ZIP="$REPO/dist/OUFT-Online-$VERSION.zip"
rm -f "$ZIP"
( cd "$REPO/dist" && bsdtar -a -cf "$ZIP" OUFT-Online )
echo "Built $ZIP ($(du -h "$ZIP" | cut -f1))"
