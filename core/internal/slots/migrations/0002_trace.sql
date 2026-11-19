-- The context trace: what each turn put in front of the agent and why, and what the owner
-- did after it. It is the record a classifier-driven Selector learns from (docs/agent.md).
CREATE TABLE context_turns (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	at          TEXT NOT NULL,
	run_id      TEXT NOT NULL DEFAULT '',
	text        TEXT NOT NULL,
	view        TEXT NOT NULL DEFAULT '',
	module      TEXT NOT NULL DEFAULT '',
	mode        TEXT NOT NULL DEFAULT '',
	layout      TEXT NOT NULL DEFAULT '',
	open_views  TEXT NOT NULL DEFAULT '[]',
	slot        TEXT NOT NULL,
	slots       TEXT NOT NULL DEFAULT '[]',
	why         TEXT NOT NULL DEFAULT '',
	top_of_mind TEXT NOT NULL DEFAULT '[]',
	routed      TEXT NOT NULL DEFAULT '',
	fired       TEXT NOT NULL DEFAULT '[]',
	halted      INTEGER NOT NULL DEFAULT 0,
	block_chars INTEGER NOT NULL DEFAULT 0,
	trimmed     TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE context_signals (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	at      TEXT NOT NULL,
	turn_id INTEGER REFERENCES context_turns(id) ON DELETE CASCADE,
	kind    TEXT NOT NULL,
	actor   TEXT NOT NULL DEFAULT '',
	detail  TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX context_signals_turn ON context_signals(turn_id);
