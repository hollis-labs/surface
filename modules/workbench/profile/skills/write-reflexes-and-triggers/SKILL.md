---
name: write-reflexes-and-triggers
description: "Write reflexes (trigger plus action) in the exact agent/steer v0.1.0 JSON, choosing trigger and action kinds that are allowed together."
---
# Write reflexes and triggers

A reflex is a standing rule: a trigger and an action, kept as configuration. Use one only for something that must hold every time. Judgment belongs in instructions.

```json
{"name": "confirm-delete",
 "about": "Before deleting, list what would be lost and ask.",
 "trigger_kind": "text", "trigger": {"pattern": "(?i)\\b(delete|remove|drop)\\b"},
 "action_kind": "remind", "action": {"body": "List exactly what would be lost, then ask with card_ask before deleting."},
 "cooldown_seconds": 120, "enabled": true}
```

**Fields**
- `name`: matches `^[a-z0-9][a-z0-9._-]{0,63}$`, unique in the file.
- `about`: what it is for, for the owner.
- `trigger_kind` and `trigger`; `action_kind` and `action`.
- `cooldown_seconds`: how long it stays quiet after firing.
- `enabled`: `true` means on everywhere. Otherwise it is on only while a preset lists it.

**Triggers**
- `text` `{"pattern": "<Go regexp>"}`: the owner's message. Anchor slash commands (`^/review\\b`). Use `(?i)` for case.
- `focus` `{"view": "<view>"}`: a window of that view comes in front. Its reminder rides on the next message.
- `event` `{"topic": "<topic>"}`: an app bus event, matched on prefix (`notes.saved`, `ade.guide.progress`).
- `predicate` `{...}`: a libs agent/core/reflexes predicate over the conversation, such as a loop detector. Build it as a conjunction of several signals; one signal alone fires falsely.

**Actions**
- `remind` `{"body": "..."}`: tells the agent with the message. The common case.
- `halt` `{"message": "..."}`: answers instead of the agent; no model call. **Only with a `text` or `predicate` trigger.** Use it for hard stops, for example `^/stop`.
- Surface only:
  - `run_command` `{"command": "<id>", "args": {}}`: runs an app command;
  - `call_tool` `{"tool": "<name>", "input": {}}`: only `read` and `act` level tools. A reflex never makes a write.

**Checks before you save**
- Never pair `halt` with a `focus` or `event` trigger; it is refused.
- Give every reflex that fires on ordinary words a cooldown.
- If a reflex is meant for one way of working, leave `enabled` off and list it in that preset.
- **In the Workbench today**, reflexes are validated and saved but not applied (they need Nanite 0.7.0). Say so when you deliver them.

Families worth considering (from the ADE audit): a loop and drift monitor (predicate, then remind, then halt); armed-not-run for risky commands (remind to show the command and ask); self-follow-up on a deliverable (event, then remind).
