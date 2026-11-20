# Agent Workbench

A Surface module for making and improving Nanite agents: create, edit, version, import and export an agent; run
eval suites against it; ask a model for edits that would fix the failures; accept or reject each; run again and
compare. It is in Studio (`apps/studio/surface.yaml`).

The agent is an **agent directory** as libs `agent/agentdir` v0.2.0 and Nanite v0.6.0 read it. The Workbench keeps
its own state about an agent in a **sidecar** beside the directory, never inside it. Everything Nanite-specific goes
through `Runtime` (`runtime.go`), which uses Nanite's public `agent` and `evals` packages only.

## What is real today, and what is recorded for later

| Part | Today |
|---|---|
| Library, templates, duplicate, delete to trash | real |
| Validation on every save (agentdir.Load, plus Nanite's variant and providers.yaml rules, its limits, literal-secret checks, the sidecar) | real; nothing that does not validate is saved |
| Versions (a full copy, a digest, a note), diff, rollback | real |
| Prompt, skills, tools (.mcp.json, permissions.yaml), variants, providers.yaml, context files, host.yaml runtime knobs | real: these are Nanite's own files |
| `compact_at` | a runtime option: Nanite takes it only in code (`agent.Options.CompactAt`). Kept in the sidecar and passed on every eval run; a host that opens the agent passes it the same way. A host.yaml key comes in a later agentdir release |
| Reflexes, triggers, presets | **steer v0.1.0 config; Nanite applies it via `agent.Options.Steering` from its next release.** libs `agent/steer` v0.1.0's own types and validation, kept in the sidecar (host configuration, not agent-directory files). Surface pins Nanite v0.6.0, which has no steering, so eval runs do not apply them; that is wired in when Nanite 0.7.0 is tagged |
| Base prompt | shown read-only (Nanite's Universal slot) on the Prompt and Assembled prompt tabs |
| Boot prompts and boot profiles | not here: they are Tether catalog concepts, not part of an agent directory |
| Slot budgets, slot placement, the context window, a compaction summary prompt | not offered: engine-owned, set by the model, or not a knob Nanite has |
| Import and export | real: `.tar.gz` export; `.tar.gz`, `.tgz` and `.zip` import; a directory copied in read-only |
| Eval runs, offline | real: Nanite's eval harness with each case's script standing in for the model. A harness check, not evidence |
| Eval runs, live | real, on the owner's action only, through the app's gateway, within a dollar cap and token budgets. Tested only against a fake gateway |
| Improvement proposals | real, on the owner's action only; each edit is accepted or rejected by the owner |

## Layout

```
<root>/<id>/agent/                  the agent directory: what Nanite opens
<root>/<id>/workbench/              the sidecar
    meta.json                       source, template, created
    steering.json                   reflexes, triggers, presets (steer v0.1.0)
    options.json                    runtime options: compact_at
    versions/000001/tree/...        every file of each saved state
    versions/000001/meta.json       n, digest, agentdir digest, note, author, created
    evals/<suite>.yaml              eval suites
    runs/<run>.json                 eval runs
    proposals/<id>.json             improvement proposals
<root>/.trash/<id>-<time>/          deleted agents
```

`<root>` is the setting `workbench.root`, by default `agents/` in the module's data directory. Other settings:
`eval_max_usd` (default 10, at most 50), `eval_turn_tokens` (150,000), `eval_run_tokens` (500,000).

## The agent's files

Authored paths only: `agent.md`, `host.yaml`, `.mcp.json`, `permissions.yaml`, `providers.yaml`, `context/<name>.md`,
`variants/<name>.md`, `skills/<name>/...` (SKILL.md and its resources). Nothing else is read, written, versioned or
exported; in particular never `.nanite/` (conversations, traces, `nanite.db*`, `agent.lock`, `transcripts/`, `jobs/`).
A file is at most 1 MiB, an agent at most 512 files and 16 MiB.

Credentials: a value passes a credential only by reference, when it is entirely `${VARIABLE}`, `Bearer ${VARIABLE}`, or
a `keychain://`, `file://` or `env://` reference. Checked in `.mcp.json` env, headers, arguments (and the value after a
credential flag) and URLs; `host.yaml` `runtime.env`; `providers.yaml` `key_ref`, `base_url` and headers. A field named
like a credential (`*KEY*`, `*TOKEN*`, `*SECRET*`, `*AUTH*`, `*PASSWORD*`, `*_PAT`) with a literal value is refused, and
so is any value shaped like a known credential (`sk-`, `sk-ant-`, `ghp_`/`github_pat_`, `xox?-`, `AKIA…`, a private key,
`user:password@` in a URL, `api_key=`/`token=` in a query, `Bearer` and a 20-character token), in a field's value, in a
command, or behind a reference (`file://sk-…`). A URL with a user part (`token@host`) is refused in configuration and a
warning in prose; `providers.yaml` `key_env` must name a variable. Every other file (prompts, skills and their resources,
context, suites) is scanned too: a known credential shape is a warning when saved and refuses an export; a
credential-like assignment is a warning (sent with an export in `X-Workbench-Warnings`).

Validation, by file, on every save (and as you type, without saving):

- `agentdir.Load` over the directory (agent.md's schema-2 front matter, host.yaml's profile, .mcp.json, skills).
- `variants/*.md` as Nanite v0.6.0 loads them: the name, strict front matter, `read_only` with `can_spawn`,
  `last_n` 1–50, budgets, tool patterns.
- `providers.yaml` with Nanite's own `ProviderSpec.Validate`; `key_ref` must be a reference.
- `runtime.subtask_limit` at most 8 (Nanite refuses the agent above it); a warning above about 5,900 for
  `subtask_result_cap`.
- No literal credential in `.mcp.json` env or headers or `host.yaml` `runtime.env`: use `${VARIABLE}` or a reference
  (`keychain://`, `file://`, `env://`). An export is refused while one is there.

**Digests.** `digest` is the Workbench's SHA-256 over every file of the agent and its sidecar files (steering,
options), length-prefixed in path order like agentdir's own. It covers what `agentdir_digest` (agentdir.Load's)
leaves out: variants, providers.yaml, skill resources and the sidecar. Runs are keyed by `digest`.

