# Running FIFA 15 Local FUT on Linux with Proton — findings

Verified 2026-08-21 on CachyOS (kernel 7.2), Ryzen 9 7950X3D + RTX 3080, `umu-launcher` 1.4.3 with
GE-Proton11-5. The player owns FIFA 15 PC; the install folder was copied to `~/Games/EA_FIFA/FIFA 15`.

## It works

FIFA 15 runs under Proton with the Local FUT payload and **completes the entire online bootstrap** against
the local server — no Proton/Wine workarounds beyond one DLL override:

- Origin LSX stub (challenge/response, `GetProfile`, presence polling) — the hook DLL chain loads under Wine.
- Blaze redirector → PreAuth → the FIFA35 login → entitlements → `/pow/auth` → `/ut/auth`.
- Full FUT front end: club, store, pack opening, squads, transfer pile, offline Seasons.

Launch path: `tools/linux/install_payload.py` then `tools/linux/play.sh`. The only Wine tweak is
`WINEDLLOVERRIDES="dinput8=n,b"` (set by `play.sh`) so Proton loads the game's hook `dinput8.dll` instead of
its own. `install_payload.py` resolves the two git-lfs `.big` files itself, so git-lfs is not required.

## Identity: FIFA 15 does not forward a login token

The hosted-mode plan assumed the LSX auth code could carry a per-player session token into Blaze/FUT. The
capture disproves it:

- The Blaze FIFA35 login (`c=35 cmd=10`) sends `AUTH=""` — an **empty** auth field. The game never calls the
  LSX `GetAuthCode` at all.
- What the game *does* send is the machine **MAC address**: in Blaze `Util.PostAuth`/`GetTelemetryServer`
  (`MAC`/`CMAC`) and in the `/ut/auth` JSON body (`"macAddress"`).

So hosted multi-account identity is keyed on **MAC address**, with client IP as the fallback (fine over a
VPN, where each player has a distinct address). The server now binds players by MAC on both the Blaze and
FUT paths; `_resolve_fut_user` checks MAC before persona-id, and the Blaze handler rebinds the connection
when a PostAuth MAC identifies a different registered player. Log lines to watch: `BLAZE LOGIN … via=mac`,
`UT AUTH … via=mac`, `BLAZE MAC rebind …`.

Registration therefore sends the client's MAC (`uuid.getnode()`), and each player must run from a machine
with a distinct MAC/VPN IP — two players behind one NAT with the same MAC would collide.

## Network info for peer matches is already on the wire

`UserSessions.updateNetworkInfo` (`c=30722 cmd=20`) carries the player's P2P endpoint. Captured locally:

```
internal_ip 192.168.0.49, port 3659 (DirtySDK NetGameLink), nat_type 5, external filled by the server
```

The server stores this per player as `NET_INFO[user_id]`. That is exactly the address block a future
GameManager implementation must hand to the opponent to introduce two players. Port 3659 UDP is the FIFA P2P
match port.

## Online Seasons crashes the FUT client (expected — this is step 3)

Opening **Online Seasons** makes FIFA send `season/list?type=online` and `season/user?type=online`. Even
with the online descriptor made byte-identical to the working offline one (only `type` differs), the game
dies with:

```
EXCEPTION_ACCESS_VIOLATION reading 0x0 in CardsDLLzf.dll + 0xB9C8A
```

The faulting code is a virtual-call visitor loop (`mov rax,[rbx]; call [rax+0x60]`) walking a collection of
season/match objects and dereferencing a null element. The online season code path expects match/opponent
objects that a standalone server cannot supply yet — i.e. it wants the Blaze `GameManager` match session
that does not exist. This is not a season-JSON bug; it is the boundary of hosted **step 3** (real matches).

Because it crashes the game, the experimental online-season route is **disabled by default**
(`online_seasons_enabled: false` in `config.json`). Offline Seasons is unaffected and still works.

## Captured artifacts (for the GameManager work)

The Blaze login sequence is decoded in the server log as TDF trees, and every packet is saved under
`%LOCALAPPDATA%`/`$XDG_DATA_HOME/fifa15-local-fut/logs/fire2-rx-*.bin`. Components seen during login:

| component | meaning | seen |
|---|---|---|
| 9 (Util) | PreAuth, client config, ping, options | yes, handled |
| 35 (FIFA login) | title login, `AUTH=""` | yes, handled |
| 30722 (UserSessions) | network info, hardware flags | yes, network info captured |
| 1 (Authentication) | ListEntitlements | yes, handled |
| 25 (AssociationLists) | friends/avoid lists | yes, empty |
| 15, 21, 11, 2249, 7 | messaging, presence, ticker, misc | yes, empty replies |
| **4 (GameManager)** | **create/join game, matchmaking** | **not yet — needs a match attempt that gets past the season screen** |

Getting `c=4` packets is the next capture: it requires either implementing enough of the online-season flow
that the client reaches matchmaking, or finding another online entry point (Online Friendlies) that brokers
a game without the season objects. That is where two real clients over a VPN become necessary.
