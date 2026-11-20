---
name: providers-and-permissions
description: "Choose models per use and the permission mode and rules, with no literal credentials; know what a module profile leaves to the app."
---
# Providers and permissions

**A module profile has neither file.** The app owns runtime, identity, MCP and permissions. For a profile, list the needs in the design (models per use, tools that should ask) and stop there.

**For a standalone agent:**

```yaml
# providers.yaml
providers:
  gateway:
    wire: anthropic
    base_url: https://gateway.example.test
    auth: bearer
    key_ref: keychain://nanite/provider/gateway
use:
  chat:       {provider: gateway, model: claude-sonnet-5-5}
  summarizer: {provider: gateway, model: claude-haiku-5-5}
  subtask:    {provider: gateway, model: claude-sonnet-5-5}
  desk:       {provider: gateway, model: claude-haiku-5-5}
```

**Providers**
- A use without an entry falls back to `chat`.
- `anthropic` and `openai` are built-in names; don't redefine them.
- The OpenAI wires take a `base_url` with `/v1`; `anthropic` takes it without.
- Credentials only by reference: `key_ref` (`keychain://`, `file://`, `env://`) or `key_env`. A credential-like header name is refused.
- `auth: warden` with `audience` and `scopes` is on Nanite main, not the Workbench's v0.6.0 yet.
- A cheaper model for `summarizer` and `desk` is usually right. Keep `chat` on the model the owner chose.

```yaml
# permissions.yaml
permissions:
  mode: default
  rules:
    - tool: mcp__notes__*
      behavior: ask
    - tool: write_file
      pattern: "*.lock"
      behavior: deny
```

**Permissions**
- Modes:
  - `default`: read-only tools run; changes ask.
  - `accept-edits`: file edits run too.
  - `plan`: read-only only.
  - `yolo`: everything runs. Never propose it for an owner-facing agent.
- Rules: deny beats ask, which beats allow.

**Writing these files:** `workbench_write_config` writes both, and the owner is asked each time. Say why in the note.
