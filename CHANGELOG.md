# Changelog

One version for the whole of Surface: the registry at the tag, the Go module and the
`@hollis-labs/surface-*` npm packages (`docs/releasing.md`).

## Unreleased

- **The agent's runs announce themselves**, whichever engine runs them: `agent.run.started` and `agent.run.ended`
  (`finished`, `canceled` or `failed`, with a reason). Attention follows them. The subject `agent` is `working` while
  the agent runs, then `unseen` until the owner has read the answer, or `failed`. The context trace keeps each end
  as a `run_end` signal (`docs/attention.md`).
- **Draft-aware delivery** (desk rule 7). The page reports unsent input with the `composing` action, for a window or
  for the agent panel. A delegate's change to what a window shows, while the owner is composing or has unsaved work
  there, is **held** on the window. It applies when they are done, and the owner can apply or drop it at once (the
  `held` action, the `desk.held` event, an attention item). Unsent input in the panel keeps the focus where it is.
  Drafts and Notes refuse a delegate's edit to a document the owner is working in. `module.Window` gains `Dirty`,
  `Composing` and `Held`, and `moduletest` gains `ToolAs` and `DeskState.SetWorking`.

- **Attention: one list of what waits for the owner** (`docs/attention.md`). There are three states: `needs_you`
  (an open card that asks something), `failed` (an error card) and `unseen` (a window a delegate opened while the
  owner was elsewhere, or a question that expired unanswered). Items are derived from the bus, and only the
  owner's own move marks something seen. `GET /api/attend`, `POST /api/attend/seen`, the `attend.changed` event
  and the agent's `attend_list` tool serve it.

- **The context trace.** Every message records what it put in front of the agent and why: the window in front,
  the slots and why they were chosen, the routed rule, the fired reflexes, and what the budget cut. Beside it,
  the trace keeps what followed: a re-pin, an unpin, the owner moving to another window. This is the training
  set for a classifier that picks slots. `GET /api/agent/trace` and `/trace/export` (JSON lines) serve it, and
  the setting `surface.agent_trace` turns it off (`docs/agent.md`). `slots.Assembly.Trimmed` names the cuts.
  `desk.focus` now says who moved the focus (`by`) and to which view.

- **The desk knows who opened each window, and gives the focus back to the owner** (`docs/desk-rules.md`).
  Each window records `opened_by`, `opener`, `reason`, `retention` and `kept`. When the window in front
  closes, the focus returns to the newest window the owner had there, not to wherever the agent last
  looked. The agent, reflexes and outside clients may not take the focus from unsaved work or close the
  owner's windows. `desk_open` takes `reason` and `retention`. `module.Actor` names who is acting.
  A request without an `Origin` (MCP, the CLI) acts as `outside`.

- **The agent can run as a Nanite agent on the conversation wire** (`SURFACE_AGENT_RUNNER=nanite`): the
  app's tools with their levels, ask-first routing rules, standing approvals that hold across restarts,
  and an agent directory beside the agent config (`docs/agent.md`). Off by default.

Found by the first outside user of the tool (the Folio presets).

- **`surface-ui add` names exact versions.** Every package a registry item imports is recorded at the
  version this repository has installed, and `add` prints one `npm install --save-exact` line with them.
- **`surface-ui add --all`** installs every item the registry has at the lock's ref.
- **`verify` suggests the command that exists where it runs:** `npx surface-ui sync` in an app,
  `node scripts/ui.mjs sync` in this repository.
- Biome's `"recommended": true` (deprecated) is now `"preset": "recommended"`.

## [0.1.0] - 2026-11-18

The first release, and the first an app outside this repository can build on.

- **npm packages.** `@hollis-labs/surface-chat`, `@hollis-labs/surface-cards` and `@hollis-labs/surface-genui`
  (behaviour, no styling) build to JavaScript with types. `@hollis-labs/surface-page` is the page core
  (desktop, agent panel, cards, generated surfaces, ADE overlays) with `/sdk` for a module's views, `/gallery`
  and a Vite plugin (`/vite`); it draws with the app's installed registry components. `@hollis-labs/surface-ui`
  is the `ui` tool (bin `surface-ui`), now with `init` and `add --for <package>`.
- **Import names are the published names** inside Surface too: a module's views import
  `@hollis-labs/surface-page/sdk` (was `@surface/sdk`).
- **The theme is a registry item** (`theme`), installed at the same ref as the components.
- **The Components window is opt-in** for an app (`@hollis-labs/surface-page/gallery`); Surface's own apps
  keep it.
- **Checks.** `make pack-check` (in CI) packs every package, installs the tarballs in an empty directory and
  imports or bundles them there; `scripts/version.mjs` keeps every package on one version.
- The registry: 56 components and the theme. The Go module: `module`, `core`, `app`.
