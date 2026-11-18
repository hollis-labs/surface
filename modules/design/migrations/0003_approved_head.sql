-- An approval names the commit it covers. approval_card and approval_head are the open
-- approval card and the head it shows; approved_head and approved_at what was approved.
ALTER TABLE jobs ADD COLUMN approval_card TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN approval_head TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN approved_head TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN approved_at TEXT NOT NULL DEFAULT '';
-- A reply that arrived after the approval naming another commit.
ALTER TABLE replies ADD COLUMN note TEXT NOT NULL DEFAULT '';
-- Work came back (a head or a pull request) without a preview to review yet.
ALTER TABLE jobs ADD COLUMN update_received INTEGER NOT NULL DEFAULT 0;
