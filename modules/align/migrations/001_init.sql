CREATE TABLE align_proposals (
  id            TEXT PRIMARY KEY,
  title         TEXT NOT NULL,
  summary       TEXT NOT NULL DEFAULT '',
  subtype       TEXT NOT NULL DEFAULT 'architecture',
  status        TEXT NOT NULL DEFAULT 'proposed',
  payload       TEXT NOT NULL DEFAULT '{}',
  decision_note TEXT NOT NULL DEFAULT '',
  rev           INTEGER NOT NULL DEFAULT 1,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL,
  decided_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX align_proposals_updated ON align_proposals (updated_at DESC);
CREATE INDEX align_proposals_subtype ON align_proposals (subtype, status);

CREATE TABLE align_proposal_revisions (
  proposal_id TEXT NOT NULL,
  rev         INTEGER NOT NULL,
  title       TEXT NOT NULL,
  summary     TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL,
  payload     TEXT NOT NULL,
  note        TEXT NOT NULL DEFAULT '',
  actor       TEXT NOT NULL DEFAULT 'owner',
  created_at  TEXT NOT NULL,
  PRIMARY KEY (proposal_id, rev),
  FOREIGN KEY (proposal_id) REFERENCES align_proposals(id) ON DELETE CASCADE
);

CREATE TABLE align_droppads (
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  mode       TEXT NOT NULL DEFAULT 'collection',
  target_dir TEXT NOT NULL DEFAULT '',
  operation  TEXT NOT NULL DEFAULT 'reference',
  status     TEXT NOT NULL DEFAULT 'active',
  items      TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX align_droppads_updated ON align_droppads (updated_at DESC);