## Saving

Before a save or a rollback writes anything, authored files changed on disk by hand since the last version (added,
edited or removed) are kept as a version of their own (`unsaved changes on disk, kept before: <note>`, author `disk`),
so nothing is overwritten without a copy. The new version is then recorded first and the files written after it: if
the app stops in the middle, the last version is what was being saved, the disk may hold a mix of old and new files,
and the next save keeps that mix as its own version before writing; roll back to either to finish.

A save writes the agent's changed authored files in place, each through a temporary file (`.wb-tmp-*`) and a rename,
and removes the authored files the new state lacks. Nothing else in the directory is touched: Nanite's `.nanite/` (`nanite.db*`,
`agent.lock`, `transcripts/`, `jobs/`), notes, backups. The directory itself is never moved, so a host running the agent
from it keeps running. At start the Workbench removes its own interrupted write's temporary files (`.wb-tmp-*` only, never inside a
hidden directory such as `.nanite/` or `.git/`),
puts back an agent directory an older version left aside, and marks runs left `running` as `interrupted`.

## Versions

Every save that changes something writes a version: a full copy of the files, the digest, the agentdir digest, a
note, who saved it (`owner`, or `agent` for a tool) and when. A save names the digest it was edited from and is
refused if the agent changed since. Rollback saves an old version's files as a new version. Accepting a proposal
saves a version whose note names the proposal.

## steering.json (steer v0.1.0 config)

```json
{
  "workbench_format": 1,
  "presets": [
    {"name": "review", "about": "...", "instructions": "...", "reflexes": ["cite-sources"],
     "slots": [], "tools": [], "rules": [], "output_style": "", "delivery": "", "dispatch": ["subtask"]}
  ],
  "reflexes": [
    {"name": "cite-sources", "about": "...",
     "trigger_kind": "text", "trigger": {"pattern": "(?i)\\bsource\\b"},
     "action_kind": "remind", "action": {"body": "Name where each fact came from."},
     "cooldown_seconds": 60, "enabled": false}
  ]
}
```

`presets` and `reflexes` are `steer.Preset` and `steer.Reflex` (libs `agent/steer` v0.1.0), checked with
`Reflex.Validate` against a vocabulary of steer's `remind` and `halt` plus Surface's `run_command` and `call_tool`, and
`Preset.Validate`. Trigger kinds: `text`
(`{"pattern"}`), `predicate` (a libs `agent/core/reflexes` predicate), `event` (`{"topic"}`), `focus` (`{"view"}`).
Action kinds: `remind` (`{"body"}`) and `halt` (`{"message"}`, text and predicate triggers only) are steer's own;
`run_command` (`{"command"}`) and `call_tool` (`{"tool"}`) are the kinds a Surface host registers. A preset may only
name reflexes the file defines. Only the owner sets a mode; no tool does.

## options.json (runtime options)

```json
{"workbench_format": 1, "compact_at": 120000}
```

`compact_at` is `agent.Options.CompactAt`: the request size in tokens (system blocks, tools and conversation) at
which Nanite compacts; 0 is 80% of the model's context window.

## evals/<suite>.yaml

