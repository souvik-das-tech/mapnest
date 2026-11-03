-- Workspaces, like GeoServer's: a store belongs to one workspace and its
-- layers follow it. Each workspace gets its own WMS/WFS endpoint
-- (ws_<name>.map). Styles and icons stay shared. Existing stores move to
-- "default". Runs with foreign_keys OFF, so the column can carry a
-- REFERENCES clause with a non-NULL default.

CREATE TABLE workspaces (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  title       TEXT,
  description TEXT,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO workspaces (id, name, title) VALUES (1, 'default', 'Default');

ALTER TABLE stores ADD COLUMN workspace_id INTEGER NOT NULL DEFAULT 1 REFERENCES workspaces(id) ON DELETE RESTRICT;
CREATE INDEX stores_workspace_id ON stores(workspace_id);
