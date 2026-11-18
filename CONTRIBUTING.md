# Contributing

How a change gets from your clone into `main`. Short on purpose: `AGENTS.md` is the orientation, and the docs sit next to what they describe.

## Before your first change

`README.md` has the build and run steps. Run `npm ci`, then `lefthook install`: a tracked config installs no hooks by itself.

## The sequence

1. **Branch.** `<type>/<short-slug>`, where the type matches the change: `feat`, `fix`, `docs`, `chore`.
2. **Change one thing.**
3. **Run the checks:** `make test vet lint`, `npm run typecheck && npm run lint`, and `make scan`. After editing an app's `surface.yaml`, run `make generate` and commit what it writes.
4. **Push, and open a pull request** against `main`.

Commit subjects follow the conventional-commit shape: a type, an optional scope, a colon, then the summary.

## What a pull request should carry

State what the change does, what it deliberately leaves alone, and the evidence that it works: the commands you ran and what came back.

A change to the module contract (`module/`) should say which modules it touches and whether a module written against the old contract still compiles.

## Things that surprise people

- **`apps/*/webui/dist/.gitkeep` must stay.** `go:embed` fails to compile without a match. Do not commit a built page beside it.
- **Everything in `apps/<name>/` except `surface.yaml` is generated**, and committed so a clone builds with `go build`. CI fails when it is stale.
- **The page's packages are at the repository root**, not in `web/`, so a module's `web/` directory resolves the same React as the core.
- **A module cannot import the core.** That is the design, not an obstacle: ask the `Host`, or propose a contract change.
- **Three accessibility lint rules are off** (`biome.json`): autofocus, and click handlers on non-interactive elements. A window manager focuses the palette's input and closes overlays on a backdrop click; both have keyboard equivalents (Escape, the shortcuts).
- **No secrets or real config in the repository.** It is public, and `make scan` is a CI check.
- **Use a disposable data directory** (`<APP>_DATA_DIR`) for experiments.

## What this does not cover

- **Which change is worth making.** That conversation happens in issues.
- **Release.** Surface has no tagged releases yet.
