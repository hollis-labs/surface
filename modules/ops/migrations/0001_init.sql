-- The working copy of Ops' documents in Caster (contract ops/v1, namespace "ops"), the activity
-- read for each workstream, and seed previews waiting on the owner. Caster is the record:
-- deleting this database loses nothing that is not read again.
CREATE TABLE docs (
	name TEXT PRIMARY KEY,         -- ws.<id> or queue
	rev INTEGER NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL,
	updated TEXT NOT NULL DEFAULT '', -- Caster's time of that revision
	fetched_at TEXT NOT NULL
);
CREATE TABLE activity (
	ws TEXT PRIMARY KEY,           -- the workstream's id
	body TEXT NOT NULL,            -- JSON: the roll-up and the merged feed
	fetched_at TEXT NOT NULL
);
CREATE TABLE seed_plans (
	id TEXT PRIMARY KEY,
	created_at TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT '',
	plan TEXT NOT NULL,            -- JSON: the preview, as the owner last edited it
	applied_at TEXT NOT NULL DEFAULT ''
);
