# The embedded agent

Every Surface app has one agent. The owner talks to it in the agent panel, and outside agents can reach the same
tools over MCP. It has every tool in the app. What changes from message to message is what it has in mind: the
**context slots**. The window in front decides them, and the modules fill them. Rules in front of the agent (the
**router**) and standing rules that watch the conversation and the app (**reflexes**) shape what it does. A saved
layout can switch a set of these on for one way of working, and the Interaction Builder writes such layouts for the
owner.

```
owner's message ─▶ POST /api/agent/conversations/{id}/turns
                     ├▶ router      a rule may run a look-only call first, or tell the agent what was asked
                     ├▶ reflexes    reminders · a halt · run a command · call a look-only tool
                     ├▶ selector    the active slots, from the Situation (window in front, mode, a pin)
                     ├▶ assembler   the <context> block: in front · slots · instructions · knowledge · note · top of mind
                     └▶ Nanite      the model, the tools, approvals, the thread  ─▶ chatstream events ─▶ panel
```

Code: `core/internal/agent` (the service, the seam, HTTP), `core/internal/slots`, `core/internal/router`,
`core/internal/reflex`. Panel: `web/src/agent`.

## The conversation runner

Embedded agents use `nanite/chatrunner` on the durable conversation wire. Nanite owns the model loop,
approvals, compaction, and model history; Surface owns `chat.db`, context assembly, routing, and app tools.
Tests use the scripted conversation runner. There is no legacy Engine or run-at-a-time HTTP transport.

Every tool is a server tool. The page declares none and runs none: the desk is the server's, so a tool that arranges
windows changes server state that the page draws. The same tools are offered to outside agents at `/mcp` and
`/api/tools`.

## Configuration: a file, not the database

The app's own settings live in its database and change live. The agent's and the harness's configuration lives in
a file:

`~/.config/<app>/agent.json` (or the file `SURFACE_AGENT_CONFIG` names)

The minimum, to copy and fill in (for the app `desk`, at `~/.config/desk/agent.json`):

```json
{
  "model": "your-model-name",
  "gateway_url": "https://your-gateway.example",
  "key_file": "~/.config/desk/agent-key"
}
```

```sh
mkdir -p ~/.config/desk && chmod 700 ~/.config/desk
$EDITOR ~/.config/desk/agent.json          # the three fields above
printf '%s' "$YOUR_KEY" > ~/.config/desk/agent-key && chmod 600 ~/.config/desk/agent-key
```

Then restart the app. `system` adds standing instructions. Nanite runtime settings belong in the agent directory's
`host.yaml`, including `runtime.max_output_tokens`, `max_steps`, and `compact_at` (tokens before older turns are
summarised). Positive `max_tokens` and `max_steps` in `agent.json` override Nanite's output-token and step limits; zero leaves the authored/default runtime limits in effect.

For testing, `auto_approve: true` in `agent.json` bypasses Nanite approval asks, including host-tool asks. It defaults to false and takes effect on restart. Explicit denies and Warden access checks still apply; automatic approvals do not persist grants. The startup log and `/api/agent/setup` expose `auto_approve`. Only the owner should opt a seat into this mode. The wire emits `auto-approved` activities; the panel does not yet render those activities.

Surface reserves 2,000 tokens for its per-turn context block, plus room for the seat's context files and the largest loaded module profile. Profile facts do not compete with the turn block for that allowance.

- `gateway_url` is an Anthropic-compatible endpoint. Nanite posts to `<gateway_url>/v1/messages`.
- `key_file` holds the bearer key. It is read on every request, so it can be rotated without a restart. The key
  never reaches the page.
- Configure outside MCP servers in the Nanite agent directory's `.mcp.json`; the old agent.json `mcp` field is reserved.
- For a development run, `SURFACE_AGENT_GATEWAY`, `SURFACE_AGENT_MODEL` and `SURFACE_AGENT_KEY_FILE` override
  single fields.

The file is read at start. With no file the app runs normally: the panel shows a setup card with the path and the
example above, and a message is refused with `503` and the same path.

## Choose a module’s agent

**Settings → Module agents** and the agent panel’s **Profile → Choose agents** let the owner
enter an absolute agent folder path (or one beginning with `~/`) for any installed module.
The live, owner-only setting `agent.bindings` maps module IDs to paths:

```json
{"scratch": "~/.config/my-app/agents/writing"}
```

A binding replaces that module’s built-in profile. A pinned module wins over focus; otherwise
the module in front selects the agent. **Reset** restores the built-in profile, or the app’s
agent when the module has none. Pinning **default** always selects the app’s agent.
The picker shows the bound agent’s name; `/api/agent/profiles` includes the source folders,
validation errors and ignored runtime keys. `/api/agent/runs` records the module in `profile`
and the selected source folder in `agent_dir`.

