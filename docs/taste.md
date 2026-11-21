# Surface Taste & Design System

Surface is an **Agentic Desktop Environment (ADE)**, not a traditional CRUD dashboard of passive tables, filter bars, and forms. Every window, card, inspector, drawer, and chat turn exists to support a deliberate **workflow or process** between the person, the agent, and the underlying systems.

This document defines the standing design philosophy, visual language, spatial window model, and interaction patterns for Surface and all apps built on it.

---

## 1. Core Principles

### 1.1 Behavior, Workflow & Process First
Traditional interfaces dump a table of records with a search box and filter dropdowns, leaving the person to do the cognitive synthesis. Surface interfaces organize around **what the person and agent are trying to accomplish right now**:
- **Deliberate Sessions over Passive Lists:** Processing an inbox or backlog is not "browse 200 rows." It is a goal-driven flow: *"Let's work through the GitHub repo reviews and land the research findings."* The UI presents the active queue, the current item's synthesized evidence, the comparison against prior art, and a one-click decision bar (`Land finding`, `Create follow-up task`, `Hold`, `Other…`).
- **Composable Operations:** Launching agents, updating a service registry, or rolling out changes across a fleet are staged workflows (`01 Validate → 02 Diff Registry → 03 Canary → 04 Fleet Rollout`) with live verification gates and rollback controls.
- **Explicit State & Ownership:** Every surface makes it obvious what the agent is tracking (via topic/context badges), what is waiting on the person (via inline or held Cards), what units of work remain (`x of y` progress), and what is actively running (via the folded Activity line).

### 1.2 Show, Don't Tell (Zero Wall-of-Text Policy)
Outside of free-form writing or reading a long-form document, **eliminate prose wherever a structured visual communicates faster**:
- **Architecture & Design Exploration:** Never answer "how does this work or what should we change?" with five paragraphs of text. Pair a 1–2 sentence executive summary with:
  1. **Current Architecture / State** (visual node graph or structured flow block)
  2. **Proposed Architecture / State** with **added, modified, and removed nodes/edges explicitly highlighted and badged**
  3. **Key Deltas & Tradeoffs** as compact side-by-side comparison cards or evidence chips.
- **Pipelines & Step Flows:** Represent multi-step processes as numbered horizontal or vertical pipelines (`01`, `02`, `03`) with semantic status accents and concise bullet facts.
- **Inline Citations & Evidence Chips:** Ground claims with clickable reference chips (`cite-chip`) that open the backing record or diff in a slide-over drawer on demand rather than pasting raw dumps inline.

### 1.3 Action in Front, Details Behind Drilldown
Every widget, card, or turn block must answer two questions at a glance: **"What do I need to know right now?"** and **"What action or decision is next?"**
- **Primary Surface:** Shows the headline signal, `x of y` progress or visual delta strip, and primary action controls.
- **Drilldown Surface:** Raw tool inputs/outputs, JSON payloads, full stack traces, context assembly blocks, and audit histories live behind one-click drilldowns:
  - **Folded Activity Line → Activity Timeline Window (`surface.activity`)**
  - **Cite Chip / Hover Card `View` → Slide-Over Cite Drawer (`<CiteDrawer />`)**
  - **Workstream Card Click → Inline Expandable Work Breakdown (`x of y` units + notes)**
  - **Context Pill → Context Modal (`Now` & `History`)**
  - **Info Icon (`surface.dev_mode`) → Developer Agent Inspector Modal**

### 1.4 Universal Object Agency (`<ObjectActionsMenu />`)
Any object that can be referenced across Surface—a Cite chip, an Operations workstream or unit of work, a Torque task, a Note, a Draft, a Work item, or a Registry record—must be actionable in place:
- **Standard Object Actions:** Every referenceable object exposes `<ObjectActionsMenu />` with grouped actions:
  - **Copy:** `Copy reference` (`kind:id`), `Copy as Markdown link`
  - **Context:** `Add to agent context` (attaches a removable context chip directly to the agent chat composer)
  - **Organize:** `Pin object`, `Bookmark`, `Add note / mark…`
  - **Forward / Share:** `Forward to Agent chat`, `Forward via Tether mail`, `Request status update`
  - **Send to Location:** `Send to Scratchpad`, `Send to Frag Capture`, `Save as Note`, `Create Draft from object`
  - **Download:** `Download (.md / .json)`
