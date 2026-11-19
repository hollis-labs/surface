-- Feeds the owner follows and their entries, kept as plain text. Ported from Basalisk's feeds.db.
CREATE TABLE feeds (
  id TEXT PRIMARY KEY,
  url TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL DEFAULT '',
  site TEXT NOT NULL DEFAULT '',
  folder TEXT NOT NULL DEFAULT '',
  added_at TEXT NOT NULL,
  fetched_at TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  etag TEXT NOT NULL DEFAULT '',
  modified TEXT NOT NULL DEFAULT ''
);
CREATE TABLE entries (
  id TEXT PRIMARY KEY,
  feed_id TEXT NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid TEXT NOT NULL,
  url TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  published TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  image TEXT NOT NULL DEFAULT '',
  read INTEGER NOT NULL DEFAULT 0,
  starred INTEGER NOT NULL DEFAULT 0,
  saved INTEGER NOT NULL DEFAULT 0,
  UNIQUE(feed_id, guid)
);
CREATE INDEX entries_by_time ON entries(published DESC);
CREATE INDEX entries_by_feed ON entries(feed_id, published DESC);
