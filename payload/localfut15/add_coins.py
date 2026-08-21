from __future__ import annotations
import json
import os
import sqlite3
import sys
from pathlib import Path


def runtime_root() -> Path | None:
    override = os.environ.get("FUT15_RUNTIME_ROOT")
    if override:
        return Path(override).expanduser()
    local = os.environ.get("LOCALAPPDATA")
    if local:
        return Path(local) / "FIFA15LocalFUT"
    if os.name == "nt":
        return None
    xdg = os.environ.get("XDG_DATA_HOME") or str(Path.home() / ".local" / "share")
    return Path(xdg) / "fifa15-local-fut"


def main() -> int:
    args = list(sys.argv[1:])
    user_name = ""
    if "--user" in args:
        idx = args.index("--user")
        if idx + 1 >= len(args):
            print("Usage: add_coins.py <amount> [--user <player name>]")
            return 2
        user_name = args[idx + 1]
        del args[idx:idx + 2]
    if len(args) != 1:
        print("Usage: add_coins.py <amount> [--user <player name>]")
        print("  --user   hosted server only: credit a registered player's club instead of the local club")
        return 2
    try:
        amount = int(args[0].replace(",", "").strip())
    except ValueError:
        print("ERROR: Coin amount must be a whole number.")
        return 2
    if amount <= 0:
        print("ERROR: Coin amount must be greater than zero.")
        return 2
    if amount > 100_000_000:
        print("ERROR: For safety, add at most 100,000,000 coins at a time.")
        return 2

    root = runtime_root()
    if root is None:
        print("ERROR: LOCALAPPDATA is unavailable.")
        return 3
    root.mkdir(parents=True, exist_ok=True)
    db = root / "fut15-local.sqlite3"
    if user_name:
        users_db = root / "users.sqlite3"
        if not users_db.exists():
            print(f"ERROR: No hosted user registry at {users_db}.")
            return 3
        ucon = sqlite3.connect(users_db)
        try:
            row = ucon.execute("SELECT id, name FROM users WHERE name=? COLLATE NOCASE", (user_name,)).fetchone()
        finally:
            ucon.close()
        if not row:
            print(f"ERROR: No registered player named '{user_name}'.")
            return 3
        db = root / "users" / f"user-{int(row[0])}.sqlite3"
        print(f"Crediting hosted player '{row[1]}' (user {row[0]}).")

    con = sqlite3.connect(db)
    try:
        con.execute("CREATE TABLE IF NOT EXISTS kv(key TEXT PRIMARY KEY, value TEXT NOT NULL)")
        row = con.execute("SELECT value FROM kv WHERE key='credits'").fetchone()
        current = int(json.loads(row[0])) if row else 0
        new_total = current + amount
        con.execute(
            "INSERT INTO kv(key,value) VALUES('credits',?) "
            "ON CONFLICT(key) DO UPDATE SET value=excluded.value",
            (json.dumps(new_total),),
        )
        con.commit()
    finally:
        con.close()
    print(f"Coins added: {amount:,}")
    print(f"New balance: {new_total:,}")
    print("If FIFA/FUT is open, leave FUT and re-enter (or restart Local FUT) to refresh the balance.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
