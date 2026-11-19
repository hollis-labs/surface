-- Late answers: an agent's question card whose run ended (abandoned) or whose wait ran out
-- (expired) can still be answered once. late marks that answer; delivered_at is when it
-- reached the agent as a message ('' until then).
ALTER TABLE cards ADD COLUMN late INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cards ADD COLUMN delivered_at TEXT NOT NULL DEFAULT '';
CREATE INDEX cards_late ON cards (late, status, delivered_at);
