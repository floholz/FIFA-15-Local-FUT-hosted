# Hosting a Local FUT server on a VPS (friends only)

The server is the Python backend in `payload/localfut15/`. It never needs the game, the DLLs, or the `.big`
files — those stay on each player's PC. This guide runs it on a VPS and gates it to your friends.

## Two gates, use both

The game's traffic is **plaintext** (the compatibility hook disables TLS), so the ports must not sit open on
the public internet. Gate at two layers:

1. **Network gate (primary) — put the server on a VPN, not the open internet.** Install **Tailscale** (or
   WireGuard/ZeroTier) on the VPS and on each friend's PC. Everyone joins your tailnet and gets a stable
   `100.x.y.z` address. Bind the server to the VPS's tailnet IP; anyone not on the tailnet cannot reach the
   ports at all. This is the strong gate and it also keeps the plaintext traffic off the public internet.
2. **App gate (defense in depth) — an access code.** Set a `SERVER_ACCESS_CODE`; a client must send the
   matching code to register, or it is refused (403). Optionally also restrict to an allowlist of player
   names. Even someone on your tailnet can't create a club without the code.

## Run it with Docker (recommended)

```sh
# on the VPS, in a checkout of this repo
PUBLIC_HOST=100.101.102.103 \
SERVER_ACCESS_CODE=choose-a-shared-code \
docker compose up -d --build
```

- `PUBLIC_HOST` is the address the game clients will use — **your VPS's Tailscale IP** (or a DNS name that
  resolves to it). The server advertises this to clients in the redirector/QoS/FUT responses, so it must be
  reachable from every player.
- `SERVER_ACCESS_CODE` turns on the access-code gate. Omit it for an open server (only do that if the
  network gate alone is enough for you).
- `ALLOWED_PLAYERS="kyro,flo,sam"` (optional) additionally restricts which names may register.
- Player clubs and the SQLite saves live in the `fut15-data` volume (`/data`); back that up to keep clubs.

Ports published: 42230, 10051, 17502, 42232, 8199, 8099 (TCP). With Tailscale you can leave the VPS
firewall closed to the public and rely on the tailnet; if you don't use a VPN, restrict these ports to your
friends' IPs at the firewall.

## Run it without Docker

```sh
FUT15_SERVER_ACCESS_CODE=choose-a-shared-code \
python3 payload/localfut15/server.py --mode server \
  --host 100.101.102.103 --public-host 100.101.102.103
```

`--host` is the interface to bind (the tailnet IP; `0.0.0.0` binds everything). `--public-host` is what the
server advertises to clients — normally the same tailnet IP. State goes under
`$XDG_DATA_HOME/fifa15-local-fut` (override with `FUT15_RUNTIME_ROOT`).

The startup banner shows the gate status:

```
 Gating     : access code required, 3-name allowlist
```

## How a friend connects

1. Install Local FUT once (they need their own FIFA 15 PC copy): `INSTALL_PREREQUISITES.cmd`, then
   `PLAY_LOCAL_FUT15.cmd`.
2. Join your Tailscale tailnet.
3. Run **`CONNECT_TO_SERVER.cmd`** and enter: the VPS's tailnet address, their player name, and the
   **access code** you gave them.
4. Launch with `PLAY_LOCAL_FUT15.cmd`. The launcher checks the server is reachable and refuses to start FIFA
   if it isn't.

Their club, coins and cards live on your VPS (`/data/users/user-<id>.sqlite3`), not on their PC. The access
code and a per-player secret are stored in their `hosted.json`.

## Server-side admin

- `python localfut15/add_coins.py <amount> --user <name>` credits one player's club.
- `GET http://<vps>:8199/localfut/status` reports version, mode, player count and whether gating is on.
- Registered players are rows in `/data/users.sqlite3`; delete a row to free a name.

## What works over the VPS today, and what doesn't yet

- **Works:** each friend gets their own persistent club on your server (own coins, squads, packs, offline
  Seasons), gated to your group.
- **In progress:** a **shared human transfer market** (buy/sell each other's cards) — the next feature.
- **Not yet:** live matches against each other. FIFA 15 matches are peer-to-peer UDP brokered by Blaze
  GameManager, and the client's online hub does not yet reach "ready" against this server (see
  `LINUX_PROTON_FINDINGS.md`). That needs GameManager + a UDP match relay on the VPS and is the long track.
