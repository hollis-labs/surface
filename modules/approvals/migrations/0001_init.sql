-- Where each Gatecrasher's event stream was read up to: the last seq handled (resumes with
-- Last-Event-ID). Keyed by the Gatecrasher's address, so pointing at another starts over.
CREATE TABLE cursor (url TEXT PRIMARY KEY, seq INTEGER NOT NULL);

-- Each approval shown, and the card it is shown on. What a card's answer is checked against
-- (the scopes requested, a secret request's intake) is kept here from Gatecrasher's own card,
-- never read back from the card, which others can edit. Nothing a secret form takes is kept
-- here or anywhere in this module: the value goes from the page straight to the intake.
CREATE TABLE shown (
  approval_id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  title TEXT NOT NULL,
  scopes TEXT NOT NULL DEFAULT '[]',
  post_url TEXT NOT NULL DEFAULT '',
  field TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
CREATE INDEX shown_card ON shown(card_id);
