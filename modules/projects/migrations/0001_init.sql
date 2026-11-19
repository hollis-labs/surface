-- The owner's projects: what selects each, where its state lives, their standing instructions,
-- and the agent's own note.
CREATE TABLE projects (
  name TEXT PRIMARY KEY,
  def TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  note_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

-- The project the agent is on: chosen by a window, or by hand.
CREATE TABLE current (one INTEGER PRIMARY KEY CHECK (one = 1), name TEXT NOT NULL, why TEXT NOT NULL, at TEXT NOT NULL);
