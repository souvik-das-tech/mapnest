# API guide

Everything MapNest does is available through its REST API (`/api/v1`). The Admin
UI is just one client of it. This guide shows how to use MapNest **without the UI**:
from scripts, CI pipelines, infrastructure-as-code or your own backend.

The complete machine-readable reference is the
[OpenAPI 3 spec](../server/msapi/internal/api/openapi.yaml), also served by a
running API at `GET /api/v1/docs`.

- [Run the API on its own](#run-the-api-on-its-own)
- [Authentication](#authentication)
- [Conventions](#conventions)
- [Workflow: from data to map in five calls](#workflow-from-data-to-map-in-five-calls)
- [Stores](#stores) · [Layers](#layers) · [Styles](#styles) · [Icons](#icons) ·
  [Workspaces](#workspaces) · [Mapfile versions](#mapfile-versions-and-rollback) ·
  [Backup](#backup-and-restore) · [Health](#health)
- [Serving the maps](#serving-the-maps)
- [Scripting tips](#scripting-tips)

## Run the API on its own

Only the `server/` stack is needed: MapServer and `msapi`.

```bash
docker network create gis-net
cd server && cp .env.example .env     # fill in the three secrets
docker compose up -d --build
```

By default the API listens on `127.0.0.1:8081`, reachable from the server host
only. To call it from elsewhere, either:

- put it behind your own HTTPS reverse proxy (recommended), or
- set `MSAPI_BIND=0.0.0.0` in `server/.env` and firewall port 8081 to the hosts
  that need it.

Never expose the API over plain HTTP on an untrusted network: the API key would
travel in clear text.

## Authentication

| Method | Header | For |
|---|---|---|
| **API key** | `X-API-Key: <MSAPI_API_KEY>` | Scripts and services. Never expires; rotate it by changing `.env`. |
| **Session token** | `Authorization: Bearer <token>` | Interactive clients (the Admin UI uses this). Expires after 8 h, or 30 min without activity. |

Get a session token:

```bash
curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"username": "admin", "password": "…"}'
# {"token":"…","username":"admin","expires_at":"2026-11-03T20:00:00Z","idle_timeout_seconds":1800}

curl -s -X POST $API/auth/logout -H "Authorization: Bearer $TOKEN"     # ends it at once
curl -s -X DELETE $API/auth/sessions -H "X-API-Key: $KEY"              # signs out every session
curl -s $API/auth/session -H "X-API-Key: $KEY"                         # who am I
```

Five wrong passwords for a username or from one IP lock sign-in for 15 minutes
(`429` with `Retry-After`). The API key is not affected, so automation keeps
working.

## Conventions

The examples use:

```bash
export API=http://localhost:8081/api/v1
export KEY=<your MSAPI_API_KEY>
export H="X-API-Key: $KEY"
export J='Content-Type: application/json'
```

- Request and response bodies are JSON unless stated otherwise. Unknown JSON
  fields are rejected (typos fail loudly).
- Errors always look like this:

  ```json
  { "error": "store \"city_db\" not found", "details": ["…"] }
  ```

- Status codes: `200`/`201`/`204` success, `400` malformed request, `401` not
  signed in, `404` not found, `409` name taken or still in use, `422` valid JSON but
  the change was refused (bad connection, invalid style, MapServer rejected the
  result), `429` too many sign-in attempts, `502` a store could not be reached.
- Names of stores, layers and styles: a letter, then letters, digits or `_`, up
  to 63 characters. Workspace names are lower case.
- **Every change is safe.** A change that would produce a mapfile MapServer
  rejects returns `422` and leaves the live mapfile untouched.

## Workflow: from data to map in five calls

```bash
# 1. Connect a PostGIS database. The connection is tested before saving.
curl -sH "$H" -H "$J" -X POST $API/stores -d '{
  "name": "city_db", "type": "postgis",
  "connection": {"host": "db.example.com", "port": 5432, "database": "gis",
                 "user": "map_reader", "password": "…", "sslmode": "require"}}'

# 2. See what can be published.
curl -sH "$H" $API/stores/city_db/resources

# 3. Publish a table. Geometry type, SRID and extent are detected.
curl -sH "$H" -H "$J" -X POST $API/layers -d '{
  "name": "schools", "title": "Schools", "store": "city_db", "table": "schools", "wfs_enabled": true}'

# 4. Style it (JSON shortcut here; SLD works the same way).
curl -sH "$H" -H "$J" -X POST "$API/styles?name=schools_by_type" -d '{
  "target": "vector",
  "rules": [
    {"name": "Primary",   "filter": "level = '\''primary'\''",   "mark": "circle", "fill": "#16a34a", "size": 9},
    {"name": "Secondary", "filter": "level = '\''secondary'\''", "mark": "square", "fill": "#2563eb", "size": 9},
    {"name": "Other", "else": true, "mark": "circle", "fill": "#94a3b8", "size": 6}
  ]}'

# 5. Assign the style.
curl -sH "$H" -H "$J" -X PUT $API/layers/schools/styles -d '{"default": "schools_by_type"}'
```

Check the result as a PNG without any map client:

```bash
curl -sH "$H" "$API/layers/schools/preview?width=800" -o schools.png
```

## Stores

A store is a connection to your data. MapNest only ever **reads** from it.

### PostGIS

```bash
curl -sH "$H" -H "$J" -X POST $API/stores -d '{
  "name": "city_db", "type": "postgis",
  "connection": {"host": "db", "port": 5432, "database": "gis", "user": "map_reader",
                 "password": "…", "schema": "public", "sslmode": "prefer"}}'
```

Create a read-only role for MapNest:

```sql
CREATE ROLE map_reader LOGIN PASSWORD '…';
GRANT CONNECT ON DATABASE gis TO map_reader;
GRANT USAGE ON SCHEMA public TO map_reader;
GRANT SELECT ON roads, schools TO map_reader;
```

MapNest additionally opens every session with
`default_transaction_read_only = on`.

### S3 and S3-compatible storage (AWS, MinIO, SeaweedFS, …)

```bash
curl -sH "$H" -H "$J" -X POST $API/stores -d '{
  "name": "imagery", "type": "s3",
  "connection": {"bucket": "rasters", "region": "us-east-1", "prefix": "ortho/",
                 "endpoint": "minio.example.com:9000", "use_https": true,
                 "access_key": "…", "secret_key": "…"}}'
```

Leave `endpoint` empty for AWS. Give the key read-only access to the bucket.

### Local raster folder

GeoTIFFs in `server/rasters/` (or `RASTER_DIR`) are mounted read-only into the
containers. A store points at that folder or a subfolder:

```bash
curl -sH "$H" -H "$J" -X POST $API/stores -d '{
  "name": "drone", "type": "local", "connection": {"path": "drone/2026"}}'
```

### Other store calls

| Call | Does |
|---|---|
| `POST /stores/test` | Test a connection body without saving it |
| `GET /stores` · `GET /stores/{name}` | List / read. Secrets are never returned, only `has_secret`. |
| `PUT /stores/{name}` | Update. Omit `password` / `secret_key` to keep the stored one. |
| `POST /stores/{name}/test` | Test a saved store |
| `GET /stores/{name}/resources` | Geometry tables (with type and row count) or `.tif` files |
| `PUT /stores/{name}/workspace` | Move the store and its layers: `{"workspace": "city"}` |
| `DELETE /stores/{name}` | Delete; `409` lists the layers still using it |

Secrets are stored encrypted (AES-256-GCM with `MSAPI_SECRET_KEY`).

## Layers

### Vector (PostGIS)

```bash
curl -sH "$H" -H "$J" -X POST $API/layers -d '{
  "name": "roads", "title": "Roads", "abstract": "Road network",
  "store": "city_db", "schema": "public", "table": "roads",
  "wfs_enabled": true, "min_scale": 1000, "max_scale": 500000}'
```

Optional fields for unusual tables:

| Field | When |
|---|---|
| `geom_column` | The table has more than one geometry column |
| `pk` | A view, or a table without a primary key: a unique column |
| `layer_type` | A generic `GEOMETRY` column: `POINT`, `LINE` or `POLYGON` |
| `srid` | The column has no SRID: declare it (like GeoServer's *Declared SRS*). `POST /layers/srid-candidates` suggests likely ones from the data. |

Dry-run first to see what would be detected:

```bash
curl -sH "$H" -H "$J" -X POST $API/layers/inspect -d '{"name": "x", "store": "city_db", "table": "roads"}'
# {"kind": "vector", "source": {…detected geometry, SRID, key…}, "extent": {…}, "warnings": […]}
```

### Raster (S3 or local GeoTIFF)

```bash
curl -sH "$H" -H "$J" -X POST $API/layers -d '{
  "name": "ortho_2026", "title": "Orthophoto 2026", "store": "drone", "path": "site-a/ortho.tif"}'
```

Cloud Optimized GeoTIFFs render fastest. For a plain GeoTIFF, MapNest builds a
COG copy in the background (`source.cache_status` shows progress;
`POST /layers/{name}/cache` retries).

### Other layer calls

| Call | Does |
|---|---|
| `GET /layers` · `GET /layers/{name}` | List / read, including `wms_url` and warnings |
| `PUT /layers/{name}` | Change `title`, `abstract`, `enabled`, `wfs_enabled`, `min_scale`, `max_scale` (`null` clears) |
| `POST /layers/{name}/refresh` | Re-read extent and metadata after the data changed |
| `GET /layers/{name}/attributes` | Columns of a vector layer |
| `GET /layers/{name}/attributes/{attr}?classes=5` | Value counts, min/max and quantile breaks, for classified styles |
| `GET /layers/{name}/preview?width=800&style=…&bbox=…&srs=…` | PNG as published |
| `POST /layers/{name}/preview` | PNG with an **unsaved** style in the body (SLD or JSON) |
| `GET` / `PUT /layers/{name}/styles` | Assigned styles: `{"default": "a", "alternates": ["b", "c"]}` |
| `DELETE /layers/{name}` | Unpublish (your data is untouched) |

## Styles

Upload existing GeoServer SLD files, or write the shorter JSON format. Both are
converted **once** into MapServer classes, so rendering has no per-request
styling cost. See the [styling guide](styling.md) for what is supported.

```bash
# SLD 1.0 / SE 1.1
curl -sH "$H" -X POST "$API/styles?name=roads_by_class" \
  -H 'Content-Type: application/vnd.ogc.sld+xml' --data-binary @roads.sld
# {"name":"roads_by_class","format":"sld","target":"vector","warnings":[],"used_by":[]}

# Validate without saving, compiled for a specific layer's geometry
curl -sH "$H" -X POST "$API/styles/validate?layer=roads" \
  -H 'Content-Type: application/vnd.ogc.sld+xml' --data-binary @roads.sld
```

| Call | Does |
|---|---|
| `GET /styles` | List with `warnings` and `used_by` |
| `GET /styles/{name}` | The original SLD / JSON, byte for byte (`?meta=1` for the JSON description) |
| `PUT /styles/{name}` | Replace the body; layers using it update at once |
| `GET /styles/{name}/compiled` | The generated MapServer `CLASS` blocks |
| `GET /styles/{name}/legend?layer=…` | Legend PNG |
| `DELETE /styles/{name}` | Layers fall back to their next style or the built-in default |

Unsupported SLD parts are listed in `warnings`, never silently dropped. A style
that would render nothing is refused with `422`.

## Icons

```bash
curl -sH "$H" -X POST $API/icons -F file=@school.png          # PNG, ≤ 2 MB, ≤ 512×512
curl -sH "$H" $API/icons                                      # with used_by
curl -sH "$H" -X DELETE $API/icons/school.png
```

Reference an icon from SLD by file name
(`<OnlineResource xlink:href="school.png"/>`, `icons/school.png` or any URL
ending in `/icons/school.png`), or from JSON with `"icon": "school.png"`.

## Workspaces

Group stores per project or client. Each workspace gets its own WMS/WFS endpoint
with only its layers; the shared endpoint keeps serving everything.

```bash
curl -sH "$H" -H "$J" -X POST $API/workspaces -d '{"name": "city", "title": "City council"}'
curl -sH "$H" -H "$J" -X PUT $API/stores/city_db/workspace -d '{"workspace": "city"}'
curl -sH "$H" $API/workspaces          # counts and each workspace's ows_url
```

## Mapfile versions and rollback

Every change regenerates the MapServer mapfile, validates it and keeps the last
50 versions.

```bash
curl -sH "$H" $API/mapfile                       # current mapfile (secrets masked)
curl -sH "$H" $API/mapfile/versions              # history, newest first
curl -sH "$H" -X POST $API/mapfile/rollback/42   # emergency switch to an old version
curl -sH "$H" -X POST $API/mapfile/rebuild       # regenerate from the database
```

The database is the source of truth: after a rollback, the next change
regenerates the mapfile from it.

## Backup and restore

```bash
curl -sH "$H" $API/system/backup -o mapnest-backup.tar.gz
curl -sH "$H" -X POST $API/system/restore \
  -H 'Content-Type: application/gzip' --data-binary @mapnest-backup.tar.gz
```

The archive holds the configuration database and icons. Store secrets inside are
encrypted with `MSAPI_SECRET_KEY`: **keep the key with your backups, not inside
them.**

## Health

```bash
curl -s $API/health                      # anonymous: {"status":"ok","checks":{...}}
curl -sH "$H" $API/health                # plus version, details and each store's reachability
```

`200` when the API, its database and MapServer are fine, `503` otherwise: suitable
for load balancers and uptime monitors.

## Serving the maps

MapServer serves the published layers as standard OGC services:

| Endpoint | Content |
|---|---|
| `http://<host>:8080/` | WMS and WFS, every layer |
| `http://<host>:8080/ows/<workspace>` | Only that workspace's layers |
| `http://<host>:3000/ows` | Same, through the UI's nginx (rate limited) |

```
?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities
?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=roads&STYLES=roads_by_class&CRS=EPSG:3857&BBOX=…&WIDTH=512&HEIGHT=512&FORMAT=image/png
?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=schools&OUTPUTFORMAT=geojson&COUNT=100
```

WFS is off per layer until you set `"wfs_enabled": true`, and returns at most
10 000 features per request.

## Scripting tips

- **Idempotent setup.** `GET` first and `POST` only on `404`, or `DELETE` then
  `POST`. Names are stable identifiers, so they work well as keys in scripts and
  infrastructure-as-code.
- **Fail fast in CI.** Every call returns a meaningful status; use `curl -f` (or
  check the status) so a `422` stops the pipeline with the API's explanation in
  `details`.
- **Dry runs.** `POST /stores/test`, `POST /layers/inspect` and
  `POST /styles/validate` check everything without changing anything.
- **Keep styles in git.** Store your SLD / JSON files next to your code and
  `PUT /styles/{name}` them on deploy; `GET /styles/{name}` returns the original
  byte for byte.
- **Generate a client.** Feed the [OpenAPI spec](../server/msapi/internal/api/openapi.yaml)
  to `openapi-generator` or similar for a typed client in your language.
