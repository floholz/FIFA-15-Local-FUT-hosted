# OUFT accounts service

Invite-only accounts for OUFT, separate from the game server (see `docs/ROADMAP.md`, Architecture and
Phase 4). Code: `cmd/ouft-accounts` (entry point), `internal/accounts` (the only package that imports
PocketBase). PocketBase is pinned in `go.mod`.

## What it does

| Path | Who | What |
|---|---|---|
| `/` | everyone / players | landing page; after login: download of the newest OUFT Online, admin link |
| `GET /api/ouft/launcher` | logged-in players | newest `launcher-*` GitHub release (version, zip URL, size, SHA-256), cached 10 min (`OUFT_RELEASES_REPO`) |
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
#   FUT15_STATE_DIR=/home/<you>/.local/share/fifa15-local-fut-server   (keeps existing clubs)
#   SERVER_ACCESS_CODE=<old access code, for launchers before 15.1>
docker compose up -d --build
docker compose exec accounts ouft-accounts superuser upsert <email> <password>
```

Then open `https://ouft.fhun.site/_/` and log in. The stack brings its own Traefik, bound only to
`OUFT_PUBLIC_IP`, so another proxy on the same host (e.g. a tailnet-only Traefik on the Tailscale IP) keeps
its ports. The certificate comes from Let's Encrypt (TLS challenge on port 443); the firewall needs TCP 80
and 443 open. The game server (`fut15`) runs with host networking (its game ports and the UDP relay must
see players' real addresses) as the owner of `FUT15_STATE_DIR` (`FUT15_UID`/`FUT15_GID`, default 1000). It
refuses to start if `PUBLIC_HOST` does not resolve.

### Moving the game server from the systemd unit to compose

Once, when nobody is playing (clubs and identities stay, they are in the state directory):

```sh
sudo tools/linux/install-service.sh --uninstall     # stops and removes fut15-server.service
# .env: FUT15_STATE_DIR=<the old state dir>, SERVER_ACCESS_CODE=$(cat ~/.fut15-access-code)
docker compose up -d --build
docker compose ps                                    # fut15, accounts, traefik: running/healthy
curl -s http://fut15.fhun.site:8199/localfut/status # "accounts": true
```

Logs: `docker compose logs -f fut15` and `$FUT15_STATE_DIR/logs/localfut15-server.log`. Once every player
uses OUFT Online 15.1+, set `FUT15_REQUIRE_ACCOUNT=1` in `.env` and `docker compose up -d`.

Update: `git pull && docker compose up -d --build`. Data lives in the `ouft_accounts-data` volume.

## Backups and restore

The compose service `backup` makes a daily archive (03:00 UTC) of everything that holds player data:
the game server's `users.sqlite3`, the club databases (`users/user-*.sqlite3`) and the accounts database
(users, invites). Logs are left out. Copies use SQLite's online backup API (safe while the servers run,
live databases opened read-only) and each one gets an integrity check. Archives:
`$OUFT_BACKUP_DIR/ouft-YYYYMMDD-HHMMSS.tar.gz` (default `./backups`), owned by `FUT15_UID`; the newest
`BACKUP_KEEP` (14) are kept. Settings in `.env`: `OUFT_BACKUP_DIR`, `BACKUP_HOUR`, `BACKUP_KEEP`,
`BACKUP_ON_START`.

```sh
docker compose run --rm backup --once     # back up now
docker compose logs backup                # what it did
```

The archives stay on the VPS: they protect against mistakes and broken databases, not against losing the
VPS. Copy them elsewhere now and then (e.g. `rsync` over Tailscale to another machine).

Restore (when nobody is playing):

```sh
A=backups/ouft-YYYYMMDD-HHMMSS.tar.gz; N=$(basename $A .tar.gz); STATE=<FUT15_STATE_DIR>
docker compose stop fut15 accounts
mkdir -p /tmp/ouft-restore && tar -xzf $A -C /tmp/ouft-restore
# game server: replace the databases (and drop their WAL files)
for f in /tmp/ouft-restore/$N/fut15/*.sqlite3; do b=$(basename $f); rm -f $STATE/$b-wal $STATE/$b-shm; cp $f $STATE/; done
mkdir -p $STATE/users && for f in /tmp/ouft-restore/$N/fut15/users/*.sqlite3; do b=$(basename $f); rm -f $STATE/users/$b-wal $STATE/users/$b-shm; cp $f $STATE/users/; done
# accounts: copy data.db into its volume
docker compose run --rm --no-deps -v /tmp/ouft-restore/$N/accounts:/restore:ro --entrypoint sh backup -c \
  'rm -f /src/accounts/data.db-wal /src/accounts/data.db-shm && cp /restore/data.db /src/accounts/data.db && chown 10001:10001 /src/accounts/data.db'
docker compose up -d
```

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
