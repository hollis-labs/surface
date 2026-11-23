# Writing a module

A module is a Go package, plus a web entry when it has views. It imports one Surface package,
[`module`](../module), and one web package, `@hollis-labs/surface-page/sdk`. [`modules/scratch`](../modules/scratch) is the
smallest complete example; read it beside this page.

```
modules/<id>/
  <id>.go            the Manifest, Start, the tools
  store.go           the module's data
  migrations/*.sql   its schema (when it declares a store)
  web/index.tsx      its views
```

## The Go side

```go
package scratch

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base // no-op Start and Stop
	st *store
}

func (m *mod) Manifest() module.Manifest { … }
func (m *mod) Start(ctx context.Context, h module.Host) error { … }
```

`Manifest()` is read once at boot. `Start` is called after the module's store is open; keep the `Host`.

### Views

```go
Views: []module.View{{
	ID: "scratch", Title: "Scratch", Icon: "sticky-note", Width: 520, Height: 640,
	Menu: true, Bare: true,
	Render: module.Component("scratch/main"),
	Describe: func(ctx context.Context, h module.Host, w module.Window) (module.WindowContext, error) { … },
}},
```

`ID` is unique in the app (the module id for the main view, `<module>.<name>` for others). `Render` names the
component the web entry exports. `Multi` allows several windows, told apart by `key`. `Tiling` (`"full"` by default,
or `"tile"`) controls window tiling behavior: modules open full-frame by default and replace the active module on
switch, whereas a module with `Multi: true, Tiling: "tile"` (such as `terminal`) autotiles multiple windows of that
module side-by-side within its workspace. `Describe` adds what the module knows about one of its windows (`Kind`,
`Ref`, `Extra`) to what the page reported: it is what the agent is told about where the owner is. Use a stable `Ref`
(`"<module>:<id>"`): marks are pinned to it.

### Commands

A command shows in the palette, can have a shortcut, and can be run by a view (`desk.run`), the agent
(`desk_run_command`), the CLI (`<app> run <id>`) or the API.

```go
{ID: "scratch.add", Title: "Scratch: jot something down", Section: "Scratch", Keys: "alt+shift+s",
	Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) { … }},
```

### Palette search providers

Modules add read-only searches to `Manifest.Palette`. Each runs independently of commands,
layouts and other providers; failures stay in that provider's group.

```go
Palette: []module.PaletteProvider{{
    ID: "search", Title: "Work items", Mode: "commands",
    Search: func(ctx context.Context, h module.Host, q module.PaletteQuery) ([]module.PaletteResult, error) {
        // Search your store using q.Text; honor ctx and q.Limit.
        return []module.PaletteResult{{
            ID: "item-1", Title: "Example item",
            Ref: &module.PaletteRef{Kind: "work.item", Canonical: "team/1234abcd"},
        }}, nil
    },
}},
```

Provider IDs are local to the module; the API exposes `<module>/<id>`. `Mode` defaults to
`commands`; `tabs` providers also run with an empty query. Results need a stable ID, title,
and exactly one action: `Ref` opens via Cite, `Command` runs a registered command, or `Window`
focuses an existing desk window. Optional `Description` and `Icon` describe the result.
`Close: true` offers the desk's normal close action for window results, including dirty checks.

The core limits queries to 512 bytes, results to 20 per provider, searches to three seconds,
and concurrent provider work to six calls. Honor context cancellation, bound source reads,
and never mutate state from `Search`. The page debounces text searches and discards stale
responses. `GET /api/palette/providers` lists providers; `/api/palette/search?provider=…&q=…&scope=all`
queries one. `Scope` is `all` or `focused`; `FocusedWindow` comes from the server's desk.

Files searches local place filenames/paths (at most 2,000 entries, eight levels, skipping
links, credentials and dependency folders); it does not connect to remote folders. Notes
searches titles and paths across at most 2,000 entries. Cited records searches currently cached,
unexpired Cite previews by title or canonical ID, honoring resolver readiness. Modules can
provide broader record searches returning `Ref` actions without duplicating Cite resolution.
Browser's Tabs mode searches open Browser windows by title and URL. All includes every Browser
window; Focused includes only the currently focused Browser window. Closing a result refreshes
the list from desk events.

