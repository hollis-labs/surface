-- What comes back: the Designer's replies, the task's state, and the ids of what went out.
ALTER TABLE rounds ADD COLUMN message_id TEXT NOT NULL DEFAULT '';

ALTER TABLE jobs ADD COLUMN head_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN task_status TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN task_comment TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN task_checked_at TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN inbox_checked_at TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN preview_announced INTEGER NOT NULL DEFAULT 0;

CREATE TABLE replies (
  message_id  TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
  from_urn    TEXT NOT NULL,
  subject     TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL DEFAULT '',
  sent_at     TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL,
  preview_url TEXT NOT NULL DEFAULT '',
  base_url    TEXT NOT NULL DEFAULT '',
  pr_url      TEXT NOT NULL DEFAULT '',
  head_sha    TEXT NOT NULL DEFAULT '',
  ignored     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX replies_job ON replies (job_id, sent_at);

-- A job whose preview was set before this: its window was opened then; do not announce it again.
UPDATE jobs SET preview_announced = 1 WHERE preview_url <> '';