```yaml
name: basics                 # the file's name
description: ...
mode: direct                 # direct (default) or subtask: the nanite/evals arm
cases:
  - id: reads-the-note
    turns:                   # the owner's messages; every one but the last on one line
      - "We are planning the launch."
      - "What does notes/launch.md say the launch date is?"
    fixtures:                # the workspace: path -> content
      notes/launch.md: "The launch moved from 2026-12-01 to 2026-12-08.\n"
    writes: false            # offer the file-writing tools
    script:                  # the stand-in model in an offline run (for the last turn)
      - calls: [{tool: read_file, args: {path: notes/launch.md}}]
      - say: "The launch moved a week.\nANSWER: 2026-12-08"
    graders:                 # one check each
      - contains: "2026-12-08"
      - not_contains: "2026-12-01"
      - regex: "(?m)^ANSWER: "
      - answer_line: "2026-12-08"            # the last "ANSWER:" line, Nanite's convention
      - json_path: {path: "$.status", equals: ok}   # the answer, or its first ```json block
      - json_path: {path: "$.error", exists: false} # absent; equals: null wants a JSON null
      - tool_called: {tool: read_file, args: {path: notes/launch.md}}  # args: a subset
      - no_tool_called: {}                   # or {tool: bash}
      - max_steps: 4
      - max_tokens: 20000                    # the task turn and its subtasks
      - file: {path: out.md, contains: "Reviewed"}  # the workspace after the run
      - rubric: "States the new date."       # model-graded; live runs only
```

Each case becomes a `nanite/evals` scenario: the agent's files (without `.mcp.json`: no MCP server is started in an
eval), the fixtures, `task.md` (the last turn) and `prelude.txt` (the earlier turns). The case's graders are the
scenario's `Grade`. Steps and tokens come from the turn's trace record (libs `agent/trace` v0.2.0: the turn's own
usage plus `subtasks[].usage`); the tool calls and their arguments, and the transcript, from the model's replies.
`compact_at` from options.json is passed as `agent.Options.CompactAt`.

**Every run happens in a child process**: the app's own binary started again with the hidden argument
`workbench-eval` (`child.go`). `nanite/evals` changes the working directory for each case, which the app's server must
never see. The parent sends the request as JSON on stdin (the gateway URL and the key file's path, never the key),
reads progress and the result as JSON lines, and never changes directory. Canceling a run kills the child. The child
gets a minimal environment (`PATH`, `HOME`, the locale, `TMPDIR`), none of the server's other variables, and all its
temporary files go under a root the parent makes and removes once the child has ended, canceled or not. One live run
goes at a time in the app (each run's dollar cap is its own); a second is refused with a message. Offline runs: two at
a time.

**Offline** (the default) plays each case's `script`: earlier turns are answered "Noted.", a subtask returns at once,
no model is called and nothing is spent. **Live** runs only when the owner presses Run live and confirms (or a
confirm-level tool call). The page shows the case count, the model, an estimate of the tokens and the caps first.
The module declares `Manifest.Gateway` to be given the gateway (`Host.Agent().Gateway`). A live run sends every
request through the app's gateway (`model`, `gateway_url`, `key_file` in the app's agent
config; the key is read per request and never logged), with:

- nanite/evals' dollar cap (`eval_max_usd`, default 10, at most 50; each request reserves its most expensive
  outcome at a conservative price before it is sent);
- a per-case token cap (`agent.Options.TurnTokenBudget` = `eval_turn_tokens`);
- a run token budget (`eval_run_tokens`): every request first reserves its most expensive outcome (its input and its
  whole output allowance), so requests running side by side cannot together pass it; a request that would is refused
  and the run is stopped.

Only the owner, at the page, can change anything through the routes: an agent's files, create, duplicate, delete,
import (an archive or a directory), save or delete a suite, start a live run or re-run, cancel a run, ask for a
proposal, accept or reject one, roll back. A request with no Origin (MCP, the HTTP tools, the CLI) is an outside client
and is refused there; reads and offline runs stay open. Outside clients use the agent tools, where the owner is asked.

## runs/<run>.json

`id`, `agent`, `suite`, `digest`, `version`, `mode` (offline or live), `model`, `started`, `finished`, `status`
(running, done, stopped, failed), `stopped` (why), `spent_usd` (an estimate; 0 offline), `tokens`, `caps`, `passed`,
`failed`, `previous` (the run this one re-ran), and `cases[]`: `id`, `pass`, `why`, `graders[]` (`kind`, `pass`,
`skipped`, `detail`), `answer`, `transcript[]` (`sent`, `reply`, `calls`, `subtask`, `tokens`), `steps`, `tokens`,
`cost_usd`, `wall_ms`. Compare two runs case by case: fixed, broken, tokens and steps.

## proposals/<id>.json

From a live or offline run with failures, Propose (live: it calls a model, after the owner confirms) sends the failed
cases (turns, graders, answer, the end of the transcript) and the agent's prompt, skills, variants, context and
steering, with a fixed meta-prompt (`proposals.go`), and asks for JSON: `{"proposals": [{"file", "diff",
"rationale"}]}` (or `content` for a new file). At most five edits; only the prompt, skills, variants, context and
steering may be changed. Each edit is checked: the diff must apply strictly (each hunk's body has
exactly the lines its header counts, at least one kept line unless the file is new, and it matches at its stated line
or at exactly one place within 50 lines; one bad hunk refuses the whole edit) and the agent must still validate, or it
is marked `invalid`. Stored as `id`, `run`, `digest`, `model`, `tokens`, `items[]` (`n`, `file`, `diff`, `rationale`,
`status`: pending, accepted, rejected, invalid, stale; `version` once accepted). Accepting applies the edit to the
agent as it is now (`stale` if it no longer applies) and saves a version over exactly the state it was applied to; one
decision at a time per proposal, so an edit cannot be accepted twice. Re-run runs the suite again and compares.

## workbench.json (the export manifest)

```json
{"workbench_format": 1, "name": "...", "id": "...", "digest": "...", "agentdir_digest": "...",
 "created": "RFC 3339", "workbench_version": "0.1.0", "source": "template:embedded", "version": 3,
 "files": ["agent.md", "host.yaml", "..."], "suites": ["basics"]}