- **Extensible Callback Hook Pattern:** Modules and plugins register custom object actions or submenus via `registerObjectAction(actionDef)` and read them via `useObjectActions(obj)` from `@hollis-labs/surface-page/sdk`.

---

## 2. Visual Language: Linux Rice / Evolved Blade Runner

Surface's default theme evolves the **Blade Runner** Omarchy palette into a deliberate, high-craft Linux rice aesthetic: dark recessed slate surfaces, crisp 1px borders, active dual-accent gradient frames, and semantic color accents that break up monochrome walls without visual clutter.

### 2.1 Color Hierarchy & Semantic Accents

| Token | Default Value | Role |
|---|---|---|
| `--desk` | `#06090b` | Recessed desktop canvas behind tiled and floating windows |
| `--background` (`--p-bg`) | `#0c1215` | Window body & primary working surface |
| `--card` | `~5.5% fg mix` | Elevated containers, assistant turn cards, module headers |
| `--muted` | `~7.5% fg mix` | Code blocks, subtle wells, table headers |
| `--secondary` | `~11.5% fg mix` | Interactive secondary buttons, active pills, selected rows |
| `--primary` (`--p-accent`) | `#e5a463` (Warm Amber) | Primary actions, active focus indicators, folder icons, `h1` accents, list markers, blockquote callout bars |
| `--chart-2` (`--p-accent-2`) | `#7ecfd4` (Cyan) | Secondary accent: inline `code`, `h3` subheadings, topic/slot badges, read/search tool nodes, active links |
| `--chart-3` (`--p-magenta`) | `#c984b2` (Magenta) | Human-in-the-loop cards, decisions, syntax keywords |
| `--success` (`--p-green`) | `#6bcf8e` (Emerald) | Completed steps, verified checks, added diff nodes (`+`) |
| `--warning` (`--p-yellow`) | `#e0b04b` (Amber Gold) | Held changes, medium-risk approvals, modified diff nodes (`~`) |
| `--destructive` (`--p-red`) | `#e56358` (Coral Red) | Errors, high-risk actions, removed diff nodes (`-`), window close hover |

### 2.2 Deliberate Geometry & Radii
Avoid bubbly, consumer-web pill shapes (`rounded-full` / `rounded-4xl`) and oversized card corners (`rounded-xl`). Every corner radius is tight and intentional:
- **Window & Card Frames (`--radius-lg` / `--radius`):** `6px` (`0.375rem`)
- **Buttons, Inputs & Composer (`--radius-md`):** `5px`
- **Badges, Chips, Kbd & Icon Buttons (`--radius-sm`):** `4px`

### 2.3 Typography, Markdown & Module Density
- **UI Chrome (`--type-ui`):** Monospace (`CaskaydiaMono Nerd Font` / `JetBrains Mono`) for window titlebars, buttons, badges, table column headers, kbd shortcuts, and eyebrow labels.
- **Body Copy (`--type-body`):** Crisp sans-serif (`Inter`, `13px–14px`) for prose, or full monospace when `data-type="mono"` is active.
- **Rich Markdown Hierarchy:**
  - `h1`: Left accent border (`3px solid var(--primary)`), bold, warm primary tint.
  - `h2`: Clean bottom border divider, structured section break.
  - `h3`: Monospace uppercase eyebrow in `--chart-2` (cyan).
  - Inline `code`: Tinted pill (`color-mix(in srgb, var(--chart-2) 12%, var(--muted))`) with subtle border and cyan text.
  - Blockquotes & Tables: Left amber accent callout bar on blockquotes; uppercase mono headers and hover-highlighted rows on tables.
