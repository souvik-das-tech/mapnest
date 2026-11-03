# Deployment

MapNest runs entirely in Docker. Pick the layout that fits you; all of them use
the same images.

| Layout | Stacks | When |
|---|---|---|
| [API only](#api-only) | `server/` | Automation, embedding in your own product |
| [Single host](#single-host-with-the-admin-ui) | `server/` + `web/` | Most installations |
| [Separate hosts](#separate-hosts) | `server/` on one, `web/` on another | The UI lives in an admin network |

Requirements: Docker Engine 24+ with Compose v2.24+, about 300 MB of RAM, and
network access from the server to your PostGIS / S3.

## API only

```bash
docker network create gis-net
cd server
cp .env.example .env          # fill in MSAPI_ADMIN_PASSWORD, MSAPI_API_KEY, MSAPI_SECRET_KEY
docker compose up -d --build
```

- WMS/WFS: `http://<host>:8080/`
- API: `http://127.0.0.1:8081/api/v1` on the host. For remote scripts, put an
  HTTPS reverse proxy in front of 8081, or set `MSAPI_BIND=0.0.0.0` and firewall
  the port.

Then follow the [API guide](api-guide.md).

## Single host with the Admin UI

```bash
docker network create gis-net

cd server && cp .env.example .env    # fill in the secrets
docker compose up -d --build

cd ../web && cp .env.example .env
docker compose up -d --build
```

Open `http://<host>:3000`. Both stacks join the `gis-net` network and find each
other by name.

### HTTPS

```bash
cd web
# in web/.env:
#   MAPNEST_DOMAIN=maps.example.com
#   MAPNEST_ACME_EMAIL=you@example.com
docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d --build
```

Caddy obtains and renews a Let's Encrypt certificate, redirects HTTP to HTTPS and
allows only TLS 1.2 and 1.3. The plain UI port is then bound to `127.0.0.1`.
The domain must resolve to the host and ports 80 and 443 must be reachable.

Also set `MSAPI_PUBLIC_URL=https://maps.example.com/ows` in `server/.env`, so
WMS capabilities advertise the public address, and consider
`MAPSERVER_BIND=127.0.0.1` so that all map traffic goes through HTTPS and nginx's
rate limits.

Already have a reverse proxy (Traefik, nginx, a cloud load balancer)? Point it
at the UI port, set `WEB_BIND=127.0.0.1` if it runs on the same host, and set
`NGINX_REAL_IP_FROM` to its address so client IPs are logged correctly.

## Separate hosts

**Server host**

```bash
cd server && cp .env.example .env
# MSAPI_BIND=0.0.0.0
# MSAPI_TRUSTED_PROXIES=<ui-host-ip>/32
docker compose up -d --build
```

Firewall port 8081 to the UI host only. Expose 8080 to the clients that need
WMS/WFS.

**UI host**

```bash
cd web && cp .env.example .env
# MSAPI_UPSTREAM=http://<server-host>:8081
# MAPSERVER_UPSTREAM=http://<server-host>:8080
```

Remove the external `gis-net` network from `web/docker-compose.yml` (it is only
needed on a shared host), then `docker compose up -d --build`. Put the link
between the hosts on a private network or VPN: the API traffic between nginx and
msapi is plain HTTP.

## Upgrades

```bash
git pull
cd server && docker compose up -d --build --pull always
cd ../web && docker compose up -d --build --pull always
```

The database schema migrates automatically at start-up, and the mapfiles are
regenerated. Read [CHANGELOG.md](../CHANGELOG.md) for upgrade notes first, and
take a backup.

## Backups

The whole installation is `server/data/` (database) plus
`server/mapfiles/icons/`. Either:

- **API / UI**: `GET /api/v1/system/backup` (or *System → Backup*) gives a
  consistent `.tar.gz`; restore with `POST /api/v1/system/restore`.
- **Files**: copy both folders. SQLite runs in WAL mode, so prefer the API
  backup for a consistent copy while the stack runs.

Store passwords in the backup are encrypted with `MSAPI_SECRET_KEY`. Keep the key
in your password manager or secret store, separate from the archives.

A nightly backup with cron:

```bash
0 2 * * * curl -sf -H "X-API-Key: $KEY" http://127.0.0.1:8081/api/v1/system/backup \
  -o /backups/mapnest-$(date +\%F).tar.gz
```

## Monitoring

- **Health:** `GET /api/v1/health` returns `200` or `503`; Docker health checks
  are built into the msapi and web images.
- **Logs:** `docker logs msapi` is JSON, one line per request with actor and
  client IP. Security events and changes carry `"audit":true`:

  ```bash
  docker logs msapi 2>&1 | grep '"audit":true'
  ```

  Logs rotate at 10 MB × 5 files per container.

## Production checklist

See [SECURITY.md](../SECURITY.md#production-checklist).
