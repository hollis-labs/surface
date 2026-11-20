---
name: adapt-agent-to-module
description: "Fit an agent to a Surface module: read its manifest, views, tools and their levels, slot and plans, then decide what goes in the profile versus what the module already provides."
---
# Adapt an agent to a module

A module already shapes the app's agent through its **slot**:
- `About`, `Views`, `Modes`;
- `Tools` top of mind;
- `Instructions`, `Where`;
- live `Knowledge`.

It can also add `Around` lines and errands. A profile adds who the agent is when that module leads. Don't repeat what the slot says.

1. **Read the module** with the `researcher` variant. Ask it for:
   - the manifest (views, commands, tools with levels, settings, slot);
   - the events it publishes;
   - the data it owns and where it lives;
   - its design doc or plan;
   - what the agent may never do (tools left out on purpose, `confirm`-level tools).
2. **Map the levels.** `read` and `act` run. `write` asks, and the owner can approve for good. `confirm` asks every time. A profile cannot loosen these. Design so the agent proposes and the owner approves.
3. **Decide placement:**
   - **agent.md:** identity and invariants. Write it to work alone, because some runtimes load only agent.md and context.
   - **context/:** the module's vocabulary and states in a few hundred words, not its docs.
   - **skills:** procedures the agent follows sometimes (seeding, a review pass, a handoff).
   - **presets:** the module's ways of working, if they differ.
   - **reflexes:** invariants tied to the module's views (`focus`) or events (`event`).
4. **Respect what the design forbids.** If the module never launches work, the profile must say so in agent.md and keep a reflex as a backstop, for example a text trigger on "launch" that reminds the agent to request, not launch.
5. **Evals:** cases over fixtures that copy the module's documents. Tools can't be stubbed yet.

**Worked example: an Operations module.** The module design describes:
- a workstream board with lanes from planning to active, and a separate ordered Queued band;
- a drill-in with Overview, Tasks, Activity and History;
- tools `ops_list`, `ops_get`, `ops_activity` (read) and `ops_create`, `ops_update`, `ops_move`, `ops_queue_reorder`, `ops_link` (write; the owner approves);
- `ops_request_agent` and `ops_launch_request`, which file a task or send a request but never launch;
- `ops_drop` (confirm).

Its profile:
- **agent.md:** "the board is the owner's"; propose moves, never launch; to start work, request it.
- **context:** a states and lanes cheat-sheet.
- **skills:** `seed-the-board`, `weekly-review`, `request-a-launch`.
- **presets:** `triage` (what needs a decision) and `review` (in-flight health).
- **reflexes:**
  - `no-launch`: text `(?i)\blaunch|start (the )?(team|agent)\b`, remind to use `ops_launch_request`;
  - `held-check`: event `ops.changed`, remind to note why when a workstream becomes held.
- **evals:** it proposes a move and doesn't make it; asked to launch, it requests; it reads the queue in order.