Folders contain `agent.md`, optional `context/`, `skills/`, `variants/`, `tools/` and
`steering.json`, plus an optional `host.yaml`. Surface checks them with `agentdir` and Nanite’s
authored validators on binding, at boot and before each turn. Invalid writes retain the old
binding; invalid saved folders refuse startup with the reason. Files changed after startup
are checked before dispatch, and a bad folder refuses the turn rather than silently switching
agents. `.mcp.json`, provider, identity and permission files are refused: the app supplies them.
Tool packages cannot request environment passthrough or Warden tokens.

**In this first version, all bound agents use the app’s runtime.** A folder’s `host.yaml` is
validated, then omitted from the staged turn profile. Its `runtime` keys (model, compaction,
token budget, and so on) do not override the app; ignored keys are listed beside the binding
and logged on binding and at boot. Tools, enabled builtin groups, levels, grants and the
Approver also remain the app’s. A binding does not enable additional tool groups.

Surface stages validated authored files into content-addressed snapshots at
`<data>/agents/bindings/<digest>/` and passes the selected snapshot through Nanite’s per-turn
`TurnProfile`/`Input.AgentDir` interface. At boot and after a binding change, Surface prunes old
snapshots, retaining current bindings, the newest 10 by last-use time, and everything used in
the last seven days. Staging a turn or resuming an interrupted turn refreshes that timestamp.
Future timestamps and snapshots whose metadata cannot be read are never pruned.
This is age-based retention, not a runtime lease: an unbound snapshot last used more than
seven days ago can be removed even if a turn is still running or interrupted. Resuming a
turn whose snapshot was removed fails explicitly; it never substitutes another agent.
Conversation state and approvals remain under the app data directory;
Surface never writes runtime state into the owner’s source folder. Unbound built-in profiles
continue using the existing seat-overlay staging behavior.

## Context slots

A slot is what the agent brings to one kind of work. A module declares slots in its manifest:

```go
module.Slot{
    Name:         "drafts",
    About:        "writing and revising a document with the owner",
    Views:        []string{"drafts"},          // a window of these in front selects the slot
    Modes:        []string{"writing"},         // a user mode that adds it, whatever is in front
    Tools:        []string{"drafts_get", "drafts_edit", "marks_reply"}, // top of mind here
    Instructions: "Read the draft before changing it…",
    Where:        "the draft in front (drafts_get); other drafts (drafts_list)",
    Knowledge:    func(ctx context.Context, h module.Host, s module.Situation) (string, error) { … },
}
```

Moving between slots changes what the agent is told, not who it is or which tools it has. Every tool stays
available, and the system prompt and tool definitions stay the same from turn to turn, so the provider's cache
holds.

### Selection

A `Selector` picks the active slots from the `Situation`: the window in front (as its module describes it), the open
windows, the user's mode, the layout in use and the message.

```go
type Selector interface {
    Select(ctx context.Context, sit module.Situation, all []kernel.BoundSlot) Selection // Slots (primary first), Why
}
```

Today's selector is `FocusSelector`:

- The window in front selects the slot whose `Views` names its view. A `<module>.<x>` view falls back to its module's
  slot when that slot names no views.
- A window with no slot keeps the slot as it was.
- A set mode adds the slots whose `Modes` name it.
- The agent can pin a slot with `context_switch`. The pin holds until the owner brings a different view to the
  front.
- With nothing else, the core's own `general` slot applies, with the desk tools top of mind.

A classifier over the owner's input, activity and workspace will be a second `Selector`. It gets the same
`Situation` and returns the same `Selection`, so the assembler and the modules do not change. `Options.Selector`
plugs it in.

That classifier runs today **in shadow** (`core/internal/classifier`, on libs `core/util/classify`). Every traced
turn is labelled on two facets, and the result is kept beside the turn (`classified`). Nothing reads it to decide
anything unless the live setting `surface.agent_selector` is `classifier`.

With that setting, the classifier overrules the window in front for a message only when all of these hold:
- the message has text;
- the owner has not pinned a slot;
- the slot facet answered for another slot with p ≥ 0.7.

The focus slot then follows as a second slot, and the trace records `why: classified`. In every other case the
focus selector's choice stands. The default is `focus`. Check the replay report before switching.
- `act`: command, question, task, correction, approval, chat, or an abstention. The rules and examples are in
  `act.json`.
- `slot`: the app's slots and `general`. Its priors come from the view and module in front and the mode, and its
  text rules from the slots' names.

A naive Bayes model per facet learns from the trace. At start it learns every turn's slot, weakly, because that is
the selector's own guess. An owner's pin soon after a turn counts in full, because it is a correction.
`GET /api/agent/classify/replay` replays a fresh classifier over the whole trace, each turn classified before it
is learned. It reports how often the classifier agrees with the selector, and how many of the owner's corrections
it would have got right. When those numbers are good enough, the classifier becomes the `Selector`.
`POST /api/agent/classify` classifies a draft in the current situation without recording it, for the composer.
`POST /api/agent/classify/feedback` takes the owner's answer to a hint on a draft (`{text, facet, label, accepted}`).
An accepted hint is learned, and an accepted slot hint also pins that slot for the message about to be sent. It is
kept in `classify_feedback` and learned again at start.

