ALTER TABLE sent ADD COLUMN account TEXT NOT NULL DEFAULT '';
ALTER TABLE sent ADD COLUMN task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sent ADD COLUMN alert_text TEXT NOT NULL DEFAULT '';
ALTER TABLE sent ADD COLUMN alert_card TEXT NOT NULL DEFAULT '';
CREATE TABLE sent_events (
  event_key TEXT PRIMARY KEY,
  message_id TEXT NOT NULL REFERENCES sent(id),
  at TEXT NOT NULL,
  seq INTEGER NOT NULL DEFAULT 0,
  kind TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  task_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sent_events_message ON sent_events(message_id, at, seq);
CREATE TABLE lifecycle_cursor (source TEXT PRIMARY KEY, seq INTEGER NOT NULL, warning TEXT NOT NULL DEFAULT '');
