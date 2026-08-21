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

## Online Friendlies blocks on a Blaze Stats leaderboard (before GameManager)

Driving toward **Online Friendlies** (2026-08-21, run 5): the game reaches the Friendlies hub without
crashing, then hangs on a loading spinner waiting for a **Blaze Stats** (component 7) leaderboard reply the
server does not implement. Exact requests captured:

- `c=7 cmd=4  getLeaderboardGroup {"NAME":"MyFriendlies"}` — wants the leaderboard definition.
- `c=7 cmd=16 getLeaderboard {"EID":[<persona>],"PCTR":1,"VID":1,...}` — wants the player's ranking rows.

The server currently answers both with an empty Fire2 body, which the client will not accept, so the UI
never advances to the host/join step. So both online entry points stall **before** GameManager:

- **Online Seasons** → crash in CardsDLL's season parser (null match/opponent objects).
- **Online Friendlies** → hang on an unimplemented Blaze Stats leaderboard (component 7 cmd 4 / 16).

Reaching GameManager (`c=4`) therefore needs one of these implemented first. The Stats leaderboard is the
lower-risk of the two (a self-contained request/response, no CardsDLL match objects), but its response TDF
(leaderboard group descriptor + ranked rows) has to be built from an open-source Blaze Stats implementation
(Pocket Relay / Arcadia) since EA's servers can no longer be captured. Raw requests are saved as
`cards-scout-c7-k4-*.json` and `cards-scout-c7-k16-*.json`.

**Update (run 7):** the Stats calls are now answered with structurally valid empty responses
(`getStatGroup` → `StatGroupResponse{DESC,KSUM,NAME,STAT:[]}`; the leaderboard fetch → `{LDLS:[],KSVL:{},COUN:0}`).
The client **accepts both and stops re-sending** — the Stats blocker is cleared. But the Online Friendlies
hub still shows the spinner: after the Stats replies the client goes quiet (only periodic presence polls and
a `/pow/v2/activity` POST), sending no further request. So it is now waiting on an *asynchronous*
online-hub-readiness signal, not a request we can answer — most likely a QoS/NAT determination (FIFA does a
UDP QoS probe on entering online modes; we only serve QoS over HTTP) or a post-login Blaze notification the
online hub expects. Reaching GameManager needs the online hub to reach "ready" first, which is a broad
surface (QoS/NAT + hub notifications + EASFC endpoints) — the Aurora writeup's "hundreds of fixes" territory.

**RESOLVED (run 8, via Blaze3SDK).** The blocker was that `(7,16)` is **`getStatsByGroupAsync`**: the RPC
reply is an empty ack, and the stats are delivered as an **async notification `(7,50)`
`GetStatsAsyncNotification`** carrying `KeyScopedStatValues`. Our first attempt returned the data inline in
the RPC and never sent the notification, so the hub waited forever. Sending an (empty) `KeyScopedStatValues`
notification after the ack **loads the Online Friendlies hub fully** (New Friendly Season / Recent
Opponents). `getStatGroup` `(7,4)` was also corrected to the full Blaze3SDK `StatGroupResponse`. Field tags
and encoder were validated byte-for-byte against Blaze3SDK. **The online hub is now reachable.**

Reference that cracked it: `Aim4kill/BlazeSDK` **Blaze3SDK** — component IDs match FIFA 15 exactly
(4/7/9/25/30722…), `ProtoFire` is the Fire2 framing, `EATDF` the Heat2 TDF. Port definitions from there
rather than guessing. See the `blaze-protocol-references` memory.

## Next gate: empty friends list (needs two clients)

At the loaded hub, "New Friendly Season" shows **"Add Friends to be able to play against them."** The friends
list is empty. To play a friend the server must populate the **AssociationLists** component (25:
`getLists` / `getListForUser` → `ListMembers`) and/or the LSX/Origin `QueryFriends` with the *other*
registered players, and then selecting a friend drives Blaze **GameManager** (component 4) to create/broker
the match. This is the point where **two clients** are required: one machine has no second player to befriend
or play. A single-client shortcut to capture the GameManager (`c=4`) request shapes is to inject a synthetic
friend so the client attempts create-game; the real playable match still needs two clients + (for arbitrary
NATs) a UDP relay on the host.

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