A third facet, `affect` (neutral, frustrated, urgent, positive), comes from libs' lexicon judge.

**The async tier** asks a model gateway about every facet after the turn, off its path. Its result is kept
beside the turn (`classified_async`), and `agent.classified` is published. It is on only when the agent config
file (`~/.config/<app>/agent.json`) names a gateway:

```json
"classifier": {
  "gateway_url": "https://<llm-gateway host>/api/llm-gateway",
  "key_file": "~/.config/surface/llm-gateway.key",
  "decide_model": "jev",
  "embed_model": "embed-local"
}
```

| Field | Meaning |
|---|---|
| `gateway_url` | The gateway's base URL, or `unix:///path.sock`. With none there is no async tier. |
| `dialect` | `llm-gateway` (the default): JEV's System-1 API at `/v1/systemone`, and OpenAI-compatible `/v1/embeddings`, with the gateway's model aliases. `tether`: a decision gateway's `/ai/decide` and `/ai/embeddings`. |
| `key_file` | The gateway's bearer key. It needs the decide scope for `decide_model` and the embed scope for `embed_model`. |
| `warden` | Instead of `key_file`: mint short-lived gateway tokens from a Warden parent credential (below). Set one of the two; with both, there is no async tier. |
| `decide_model` | Turns on the JEV judge on every facet: the gateway's decision route (`jev`). |
| `embed_model` | Turns on the slot centroid judge and the emerging-label report: the gateway's embedding route (`embed-local`, `embed-openai`). Vectors from different models do not compare, so a change refits from scratch. |
| `purpose` | What a `tether` gateway accounts the spend to (default `surface_classify`). |

The slot centroid is fitted on each slot's own description (its About, name and module) and on the newest
texts each slot was given or corrected to, at most 40 per slot. It is refitted after every 25 learned turns, in the
background. It is on the slot facet only, because a sentence-embedding model groups texts by topic, not by what
they ask for. `act` and `affect` are left to rules, the n-gram models and the decider.
`GET /api/agent/classify/emerging?facet=act&limit=500` clusters the newest turns a facet abstained on, after
collapsing near-copies. It returns groups with at least three distinct members and a representative text:
candidate labels, for a person to name, not answers.

The text is scrubbed of recognisable credentials before it leaves. A failed or slow call (30s) leaves the column
empty and changes nothing else.

**Tokens from Warden.** Instead of a static `key_file`, the classifier can mint its gateway tokens from a Warden
parent credential:

```json
"classifier": {
  "gateway_url": "https://<llm-gateway host>/api/llm-gateway",
  "warden": {
    "url": "https://<warden host>",
    "credential_file": "~/.config/surface/warden.token",
    "audience": "llm-gateway"
  },
  "decide_model": "jev",
  "embed_model": "embed-local"
}
```

| Field | Meaning |
|---|---|
| `warden.url` | Warden's base URL; tokens come from `<url>/v1/token`. |
| `warden.credential_file` | The parent credential. It is rewritten on every mint, so it must be writable; it is kept at mode 0600. |
| `warden.audience` | The tokens' audience (`llm-gateway`). |
| `warden.scopes` | Optional: narrows the tokens. Empty asks for what the grant allows. |

Parent credentials are rotate-on-use, and presenting one twice revokes the whole family. So
(`core/internal/wardentoken`):
- every mint holds an exclusive lock on `<credential_file>.lock`, so apps sharing one file never present the same
  parent;
- the next parent is saved (temp file, fsync, rename) before the token is used;
- `<credential_file>.inflight` is written before a parent is sent and removed once the outcome is known. When the
  answer is lost (a timeout, a 5xx, a cut connection), the parent may be spent, so the marker stays and every later
  mint refuses, with an error in the log. To recover, re-enroll, store the new parent, then delete the marker;
- a token is renewed at two thirds of its life; when a renewal fails, the current token is used until it expires.

Neither the parent nor a token is ever logged.

### Assembly

The assembler turns a selection into the `<context>` block that leads the owner's message:

- what is in front: title, view, kind, ref or URL, and the selection;
- the other open windows, the layout and the mode;
- for each active slot: its instructions, the agent's note on it, where things are kept, and what its module knows
  now (`Knowledge`);
- the top-of-mind tools: the active slots' `Tools` that the app has;
- the layout's standing instructions;
- one-line asks to keep state current (below);
- a routed call, and reflex reminders.

`Knowledge` runs on every message, so it must be quick. Each call has a 1.5 s timeout, and its output is cut to a
budget:

