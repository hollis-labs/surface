-- Who asked: an outside agent (a seat over MCP) that named itself, and its handle for
-- routing a late answer back. Empty for the embedded agent and for modules.
ALTER TABLE cards ADD COLUMN agent TEXT NOT NULL DEFAULT '';
ALTER TABLE cards ADD COLUMN reply_to TEXT NOT NULL DEFAULT '';
