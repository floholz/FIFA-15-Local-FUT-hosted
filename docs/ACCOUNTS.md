# OUFT accounts service

Invite-only accounts for OUFT, separate from the game server (see `docs/ROADMAP.md`, Architecture and
Phase 4). Code: `cmd/ouft-accounts` (entry point), `internal/accounts` (the only package that imports
PocketBase). PocketBase is pinned in `go.mod`.

## What it does

| Path | Who | What |
|---|---|---|
| `/` | everyone | short landing page |
| `/register?invite=CODE` | invited players | create an account (username + password) |
| `POST /api/ouft/register` | the register page | `{invite, username, password, passwordConfirm}` |
| `GET /api/ouft/invite/CODE` | the register page | is this invite still usable? |
| `POST /api/collections/users/auth-with-password` | launcher | PocketBase login: `{identity: username, password}` → token (valid 30 days) |
| `POST /api/collections/users/auth-refresh` | launcher | swap a valid token for a fresh one |
| `POST /api/ouft/internal/verify` | game servers only | `Authorization: Bearer <service key>`, `{token}` → `{valid, id, username}` |
| `/_/` | admins | PocketBase dashboard (users, invites, logs, settings) |

Public signup through PocketBase's own API is off; accounts only come from invites. Usernames are 2–20
characters (letters, digits, `_ . -`), unique regardless of case. Rate limits are on (registration: 5 per
minute per IP).

The internal API is never routed through the public proxy: game servers reach it on the compose network
(`http://accounts:8090`) or, for the host-network game server, on `http://127.0.0.1:8095`.

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
