# FIFA 15 Local FUT — Hosted fork

> **TL;DR** — EA's FIFA 15 Ultimate Team servers are gone. This project reimplements the FUT backend
> (EA **Blaze** protocol) so you can run your own server and keep playing. The local/offline restoration is
> complete and stable, and **online FUT matches between friends work over the public internet** through a
> self-hosted server (verified 2026-10-16: full match, results and coins saved for both players, no VPN
> needed). Players use **OUFT Online** (Open Ultimate Football Team), a one-click launcher for Windows and
> Linux, and log in with an invite-only **OUFT account**. See **[Play online](#play-online-ouft-online)**
> and **[Status](#status--online-matches-work-current-focus)**.

Local/offline FIFA 15 Ultimate Team restoration for PC, based on the working v0.2.39 backend of
[KyroGeorge2/FIFA-15-Local-FUT](https://github.com/KyroGeorge2/FIFA-15-Local-FUT).

This fork adds a **hosted mode**: one person (or a small VPS) runs the FUT backend, friends connect to it
with the OUFT Online launcher. See [Play online](#play-online-ouft-online),
[Hosted mode](#hosted-mode-play-with-friends) and the [roadmap](#hosted-roadmap) below. Everything in the
original local/offline mode keeps working unchanged.

## Status — online matches work (current focus)

The goal: friends, each with their own club on a shared self-hosted server, play real FUT matches against
each other over the internet, with no VPN and no port forwarding on their side.

**Milestone (2026-10-16): a complete online match over the public internet.** Server on a VPS
(`fut.fhun.site`), one player on a home connection, the other behind a VPN. Matchmaking, the opponent's
real squad, a full 90 minutes through the server's UDP relay (~28 ms, no packet loss), both match reports
accepted, and win/loss, coins and season points saved in both clubs. The same flow was verified before that
with direct P2P between two machines.

**What works:**

- Full self-hosted Blaze backend: login, persona, **per-player clubs**, store/packs, transfer market
  (vs AI), offline seasons.
- **Online matches end to end:**
  - matchmaking → game setup → P2P mesh, either direct, or through the server's **UDP match relay**
    when players can't reach each other (the default for public servers);
  - each client loads the **opponent's squad** from the server before kick-off;
  - kick-off → full match → both match reports (GameReporting) → `/match/end` → rewards and season
    progress in each club.
- **OUFT Online launcher** (Go, Windows + Linux): finds the game, checks its version, installs the online
  files with a backup (one-click restore), logs in, runs the Origin stub and starts the game. It also swaps
  the startup splash for the OUFT crest and skips the boot intro.
- **OUFT accounts:** invite-only registration at `ouft.fhun.site`, username + password login from any PC,
  admins hand out invite links from a web page. A player's existing club is linked to their account on the
  first login.
- **Hosting:** one `docker compose up -d` runs the whole stack (HTTPS proxy, accounts, game server); player
  identity survives restarts; the server log keeps its history.

**How we got there (2026-10-15/16):** the decrypted `fifa15.exe`/`CardsDLLzf.dll` showed that the joiner
needed a peer address in `ReplicatedGamePlayer.PNET`. After that the mesh connected, but every match aborted
at kick-off with *"Connection has been lost with your opponent"* although the UDP link was perfect (proven
with packet captures on both ends). The cause was that the server answered
`GET /ut/game/fifa15/squad/active/user/<opponent>` with `{}`, so both games simulated against a placeholder
squad and desynced. Two more fixes followed: the local Origin (LSX) session no longer times out mid-match
(it used to quit in the 52nd minute), and online match reports get their result notification. Details:
[docs/P2P_STATUS.md](docs/P2P_STATUS.md), [docs/RE_FIFA15_EXE.md](docs/RE_FIFA15_EXE.md).

**Current focus:**

1. **First real friend sessions** with OUFT Online 15.1, including the first player on a real Windows PC.
2. **Patches instead of whole files:** since 15.2.0 the launcher ships zstd patches (~38 MB) instead of
   modded EA/repack files, which is the precondition for public releases. See
   [docs/ROADMAP.md](docs/ROADMAP.md), Phase 2.
3. Backups of the server state (`users/`, accounts) once real play starts.

**Known issues:**

- After a match, a *"Leaving Ultimate Team"* screen shows briefly and then returns to UT (cosmetic).
- Sound glitches during matches on one Linux/Proton PC.
- `GET /ut/game/fifa15/user/list` (opponent club info) is still answered with `{}`; no visible effect so far.
- Linux/Proton: on one Optimus laptop with KDE 6.8, DXVK shows a white screen; `PROTON_USE_WINED3D=1`
  works around it. `play.sh` downloads the newest GE-Proton unless `PROTONPATH` is set.
- Every club numbers its items from the same base, so both squads in a match share item IDs. No problem
  observed so far, but worth keeping in mind.

## Play online (OUFT Online)

You need the same FIFA 15 copy as your friends, the OUFT Online zip (shared privately for now), and an
**OUFT account**: open the invite link you got from an admin (`https://ouft.fhun.site/register?invite=…`)
and pick a username and password.

1. Unzip the whole folder anywhere and keep `payload/` next to the launcher.
2. Start **`OUFT-Online.exe`** (Windows; on "Windows protected your PC" click *More info* → *Run anyway*)
   or **`./ouft-online`** (Linux; needs `umu-launcher` and ideally a local GE-Proton).
3. Log in with your OUFT username and password, check that the game was found (otherwise *Settings* →
   game folder), and press **Play**. The first time it patches a few game files with the online changes
   (the zip ships patches, not game files) and backs up the originals; *Settings* → *Restore original
   files* undoes everything.
4. In the game: **Ultimate Team → Online → Single Match**. Both players search at the same time.

Your club lives on the server and follows your account to any PC. Keep the launcher open while you play.
Logs: `%LOCALAPPDATA%\FIFA15LocalFUT\logs` (Windows), `~/.local/share/fifa15-local-fut/logs` (Linux).
Build the zip yourself with `tools/build-launcher.sh` (needs Go, the built p2p hook and the LFS assets).

## Single player (offline) quick start

1. Extract the ZIP to a normal folder (for example, Downloads).
2. Run **`INSTALL_PREREQUISITES.cmd`** once. It checks/installs Python, the required Python package, and the Visual C++ runtime used by the FIFA 15 Cards DLL.
3. Run **`PLAY_LOCAL_FUT15.cmd`**. On first run it finds your FIFA 15 installation, backs up files it replaces, installs the Local FUT payload, creates a desktop shortcut, starts the localhost services, and launches FIFA 15.
4. Future launches can use the **FIFA 15 Local FUT** desktop shortcut.

This requires your own installed copy of FIFA 15 PC. The project is intended for local/offline restoration testing; it does not connect you to EA's retired FUT service.

## Fresh starter club

A brand-new Local FUT save starts intentionally small:

- **0 coins** by default.
- **14 bronze Premier League players** only (including a usable mix of GK/DEF/MID/ST positions).
- One active **Arsenal badge**.
- **Arsenal home + away kits**.
- One starter **stadium** (Sanderson Park).
- One starter **ball** so matches have a complete club identity.
- One starter squad, with additional squads supported.

FUT will still let the player choose/confirm their own club name. Club progress, coins, squads, items and Transfer List state are persisted in:

`%LOCALAPPDATA%\FIFA15LocalFUT\fut15-local.sqlite3`

If you previously used a development build and want to test the exact fresh-public state, run **`RESET_TO_STARTER_CLUB.cmd`**. It only deletes the Local FUT database; it does not delete normal Career/Settings saves.

## Optional test coins

Run **`ADD_COINS.cmd`** and enter how many local coins you want to add. The default is 1,000,000 coins. This modifies only the localhost FUT SQLite balance.

## What is included

- Persistent local FUT club/profile.
- Store and pack opening, including promo packs.
- FIFA 15 player database and special-card pack pools.
- Club consumables.
- Badge/kit/stadium/ball support.
- Transfer List lifecycle, relisting, sold-item clearing and quick sell.
- Large deterministic local AI Transfer Market and local AI buyers for user listings.
- Multiple squads.
- Offline Seasons work from the current development line.
- Port auto-remapping for local FIFA services where possible.

This is a **test release**, so logs are intentionally verbose. They are stored under `%LOCALAPPDATA%\FIFA15LocalFUT\logs` and are useful when reporting bugs.

## Files new testers should care about

- `INSTALL_PREREQUISITES.cmd` — one-time dependency setup.
- `PLAY_LOCAL_FUT15.cmd` — main first-run installer/launcher.
- `ADD_COINS.cmd` — optional local coin helper.
- `RESET_TO_STARTER_CLUB.cmd` — optional destructive Local FUT reset.
- `RESTORE_BACKUP.cmd` — restores game files backed up by the Local FUT installer.
- `CONNECT_TO_SERVER.cmd` — the old way to point this PC at a hosted server (replaced by OUFT Online).
- `START_FUT15_SERVER.cmd` — host the FUT backend for friends on this PC.

Everything inside `payload/` is installed automatically by the main launcher.

## About `ItsAMe_Origin.dll`

The filename is intentionally left unchanged. It is part of the compatibility chain used by this build and its exact filename is embedded in the binary, so renaming it just for presentation could break startup on clean machines.

## Hosted mode (play with friends)

The backend can run in three modes (`mode` in `localfut15/config.json`, or `hosted.json` next to the save):

| mode | what runs on this PC | who it is for |
|---|---|---|
| `local` (default) | everything on `127.0.0.1` — the original offline build | single player |
| `server` | redirector, Blaze, QoS, EASW and FUT services bound to `0.0.0.0`, no game files needed | the host / a VPS |
| `client` | only the Origin LSX stub; `cl.ini`/`EA-MITM.ini` are rewritten to point FIFA at the server | every player |

**Public internet or VPN overlay.** A public server (e.g. a VPS) works for players behind any router: the
server relays match traffic when players can't reach each other. Players log in with OUFT accounts (the old
shared access code still works for launchers before 15.1). The compatibility hook disables TLS, so game
traffic is plaintext; if you'd rather keep it off the open internet, put host and players on a VPN overlay
(Tailscale, ZeroTier…) instead.

### Host the OUFT stack (Linux VPS, Docker)

```sh
cp .env.example .env      # public IP, domains, ACME email, service key, game server state dir
docker compose up -d --build
docker compose exec accounts ouft-accounts superuser upsert <email> <password>
```

One compose file runs everything: its own Traefik (HTTPS for the accounts web app, bound only to the public
IP), the **accounts** service (Go + PocketBase: invite-only registration, login, admin pages) and the
**game server** (`fut15`, host networking for the game ports and the UDP relay). Invite links:
`docker compose exec accounts ouft-accounts invite` or `https://<your domain>/admin` for users with the
admin role. Setup, admin and the move from the older systemd unit: **[docs/ACCOUNTS.md](docs/ACCOUNTS.md)**.
Firewall ports (TCP and UDP, incl. the match relay range): **[docs/HOSTING_VPS.md](docs/HOSTING_VPS.md)**.

### Host on Windows

Run **`START_FUT15_SERVER.cmd`** (from the release folder or the FIFA install) and enter the address the
players should use (your VPN IP). Equivalent: `python localfut15\server.py --mode server --public-host <ip>`.

Only `payload/localfut15/` goes into the game server image — no game files, DLLs or `.big` archives.

### Host on Linux without Docker (game server only, old login)

```sh
tools/linux/start-server.sh --detach   # or without --detach inside tmux
tools/linux/start-server.sh --status   # / --stop
```

Listens on all interfaces, advertises `PUBLIC_HOST` (default `fut15.fhun.site`), keeps the match relay on and
requires the access code from `~/.fut15-access-code` (created on first start). State and logs:
`~/.local/share/fifa15-local-fut-server`.

To keep it running across reboots and crashes without Docker, install it as a systemd service once (run
with sudo from the account that should own the server; the compose setup above replaces this):

```sh
sudo tools/linux/install-service.sh               # install, enable, start (--uninstall removes it)
systemctl status fut15-server                     # / sudo systemctl restart fut15-server
journalctl -u fut15-server -f
```

The service runs `start-server.sh` as that user, with the same access code and state as a manual start.

### Connect as a player (old way, before OUFT Online)

Use [OUFT Online](#play-online-ouft-online). The old script-based way still works with the old login:

1. Install Local FUT as usual (`INSTALL_PREREQUISITES.cmd`, `PLAY_LOCAL_FUT15.cmd` once).
2. Run **`CONNECT_TO_SERVER.cmd`**, enter the host's address, the player name you want on that server
   and the server's access code. Leave the address empty to return to local mode.
   Use an up-to-date download: online matches need player-side fixes from 2026-10-16.
3. Start the game with `PLAY_LOCAL_FUT15.cmd` / the desktop shortcut. The launcher checks that the server
   is reachable before it starts FIFA and refuses to launch if it is not.

The setting is stored in `%LOCALAPPDATA%\FIFA15LocalFUT\hosted.json`, so re-installing the payload does
not forget it. `LOCAL_FUT_STATUS.cmd` shows the active mode and server address.

### Players and clubs on a hosted server

- Every OUFT account gets its own club (fresh starter club, own coins, own transfer list), named after the
  username. The club lives on the **server** in `users/user-<id>.sqlite3` and follows the account to any PC.
- On the first account login, a PC that already played with the old login (`hosted.json` with
  `player_name` + `player_secret`) gets its old club linked to the account.
- Old login (launchers before 15.1, `CONNECT_TO_SERVER.cmd`): the club belongs to the player name, proven by
  the per-PC `player_secret` in `hosted.json`. `FUT15_REQUIRE_ACCOUNT=1` turns it off.
- Host-side tools: `add_coins.py <amount> --user <name>` credits a specific player;
  `GET http://<server>:8199/localfut/status` shows version, mode and player count.
- `ADD_COINS.cmd` on a player's PC only touches that PC's local offline club, never the server.

Server operators can also set `FUT15_RUNTIME_ROOT` to choose where the SQLite save and logs are kept.

### Running on Linux with Proton

Verified working (game boots, FUT loads, offline Seasons play) — see
**[docs/LINUX_PROTON_FINDINGS.md](docs/LINUX_PROTON_FINDINGS.md)**.

```sh
sudo pacman -S umu-launcher                                   # or your distro's package
python3 tools/linux/install_payload.py --game "/path/to/FIFA 15"
tools/linux/play.sh --game "/path/to/FIFA 15"
```

`install_payload.py` downloads the git-lfs `.big` assets itself. `play.sh` starts the Local FUT server
(mode from `hosted.json`) and launches the game through `umu-run` with the `dinput8` hook override.
For online play, the Linux build of OUFT Online does all of this (see [Play online](#play-online-ouft-online)).

### Testing with the game (Windows testers)

See **[docs/TESTER_GUIDE.md](docs/TESTER_GUIDE.md)** — phased checklist and exactly which log lines and
capture files to send back.

### Testing without the game

```sh
python3 tests/run_all.py
```

Starts real server/client processes on the standard FIFA ports (127.0.0.1 only) inside a temp directory and
checks routing, advertised addresses, multi-account isolation, account login, the LSX identity stub and the
TDF decoder. No dependencies beyond Python 3.10+. Name scenarios to run a subset, e.g.
`python3 tests/run_all.py multi`. The Go parts (launcher, accounts): `go test ./...`.

### Hosted roadmap

- [x] **Step 1 — server/client split.** `--mode`, `--public-host`, client-side routing, Docker image.
- [x] **Step 2 — multi-account.** Each player registers a name (+ auto-generated secret) on the server
  and gets their own club database under `users/`. Identity travels LSX → Blaze login → FUT session
  token; unknown clients fall back to their VPN IP. *Not yet:* a shared human transfer market
  (each club still trades against the AI market).
- [x] **Step 3 — matches between friends.** Matchmaking, P2P mesh, opponent squads, full match,
  two-sided GameReporting and `/match/end` with rewards in both clubs (2026-10-16).
- [x] **Step 4 — public internet.** Server-relayed match traffic for players behind their own NATs; verified
  with a home player vs a VPN player (2026-10-16). No VPN needed for players.
- [x] **Step 5 — launcher, accounts, hosting.** OUFT Online launcher (15.0, 2026-10-17), invite-only OUFT
  accounts with linking of existing clubs (15.1), the whole server stack in one compose file, opt-in debug
  dumps.
- [ ] **Now:** first friend sessions (incl. a real Windows PC), patches instead of whole files, backups.
- [ ] **Later:** shared human transfer market, cosmetic fixes (see Known issues), the server in Go.

Details and phases: [docs/ROADMAP.md](docs/ROADMAP.md).

## Bug reports

When reporting a problem, include:

- What screen/action you were on.
- What you expected to happen.
- What actually happened/crashed/froze.
- The newest log from `%LOCALAPPDATA%\FIFA15LocalFUT\logs`.

Please test on a legitimate FIFA 15 PC installation and keep reports focused on the localhost/offline restoration.
