# OUFT accounts service

Invite-only accounts for OUFT, separate from the game server (see `docs/ROADMAP.md`, Architecture and
Phase 4). Code: `cmd/ouft-accounts` (entry point), `internal/accounts` (the only package that imports
PocketBase). PocketBase is pinned in `go.mod`.

## What it does

| Path | Who | What |
|---|---|---|
| `/` | everyone | short landing page |
| `/register?invite=CODE` | invited players | create an account (username + password) |
| `/admin` | users with role `admin` | log in, create invite links (copy button), see and revoke invites |
| `POST /api/ouft/register` | the register page | `{invite, username, password, passwordConfirm}` |
| `GET /api/ouft/invite/CODE` | the register page | is this invite still usable? |
| `POST /api/collections/users/auth-with-password` | launcher | PocketBase login: `{identity: username, password}` → token (valid 30 days) |
| `POST /api/collections/users/auth-refresh` | launcher | swap a valid token for a fresh one |
| `POST /api/ouft/internal/verify` | game servers only | `Authorization: Bearer <service key>`, `{token}` → `{valid, id, username}` |
| `/_/` | superusers | PocketBase dashboard (users, roles, invites, logs, settings) |

Public signup through PocketBase's own API is off; accounts only come from invites. Users have a `role`
(empty/`player`, or `admin`), set by a superuser in the dashboard; players cannot change their own role or
username. Admin users manage invites through the normal records API (the `/admin` page): the server picks
the code, records who created it, and caps it at 20 players and 30 days. Usernames are 2–20
characters (letters, digits, `_ . -`), unique regardless of case. Rate limits are on (registration: 5 per
minute per IP).

The internal API is never routed through the public proxy: game servers reach it on the compose network
(`http://accounts:8090`) or, for the host-network game server, on `http://127.0.0.1:8095`.

## How a game server uses it

The launcher (OUFT Online 15.1+) logs in at the accounts service and keeps the token (never the password).
On Play it refreshes the token and sends it to the game server's `/localfut/register`. The game server
asks `POST /api/ouft/internal/verify` who it is and picks the club by the account id (`users.pb_id`):

- **first account login on a PC that already has a club** (the launcher also sends the old name + per-PC
  secret): that club is linked to the account, so nobody loses their club;
- otherwise a new club, named after the OUFT username (or `name-2` if an old club already has the name);
- later logins, from any PC, find the club by the account.

Game server settings: `FUT15_ACCOUNTS_URL` (e.g. `http://127.0.0.1:8095`) and `FUT15_ACCOUNTS_KEY` (its
service key; the `OUFT_SERVICE_KEYS` list works as is, the server picks its `fut15:` entry). The compose
file passes both. `/localfut/status` then reports `"accounts": true` and the launcher shows the account
login. The old name + secret + access code login keeps working for older launchers until
`FUT15_REQUIRE_ACCOUNT=1`.

## Run it (VPS)

Everything is in `docker-compose.yml`; settings in `.env` (copy `.env.example`):

```sh
cp .env.example .env            # then edit:
#   OUFT_PUBLIC_IP=217.160.125.159
#   OUFT_DOMAIN=ouft.fhun.site
#   OUFT_ACME_EMAIL=<your email>
#   OUFT_SERVICE_KEYS=fut15:<output of: openssl rand -hex 32>
#   PUBLIC_HOST=fut15.fhun.site
docker compose up -d --build
docker compose exec accounts ouft-accounts superuser upsert <email> <password>
```

Then open `https://ouft.fhun.site/_/` and log in. The stack brings its own Traefik, bound only to
`OUFT_PUBLIC_IP`, so another proxy on the same host (e.g. a tailnet-only Traefik on the Tailscale IP) keeps
its ports. The certificate comes from Let's Encrypt (TLS challenge on port 443); the firewall needs TCP 80
and 443 open. The game server (`fut15`) is in the same file under the `game` profile and is not started yet:
it still runs as the `fut15-server` systemd unit until the switch-over.

Update: `git pull && docker compose up -d --build`. Data lives in the `ouft_accounts-data` volume.

## Admin

Give trusted players the `admin` role (dashboard → `users` → `role`); they can then create and share
invite links at `https://ouft.fhun.site/admin` with their normal OUFT login. As superuser you can also use
the CLI:

```sh
docker compose exec accounts ouft-accounts invite --uses 1 --days 7 --note "for Markus"
# -> https://ouft.fhun.site/register?invite=...   (send this link)
```

Invites can also be created in the dashboard (collection `invites`): leave the code empty and it gets a
random one, one use and a 7-day expiry. The register link is `https://ouft.fhun.site/register?invite=<code>`.
Ban or remove a player, or reset a password: collection `users` in the dashboard.

## Develop

```sh
go test ./internal/accounts/
go run ./cmd/ouft-accounts serve --http 127.0.0.1:8090 --dir /tmp/ouft-pb   # local, no proxy
go run ./cmd/ouft-accounts invite --dir /tmp/ouft-pb
```
