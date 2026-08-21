# Hosted-mode tester guide (Windows)

Everything in hosted mode so far was verified only with scripted clients on Linux. This guide lists what a
real FIFA 15 PC client needs to confirm, in order, and exactly what to send back. Each phase builds on the
previous one — stop at the first phase that fails and report it.

**Golden rule: copy the logs before restarting anything.** Every start of `server.py` truncates its log.

## What you need

- Your own FIFA 15 PC install with the normal Local FUT payload working (offline FUT loads, packs open).
- Python 3.10+ (same as before; `INSTALL_PREREQUISITES.cmd`).
- This fork: `git clone https://github.com/floholz/FIFA-15-Local-FUT-hosted.git` (or a ZIP of `main`).
- Phase B+ : a second PC with FIFA 15 and a VPN overlay between the two (Tailscale is the easiest — both
  machines get a `100.x.y.z` address; use the **IP**, not the MagicDNS name, for now).

## Where things live

| what | where |
|---|---|
| server log (hosted server) | `%LOCALAPPDATA%\FIFA15LocalFUT\logs\localfut15-server.log` |
| client/local log | `%LOCALAPPDATA%\FIFA15LocalFUT\logs\localfut15.log` |
| raw Blaze packets | `%LOCALAPPDATA%\FIFA15LocalFUT\logs\fire2-rx-*.bin` |
| decoded unknown Blaze packets | `%LOCALAPPDATA%\FIFA15LocalFUT\logs\cards-scout-*.json` |
| player accounts (server) | `%LOCALAPPDATA%\FIFA15LocalFUT\users.sqlite3`, clubs in `users\user-<id>.sqlite3` |
| this PC's hosted settings | `%LOCALAPPDATA%\FIFA15LocalFUT\hosted.json` |

To go back to the normal offline build at any time: `CONNECT_TO_SERVER.cmd`, leave the address empty.

---

## Phase A — one PC: hosted server + client on the same machine

Goal: does FIFA still boot and load FUT when it talks to a *hosted* server instead of the local one, and
does the server recognise which player is connecting?

1. Run the fork's top-level `PLAY_LOCAL_FUT15.cmd` once so the new payload (new `.cmd` files,
   `server.py`) is installed into the game folder. Let it launch FIFA, confirm offline FUT still works, quit.
2. In the game folder run **`START_FUT15_SERVER.cmd`**. When asked for the address, type `127.0.0.1`.
   Windows Firewall may ask about python — allow it (private networks). Leave this window open.
   Expected banner: `READY [mode=server]`, `Players : 0 registered`.
3. Run **`CONNECT_TO_SERVER.cmd`**: address `127.0.0.1`, player name e.g. `kyro`.
4. Start the game with the usual **`PLAY_LOCAL_FUT15.cmd`** / desktop shortcut. The launcher now waits for
   the *server*; the client window should say `READY [mode=client]` and `Player : kyro (persona 1200000001)`.
5. In FIFA: enter Ultimate Team. Note everything: club-name prompt? coins? squad? store/pack opening works?
   transfer market search works? Does the game show `kyro` anywhere?
6. Quit FIFA. Copy the whole `logs` folder to `logs-phaseA`.

Report: did FUT load (yes/no/where it stopped) and these lines from `localfut15-server.log`:

- `REGISTER ok user=...`
- `BLAZE LOGIN user=... via=... fields=...`  ← **the most important line in this whole guide**
- `UT AUTH user=... via=... body=...`
- any `UNIDENTIFIED FUT client` lines

Verified with a real client (Proton/Linux, 2026-08-21): FIFA 15 sends an **empty** auth field in its Blaze
login, so `via=token` will not appear. The server identifies players by the machine **MAC address** the game
reports (Blaze PostAuth and the `/ut/auth` body) and by client IP. Expected: `BLAZE LOGIN … via=ip` or
`via=mac`, then `UT AUTH … via=mac`. `via=default` on a hosted server means the player was not recognised —
report it together with the `REGISTER ok … mac=` line.

## Phase B — two PCs over a VPN

Goal: non-loopback routing. This is the first time the hook's `EA-MITM.ini` redirects to a real IP.

1. PC1 (host): `START_FUT15_SERVER.cmd`, address = PC1's Tailscale IP (e.g. `100.101.102.103`).
2. PC2: `CONNECT_TO_SERVER.cmd` with that IP and a *different* player name. `PLAY_LOCAL_FUT15.cmd`.
   If the launcher says the server is not reachable, it lists which ports failed — that's a firewall/VPN
   problem, not a game problem; check Windows Firewall on PC1 for python.
3. PC1 can play at the same time as a client (`CONNECT_TO_SERVER.cmd` with its own Tailscale IP or
   `127.0.0.1`, its own name).
4. Both enter FUT. Check both have **separate** clubs/coins (open a pack on one; the other must not change).
5. Server log: both `REGISTER ok` lines, both `BLAZE LOGIN` lines with different `user=`.

Report: same as Phase A plus whether anything was slower/odd with the remote address (menu loading,
store images, squad loading).

## Phase C — poke every online entry point (this is the capture we need)

Goal: make FIFA send us the Blaze `GameManager` traffic we have never seen. Nothing is implemented for it
yet, so every one of these will fail, hang or bounce back — **that is expected**; the logs are the result.

With both players logged in (Phase B) — or one player in Phase A — try each of these, one at a time, and
write down the time and what the screen did (error text, hang, returned to menu, timeout after N seconds):

1. FUT → **Online Seasons** → start a match (matchmaking).
2. FUT → **Online Single Match** / any other online mode FUT 15 offers.
3. Main menu → **Online** → Online Seasons.
4. Main menu → **Online** → Online Friendlies → invite (the friends list is empty — note what it shows).
5. Main menu → **Online** → Pro Clubs (if it opens at all).
6. Main menu → EA SPORTS Football Club / Catalogue screens (cheap extra coverage).
7. Let the game sit in a FUT menu for 3–4 minutes (timed Blaze traffic: pings, presence, updates).

If the game freezes hard, kill it and move on; the packets up to that point are already on disk.

After each run, copy `logs` to `logs-phaseC-<what you clicked>`. The files we want:

- `localfut15-server.log` — contains `CARDS-SCOUT Unhandled Blaze request c=... cmd=... :: tree={...}`
- `cards-scout-c*.json` — one per unknown packet, with a decoded `tree` and the raw hex
- `fire2-rx-*.bin` — raw frames (zip the whole folder; they are small)

`c=4` is GameManager (matchmaking / game sessions), `c=30722` UserSessions (network info), `c=25`
Association lists (friends), `c=9` Util. Packets with `c=4` are the prize.

## Phase D — bonus, only if A–C were painless

- Run `ADD_COINS.cmd` on a client PC: it must change only that PC's *offline* club, not the hosted one.
  The server-side equivalent is `python localfut15\add_coins.py 100000 --user kyro`.
- Reinstall the payload (`PLAY_LOCAL_FUT15.cmd` from the release folder) and confirm `hosted.json` kept the
  server address and player name.
- Change the club name in FUT, restart, confirm it stuck (per-player club DB).

## Sending it back

Zip per phase: the `logs` copy, `hosted.json` (it contains your `player_secret` — fine for testing, but
you'll want a new name afterwards), and the timeline notes. A screenshot of the banner windows helps. Drop
it in the repo as an issue or send it to floholz.
