# Status

What exists, what is checked, and what is deliberately not there yet.

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
| Cards, generated UI | not started |
| A baseline palette | undecided; the theme holds shadcn's neutral defaults |

## Not there yet

- **Nanite's embeddable package.** It does not exist yet; the agent runs on `libs/agent/host` behind the
  `Engine` seam ([substrate.md](substrate.md)).
- **Apps outside this repository.** The generator builds apps under `apps/`. An app in its own repository
  needs the page's core published as a package; until then a Folio preset for Surface apps has nothing to
  scaffold against.
- **Run-time loaded and generated modules.** The contract leaves room (`Render` kinds, a manifest that is plain
  data); nothing loads one.
- **The later contract:** data and service dependencies, module-declared skills, reflexes and triggers, and
  permissions put to the user at install.
- **Remote stores with a cache.** Modules have local stores only.
- **Choosing slots by classification.** The selector is the window in front (and the mode); the interface
  takes another selector.
- **Approvals.** A hook for Gatecrasher (`agent.Approver`); the default asks in the panel.
- **A desktop shell.** The app is served to a browser. Basalisk's native shell pattern (a web view on the
  loopback server) would fit unchanged.
- **Sign-in.** Loopback, single owner.
- **Rebinding shortcuts, and window snapping in float mode.**
