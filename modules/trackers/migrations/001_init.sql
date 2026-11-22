CREATE TABLE trackers (
  id         TEXT PRIMARY KEY,
  slug       TEXT NOT NULL UNIQUE,
  title      TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  category   TEXT NOT NULL DEFAULT 'general',
  tags       TEXT NOT NULL DEFAULT '[]',
  lifecycle  TEXT NOT NULL DEFAULT 'active',
  metadata   TEXT NOT NULL DEFAULT '{}',
  extras     TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX trackers_updated ON trackers (updated_at DESC);
CREATE INDEX trackers_category ON trackers (category, lifecycle);

CREATE TABLE tracker_items (
  id         TEXT PRIMARY KEY,
  tracker_id TEXT NOT NULL,
  title      TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  lifecycle  TEXT NOT NULL DEFAULT 'todo',
  category   TEXT NOT NULL DEFAULT '',
  tags       TEXT NOT NULL DEFAULT '[]',
  position   INTEGER NOT NULL DEFAULT 0,
  assignee   TEXT NOT NULL DEFAULT '',
  metadata   TEXT NOT NULL DEFAULT '{}',
  extras     TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (tracker_id) REFERENCES trackers(id) ON DELETE CASCADE
);
CREATE INDEX tracker_items_tracker ON tracker_items (tracker_id, position ASC, created_at ASC);
CREATE INDEX tracker_items_lifecycle ON tracker_items (lifecycle, category);