- **Top Application Header Bar (Basalisk Parity):**
  - **Left cluster:** Main menu button (`3-level nav flyout`), active module icon + title, horizontal open window tabs (with dirty dot and close `×`), and Command Palette search trigger (`⌘K`).
  - **Right cluster:** Focus history back button, tile/float arrangement toggle, saved layouts dropdown, active mode badge, theme/dark-light/type controls, attention indicator (`⌘.`), and the Surface Agent Panel toggle button.

---

## 3. Window, Navigation & Spatial Behavior

### 3.1 Full-Frame Module Switching & Per-Module Autotiling
- **Full by Default:** Modules open **full-frame by default**, even when another module is already open. Switching modules from the menu or command palette **replaces** the active module on screen rather than cramming unrelated modules into narrow side-by-side splits.
- **Restore Previous on Close:** When the active module window is closed, focus automatically restores the previous module the owner had in front (falling back to the focus trail).
- **Autotiling Within Modules (`View.Tiling`):** Autotiling happens *within* a module when that module opts into multi-pane tiling (`Tiling: "tile"`, such as `terminal`) or when multiple windows of the same module are open concurrently. In Terminal, clicking the `+` button tiles a new terminal session pane side-by-side inside the Terminal workspace.

### 3.2 Contained In-Module Navigation
- **Stay in Context:** Clicking an item inside a module (a workstream detail on the Operations board, a design job, a note, a draft, a Planner/Writer/Content item, a file preview, a fragment, or an image) opens it **contained within that module's window** with an in-view back breadcrumb.
- **Explicit Pop-Out:** Opening an item in a separate window is always a deliberate opt-in via **modifier-click** (`⌘`-click, `Ctrl`-click, `Shift`-click, or middle-click) or the item's **Open in new window** icon button (`square-arrow-out-up-right`).

### 3.3 Window Titlebars & Float/Dock Semantics
- **Right-Aligned Window Controls:** Window titlebars (`28px–30px` height) keep the module icon and title on the left and group all window controls on the far right in a strict order: `[module actions, Float/Dock toggle, Maximize/Restore, Close X]`, with **`X` always at the far right**.
- **Symmetric Float & Dock:** Tiled windows show a **Float window** button (`PictureInPicture2`); floating windows show a **Dock back into tile** button (`PanelTopClose`) alongside Maximize and Close so floating windows never get stranded.

### 3.4 3-Level Nav Groups Semantic & Menu Organization
- **Up to 3 Levels Deep (`MAX_NAV_DEPTH = 3`):** The main menu organizes views and commands into a 3-level hierarchy (`L1` domain group → `L2` subgroup → `L3` leaf group) with explicit `order` weights.
- **Module Hooks & Live Organizer:** Modules can register nav groups (`registerNavGroup`) and attach items (`registerNavItem`) via `@hollis-labs/surface-page/sdk`. Users can reorganize groups, change parent nesting, reorder items, toggle visibility, or reset defaults live in **Settings → Navigation & Menu** (persisted in `surface.nav_config`).

### 3.5 Command Palette (`⌘K`)
- **Two-Line Header & Immediate Autofocus:** The Command Palette renders a compact top mode bar (`Tabs` | `Commands` + Esc hint) above a dedicated full-width search input row.
- **Zero-Click Typing:** Opening the palette immediately autofocuses the search input (`autoFocus` + `requestAnimationFrame` focus; mode buttons use `tabIndex={-1}` so they never steal initial focus), with no nested inner input border or focus ring halo.

---

## 4. Core Interaction Patterns

