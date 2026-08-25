# Design sketch: client hook + server tunnel for real hosted P2P

Goal: friends behind their own home NATs play each other through **your VPS** — no VPN, no
port-forwarding, no reversing EA's ProtoTunnel. Modeled on **Pocket Relay** (Mass Effect 3, same
BlazeSDK/DirtySDK stack), which makes live P2P work by tunnelling the game's traffic through its
own server instead of using EA's dead tunnel/NAT services.

## Why a tunnel (not direct P2P, not EA's ProtoTunnel)
- Friends are behind home NATs → cannot reach each other directly without NAT traversal, and the
  service that did that (`demangler.ea.com`) is dead. So NAT stays type 5 and the client won't do
  direct P2P anyway.
- Reversing EA's ProtoTunnel wire format on a protected binary is intractable and the config
  tunables to steer ConnApi aren't obtainable (see `fifa15-p2p-protocol-findings` memory).
- A **relay through the VPS** needs only outbound connections from each client (any NAT allows
  that) and no address of the peer at all. This is the correct architecture regardless.

## Architecture
```
 FIFA (client A) --UDP P2P--> [our WinSock hook A] --our tunnel proto--> VPS tunnel server
                                                                              |  pairs by game
 FIFA (client B) <--UDP P2P-- [our WinSock hook B] <--our tunnel proto-- VPS tunnel server
```
Each client's hook intercepts the game's P2P UDP, wraps it, and sends it to the VPS. The VPS
forwards between the two hooks of the same game. Each hook delivers received payloads back to the
game as if they came directly from the peer. Neither game ever learns the other's real address.

## Component 1 — client hook (new DLL, WinSock layer)
We do NOT have EA-MITM source, and it hooks ProtoSSL (TCP) only. Write a **separate injected DLL**
that hooks the OS UDP send/recv path — game-agnostic, below DirtySDK/ProtoTunnel:
- Hook `ws2_32.dll` `sendto` / `recvfrom` (and `WSASendTo`/`WSARecvFrom`) via MinHook/Detours.
- Injection vector: same as today. The game already loads our `dinput8.dll`; ship this as a second
  DLL it force-loads, or fold it into the same injection. (Coordinate with whoever maintains
  EA-MITM, or load independently — the two hooks touch different layers and don't conflict.)

Behaviour:
- **Outbound**: when the game `sendto`s a game/P2P packet, prepend a small header
  `{magic, game_id, self_slot, dest_slot, seq}` and redirect the datagram to the VPS tunnel port.
  The `dest_slot`/peer identity comes from a per-game marker the server planted in the peer address
  it handed us (see "Steering"), so the hook knows which peer each packet is for.
- **Inbound**: datagrams from the VPS tunnel port are unwrapped; the hook returns the inner payload
  to the game's `recvfrom` with the source address spoofed to the "peer" address the game expects,
  so DirtySDK believes it has a direct link.
- **Pass-through**: non-game traffic (DNS, the ProtoSSL services EA-MITM already handles, QoS) is
  left untouched — filter by socket/port so we only tunnel the game P2P socket (game port 3659).

Open question the hook resolves immediately (Phase 0 below): whether the game emits **direct
CommUDP** to the peer address (easy: wrap it) or **ProtoTunnel frames** to :17502 (then we either
blind-forward those frames keyed by their header, or force direct — decided by what we observe).

## Component 2 — server tunnel (extend the Python server)
We already have `_UdpRelay` (learns two peers, cross-forwards). Grow it into a **game-keyed tunnel**:
- Bind one well-known UDP tunnel port on the VPS (reuse 17502 or a new one).
- Read our header, look up `game_id`, forward the payload (re-wrapped) to the other slot's last-seen
  tunnel address. Learn each client's tunnel source address from its first packet (opens its NAT
  mapping), exactly like `_UdpRelay` does today.
- Tie tunnel sessions to the existing GameManager games (`_MM_GAMES`) so a tunnel packet for game
  900001 is routed to the right opponent, and cleaned up on `leaveGame`/`destroyGame`.
- Auth: include the player's session token / game id in the tunnel header so a client can only join
  the game it belongs to.

## Steering (how the hook knows the game/peer without reversing ConnApi)
We already control the addresses handed to the client (`UserSessions.UserAdded` `ADDR`, and HNET).
Plant a **synthetic peer address** there that encodes routing for the hook, e.g. a fixed private
subnet `10.66.<game_low>.<slot>` on port 3659. The hook recognises that subnet, reads game/slot from
the octets, and tunnels accordingly. The game just sees "a peer address," connects to it, and the
hook captures every packet aimed at it. No ConnApi tunables needed.

## Phased plan
- **Phase 0 — visibility (small, do first).** Ship a *logging-only* WinSock hook: log every
  `sendto`/`recvfrom` (dest ip:port, length, first 16 bytes) for the game socket. This finally gives
  the client-side view we never had (DirtySDK logging is off). It answers the one architectural
  unknown: direct-CommUDP vs ProtoTunnel-to-:17502, and on which port/socket. ~a day of hook work,
  decides everything after.
- **Phase 1 — loopback tunnel.** Implement the wrap/redirect + server game-keyed forward; prove two
  clients on one LAN connect *through the VPS* (both send only to the VPS). Reuses `_UdpRelay`.
- **Phase 2 — real hosted.** Two friends, different networks, VPS on the public internet. Add the
  session-token auth, NAT-mapping learning, keepalives, teardown on game end.
- **Phase 3 — hardening.** Reconnect, packet-loss behaviour, MTU, multiple concurrent games,
  spectators/8-slot if ever needed, metrics.

## Skills / effort / risk
- New skill vs. the Python work so far: **C/C++ Windows API hooking** (MinHook/Detours, `sendto`
  interception). Bounded, well-trodden, and there's an open-source reference (the Pocket Relay
  client). Runs under Proton fine (it's just a WinSock hook in the Wine process).
- Main risk: if the game hard-refuses to emit game data until ConnApi "connects" (i.e. it won't send
  P2P at all while it thinks it must ProtoTunnel), Phase 0 reveals it and we fall back to
  blind-forwarding the ProtoTunnel frames by header — still no full reverse needed, just header
  routing.
- Everything server-side we built (matchmaking, roster, addresses, game lifecycle) stays; this adds
  the transport under it.

## References
- Pocket Relay server (Rust): `PocketRelay/Server` — `src/services/tunnel/` (udp_tunnel, mappings),
  `src/routes/qos.rs` (numprobes=0). Its client shows the game-side interception pattern.
- Our seed: `_UdpRelay` / `_RelayManager` in `payload/localfut15/server.py`.
- `fifa15-p2p-protocol-findings` memory for why direct/ProtoTunnel/tunables are off the table.
