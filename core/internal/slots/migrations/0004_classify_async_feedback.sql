-- The classifier's async answer for a turn (a decision provider's vote joined to the rest),
-- '' until it arrives or when the async tier is off.
ALTER TABLE context_turns ADD COLUMN classified_async TEXT NOT NULL DEFAULT '';

-- The owner's answer to a classifier hint on a draft: accepted or dismissed. A label the
-- classifier learns from; it belongs to the draft, not to a turn.
CREATE TABLE classify_feedback (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	at       TEXT NOT NULL,
	text     TEXT NOT NULL,
	facet    TEXT NOT NULL,
	label    TEXT NOT NULL,
	accepted INTEGER NOT NULL,
	actor    TEXT NOT NULL DEFAULT ''
);
