# Surface / ADE framework: architecture and features

Status as of 2026-11-19 07:50 UTC (Local Dev's night handoff). Repo: github.com/hollis-labs/surface. main = a4435b2, CI green, no open PRs.
Legend: [x] landed on main · [~] partly there / in review · [ ] planned or noted

Home: this doc lives in the repo at `docs/ade-framework.md`; Local Dev keeps it current. Tracked in the Portfolio Planner under ADE development (hu3p0btj).

Sources: Local Dev handoff (tess project/sysadmin/workspace/handoff, key local_dev_2026_11_19_0750; file ~/dev/workspaces/agent-state/sysadmin-local-dev/HANDOFF.md); repo docs (architecture.md, status.md, agent.md, desk-rules.md, attention.md, scripts.md, gui.md); ADE audit synthesis ([ade-audit-2026-11-18.md](ade-audit-2026-11-18.md); the permanent record is in Tesseract); Torque tasks named below.

---

## 1. Architecture in one page

A Surface app is **one binary**: the Surface core, plus the modules its `surface.yaml` names, with the page embedded. It serves on loopback for one owner. `surface generate` writes the Substrate shell (substrate.yaml and wiring), the module wiring (blank imports) and the page's generated views.

```
main ─▶ app.Main ─▶ Substrate boot (config, logging, health, database, cli)
                 └▶ core.New ─▶ kernel: desk · commands · settings · marks · events
                               ├▶ each module: Manifest, store, Host, Start
                               └▶ agent service: conversation runner · slots · router · reflexes
```

Load-bearing decisions:
- **The module seam.** A module imports only `surface/module` (plus `moduletest` and `tetherlink`). Two things enforce this: the Go `internal` rule and `module/seam_test.go`. A manifest is plain data plus handlers, so run-time loaded and agent-generated modules can come later without changing the core.
- **The desk belongs to the server.** Window state lives in `core/internal/desk`. The page draws it and posts actions. So the embedded agent, the palette, the CLI and outside agents over MCP or HTTP all drive the same desk the same way. This is the deliberate break from Basalisk.
- **Every tool is a server tool.** The same tools are served at `/api/tools` and `/mcp` and through the CLI.
- **Runtime.** Nanite is the default embedded agent on the durable conversation wire. Tests use the scripted conversation runner.
- **Module profiles.** One agent, many profiles: a module's profile changes what the agent is told and which helpers it has, never its tools, approvals or identity.
- **Two kinds of config.** App settings live in the DB and change live. The agent and harness config lives in a file (`~/.config/<app>/agent.json`).
- **Data.** One core SQLite DB, plus a store and a data dir per module.
- **Approvals belong to Gatecrasher.** Surface only leaves the `agent.Approver` hook.
- **The UI standard.** It is stock shadcn through the `@surface` registry. The window manager is flexlayout-react, pinned. Lint forbids raw controls.

Apps today: **Desk** (:8190, running Nanite) and **Studio** (:8091, with the Interaction Builder).

---

## 2. Features

### Core and app shell
- [x] Core separate from modules (seam test, `core/api_test.go`)
- [x] Generator (`cmd/surface`): from surface.yaml to Substrate shell and wiring
- [x] Windows and layout: tile, tab and float in a tree the server owns; focus history; maximize
- [x] Saved layouts with a setup (mode, standing instructions, reflexes)
- [x] Palette and commands (core, modules, page), with shortcuts
- [x] Live app config: settings in the DB, no restart needed
- [x] Collaboration marks on any module's refs (owner and agent)
- [x] HTTP API, SSE event bus, MCP endpoint, CLI (`open|run|call|tools|desk|settings|layouts`)
- [ ] Remote stores with a cache (modules have local stores only)
- [ ] Desktop shell (it is served to a browser today; Basalisk's web-view pattern would fit)
- [ ] Sign-in (loopback, single owner today)
- [ ] Rebinding shortcuts; window snapping in float mode

### Modules
- [x] Notes, Files, Scratch, Drafts, Browser
- [x] Design module and review window (design jobs, branch preview against base, marks, rounds to the Design team, approval bound to a commit)
- [x] Basalisk ports: terminal, files, viewer, diagram, inbox, reader
- [x] Builder (Interaction Builder, in Studio)
- [x] **Approvals** (`approvals`, CW-20261119-0041): Gatecrasher's approvals as cards. The module follows Gatecrasher's event stream as the owner (`approvals.url`, `approvals.key_file`), resuming from the last seq handled. Each pending approval is shown as a card: a grant on the core's approval card (one change per scope, so the owner can narrow; a critical one asks for the phrase), a choice on a choice card, a question on a prompt card. The owner's answer is sent to Gatecrasher's decide, and the card goes when the approval closes anywhere. A secret request is the module's own card (`gatecrasher.secret`), whose form posts the value from the page straight to the producer's intake; it never reaches the server, its database, its log, the bus or the agent (a canary test checks this)
- [ ] Remaining Basalisk ports: Frag, Planner, Contexts, Done for you, Story, Writer, Content
- [ ] Reader's Fragments Engine half
- [ ] Move Design onto `module/tetherlink`
- [ ] Run-time loaded and generated modules (the contract leaves room for them)
- [~] **Module agent profiles** (CW-20261120-0128, in review): the contract for module-declared skills and helpers. A module ships an optional profile, `modules/<id>/profile/` (a Nanite agent directory without host.yaml or .mcp.json: agent.md, context/, skills/, variants/, tools/, steering.json; evals/ is the Workbench's), embedded and named by `Manifest.Profile` (`module.ProfileFS`). On the Nanite runner each turn runs as the pinned profile (the panel's Profile picker, or the agent's `profile_switch`, act level; kept per conversation in `agent_profile_pins`), else the focused module's, else the app's seat. All profiles share the conversation, front desk, mailbox, tools, levels, grants, the Approver and the identity (one ADE, one Warden grant); internal helpers are Nanite variants inside a profile, not identities. The run log's `profile` column says which profile acted. Off on the scripted runner. Not yet: steering.json applied (Nanite takes steering once, at open), a per-request header naming the profile (Nanite's gateway headers are static), split grants per profile (Warden token exchange). Format: writing-a-module.md, "An agent profile"
- [ ] The rest of the later contract: data and service dependencies, module-declared reflexes and triggers outside a profile, install-time permissions

### Embedded agent
- [x] Agent panel: one agent beside the windows, asks before a write
- [x] Nanite embedded runtime and scripted test runner
- [x] Context slots: chosen by the window in front and the mode, with a pin through `context_switch`, per-slot notes, live Knowledge and budgets
- [x] Router: rules answered without the model (`/open`, `/layout`, `/run`, plus the owner's rules)
- [x] Reflexes: text, predicate, event and focus triggers; remind, halt, run_command and call_tool actions; switched on per layout
- [x] Approval levels read/act/write/confirm, plus the `Approver` hook
- [x] **Context trace** (#33, CW-20261119-0009): each turn's situation, slots, tools and cuts, plus 15-minute signals (pin, unpin, focus). This is the classifier's training data.
- [x] **Context inspector** (#37): the `surface.context` window. Now shows `GET /api/agent/context`, History shows `/api/agent/trace`, and corrections are flagged.
- [x] **Run end states** (#36, CW-20261119-0011): each run announces how it ended (working, unread, failed)
- [x] **Conversation wire** (`libs/agent/chat`): default for configured embedded agents; durable conversations, viewer/controller attach, and standing approvals.
- [x] **Nanite runner**: default runtime, authored host.yaml settings, 110000/22000 compaction, protected Context, recovery, tool deadlines, and Surface reflex steering.
- [ ] **Classifier-driven Selector**: the owner has an agent building it overnight, and its PR(s) are expected. Nothing in the 33 audited repos does this.
- [ ] The engine as an MCP client (the `mcp` config field is reserved)
- [x] Conversation wire is the default; legacy runtime removed and old transcripts preserved

### Desk rules and attention (from the ADE audit)
- [x] **Desk rules** (#32, CW-20261119-0008): every window records who opened it, focus goes back to the owner's window, and delegated windows follow etiquette. The owner's rulings (tess `desk_rules_owner_rulings`): when the owner moves a window, it becomes theirs; agent windows open beside the window that opened them; zoom is a camera zoom, not a loupe; the CLI counts as outside until sign-in.
- [x] **Attention model** (#34, CW-20261119-0010): needs_you, failed and unseen are worked out from the event bus
- [x] **Draft-aware delivery**, desk rule 7 (#38, CW-20261119-0012): changes are held while the owner types
- [x] Page side (#35, #40): composing reports, held-change Apply/Drop on window tabs, agent answers marked as seen
- [x] Attention shutdown race fix (#41, CW-20261119-0014)

### ADE overlays, guides and scripts
- [x] `ade_point` (highlight, spotlight, arrow), `ade_clear`, guides (`ade_guide_*`, which wait for the owner or for a view), and anchors (`useAnchor`)
- [x] Ghost cursor and camera zoom for script demos (#40)
- [x] **Scripts** (#39, CW-20261119-0013): one walk-through works as an onboarding guide, a recorded demo and a test; demo recorder (ffmpeg)
- [ ] Saved flows become regression tests
- [ ] The agent screenshots its own UI before showing it
- [ ] Screenshot diffs for design review (also CW-20261118-0001: screenshot comparison in CI)
- [ ] Accessibility snapshots as context
- [ ] Bug reports as exact replays from the desk event log

### Cards and generated UI
- [x] Cards held on the server: show, update, answer once, lapse by the server's clock; `card_show`, `card_ask`, `card_update`; ten `card-*` registry components
- [x] GenUI with Surface as host: three reviewed patterns (approval, form, list with detail), immutable snapshots, journaled actions run once, a second step for high-risk choices
- [ ] Card lifecycle (audit gap 4): end-of-turn next-actions, a card lapses when the owner types instead of answering, edit before approving, approval bound to a hash, lapse tied to state, a per-turn budget for questions, a blocked-on-you cascade
- [ ] GenUI additions (gap 9): a next-actions card, a sandboxed visual with a screenshot self-check, a report kit restricted to theme tokens

### Chat
- [x] `packages/chat` (wire, reducer, store, scroll-follow) and the registry components conversation, message, markdown, prompt-input and tool
- [~] Chat composer mock-up (Design, CW-20261118-0040, in review)
- [ ] Input semantics, the rest of gap 3: steer, queue and interrupt as separate actions; stop holds the queue; a stall banner; a `/btw` side-chat fork

### Interface standard, theme and design
- [x] Theme, registry (36 components), sync and drift check; the page built entirely from registry components
- [x] `app-frame` for apps that are not a desk
- [x] Theme system: Blade Runner default, mixed and mono type, Hyprland borders, bar picker, dark only; scrollbars hidden
- [x] One version line (`vX.Y.Z`) for the registry, Go module and npm packages; `make pack-check`
- [x] Live design review, slice 1
- [ ] **Design polish**: the owner's next focus. The base theme is meant to be a good starting point for Basalisk and other apps. (Design rounds CW-20261119-0006 palettes and 0007 bar and window chrome; approval card mock-up 0004)
- [ ] `surface-ui report` shows whether a newer registry tag exists (CW-20261118-0002)
- [ ] Automatic registry-update PRs for app repos (CW-20261118-0003)
- [ ] ui-shots for palettes; Diagram fit zoom (about 22% in half-width windows)

### Still-open audit gaps (not yet started)
- [ ] 5: Context assembly: snapshot once, then send only the changes; explicit attachments outrank implicit focus; ambient dwell observations; sticky selection with a no-match cooldown
- [ ] 6: The classifier's data loop beyond the trace: decision snapshots, implicit signals (silence counts as acceptance), happier's 4 request modes as a first classifier head, request origin
- [ ] 7: Ways of working written by the agent: propose-only activation, change set then partial apply then undo, a recursion guard
- [ ] 8: Default reflex families: a loop and drift monitor, auto-retry for classified errors, watches and self-follow-ups, durable awaits, armed-not-run for risky commands
- [ ] 10: The owner pointing for the agent: Pinpoint, element annotation, select-then-ask
- [ ] 11: Tool surface hygiene: exposure per surface, a discoverable-only tier, evals over tool wording, typed partial answers, large results spilled to a window

### Housekeeping
- [ ] Inbox: unread cap (15 per address); polling isn't joined on Stop
- [ ] Prune merged worktrees (24 under ~/dev/hollis-labs/worktrees, many stale)
- [ ] Sending identity: apps send as the operator and need their own (Tether CW-20261118-0017); local agents not started

---

## 3. Decisions and open questions
- **Audit synthesis (decided 19 Nov, owner):** both. It is a permanent Tesseract record and is in the repo as [ade-audit-2026-11-18.md](ade-audit-2026-11-18.md).
- **This doc (decided 19 Nov, owner):** lives in the repo's `docs/`; Local Dev keeps it current.
- Baseline palette: undecided (shadcn neutral defaults today).
- Classifier PR merge order: the classifier agent's author gives it.