| Part | Budget (characters) |
|---|---|
| The primary slot's knowledge | 6,000 |
| Each other active slot's knowledge | 1,500 |
| A slot note | 2,000 |
| The whole block | 12,000 |

When the block would be over 12,000, the secondary slots' knowledge goes first, then the primary's is trimmed.
What is in front, the slots, the notes and the reminders always stay. A module whose knowledge is slow to get
should keep a cache and warm it in the background, as Browser does for the page in front.

### Notes and pins

The agent keeps a short note per slot, saying where that work stands, with `context_note`. The note is shown
whenever the slot is active, so the agent can pick the work up again later. `context_switch` pins a slot, or lists
the slots when called with none. The panel shows the active slot, the top-of-mind tools and the block itself,
updated live: the service publishes `agent.context` whenever the desk changes it.

### Keeping state current

The agent carries two kinds of state across turns, compactions and restarts: its note on each slot (above), and
its **standing instructions**, the owner's rules for all work, in `context/standing.md` in the agent directory
(`<agents.dir>/<app>`). Neither should drift.

- `standing_get` (read) returns the text, and whether the system prompt has it yet. `standing_update` (write, so
  the owner is asked unless they granted it) takes the whole new text and a `why`. It refuses empty text and text
  over 16,000 characters, keeps the text it replaces in `context/.history/standing-<UTC time>.md` with a line in
  `context/.history/standing.log` (time, kept file, lines added and removed, why), writes the file atomically,
  publishes `agent.standing` and returns the changes. `.history` is a subdirectory, so the runner's
  `context/*.md` does not read it. Both are top of mind in the general slot.
- Nanite reloads the authored agent directory for each turn, so updated standing instructions reach the next
  request without a restart. It does not need a `<standing changed>` overlay.
- **Note on leaving a slot.** When the owner's message lands in a different primary slot from their previous
  message, the block asks once, in one line, for the note on the slot left to be brought up to date with
  `context_note`. Errands and automatic turns neither ask nor count as the previous message. What the previous
  slot was is kept in memory, so the first message after a restart does not ask.
- **A note that may be behind.** When the primary slot's note is over 24 hours old and the trace has at least
  five of the owner's messages in that slot since, the block says so in a line. With the trace off, it does not.
- **After a compaction.** Nanite publishes `agent.compacted` (`{conversation_id, before, after}`, in
  tokens) when it has summarized a thread's older turns. The seeded reflex `save-state` (event trigger on that
  topic) reminds the agent, with the next message, to bring the slot note and standing up to date.
- **When the owner sets a rule.** The seeded reflex `standing-rule` (text trigger on phrasing such as "from now
  on", "going forward", "standing rule", "don't ever", or "always"/"never" opening a sentence) reminds the agent
  to record the rule with `standing_update` in that turn and confirm it in a line.

Both reflexes are seeded on and once, by a migration (`core/internal/reflex/migrations/0002_seed_keep_state.sql`):
a reflex of the same name the owner already has is left as it is, and one the owner deletes is not put back.
Change them like any other reflex.

### The context trace

Every message leaves a record in the core database (`context_turns`, `core/internal/slots/trace.go`):
- the text;
- the window in front and the open views;
- the mode and layout;
- the slots chosen and why;
- the top-of-mind tools;
- the routed rule and the reflexes that fired, and whether one halted the turn;
- the block's size;
- what a budget cut (`<slot>:clipped`, `:trimmed`, `:dropped`).

Beside each record are **signals** (`context_signals`): what happened in the 15 minutes after the turn. Each
signal names its actor, so the owner's corrections can be told apart from the agent's own moves.
- `pin`: a slot pinned with `context_switch`, with the slot it replaced;
- `unpin`;
- `focus`: the owner moving to a window. The agent's moves are not kept.

This is what a classifier-driven `Selector` learns from: the situation and message are the input, the slot
chosen is the guess, and a re-pin or a move soon after is the correction. It is kept from the first message
because it only grows with use. It stays on the owner's machine, keeps the newest 50,000 turns, and the live
setting `surface.agent_trace` turns it off.

The **Context** window (`surface.context`, in the apps menu) reads it. **Now** shows what a message sent this
moment would carry: the window in front, the slot and why it was chosen, the tools brought to mind and the block
itself (`GET /api/agent/context`). **History** lists the turns, each with its window, slots and why, tools, routed
rule, reflexes, size, budget cuts and what followed; an owner's re-pin soon after a turn is flagged as a correction.
It also turns the trace off and exports it.

`GET /api/agent/trace?limit=` gives the newest turns with their signals.
`GET /api/agent/trace/export` gives every turn as JSON lines.

## The router

The owner's message is first put to a set of rules (`libs core/util/router`). Each rule is a list of conditions over
these fields:

- `text`: the message;
- `slash` and `arg`: a leading `/word` and the rest of the message;
- `view` and `module`: the window in front;
- `slot`, `mode` and `layout`.

A matching rule names a target, `tool:<name>` or `command:<id>`, and its `params`. A param may use `{arg}`, `{text}`,
`{view}` and the other fields, and `_say` is what the agent is told.

- An `auto` rule whose tool only looks or acts on screen (`read`, `act`) runs straight away, and the agent is told
  it was done.
- A rule that is not `auto`, or a tool that writes, is never run by the router. The agent is told what the owner
  asked for and makes the call itself, so the owner's say on a change is never skipped.

Built-in rules (`router/rules.json`): `/open <view>`, `/layout <name>`, `/run <command>`. The owner's own rules
(`agent_rule_put`, `PUT /api/agent/rules/{id}`) live in the core database and are tried first. A rule for one
layout only carries `{"field":"layout","op":"eq","value":"<name>"}`.

## What modules add: Around and errands

Besides its slots, a module can add lines to every message's context (`Manifest.Around`, shown as
`Around (<module>):`, 1.5 s and 1500 characters each) and hand the agent an errand (`Host.Agent().Ask`). An errand
is a turn on the agent's conversation with meta `{auto, errand: <module>, title}`. It skips the router and is not
recorded in the context trace, and the agent is told it is not the owner speaking. It needs the conversation
wire; `agent.errand` is published when one starts.

