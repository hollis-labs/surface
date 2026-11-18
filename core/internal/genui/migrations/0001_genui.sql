-- Generated UI: the surfaces shown (immutable snapshots) and the journal of actions taken
-- on them. A surface is never edited; an action's request and digest never change.
CREATE TABLE genui_surfaces (
  namespace  TEXT NOT NULL,
  actor      TEXT NOT NULL,
  id         TEXT NOT NULL,
  generation TEXT NOT NULL,
  snapshot   BLOB NOT NULL,
  superseded INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (namespace, actor, id)
);
CREATE TABLE genui_actions (
  namespace TEXT NOT NULL,
  actor     TEXT NOT NULL,
  id        TEXT NOT NULL,
  revision  INTEGER NOT NULL,
  digest    TEXT NOT NULL,
  request   BLOB NOT NULL,
  claimed   INTEGER NOT NULL DEFAULT 0,
  record    BLOB NOT NULL,
  PRIMARY KEY (namespace, actor, id)
);