```

The archive holds the agent's files at its root (it unpacks into a directory Nanite opens as it is),
`workbench.json`, and `workbench/steering.json`, `workbench/options.json`, `workbench/evals/*.yaml`. Import takes
`.tar.gz`, `.tgz` or `.zip` and refuses absolute paths, `..`, links and special files, more than 1,000 entries, a file
over 1 MiB, more than 24 MiB unpacked or 32 MiB uploaded; it skips (and lists) anything that is not part of an
agent, and the files must validate. An imported agent gets a free id; a digest that differs from the manifest's is
reported. Import from a directory (an absolute path) reads only its authored files and never changes it.

## HTTP (under /api/m/workbench/)

`GET templates` · `GET|POST agents` · `GET|DELETE agents/{id}` · `POST agents/{id}/duplicate` ·
`PUT agents/{id}/files` (`{changes: {path: text|null}, edits: [{file, set: {"runtime.max_steps": 30}, body}], base,
note, dry_run}`) · `GET agents/{id}/versions/{n}/diff?to=current|m` · `POST agents/{id}/versions/{n}/rollback` ·
`GET agents/{id}/preview` · `GET agents/{id}/export` · `POST import` (raw bytes, or `{name, data: base64, id}`) ·
`POST import-dir` (`{path, id}`) · `GET|PUT agents/{id}/suites` · `DELETE agents/{id}/suites/{name}` ·
`GET agents/{id}/suites/{name}/estimate` · `POST agents/{id}/suites/{name}/run` (`{mode, confirm}`; live needs
`confirm: true`) · `GET agents/{id}/runs[/{run}]` · `POST agents/{id}/runs/{run}/cancel|rerun|propose` ·
`GET agents/{id}/compare?a=&b=` · `GET agents/{id}/proposals` · `POST agents/{id}/proposals/{p}/{n}/accept|reject`.

## Agent tools

`workbench_list`, `_get`, `_validate`, `_versions`, `_suites`, `_estimate`, `_runs`, `_run_get`, `_compare`,
`_proposals` (read); `workbench_create`, `_write_file` (the prompt, skills, variants, context, steering, options),
`_write_suite` (write; the writes wait while the owner has unsaved changes to that agent and save over the
state they read); `workbench_write_config` (`host.yaml`, `.mcp.json`, `permissions.yaml`, `providers.yaml`: what the
agent may run and reach), `workbench_import_dir` (it reads from disk), `workbench_run_eval` and `workbench_propose`
(confirm: asked every time). No tool accepts or rejects a proposal, rolls back, deletes or sets a mode: those are the
owner's.

## Known gaps

Stopgaps, labeled as such in the code, pending Nanite exports (asked of Nanite):

- Nanite exports no model provider, and nanite/evals needs one for a live run, so `provider.go` is a small Anthropic
  Messages client over the gateway (non-streaming). Eval runs therefore speak the Anthropic wire only; an agent with
  `runtime.provider: openai` cannot be run live. When Nanite exports a constructor this file goes.
- nanite/evals has no hook for host tools, so a case cannot stub a tool yet: an eval has the file tools over its
  fixtures, and `.mcp.json` servers are not started.
- nanite/evals.Run changes the process's working directory for each case; runs therefore happen in a child process.
- Variant and providers.yaml validation mirrors Nanite v0.6.0's internal rules (they are not exported).