## Questions: `card_ask`, and answers that come late

The agent asks the owner with `card_ask`: it shows a card and waits for the answer (10 minutes by default,
`timeout_seconds` up to an hour). Its result has the card's `status`:

| Status | Means |
|---|---|
| `submitted` | answered: `data` for a form, `answers` for prompt and choice, `decisions` for approval |
| `canceled` | the owner declined. Do not ask again unasked |
| `expired` | no answer in time. The card stays answerable; a later answer comes as a message (below) |

When the run that asked ends while the card is up (the run is stopped, the app shuts down or restarts),
`card_ask` returns an error and the card ends `abandoned`, not `canceled`: `canceled` is only the owner
declining. An app that was killed with a question open abandons it at its next start, since no run can be
waiting for it.

**Late answers.** An agent's question that is `abandoned`, or `expired` because `card_ask` stopped waiting,
stays in the agent's panel and the Cards window with its inputs usable and a note saying the agent stopped
waiting (for a week; `GET /api/cards?late=1` lists the open cards and these). The owner may answer it once,
through the same `POST /api/cards/{id}/response`: the card becomes `submitted` with `late: true`, and a
second answer is refused (409). Declining it (`canceled`) only dismisses it. An answer to any other closed
card is refused as before.

A submitted late answer goes to the agent as a turn on its conversation, like an errand: meta
`{auto: true, errand: "late-answer", title}`, not recorded in the context trace, run-log origin
`errand:late-answer`. It reads `[The owner's answer to your card "<title>" (<kind>, id <id>), given after
the run that asked it ended. …]` followed by the answer: `data: {…}` as JSON, an answer or decision per line.
If the agent is busy it waits and is tried again when a run ends (`agent.run.ended`) and every 2 s. The
queue is the cards themselves (submitted late, `delivered` empty) in the core database, so a restart does
not lose one; one worker delivers in order and stamps `delivered` once the turn is accepted, so none is sent
twice (a crash between the two can send one again; none is lost). Off the conversation wire the answer stays on the card and the log says it cannot be delivered.

What the owner types into an open question (form, prompt, choice) is kept in the browser as a draft per card
(`localStorage`), so a reload or a server restart does not lose it; it is cleared once the card is answered
or dismissed. The server never sees a draft.

## Reflexes and triggers

A reflex is a trigger and an action, kept in the core database.

| Trigger | Fires when |
|---|---|
| `text` `{"pattern": "(?i)summar"}` | the owner's message matches |
| `predicate` `{…}` | a `libs agent/core/reflexes` predicate over the conversation holds |
| `event` `{"topic": "notes.saved"}` | an event on the app's bus (prefix match) |
| `focus` `{"view": "browser"}` | a window of that view comes in front |

| Action | Does |
|---|---|
| `remind` `{"body": "…"}` | tells the agent with the message. From an event or focus trigger, with the next message. |
| `halt` `{"message": "…"}` | answers the message instead of the agent (`422 {code: "halted"}`); text and predicate triggers only |
| `run_command` `{"command": "…", "args": {…}}` | runs an app command |
| `call_tool` `{"tool": "…", "input": {…}}` | calls a tool; `read` and `act` tools only |

`cooldown_seconds` keeps a reflex quiet for a while after it fires. A reflex is on when `enabled` is set. Otherwise
it is on only while a layout whose `Setup["reflexes"]` lists its name is in use. That is how one way of working
carries its own reflexes without them firing everywhere.

## What a layout's setup switches on

A saved layout's `Setup` (from `desk_layout_save`, `desk_arrange` or the Builder) is read on every message:

