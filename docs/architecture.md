# Architecture

A Surface app is one binary: the Surface core plus the modules its `surface.yaml` names, with the page embedded.
It serves on loopback, for one owner.

```
apps/<name>/surface.yaml ──surface generate──▶ substrate.yaml + substrate_wiring.go   (Substrate's shell)
                                               surface_wiring.go                      (blank imports of the modules)
                                               main.go, webui/embed.go
                                               web/src/generated/<name>.ts            (the modules' views)

main ─▶ app.Main ─▶ Substrate boot (config, logging, health, database, cli)
                 └▶ core.New ─▶ kernel: desk · commands · settings · marks · events
                               ├▶ each module: Manifest() read, store opened, Host built, Start(ctx, host)
                               └▶ agent service: engine seam · slots · router · reflexes
```

## Layout

| Path | What |
|---|---|
| `module/` | The module contract. The only Surface package a module imports. `module/moduletest` is a fake Host for module tests. |
| `core/` | `core.New(Options)`: the public assembly an app's main calls. |
| `core/internal/kernel` | Reads manifests, opens module stores, builds each module's Host, binds tools and slots, holds the core's own commands, tools and views. `Kernel` is the interface the core's services are given. |
| `core/internal/desk` | The window manager: a reducer over the desk's state, and saved layouts. |
| `core/internal/config` | Live settings: declarations, values in the database, watchers. |
| `core/internal/events`, `marks`, `storage` | The bus, the collaboration marks, SQLite open and migrate. |
| `core/internal/api` | The HTTP API, the event stream, the MCP endpoint, the page. |
| `core/internal/agent`, `slots`, `router`, `reflex` | The embedded agent and what shapes it ([agent.md](agent.md)). |
| `modules/<id>/` | A module: Go, and its views under `web/`. |
| `web/src/sdk` | `@hollis-labs/surface-page/sdk`: the page's side of the contract. `web/src/desk`: the desktop. `web/src/agent`: the agent panel. |
| `app/` | The body of a generated main: Substrate boot, serve, and the client commands. |
| `cmd/surface` | The generator. |

## The seam

A module imports `github.com/hollis-labs/surface/module` and nothing else of Surface. Two things enforce it:
the core is under `core/internal`, which Go will not let `modules/` import, and `module/seam_test.go` fails on
any other Surface import. What a module needs from the core it asks of its `Host`.

A `Manifest` is plain data apart from its handlers, and a view names how it is drawn (`Render`): today a
component compiled into the page, with `frame` reserved for a page the module serves itself. That is what keeps
run-time loaded and agent-generated modules possible: such a module is another implementation of the same
manifest, not a change to the core.

Modules register from `init` (`module.Register`), as `database/sql` drivers do; an app selects them by blank
import in its generated `surface_wiring.go`, the same idiom Substrate uses for its own modules.

## The desk is the server's

Window state (which windows are open, focus, the tree they are arranged in (tiled, tabbed, floating), the layout in use, the user's
mode) lives in `core/internal/desk`, is kept in the core database, and is streamed to the page. The page draws
it and posts actions (`POST /api/desk/actions`); it never changes window state by itself. Sizes in the tiling are
shares of the room, so the page works out the pixels from its own size. When the owner drags a window, the page
posts the tree as it now stands (`type: "tree"`); the server checks it holds each open window once and keeps it.

This is the one deliberate departure from Basalisk, where the page owns the windows and runs the agent's
tools. Here every tool is Go, so the embedded agent, a palette command, the CLI and an outside agent over MCP
all drive the same desk the same way, and a layout can be arranged and saved with no page involved.

What a window is *showing* still comes from the page: a view calls `desk.report(win, state)` (selection, the
record in front), the desk keeps it on the window, and the window's module may add to it (`View.Describe`).
That is `WindowContext`: what the agent and the context slots mean by "where the owner is".

Each change to the desk is made by an actor (the owner, the agent, a reflex, an outside client), and the desk
keeps who opened each window and holds the agent to the owner's say over the windows that are theirs:
[desk-rules.md](desk-rules.md).

## Layouts

A layout is an arrangement the owner can come back to: the windows (view, key, args, placement), the mode
(tile or float), and a **setup** that goes with it: the user's mode, standing instructions for the agent, the
reflexes that are on. `Desk.Capture` reads the present arrangement, `SaveLayout` keeps it, `Arrange` restores
one. The Interaction Builder is built on exactly this.

## Data

| Store | Where | Holds |
|---|---|---|
| Core database | `<data>/surface.db` (opened by Substrate's database module) | settings, desk state, layouts, marks; the agent's threads, slot notes, reflexes and rules |
| A module's store | `<data>/modules/<id>/<id>.db` | whatever the module's migrations define (`module.Store`) |
| A module's files | `<data>/modules/<id>/` (`Host.DataDir`) | files that are not rows |

`<data>` is `$XDG_DATA_HOME/surface/<app>`, or `<APP>_DATA_DIR`. Each store moves forward by numbered SQL
files, recorded per set, so several sets share one database. Remote stores with a cache are a later kind of
`module.Store`, not a change to this one.

## Config

Two different things:

- **App settings** are declared in code (`module.Setting`, and the core's own under `surface.*`), kept in the
  core database and changed live: from the Settings window, `PUT /api/settings/<key>`, the `settings_set`
  tool or `<app> settings <key> <value>`. A read (`Host.Config()`) always returns the present value, a watcher
  hears the change, and the page hears it on the bus. Nothing restarts. Try
  `./bin/desk settings surface.theme light` with the page open.
- **Agent and harness config** (model, gateway, key file, MCP config) lives in files ([agent.md](agent.md)).

Substrate's own config (`substrate.yaml`, overridable with `SUBSTRATE_CONFIG`) holds only what must be known
before the database opens: the listen address, logging, the database path.

## HTTP

| Route | |
|---|---|
| `GET /api/app` | The manifest: modules, views, commands. |
| `GET /api/desk`, `POST /api/desk/actions` | The desk's state; one action. |
| `GET /api/events` | The bus as server-sent events. The desk's state is sent first on every connect. |
| `GET /api/context` | What the focused window is showing, and the situation. |
| `GET /api/commands`, `POST /api/commands/{id}` | The palette's commands; run one. |
| `GET|PUT|DELETE /api/settings[/{key}]` | Live settings. |
| `GET|POST /api/marks`, `…/{id}/replies`, `…/{id}/resolve` | Marks. |
| `GET|POST /api/layouts`, `PUT|DELETE /api/layouts/{name}`, `POST …/load` | Saved layouts. |
| `GET /api/tools`, `POST /api/tools/{name}` | Every agent tool, callable. |
| `POST /mcp` | The same tools over the Model Context Protocol. |
| `/api/agent/…` | The embedded agent. |
| `/api/m/<id>/…` | A module's own routes (`Host.Handle`). |
| `/healthz` | Substrate's. |

## Outside agents

Besides the embedded agent, anything can drive the app: MCP (`/mcp`), HTTP (`/api/tools`, `/api/desk/actions`,
`/api/commands`), and the CLI (`<app> open|run|call|tools|desk|settings|layouts`). They get the same tools.
Asking the owner before a write is the embedded agent's panel's job; approvals proper belong to Gatecrasher,
for which the agent service has a hook (`agent.Approver`) and nothing more.
