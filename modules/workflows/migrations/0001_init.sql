CREATE TABLE briefs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    outcome TEXT NOT NULL,
    user_steps TEXT NOT NULL,
    agent_steps TEXT NOT NULL,
    system_steps TEXT NOT NULL,
    acceptance TEXT NOT NULL,
    stage TEXT NOT NULL CHECK(stage IN ('draft', 'review', 'ready')),
    revision INTEGER NOT NULL,
    updated TEXT NOT NULL
);
