-- Where the event stream was read up to: the last seq handled (resumes with Last-Event-ID).
CREATE TABLE cursor (one INTEGER PRIMARY KEY CHECK (one = 1), seq INTEGER NOT NULL);

-- Each approval shown, and the card it is shown on. Nothing a secret form takes is kept here
-- or anywhere in this module: the value goes from the page straight to the producer's intake.
CREATE TABLE shown (
  approval_id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
