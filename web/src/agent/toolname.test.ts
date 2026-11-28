import assert from "node:assert/strict"
import { test } from "node:test"
import { baseToolName } from "./toolname.ts"

test("baseToolName strips MCP server prefixes and titles", () => {
  assert.equal(baseToolName("card_ask"), "card_ask")
  assert.equal(baseToolName("mcp__basalisk__card_ask"), "card_ask")
  assert.equal(baseToolName("mcp__my_server__cite_resolve"), "cite_resolve")
  assert.equal(baseToolName("basalisk: card_show"), "card_show")
  assert.equal(baseToolName("Read /etc/hosts"), "Read /etc/hosts")
  assert.equal(baseToolName("`ls -la`"), "`ls -la`")
})
