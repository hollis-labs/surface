-- The owner's standing "always" answers to the MCP approval bridge: this caller may use this
-- write tool without asking again. Kept until revoked.
CREATE TABLE mcp_grants (
  caller     TEXT NOT NULL,
  tool       TEXT NOT NULL,
  granted_at TEXT NOT NULL,
  PRIMARY KEY (caller, tool)
);
