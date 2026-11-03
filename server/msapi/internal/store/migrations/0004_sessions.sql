-- Admin UI sessions, so sign-out and idle timeout work on the server side.
-- Only a SHA-256 of the random session id is stored: a copy of this file
-- (or a backup) cannot be used to sign in. Not part of backup/restore.

CREATE TABLE sessions (
  id_hash      TEXT PRIMARY KEY,           -- hex SHA-256 of the session id
  username     TEXT NOT NULL,
  created_at   INTEGER NOT NULL,           -- unix seconds
  expires_at   INTEGER NOT NULL,           -- absolute expiry
  last_seen_at INTEGER NOT NULL,           -- for the idle timeout
  client_ip    TEXT,
  user_agent   TEXT
);
CREATE INDEX sessions_expires_at ON sessions(expires_at);
