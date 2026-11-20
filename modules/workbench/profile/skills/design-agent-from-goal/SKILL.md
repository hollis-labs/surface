---
name: design-agent-from-goal
description: "Turn a goal or a module into an agent design the owner can approve in one read, with each part justified and pending features marked."
---
# Design an agent from a goal

1. **Say what you are making**: a module profile or a standalone agent. Default: if a Surface module is named, it is a profile.
2. **Research first.** Send the `researcher` variant the module's path or the goal, with a return spec, for example: "views, tools with their levels, events it publishes, data it owns, plans, open questions". Read its findings, not the raw files.
3. **Write the design** in this order. Each line says what, and why it earns its place.

```
## <name>: design
Purpose: <one sentence in the owner's words>
Delivers: module profile at modules/<m>/profile/ | standalone agent
Identity (agent.md): who it is, 3-6 standing rules, what it must never do
Context (always loaded, keep small): <files, about how many words each>
Skills (fetched on demand): <name: when the model should reach for it>
Modes (presets): <name: instructions, reflexes, tools top of mind, output style>
Reflexes: <name: trigger -> action, why it must hold every time, cooldown>
Variants: <name: role, read_only?, budgets>
Tools: top of mind <the 3-6 that matter>; MCP <servers>; tool packages <later>
Providers: chat <model>; summarizer, subtask, desk <if different>
Permissions: <what asks, what is allowed outright>
Front desk: intake <off|shadow|suggest|auto>, delivery policy changes
Auth needs: <audience: scopes> (the ADE's one grant covers these; list, don't configure)
Evals: <3-6 cases, what each proves>
Pending: <parts waiting on Nanite 0.7.0 or Surface profiles>
```

4. **Keep it lean.** One identity, two or three skills, and reflexes only for invariants. A design that needs ten skills is two agents.
5. **Stop for the owner's approval** before building, unless they said to go ahead.

**Worked example** (abridged): a notes module profile. Purpose: "help me keep notes tidy and findable". Context is one file, `notes-conventions.md`, about 150 words. Skills: `file-a-note` and `merge-duplicates`. Mode: `tidy` (instructions: "propose merges, never delete"; reflex `confirm-delete`). One reflex: `confirm-delete`, a text trigger `(?i)\bdelete\b` that reminds "list what would be lost and ask with card_ask". Variant: `deduper`, `read_only`. Evals: it finds the duplicate; it asks before deleting.
