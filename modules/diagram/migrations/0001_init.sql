-- Diagrams: a body of JSON (nodes, edges, groups) with every save kept as a revision.
CREATE TABLE diagrams (
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  rev        INTEGER NOT NULL,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE revisions (
  diagram_id TEXT NOT NULL REFERENCES diagrams(id) ON DELETE CASCADE,
  n          INTEGER NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (diagram_id, n)
);
CREATE INDEX diagrams_updated ON diagrams (updated_at);