### 4.1 Conversation Thread, Context Chips & Turn Framing
- **Clear Turn Separation:** Assistant responses render inside distinct framed turn cards (`border-border/80 bg-card/75 rounded-md`) rather than floating as raw text on the background.
- **Vertical-Only Scroll Lock:** The conversation viewport enforces `overflow-x-hidden` and `min-w-0 max-w-full` on turn frames so trackpad gestures never cause horizontal wobble.
- **Topic / Slot Labeling:** Each assistant turn header displays a compact mono topic chip (`#slot` / tracked subject in `--chart-2`) so multi-threaded work remains scannable.
- **Folded Activity Line:** Consecutive tool calls never flood the chat stream. They collapse into a single compact activity bar showing a live spinner/status dot, step count (`4 steps`), and mono tool trail (`files_list → notes_read → ...`). Clicking the bar opens the **Activity Timeline Window (`surface.activity`)**.
- **Composer Context Chips:** Any object sent via `Add to agent context` or `Request status` attaches as a removable mono badge above the composer input and is prepended to the next turn.

### 4.2 Cite Chips, Hover Previews & Slide-Over Drawer
- **Zero Layout Shift on First Hover:**
  - `initCite()` is awaited at boot before initial render so visible references batch-prefetch immediately (`10ms` batch window, `5 min` cache TTL).
  - Inline `<CiteChip>` text and `mono` styling stay fixed to the written token (`ops:id`, `CW-...`) when metadata resolves so paragraphs never reflow under the cursor.
- **Hover Card → Slide-Over Drawer (`<CiteDrawer />`):**
  - Hovering a Cite chip shows a compact preview card (`RefCard`) whose primary action is **`View`** (no separate `"Open"` button) alongside `<ObjectActionsMenu />`.
  - Clicking the Cite chip or clicking **`View`** in the hover card **immediately closes the hover card** and opens the record in the slide-over **`<CiteDrawer />`** on the right edge of the desk.
- **Compact Item Header (`CiteRecordHeader`):** Record views use a tight single-row eyebrow (`kind · id · status · actions`), a `15px` bold title, and an inline key-value metadata strip so the document body gets maximum vertical space.

### 4.3 Cards, Context Selector & Developer Agent Inspector
- **Context Pill & Context Modal:** The agent panel header displays the active context slot pill (`auto` or `manual` via `surface.context_mode`) which opens the **Context Modal** (`Now` assembly preview + `History` trace) without leaving the current workspace.
- **Developer Agent Inspector (`surface.dev_mode`):** Enabling Developer Mode adds an info (`i`) icon in the agent header that opens the **Agent Inspector Modal** (Active Profile & Instructions, Top-of-Mind Tools, Skills & Helpers, Live Prompt Assembly, and Context Trace & Signals).
- **Non-Blocking Cards & Wizard Pagination:** Any blocking card can be held (`"Hold & open in Cards window"`) to free the chat composer immediately. Multi-question wizard batches paginate `N` at a time (`surface.wizard_page_size`, default `3`) and every interactive card includes an `"Other: tell the agent…"` escape hatch.

### 4.4 Deliberate Settings Workbench (`surface.settings`)
- Settings is never rendered as a long, wide single-column form. It uses a **two-pane workbench**:
  - **Left Category Rail (`w-52`):** Instant search filter + logical categories (**Appearance & Theme**, **Workspace & Windows**, **Navigation & Menu**, **Agent & Context**, **Connections & Tether**, **Module Preferences**) and live override counters.
  - **Right Bounded Canvas (`max-w-2xl`):** Compact setting cards with segmented pill toggles for short enums (`2–3` options), modified badges, and one-click per-setting reset.

### 4.5 Operations (`ops`) Workstreams & Unit-of-Work Tracking
- **Unnumbered Launch-Order Queue:** The Queued band lists workstreams in launch order without visual number clutter, ordered via drag-and-drop or up/down arrows.
- **`x of y` Expandable Work Breakdown:**
  - Every workstream card computes `done of total` (`x of y`) units of work across child workstreams, `active_when` checklist items, and linked Torque tasks. As new tasks or checks are added, `y` simply increases.
  - Clicking a workstream card expands its work units inline on the board, showing each unit's status badge, inline notes, and actions.
