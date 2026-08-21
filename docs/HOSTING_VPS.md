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

## Behind Traefik / with a domain — important

**Do NOT put this server behind Traefik (or any HTTP reverse proxy).** It is not an HTTP app:

- The game client is patched to speak **plaintext** (TLS is disabled in the hook), so Traefik's HTTPS/ACME
  certificates have nothing to terminate — the client never does a TLS handshake.
- **Blaze (port 10051) is a raw binary TCP protocol**, not HTTP — Traefik's HTTP router can't carry it.
- The client connects to fixed **ports** (42230/10051/17502/42232/8199/8099) by address, not by HTTP `Host`
  header, so domain/path routing doesn't apply.

Traefik and this server **coexist fine** on the same VPS, because Traefik owns 80/443 for your other apps
and this server uses its own six ports. Just publish those ports directly and leave Traefik out of the path.

**Use your domain only for DNS**, as a stable address:

1. Add a subdomain **A record** pointing at the VPS's public IP, e.g. `fut.yourdomain.com  →  203.0.113.10`.
   (This is a plain DNS record in your DNS provider — it does not go through Traefik.)
2. Set `PUBLIC_HOST=fut.yourdomain.com`. The server advertises that name to clients and they resolve it to
   the IP, then connect to the six ports directly.
3. Open those six TCP ports on the VPS firewall **and** your cloud provider's security group:
   `42230, 10051, 17502, 42232, 8199, 8099`.
4. For **online matches**, also open the **UDP** match-relay range (default `45000-45063`): the server relays
   gameplay between two players who cannot connect directly through their home NATs.

### Deploy (Docker)

Put the settings in a `.env` file so every `docker compose` command (up, logs, restart) sees them — passing
them only on `up` makes later commands like `docker compose logs` fail with an interpolation error.

```sh
git clone https://github.com/floholz/FIFA-15-Local-FUT-hosted.git
cd FIFA-15-Local-FUT-hosted
# git-lfs is NOT required — the card DB is a normal file; only trophy art is LFS (optional, non-critical)
cp .env.example .env            # then edit .env: PUBLIC_HOST, SERVER_ACCESS_CODE
docker compose up -d --build
docker compose logs -f          # watch the banner + verbose protocol log
```

To update after new server code lands:

```sh
git pull && docker compose up -d --build   # .env supplies PUBLIC_HOST / SERVER_ACCESS_CODE
```

Server logs live inside the container at `/data/logs/localfut15-server.log` (the `fut15-data` volume); that
is the file to share when debugging a match session.

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
- **Remote diagnostics:** `GET /localfut/debug?code=<access code>&lines=200` returns the recent server log
  plus each club's coins/state as JSON (server mode only, gated by the access code). Add `&grep=<text>` to
  filter the log. Useful for debugging a session without shelling into the box.

## What works over the VPS today, and what doesn't yet

- **Works:** each friend gets their own persistent club on your server (own coins, squads, packs, offline
  Seasons), gated to your group.
- **In progress:** a **shared human transfer market** (buy/sell each other's cards) — the next feature.
- **Not yet:** live matches against each other. FIFA 15 matches are peer-to-peer UDP brokered by Blaze
  GameManager, and the client's online hub does not yet reach "ready" against this server (see
  `LINUX_PROTON_FINDINGS.md`). That needs GameManager + a UDP match relay on the VPS and is the long track.