| Key | Effect |
|---|---|
| `mode` | the user's mode: adds the slots whose `Modes` name it, and is a router field |
| `instructions` | standing guidance, in the block as "For this layout: …" |
| `reflexes` | names of reflexes that are on while the layout is in use |

## Approvals and the `Approver` hook

A tool's level decides whether the owner is asked:

| Level | Asked? |
|---|---|
| `read`, `act` | Never. |
| `write` | Yes. The answer can stand for the session or for good. |
| `confirm` | Every time. |

By default Nanite asks in the panel.

Human-in-the-loop approval belongs to Gatecrasher, not Surface, so Surface leaves one hook:

```go
type Approver interface {
    Approve(ctx context.Context, r Request) (bool, error) // Request{Tool, Module, Level, Input}
}
agent.NewWith(k, agent.Options{Approver: myApprover})
```

With an approver set, `write` and `confirm` calls go to it instead of the panel. A refusal is told to the model as a
refusal. Reflexes and router rules never make `write` or `confirm` calls themselves.

## Showing the owner things: ADE's overlays

The agent can point. `ade_point` rings a window or an anchor in one (`highlight`), dims the desk around it
(`spotlight`), or draws an arrow to it from another (`arrow`), with a note beside it; `ade_clear` takes it away and
the owner can dismiss the note. `ade_guide_start` puts a short guide over the desk: each step says one thing, may
point at windows or anchors, and waits for the owner's Next or for a view to be open or in front (`await`), then moves
on by itself. `ade_guide_status` says where it stands; `ade_guide_stop` ends it. `ade_anchors` lists the windows and
the anchors their views declared (`useAnchor` in `@hollis-labs/surface-page/sdk`, docs/writing-a-module.md).

The server keeps what is asked for (`core/internal/ade`) and the page draws it: a tool call publishes `ade.attention`
or `ade.guide`, the page applies it to its attention hub (`@hollis-labs/ade-core`, with the desk as one surface whose
parts are the windows and the anchors) and draws it with the registry's `attention-layer` and `guide-bar`. The page
reports the anchors each window declares and where a guide stands; a guide's progress is also published as
`ade.guide.progress`, which a reflex can trigger on. The tools are `act` level: they change only what is on screen.

Routes: `GET /api/ade` (the attention, the guide and its report), `PUT /api/ade/anchors/{window}`, `POST
/api/ade/guide`, `DELETE /api/ade/attention`.

## HTTP

All routes are under `/api/agent`. The app is single-user and loopback-only, so every request is the owner's.

| Route | |
|---|---|
| `POST /api/agent/agents/{agent}/conversation` | open the agent's durable conversation |
| `POST /api/agent/conversations/{id}/attachments` | attach as a controller or viewer |
| `POST /api/agent/conversations/{id}/turns` | submit `{attachment, text, request_id?, meta?}`; returns `{run_id}` |
| `GET /api/agent/conversations/{id}/events` | the resumable conversation stream, including history |
| `POST /api/agent/conversations/{id}/approvals/{approval}` | answer an approval with the attachment and decision |
| `POST /api/agent/conversations/{id}/cancel` | cancel the active run |
| `GET /api/agent/context` | the assembly the next message would carry |
| `GET /api/agent/setup` | `{ready, model, config_path}` |
| `GET /api/agent/slots`, `POST /api/agent/slots/pin` | every slot with its note; pin a slot (`""` unpins) |
| `GET /api/agent/trace`, `GET /api/agent/trace/export` | the context trace: the newest turns with their signals; every turn as JSON lines |
| `POST /api/agent/classify`, `GET /api/agent/classify/replay` | the classifier's result for a draft (records nothing); a fresh classifier replayed over the trace against the selector |
| `POST /api/agent/classify/feedback` | `{text, facet, label, accepted}`: the owner's answer to a hint; an accepted slot pins it |
| `GET /api/agent/classify/emerging` | `?facet=&limit=`: clusters among turns the facet abstained on (409 without an embedder) |
| `GET /api/agent/runs` | the run log (conversation wire): each run's origin (`owner`, `errand:<module>`, `auto`), slot, model, token usage, tools called, end and duration, newest first; `?limit=`, `?format=jsonl` |
| `GET /api/agent/reflexes`, `PUT`/`DELETE /api/agent/reflexes/{name}` | the reflexes |
| `GET /api/agent/rules`, `PUT`/`DELETE /api/agent/rules/{id}` | the owner's and the built-in routing rules |

The agent's own tools:

- `context_note`, `context_switch`;
- `standing_get`, `standing_update`;
- `agent_reflex_list`, `agent_reflex_put`, `agent_reflex_enable`, `agent_reflex_delete`;
- `agent_rule_list`, `agent_rule_put`, `agent_rule_delete`.

They are offered to outside agents too.

A view hands the agent a message with the page command `agent.ask`
(`desk.run("agent.ask", {text})`), which opens the panel to show the answer.

## The Interaction Builder

