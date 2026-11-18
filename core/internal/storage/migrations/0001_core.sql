-- The core's own tables. Modules keep theirs in their own databases.
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE desk_state (
  id    INTEGER PRIMARY KEY CHECK (id = 1),
  state TEXT NOT NULL
);
CREATE TABLE layouts (
  name       TEXT PRIMARY KEY,
  body       TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE marks (
  id         TEXT PRIMARY KEY,
  ref        TEXT NOT NULL,
  anchor     TEXT NOT NULL DEFAULT 'null',
  quote      TEXT NOT NULL DEFAULT '',
  body       TEXT NOT NULL,
  author     TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'open',
  created_at TEXT NOT NULL,
  replies    TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX marks_ref ON marks (ref, created_at);
