-- Local raster folder stores: allow type 'local'. SQLite cannot change a
-- CHECK constraint, so the table is rebuilt (runs with foreign_keys OFF;
-- layers.store_id keeps pointing at the same ids).

CREATE TABLE stores_new (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  type        TEXT NOT NULL CHECK (type IN ('postgis','s3','local')),
  config      TEXT NOT NULL,
  secret      BLOB,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO stores_new (id, name, type, config, secret, created_at, updated_at)
  SELECT id, name, type, config, secret, created_at, updated_at FROM stores;
DROP TABLE stores;
ALTER TABLE stores_new RENAME TO stores;