`modules/builder` adds no machinery of its own. It is a module whose slot teaches the agent to compose a way of
working from the pieces above. The owner describes a flow ("write an article", "triage my inbox"), and the agent:

1. reads `builder_catalog`: every module's views, slots, commands and tools;
2. arranges the windows: which views, the master, the ratio, the focus and what each opens with;
3. writes the setup: a mode, its own standing instructions, and flow-only reflexes (`agent_reflex_put` with
   `enabled: false`) and rules (with a `layout` condition);
4. saves it all with `builder_flow_save`, which arranges the desk, saves the layout with its setup, keeps the version
   and puts the flow in use.

A refinement ("make the browser bigger") is a new version with a note. Every version is kept and can be restored.
The owner describes and refines a flow in the agent's panel; the Builder window has no input of its own. It lists
the flows, opens one on the desk, and shows its versions. Generating a new app binary from a flow
comes later.

## The conversation wire (in progress)

The agent can be served on the conversation wire (`github.com/hollis-labs/libs/agent/chat`) instead of one run
at a time: a durable conversation that outlives a turn and a page. A page attaches as a viewer or a controller,
reads one event stream from wherever it says, and submits turns when it holds the input; a reload, a second tab
and a turn started inside the app are all the same stream. `core/internal/agent/conversation.go` mounts the
library's handler under `/api/agent` and supplies what varies:

| The library asks for | Surface gives |
|---|---|
| Who may do what | The app is loopback-only and single-user: the owner, everything |
| The agent | One, named after the app, kind `embedded` |
| The log and the control records | `chat.db` in the app's data directory |
| The hook before a turn runs | Before the runner starts: a routing rule may run a call, a reflex may answer instead (a host-answered turn every attached page sees), and the context block is assembled and attached as the turn's context |
| Standing approvals ("always") | The `agent_grants` table in the core database; listed at `GET /api/agent/grants` |

It defaults to Nanite when `SURFACE_AGENT_RUNNER` is unset. Explicit choices are `nanite` or `scripted`; `host` is rejected.
An unconfigured app remains usable without opening a model.

### `nanite`: the app's agent as a Nanite agent

`SURFACE_AGENT_RUNNER=nanite` opens the app's agent as a Nanite agent in the app's own process
(`github.com/hollis-labs/nanite/chatrunner`, wired in `core/internal/agent/nanite.go`).

