CREATE TABLE drafts (
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  lang       TEXT NOT NULL DEFAULT 'markdown',
  body       TEXT NOT NULL DEFAULT '',
  rev        INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);
CREATE TABLE revisions (
  draft_id   TEXT NOT NULL REFERENCES drafts (id),
  rev        INTEGER NOT NULL,
  body       TEXT NOT NULL,
  author     TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (draft_id, rev)
);