### Agent tools

```go
{Name: "scratch_add", Level: module.LevelWrite,
	Description: "Keep something in the owner's scratch, word for word.",
	Input:       json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`),
	Describe:    func(in json.RawMessage) string { return "Add to scratch: …" },
	Run:         func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) { … }},
```

| Level | Meaning | The owner is asked |
|---|---|---|
| `read` | looks, changes nothing | no |
| `act` | changes what is on screen, undone as easily | no |
| `write` | changes data or reaches outside | yes; the answer can stand |
| `confirm` | its own decision each time (a delete) | yes, every time |

Tools run in the server, so they work with no page open and are the same tools an outside agent gets over
MCP. Write descriptions for a model: what the tool is for and when to use it. Return a string, or a value to be
sent as JSON; return an error to report a failed call.

### A local store

```go
//go:embed migrations/*.sql
var migrations embed.FS
…
Store: &module.Store{Migrations: migrations, Dir: "migrations"},
```

The core opens `<data>/modules/<id>/<id>.db`, applies the numbered files that have not run, and hands the
module `h.Store()` (a `*sql.DB`) before `Start`. Add a file to change the schema; never edit one that shipped.

### Live settings

```go
Settings: []module.Setting{
	{Key: "show_done", Title: "Show finished items", Type: module.SettingBool, Default: true},
},
```

Read a setting where it is used (`h.Config().Bool("show_done")`), never into a field at start: the owner can
change it at any moment and nothing restarts. `h.Config().Watch(key, fn)` is for a module that must act on the
change. In the page the full key is `<module>.<key>` (`useSetting("scratch.show_done")`).

### Context slots

```go
Slots: []module.Slot{{
	Name: "scratch", About: "quick capture: things the owner jotted down to deal with later",
	Views: []string{"scratch"},
	Tools: []string{"scratch_list", "scratch_add"},
	Instructions: "Keep the owner's words as they wrote them.",
	Where: "the scratch items (scratch_list)",
	Knowledge: func(ctx context.Context, h module.Host, s module.Situation) (string, error) { … },
}},
```

A slot is what the module feeds the agent when its kind of work is in front, so one agent does not have to
hold everything at once. `Views` (and later `Modes`) say when it applies; `Tools` are put top of mind;
`Knowledge` returns what the agent should know right now and must be quick (no network).

### An agent profile

A module may ship an **agent profile**: who the app's one agent is while the module is in front. It is a Nanite agent
directory without `host.yaml` or `.mcp.json`, kept in `modules/<id>/profile/` and embedded:

```go
//go:embed all:profile
var profileFiles embed.FS

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{ID: "widgets", /* … */ Profile: module.ProfileFS(profileFiles, "profile")}
}
```

| File | What it is |
|---|---|
| `agent.md` | Required. agentdef front matter (`schema_version`, `definition_id`, `name`, `description`, …) and the profile's instructions (the body). |
| `context/*.md` | Standing context for the module's work. |
| `skills/<name>/SKILL.md` | Skills, as Nanite reads them (`skill_list`, `skill_get`). |
| `variants/<name>.md` | Nanite subtask variants: the profile's internal helpers (a reviewer, a researcher). They are not identities of their own; a variant only narrows the parent's tools, and may not name a `provider`. |
| `tools/<name>/tool.yaml` | Nanite tool packages (and their entry). Offered only when the app's agent config turns Nanite's `tools` group on; each call asks the owner, and an approval holds for one digest of the package. |
| `steering.json` | libs agent/steer v0.1.0 presets and reflexes (the Workbench's shape). Checked, not applied yet. |
| `evals/*.yaml` | The Workbench's eval suites. The core ignores them. |
| `README.md` | Notes for people. Ignored. |

A profile may not carry `host.yaml`, `.mcp.json`, `permissions.yaml`, `providers.yaml` or `identity.yaml`: the app owns
the runtime, the model, MCP, permissions and the identity, and every profile shares them, with the conversation and its
history, the front desk, the mailbox, the tools and their levels, the standing approvals and the Approver. A profile
brings words and helpers, never authority. A `profile/` with no `agent.md` (a README only) is an empty slot: declared,
not written yet, and not offered.

**How the agent works as it.** On the Nanite runner (`SURFACE_AGENT_RUNNER=nanite`) each turn runs as one profile:
the conversation's pin when the owner (the Profile picker in the agent panel) or the agent (`profile_switch`) set one;
else the profile of the module whose window is in front; else the app's own seat. Nanite opens on a staged directory
under the app's data directory (`agents/<app>.staged/`), and before each turn Surface writes into it the app's seat
with the profile laid over it: the seat's `agent.md` with the profile's instructions after it under `## Profile: <title>`,
the profile's context as `context/profile.<name>.md`, its skills, variants and tool packages beside (replacing) the
seat's. The owner's seat directory is never changed. The run log records which profile each run acted as (`profile`:
the module's id, or `default`). Profiles are off on the scripted test runner.

**Checking it.** `core/internal/agent/profile` loads a profile as the app will (libs agentdir over the profile beside
a stand-in `host.yaml`, then Nanite's `agent/authored` validators for variants and tool packages, and steer's for
`steering.json`); `TestEveryModuleProfileValidates` runs it over every `modules/*/profile`. A profile that does not
check out is left out at boot, with the reason in the log; the app still starts.

### Routes, events, marks

- `h.Handle("GET /items", handler)` serves the module's views at `/api/m/<id>/items`. Call it in `Start`.
- In an app with sign-in (`auth: identity`), every route needs a signed-in caller: `<app>.read` to read,
  `<app>.use` to change anything. `module.UserOf(r.Context())` says who (id, username, role, scopes); stamp
  records with it and check `HasScope("<app>.admin")` for admin-only actions. A route the module authenticates
  itself (a device token, a single-use code) or serves to anyone is wrapped: `h.Handle("POST /enroll",
  module.Public(handler))`. With sign-in off there is no user and `Public` changes nothing.
- A module that hands a credential to something outside the page (an agent it launches, a device, a webhook)
  mints an identity API key instead of keeping tokens of its own: `keys, ok := module.KeysOf(h)` (false with
  sign-in off), then `keys.Create(ctx, module.KeyRequest{Name, Scopes, TTL, CreatedBy})` (only the app's own
  `<app>.…` scopes), `keys.Revoke(ctx, id)` when it is done, and `keys.Verify(ctx, bearer)` on its
  `module.Public` route. The keys show in the app's key list and the owner can revoke them. In tests, set
  `moduletest.Host.KeyStore = moduletest.NewKeys()`. People also make keys of their own (the account menu's
  **API keys**) for their agents and scripts. `Verify` then returns `Caller.For`, the maker (`user:<id>`), and on
  the app's routes `module.UserOf` gives a service `User` whose `For` is that maker.
  Attribute such a caller's work to that person, not to the key.
- The app answers only at IPs, `localhost` and the names in `SURFACE_ALLOWED_HOSTS` (a guard against DNS
  rebinding). A module whose own clients still dial a name the app no longer serves can add names for its
  `module.Public` routes only: `ph, ok := module.PublicHostsOf(h)`, then `ph.Set([]string{"old.example.net"})`.
  It takes effect at once, and `ph.Set(nil)` removes them. A hub whose public URL changed uses it so its runners
  can reach it at the old name until they have moved. Only add names the operator configured, never one taken
  from a request. In tests, `moduletest.Host.PublicHostNames` records the last set.
- Every API key made or revoked in the app (a person's, an admin's, a module's) is published on the bus as
  `identity.key.created` or `identity.key.revoked`, with a `module.KeyEvent` (id, name, scopes, maker, expiry,
  who did it, when; never the secret): `h.Events().Subscribe(module.KeyEventTopic, …)`. For an activity feed or
  an audit log. `identity.*` events stay in the process: they are not streamed to the page.
- `GET /readyz` is the app's readiness probe (`/healthz` says only that the process answers). It is public and
  answers `{"status": "ok" | "degraded" | "down"}`, 503 when down: the worst of every module's. A module joins
  by implementing `module.Readier` (`Ready(ctx) module.Readiness{Status, Reasons}`); it is asked on each probe
  with a few seconds' deadline, so answer from what you already know. Serve the detailed report on a route of
  your own, behind sign-in. A module without it counts as ready once started.
- After any write, `h.Events().Publish("<id>.changed", …)`. Views listen and refresh; that is how a window
  learns the agent changed its data.
- `h.Marks()` pins notes to a part of something (`Ref`, `Quote`, `Anchor`). Do not build your own comments.
- `h.Desk()` opens, focuses and arranges windows. `h.RunCommand` runs any command.

### When the owner is working in a window

`Host.Desk().Windows` reports, for each window, whether the owner has unsaved work in it (`Dirty`) or unsent input
(`Composing`). A tool that would change the document a window shows, called by a delegate
(`module.ActorOf(ctx).Delegate()`), should refuse with a message that says to wait or ask. It should not overwrite
the owner's text. Drafts and Notes do this. A change to what the window shows (`Desk().Open` or `SetArgs` with new
args) is held by the desk for you, and returns `desk.ErrHeld`'s message (docs/desk-rules.md, rule 7). In tests,
`moduletest`'s `DeskState.SetWorking` and `ToolAs` set this up.

### Around, and errands for the agent

Two ways for a module to reach the agent outside its own slot:

- **`Manifest.Around`** returns a few lines the agent carries with every message, whatever the slot: what waits on
  the owner, an answer that came back. It runs on every message, so read what the module already holds. The core
  gives it 1.5 seconds and caps it at 1500 characters; `""` adds nothing. It shows in the context block as
  `Around (<module>):`.
- **`Host.Agent().Ask`** hands the agent an errand: a turn that did not come from the owner, such as mail that asks
  for something. It runs on the agent's conversation, headed as the module's errand. The router does not route it,
  and the context trace (the owner's messages, the classifier's training data) does not record it. It returns the
  run id without waiting; `module.ErrAgentBusy` while a run is active (ask again later); `module.ErrNoAgent` when
  the agent is not on the conversation wire. `moduletest.Host.AgentState` records the errands in tests.

### The model gateway

`Host.Agent().Gateway(ctx)` returns the model gateway the app's agent config names: the model, the gateway's URL
and the key file's path (never the key). It is read at each call, so an edit to the config takes effect at once;
`module.ErrNoAgent` when the config names none, and for a module whose manifest does not set `Gateway: true`. It is for a module that calls a model itself, and only on the
owner's action (the Workbench's live eval runs): read the key from the file at each request, and never log it.

### References (Cite)

A module that owns records people name in text (a task id, a path, an item) declares the kinds it
resolves, and the page shows each reference as a chip with a preview card wherever text is shown:

```go
Refs: []module.RefKind{{
	Kind: "work.item", Label: "Work item", ID: regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9]{8}$`),
	Ready:   func(ctx context.Context, h module.Host) bool { … },            // configured now? optional
	Resolve: func(ctx context.Context, h module.Host, ids []string) (map[string]module.RefEntity, error) { … },
	Record:  func(ctx context.Context, h module.Host, id string) (module.RefEntity, error) { … }, // optional
}},
```

`Resolve` gets a batch (at most 50) and leaves out what it did not find; an error means the lookup
failed, not that the record is missing. `Record` returns the whole record with its `Body` (Markdown)
for the record window, or `module.ErrRefNotFound`. A `RefEntity` may name a command that opens the
record in the module's own window (`Open`) and an outside link (`URL`). Resolving is reading: never
write anything, here or in a service it reaches. The core batches and caches the answers (45 s, 15 s
for not found) and serves them at `GET /api/cite/resolve` and `/api/cite/record`; the agent has
`cite_resolve`. The page finds references with the detector in `web/src/cite/core`: a new kind needs
a `RefKind` there too.

## The web side

```tsx
// modules/scratch/web/index.tsx
import { type ModuleWeb, useEvent, useSetting, type ViewProps } from "@hollis-labs/surface-page/sdk"

function Scratch({ win, desk, api }: ViewProps) {
  const [items, setItems] = useState<Item[]>([])
  const load = useCallback(() => void api.get<Item[]>("items").then(setItems), [api])
  useEffect(load, [load])
  useEvent("scratch.changed", load)                       // the agent or a command changed the data
  useEffect(() => desk.report(win.id, { kind: "data", ref: "scratch:items" }), [desk, win.id])
  …
}

export default { views: { "scratch/main": Scratch } } satisfies ModuleWeb
```

A view gets `win` (its window: `args`, `key`, `focused`), `desk` (open, close, setTitle, setArgs, report,
setDirty, run, confirm) and `api` (calls to the module's own routes). Hooks: `useEvent(prefix, fn)`,
`useSetting(fullKey)` (and `setSetting` to change one), `useMarks(ref)`; marks: `addMark`, `replyMark`, `resolveMark`; `Icon` draws a lucide
icon by name. `CiteText` and `CiteRef` draw references as chips (clicking a chip or `"View"` in its hover card opens the
record in the slide-over `<CiteDrawer />`); `RecordBody` draws a record's text as the Cite drawer/window does (a JSON
document as fields, with the raw JSON a toggle away).

- **Contained in-module navigation:** Clicking an item inside a module (a card, row, note, draft, or file) should navigate in-place within the current module window (`desk.setArgs(win.id, ...)` or local view state). Reserve opening a separate window for modifier-clicks (`e.metaKey || e.ctrlKey || e.shiftKey`, middle-click) or an explicit `"Open in new window"` icon button (`square-arrow-out-up-right`).
- **Reusable Object Actions (`<ObjectActionsMenu />`):** Render `<ObjectActionsMenu object={{ kind, id, title, subtitle, url, body }} />` on any referenceable object so the owner can copy its reference/Markdown link, add it to agent context, pin, bookmark, add a mark, forward it (Agent chat, Tether mail, Request status), send it to a location (Scratchpad, Frag, Notes, Drafts), or download it. Modules can register additional object actions with `registerObjectAction(actionDef)`.
- **Menu Nav Groups (`registerNavGroup`, `registerNavItem`):** Modules can register custom navigation groups (up to 3 levels deep, `MAX_NAV_DEPTH = 3`) or attach views/commands to existing nav groups with explicit `order` weights.

- `desk.report(win.id, state)` replaces what the window says it is showing; send the whole state each time.
  The keys the desk reads are `kind`, `ref`, `url` and `selection`.
- `desk.setDirty(win.id, true)` makes closing ask first.
- `useAnchor(win, name, label)` names a part of the window so the agent can point at it (ring it, spotlight it,
  draw an arrow to it, or walk the owner to it in a guide). Put the returned ref on the element:
  `<div ref={useAnchor(win, "marks", "The marks")}>`. The anchor goes when the element does; the agent lists them
  with `ade_anchors`.
- `desk.openFile(name, opts)` opens a file in whichever view declares it (`View.Opens`), or resolves to
  `null` when this app has none: a module offers "open" without knowing which other modules are present.
- Style with the design tokens (`var(--color-…)`), the shared `sf-*` classes in `web/src/desk/desk.css`, or
  Tailwind classes.

### A card kind of the module's own

A module that shows a card of a kind the core does not draw brings its renderer in its web entry:
`export default { views: {...}, cards: { "<kind>": Renderer } }` ("<kind>@<version>" for a version other than 1).
The renderer receives `CardProps`: the card, `respond(outcome)`, and the module's `api`. The page registers it
at start. Prefer the core's kinds (info, confirm, prompt, choice, form, approval, progress, plan) when one fits:
they are drawn the same everywhere and count in the attention list without more. A module kind counts there
when the card is `Blocking`. `modules/approvals` has one: the secret request.

## Add it to an app

List the id under `modules:` in the app's `surface.yaml`, then `make generate` (or `make <app>`). The
generator finds `modules/<id>` and `modules/<id>/web/index.tsx` itself. A module in another repository is
named in full:

```yaml
modules:
  - scratch
  - { id: planner, import: example.com/me/planner, web: ../planner/web/index.tsx }
```

## Tests

`module/moduletest` gives a module a Host without the core. The seam test (`module/seam_test.go`) fails if a
module imports anything of Surface but `module` and the helpers beside it: `module/tetherlink` (reaching Tether: Caster, Torque, mail, over MCP), `module/collab` (Caster's collab namespace as Basalisk keeps it, collab/v1: documents with revisions, conflicts and marks, so Desk and Basalisk share them) and the test helpers `module/moduletest` and `module/collab/collabtest`.
