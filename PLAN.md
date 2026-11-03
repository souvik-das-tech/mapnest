# MapNest (MapServer admin REST API) — Plan

Goal: replace GeoServer with MapServer for basic GIS projects, keeping the same
workflow we used with GeoServer's REST API, while keeping MapServer's advantage:
**fast and low on resources**.

Status (2026-10-14): all phases are coded but **nothing has been compiled or
run yet** (Docker isn't installed yet). Everything runs in Docker; nothing is
built or tested on the host. Items marked **[verify]** are assumptions to
confirm once Docker is up. See §15 for what is done and the first-run checklist.

---

## 1. What we did in GeoServer, and how it maps to MapServer

We never used GeoServer workspaces. Our flow was always:

| Step | GeoServer (what we did) | MapServer (how this API will do it) |
|---|---|---|
| 1 | REST: create a **store** (PostGIS for vector, S3 COG for raster) | `POST /api/v1/stores`. Saved in the config DB. A PostGIS store becomes a libpq service entry; an S3 store becomes GDAL S3 config. |
| 2 | REST: **publish a layer** from the store | `POST /api/v1/layers`. API inspects the table / COG (geometry type, SRID, extent, bands) and writes a `LAYER` block into the generated mapfile. |
| 3 | REST: upload an **SLD style** and set it on the layer | `POST /api/v1/styles` (SLD body). API converts SLD → mapfile `CLASS`/`STYLE`/`LABEL` blocks once, at upload time. `PUT /api/v1/layers/{name}/styles` assigns it. |
| 4 | Upload **icons** used by the SLD | `POST /api/v1/icons`. Icons are stored as files; SLD `ExternalGraphic` references are resolved to mapfile `SYMBOL TYPE PIXMAP`. |
| 5 | Web app reads WMS/WFS from GeoServer | Web app reads WMS/WFS from MapServer. Same OGC requests; `STYLES=` picks alternate styles. |

Originally there were no workspaces: one flat namespace for stores, layers,
styles and icons, and one MapServer service endpoint. Since 2026-10-18 stores
can be grouped into GeoServer-style workspaces (migration 0003): each
workspace gets its own mapfile `ws_<name>.map`, served at `/ows/<name>`, while
`/ows` still serves every layer. Names stay globally unique; styles and icons
are shared. See README "Workspaces".

**The package brings no database of its own for user data.** Users connect their
own PostGIS (and their own S3) as stores. We **only read** from those databases:
no tables, schemas, extensions or rows are ever created or changed in them. Our
own configuration lives in an embedded SQLite file inside our volume (§4).

---

## 2. Tech stack (chosen for low resource use)

| Part | Choice | Why |
|---|---|---|
| Language | **Go** | Compiles to one static binary. Idle RAM is roughly 10–30 MB, starts in milliseconds, no runtime to install. Closest to MapServer's "small and fast" profile. |
| HTTP router | **chi** (or Go 1.22+ standard `net/http` routing) | Thin layer over the standard library, no framework overhead. |
| Config DB | **SQLite**, embedded in the Go binary via **modernc.org/sqlite** (pure Go, no cgo). One file `data/msadmin.db` in our volume, WAL mode. | No database server to run, no RAM when idle, nothing to install. Users' Postgres is never used for our config. |
| DB migrations | Plain SQL files embedded in the binary (`embed`), applied at startup, version tracked with SQLite `PRAGMA user_version` | No migration tool to ship. |
| User PostGIS access | **pgx v5**, read-only queries only; small pool per store (max 2 connections), opened on demand and closed when idle | Used only to test connections and inspect tables when publishing. MapServer opens its own connections for rendering. |
| S3 access | **minio-go** | Small client, works with AWS S3 and MinIO alike. Used only for listing objects in a store. |
| COG inspection | `gdalinfo -json` from the MapServer image | GDAL is already in the MapServer image, so no GDAL bindings in Go and no cgo. |
| Mapfile generation | Go `text/template` | Mapfile is plain text; templates are enough and easy to read. |
| Mapfile validation / preview | `map2img` (MapServer CLI) **[verify name/flags in 8.6]** | Uses MapServer's own parser, so "valid" means MapServer will actually load it. |
| SLD parsing | Go `encoding/xml` + our own SLD → mapfile converter | We control exactly which SLD features we support and how icons resolve. See §6. |
| Secrets encryption | Go `crypto/aes` (AES-GCM), key from env | Store passwords and S3 keys are encrypted at rest in the SQLite file. |
| Logging | Go `log/slog` | Standard library, structured JSON logs. |
| API docs | Hand-written `openapi.yaml` served at `/api/v1/docs` | No code generator in the runtime. |

**Container image:** the API image is built `FROM` the same MapServer image that
serves maps, with the Go binary copied in. Both containers share the image layers
on disk, so the API adds only the binary size. This also guarantees the API
validates mapfiles with the *same* MapServer and GDAL versions that render them.

Alternatives considered:
- **Python (FastAPI + mappyfile + MapScript):** best GIS libraries, but the image with
  GDAL/MapScript is large and RAM is several times higher than Go. Rejected for the
  "lightweight" goal.
- **Rust:** even lighter than Go, but slower to build features in. Not worth it for a
  CRUD + file generation service.
- **Node.js:** lighter than Python, heavier than Go, no advantage for this job.

---

## 3. Architecture

The repo has **three independent compose stacks**. Each has its own
`docker-compose.yml` and can be started, stopped and updated on its own.
**Only stacks 1 and 3 are the product.** Stack 2 exists only so we can test
without a customer's database and bucket.

```
 ┌─ web/  (stack 3, product) ────┐
 │ web  nginx:alpine  :3000      │
 │  /        React build (static)│
 │  /api/ ─────────────────────────────┐
 │  /ows  ───────────────────────────┐ │
 └───────────────────────────────┘   │ │
                                     │ │
 ┌─ server/  (stack 1, product) ─────┼─┼─────────────────────────────┐
 │                                   ▼ ▼                             │
 │  mapserver :8080  ◄── reads ── ./mapfiles ◄── writes ── msapi :8081 (Go)
 │     │  (WMS/WFS)                (volume)                  │  /api/v1/...
 │     │                                         ./data/msadmin.db (SQLite)
 └─────┼─────────────────────────────────────────────────────┼─────────┘
       │ render: vector + COG                                │ read-only: test + inspect
       ▼                                                     ▼
 ┌─ User's own infrastructure (not ours) ──────────────────────────────┐
 │  their PostGIS          their S3 / MinIO bucket                     │
 └─────────────────────────────────────────────────────────────────────┘
       ▲  replaced during development/testing by:
 ┌─ test-infra/  (stack 2, TEST ONLY, never shipped) ─────────────────┐
 │  postgres (PostGIS) :5432   - sample cities / rivers / states      │
 │  s3 (SeaweedFS) :8333       - bucket "rasters" (sample COGs)       │
 │  s3-bucket + seed (profile "seed", run once)                       │
 └────────────────────────────────────────────────────────────────────┘

 External web apps ── WMS/WFS ──► mapserver :8080 directly
```

| Stack | Folder | Services | Ships to customers? | Depends on |
|---|---|---|---|---|
| 1. Server | `server/` | `mapserver`, `msapi` | **Yes** | nothing (user's PostGIS/S3 are added later as stores) |
| 2. Test infra | `test-infra/` | `postgres`, `s3` (SeaweedFS), `s3-bucket` + `seed` (profile `seed`) | **No** | nothing |
| 3. Web | `web/` | `web` (nginx serving the React build) | **Yes** | server (`msapi`, `mapserver`) |

**How the stacks find each other**
- **Same host (default):** all stacks join one external Docker network `gis-net`,
  created once with `docker network create gis-net`. Services reach each other by
  name (`msapi`, `mapserver`, and in testing `postgres`, `s3`).
- **Different hosts:** `web/.env` holds `MSAPI_UPSTREAM` and `MAPSERVER_UPSTREAM`.
  The server stack needs no addresses at startup: user databases and buckets are
  entered as stores through the API.
- **Start order** doesn't matter. `msapi` needs nothing external to start (its DB
  is a local file). nginx resolves its upstreams per request (Docker DNS
  `resolver 127.0.0.11` + variable in `proxy_pass`), so it starts even if `msapi`
  is down and recovers when it comes up.

**Web stack:** a multi-stage Dockerfile builds the React app with Node, then copies
the static files into `nginx:alpine` (a few MB of RAM). nginx also proxies `/api`
to `msapi` and `/ows` to `mapserver`, so the browser sees one origin: no CORS
setup, and `GetFeatureInfo` works. External web apps still call MapServer
directly on :8080.

**Volumes (server stack):** `msapi` writes and `mapserver` reads `./mapfiles`
(read-only for MapServer). `./data` holds `msadmin.db` and is mounted only into
`msapi`. Backing up these two folders backs up the whole installation.

Key rules:
1. **The SQLite DB is the source of truth, not the mapfile.** The mapfile is always
   regenerated from `msadmin.db`. Nobody edits `services.map` by hand.
2. **Atomic write.** Generate to `services.map.tmp`, validate with MapServer, then
   rename over `services.map`. MapServer never reads a half-written file.
3. **No restart needed.** MapServer reads the mapfile on each request
   **[verify with the FastCGI setup in the camptocamp image]**.
4. **One writer at a time.** `msapi` runs as a single instance (SQLite has one
   writer). Regeneration holds an in-process mutex, so two API calls can't write
   the mapfile simultaneously.
5. **Every generated mapfile is versioned** in the `mapfile_versions` table for
   rollback (last 50 kept, older ones pruned automatically).
6. **User databases are read-only to us.** See §5.1.

---

## 4. Data model (SQLite file `data/msadmin.db`)

Opened with `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`.
JSON is stored as `TEXT` (validated in Go), times as ISO-8601 `TEXT` in UTC.

```sql
CREATE TABLE stores (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,            -- e.g. "city_db", "raster_bucket"
  type        TEXT NOT NULL CHECK (type IN ('postgis','s3')),
  config      TEXT NOT NULL,                   -- JSON: host/port/db/user/schema or bucket/region/endpoint/prefix
  secret      BLOB,                            -- AES-GCM encrypted password / S3 secret key
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE layers (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,            -- WMS layer name
  title       TEXT,
  abstract    TEXT,
  store_id    INTEGER NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
  kind        TEXT NOT NULL CHECK (kind IN ('vector','raster')),
  source      TEXT NOT NULL,                   -- JSON. vector: {schema,table,geom_column,pk,geom_type,srid}
                                               --       raster: {path,bands,dtype,nodata,srid}
  minx REAL, miny REAL, maxx REAL, maxy REAL,  -- extent in native SRID
  enabled     INTEGER NOT NULL DEFAULT 1,
  wfs_enabled INTEGER NOT NULL DEFAULT 0,
  min_scale   REAL,
  max_scale   REAL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE styles (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  format      TEXT NOT NULL CHECK (format IN ('sld','json')),
  body        TEXT NOT NULL,                   -- original SLD/JSON as uploaded (returned unchanged on GET)
  compiled    TEXT NOT NULL,                   -- generated mapfile CLASS blocks
  target      TEXT NOT NULL CHECK (target IN ('vector','raster')),
  warnings    TEXT,                            -- JSON array: SLD parts we could not convert
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE layer_styles (
  layer_id    INTEGER NOT NULL REFERENCES layers(id) ON DELETE CASCADE,
  style_id    INTEGER NOT NULL REFERENCES styles(id) ON DELETE RESTRICT,
  is_default  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (layer_id, style_id)
);

CREATE TABLE icons (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,            -- referenced from SLD as "school.png" or "icons/school.png"
  file_name   TEXT NOT NULL,                   -- file under mapfiles/icons/
  mime        TEXT NOT NULL,                   -- image/png (image/svg+xml if supported, see Phase 1)
  width       INTEGER,
  height      INTEGER,
  sha256      TEXT NOT NULL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE mapfile_versions (
  id          INTEGER PRIMARY KEY,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  content     TEXT NOT NULL,
  sha256      TEXT NOT NULL,
  reason      TEXT                             -- e.g. "layer roads created"
);
```

Deleting a store that has layers, or a style/icon that is in use, returns
`409 Conflict` with the list of dependents.

**Backup:** `GET /api/v1/system/backup` returns a `.tar.gz` with a consistent copy
of the DB (SQLite `VACUUM INTO`) plus `mapfiles/icons/`. `POST
/api/v1/system/restore` takes the same file. Since the mapfile is regenerated from
the DB, nothing else needs backing up.

Why not Postgres for our config: the customer's Postgres is theirs, and we may not
create schemas in it; running our own Postgres would add a server, RAM and
upgrades to a package meant to be light. The config is small (tens to a few
thousand rows) and written rarely, which is exactly what SQLite is good at.

---

## 5. Stores and layers

### 5.1 PostGIS store
- Input: `host, port, database, user, password, schema` (default `public`).
- **Read-only by design.** We only run catalog/metadata `SELECT`s: `geometry_columns`,
  `information_schema`, `pg_index` (primary key), `ST_EstimatedExtent` (falling back
  to `ST_Extent` when statistics are missing), `count(*)` estimates from
  `pg_class.reltuples`. Every API session is opened with
  `SET default_transaction_read_only = on` as a second guard. The README tells
  users to give us a role with only `CONNECT`, `USAGE` on the schema and `SELECT`
  on the tables they want to publish.
- On create: test connection with pgx, check the `postgis` extension exists (we
  never create it).
- Mapfile side: the API writes `secrets/pg_service.conf` with one section per store,
  and the mapfile uses `CONNECTION "service=<store_name>"`, so **passwords never
  appear in the mapfile**. MapServer container gets `PGSERVICEFILE` pointing at it
  **[verify env var reaches mapserv under Apache/FastCGI]**. Fallback: password in
  the generated mapfile, which lives only in the volume, not in git.
- `GET /stores/{name}/resources` lists tables/views from `geometry_columns`.

### 5.2 S3 store
- Input: `bucket, region, endpoint (optional, for MinIO), prefix, access_key,
  secret_key, use_https, virtual_hosting`.
- On create: list one object with minio-go to test credentials.
- Mapfile side: GDAL S3 options. If all S3 stores share credentials, use `CONFIG`
  in the `MAP` block. For different credentials per bucket, use GDAL's per-path
  credentials in a GDAL config file (`[credentials]` sections, GDAL ≥ 3.5)
  **[verify]**. Always set `GDAL_DISABLE_READDIR_ON_OPEN=EMPTY_DIR`,
  `CPL_VSIL_CURL_ALLOWED_EXTENSIONS=.tif,.tiff`, `VSI_CACHE=TRUE`.
- `GET /stores/{name}/resources` lists `.tif` objects under the prefix.

### 5.3 Vector layer (PostGIS)
- Input: `name, title, store, table` (optional `geom_column`, `pk`, `sql` for a view).
- Auto-detect from PostGIS: geometry column, geometry type → mapfile `TYPE`
  (POINT/LINE/POLYGON), SRID, primary key, estimated extent.
- Generated `LAYER`:
  ```
  LAYER
    NAME "roads"
    TYPE LINE
    CONNECTIONTYPE POSTGIS
    CONNECTION "service=gis_postgis"
    DATA "geom FROM public.roads USING UNIQUE gid USING SRID=4326"
    PROCESSING "CLOSE_CONNECTION=DEFER"
    PROJECTION "init=epsg:4326" END
    METADATA "ows_title" "Roads" "wfs_enable_request" "!*" ... END
    CLASSGROUP "roads_default"
    # CLASS blocks from assigned styles
  END
  ```
- If no style is assigned, a default style is generated for the geometry type
  (like GeoServer's default point/line/polygon).

### 5.4 Raster layer (single COG on S3)
- Input: `name, title, store, path`.
- Inspect with `gdalinfo -json /vsis3/<bucket>/<path>`: CRS, extent, bands, data
  type, nodata, and whether it is a valid COG (has overviews, tiled). Non-COG is
  rejected with a message telling the user to convert with `gdal_translate -of COG`.
- Generated `LAYER` with `TYPE RASTER` and `DATA "/vsis3/<bucket>/<path>"`.

### 5.5 Raster mosaic (many COGs) — later phase
- A tile-index **file in our volume** (GeoPackage or FlatGeobuf, built with
  `gdaltindex` from the `/vsis3/...` paths), used by `TILEINDEX`. Not a table in
  the user's database. API endpoint to rebuild it when files change.

---

## 6. Styles (SLD) and icons

### 6.1 Approach
SLD is converted **once, at upload**, into mapfile blocks and stored in
`styles.compiled`. Rendering never parses SLD, so there is no per-request cost.
The original SLD is kept and returned unchanged by `GET /styles/{name}`, so
existing GeoServer SLDs can be round-tripped.

Why our own converter instead of MapServer's built-in SLD support: MapServer only
applies SLD per request (`SLD=`/`SLD_BODY=`), which costs time on every map
request and fetches URLs server side. Converting it ourselves gives permanent
styles, lets us resolve icons to local files, and lets us report exactly what
was not supported.

### 6.2 Multiple styles per layer
Each style compiles to `CLASS` blocks with `GROUP "<style_name>"`. The layer's
`CLASSGROUP` is the default style. The web app picks another with the standard
WMS `STYLES=<style_name>` parameter. `GetLegendGraphic` works per style.

### 6.3 Supported SLD subset (v1)

| SLD 1.0 / SE 1.1 | Mapfile |
|---|---|
| `Rule` | `CLASS` |
| `MinScaleDenominator` / `MaxScaleDenominator` | `MINSCALEDENOM` / `MAXSCALEDENOM` |
| `ogc:Filter`: `PropertyIsEqualTo`, `NotEqualTo`, `LessThan(OrEqualTo)`, `GreaterThan(OrEqualTo)`, `PropertyIsBetween`, `PropertyIsLike`, `PropertyIsNull`, `And`, `Or`, `Not` | `EXPRESSION (...)` |
| `ElseFilter` | last `CLASS` without `EXPRESSION` |
| `PointSymbolizer` + `Mark` (`circle, square, triangle, star, cross, x`) | `STYLE` + built-in symbols in `symbols.sym` |
| `PointSymbolizer` + `ExternalGraphic` | `STYLE SYMBOL "<icon>"` → `SYMBOL TYPE PIXMAP` |
| `Size`, `Rotation`, `Opacity` | `SIZE`, `ANGLE`, `OPACITY` |
| `LineSymbolizer`: `stroke`, `stroke-width`, `stroke-opacity`, `stroke-dasharray`, `stroke-linecap`, `stroke-linejoin` | `STYLE` `OUTLINECOLOR`/`COLOR`, `WIDTH`, `OPACITY`, `PATTERN`, `LINECAP`, `LINEJOIN` |
| `PolygonSymbolizer`: `fill`, `fill-opacity`, `stroke` | `STYLE` `COLOR`, `OPACITY`, `OUTLINECOLOR`, `WIDTH` |
| `TextSymbolizer`: `Label`, `Font` (family, size, weight), `Halo`, `Fill`, `LabelPlacement` (point offset, line placement) | `LABEL` with `TEXT`, `FONT`/`SIZE`, `OUTLINECOLOR`/`OUTLINEWIDTH`, `COLOR`, `OFFSET`, `ANGLE FOLLOW` |
| `RasterSymbolizer` + `ColorMap` type `ramp` | `CLASS` with `STYLE COLORRANGE`/`DATARANGE` |
| `ColorMap` type `intervals` / `values` | one `CLASS` per entry with `EXPRESSION ([pixel] ...)` |
| `RasterSymbolizer` `Opacity` | layer `OPACITY` |

Anything outside this list is **not silently dropped**: the upload succeeds with a
`warnings` array listing the unsupported elements, or fails with `422` if the
style would render nothing. GeoServer-only features (rendering transformations,
vendor options) produce warnings.

Fonts: a `fontset.txt` with a small set of TTF fonts shipped in the image; SLD
font families are mapped to these names.

### 6.4 Icons
- `POST /api/v1/icons` (multipart). PNG in v1. SVG only if the MapServer build
  supports `TYPE SVG` symbols **[verify]**.
- Stored under `mapfiles/icons/`, deduplicated by sha256.
- In SLD, `ExternalGraphic/OnlineResource/@xlink:href` is matched to an icon by its
  file name (`school.png`, `icons/school.png`, or a full URL ending in
  `/icons/school.png`). Unknown icon → `422` with the missing name.
- Each used icon becomes `SYMBOL NAME "icon_school" TYPE PIXMAP IMAGE "icons/school.png" END`
  in the generated `symbols.sym`.

### 6.5 JSON style (shortcut)
For simple cases without writing SLD:
```json
{ "target": "vector", "rules": [
  { "name": "Schools", "filter": "type = 'school'", "icon": "school.png", "size": 24 },
  { "name": "Other", "else": true, "mark": "circle", "fill": "#3366cc", "size": 8 }
]}
```
Compiles to the same `CLASS` blocks as SLD.

---

## 7. REST API (v1)

Base path `/api/v1`. JSON in/out, except SLD (`application/vnd.ogc.sld+xml`) and
icon upload (multipart). Errors: `{"error": "...", "details": [...]}`.

**Stores**
```
POST   /stores                         create (postgis | s3), tests connection first
GET    /stores                         list (secrets never returned)
GET    /stores/{name}
PUT    /stores/{name}                  update; re-tests connection
DELETE /stores/{name}                  409 if layers use it
POST   /stores/{name}/test             test connection
GET    /stores/{name}/resources        PostGIS tables / S3 .tif objects available to publish
```

**Layers**
```
POST   /layers                         publish from a store (auto-detects metadata)
GET    /layers
GET    /layers/{name}
PUT    /layers/{name}                  title, abstract, enabled, wfs_enabled, scales
DELETE /layers/{name}
PUT    /layers/{name}/styles           {"default": "roads_main", "alternates": ["roads_night"]}
GET    /layers/{name}/preview          PNG; ?bbox=&width=&height=&style=
POST   /layers/{name}/preview          PNG rendered with an unsaved style in the body (style editor live preview)
POST   /layers/{name}/refresh          re-read extent / metadata from source
```

**Styles**
```
POST   /styles?name=roads_main         body: SLD XML or JSON style
GET    /styles
GET    /styles/{name}                  original SLD/JSON
GET    /styles/{name}/compiled         generated mapfile blocks (debugging)
POST   /styles/validate                compile without saving; returns compiled blocks + warnings
GET    /styles/{name}/legend           legend PNG (via MapServer GetLegendGraphic)
PUT    /styles/{name}
DELETE /styles/{name}                  409 if assigned to a layer
```

**Icons**
```
POST   /icons                          multipart file, optional name
GET    /icons
GET    /icons/{name}                   the image
DELETE /icons/{name}                   409 if used by a style
```

**System**
```
GET    /health                         API + SQLite + MapServer reachable (+ per-store status)
GET    /mapfile                        current generated mapfile (admin)
GET    /mapfile/versions
POST   /mapfile/rollback/{id}
POST   /mapfile/rebuild                regenerate from DB
GET    /system/backup                  .tar.gz: DB snapshot + icons
POST   /system/restore                 restore from that file, then rebuild mapfile
```

**OWS for the admin UI** is not part of this API: the web stack's nginx proxies
`/ows` straight to MapServer (see §3), so map requests never pass through Go.

Example — the full GeoServer-style flow:
```bash
# 1. store
curl -X POST localhost:8081/api/v1/stores -H 'Content-Type: application/json' -d '{
  "name":"city_db","type":"postgis",
  "connection":{"host":"db.customer.local","port":5432,"database":"gisdb","user":"map_reader","password":"***","schema":"public"}}'

# 2. layer
curl -X POST localhost:8081/api/v1/layers -H 'Content-Type: application/json' -d '{
  "name":"cities","title":"Indian Cities","store":"city_db","table":"cities"}'

# 3. icon + SLD style
curl -X POST localhost:8081/api/v1/icons -F file=@city.png
curl -X POST 'localhost:8081/api/v1/styles?name=cities_icon' \
  -H 'Content-Type: application/vnd.ogc.sld+xml' --data-binary @cities.sld

# 4. assign style
curl -X PUT localhost:8081/api/v1/layers/cities/styles -H 'Content-Type: application/json' \
  -d '{"default":"cities_icon"}'

# Web app
# http://localhost:8080/?SERVICE=WMS&REQUEST=GetMap&LAYERS=cities&STYLES=&...
```

---

## 8. Security

- **API:** API-key header (`X-API-Key`) in v1, key from env. JWT later if the web
  app needs per-user access.
- **MapServer:** WMS/WFS stay public in v1 (same as our GeoServer setups). If
  per-layer access is needed later: put nginx (alpine, a few MB RAM) in front and
  use `auth_request` to ask `msapi` whether the caller may see the requested layers.
- Secrets encrypted in `msadmin.db` (AES-GCM, key from `MSAPI_SECRET_KEY` env).
  Losing the key means re-entering store passwords; the README says to keep it
  with the backup, not inside it.
- Generated mapfile and `secrets/` live in a Docker volume, never in git.
- `MS_MAP_NO_PATH` / fixed `MS_MAPFILE` so clients cannot load other mapfiles via `?map=`.
- WFS disabled per layer by default (`wfs_enable_request "!*"`), enabled via API.

---

## 9. Project layout

```
MAPSERVER_UI/
  README.md                   how to create gis-net and start the stacks

  server/                     ── stack 1: mapserver + API (PRODUCT) ──
    docker-compose.yml        mapserver, msapi
    .env.example              API key, secret key, public URL, ports
    mapserver/
      mapserver.conf          MapServer 8 config (MS_MAP_NO_PATH, fixed mapfile)
    mapfiles/                 volume: generated services.map, symbols.sym, fontset, icons/, secrets/
    data/                     volume: msadmin.db (SQLite), mounted only into msapi
    msapi/
      Dockerfile              multi-stage: golang:1.x build → FROM mapserver image
      go.mod
      cmd/msapi/main.go
      internal/
        config/               env config
        store/                SQLite (modernc.org/sqlite): open, embedded migrations (migrations/*.sql), queries
        pgread/               read-only pgx pools for user PostGIS stores
        crypto/               AES-GCM for secrets
        api/                  handlers: stores.go layers.go styles.go icons.go system.go
        inspect/              postgis.go (geometry_columns, extent), raster.go (gdalinfo -json)
        sld/                  parse.go, filter.go, convert.go  (+ golden-file tests)
        mapfile/              templates/*.tmpl, build.go, write.go (atomic + validate), symbols.go
        s3/                   minio-go listing
      openapi.yaml

  web/                        ── stack 3: React admin UI + map viewer (see §11) ──
    docker-compose.yml        web (nginx)
    .env.example              MSAPI_UPSTREAM, MAPSERVER_UPSTREAM, public port
    Dockerfile                multi-stage: node build → nginx:alpine
    nginx.conf                SPA fallback, gzip, cache headers, /api + /ows proxy
    package.json
    vite.config.ts            dev proxy: /api → msapi, /ows → mapserver
    src/
      app/                    router, layout, theme, query client
      api/                    typed API client + TanStack Query hooks
      components/ui/          shadcn/ui components
      features/
        dashboard/
        stores/
        layers/
        styles/
        icons/
        map/                  full-screen OpenLayers viewer
        system/               mapfile viewer, versions, rollback
      lib/ol/                 OpenLayers helpers (WMS source, extent, GetFeatureInfo)

  test-infra/                 ── stack 2: TEST ONLY, never shipped ──
    docker-compose.yml        postgres, s3 (SeaweedFS), s3-bucket + seed (profile "seed")
    .env.example              test DB + S3 credentials
    init-db.sql               PostGIS extensions + sample cities/rivers/states + map_reader role
    postgres_data/            (moved from repo root; git-ignored)
    s3_data/                  (git-ignored)
    seed/
      seed.sh                 download → convert to COG → upload to S3 (GDAL /vsis3/)
      s3.json                 SeaweedFS S3 test credentials

  dev/                        test fixtures (not deployed)
    sld/                      sample SLDs for points, lines, polygons, raster
    icons/                    sample PNG icons
```

In Phase 1 the current root files move: `init-db.sql` and `postgres_data/` →
`test-infra/`, `mapfiles/` → `server/`, and the root `docker-compose.yml` is
replaced by the per-stack files. `postgres_data/` is moved only with Postgres
stopped, after a `pg_dump`.

---

## 10. Test environment (S3 + sample raster)

`test-infra/` plays the part of "the customer's infrastructure" during
development: a PostGIS with sample tables and an S3 bucket (SeaweedFS) with sample COGs.
It is never part of the product. In tests, `msapi` reaches it exactly the way it
would reach a customer's systems: as stores created through the API.

The one-off seed job is under compose **profile `seed`**, so a normal
`docker compose up` in `test-infra/` never runs it; load sample data with
`docker compose --profile seed up seed`.

| Service | Image | Purpose |
|---|---|---|
| `s3` | `chrislusf/seaweedfs:4.47`. **Checked 2026-10-14: `minio/minio` is no longer on Docker Hub**, so SeaweedFS is used instead. | Local S3 on :8333. Credentials in `seed/s3.json`. Data in `./s3_data`. |
| `s3-bucket` | same SeaweedFS image (`weed shell`) | Profile `seed`. Creates bucket `rasters`, then exits. |
| `seed` | `ghcr.io/osgeo/gdal:ubuntu-small-3.13.0` | Profile `seed`. Prepares sample rasters and uploads them with GDAL `/vsis3/` (no `mc` needed). |

**Sample rasters** (seed script `test-infra/seed/seed.sh`):

| File | Source | Tests |
|---|---|---|
| `dem/kolkata_dem_cog.tif` | Copernicus DEM GLO-30 tile `Copernicus_DSM_COG_10_N22_00_E088_00_DEM` on AWS Open Data, already a COG (URL checked 2026-10-14). | Single-band raster, `ColorMap` ramp/intervals styles, nodata. |
| `rgb/sample_rgb_cog.tif` | Sentinel-2 TCI COG `S2B_45QXE_20250227_0_L2A` (sentinel-cogs, found via Earth Search STAC, checked 2026-10-14), clipped to Kolkata. | 3-band RGB rendering. |
| `synthetic/gradient_cog.tif` | Generated by GDAL in the script (no download). | Offline fallback so tests work without internet. |
| `ortho/odm_orthophoto_cog.tif` | **Already downloaded** — `odm_orthophoto.tif` (~435 MB, OpenDroneMap orthophoto) is in the repo root. **Do not download it again.** The seed job mounts it read-only and converts/uploads it. | Real-world high-resolution RGB(A) drone orthophoto: large-file COG conversion, overviews, rendering at many zoom levels. |

**Local orthophoto:** `odm_orthophoto.tif` must stay out of git (add it to
`.gitignore`; it is too large to commit). `seed.sh` uses it if present and
skips it with a warning if missing; it is never fetched from the internet.

Each file is checked with `gdalinfo` for `LAYOUT=COG`; anything that isn't COG is
converted with `gdal_translate -of COG -co COMPRESS=DEFLATE` first.

**Sample vector data:** the existing `cities` table, plus `init-db.sql` gains one
line table (e.g. `rivers`) and one polygon table (e.g. `states`) with a few
simplified features, so every symbolizer type has something to test against.

**Test assets:** `dev/sld/` holds one SLD per case (icon points, filtered points,
dashed lines, labelled polygons, DEM ramp); `dev/icons/` a few PNG icons. These
are the end-to-end fixtures and the converter's golden-file inputs.

**Default test credentials** (S3 `testaccess`, DB `gisuser`, read-only `map_reader`) live in a
`.env.example`; the real `.env` is git-ignored.

---

## 11. Web UI (admin + map viewer)

A small, modern single-page app. The goal is the opposite of GeoServer's admin:
few screens, guided flows, live previews, clean visuals.

### 11.1 Stack

| Part | Choice | Why |
|---|---|---|
| Build | **Vite + React + TypeScript** | Fast dev server, small static output. |
| Styling | **Tailwind CSS + shadcn/ui** (Radix primitives) | Modern, consistent look; components are copied into the repo, not a heavy UI framework. |
| Icons | **lucide-react** | Clean line icons, tree-shaken. |
| Map | **OpenLayers** (pinned version) | Same library as `viewer.html`; strongest WMS support. |
| Server data | **TanStack Query** | Caching, loading/error states, refetch after mutations. |
| Forms | **react-hook-form + zod** | Validation with clear inline errors. |
| Routing | **React Router** | |
| Code editor (SLD) | **CodeMirror 6** with XML mode | Much lighter than Monaco. |
| Toasts / command palette | **sonner**, **cmdk** | Feedback after actions; `Ctrl+K` to jump to any layer/style. |

Output is static files (target: well under 1 MB gzipped, OpenLayers being the
largest part), served by nginx in the web stack. In development, `npm run dev`
runs Vite with the same `/api` and `/ows` proxy rules, so code works the same in
both.

### 11.2 Look and feel
- Left sidebar navigation, top bar with search (`Ctrl+K`), theme toggle, health dot.
- Light and dark themes, one accent colour, Inter font, generous spacing,
  rounded cards, subtle shadows. No dense tables of checkboxes.
- Every list has an empty state with a "create" button and a one-line hint.
- Every destructive action shows what depends on it ("used by 3 layers") before
  confirming.
- Responsive down to tablet width; the map viewer works on phones.

### 11.3 Screens

| Screen | What it does |
|---|---|
| **Login** | Enter API key (kept in `sessionStorage`). Replaced by real login if JWT is added. |
| **Dashboard** | Counts of stores/layers/styles/icons; health of API, DB, MapServer, each store; recently changed layers. |
| **Stores** | Cards per store with type badge and status. **Create wizard:** choose PostGIS or S3 → form → "Test connection" (must pass) → save. Store page lists publishable resources (tables with geometry type/row count, or COG files with size) with a "Publish" button on each. |
| **Layers** | Searchable table with type icon, store, default style, enabled toggle. **Publish wizard:** store → resource → auto-detected metadata shown for confirmation (geometry, SRID, extent, bands) → name/title → pick style or keep default → publish. **Layer page:** live map preview zoomed to extent, metadata, styles tab (set default, add alternates), ready-to-copy snippets for OpenLayers / Leaflet / QGIS / raw WMS URL. |
| **Styles** | Grid with legend thumbnails. **Editor** in split view: left side has two tabs, *Visual* (rules list, filter builder, colour pickers, size/width sliders, icon picker, label settings) and *SLD* (CodeMirror); right side is a live map preview on a chosen layer that re-renders as you edit (debounced, via `POST /layers/{name}/preview`). Warnings from the converter are shown inline. Import an existing `.sld` file by drag-and-drop. |
| **Icons** | Gallery grid, drag-and-drop upload (multiple files), shows where each icon is used. |
| **Map viewer** | Full-screen OpenLayers map. Layer panel: toggle, opacity slider, style dropdown, drag to reorder. Basemap switcher (OSM, light, dark, none). Click on map → `GetFeatureInfo` popup. Legend panel. "Zoom to layer". Shareable URL with current layers/view. |
| **System** | Current generated mapfile (read-only, syntax-highlighted), version history with diff, rollback button. |

---

## 12. Phases (about two per working day)

Each phase ends with something that can be run and checked. Work stops at the end
of each phase for review before the next one starts.

| Day | Phase | Work | Done when |
|---|---|---|---|
| **1** | **1. Split stacks + test infra + spikes** | `pg_dump` backup, then split the root compose into `test-infra/` (Postgres + S3 + seed, test only) and `server/` (MapServer), shared `gis-net` network, `.env.example` per stack. Upgrade MapServer to `camptocamp/mapserver:8.6-gdal3.13` (tag checked). Seed job + sample rasters. Answer every **[verify]**: `map2img` validation, mapfile reload without restart, `PGSERVICEFILE` under FastCGI, S3 per-bucket credentials, SVG symbols, S3 image (done: SeaweedFS). Hand-write a mapfile COG layer to prove MapServer reads from the test S3. | `test-infra` and `server` stacks start independently; existing `cities` data intact; the S3 DEM renders in `viewer.html`. Plan updated with findings. |
| **1** | **2. API skeleton** | Go module, config, SQLite (`data/msadmin.db`) with embedded migrations, API key middleware, `/health`, Dockerfile (Go build → MapServer base image), `msapi` added to `server/docker-compose.yml`. | `/api/v1/health` reports SQLite + MapServer OK with **no** Postgres running. |
| **2** | **3. Stores API** | PostGIS + S3 stores, AES-GCM secrets, connection test, `/resources` listing, `pg_service.conf` generation. | Both store types created via curl; S3 bucket and `cities` table show up in `/resources`. |
| **2** | **4. Vector layers + mapfile engine** | PostGIS inspection, default styles per geometry type, mapfile templates, atomic write + `map2img` validation, write mutex, versions, rollback, preview PNG. Sample `rivers`/`states` tables in `test-infra`. Read-only guard on user DB sessions. | `cities`, `rivers`, `states` published via API and render via `/ows`. Bad input never breaks the live mapfile. |
| **3** | **5. Raster layers** | `gdalinfo -json` inspection, COG check, raster LAYER with S3 config, default raster style. | DEM and RGB from S3 published via API and render. |
| **3** | **6. UI foundation + web stack** | Vite + React + TS, Tailwind + shadcn/ui, layout (sidebar, top bar, theme), API client, login, dashboard, **Stores** screens with create wizard and resource list. `web/` stack: Dockerfile (node build → nginx:alpine), `nginx.conf` with `/api` + `/ows` proxy. | Can create both store types from the UI, both via `npm run dev` and via the `web` stack container. |
| **4** | **7. UI layers + map viewer** | Publish wizard, layers list, layer page with live preview and copy snippets, full-screen map viewer (layer panel, basemaps, GetFeatureInfo, legend). | Every table and COG can be published and viewed entirely from the UI. |
| **4** | **8. SLD converter (vector)** | SLD parser; `Rule`, scale, filters, Point (Mark), Line, Polygon symbolizers; `CLASS GROUP` / `CLASSGROUP` for multiple styles; `/styles` CRUD + `/styles/validate`; golden-file tests with `dev/sld/`. | Point/line/polygon sample SLDs render correctly; alternate style selectable with `STYLES=`. |
| **5** | **9. Icons, labels, raster styles** | Icons API + `symbols.sym` generation, `ExternalGraphic`, `TextSymbolizer` + fontset, `RasterSymbolizer` ColorMap, legend endpoint, JSON style format. | Icon SLD, labelled polygons and DEM colour ramp all render; legends show. |
| **5** | **10. UI styles + icons** | Style editor (Visual + SLD tabs, live preview, inline warnings, SLD import), styles grid with legend thumbnails, icons gallery with drag-and-drop, assign styles on layer page. | A style can be created from scratch in the visual editor, with icons, and seen live on the map. |
| **6** | **11. Hardening + packaging** | System screen (mapfile, versions, diff, rollback), OpenAPI docs, Go tests + one end-to-end test run against seeded data, load test of WMS GetMap, backup/restore endpoints, RAM measurement of every container, README for server setup (same-host and separate-host). | On a clean server: create `gis-net`, `docker compose up -d` in `server/` and `web/` only, add an external PostGIS and bucket as stores, and everything works; numbers recorded in README. |
| Later | — | Raster mosaics (tile index), nginx auth for per-layer access, JWT login, MapCache tiles, our real GeoServer SLDs as extra fixtures. | As needed. |

Phases can move between days. The rule is to finish and check one phase before
starting the next, not to fit a fixed calendar.

---

## 13. Existing issues to fix along the way

- `docker-compose.yml`: `camptocamp/mapserver:8.0` is an unsupported branch; current
  release is 8.6.6 (security fixes, including one in the SLD parser in 8.6.3).
- `mapfiles/gis.map`: PostGIS password is in plain text in the mapfile.
- `mapfiles/gis.map`: `cities_geojson` points to `/mapdata/sample_cities.geojson`,
  but no `/mapdata` volume is mounted.
- `mapfiles/gis.map`: `ows_enable_request "*"` exposes every request on every layer.
- `mapfiles/gis.map`: `wms_onlineresource` is hard-coded to `localhost:8080`; must
  come from config for server deployment.
- `viewer.html`: loads `ol@latest` from CDN; pin a version.
- `docker-compose.yml`: Postgres port 5432 exposed to the host; DB credentials inline
  (acceptable once it is only `test-infra`, but credentials move to `.env`).

`gis.map` will be replaced by the generated `services.map` once Phase 4 is done.

---

## 14. Open questions

1. Do we have real SLDs from past GeoServer projects to use as test cases? They set
   the scope of the SLD converter.
2. S3 provider: AWS S3, MinIO, or both? One set of credentials or different per bucket?
3. Is a public (unauthenticated) WMS acceptable for v1?
4. Is WFS needed by the web apps, or only WMS?

---

## 15. Progress log

### 2026-10-14: all phases coded, nothing run yet

Docker isn't installed on the dev machine yet. Everything runs in Docker and
nothing is built or tested on the host, so **none of this code has been
compiled or run**. Expect compile errors and small fixes on the Docker day.
The phase rule (stop for review after each phase) was relaxed on request: all
phases were written in one go, and review and testing happen together.

**Phase 1 (files only)**
- `.gitignore`, `.gitattributes` (LF for `.sh`, `.map`, `.conf`, `.sql`, `.go`).
  `odm_orthophoto.tif` was removed from git and stays local.
- `test-infra/`: compose (`postgres`, `s3` = SeaweedFS, `s3-bucket` + `seed`
  under profile `seed`), `.env.example`, `seed/seed.sh` (DEM, Sentinel-2 RGB,
  synthetic gradient, local orthophoto → COG → `/vsis3/`), `seed/s3.json`.
  `init-db.sql` was moved here; it adds `rivers`, `states` and a read-only
  `map_reader` role.
- `server/`: compose (`mapserver` 8.6, `msapi`), `.env.example`,
  `mapserver/mapserver.conf` (`MS_MAPFILE` = generated `services.map`,
  `MS_MAP_NO_PATH`, `PGSERVICEFILE`). `mapfiles/gis.map` is kept as the
  hand-written Phase 1 spike (COG layers from the test S3).
- `viewer.html`: OpenLayers pinned to 10.6.1; `?layers=&lon=&lat=&zoom=`.

**Phases 2–5, 8, 9, 11: API (`server/msapi`)**
- Config from env, SQLite + embedded migrations, API-key middleware, slog,
  graceful shutdown, `/health` (store status only with a valid key).
- Stores: PostGIS (pgx, read-only sessions, pools of max 2) and S3
  (minio-go); AES-GCM secrets; unsaved-store test; `/resources`;
  `pg_service.conf`. The stored secret is reused for tests only when the
  target is unchanged.
- Layers: PostGIS inspection (geometry, SRID, primary key, extent), COG
  inspection with `gdalinfo -json` (COG check, EPSG, stats for single-band
  rasters), `/layers/inspect`, refresh, preview PNG.
- Mapfile engine: `text/template` → temp file → `mapserv` GetCapabilities
  validation → rename; write mutex; versions (50 kept), rollback, rebuild at
  startup. A failed rebuild rolls back the database change, so the live
  mapfile never breaks.
- SLD converter (`internal/sld`): SLD 1.0 / SE 1.1 and the JSON shortcut →
  model → CLASS blocks rendered per layer type (a line symbolizer on polygons
  becomes an outline; a point symbolizer on lines/polygons uses a centroid).
  Supports filters (incl. Like, Between, Null), scales, marks, icons, dashes,
  labels with halo/placement, raster ColorMap ramp/intervals/values and
  opacity. Unsupported parts become warnings; missing icons and "renders
  nothing" give 422. Golden tests use `dev/sld/`.
- Styles API (CRUD, validate, compiled, legend, layer assignment with
  default + alternates via `CLASS GROUP`, preview with an unsaved style via
  the `mapserv` CLI). Icons API (PNG, content-addressed, `symbols.sym`).
  DejaVu fonts are copied into the mapfiles volume for labels.
- Backup/restore (`VACUUM INTO` + icons as `.tar.gz`; restore via `ATTACH` in
  one transaction) and OpenAPI at `/api/v1/docs`.

**Phases 6, 7, 10, 11: UI (`web/`)**
- Vite + React 19 + TS, Tailwind 4, Radix/shadcn-style components, TanStack
  Query, react-hook-form + zod, OpenLayers 10, CodeMirror, sonner, cmdk.
- Login, layout (sidebar, Ctrl+K, theme, health dot), dashboard, stores
  (wizard with mandatory connection test, resource list with Publish),
  layers (list, publish wizard with detection step, layer page with live map,
  settings, styles tab, snippets), full-screen map viewer (layer panel,
  opacity, styles, drag to reorder, basemaps, GetFeatureInfo, legend,
  shareable URL), styles (grid with legends, SLD import, editor with Visual
  and SLD tabs, live preview, warnings, compiled output), icons gallery with
  drag-and-drop, system (mapfile, versions, diff, rollback, rebuild,
  backup/restore).
- `web/` stack: Dockerfile (node build → nginx), `nginx.conf` with
  `/api` + `/ows` proxy and runtime upstreams.

**Test tooling:** `dev/e2e.sh` (full API flow against test-infra),
`dev/loadtest/getmap.js` (k6), `dev/sld/*` fixtures, `dev/icons/*`.

**Changes from the plan**
- Test S3 server is SeaweedFS, because `minio/minio` is no longer on Docker Hub.
- `openapi.yaml` lives in `internal/api/` (it is embedded in the binary).
- Extra endpoints: `POST /stores/test`, `POST /layers/inspect`,
  `GET /mapfile/versions/{id}`, `GET /api/v1/docs`.
- Styles are recompiled for each layer's geometry type on rebuild (still never
  per request); `styles.compiled` stores the generic output for display.
- Rasters from several S3 stores must share one endpoint and credentials
  (MAP-level `CONFIG`); otherwise publishing fails with 422.

**To do on the Docker day**
1. `docker network create gis-net`; copy each `.env.example` to `.env` and set
   `MSAPI_API_KEY` and `MSAPI_SECRET_KEY` (`openssl rand -base64 32`).
2. Stop the old root stack. `postgres_data/` is still in the repo root: take a
   `pg_dump`, then move it to `test-infra/postgres_data/` and update the
   compose volume. `init-db.sql` runs only on an empty data dir, so on the old
   data run it by hand for `rivers`, `states` and `map_reader`.
3. `test-infra`: `docker compose up -d`, then `docker compose --profile seed up seed`.
4. Go: `docker run --rm -v "$PWD:/repo" -w /repo/server/msapi golang:1.26-alpine sh -c 'go mod tidy && go vet ./... && go test ./...'`.
   Fix compile errors and check in the generated `go.sum`.
5. Web: `docker run --rm -v "$PWD/web:/app" -w /app node:24-alpine sh -c 'npm install && npm run build'`.
   Fix type errors and check in `package-lock.json`.
6. Phase 1 spike: temporarily set `MS_MAPFILE` to `gis.map` and check
   `viewer.html?layers=dem_kolkata&lon=88.36&lat=22.57&zoom=9`.
7. `server`: `docker compose up -d --build`; `web`: `docker compose up -d --build`;
   then `MSAPI_API_KEY=… ./dev/e2e.sh`.
8. Answer the remaining **[verify]** items: `mapserv -nh` CLI with
   `MAPSERVER_CONFIG_FILE` and `MS_MAP_PATTERN` (validator and previews),
   whether `MAPSERVER_CONFIG_FILE` and `PGSERVICEFILE` reach mapserv in the
   camptocamp image, mapfile reload without restart, `GEOMTRANSFORM
   "centroid"` and `COLOR "#rrggbbaa"` in 8.6, uid/gid for the shared
   `mapfiles/` volume, SeaweedFS bucket creation, and `gdal vsi copy` to
   SeaweedFS.
9. Run the load test and `docker stats`; fill in the README table.
10. Remove the old root `docker-compose.yml` once the new stacks work.

### 2026-10-15: first run in Docker

Docker Desktop with WSL 2 was set up on the dev machine. Results:

- `go vet` clean; all Go unit tests pass. `gofmt` applied. Web: `tsc` +
  `vite build` clean (about 450 KB gzipped JS). `go.sum` and
  `web/package-lock.json` were generated.
- Seed job: all four COGs uploaded to SeaweedFS (DEM, Sentinel-2 RGB,
  gradient, orthophoto 13716×25872 converted in about 50 s).
- `dev/e2e.sh`: **44/44 passed** (stores, publish, WMS GetMap for vector and
  raster, icons, six SLD/JSON styles, alternate style via `STYLES=`, legend,
  409 guards, unsaved-style preview, versions, backup, restore).
- Load test and RAM: see README (182 req/s, p95 35 ms, msapi 11 MiB,
  mapserver about 100 MiB).
- Not yet checked: the web UI in a browser (the Chrome extension was not
  connected). The UI builds, nginx serves it, and `/api` and `/ows` proxy correctly.

Bugs found and fixed:
1. SE 1.1 rule titles (`Description/Title`) were read after `Name`.
2. MapServer 8 loads only paths ending in `.map`: temp mapfiles are now
   `.services-*.tmp.map`, and the validator's `MS_MAP_PATTERN` is `^/etc/mapserver/[^/]+\.map$`.
3. Layer-level `OPACITY` no longer exists in MapServer 8: it is now `COMPOSITE OPACITY n END`.
4. MapServer 8 quotes column names in `DATA` itself: geometry and unique
   columns are no longer quoted by us (`""geom""` broke the SQL).
5. `PGSERVICEFILE` in the `ENV` block of `mapserver.conf` does **not** reach
   libpq. It is now a process env var (compose for `mapserver`, Dockerfile
   for `msapi`).
6. MapServer's worker runs as `www-data` (gid 33): `pg_service.conf` is now
   `0640` with group gid (`MSAPI_MAPSERVER_GID`, default 33).
7. e2e script: `pipefail` + `grep -q` gave a false "backup broken".

[verify] items answered:
- The camptocamp image honours `MAPSERVER_CONFIG_FILE`; `MS_MAPFILE` and
  `MS_MAP_NO_PATH` from the config file work.
- `mapserv -nh "QUERY_STRING=…"` works for validation and previews.
- A changed mapfile is picked up without a restart.
- `GEOMTRANSFORM "centroid"` and `COLOR "#rrggbbaa"` are accepted.
- SeaweedFS `weed shell s3.bucket.create` and `gdal vsi copy` to `/vsis3/` work.

Still open: SVG icons, per-bucket S3 credentials, a browser walk-through of
the UI, and removing the root files (`docker-compose.yml`, `viewer.html`,
`postgres_data/`, which were deleted from the working tree but are still in git).
