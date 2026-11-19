-- Scripts: a guide that runs as onboarding, a recorded demo, or a test (docs/scripts.md).
CREATE TABLE scripts (
	name       TEXT PRIMARY KEY,
	about      TEXT NOT NULL DEFAULT '',
	steps      TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
