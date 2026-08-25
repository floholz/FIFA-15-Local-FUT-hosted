# Online-match (Step 3) status — where the P2P work stands

Snapshot 2026-08-25 (server `payload/localfut15/server.py` v0.4.8). Paused at the DirtySDK
ProtoTunnel wall. Everything up to the peer link works; the peer link itself does not yet.

## Works now
- Matchmaking: `startMatchmaking` → pair → `NotifyGameSetup` (host-created) → `finalizeGameCreation`
  → PRE_GAME + `NotifyPlayerJoining` + joiner `NotifyGameSetup`. No crashes.
- **FIFA-accurate `ReplicatedGamePlayer`** decoded from the exe: it has **no per-player address**
  (fields: CONG, CSID, EXID, GID, JFPS, LOC, NAME, NASP, PID, PSET, RCRE, ROLE, SID, SLOT, STAT, TIDX,
  TIME, UGID, UID, UUID). The old Blaze3SDK PNET/BLOB/PATT layout crashed the joiner.
- **Peer address delivery via `UserSessions.UserAdded`** (component 30722, cmd 2): `DATA` =
  UserSessionExtendedData with `ADDR` (IpPairAddress union), `USER` = UserInfo. Connection group id
  `CONG` == persona, matching the SCG/TCG the client sends in `updateMeshConnection`.
- GameManager notifications on Blaze3SDK ids (116 GamePlayerStateChange, 30 JoinCompleted, 21 Joining,
  100 GameStateChange, 40 PlayerRemoved, 16 GameRemoved), leave/destroy, socket-drop cleanup.
- `tools/tdftags.py` extracts any Blaze struct's exact TDF layout from `fifa15.exe`.

## The wall: DirtySDK ProtoTunnel
Tested two machines over Tailscale (off-subnet → forces unicast). Even with the peer address delivered
and QoS set to `numprobes=0`, the joiner **never sends to the host's :3659**. Its only UDP traffic is a
steady 20-byte stream to the server's **:17502** — format `00000002 00000001 00000000 00000001 <tick>` —
which `numprobes=0` did not stop, so it is **not** the QoS ping-probe. After setup it sends only
`updateHardwareFlags` (30722 cmd 8) then idles.

FIFA 15 routes this P2P through **DirtySDK ProtoTunnel** (exe strings `Override_ProtoTunnel_`,
`prototunnel-tunnel`, `prototunnel-global-recv`): a server-mediated tunnel on the QoS/17502 port that we
do not implement, so no game data flows and no direct 3659 connection is attempted.

Note: `fifa15.exe` is a **protected/virtualized binary** (`.arch`/`.xtext` sections) — its QoS/ConnApi
*code* cannot be statically disassembled. Its TDF *metadata* (data) is readable.

## To resume — options, roughly in order of leverage
1. **Client-side visibility.** Find a way to enable DirtySDK NetPrintf logging in FIFA 15 so the client
   reveals its tunnel decision and the 17502 packet format. Unblocks everything; no known switch yet.
2. **Find an existing ProtoTunnel/QoS server** for a same-era DirtySDK/Blaze title and port its tunnel.
3. **Reverse + implement ProtoTunnel** server-side (relay the tunnel between the two clients on 17502)
   from packet captures. High effort, undocumented, protected binary.

## Server knobs (env)
`FUT15_NTOP` (topology, default 130), `FUT15_SUPPRESS_LANIP` (mirror overlay IP into INIP, default on),
`FUT15_RELAY_ENABLED` (UDP relay), `FUT15_QOS_MODE` (rawecho|addr), `FUT15_UDP_PROBES` (VPS diagnostics).

## Test recipe (two machines, tailnet)
See `docs/LAN_MATCH_TEST.md`. Over Tailscale: server `--public-host <A_tailnet_ip>`, both clients
`hosted_config.py client <A_tailnet_ip> <name>`, `sudo ufw allow in on tailscale0`, capture with
`sudo tcpdump -ni tailscale0 'udp'`.

---

## Update 2026-08-25 — demangler SOLVED; wall moved to the ConnApi mesh

The ProtoMangle demangler is no longer the blocker. Against EA's real `protomangle.c` (DirtySDK 15.1.6,
found in `TornadoCookie/EAWebKit16-Linux`) we established it is **HTTP-driven**, not the UDP service we first
answered: the client polls `GET /getPeerAddress?myIP=..&myPort=..&version=1.0` (Cookie: `sessionID`) and acts
on the response body's `status=` line — `probe` (fire the `:10000` UDP NAT-punch probes, re-poll), `success`
(`peerIP`/`peerPort` → done), or `failure`.

Implemented `_start_demangler_http_responder` (server) returning `status=success\r\npeerIP=..\r\npeerPort=..`
once matchmaking pairs the two players, routed via kernel DNAT+MASQUERADE for TCP `:10000`/`:3658`.
**Verified:** the game's real `/getPeerAddress` gets `success` and the host stops probing (ProtoMangle
accepts it). connapi.c confirms the shape is right — on demangle success ConnApi sets `pClient.uAddr = iAddr`.

**Current wall (BlazeSDK ConnApi/GameManager, source not public):**
- Host demangles cleanly but its CommUDP peer stays `0.0.0.0:3659` and it times out (`MM MESH P2P FAILED`).
- Joiner emits nothing — no demangle, no `updateMeshConnection` — only the `255.255.255.255:9999`
  LAN-discovery broadcast. It never calls `ConnApiOnline`.

**Experiments that did NOT unblock it:**
- `FUT15_JOINER_INIT_STATE=1` (admit the joiner in `INITIALIZING`, advance to `PRE_GAME` after the mesh):
  no change in joiner behaviour.
- `FUT15_BCAST_RELAY=1` (hook rewrites `:9999` broadcasts → server, server cross-forwards to the peer):
  bidirectional forwards verified and the game *does* bind `:9999` to receive, but it ignores an advert
  whose source is the relay, not a same-subnet peer — and a Tailscale `/32` overlay has no same-subnet
  peers, so LAN discovery is structurally the wrong path for this network shape.

**Next steps** (same priority as the top of this doc): (1) client-side DirtySDK/ConnApi NetPrintf to see why
`uAddr` stays 0 and whether ProtoTunnel is the real transport; (2) hook relay of the actual `:3659`/ProtoTunnel
transport (Pocket-Relay model), which sidesteps ConnApi; (3) change the network shape (real L2/same-subnet
overlay, or a server-hosted topology). New env knobs added: `FUT15_DEMANGLER_REPLY` (source|peer|relay),
`FUT15_JOINER_INIT_STATE`, `FUT15_BCAST_RELAY`, `FUT15_HOOK_REDIRECT` — all off/neutral by default.
