-- The durable capture queue. A row is written before anything is sent, so a capture is never
-- lost and capturing works offline. states: queued, needs_attention, saved, discarded.
CREATE TABLE submissions (
  sequence             INTEGER PRIMARY KEY AUTOINCREMENT,
  capture_id           TEXT NOT NULL UNIQUE,
  payload_hash         TEXT NOT NULL,
  payload              TEXT NOT NULL,
  owner_context        TEXT NOT NULL DEFAULT '',
  state                TEXT NOT NULL,
  code                 TEXT NOT NULL,
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL,
  manifest             TEXT NOT NULL DEFAULT '',
  delivery_origin      TEXT NOT NULL DEFAULT '',
  upstream_capture_id  TEXT NOT NULL DEFAULT '',
  receipt              TEXT NOT NULL DEFAULT '',
  fragment_id          TEXT NOT NULL DEFAULT '',
  fragment_revision_id TEXT NOT NULL DEFAULT '',
  attempts             INTEGER NOT NULL DEFAULT 0,
  next_attempt         INTEGER NOT NULL DEFAULT 0,
  lease_token          TEXT NOT NULL DEFAULT '',
  lease_until          INTEGER NOT NULL DEFAULT 0,
  may_have_sent        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX submissions_due ON submissions (state, next_attempt, lease_until);

-- The capture being written: one, kept until it is committed.
CREATE TABLE draft (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  content    TEXT NOT NULL DEFAULT '',
  tags       TEXT NOT NULL DEFAULT '',
  draft_at   TEXT NOT NULL DEFAULT '',
  capture_id TEXT NOT NULL DEFAULT '',
  id_for     TEXT NOT NULL DEFAULT '',
  context    TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);
