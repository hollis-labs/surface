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
component the web entry exports. `Multi` allows several windows, told apart by `key`. `Describe` adds what the
module knows about one of its windows (`Kind`, `Ref`, `Extra`) to what the page reported: it is what the agent
is told about where the owner is. Use a stable `Ref` (`"<module>:<id>"`): marks are pinned to it.

### Commands

A command shows in the palette, can have a shortcut, and can be run by a view (`desk.run`), the agent
(`desk_run_command`), the CLI (`<app> run <id>`) or the API.

```go
{ID: "scratch.add", Title: "Scratch: jot something down", Section: "Scratch", Keys: "alt+shift+s",
	Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) { … }},
```

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

### Routes, events, marks

- `h.Handle("GET /items", handler)` serves the module's views at `/api/m/<id>/items`. Call it in `Start`.
- After any write, `h.Events().Publish("<id>.changed", …)`. Views listen and refresh; that is how a window
  learns the agent changed its data.
- `h.Marks()` pins notes to a part of something (`Ref`, `Quote`, `Anchor`). Do not build your own comments.
- `h.Desk()` opens, focuses and arranges windows. `h.RunCommand` runs any command.

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
icon by name.

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
module imports anything of Surface but `module`.
