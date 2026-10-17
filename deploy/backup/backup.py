#!/usr/bin/env python3
"""OUFT backups: consistent copies of the game server's and the accounts service's databases.

Runs as the compose service "backup" (daily) or once:

    docker compose run --rm backup --once

Each run writes /backups/ouft-YYYYMMDD-HHMMSS.tar.gz with
    fut15/users.sqlite3, fut15/users/user-*.sqlite3, fut15/fut15-local.sqlite3   (clubs, identities)
    accounts/data.db, accounts/storage/...                                       (users, invites)
    manifest.json                                                                (sizes, integrity checks)
and keeps the newest BACKUP_KEEP archives. SQLite files are copied with SQLite's online
backup API, which is safe while the servers write; logs are left out.

Environment (set in .env, passed by docker-compose.yml): BACKUP_HOUR (0-23, UTC, default 3),
BACKUP_KEEP (archives to keep, default 14), BACKUP_ON_START (1 = also back up when the service
starts), BACKUP_UID / BACKUP_GID (owner of the archives, default 1000).

The live databases are opened read-only, so the backup never creates files in the servers' data.
"""
from __future__ import annotations

import datetime as dt
import json
import os
import shutil
import sqlite3
import sys
import tarfile
import tempfile
import time
from pathlib import Path

SRC_FUT15 = Path(os.environ.get("BACKUP_SRC_FUT15", "/src/fut15"))
SRC_ACCOUNTS = Path(os.environ.get("BACKUP_SRC_ACCOUNTS", "/src/accounts"))
OUT = Path(os.environ.get("BACKUP_OUT", "/backups"))


def log(msg: str) -> None:
    print(f"{dt.datetime.now(dt.timezone.utc):%Y-%m-%d %H:%M:%S}Z backup: {msg}", flush=True)


def copy_sqlite(src: Path, dst: Path) -> dict:
    """Online-backup one SQLite database and check the copy."""
    dst.parent.mkdir(parents=True, exist_ok=True)
    source = sqlite3.connect(f"file:{src}?mode=ro", uri=True, timeout=30)
    try:
        target = sqlite3.connect(dst)
        try:
            source.backup(target)
            check = target.execute("PRAGMA integrity_check").fetchone()[0]
            target.execute("PRAGMA journal_mode=DELETE")  # a single self-contained file
        finally:
            target.close()
    finally:
        source.close()
    return {"file": str(dst), "bytes": dst.stat().st_size, "integrity": check}


def collect(stage: Path) -> list[dict]:
    items: list[dict] = []
    # Game server: player registry + one club per player (+ the legacy single-player save).
    dbs = [SRC_FUT15 / "users.sqlite3", SRC_FUT15 / "fut15-local.sqlite3", *sorted((SRC_FUT15 / "users").glob("user-*.sqlite3"))]
    for db in dbs:
        if db.is_file():
            items.append(copy_sqlite(db, stage / "fut15" / db.relative_to(SRC_FUT15)))
    # Accounts: the main database (logs live in auxiliary.db and are skipped) + uploaded files.
    if (SRC_ACCOUNTS / "data.db").is_file():
        items.append(copy_sqlite(SRC_ACCOUNTS / "data.db", stage / "accounts" / "data.db"))
    storage = SRC_ACCOUNTS / "storage"
    if storage.is_dir():
        shutil.copytree(storage, stage / "accounts" / "storage")
        items.append({"file": str(stage / "accounts" / "storage"), "bytes": sum(
            f.stat().st_size for f in (stage / "accounts" / "storage").rglob("*") if f.is_file())})
    return items


def run_once(keep: int, uid: int, gid: int) -> Path:
    OUT.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%d-%H%M%S")
    with tempfile.TemporaryDirectory(dir=OUT, prefix=".tmp-") as tmp:
        stage = Path(tmp) / f"ouft-{stamp}"
        stage.mkdir()
        items = collect(stage)
        if not items:
            raise RuntimeError(f"nothing to back up (are {SRC_FUT15} and {SRC_ACCOUNTS} mounted?)")
        bad = [i for i in items if i.get("integrity") not in (None, "ok")]
        for i in items:
            i["file"] = str(Path(i["file"]).relative_to(stage))
        (stage / "manifest.json").write_text(json.dumps({"created": stamp, "items": items}, indent=2))
        archive = OUT / f"ouft-{stamp}.tar.gz"
        part = archive.with_suffix(".gz.part")
        with tarfile.open(part, "w:gz") as tar:
            tar.add(stage, arcname=stage.name)
        part.rename(archive)
    try:
        os.chown(archive, uid, gid)
    except PermissionError:
        pass
    archives = sorted(OUT.glob("ouft-*.tar.gz"))
    for old in archives[:-keep] if keep > 0 else []:
        old.unlink()
    log(f"wrote {archive.name} ({archive.stat().st_size / 1e6:.1f} MB, {len(items)} items); keeping {min(len(archives), keep)}")
    if bad:
        raise RuntimeError(f"integrity check failed for: {', '.join(i['file'] for i in bad)}")
    return archive


def seconds_until(hour: int) -> float:
    now = dt.datetime.now(dt.timezone.utc)
    nxt = now.replace(hour=hour, minute=0, second=0, microsecond=0)
    if nxt <= now:
        nxt += dt.timedelta(days=1)
    return (nxt - now).total_seconds()


def main(argv: list[str]) -> int:
    keep = int(os.environ.get("BACKUP_KEEP", "14"))
    hour = int(os.environ.get("BACKUP_HOUR", "3")) % 24
    uid, gid = int(os.environ.get("BACKUP_UID", "1000")), int(os.environ.get("BACKUP_GID", "1000"))
    if "--once" in argv:
        run_once(keep, uid, gid)
        return 0
    log(f"daily at {hour:02d}:00 UTC, keeping {keep} archives in {OUT}")
    if os.environ.get("BACKUP_ON_START", "0").strip().lower() in ("1", "true", "yes", "on"):
        try:
            run_once(keep, uid, gid)
        except Exception as exc:
            log(f"FAILED: {exc}")
    while True:
        time.sleep(seconds_until(hour))
        try:
            run_once(keep, uid, gid)
        except Exception as exc:  # keep the service alive; the next day tries again
            log(f"FAILED: {exc}")
        time.sleep(60)  # do not run twice in the same minute


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
