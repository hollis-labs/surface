CREATE TABLE route_rules (
	id       TEXT PRIMARY KEY,
	priority INTEGER NOT NULL DEFAULT 0,
	body     TEXT NOT NULL
);
