-- The working copy of the Work items in Caster (contract work/v1), and the owner's draft
-- notes not yet sent. Caster is the record; nothing lives only here once it is there.
CREATE TABLE items (
	ns TEXT NOT NULL,             -- "work" (the index) or w-<workspace>
	name TEXT NOT NULL,           -- i.<id>, kind.<kind>, v.<id>, r.<id>.<n>; <workspace> in "work"
	rev INTEGER NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL,
	updated TEXT NOT NULL DEFAULT '', -- Caster's time of that revision
	fetched_at TEXT NOT NULL,
	PRIMARY KEY (ns, name)
);
CREATE TABLE notes (
	id TEXT PRIMARY KEY,
	ws TEXT NOT NULL,
	item TEXT NOT NULL DEFAULT '',
	item_rev INTEGER NOT NULL DEFAULT 0,
	kind TEXT NOT NULL,             -- comment, suggestion
	section TEXT NOT NULL DEFAULT '',
	quote TEXT NOT NULL DEFAULT '',
	replace TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	round INTEGER NOT NULL DEFAULT 0 -- 0: not sent
);
CREATE INDEX notes_item ON notes(ws, item, round);
