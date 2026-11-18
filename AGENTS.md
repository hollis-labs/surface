# Surface

A framework for agent development environments (ADEs): the Surface core plus modules, compiled into an app binary. Built on [Substrate](https://github.com/hollis-labs/substrate).
Not: a place for app-specific code (that is a module), an agent runtime (the agent is embedded behind a seam), an approvals system (Gatecrasher's; there is a hook), or a data service (interface only).

## Start here

- `module/` — the contract. The ONLY Surface package a module imports: `Module`, `Manifest` (views, commands, tools, store, settings, slots) and `Host` (desk, store, config, events, marks, routes).
- `core/` — the public assembly (`core.New`). Everything else is under `core/internal/`: `kernel` (assembles modules, hosts, the desk's own tools and commands), `desk` (the window manager and saved layouts; the state lives in the server), `config` (live settings in the database), `events`, `marks`, `storage`, `api` (HTTP, SSE, MCP), `agent`, `slots`, `router`, `reflex`.
- `modules/<id>/` — one module: Go at the top, its views in `web/index.tsx`. `modules/scratch` is the template.
- `registry/` — the `@surface` component registry and the theme (`registry/README.md`). Installed copies are in `web/src/components/ui`; `ui.lock.json` records them.
- `web/` — the page's core (`src/desk`, `src/agent`, `src/sdk`). A module's web code imports only `@surface/sdk`. Packages are installed at the repository root so `modules/*/web` resolves them.
- `apps/<name>/surface.yaml` — an app: its modules. Everything else in the directory is GENERATED (`make generate`); do not edit it.
- `cmd/surface` — the generator. `app/` — the generated main's body (Substrate boot, serve, the client CLI).
- `docs/architecture.md` first, then `docs/writing-a-module.md`.

## Commands

```sh
make desk studio                    # generate, build the page, build bin/<app>
make generate                       # regenerate wiring after editing a surface.yaml
make test vet lint                  # go test -race, go vet, golangci-lint
npm run typecheck && npm run lint   # the page
make ui-build ui-verify             # regenerate the registry; check the page's copies match it
make scan                           # secrets / hosts / private paths, tree and history
SURFACE_APP=desk npm run dev        # the page with hot reload, against a running bin/desk
```

## Boundaries

- **The seam.** A module imports `module` and nothing else of Surface. `module/seam_test.go` and Go's `internal` rule enforce it. If a module needs something the Host lacks, add it to the contract; do not reach around it.
- **Anything that draws comes from the registry.** Build views from `@/components/ui/*`; no raw `<button>`, `<input>`, `<select>` or `<textarea>`, and no restyling a component beyond layout. Never edit a file under `web/src/components/ui`: change `registry/ui`, run `make ui-build`, then `node scripts/ui.mjs sync`. `make ui-verify` and lint enforce this. Code written before the registry (the modules' views, parts of the desk) still uses `sf-*` classes through a bridge in `web/src/desk/desk.css`; move a file onto components when you touch it and add it to the lint override in `biome.json`.
- **The desk is the server's.** The page never changes window state by itself: it posts an action and draws what the stream returns. That is what lets the owner, the embedded agent and an outside client (MCP, API, CLI) drive the same desk. So agent tools are Go (`module.Tool.Run`), never page code.
- **Config is two things.** App settings (`module.Setting`) live in the app's database and take effect live: read them at the moment of use (`Host.Config`), never cache one, never require a restart. Agent and harness config (model, gateway, key file) lives in files.
- **Events.** After a write, publish `<module>.<thing>` on the bus; views listen with `useEvent` and refresh. Do not poll.
- **Marks are the core's.** Pin notes with `Host.Marks()` against a ref (`"<module>:<id>"`); do not build a module-local comment store.
- **Loopback only, no sign-in.** Do not add auth here; it is a later, portfolio-wide workstream.
- **Substrate supplies mechanism** (config file, logging, health, HTTP lifecycle, the database handle). Pins follow Substrate's BOM (`make pins`).
- The repo is public. No secrets, internal hosts, internal addresses or personal paths in files, commits or CI (`make scan`). Tags and releases are the owner's decision.
- Use a disposable `<APP>_DATA_DIR` for tests and browser checks.
