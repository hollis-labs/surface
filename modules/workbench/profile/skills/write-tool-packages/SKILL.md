---
name: write-tool-packages
description: "Design a tool package (tools/<name>/tool.yaml plus an entry) for work no existing tool covers, with the least access, and know it waits for Nanite 0.7.0."
---
# Write tool packages

A tool package is a tool written for the agent: `tools/<name>/tool.yaml` plus an executable entry. It is on Nanite main (0.7.0); the Workbench doesn't author `tools/` yet. Design it now, and build it once the Workbench supports it.

```yaml
# tools/queue-peek/tool.yaml
description: Show the oldest items waiting in the work queue.
version: 0.1.0
kind: command            # JSON args on stdin, a JSON result on stdout; or mcp (a stdio MCP server)
entry: run.sh            # executable, inside the package; not a symlink
input_schema:
  properties:
    limit: {type: integer}
timeout: 30s             # default 60s, at most 10m
background: false        # true: runs as a background job; its finish wakes the agent
network: true            # declared to the approver; not enforced
env: [QUEUE_URL]         # host variables passed through; no credential-like names
needs:                   # short-lived Warden tokens, one variable each
  - audience: queue
    scopes: [queue.read]
    env: QUEUE_TOKEN
```

**Rules**
- **Approval:** nothing runs until the owner approves, and the approval is for this exact content; any edit asks again. Package tools never count as read-only.
- **Credentials:** never put one in `env`; ask for a token through `needs`. The package never sees the agent's own credential.
- **Not a sandbox:** a package runs as the agent's OS user. Only build what you would run yourself.
- **Watchers:** `background: true`, and exit when the condition is met. The finish wakes the agent.

**Build a package only when** a module or MCP tool doesn't already do the job and the work recurs. Prefer the module's own tools.
