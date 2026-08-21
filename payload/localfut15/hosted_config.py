"""Write the hosted-mode settings used by server.py (mode / server_host / public_host).

Settings are stored in hosted.json beside the Local FUT save so a payload
re-install does not forget them.

Usage:
    hosted_config.py show
    hosted_config.py local
    hosted_config.py client <server-host> [player-name]
    hosted_config.py server [public-host]
"""
from __future__ import annotations

import json
import os
import secrets
import sys
from pathlib import Path


def runtime_root() -> Path:
    override = os.environ.get("FUT15_RUNTIME_ROOT")
    if override:
        return Path(override).expanduser()
    local = os.environ.get("LOCALAPPDATA")
    if local:
        return Path(local) / "FIFA15LocalFUT"
    if os.name == "nt":
        return Path.home() / "AppData" / "Local" / "FIFA15LocalFUT"
    xdg = os.environ.get("XDG_DATA_HOME") or str(Path.home() / ".local" / "share")
    return Path(xdg) / "fifa15-local-fut"


def main(argv: list[str]) -> int:
    path = runtime_root() / "hosted.json"
    current: dict = {}
    if path.exists():
        try:
            current = json.loads(path.read_text(encoding="utf-8")) or {}
        except Exception:
            current = {}

    cmd = (argv[1] if len(argv) > 1 else "show").lower()
    if cmd == "show":
        print(f"hosted.json: {path}")
        shown = dict(current or {"mode": "local"})
        if shown.get("player_secret"):
            shown["player_secret"] = "(saved)"
        print(json.dumps(shown, indent=2))
        return 0

    if cmd == "local":
        current["mode"] = "local"
        current["server_host"] = ""
    elif cmd == "client":
        host = (argv[2] if len(argv) > 2 else "").strip()
        if not host:
            print("ERROR: client mode needs a server address.")
            return 2
        current["mode"] = "client"
        current["server_host"] = host
        name = " ".join((argv[3] if len(argv) > 3 else "").split())
        if name:
            current["player_name"] = name[:20]
        if not current.get("player_name"):
            default = os.environ.get("USERNAME") or os.environ.get("USER") or "Player"
            current["player_name"] = "".join(ch for ch in default if ch.isalnum() or ch in " _.-")[:20] or "Player"
        # The secret proves ownership of the player name on the server. Keep it.
        if len(str(current.get("player_secret") or "")) < 8:
            current["player_secret"] = secrets.token_hex(16)
    elif cmd == "server":
        current["mode"] = "server"
        if len(argv) > 2 and argv[2].strip():
            current["public_host"] = argv[2].strip()
    else:
        print(__doc__)
        return 2

    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(current, indent=2), encoding="utf-8")
    shown = dict(current)
    if shown.get("player_secret"):
        shown["player_secret"] = "(saved)"
    print(f"Saved {path}:")
    print(json.dumps(shown, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
