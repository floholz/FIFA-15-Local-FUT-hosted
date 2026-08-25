# P2P hook — Phase 0 (visibility)

A `dinput8.dll` proxy injected into FIFA 15 that logs the game's UDP/P2P socket
traffic from **inside the client**. It does not change any traffic yet — Phase 0
only answers one question: when two players are matched, does the game send
**direct CommUDP to the peer** (then we can transparently relay it), or
**ProtoTunnel frames to the QoS/tunnel port** (then the relay must route tunnel
frames)? That decides the Phase 1 design in `docs/P2P_TUNNEL_DESIGN.md`.

It coexists with the existing EA-MITM hook by:
1. forwarding `DirectInput8Create` to the real system `dinput8.dll`,
2. chain-loading the current EA-MITM hook (renamed `ea-mitm.dll`) so its
   ProtoSSL redirect keeps working,
3. hooking `sendto`/`recvfrom`/`WSASendTo`/`WSARecvFrom` (WinSock UDP) to log
   each datagram's peer `ip:port`, length, and first 32 bytes.

## Build (on Linux, mingw-w64)

```sh
sudo pacman -S --needed mingw-w64-gcc cmake ninja   # one-time (CachyOS/Arch)
tools/p2p-hook/build.sh
# -> tools/p2p-hook/build/dinput8.dll
```

## Install into a FIFA 15 install (per client under test)

In the game folder (the one with `fifa15.exe`, e.g. `~/Games/EA_OSS/FIFA_15`):

```sh
cd ~/Games/EA_OSS/FIFA_15
cp dinput8.dll ea-mitm.dll                       # the EXISTING EA-MITM hook, renamed
cp /path/to/tools/p2p-hook/build/dinput8.dll .   # our observer becomes dinput8.dll
```

Keep `EA-MITM.ini` in place — the renamed `ea-mitm.dll` still reads it.
`WINEDLLOVERRIDES="dinput8=n,b"` (already set by `tools/linux/play.sh`) makes
Proton load the game-folder `dinput8.dll` — now ours.

## Run and read

Launch as usual (`play.sh`, NVIDIA vars on the Optimus laptop). Get to a match
against the other client. Then read, in the game folder:

```
p2p-hook.log
```

Lines look like:
```
16:44:51.520 SENDTO   sock=123 100.121.67.31:17502 qos/tunnel? len=20 hex=00000002...
16:44:51.560 SENDTO   sock=124 100.103.106.74:3659  GAME-P2P    len=96  hex=...
```

What to look for:
- Traffic to `:3659 GAME-P2P` on the **peer's** address  → direct CommUDP; Phase 1
  can transparently relay. Best case.
- Only `:17502 qos/tunnel?` and no peer `:3659`  → ProtoTunnel; the relay must
  route tunnel frames (still no full reverse — route by header).
- Which `sock=` handles which — tells us the game vs. service sockets.

Send back `p2p-hook.log` from both clients after a match attempt.

## Uninstall

Delete `dinput8.dll` and `ea-mitm.dll`, then restore the original EA-MITM hook as
`dinput8.dll` (or re-run the payload installer).

## Notes
- Phase 0 is observe-only and touches no game memory beyond MinHook's inline hooks
  on documented WinSock exports (hooked by name, no FIFA offsets needed).
- License: GPL-2.0-or-later (it loads alongside EA-MITM, which is GPL-2.0).
