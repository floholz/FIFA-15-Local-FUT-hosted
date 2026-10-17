# Roadmap — from working prototype to a clean, shippable project

Written 2026-10-17, updated the same day after the launcher shipped. Online FUT matches over the public
internet work (see the README status). This document plans what comes after: a clean server/client split,
a launcher for players, real accounts, and a gradual move from Python to Go. **No rush:** every phase is useful on its own, and the Python server stays the
working reference until the Go code matches it.

## Where we are

**Server.** The split is a runtime switch, not separate code. `payload/localfut15/server.py` (~10.9k lines)
holds every role: redirector, Blaze, QoS, FUT HTTP, GameManager, UDP relay, Origin LSX stub, the code that
writes `cl.ini`/`EA-MITM.ini`, game-folder detection and port remapping. `--mode local|server|client` picks
which listeners start. The Docker image copies only `payload/localfut15/`, so it leaves out the game files
but still carries the client-side code. `payload/server.py` is a stale copy (out of sync since 188cd72).

**Client files.** Measured against the repack's originals (backup from the first install):

| File | Status |
|---|---|
| `fifa15.exe` | not replaced; the repack's CPY exe |
| `dinput8.dll` | the EA-MITM hook (GPL-2.0, no source here): turns TLS off, redirects hosts/ports. Hooks **fixed addresses** (`ProtoSSL Connect=0x143066524`, `LaunchFailureCheckRva`), so it is pinned to this exe build |
| `ItsAMe_Origin.dll` | the repack's Origin emulator, 62 bytes patched |
| `CardsDLLzf.dll` | same size as the original, 100 bytes patched in ~11 spots |
| `cards0.big`, `data_patch.big` | modded archives (data_patch: 124 MB original → 158 MB) |

So today's client is tied to one specific exe build plus an Origin emulator. Whether our CardsDLL and `.big`
changes apply to a retail install is unknown until we compare hashes from a legit copy.

**Identity.** Accounts are bound to the machine's MAC addresses, and the server is gated by one shared access
code. FIFA sends an empty `AUTH` in the Blaze login and never asks LSX for an auth code, so there is no token
in the game protocol to build on. MACs can be spoofed, and the code is shared.

**Players' setup.** Since 2026-10-17: the **OUFT Online** launcher (`cmd/ouft-launcher`, Go, Windows +
Linux), shipped as a zip with the payload. No Python on players' PCs. The `.cmd` scripts, `play.sh` and the
Python client mode still work but are no longer needed.

**Name.** Player-facing parts are called **OUFT** (Open Ultimate Football Team); the launcher is
**OUFT Online**, versioned `15.<minor>.<patch>` (15 = the game, minor 0 = beta). Keep "FIFA"/"FUT" out of
anything players see; the game's own strings, file names and data paths stay as they are.

## Architecture (decided 2026-10-17)

Two separate services, started together by **one `docker compose up -d`** on the VPS:

```
docker-compose.yml
├── accounts   Go + PocketBase. Game-agnostic: users, invites, login, per-game profiles, stats.
│              Behind Traefik (HTTPS) at ouft.fhun.site: register page, login API, admin dashboard.
└── fut15      The game server: Python today, Go after Phase 5. FIFA 15-specific.
               Game ports published directly (TCP 42230/10051/17502/42232/8199/8099, UDP relay
               45001-45063); the game speaks plaintext to fixed ports on an IP, so no Traefik here.
               Address for players: fut15.fhun.site. Talks to accounts over the compose network.
```

- **Separate, not one binary:** a game-server crash or restart does not log anyone out, an accounts update
  does not end a match, and other games or versions can use the same accounts service later.
- **Simple setup anyway:** compose starts both in the right order (`depends_on` + healthcheck). The
  systemd unit for the game server goes away with the switch to compose.
- **Accounts know nothing about FIFA.** Users and invites are generic; each user gets a profile per game.
  Game servers are API clients with a service key and use a small internal API ("verify this player's token",
  later "record this match result"). Game data (clubs) stays in the game server, keyed by the account id.
- **PocketBase stays at the edge** of the accounts code (one package imports it): it is pre-1.0 and has had
  breaking API rewrites. Pin the version.
- **Domains:** `ouft.fhun.site` = accounts web app (HTTPS via Traefik); `fut15.fhun.site` = FIFA 15 game
  server (plain A record, no certificate needed). `fut.fhun.site` keeps pointing at the same box until all
  players use a launcher with the new default.

## Why Go

- **The code maps directly.** The server uses only the Python standard library (sockets, `socketserver`,
  `sqlite3`, `struct`, the built-in HTTP server, XML) plus `cryptography` for LSX. Each has a direct Go
  equivalent. The TDF codec, Blaze framing and LSX AES-ECB are byte-level work, which suits Go.
