# The embedded agent

Every Surface app has one agent. The owner talks to it in the agent panel, and outside agents can reach the same
tools over MCP. It has every tool in the app. What changes from message to message is what it has in mind: the
**context slots**. The window in front decides them, and the modules fill them. Rules in front of the agent (the
**router**) and standing rules that watch the conversation and the app (**reflexes**) shape what it does. A saved
layout can switch a set of these on for one way of working, and the Interaction Builder writes such layouts for the
owner.

```
owner's message ─▶ POST /api/agent/turns
                     ├▶ router      a rule may run a look-only call first, or tell the agent what was asked
                     ├▶ reflexes    reminders · a halt · run a command · call a look-only tool
                     ├▶ selector    the active slots, from the Situation (window in front, mode, a pin)
                     ├▶ assembler   the <context> block: in front · slots · instructions · knowledge · note · top of mind
                     └▶ Engine      the model, the tools, approvals, the thread  ─▶ chatstream events ─▶ panel
```

Code: `core/internal/agent` (the service, the seam, HTTP), `core/internal/slots`, `core/internal/router`,
`core/internal/reflex`. Panel: `web/src/agent`.

## The seam: `Engine`

The agent underneath is behind one interface, so it can change without the rest:

```go
type Engine interface {
    Ready() bool
    Model() string
    AddTools(tools ...EngineTool) error            // every tool, before serving
    Start(ctx context.Context, t Turn) (runID string, err error)
    History(ctx context.Context, owner string) ([]Said, error)
    Handler(prefix string, owner func(*http.Request) (string, error)) http.Handler
    Close()
}
```

Today the engine is `github.com/hollis-labs/libs/agent/host`. It runs the model loop, keeps the thread in the core
database (its `agenthost_*` tables), asks for approvals, compacts long threads and serves the run streams. Its wire
is `agent-host/1`, which the page's `packages/chat` client speaks. The direction is Nanite's embeddable
package. When it ships, it becomes a second `Engine`, and nothing above the seam changes.

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

Then restart the app. Optional fields: `system` (extra standing instructions), `max_tokens` (default 8192),
`max_steps` (tool rounds per message, default 24) and `compact_at` (thread size in tokens before older turns are
summarised).

- `gateway_url` is an Anthropic-compatible endpoint. The engine posts to `<gateway_url>/v1/messages`.
- `key_file` holds the bearer key. It is read on every request, so it can be rotated without a restart. The key
  never reaches the page.
- `mcp` is reserved for outside MCP servers whose tools the agent should get. The engine does not speak MCP as a
  client yet.
- For a development run, `SURFACE_AGENT_GATEWAY`, `SURFACE_AGENT_MODEL` and `SURFACE_AGENT_KEY_FILE` override
  single fields.

The file is read at start. With no file the app runs normally: the panel shows a setup card with the path and the
example above, and a message is refused with `503` and the same path.

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

### Assembly

The assembler turns a selection into the `<context>` block that leads the owner's message:

- what is in front: title, view, kind, ref or URL, and the selection;
- the other open windows, the layout and the mode;
- for each active slot: its instructions, the agent's note on it, where things are kept, and what its module knows
  now (`Knowledge`);
- the top-of-mind tools: the active slots' `Tools` that the app has;
- the layout's standing instructions;
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

By default the engine asks in the panel.

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
| `GET /api/agent` | `{available, ready, model}` (engine) |
| `GET /api/agent/thread`, `DELETE /api/agent/thread` | the conversation; start a new one (engine) |
| `POST /api/agent/turns` | `{text, client_id, auto?}` → `{run_id, assembly, routed?, fired?}`. A page's own `context` and `tools` are ignored. |
| `GET /api/agent/runs/{id}/events` | the run's chatstream/v1 events, resumable with `Last-Event-ID` (engine) |
| `POST /api/agent/runs/{id}/approvals`, `…/cancel` | answer a question; stop a run (engine) |
| `GET /api/agent/context` | the assembly the next message would carry |
| `GET /api/agent/setup` | `{ready, model, config_path}` |
| `GET /api/agent/slots`, `POST /api/agent/slots/pin` | every slot with its note; pin a slot (`""` unpins) |
| `GET /api/agent/reflexes`, `PUT`/`DELETE /api/agent/reflexes/{name}` | the reflexes |
| `GET /api/agent/rules`, `PUT`/`DELETE /api/agent/rules/{id}` | the owner's and the built-in routing rules |

The agent's own tools:

- `context_note`, `context_switch`;
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
| The hook before a turn runs | What `Send` does on the engine: a routing rule may run a call, a reflex may answer instead (a host-answered turn every attached page sees), and the context block is assembled and attached as the turn's context |
| Standing approvals ("always") | The `agent_grants` table in the core database; listed at `GET /api/agent/grants` |

It is on when a runner is chosen with `SURFACE_AGENT_RUNNER`. Today the only runner is `scripted`, the
library's test runner: it plays fixed scripts (`hello`, `tool`, `approval`, `confirm`, `reasoning`, `error`,
`abort`, `length`, `hold`; anything else is echoed) and runs no tools. It is how the wire and the page are
exercised until the real runner is wired in. With it, a turn starting `/no` is refused and one starting `/ping`
is answered by the host, and `POST /api/agent/test/replace` replaces the agent's side (a generation bump).
`SURFACE_AGENT_SCRIPT_DELAY` paces its events. With no runner chosen, nothing changes: the engine and the
run-at-a-time wire serve the panel as before.

The page asks `GET /api/agent/setup`, which says `"wire": "conversation"` and the agent's name when the wire is
on, and binds `@hollis-labs/surface-chat`'s conversation store instead of the run store
(`web/src/agent/session.ts`). `?chat=viewer` attaches a page as a viewer and `?chat=committed` takes each part
whole instead of as it is written.

Not there yet: a runner that runs the app's tools (so tool levels, the ask-first rule and checking the grants
before asking are not exercised), and the reflexes' view of the earlier conversation on this wire.
