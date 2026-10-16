# Roadmap — from working prototype to a clean, shippable project

Written 2026-10-17. Online FUT matches over the public internet work (see the README status). This document
plans what comes after: a clean server/client split, a launcher for players, real accounts, and a gradual
move from Python to Go. **No rush:** every phase is useful on its own, and the Python server stays the
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

**Players' setup.** Python and the `cryptography` package on every PC, `.cmd` scripts on Windows,
`play.sh`/umu on Linux.

## Why Go

- **The code maps directly.** The server uses only the Python standard library (sockets, `socketserver`,
  `sqlite3`, `struct`, the built-in HTTP server, XML) plus `cryptography` for LSX. Each has a direct Go
  equivalent. The TDF codec, Blaze framing and LSX AES-ECB are byte-level work, which suits Go.
- **It fits what the server does:** ~6 TCP listeners, a UDP relay pool and per-connection threads become
  goroutines. The relay and the ~50/s per-client link reports cost far less CPU.
- **Single binaries, cross-compiled.** One static file per side for Windows and Linux. That means no Python
  on players' PCs, no prerequisite installer, and a server that is one file plus a systemd unit.
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

### Phase 0 — proper hosting *(current focus, README step 5)*
- [x] Server as a systemd unit that survives reboots (`tools/linux/install-service.sh`).
- [ ] Per-packet debug dumps opt-in (they slowly fill the disk).
- [ ] Backups of `users/`.
- [ ] First real friend sessions, including the first Windows player.

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
- [ ] A version table: known exe/DLL hashes → matching patch set.
- [ ] Target the player's own retail install. Find out whether our hook and LSX stub can work next to the
      EA App, or whether they have to replace it (open question).

### Phase 3 — launcher MVP (first Go code)
One binary for Windows and Linux that replaces the `.cmd` scripts, `install_payload.py`, `play.sh` and the
Python client mode:
- [ ] **Find the game:** registry and EA App/Origin paths, Steam/Proton prefixes, or let the user pick.
- [ ] **Identify the version** by hash and pick the matching patch set, or refuse with a clear message.
- [ ] **Patch** with a backup first, and one-click restore (replaces `RESTORE_BACKUP.cmd`).
- [ ] **Server setup:** resolve the server to an IP and write `cl.ini`/`EA-MITM.ini` (the hook does not
      resolve DNS), check that ports are reachable, optional UDP relay probe.
- [ ] **Run the client side:** host the LSX stub (~300 lines to port) for as long as the game runs.
- [ ] **Linux:** launch through umu with a pinned GE-Proton, the `dinput8=n,b` override and the WineD3D
      toggle.

Interim option: ship the Python client code as a PyInstaller build until the Go launcher is ready.

### Phase 4 — accounts and login
- [ ] Login in the launcher, over an HTTPS control API on the server (this API may sit behind Traefik/TLS;
      the game protocol stays plaintext and never carries the token). Options: Discord OAuth, invite + password,
      device-code flow.
- [ ] **Session binding:** after login the launcher reports "account X plays from this IP + these MACs".
      The server accepts Blaze/FUT connections only for active bindings, kept alive by a launcher heartbeat
      and expiring when it stops.
- [ ] This gives: one account per person instead of a shared code, banning and revoking, moving to a new PC
      without losing the club, and a home for admin tools and backups.
- [ ] Remove the shared access code once all players are on the launcher.

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
cmd/fut15-server/      server binary
cmd/fut15-launcher/    launcher + LSX stub
internal/tdf/          TDF codec (shared)
internal/blaze/        framing, component/command ids, structs
internal/server/...    redirector, qos, relay, gamemanager, reporting, futhttp, storage
internal/client/...    gamedir, patch, routing, lsx, proton
tests/                 run_all.py keeps driving either implementation over sockets
```

## Open questions
- Do the retail `fifa15.exe`/`CardsDLLzf.dll` match the repack's? (Decides how portable the patches are.)
- Can our LSX stub replace or run next to the EA App for a retail install?
- Which login method: Discord OAuth (what Aurora uses) or our own invite + password?
- Where does the launcher get patch sets and updates: bundled, or downloaded from our server?
