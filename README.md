# FIFA 15 Local FUT — Hosted fork

> **TL;DR** — EA's FIFA 15 Ultimate Team servers are gone. This project reimplements the FUT backend
> (EA **Blaze** protocol) so you can run your own server and keep playing. The original local/offline
> restoration is complete and stable. **The frontier we're working on now is real *online* FUT matches —
> peer-to-peer games between friends — routed through a self-hosted server** (a VPS, no VPN required for
> the players). Everything up to the match transport already works; the one remaining blocker is decoding
> EA's dead NAT-punch ("demangler") response format. See **[Status](#status--real-online-p2p-matches-current-focus)** below.

Local/offline FIFA 15 Ultimate Team restoration for PC, based on the working v0.2.39 backend of
[KyroGeorge2/FIFA-15-Local-FUT](https://github.com/KyroGeorge2/FIFA-15-Local-FUT).

This fork adds a **hosted mode**: one person (or a small VPS) runs the FUT backend, friends point their
FIFA 15 at it. See [Hosted mode](#hosted-mode-play-with-friends) and the [roadmap](#hosted-roadmap) below.
Everything in the original local/offline mode keeps working unchanged.

## Status — real online P2P matches (current focus)

The goal: two friends, each with their own club on a shared self-hosted server, play a real FUT match
against each other over the internet — the game's peer-to-peer UDP routed with the server's help, so it
works behind normal NATs without a VPN. (A private Tailscale link between two dev machines is used **only**
as the test rig; it is not the shipping solution.)

**What already works (the whole stack up to the match transport):**

- Full self-hosted Blaze backend: login, persona, **per-player clubs**, store/packs, transfer market,
  offline seasons — everything in Hosted mode below.
- **Matchmaking and game setup** between two clients: `startMatchmaking` → pair → `NotifyGameSetup`
  (create/join) → `finalizeGameCreation` → pre-game + player-joining, no crashes. This required decoding
  FIFA 15's exact `ReplicatedGamePlayer` TDF layout (it has **no** per-player address field) and delivering
  peer addresses over `UserSessions.UserAdded` — see `tools/tdftags.py` and `docs/P2P_STATUS.md`.
- **The P2P NAT-punch transport.** FIFA resolves the peer's address through DirtySDK's **ProtoMangle
  "demangler"** — a plaintext UDP service on `:10000` that only ever lived on EA servers that are now dead.
  We stand up our own demangler on the server and steer the game's `:10000` traffic to it purely in the
  kernel (`iptables` DNAT + MASQUERADE; conntrack reverses the reply so the game accepts it as coming from
  the original EA IP). Probes reach the server and replies come back, verified end-to-end. See
  `tools/linux/demangler-redirect.sh` and the client hook `tools/p2p-hook/`.

**What we're solving right now — the ProtoMangle *response* format.** The game receives our demangler
reply but rejects its *contents*: it keeps re-probing, its NAT type never resolves, and P2P stays
unaddressed (`0.0.0.0`). We know the *request* format (plaintext `sourceIP/sourcePort/tag/sendCount`) but
our guessed *response* (`targetIP/targetPort/tag`) isn't what DirtySDK expects. EA's ProtoMangle source
isn't public, so the next step is recovering the exact response wire format (candidate leads: same-era
Battlefield Blaze emulators) — or, as a fallback, bypassing the demangler entirely by forcing/relaying the
P2P from inside the client hook (the "Pocket Relay" model). Deep technical notes live in
**[docs/P2P_STATUS.md](docs/P2P_STATUS.md)** and **[docs/P2P_TUNNEL_DESIGN.md](docs/P2P_TUNNEL_DESIGN.md)**.

## Quick start

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
- `CONNECT_TO_SERVER.cmd` — point this PC's FIFA at a friend's hosted server (or back to local).
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

**Use a VPN overlay** (Tailscale, ZeroTier, Radmin, Hamachi…) between the host and the players. The
compatibility hook disables TLS, so traffic is plaintext; an overlay network keeps it off the open
internet and also removes the port-forwarding problem for the later peer-to-peer match work.

### Host on Windows

Run **`START_FUT15_SERVER.cmd`** (from the release folder or the FIFA install) and enter the address the
players should use (your VPN IP). Equivalent: `python localfut15\server.py --mode server --public-host <ip>`.

### Host with Docker (Linux VPS / NAS)

```sh
PUBLIC_HOST=<address players use> SERVER_ACCESS_CODE=<shared code> docker compose up -d --build
```

Friends must send the access code to register (omit it for an open server; add `ALLOWED_PLAYERS="a,b,c"`
to also restrict names). Full walkthrough with VPN gating in **[docs/HOSTING_VPS.md](docs/HOSTING_VPS.md)**.

Only `payload/localfut15/` goes into the image — no game files, DLLs or `.big` archives. State lives in
the `fut15-data` volume (`/data`). Ports 42230, 10051, 17502, 42232, 8199 and 8099 (TCP) must be reachable.

### Connect as a player

1. Install Local FUT as usual (`INSTALL_PREREQUISITES.cmd`, `PLAY_LOCAL_FUT15.cmd` once).
2. Run **`CONNECT_TO_SERVER.cmd`**, enter the host's address and the player name you want on that server.
   Leave the address empty to return to local mode.
3. Start the game with `PLAY_LOCAL_FUT15.cmd` / the desktop shortcut. The launcher checks that the server
   is reachable before it starts FIFA and refuses to launch if it is not.

The setting is stored in `%LOCALAPPDATA%\FIFA15LocalFUT\hosted.json`, so re-installing the payload does
not forget it. `LOCAL_FUT_STATUS.cmd` shows the active mode and server address.

### Players and clubs on a hosted server

- Every player name gets its own club (fresh starter club, own coins, own transfer list). The club lives on
  the **server** in `users/user-<id>.sqlite3`; the player's PC keeps nothing but `hosted.json`.
- `hosted.json` also holds a random `player_secret` that proves you own the name. Copy it to another PC to
  play the same club from there; lose it and the name is locked (the host can delete the row in
  `users.sqlite3`).
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

### Testing with the game (Windows testers)

See **[docs/TESTER_GUIDE.md](docs/TESTER_GUIDE.md)** — phased checklist and exactly which log lines and
capture files to send back.

### Testing without the game

```sh
python3 tests/run_all.py
```

Starts real server/client processes on the standard FIFA ports (127.0.0.1 only) inside a temp directory and
checks routing, advertised addresses, multi-account isolation, the LSX identity stub and the TDF decoder.
No dependencies beyond Python 3.10+. Name scenarios to run a subset, e.g. `python3 tests/run_all.py multi`.

### Hosted roadmap

- [x] **Step 1 — server/client split.** `--mode`, `--public-host`, client-side routing, Docker image.
- [x] **Step 2 — multi-account.** Each player registers a name (+ auto-generated secret) on the server
  and gets their own club database under `users/`. Identity travels LSX → Blaze login → FUT session
  token; unknown clients fall back to their VPN IP. *Not yet:* a shared human transfer market
  (each club still trades against the AI market).
- [~] **Step 3 — matches between friends.** *In progress.* Blaze `GameManager` create/join, mesh
  connection and game-setup notifications, player network-info exchange over `UserSessions.UserAdded`, and
  QoS answers all **work**; matchmaking pairs the two clients cleanly. The remaining piece is the P2P
  NAT-punch: our own **ProtoMangle demangler** + kernel steering deliver probes and replies, but the exact
  demangler *response format* the game accepts is not yet decoded (see
  [Status](#status--real-online-p2p-matches-current-focus)). Two-sided `GameReporting` comes after.
- [ ] **Step 4 — public internet.** NAT traversal/relay for friends behind their own NATs (server-relayed
  P2P, the Pocket Relay model), matchmaking, hardening. No VPN for players.

## Bug reports

When reporting a problem, include:

- What screen/action you were on.
- What you expected to happen.
- What actually happened/crashed/froze.
- The newest log from `%LOCALAPPDATA%\FIFA15LocalFUT\logs`.

Please test on a legitimate FIFA 15 PC installation and keep reports focused on the localhost/offline restoration.
