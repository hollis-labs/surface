-- Caster (sync.go): the ref an item is kept under there and the revision it is at, the
-- updated_at of the version Caster has, why the last write failed, Caster's tags this module
-- does not manage (kept on every rewrite), and the revision of a change made in Caster while
-- this item had changes of its own (a conflict: neither is written over). passed: refs in
-- Caster's scratch namespace that were looked at and are not scratch items.
ALTER TABLE items ADD COLUMN caster_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN caster_rev INTEGER NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN synced_at TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN caster_tags TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN conflict_rev INTEGER NOT NULL DEFAULT 0;
CREATE TABLE passed (ref TEXT PRIMARY KEY);
