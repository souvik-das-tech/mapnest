# Security Policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Report them privately through GitHub: **Security → Report a vulnerability**
on this repository (GitHub private vulnerability reporting). Include:

- the affected version or commit,
- steps to reproduce (requests, payloads, configuration),
- the impact you observed.

You can expect an acknowledgement within 5 working days and a fix or
mitigation plan within 30 days for confirmed issues. Credit is given in the
release notes unless you ask otherwise.

Vulnerabilities in **MapServer**, **GDAL** or **PostGIS** themselves should go
to those projects ([MapServer](https://github.com/MapServer/MapServer/security),
[GDAL](https://github.com/OSGeo/gdal/security)). MapNest only generates
MapServer's configuration.

## Supported versions

| Version | Supported |
|---|---|
| `main` / latest release | Yes |
| Older releases | No; please upgrade |

## Security model in short

MapNest has two kinds of traffic:

| Surface | Who uses it | Protection |
|---|---|---|
| **Admin UI and API** (`/api/v1`, port 8081, UI on 3000) | One administrator, and scripts | Sign-in with lockout, server-side sessions with idle timeout, API key for scripts, audit log |
| **WMS / WFS** (`/ows`, port 8080) | Map clients (OpenLayers, Leaflet, QGIS) | Public and read-only by design, like a typical GeoServer setup; rate limited through nginx |

Your data stays read-only: the API opens every PostGIS session with
`default_transaction_read_only = on`, raster folders are mounted read-only, and
you are asked to connect with a `SELECT`-only role.

The full list of controls, mapped to a VAPT Level 1 checklist with commands to
verify each one, is in [docs/security/VAPT-L1.md](docs/security/VAPT-L1.md).

## Production checklist

Before exposing MapNest to a network you do not fully trust:

1. **Secrets.** Set `MSAPI_ADMIN_PASSWORD` (12+ characters), `MSAPI_API_KEY`
   (`openssl rand -hex 32`) and `MSAPI_SECRET_KEY` (`openssl rand -base64 32`).
   msapi refuses to start with missing, short or known default values.
2. **HTTPS.** Run the web UI behind TLS: `docker compose -f docker-compose.yml
   -f docker-compose.tls.yml up -d` in `web/` (automatic Let's Encrypt), or your
   own reverse proxy. HSTS is sent automatically over HTTPS.
3. **Ports.** Keep the API (8081) bound to `127.0.0.1` (default). Publish only
   the web UI (or the TLS proxy) and, if apps need it directly, MapServer (8080).
   Set `MAPSERVER_BIND=127.0.0.1` to force map traffic through nginx's rate limits.
4. **Database role.** Connect stores with a read-only role (see README,
   "Giving us database access").
5. **Keep `MSAPI_INSECURE_DEV=false`** and `MSAPI_PUBLIC_DOCS=off`.
6. **Backups.** Store `MSAPI_SECRET_KEY` apart from the backup archives.
7. **Updates.** Rebuild images regularly (`docker compose build --pull`) to
   pick up MapServer, GDAL, nginx and OS fixes.
8. **Logs.** Ship `docker logs msapi` to your log system; security events carry
   `"audit":true`.

## Known limitations

These are deliberate trade-offs for a lightweight single-admin tool. They are
listed so that reviewers do not have to rediscover them:

- **One administrator account.** There are no per-user roles. Every signed-in
  user and every API-key holder has full admin rights.
- **WMS/WFS are public.** There is no per-layer access control. Publish only
  data that may be public, or put an authenticating proxy in front of `/ows`.
- **Lockout can be triggered by others.** Repeated wrong passwords for `admin`
  lock that username for `MSAPI_LOGIN_LOCKOUT_MINUTES`, also for the real
  administrator. The API key keeps working, and the lockout ends by itself.
- **Rate-limit state is in memory** and resets when msapi restarts.
- **Store connections are made from the server.** An administrator can point a
  store at any host the server can reach (by design, like GeoServer). Treat
  admin access as trusted.
- **The session token is kept in `sessionStorage`**, readable by scripts on the
  page. A strict Content-Security-Policy (no inline or third-party scripts)
  is the mitigation; the token is never in a cookie, so CSRF does not apply.
- **`pg_service.conf`** on the shared mapfile volume holds store passwords in
  clear text, because MapServer must read them. It is mode `0640`, readable only
  by MapServer's group; protect the host volume accordingly.
