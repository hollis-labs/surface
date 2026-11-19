-- What needs the owner, or has not been seen by them yet (docs/attention.md).
CREATE TABLE attend_items (
	subject TEXT PRIMARY KEY,
	item    TEXT NOT NULL
);
