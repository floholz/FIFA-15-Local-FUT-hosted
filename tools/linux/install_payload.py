#!/usr/bin/env python3
"""Install the Local FUT payload into a FIFA 15 folder on Linux/macOS.

Mirrors what PLAY_LOCAL_FUT15.cmd does on Windows:

  * backs up the game files it replaces (once) into <runtime root>/install-backup/
  * copies payload/* into the game folder
  * drops CardsDLLzf.dll into dlc/dlc_CardsDLL/dlc/ when that folder exists

It also resolves git-lfs pointer files (cards0.big, data_patch.big) by downloading the
real objects from GitHub's LFS endpoint, so git-lfs does not need to be installed.

    python3 tools/linux/install_payload.py --game "$HOME/Games/EA_FIFA/FIFA 15"
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import sys
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
PAYLOAD = REPO / "payload"
LFS_REPOS = ("floholz/FIFA-15-Local-FUT-hosted", "KyroGeorge2/FIFA-15-Local-FUT")
BACKED_UP = ("dinput8.dll", "CardsDLLzf.dll", "ItsAMe_Origin.dll", "EA-MITM.ini", "cl.ini",
             "data_patch.big", "data_patch.bh", "cards0.big", "cards0.bh")


def runtime_root() -> Path:
    override = os.environ.get("FUT15_RUNTIME_ROOT")
    if override:
        return Path(override).expanduser()
    xdg = os.environ.get("XDG_DATA_HOME") or str(Path.home() / ".local" / "share")
    return Path(xdg) / "fifa15-local-fut"


def lfs_pointer(path: Path) -> tuple[str, int] | None:
    """Return (oid, size) if the file is a git-lfs pointer instead of real content."""
    try:
        if path.stat().st_size > 1024:
            return None
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return None
    if not text.startswith("version https://git-lfs.github.com/spec/"):
        return None
    oid = size = None
    for line in text.splitlines():
        if line.startswith("oid sha256:"):
            oid = line.split(":", 1)[1].strip()
        elif line.startswith("size "):
            size = int(line.split(" ", 1)[1].strip())
    return (oid, size) if oid and size else None


def fetch_lfs(oid: str, size: int, name: str, cache: Path) -> Path:
    """Download one LFS object (verified by sha256) into the cache and return its path."""
    target = cache / oid
    if target.exists() and target.stat().st_size == size:
        return target
    cache.mkdir(parents=True, exist_ok=True)
    body = json.dumps({"operation": "download", "transfers": ["basic"],
                       "objects": [{"oid": oid, "size": size}]}).encode()
    errors: list[str] = []
    for repo in LFS_REPOS:
        req = urllib.request.Request(
            f"https://github.com/{repo}.git/info/lfs/objects/batch", data=body, method="POST",
            headers={"Accept": "application/vnd.git-lfs+json", "Content-Type": "application/vnd.git-lfs+json"},
        )
        try:
            reply = json.loads(urllib.request.urlopen(req, timeout=30).read())
            entry = reply["objects"][0]
            action = entry["actions"]["download"]
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{repo}: {exc}")
            continue
        print(f"  downloading {name} ({size / 1e6:.0f} MB) from {repo} ...", flush=True)
        dl = urllib.request.Request(action["href"], headers=action.get("header", {}))
        digest = hashlib.sha256()
        tmp = target.with_suffix(".part")
        with urllib.request.urlopen(dl, timeout=900) as resp, tmp.open("wb") as out:
            while True:
                chunk = resp.read(1 << 20)
                if not chunk:
                    break
                digest.update(chunk)
                out.write(chunk)
        if digest.hexdigest() != oid:
            tmp.unlink(missing_ok=True)
            errors.append(f"{repo}: sha256 mismatch")
            continue
        tmp.rename(target)
        return target
    raise SystemExit(f"ERROR: could not fetch LFS object for {name}:\n  " + "\n  ".join(errors))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--game", required=True, help="folder containing fifa15.exe")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    game = Path(args.game).expanduser().resolve()
    if not (game / "fifa15.exe").exists():
        print(f"ERROR: fifa15.exe not found in {game}")
        return 2
    if not (PAYLOAD / "localfut15" / "server.py").exists():
        print(f"ERROR: payload folder incomplete at {PAYLOAD}")
        return 2

    rt = runtime_root()
    backup = rt / "install-backup"
    cache = rt / "lfs-cache"
    print(f"Game    : {game}")
    print(f"Payload : {PAYLOAD}")
    print(f"Backups : {backup}")

    # 1. back up originals once
    for name in BACKED_UP:
        src, dst = game / name, backup / name
        if src.exists() and not dst.exists():
            print(f"  backup  {name}")
            if not args.dry_run:
                backup.mkdir(parents=True, exist_ok=True)
                shutil.copy2(src, dst)

    # 2. copy payload (resolving LFS pointers)
    for src in sorted(PAYLOAD.rglob("*")):
        rel = src.relative_to(PAYLOAD)
        if "__pycache__" in rel.parts:
            continue
        dst = game / rel
        if src.is_dir():
            if not args.dry_run:
                dst.mkdir(parents=True, exist_ok=True)
            continue
        pointer = lfs_pointer(src)
        if pointer:
            real = fetch_lfs(pointer[0], pointer[1], rel.name, cache)
            print(f"  install {rel}  (from LFS)")
            if not args.dry_run:
                shutil.copyfile(real, dst)
            continue
        print(f"  install {rel}")
        if not args.dry_run:
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dst)

    # 3. Cards DLL inside the DLC folder, when the install has one
    cards_dlc = game / "dlc" / "dlc_CardsDLL" / "dlc" / "CardsDLLzf.dll"
    if cards_dlc.exists():
        dst = backup / "dlc_CardsDLL" / "CardsDLLzf.dll"
        if not dst.exists() and not args.dry_run:
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(cards_dlc, dst)
        print("  install dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll")
        if not args.dry_run:
            shutil.copy2(PAYLOAD / "CardsDLLzf.dll", cards_dlc)

    if not args.dry_run:
        rt.mkdir(parents=True, exist_ok=True)
        (rt / "installed-version.txt").write_text("hosted-dev (linux installer)\n", encoding="utf-8")
        (rt / "game-dir.txt").write_text(str(game) + "\n", encoding="utf-8")
    print("Done." if not args.dry_run else "Dry run - nothing written.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
