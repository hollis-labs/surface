CREATE TABLE jobs (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  brief       TEXT NOT NULL DEFAULT '',
  refs        TEXT NOT NULL DEFAULT '',
  branch      TEXT NOT NULL DEFAULT '',
  pr_url      TEXT NOT NULL DEFAULT '',
  preview_url TEXT NOT NULL DEFAULT '',
  base_url    TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'requested',
  task_id     TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX jobs_updated ON jobs (updated_at);

CREATE TABLE rounds (
  job_id     TEXT NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
  n          INTEGER NOT NULL,
  asked      TEXT NOT NULL,
  sent       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, n)
);
