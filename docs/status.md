# Status

What exists, what is checked, and what is deliberately not there yet.

The feature list by area, with what has landed, what is in review and what is planned, is [ade-framework.md](ade-framework.md).

## First milestone

| | State | How it is checked |
|---|---|---|
| Core separate from modules | done | Go's `internal` rule; `module/seam_test.go`; `core/api_test.go` runs a module through the core over HTTP |
| Windows and layout | done | windows tile, tab together and float in a tree the server owns (`core/internal/desk/tree.go`); the page draws it with the registry's `window-manager` (flexlayout-react, pinned) and posts back what the owner drags; focus history ("back"), maximize, saved layouts with their setup and exact arrangement; `core/internal/desk` tests |
| Palette and commands | done | commands from the core, the modules and the page; shortcuts; saved layouts in the palette |
| Agent panel | done | one embedded agent beside the windows; asks before a write |
| Contexts and modes | done | window context from the page's report and the module's `Describe`; the user's mode on the desk |
| Context slots | done | the slot, its top-of-mind tools and its knowledge follow the window in front; `core/agent_test.go` |
| Router and reflexes | done | rules answered without the model; reflexes and triggers; switched on per layout |
| Collaboration marks | done | core marks on any module's refs, by the owner and the agent; Drafts adds review and revisions |
| Notes, Files, Scratch, Drafts, Browser | done | each on the contract alone, with views, commands and tools |
| Live app config | done | settings in the database; a change reaches the page and the module with no restart; `core/internal/config` tests |
| Module agent profiles | in review | `Manifest.Profile` (`modules/<id>/profile/`); the agent works as the focused module's profile or the pinned one on the Nanite runner; `core/internal/agent/profile` tests (every `modules/*/profile` validates) and `core/profile_test.go` (a turn per profile through a fake gateway) |
| Second app with the Interaction Builder | done | `studio`: describe a flow, the agent arranges and saves it, refine it as versions |

## In progress: the interface standard

Surface is becoming the place every app's interface is built, on stock shadcn through the `@surface` registry
(`registry/README.md`).

| | State |
|---|---|
| Theme, registry (36 components), sync and drift check | done |
| The page built from registry components | done: the desk's bar, menu, window frames and palette, the core windows, and all six modules' views. No hand-written control or old stylesheet remains; lint fails on a raw control anywhere in the page |
| A plain frame for apps that are not a desk | done: the registry's `app-frame` (bar, rail and a resizable side panel, each optional, around the content). Surface's own page is on it, the agent panel in the side region, its width a live setting |
| The ADE overlays | done: `attention-layer` and `guide-bar` in the registry, `@hollis-labs/ade-core`'s hub in the page, and the agent's `ade_*` tools (point, spotlight, arrow, guide) on the server; views declare anchors with `useAnchor` |
| Chat on shared behaviour and registry components | done: `packages/chat` (wire, reducer, store, scroll-follow; no styling) and the registry's `conversation`, `message`, `markdown`, `prompt-input` and `tool`. Typing redraws only the composer, a token only its message, and the thread is read on one stated policy (`packages/chat/README.md`) |
| Cards | done | server-held cards (`core/internal/cards`): show, update, answer once, lapse by the server's clock, a late answer to the agent's question its run stopped waiting for (sent to the agent as a message), drafts kept in the browser; modules through `Host.Cards()`, the agent through `card_show`, `card_ask` (waits for the answer) and `card_update`; `packages/cards` (engine, outcome contract, form subset, renderer table) and ten `card-*` registry components; the agent's cards dock in its panel, others pop up, a blocking one takes the middle, all in the Cards window |
| Generated UI | done | Substrate's GenUI contract and gateway with Surface as the host (`core/internal/genui`): three reviewed patterns (approval, form, list with detail), surfaces kept as immutable snapshots, actions journaled and executed once, a second step for a high-risk choice; shown as cards; the agent's `genui_show`; `packages/genui` (contract, strict decoding, controller) and the registry's `genui-surface`; end-to-end test through the real gateway |
| Apps outside this repository | done: one version line (`vX.Y.Z` tags the registry, the Go module and the npm packages). `@hollis-labs/surface-page` (the page core, its sdk, a Vite plugin), `-chat`, `-cards`, `-genui` and `-ui` (the `ui` tool) build and pack; `make pack-check` (CI) installs the tarballs in an empty directory and uses them. Setup in `registry/README.md` |
| Live design review | slice 1 done: `make design-preview` serves the gallery on its own page with hot reload; the `design` module keeps design jobs, frames a job's branch preview beside its base, takes notes as marks, and hands rounds to the Design team (Tether message and manual Torque task behind one `Dispatcher` interface, or text to send by hand); progress is read from GitHub. The way back: while a job is open the module reads the Designer's replies on its thread (`design-<job id>`, only for jobs this instance holds, only from the configured Designer) and its Torque task on a timer and with `design_refresh`; labeled lines (`preview:`, `base:`, `pr:`, `head:`) set the job, the rest shows as text, and the first preview opens the review window with a notice. An approval names the commit it covers (`approved_head`, sent back as `approved-head:`); a head that changes while the card is open supersedes it; after approval the thread is read until `merged:` (of that commit) or `closed:`, for a week at most. Not yet: screenshot diffs, starting the preview from Surface |
| A baseline palette | undecided; the theme holds shadcn's neutral defaults |