- **It fits what the server does:** ~6 TCP listeners, a UDP relay pool and per-connection threads become
  goroutines. The relay and the ~50/s per-client link reports cost far less CPU.
- **Single binaries, cross-compiled.** One static file per side for Windows and Linux. That means no Python
  on players' PCs, no prerequisite installer, and small server images for the compose setup.
- **Shared code.** One TDF/Blaze package for the server, the launcher and debugging tools.

**Costs to keep in mind:** much of the 11k lines is game-specific knowledge (response shapes, the
notifications the client waits for, report cmd 1 vs cmd 2, item ID bases…). Each one lost in the port costs
a session with real clients to find again. Examples: answering the opponent-squad request with `{}` caused
"connection lost" at kick-off; an Origin session timeout quit the game in the 52nd minute; a missing
notification 114 only showed as an error after 90 minutes. The game fails quietly or misleadingly, often
late, and checking needs two machines and people playing. The test suite, recorded packets and shadow mode
below are how we avoid that.
Python also stays handy for quick live fixes while the protocol still changes.

## Phases

### Phase 0 — proper hosting
- [x] Server as a systemd unit that survives reboots (`tools/linux/install-service.sh`).
- [x] Per-packet debug dumps opt-in (`FUT15_DEBUG_DUMPS=1`; they slowly fill the disk).
- [ ] Backups of `users/` (deferred until real play starts).
- [ ] First real friend sessions, including the first Windows player.
- [ ] Move the game server into the compose setup (see Architecture); check in one match that the UDP relay
      still sees players' real source addresses through Docker's port publishing.

### Phase 1 — tidy the Python and build the safety net
- [ ] Delete the stale `payload/server.py`.
- [ ] Split `server.py` into modules along the future boundaries: `core` (TDF codec, config, constants),
      `server` (redirector, QoS, Blaze components, GameManager, relay, FUT HTTP, storage, catalogs),
      `client` (LSX stub, routing-file writer, install/backup/restore).
- [ ] Server-only entry point and package (no client code, no game files).
- [ ] Extend `tests/run_all.py` to the full online flow: matchmaking → game setup → relay →
      GameReporting (cmd 1 and 2, notification 114) → `/match/end` → rewards in both clubs.
- [ ] Turn the recorded packets (`cards-scout` JSON, Blaze dumps) into golden tests: given these request
      bytes, the response must be exactly these bytes.

Most scenarios in `tests/run_all.py` are black-box: they start the server process and talk HTTP and raw
Blaze over sockets. They will run against a Go binary unchanged. The few that import `server.py` directly
(e.g. `tdf`) get a socket-level or golden-file version.

### Phase 2 — client patches
- [ ] Get `fifa15.exe` / `CardsDLLzf.dll` / `.big` hashes from a legit retail install and compare them with
      the repack originals.
- [ ] Describe our changes as **patches, not whole files:** byte patches for the DLLs (CardsDLL is
      100 bytes), deltas for the `.big` archives. That means small downloads and no EA files to redistribute.
      Measured with `zstd --patch-from` against the repack originals: ~38 MB of patches instead of ~210 MB of
      whole files (data_patch.big 35.8 MB, cards0.big 1.9 MB, CardsDLL 373 B, ItsAMe_Origin 96 B). The
      launcher would check each original's hash, back it up and apply the patch, like it already does for the
      boot intro.
- [ ] **Rule until then:** no public GitHub releases. The launcher zip still contains whole EA and repack
      files, so it is only shared privately.
- [ ] A version table: known exe/DLL hashes → matching patch set.
- [ ] Target the player's own retail install. Find out whether our hook and LSX stub can work next to the
      EA App, or whether they have to replace it (open question).

### Phase 3 — launcher MVP (first Go code) *(done 2026-10-17: OUFT Online 15.0.0)*
One binary for Windows and Linux that replaces the `.cmd` scripts, `install_payload.py`, `play.sh` and the
Python client mode. UI: an embedded web page in an Edge/Chrome `--app` window (no cgo).
- [x] **Find the game:** registry, common install paths, `~/Games`, or let the user pick.
- [x] **Identify the version** by hash, or refuse with a clear message ("Try anyway" override).
- [x] **Install** with a backup first, and one-click restore. Today it copies whole files (Phase 2 turns
      them into patches).
- [x] **Server setup:** resolve the server to an IP and write `cl.ini`/`EA-MITM.ini`/`p2p-hook.ini`, check
      that the ports are reachable, register the player.
- [x] **Run the client side:** the LSX stub, ported to Go and byte-checked against the Python one.
- [x] **Linux:** launch through umu with the newest local GE-Proton, `dinput8=n,b` and a WineD3D toggle.
- [x] Extras: OUFT crest as the game's startup splash, boot intro cut to 3 frames (English file only;
      shortening the French one too hangs the game on the legal screen).