- **Per-Unit Comments/Notes, Request Status & Itinerary Activity Views:**
  - Any workstream or individual unit of work supports inline comments/notes (`<UnitNotesInline />` backed by Surface Marks), a **Request status** action, and `<ObjectActionsMenu />`.
  - **Itinerary-Inspired Activity & Timeline Views (`BoardActivity` & `WorkstreamView` Activity/History):**
    - **Route / Stage Header:** Shows the flow progression (`Origin ⇄ In Flight · Workstream Title` or `All Workstreams ⇄ Live Activity`) above a pipe-separated aspect summary strip (`Workstreams | Units x of y | Local time`) and a dashed divider (`border-b border-dashed`).
    - **Episode Header Bars:** Groups events by day/stage episode with a tinted directional pill (`In Flight →`), bold date, scope summary, and clock icon with time span (`🕒 4h 25m · 6 events`).
    - **3-Column Spine Grid (`[Timestamp + Elapsed Delta] [Vertical Spine + Semantic Action Icon] [Milestone + Clickable Rich Card + Shaded Callout Div]`):**
      - Left column displays crisp 24h clock timestamps (`14:10`) at milestones and muted elapsed durations (`3h 25m`) along connecting segments.
      - Middle spine places action-specific semantic icons (`list-checks`, `arrow-right-left`, `refresh-cw`, `message-square`) directly on the vertical line.
      - Right column pairs a **Clickable Rich Item Card** (circular colored action badge, bold title, pipe-separated related aspects `Source | State | Stage | Owner`, `<ObjectActionsMenu />`, and a right chevron `>` to expand/inspect the richer view) with a **Distinct Shaded Callout Div** (`bg-muted/45 border-border/50`) containing a semantic header, bulleted details, and inline unit notes.
- **Full-Page Follow-ups Triage & Canonical Data Template (`ops/followup/v1`):**
  - Follow-ups (`caster://ops/followups`) open as a first-class full-page view (`Kanban | Cards | Activity | Fleet | Follow-ups`) rather than an inline list banner.
  - **Canonical Template (`ops/followup/v1`):**
    `- [KIND] <Title> | at: <Timestamp> | priority: <needs_you|high|normal|deferred> | owner: <Owner> | workstream: <ops:id> | refs: <CW-... / caster://...> :: <Executive summary> :: Action: <Decision or next step> :: Evidence: <Links, PRs, or root-cause notes>`
  - **Card Grid → Detail Modal:** Each follow-up renders as a structured card (semantic kind icon/badge for `DECISION`, `INCIDENT`, `POLICY`, `BACKLOG`, `STATE`, `TASK`, timestamp, 2-line headline, shaded action preview or summary, and extracted Cite references). Clicking any card opens a rich **Follow-up Detail Modal** with shaded callout sections, interactive Cite chips, one-click **"Append to Workstream Notes"**, inline triage notes (`<UnitNotesInline />`), and the formatted template line.

### 4.6 Golden Workflow Archetypes (`surface.workflows`)
1. **Architecture & Topic Explorer ("Show, Don't Tell"):**
   - Top summary strip (2 lines max) + KPI delta badges.
   - Side-by-side or toggled **Current vs. Proposed Architecture** visual diagram with highlighted change nodes (`+ Added`, `~ Modified`, `- Removed`).
   - Numbered implementation pipeline (`01 → 02 → 03`) with drilldown diffs.
2. **Guided Review / Triage Session (e.g., GitHub Repo & Prior Art Review):**
   - Session goal banner + progress stepper (`3 of 8 reviewed · 2 landed`).
   - Focused item canvas: repo/subject vitals, extracted novel patterns, architecture comparison against our stack, and agent recommendation.
   - Decision bar: `Land to Tesseract & Create Task`, `Bookmark Only`, `Skip`, or `Other: tell agent…`.
3. **Fleet & Registry Operations Pipeline:**
   - Staged execution pipeline with live health checks, registry diff preview, approval gate card, and per-node rollout status.
