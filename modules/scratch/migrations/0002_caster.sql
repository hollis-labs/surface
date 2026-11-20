-- Caster: the ref an item is kept under there, the updated_at of the version Caster has, and
-- why the last write failed. passed: refs in Caster's scratch namespace that were looked at and
-- are not scratch items (too large, unreadable), so they are not read again.
ALTER TABLE items ADD COLUMN caster_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN synced_at TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';
CREATE TABLE passed (ref TEXT PRIMARY KEY);
