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
