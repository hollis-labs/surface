CREATE TABLE reflexes (
	name         TEXT PRIMARY KEY,
	about        TEXT NOT NULL DEFAULT '',
	trigger_kind TEXT NOT NULL,
	trigger      TEXT NOT NULL,
	action_kind  TEXT NOT NULL,
	action       TEXT NOT NULL,
	cooldown     INTEGER NOT NULL DEFAULT 0,
	enabled      INTEGER NOT NULL DEFAULT 0,
	last_fired   TEXT NOT NULL DEFAULT '',
	fired        INTEGER NOT NULL DEFAULT 0
);