## Agent Workbench (MVP)

`modules/workbench`, in Studio. Real today: the library, templates, import and export, validation on every save,
versions, the editors for every file Nanite reads, offline and live eval runs through nanite/evals with the cost
guard, run comparison, and improvement proposals the owner accepts. Recorded for later: reflexes and triggers (libs
agent/steer v0.1.0 config in the sidecar, applied once Nanite 0.7.0 is tagged), `compact_at` as a runtime option until agentdir has the
key, stub tools in evals (nanite/evals has no hook), a live provider from Nanite itself. Checked by
`modules/workbench` tests against a fake gateway. See [modules/workbench/README.md](../modules/workbench/README.md).

## Operations (MVP)

`modules/ops`, in Desk: the owner's workstream board (Torque CW-20261120-0117). Real today: workstreams as
`ops/v1` documents in Caster (`caster://ops/ws.<id>`, the launch order in `caster://ops/queue`), a working copy
read again every 15 s, nesting (a workstream is a concept of any size, with a parent and children), the board
(Planning, Planned, the numbered Queued band ordered by drag or arrows among siblings, In Flight; Review, Adoption
and Active behind Later; Kanban and Cards), the drill-in (Overview, Tasks, Activity, History), the agent's
`ops_*` tools and slot, `ops:<id>` in Cite, and a seed from the owner's tracker that is always a preview the owner
edits and applies. Activity is Torque (task state and `STATUS` comments), the status stream per `status_ns`, and
Ops' own moves, rolled up from children. Stubbed until they land: Registry identity by key and correlation
records (`workstream_ensure` not deployed; the identity link is by hand), trusted status writers (every status row
is "claimed"), a cross-project status read, plan expansion, and a `tether.team` Cite kind. Its Tether link has no
launch, dispatch, claim or team tool. Checked by `modules/ops` tests against collabtest's fake Caster and a fake
Torque. See [modules/ops/README.md](../modules/ops/README.md).

## Not there yet

- **Nanite's embeddable package.** It does not exist yet; the agent runs on `libs/agent/host` behind the
  `Engine` seam ([substrate.md](substrate.md)).
- **Automatic update pull requests** for apps on an older Surface tag. `surface-ui report` shows who is behind;
  nothing opens the pull request yet.
- **Run-time loaded and generated modules.** The contract leaves room (`Render` kinds, a manifest that is plain
  data); nothing loads one.
- **The rest of the later contract:** data and service dependencies, reflexes and triggers declared outside a
  profile, and permissions put to the user at install. Module-declared skills and helpers are the agent profile
  contract (`Manifest.Profile`, writing-a-module.md); a profile's `steering.json` is checked but not applied yet.
- **Remote stores with a cache.** Modules have local stores only.
- **Choosing slots by classification.** The selector is the window in front (and the mode); the interface
  takes another selector.
- **Approvals.** A hook for Gatecrasher (`agent.Approver`); the default asks in the panel.
- **A desktop shell.** The app is served to a browser. Basalisk's native shell pattern (a web view on the
  loopback server) would fit unchanged.
- **Sign-in.** Loopback, single owner.
- **Rebinding shortcuts, and window snapping in float mode.**
