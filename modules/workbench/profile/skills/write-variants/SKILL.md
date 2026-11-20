---
name: write-variants
description: "Write subtask variants (variants/<name>.md) with exact front matter, narrow tools and budgets, and a role prompt with a clear return contract."
---
# Write variants

A variant is a kind of subtask: a role prompt plus narrowing. It never gains a tool the agent lacks.

```markdown
---
description: Reads a module's code and docs and returns its views, tools, events, data and plans. Changes nothing.
read_only: true
tools:
  deny: ["mcp__*"]
budgets:
  steps: 25
  wall: 10m
last_n: 2
---
You are a research subtask. Read what the goal names. Return only what the return spec asks for, each finding with the file or record it came from. Never paste raw tool output. If you cannot finish, return partial with what is missing. End by calling return_result.
```

**Front matter** (all optional; unknown keys are errors):
- `description`: the model chooses variants by it. Say what it does and what it returns.
- `tools.allow` and `tools.deny`: names or globs.
- `read_only`: offers only read-only tools.
- `provider` and `model`: `provider` needs `model`.
- `budgets`: `steps`, `tokens`, `wall`, `cost` (not enforced).
- `last_n`: 1-50.
- `result_cap`.
- `can_spawn`: a lead that starts subtasks one level down.

**Rules**
- `read_only` and `can_spawn` can't be combined.
- The name: lower-case letters, digits and dashes, starting with a letter, at most 40 characters. Not `chat`, `code`, `research`, `worker` or `planner`.
- The built-ins already cover most needs: `research` (findings with sources), `planner` (a plan, no MCP tools), `code`, `worker`. Write your own only for a distinct role or tighter limits.
- Keep `last_n` small for focused work. It saves tokens and keeps the child on its goal.
