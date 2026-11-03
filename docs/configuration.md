# Configuration

Both stacks are configured with environment variables in a `.env` file next to
their `docker-compose.yml`. Start from the `.env.example` in each folder. After a
change, run `docker compose up -d` in that folder.

## `server/.env`: API and MapServer

### Required secrets

msapi refuses to start, and says why in `docker logs msapi`, when one of these is
missing or weak.

| Variable | Rule | Generate |
|---|---|---|
| `MSAPI_ADMIN_PASSWORD` | ≥ 12 characters, not a known default, not the username | your own, or `openssl rand -base64 18` |
| `MSAPI_API_KEY` | ≥ 32 characters, not a placeholder | `openssl rand -hex 32` |
| `MSAPI_SECRET_KEY` | exactly 32 bytes, base64 | `openssl rand -base64 32` |

`MSAPI_SECRET_KEY` encrypts store passwords in the database and signs sessions.
If you lose it you have to re-enter store passwords; keep it with your backups,
**not inside** them. Changing `MSAPI_ADMIN_PASSWORD` signs every session out.

### Sign-in and sessions

| Variable | Default | Meaning |
|---|---|---|
| `MSAPI_ADMIN_USER` | `admin` | Admin UI username |
| `MSAPI_SESSION_HOURS` | `8` | Absolute session lifetime (1–720) |
| `MSAPI_SESSION_IDLE_MINUTES` | `30` | Sign out after this much inactivity (1–1440) |
| `MSAPI_LOGIN_MAX_ATTEMPTS` | `5` | Failed sign-ins per username or client IP before a lockout |
| `MSAPI_LOGIN_LOCKOUT_MINUTES` | `15` | Lockout length |

### Network

| Variable | Default | Meaning |
|---|---|---|
| `MAPSERVER_PORT` | `8080` | Host port for WMS/WFS |
| `MAPSERVER_BIND` | `0.0.0.0` | Interface for WMS/WFS. `127.0.0.1` = maps only through the UI's nginx (rate limited) |
| `MSAPI_PORT` | `8081` | Host port for the API |
| `MSAPI_BIND` | `127.0.0.1` | Interface for the API. `0.0.0.0` only when the UI or your scripts run on another host; firewall it |
| `MSAPI_TRUSTED_PROXIES` | loopback + private ranges | CIDRs allowed to report the client IP (`X-Real-IP`, `X-Forwarded-For`), or `none`. Narrow it to your proxy when the API port is reachable from untrusted private networks |
| `MSAPI_PUBLIC_URL` | `http://localhost:8080/` | Public MapServer URL written into WMS capabilities. Set it to the address clients use, e.g. `https://maps.example.com/ows` |

### Data and behaviour

| Variable | Default | Meaning |
|---|---|---|
| `RASTER_DIR` | `./rasters` | Host folder with GeoTIFFs for local-folder stores, mounted read-only |
| `MAPSERVER_TIMEOUT` | `180` | Seconds one map request may take |
| `MSAPI_PUBLIC_DOCS` | `off` | Serve `/api/v1/docs` and `/api/v1/swagger` without sign-in |
| `MSAPI_INSECURE_DEV` | `false` | Accept weak secrets. **Local development only** |
| `MSAPI_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

### Advanced (set inside the container; rarely changed)

| Variable | Default | Meaning |
|---|---|---|
| `MSAPI_VALIDATE` | `on` | Validate each generated mapfile with `mapserv` before it goes live |
| `MSAPI_MAPSERVER_GID` | `33` | Group of MapServer's worker; `pg_service.conf` is made readable for it |
| `MSAPI_LISTEN` | `:8081` | Listen address inside the container |
| `MSAPI_DATA_DIR`, `MSAPI_MAPFILE_DIR`, `MSAPI_RASTER_DIR`, `MSAPI_RASTER_CACHE_DIR` | set by compose | Container paths of the volumes |
| `MSAPI_MAPSERVER_URL` | `http://mapserver/` | Internal MapServer URL for health checks and previews |
| `MSAPI_FONT_DIR`, `MSAPI_PROJ_DB`, `MSAPI_VALIDATE_CONFIG` | image paths | Fonts, PROJ database, validator config |

## `web/.env`: Admin UI

| Variable | Default | Meaning |
|---|---|---|
| `WEB_PORT` | `3000` | Host port of the UI |
| `WEB_BIND` | `0.0.0.0` | Interface. `127.0.0.1` when a reverse proxy on the same host is the only way in |
| `MSAPI_UPSTREAM` | `http://msapi:8081` | Where nginx sends `/api` |
| `MAPSERVER_UPSTREAM` | `http://mapserver:80` | Where nginx sends `/ows` |
| `NGINX_REAL_IP_FROM` | `127.0.0.1/32` | Proxy allowed to set the client IP for nginx |

### HTTPS overlay (`docker-compose.tls.yml`)

| Variable | Default | Meaning |
|---|---|---|
| `MAPNEST_DOMAIN` | required | Domain for the certificate, e.g. `maps.example.com` |
| `MAPNEST_ACME_EMAIL` | required | Contact address for Let's Encrypt |
| `NGINX_REAL_IP_FROM_TLS` | `172.16.0.0/12` | Docker network range of the Caddy container |

## Built-in limits

| Limit | Value | Where |
|---|---|---|
| GetMap image size | 4096 × 4096 px | mapfile `MAXSIZE` |
| WFS features per request | 10 000 | mapfile `wfs_maxfeatures` |
| JSON request body | 1 MB | API |
| Icon | 2 MB, 512 × 512 px, PNG | API |
| Backup restore | 512 MB | API and nginx |
| Upload through nginx | 20 MB | nginx |
| Rate limits per IP (nginx) | sign-in 10/min, API 30/s, WMS/WFS 60/s, 100 connections | nginx |
| Mapfile history | last 50 versions | API |
