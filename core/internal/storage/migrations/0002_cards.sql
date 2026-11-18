-- Cards: things shown to the user outside a window (a notice, a question, a request to
-- approve, progress), with what they answered.
CREATE TABLE cards (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL,
  version      TEXT NOT NULL DEFAULT '1',
  title        TEXT NOT NULL DEFAULT '',
  source       TEXT NOT NULL DEFAULT '',
  subject      TEXT NOT NULL DEFAULT '',
  risk         TEXT NOT NULL DEFAULT '',
  blocking     INTEGER NOT NULL DEFAULT 0,
  payload      TEXT NOT NULL DEFAULT '{}',
  status       TEXT NOT NULL DEFAULT '',
  outcome      TEXT NOT NULL DEFAULT 'null',
  revision     INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  responded_at TEXT NOT NULL DEFAULT '',
  expires_at   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX cards_open ON cards (status, created_at);
