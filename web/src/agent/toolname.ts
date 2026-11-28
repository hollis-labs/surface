/**
 * The tool's own name. An agent that reaches a Surface app's tools over MCP (a seat run by a
 * seat host, say) reports them as `mcp__<server>__<tool>`, or titled `<server>: <tool>` (Claude's
 * ACP adapter); cards and labels key on `<tool>`.
 */
export function baseToolName(name: string): string {
  const mcp = /^mcp__[^_](?:[^_]|_(?!_))*__([a-z][a-z0-9_]*)$/.exec(name)
  if (mcp) return mcp[1]
  const titled = /^[a-z0-9][a-z0-9._-]*: ([a-z][a-z0-9_]*)$/.exec(name)
  return titled ? titled[1] : name
}
