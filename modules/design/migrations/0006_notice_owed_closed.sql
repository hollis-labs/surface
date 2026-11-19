-- notice_card: the open "not approvable yet" notice, ended once it no longer stands.
-- approval_owed: the owner is to be asked again once GitHub shows the Designer's head.
-- closed_reason: why the job was closed (the Designer's closed: line, or by hand).
ALTER TABLE jobs ADD COLUMN notice_card TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN approval_owed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN closed_reason TEXT NOT NULL DEFAULT '';