| What | Where it comes from |
|---|---|
| Model, gateway, key | The app's agent config,  `model`, `gateway_url`, `key_file`. The key is passed as a reference to the file and read by Nanite when it calls the gateway. The key file must be under the home directory, owned by the user and mode 0600. |
| The agent's standing instructions | Its **agent directory**: `<agents.dir>/<app>/` with `agent.md` and `host.yaml`. `agents.dir` is `"agents": {"dir": "..."}` in the agent config (or `SURFACE_AGENTS_DIR`); the default is `agents/` beside the agent config file. Surface writes a starter the first time and never touches it again: it is the owner's to edit, and Nanite reads it at each turn. |
| Run-time state | `<data dir>/agents/<app>/` (Nanite's conversations and lock) |
| Tools | The app's tools, run in the app, with their levels. `read` and `act` run; `write` asks, and "This conversation" or "Always" covers later calls; `confirm` asks every time and offers no standing choice. A level that is empty or unknown is `write`. |
| Ask first | A routing rule that is not `auto` tells the agent what was asked; the call it then makes to that rule's tool asks the owner first, whatever the tool's level. |
| Standing approvals | "Always" is kept in `agent_grants` and read before asking, so it holds across restarts and new conversations. |
| Nanite's own tools | None by default. `"builtins": ["files-read", "skills", ...]` in the agent config turns groups on (`files-read`, `files-write`, `shell`, `background`, `subagents`, `skills`, `notes`, `questions`, or `all`). |
| What is in front of the owner | The context block, as text, in the turn's context: Nanite places it in one slot for that turn only. Slot and layout instructions travel in it, so `agent.md` stays about who the agent is. The config's `system`, when set, leads the block. |
| A turn the host answers (a halting reflex) | Shown on the stream like any turn and recorded in the agent's history, so the agent knows it happened. |
| The reflexes' view of the conversation | Read from the agent's history. |
| Module profiles | When a module ships one (`Manifest.Profile`), Nanite opens on a staged directory, `<data dir>/agents/<app>.staged/`: the agent directory with the active profile laid over it, written again before each turn. The `subtasks` and `skills` groups are turned on when a profile has variants or skills. See writing-a-module.md, "An agent profile". |

The model is named in the app's agent config, not in `host.yaml`: Surface sets `NANITE_DEFAULT_MODEL` in
its own process from the config, which Nanite uses when `host.yaml` names no model. A `model` written into
`host.yaml` wins. To ask the owner something the agent uses `card_ask`; Nanite's `ask_user` is declined on
this wire. `SURFACE_AGENT_DEBUG=1` logs each turn's context block and what the hook saw.

### `scripted`: the library's test runner

`scripted` plays fixed scripts (`hello`, `tool`, `approval`, `confirm`, `reasoning`, `error`,
`abort`, `length`, `hold`; anything else is echoed) and runs no tools: it exercises the wire and the page
without a model. With it, a turn starting `/no` is refused and one starting `/ping`
is answered by the host, and `POST /api/agent/test/replace` replaces the agent's side (a generation bump).
`SURFACE_AGENT_SCRIPT_DELAY` paces its events.

The page asks `GET /api/agent/setup`, which says `"wire": "conversation"` and the agent's name when the wire is
on, and binds `@hollis-labs/surface-chat`'s conversation store instead of the run store
(`web/src/agent/session.ts`). `?chat=viewer` attaches a page as a viewer and `?chat=committed` takes each part
whole instead of as it is written.

Nanite is the default for a configured embedded agent. Its authored `host.yaml` owns runtime settings:
`runtime.max_output_tokens` (starter: 32000), `compact_at` (110000), and `compact_keep_tokens` (22000).
Surface supplies the compaction defaults for existing agent directories too; explicit host settings win.
The default compaction prompt and stream idle timeout come from Nanite. Context retains its 2000-token
budget and is not compactable. Ordinary tools have five-minute deadlines; tools accepting
`timeout_seconds` may wait for the owner for up to 70 minutes.

`runtime.daily_token_budget` caps the agent's token usage per UTC day. Zero (or omission) means no cap;
Surface supplies no default. The panel displays Nanite's 80% budget notice. Once the cap is spent,
turn submissions return HTTP 429 with `daily_budget` and a message naming the 00:00 UTC reset.

The agent panel menu's **Stop everything** calls the owner-only `POST /api/agent/stop-all` route.
For Nanite this cancels turns, subtasks and background jobs and permanently stops that instance.
The response includes `restart_required: true`: restart the app before sending more turns. Until then,
turns return HTTP 503 with `agent_stopped`. In-process reopening is not implemented; existing wire,
profile and event callbacks retain the runner. The scripted test runner cancels its current turns and
remains available. The route leaves transcripts and agent files intact.

Surface connects runner notices and recovered turns to the chat service. Startup calls `RecoverTurns`
after the service is ready, exposing interrupted turns for owner action. Successful `nanite.compact`
activities publish `agent.compacted` on the app bus. Nanite steering matches live Surface reflexes;
Surface performs their command/tool actions and records firing counts. Event and focus reflexes use
Nanite's conversation steering, and compaction reflexes run once from Nanite's own event.

When a conversation last used a host thread, Surface selects a fresh Nanite history while keeping its
chat ID and transcript. The first model turn says earlier messages remain in that transcript and points
to the slot notes and standing instructions. The saved runner reference preserves the new history across
restarts. No old model history, `agenthost_*` tables, or `chat.db` transcript records are deleted.

### Optional front desk

The agent config can opt into Nanite's intake planner and paced background deliveries:

```json
{
  "front_desk": {
    "mode": "auto",
    "plan_with_model": true,
    "intake_timeout_seconds": 60,
    "merge_window_ms": 100,
    "resume_interrupted": true
  },
  "builtins": ["files-read", "subtasks", "background"]
}
```

Omit `front_desk` (or set it to null) to retain ordinary chat. An empty object
paces notices but does not plan. `mode` is `off`, `shadow`, `suggest`, or `auto`:
shadow evaluates plans without dispatch; suggest waits for the owner; auto can
dispatch confident plans and asks on uncertain ones. `plan_with_model` enables
model planning, and optional `planner_instructions` adds host guidance. Neither
setting changes grants or approvals. Configure the required builtins and any
custom research/worker variants in the agent directory as usual.

Timeout zero uses Nanite's default (15 seconds); a positive timeout may be at
most 300 seconds. Merge-window zero uses Nanite's default, and a positive value
may be at most 60,000 milliseconds. Configuration changes require an app restart.
`resume_interrupted` is a separate explicit opt-in to resume unfinished root
turns; otherwise Nanite retains its default interrupted-turn behavior. The front
desk recovers its saved background work after the conversation service is ready.

The conversation panel shows plan events and paced delivery notices, including
when history replays after reconnect. Suggested plans have owner-only Start and
Decline actions; a closed or outdated suggestion is rejected by Nanite. The
read-only `GET /api/agent/front-desk` reports whether it is enabled and its topic
ledger. Plan decisions use `POST /api/agent/front-desk/plans/{id}/accept` or
`/decline` for the current conversation; outside clients cannot answer them.

A headless pilot's token budget may be too small for a full Surface app: the
app supplies its module tools in addition to Nanite builtins. Measure usage on a
disposable app before choosing a budget. Tests must use a separate data directory,
a separate port and copied agent configuration, never an owner's conversation.
