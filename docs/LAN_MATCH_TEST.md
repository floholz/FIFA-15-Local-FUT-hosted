# Two-machine LAN match test (Linux/Proton)

Goal: prove the Blaze GameManager flow and the FIFA 15 peer-to-peer UDP link on the **simplest possible
topology** — two machines on one LAN (or one tailnet), no relay, no cloud firewall — with `tcpdump` on
both clients so we finally *see* what the game sends, instead of guessing from a VPS.

Two unknowns were tangled together in the VPS attempts: "is the GameManager sequence right?" and "does
UDP reach the VPS?". This test removes the second one entirely.

## What changed in 0.4.0 (`gm-blaze-flow`)

The GameManager now follows the Blaze 3 sequence (verified against Blaze3SDK / Pocket Relay):

1. host's `NotifyGameSetup` (roster = host only, host `ACTIVE_CONNECTED`, game `INITIALIZING`,
   `RSLT=SUCCESS_CREATED_GAME`);
2. host sends `finalizeGameCreation` → `NotifyGameStateChange(PRE_GAME)`; joiner admitted:
   host gets `NotifyPlayerJoining`, joiner gets `NotifyGameSetup` (host `CONNECTED`, self `CONNECTING`,
   `RSLT=SUCCESS_JOINED_NEW_GAME`);
3. clients connect over UDP and report `updateMeshConnection`; when both directions say `CONNECTED`
   the joiner becomes `ACTIVE_CONNECTED` (`NotifyGamePlayerStateChange` **116** — the old code sent 90,
   which is `NotifyPlayerAttribChange` — plus `NotifyPlayerJoinCompleted`);
4. the host drives `advanceGameState` → `IN_GAME` itself; the server no longer forces it.

Every GameManager request is logged decoded (`GM RX cmd=…`), and every `updateMeshConnection` target
status is logged (`MM MESH … status=CONNECTED/DISCONNECTED`). That line is the verdict on the UDP link.

## Setup

Machine A (this laptop) = **server + player 1**. Machine B = **player 2**. Both need the game installed
with the Local FUT payload (`tools/linux/install_payload.py --game <dir>`).

Pick A's LAN IP (`ip -4 addr`), e.g. `192.168.0.49`. If the machines are only connected through Tailscale,
use A's tailnet IP instead — everything below is the same.

### A: start the server (separate terminal, separate runtime root)

```sh
cd ~/dev/FIFA-15-Local-FUT-hosted
FUT15_RUNTIME_ROOT=~/.local/share/fifa15-local-fut-server \
FUT15_RELAY_ENABLED=0 \
python3 payload/localfut15/server.py --mode server --public-host 192.168.0.49
```

`FUT15_RELAY_ENABLED=0` hands each player the other's *real* address (direct P2P on UDP 3659). The
separate runtime root keeps the server's `hosted.json`/`runtime_ports.json` apart from the client's.
Server log: `~/.local/share/fifa15-local-fut-server/logs/localfut15-server.log`.

### A: play as client 1

```sh
python3 payload/localfut15/hosted_config.py client 192.168.0.49 flo
SYNC_PAYLOAD_FROM=auto tools/linux/play.sh --game ~/Games/EA_OSS/FIFA_15
```

Connect to the **LAN IP, not 127.0.0.1** — the address the server sees on the Blaze socket is the one
it hands to the opponent as your external address.

Optimus laptop (Intel iGPU listed first in `vulkaninfo`)? DXVK renders on the iGPU and FIFA shows a white
window with working sound. Force the NVIDIA GPU:

```sh
__NV_PRIME_RENDER_OFFLOAD=1 __VK_LAYER_NV_optimus=NVIDIA_only __GLX_VENDOR_LIBRARY_NAME=nvidia \
DXVK_FILTER_DEVICE_NAME=GeForce SYNC_PAYLOAD_FROM=auto tools/linux/play.sh --game ~/Games/EA_OSS/FIFA_15
```

### B: play as client 2

```sh
python3 payload/localfut15/hosted_config.py client 192.168.0.49 <other-name>
SYNC_PAYLOAD_FROM=auto tools/linux/play.sh --game <game dir>
```

### Both: capture UDP before starting matchmaking

```sh
sudo tcpdump -ni any -l 'udp and not port 53 and not port 5353 and not port 41641' | tee ~/fut-udp.txt
```

(41641 is Tailscale; drop that clause on a plain LAN.) Anything to/from port **3659** is the game link.

## Run

1. Both enter FUT → Online Seasons (or Online Friendlies) → start matchmaking.
2. Watch the server log for, in order:
   `MM PAIR` → `MM NOTIFY GameSetup(host, created)` → `GM RX cmd=15(finalizeGameCreation)` →
   `MM ADMIT` → `MM NOTIFY PlayerJoining` / `GameSetup(joiner)` → `GM RX cmd=29(updateMeshConnection)` →
   `MM MESH … status=…` → `MM JOIN COMPLETE` → `GM RX cmd=3(advanceGameState)` / `MM STATE … -> IN_GAME`.
3. Watch tcpdump on both machines.

## Reading the result

| you see | meaning | next |
|---|---|---|
| no `GM RX cmd=15` after `GameSetup(host, created)` | FIFA does not finalize the way Blaze expects; the joiner is admitted after the 4 s fallback anyway | note whether the host client crashed/what it showed |
| joiner gets `GameSetup(joiner)` but **no UDP at all** on either tcpdump | the client is not starting ConnApi — a Blaze-flow problem, not a network one | compare the `GM RX` trees / roster states against Blaze3SDK; the P2P addresses are irrelevant at this point |
| UDP from the joiner to the host's IP:3659, nothing back | host isn't listening / firewall on the host machine (`ufw`/`firewalld`) | open UDP 3659 on the host |
| UDP both ways, `MM MESH … CONNECTED` both directions | **the match link works** — the GameManager side is proven | move on to match flow / GameReporting; transport across NATs becomes a separate, isolated task |
| UDP both ways but `MM MESH … DISCONNECTED` | packets flow but the DirtySDK handshake fails (wrong port/addr pairing, or the client expects the tunnel on a different port) | check which ports appear on the wire vs. the advertised `PNET`/`HNET` |
| UDP goes to a **private** address that is not the peer | the client picked the INIP path (thinks the peer is behind the same NAT) | the EXIP/INIP handed out need adjusting |

Send back: both `fut-udp.txt`, the server log, and the client-side `logs/` folders.

## After this test passes

- Friends over the internet: everyone on a shared VPN overlay (Tailscale/ZeroTier) with the server also
  reachable there — the same direct-P2P path, no relay. The relay (`FUT15_RELAY_ENABLED=1`, default in
  server mode) stays for players who cannot share an overlay.
- Match results into both clubs (two-sided GameReporting), seasons progression, leave/disconnect handling.
