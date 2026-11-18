CREATE TABLE flows (
  name        TEXT PRIMARY KEY,
  goal        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  version     INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE TABLE flow_versions (
  name       TEXT NOT NULL REFERENCES flows (name) ON DELETE CASCADE,
  version    INTEGER NOT NULL,
  layout     TEXT NOT NULL,
  note       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (name, version)
);
