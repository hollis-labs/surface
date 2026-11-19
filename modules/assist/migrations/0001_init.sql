-- What was done in the owner's name, written before it is done.
CREATE TABLE activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  kind TEXT NOT NULL,
  summary TEXT NOT NULL,
  with_whom TEXT NOT NULL DEFAULT '',
  ref TEXT NOT NULL DEFAULT '',
  asked INTEGER NOT NULL DEFAULT 0,
  detail TEXT NOT NULL DEFAULT '{}'
);

-- The owner's mail as the sweep sorted it.
CREATE TABLE mail (
  id TEXT PRIMARY KEY,
  account TEXT NOT NULL,
  from_addr TEXT NOT NULL,
  from_name TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  subject TEXT NOT NULL,
  start TEXT NOT NULL DEFAULT '',
  at TEXT NOT NULL,
  class TEXT NOT NULL,
  ask TEXT NOT NULL DEFAULT '',
  why TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'open',
  card TEXT NOT NULL DEFAULT '',
  handed_at TEXT NOT NULL DEFAULT '',
  seen_at TEXT NOT NULL
);
CREATE INDEX mail_open ON mail(state, class);

-- What the assistant sent in the owner's name, so the answer is known for what it is.
CREATE TABLE sent (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  about TEXT NOT NULL DEFAULT '',
  to_addr TEXT NOT NULL DEFAULT '',
  at TEXT NOT NULL
);

-- The owner's rules: mail that matches goes to class. Empty fields match anything.
CREATE TABLE rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  from_addr TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT '',
  subject_has TEXT NOT NULL DEFAULT '',
  class TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

-- When the sweep first ran: mail already waiting then is left as the owner had it.
CREATE TABLE swept (first_at TEXT NOT NULL);
