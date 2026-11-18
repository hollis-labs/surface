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
| `desk` | notes, files, scratch, drafts, browser | The core modules. |
| `studio` | scratch, notes, drafts, browser, builder | Adds the **Interaction Builder**: describe a flow, and the agent arranges windows, modules and reflexes for it and saves that as a layout you can return to. |

## Run it

Needs Go 1.26.11+ and Node 22+.

```sh
npm ci
make desk            # generate, build the page, build bin/desk
./bin/desk           # serves http://127.0.0.1:8190
```

Data lives under `$XDG_DATA_HOME/surface/<app>` (or `<APP>_DATA_DIR`); the address is `<APP>_ADDR`. The
embedded agent needs a model gateway: see [`docs/agent.md`](docs/agent.md). Without one the app runs and the
panel says what to set.

The same binary drives a running app from a shell, and any MCP client can connect to `/mcp`:

```sh
./bin/desk open scratch
./bin/desk call scratch_add '{"text":"todo: try the palette"}'
./bin/desk settings surface.theme light      # live: the open page changes at once
./bin/desk layouts
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

- [`docs/architecture.md`](docs/architecture.md) — layout, the seam, the server-authoritative desk, data, config.
- [`docs/writing-a-module.md`](docs/writing-a-module.md) — the contract, with the page's side.
- [`docs/gui.md`](docs/gui.md) — how the interface is built, how to change a look or add a component so it reaches every app, and the open design decisions.
- [`docs/agent.md`](docs/agent.md) — the embedded agent, context slots, the router and reflexes.
- [`docs/substrate.md`](docs/substrate.md) — what Substrate supplies and where Surface goes its own way.
- [`docs/status.md`](docs/status.md) — what is done, how it is checked, and what is not there yet.
- [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md), [`SECURITY.md`](SECURITY.md).

Licensed under the [MIT License](LICENSE).
