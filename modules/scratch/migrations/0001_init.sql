CREATE TABLE items (
  id         TEXT PRIMARY KEY,
  text       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'note',
  done       INTEGER NOT NULL DEFAULT 0,
  tags       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);
CREATE INDEX items_created ON items (created_at);
