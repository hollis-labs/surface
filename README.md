# Surface

A framework for **agent development environments (ADEs)**: apps where a person and agents work together over
the same windows, documents and data. A Surface app surfaces what the owner needs to know or decide, gives a
shared place to design and iterate, and shapes how an agent approaches the work through context, reflexes,
rules and nudges. Plain chat cannot do those things.

Surface is a [Hollis Labs](https://github.com/hollis-labs) tool, a sibling of
[Substrate](https://github.com/hollis-labs/substrate), which it is built on. It is **interface only**: it
reaches data and services through MCP, CLI, API or connectors and depends on none in particular.

> **Status: early.** The core, six modules and two apps exist and run. APIs, the module contract and the
> manifest may change. [Basalisk](https://github.com/hollis-labs/basalisk) is the prototype its design draws on.

## Shape

```
Surface app (a generated binary)  =  Surface core  +  modules
```

- **Core** — what makes it an ADE: the window manager (tile and float, saved layouts), the command palette,
  the agent panel, contexts and context slots, the router and reflexes, collaboration marks, and live settings.
- **Module** — primitives plus data plus AI. A module brings **views, commands and agent tools**, and may
  declare a local store, live settings and context slots. Modules are compiled in and kept apart from the core:
  the only package a module imports is [`module`](module/).
- **App** — a `surface.yaml` naming the modules. `surface generate` writes the wiring; `surface build` builds
  the page and the binary.

| App | Modules | |
|---|---|---|
| `demo` (`examples/demo`) | every module in this repository | A preset for CI and local development, not a product. Apps live in their own repositories and pick the modules they use: Basalisk (`hollis-skunkworks/basalisk`), Exo (`hollis-labs/exo`). |

## Run it

Needs Go 1.26.11+ and Node 22+.

```sh
npm ci
make demo            # generate, build the page, build bin/demo
./bin/demo           # serves http://127.0.0.1:8190
```

Data lives under `$XDG_DATA_HOME/surface/<app>` (or `<APP>_DATA_DIR`); the address is `<APP>_ADDR`. The
embedded agent needs a model gateway: see [`docs/agent.md`](docs/agent.md). Without one the app runs and the
panel says what to set.

The same binary drives a running app from a shell, and any MCP client can connect to `/mcp`:

```sh
./bin/demo open scratch
./bin/demo call scratch_add '{"text":"todo: try the palette"}'
./bin/demo settings surface.theme light      # live: the open page changes at once
./bin/demo layouts
```

## Write a module

[`modules/scratch`](modules/scratch) is the smallest complete one. See
[`docs/writing-a-module.md`](docs/writing-a-module.md).

```go
func init() { module.Register(&mod{}) }

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "scratch", Title: "Scratch",
		Views:    []module.View{{ID: "scratch", Title: "Scratch", Menu: true, Render: module.Component("scratch/main")}},
		Commands: []module.Command{{ID: "scratch.add", Title: "Scratch: jot something down", Run: m.add}},
		Tools:    []module.Tool{{Name: "scratch_list", Level: module.LevelRead, Run: m.list /* … */}},
		Store:    &module.Store{Migrations: migrations, Dir: "migrations"},
		Slots:    []module.Slot{{Name: "scratch", Views: []string{"scratch"}, Tools: []string{"scratch_list"}}},
	}
}
```

## Docs

- [`docs/taste.md`](docs/taste.md) — the Surface Taste & Design System: workflow-first philosophy, Evolved Blade Runner visual language, spatial window rules, Cite & Object Actions patterns, and golden workflow archetypes.
- [`docs/ade-framework.md`](docs/ade-framework.md) — architecture in one page and the complete feature inventory across core, modules, agent, cards, and UI.
- [`docs/architecture.md`](docs/architecture.md) — layout, the seam, the server-authoritative desk, data, config.
- [`docs/writing-a-module.md`](docs/writing-a-module.md) — the contract, with the page's side.
- [`docs/gui.md`](docs/gui.md) — how the interface is built, how to change a look or add a component so it reaches every app, and the extension points.
- [`docs/agent.md`](docs/agent.md) — the embedded agent, context slots, the router and reflexes.
- [`docs/substrate.md`](docs/substrate.md) — what Substrate supplies and where Surface goes its own way.
- [`docs/status.md`](docs/status.md) — what is done, how it is checked, and what is not there yet.
- [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md), [`SECURITY.md`](SECURITY.md).

Licensed under the [MIT License](LICENSE).

## Choosing embedded chat or a Tether seat

Nanite is the default embedded runner and keeps its approval panel (docs/agent.md).
`SURFACE_AGENT_RUNNER=nanite` selects it explicitly; `scripted` is available for tests.
To attach the same chat page to a statically app-bound Tether CLI seat instead,
add this to the app's `agent.json` (or the file `SURFACE_AGENT_CONFIG` names):

```json
{
  "tether": {
    "url": "http://127.0.0.1:9030/chat/v1",
    "seat": "surface-seat",
    "key_file": "~/.config/my-app/tether-token"
  }
}
```

`SURFACE_TETHER_URL`, `SURFACE_TETHER_SEAT`, and `SURFACE_TETHER_KEY_FILE` override
these fields. Omit `key_file` only for an endpoint that needs no bearer token.
The credential must have chat access to the named seat. It stays on the Surface
server and is reread per request. Tether configuration takes precedence over
`SURFACE_AGENT_RUNNER`; remove it to return to embedded chat.

Enable the chat wire on the target Tether daemon and configure that seat's static
MCP servers to include this Surface app's `http://127.0.0.1:PORT/mcp` endpoint.
The endpoint must be reachable from the CLI agent's host. See Tether's
[app-bound seats](https://github.com/hollis-labs/tether/blob/main/docs/app-bound-seats.md)
for launch configuration. Attaching a page does not inject MCP servers into an
already running agent.

The page uses `packages/chat` through Surface's same-origin `/api/agent/tether`
route, which forwards to `/chat/v1`. Streams, attachment leases, detach/reattach,
and replay cursors use the same conversation protocol. Surface adds its current
slot/window context and configured system instructions to each submitted turn;
Tether keeps that context out of the conversation event log. The Tether path
assembles context but does not run the embedded runner's routing/reflex or approval
hooks. The local context panel remains available.

MCP serves read and act tools to every client. A client that names itself with the
`X-Surface-Agent` header (a seat host sends its seat) also gets write and confirm tools, which
**ask the owner first** with a card naming the caller and wait up to ten minutes for the answer.
An anonymous client sees and calls read and act tools only. A write tool offers "Allow once",
"Always allow … for <caller>" (kept in the core database until revoked: `GET`/`DELETE /api/mcp/grants`) and "Deny"; a confirm tool asks
with an approval card every time.


## Project scaffolding

The [Surface preset pack](scaffolding/README.md) owns new-project templates and
their conformance checks. Load it explicitly with Substrate’s Folio generator.
It is independent of the running app and root framework build.
