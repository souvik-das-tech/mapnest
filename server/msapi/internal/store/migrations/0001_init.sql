-- Initial schema (PLAN.md §4).

CREATE TABLE stores (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  type        TEXT NOT NULL CHECK (type IN ('postgis','s3')),
  config      TEXT NOT NULL,
  secret      BLOB,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE layers (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  title       TEXT,
  abstract    TEXT,
  store_id    INTEGER NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
  kind        TEXT NOT NULL CHECK (kind IN ('vector','raster')),
  source      TEXT NOT NULL,
  minx REAL, miny REAL, maxx REAL, maxy REAL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  wfs_enabled INTEGER NOT NULL DEFAULT 0,
  min_scale   REAL,
  max_scale   REAL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX layers_store_id ON layers(store_id);

CREATE TABLE styles (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  format      TEXT NOT NULL CHECK (format IN ('sld','json')),
  body        TEXT NOT NULL,
  compiled    TEXT NOT NULL,
  target      TEXT NOT NULL CHECK (target IN ('vector','raster')),
  warnings    TEXT,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE layer_styles (
  layer_id    INTEGER NOT NULL REFERENCES layers(id) ON DELETE CASCADE,
  style_id    INTEGER NOT NULL REFERENCES styles(id) ON DELETE RESTRICT,
  is_default  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (layer_id, style_id)
);
CREATE INDEX layer_styles_style_id ON layer_styles(style_id);

CREATE TABLE icons (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  file_name   TEXT NOT NULL,
  mime        TEXT NOT NULL,
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
  reason      TEXT
);