- [ ] Not yet tested on a real Windows PC (Edge app window, SmartScreen, Defender, Program Files).

### Phase 4 — accounts and login *(next; see Architecture)*
Invite + password, on the separate accounts service. The game protocol stays plaintext and never carries a
token: the launcher logs in over HTTPS, and the game server keeps binding the PC's MACs to the account, as it
does today with the per-PC secret.
- [ ] **Accounts service** (`cmd/ouft-accounts`, Go + PocketBase as a library): built-in users, an `invites`
      collection (token, created by, expiry, used by), registration only with a valid unused invite (public
      signup off), a small register page and a "generate invite link" admin page; the PocketBase dashboard for
      the rest (ban, reset password). Compose service behind Traefik at `ouft.fhun.site`; trust Traefik's
      `X-Forwarded-For` so rate limiting sees real client IPs.
- [ ] **Internal API** for game servers (service key, compose network only, never routed through Traefik):
      verify a player's token → account id + per-game profile.
- [ ] **Game server:** `/localfut/register` takes a login token instead of name + secret, verifies it with
      accounts, finds or creates the game account (a `pb_id` column in `users`) and binds the MACs. The account
      id from accounts becomes the id everywhere, so the Go server later needs no remapping.
- [ ] **Launcher:** username + password instead of name + access code, a "register with an invite" link that
      opens the register page, a refresh token stored instead of the secret (log in once per PC). Default game
      server `fut15.fhun.site`.
- [ ] **Migration:** link existing accounts (clubs) to new logins with a one-off script; accept the old
      access code for a short transition, then remove it.
- [ ] Later: **session binding** (the server accepts Blaze/FUT connections only for accounts with a live
      launcher heartbeat), and match results / stats posted to per-game collections in accounts (for
      profiles and leaderboards).

### Phase 5 — server to Go, one service at a time
Each service has its own port, so Go and Python versions can run side by side during the move.
- [ ] **Shadow mode** before any cut-over: the server sends every real request to both the Python and the
      Go version, answers with Python's reply, and logs every difference (bytes, or decoded TDF/JSON with
      volatile fields like timestamps and IDs masked). Real sessions then test the Go code without risking a
      match. A service moves to Go only after real sessions show no unexplained differences. For stateful
      services, Go gets its own copy of the databases so its writes never reach real clubs.
- [ ] Shared Go `tdf` + `blaze` packages, checked against the golden packets.
- [ ] Redirector and QoS (small; good first targets).
- [ ] UDP match relay (the biggest performance win).
- [ ] Blaze: login/sessions, UserSessions, Stats, GameManager, GameReporting.
- [ ] FUT HTTP last: the biggest part with the most quirks, and the one that gains least from Go.
- [ ] SQLite schema unchanged so existing `users/` databases carry over; a migration test against a copy of
      the live data before cut-over.
- [ ] Cut over once the protocol has settled (remaining unknowns: the "Leaving UT" screen, `user/list`, a
      shared transfer market), the whole test suite passes against Go, and shadow mode is clean.

### Phase 6 — our own hook *(later)*
- [ ] One hook DLL with source in this repo that replaces EA-MITM and merges `tools/p2p-hook`.
- [ ] Find its targets by **signature scanning** instead of fixed addresses, so it survives other builds.
- [ ] Probably stays C or Zig (`tools/p2p-hook` already builds with zig); it does not need to be Go.

## Planned Go layout

```
go.mod
cmd/ouft-launcher/     launcher + LSX stub (exists)
cmd/ouft-accounts/     accounts service: PocketBase app (Phase 4)
cmd/fut15-server/      game server binary (Phase 5)
internal/client/...    account, gamedir, install, lsx, routing (exist)
internal/accounts/...  the only code that imports PocketBase
internal/tdf/          TDF codec (shared)
internal/blaze/        framing, component/command ids, structs
internal/server/...    redirector, qos, relay, gamemanager, reporting, futhttp, storage
tests/                 run_all.py keeps driving either implementation over sockets
```

The accounts service could move to its own repository once a second game uses it; keeping it game-agnostic
from the start makes that a copy, not a rewrite.

## Open questions
- Do the retail `fifa15.exe`/`CardsDLLzf.dll` match the repack's? (Decides how portable the patches are.)
- Can our LSX stub replace or run next to the EA App for a retail install?
- Where does the launcher get patch sets and updates: bundled, or downloaded from our server?
- How Traefik runs on the VPS (Docker labels or file provider) decides how the accounts service joins it.
- Discord OAuth as an extra login option later? (Decided for now: our own invite + password.)
