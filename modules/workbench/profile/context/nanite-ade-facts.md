# Nanite and ADE facts (reference)

## What the Workbench edits today
It edits Nanite v0.6.0 agents (libs agentdir v0.2.0). Authored files:
- `agent.md` (schema-2 front matter: `schema_version`, `definition_id`, `revision`, `name`, `description`, `behavior.purpose`, `requirements`, `harness_profile.permissions.profile`, `continuity.mode`; the body is the agent's standing instructions);
- `host.yaml` (`kind`/`lifetime`/identity key: `embedded`/`application`/`application_id`, `task`/`until-task-end`/`task_id`, `stable`/`on-demand`/`seat_id`; `runtime.*` limits; `permissions.mode`);
- `.mcp.json`, `permissions.yaml`, `providers.yaml`;
- `context/<n>.md`: always in context, every turn, so keep it small;
- `skills/<n>/SKILL.md`: front matter `name`, `description`; the body is fetched on demand with `skill_get`, and the model picks skills by `description`;
- `variants/<n>.md`.

Sidecar: `workbench/steering.json`, `workbench/options.json` (`compact_at`), `workbench/evals/<suite>.yaml`.

Limits and rules:
- A file is at most 1 MiB; an agent at most 512 files.
- A literal credential is refused anywhere it would be used.

## Not available in the Workbench yet (design them, mark them pending)
- **Reflexes and presets** are saved and validated, but not applied: eval runs don't use them until Nanite 0.7.0 is tagged and the Workbench moves to it.
- **On Nanite main, not released** (Nanite 0.7.0): `identity.yaml` and `auth: warden` providers; tool packages (`tools/<name>/tool.yaml`); the front desk and its recovery; resuming interrupted turns. The Workbench doesn't author these files yet.
- **Surface core:** one agent per app today. Per-module agent profiles are planned (Surface core), as are module-declared skills and reflexes.
- **Evals:** no tool stubs and no MCP servers. Cases have the file tools over their fixtures only. Live runs speak the Anthropic wire only.

## Module profiles (Surface)
A module may bring its own agent profile: a Nanite agent directory at `modules/<module>/profile/`. The app owns the runtime, identity, MCP and permissions, so a profile has no `host.yaml`, `.mcp.json`, `permissions.yaml` or `providers.yaml`.

What a profile holds:
- `agent.md`;
- `context/*.md`;
- `skills/<n>/SKILL.md`;
- `variants/*.md`;
- `tools/<n>/tool.yaml`, if any;
- `steering.json` at the root, holding `presets` and `reflexes` in the shape below;
- `evals/*.yaml`.

It must load with libs agentdir and Nanite's `agent/authored`.

Which profile runs is chosen by the module in front.

For the whole ADE, auth is one identity with one Warden grant (MVP): profiles and their subtasks share it, so list a profile's auth needs and don't configure them.

## Variants (`variants/<name>.md`)
Front matter, all optional; unknown keys are errors:
- `description` (how the model chooses it)
- `tools.allow`, `tools.deny` (names or globs, such as `mcp__*`)
- `read_only`
- `provider` and `model` (`provider` needs `model`)
- `budgets`: `steps`, `tokens`, `wall` (for example `10m`), `cost` (not enforced)
- `last_n` (1-50, default 6)
- `result_cap`
- `can_spawn` (a lead)

The body is the role prompt.

Rules:
- `read_only` and `can_spawn` can't be combined.
- Name: lower-case, digits and dashes, starting with a letter, at most 40 characters. Not `chat`, and not a built-in: `code`, `research`, `worker`, `planner`.
- A variant only narrows: it never adds a tool the agent lacks.

## providers.yaml
`providers.<name>`:
- `wire`: `anthropic`, `openai`, `openai-chat` or `openai-responses`;
- `base_url`;
- `key_ref` (a reference) or `key_env`;
- `auth`: empty, `bearer`, `none`, or later `warden` with `audience` and `scopes`;
- `headers` (no credential-like names);
- `disable_anthropic_beta`, `disable_cache_control`.

`use.<chat|summarizer|subtask|desk>: {provider, model}`. A use without an entry falls back to `chat`. `anthropic` and `openai` are built-in names.

## permissions.yaml
`permissions.mode`: `default`, `accept-edits`, `plan` or `yolo`. `rules: [{tool, pattern?, behavior: allow|ask|deny}]`; deny beats ask, which beats allow.

In Surface, app tools carry levels:
- `read` and `act` run;
- `write` asks (the answer can stand for the conversation or "always");
- `confirm` asks every time.

## steering.json (libs agent/steer v0.1.0)
```json
{"workbench_format": 1,
 "presets": [{"name": "review", "about": "...", "instructions": "...", "reflexes": ["r1"],
              "slots": [], "tools": [], "rules": [], "output_style": "brief", "delivery": "", "dispatch": ["subtask"]}],
 "reflexes": [{"name": "r1", "about": "...", "trigger_kind": "text", "trigger": {"pattern": "(?i)..."},
               "action_kind": "remind", "action": {"body": "..."}, "cooldown_seconds": 60, "enabled": false}]}
```
- **Names:** `^[a-z0-9][a-z0-9._-]{0,63}$`.
- **Triggers:**
  - `text {"pattern"}`: the owner's message;
  - `predicate {...}`: libs agent/core/reflexes predicates over the conversation;
  - `event {"topic"}`: the app bus, prefix match;
  - `focus {"view"}`: a window of that view comes in front.
- **Actions:**
  - `remind {"body"}`;
  - `halt {"message"}`: text and predicate triggers only; answers instead of the agent;
  - in Surface also `run_command {"command"}` and `call_tool {"tool"}` (read and act tools only).
- **Enabling:** `enabled: true` is on everywhere. Otherwise a reflex is on only while a preset lists it. A preset may only name reflexes the file defines.
- **Modes:** a mode is a preset the owner chose; only the owner sets it.
- **Defaults** (`steer.DefaultPresets`): general, focus, triage, research. Layering: app > module > default.

## Eval suites (`workbench/evals/<suite>.yaml`)
`name`, `description`, `mode` (`direct` or `subtask`), and `cases[]`:
- `id`;
- `turns` (the owner's messages);
- `fixtures` (path to content);
- `writes`;
- `script` (an offline stand-in: `calls: [{tool, args}]`, `say`);
- `graders`: `contains`, `not_contains`, `regex`, `answer_line`, `json_path {path, equals|exists}`, `tool_called {tool, args}`, `no_tool_called`, `max_steps`, `max_tokens`, `file {path, contains}`, `rubric` (live only).

An offline run checks the harness, not the agent. Only a live run shows behaviour, and it costs money.

## Surface (the ADE)
- **One agent per app.** Modules shape it through:
  - slots (`About`, `Views`, `Modes`, `Tools` top of mind, `Instructions`, `Where`, `Knowledge`), chosen by the window in front, the mode or a pin;
  - `Around` lines;
  - errands (`Host.Agent().Ask`);
  - the model gateway.
- **The router** runs first (`/open`, `/layout`, `/run`, and the owner's rules), then reflexes, then the selector, then the context block.
- **Asking the owner:** `card_ask`, not `ask_user`. Showing things: `ade_point`, `ade_guide_start`.
- **Desk rules:** give a reason for every window opened; open `transient` for a step and `deliverable` for a result; close your own transient windows; never take focus from the owner's unsaved work; never work around a refusal.
- **Attention:** `needs_you` > `failed` > `unseen` > `working`. Items come from events, never from an agent's claim; to get an answer, show a card.
- **Runners** (`SURFACE_AGENT_RUNNER`): `nanite` (the default) reads `<agents.dir>/<app>/`; `scripted` is the test runner. libs agent/host is removed (Surface 0.4.0).

## Nanite front desk (chatrunner, 0.7.0)
- **Intake:** `off`, `shadow`, `suggest` or `auto` (`AutoConfidence` 0.8); rules, or `PlanWithModel` through the `desk` use.
- **Plans:** items of kind `answer`, `research`, `analysis`, `council`, `delegate` or `question`. Research and analysis run as `research` subtasks; council and delegate as `worker`.
- **Delivery policy:** `now`, `merge` (20s window), `hold` (while a question is open; at most 30m), `digest` (every 30m), `file`.
- **Ledger:** a topic ledger. State survives restarts.
