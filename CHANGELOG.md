# Changelog

One version for the whole of Surface: the registry at the tag, the Go module and the
`@hollis-labs/surface-*` npm packages (`docs/releasing.md`).

## Unreleased

- **The desk on a narrow screen.** Below 720 px wide, the desk shows the focused window full width instead of
  tiling every window side by side, which made each one a few dozen pixels wide on a phone. A switcher in the bar
  lists the open windows and closes the one shown. The agent panel takes the whole screen while it is open. The
  saved arrangement is left as it is and comes back when the screen is wider. `web/src/desk/narrow.ts`.
## [0.19.0] - 2026-11-22

- **API keys in the account menu.** Everyone signed in can make keys for their own agents and scripts. Each key
  has a name, Read and act or Read only access to this app, and an expiry of 30 or 90 days, a year, or never.
  The secret is shown once to copy. You can see when each key was last used and revoke it. Administrators also
  see and revoke everyone's keys. A key acts as its maker and never with more than the maker holds (Substrate
  4576546).
- **`module.Caller.For` and `module.User.For`.** For an API key a person made, this is that person
  (`user:<id>`), on `Verify` and on the app's own routes (`UserOf`), so a module can attribute the key's work to
  them.
## [0.18.1] - 2026-11-22

- **Recovering a locked-out account.** A recovery link (`<app> identity recover --user <name> --url …`, made on the
  host) opens a page that signs that person in once. There they set a new password, remove their passkeys and
  turn off two-step sign-in, as needed. Needs Substrate 195cd13.
- **A passkey the app doesn't know is explained.** Signing in with a passkey that isn't registered here (removed,
  or saved for another install) now says so and points to the password form below it, instead of a generic
  failure. The steps that add a passkey suggest saving it to the same password-manager item as the password.
## [0.18.0] - 2026-11-22

- **Modules can add host names for their public routes (`module.PublicHosts`).** The host guard answers only
  at IPs, localhost and `SURFACE_ALLOWED_HOSTS`, read once at start. A module can now add names live with
  `module.PublicHostsOf(h)`, then `Set(names)` (nil removes them). They are answered for that app's
  `module.Public` routes only, never the page, sign-in or other routes. IPs, ports and wildcards are refused.
  The use case is a hub whose public URL changed: it keeps taking its runners at the old name until they have
  moved. `moduletest.Host.PublicHostNames` records the names in tests. Additive: the `Host` interface is
  unchanged.
- **Icons lucide 1.x renamed draw again.** `<Icon name>` names lucide dropped as aliases drew a dot: the account
  menu's Passkeys (`fingerprint`), and `trash-2`, `alert-triangle`/`triangle-alert`, `square-arrow-out-up-right`,
  `file-plus-2`, `history`, `code-2`, `building-2`, `shield-alert`, `check-circle-2`, `pause-circle`, `list-ul`,
  `archive-restore` in the modules. A test now checks every `<Icon name>` against lucide. Work kinds stored with
  the old names keep their icons.
- **Two-step sign-in (TOTP).** For apps with sign-in: the account menu's "Two-step sign-in" sets up an
  authenticator app (QR code or key, one code to confirm, ten recovery codes shown once to copy or download),
  makes a new set of recovery codes, or turns it off (each takes a code). Signing in with a password then asks
  for the code, or a recovery code. When Substrate's `identity.require_second_factor` lists the account's
  role, the gate asks for a passkey or an authenticator app before the app opens. In Users and invites, an
  admin sees who has two-step on and can reset it for someone who lost their app. Needs Substrate e25430c.

## [0.17.0] - 2026-11-22

- **Sign-in (opt-in), through Substrate identity.** `auth: identity` in an app's `surface.yaml` turns it on; the
  default `auth: off` leaves apps as they were, and `desk` and `studio` stay off.
  - With it on, Substrate serves the identity routes. Every other `/api` and `/mcp` request needs a verified
    credential: a session cookie, an API key, or a token minted from one. Reading takes `<app>.read`, and changes
    take `<app>.use`.
  - The page and its assets stay public, and so do module routes marked `module.Public`. Binding beyond loopback
    is allowed, because auth decides.
  - See `docs/sign-in.md`.
- **Module contract (additive).** `module.User`, `module.WithUser` and `module.UserOf(ctx)` give a module the
  signed-in user, with their role and scopes (`HasScope`, `Person`). `module.Public` marks a route that needs no
  sign-in. Modules written against 0.16 compile unchanged.
- **The CLI signs its requests** with an operator key kept 0600 in the app's data directory.
- **The page's AuthGate.** Setup from a setup link, sign-in, "set up on the host", and an expired session. Requests
  are same-origin, add `X-Requested-With`, and return to sign-in on a 401. An account menu offers changing the
  password and signing out. `mountPage` takes the generated `app` (name and title) for the gate; it is optional, so
  an existing call still works.
- **HTTPS, trusted proxies and passkeys** for apps with sign-in.
  - `<APP>_PUBLIC_URL` sets the identity public URL (Secure cookies, the passkey relying party), and its host is
    allowed by the host guard.
  - `<APP>_TRUSTED_PROXIES` names the proxies whose forwarded headers are trusted.
  - `<APP>_TLS_CERT` and `<APP>_TLS_KEY` serve HTTPS directly, re-reading the certificate when the files change.
  - The page offers "Sign in with a passkey" (in a secure context, when the server offers passkeys), an "Add a
    passkey" step after the owner is created, and a Passkeys dialog in the account menu (add, remove,
    passkey-only).
- **Invites and a users page.** An invite link (`/invite#c=…`) shows "Join <app>" with the invite's role;
  accepting creates the account and signs in. Admins get "Users and invites" in the account menu: people and
  their roles (change, disable), new invites for a role, and pending invites to revoke.
- **Module contract (additive): Keys.** `module.KeysOf(h)` returns the app's key service when it has sign-in, with
  no change to `Host`. A module can mint identity API keys with the app's own scopes for what it launches (an
  agent, a device), revoke them, and verify bearers on its `module.Public` routes. The owner sees and can revoke
  them. `moduletest.NewKeys` gives tests an in-memory store.
- **Substrate** bumped to 90857c1 (identity roles, setup links, passkeys, public URL, trusted proxies, invites).
- **The app's providers file.** The embedded agent can take its model providers from a Nanite `providers.yaml`
  the app keeps outside the agent directory: `<app data dir>/agent/providers.yaml`, or `providers_file` in
  `agent.json` (`SURFACE_AGENT_PROVIDERS_FILE`). It is staged into the agent directory before each turn, so an
  edit applies on the next message, and when it selects `use.chat` the agent needs no `gateway_url`, `key_file` or
  `model`. A module's settings page can offer Anthropic, OpenAI-compatible and gateway providers this way. An
  existing gateway configuration works as before (`docs/agent.md`).
- **`<app> call` waits as long as the tool does.** It had a 60 s client timeout, so a tool that waits on purpose
  printed nothing; `--timeout` sets one when wanted. The other client commands keep 60 s.
- **Evolved Blade Runner and a design pass.** A new theme, a taste guide (`docs/taste.md`), workflow golden
  samples, and refinements to the registry's card, dialog and cite components. Navigation gains 3-level groups,
  Settings becomes a two-pane workbench, and there is a reusable `ObjectActionsMenu` and a Cite drawer.
- **Ops redesign.**
  - Kanban cards show the full title and an activity pulse driven by the event bus, with footer actions and a large
    detail modal.
  - Activity and History are timelines; Follow-ups is a full-page card grid; Fleet has line-art server cards with a
    detail modal.
  - Agents, sessions and Usage get line-art emblems, stepped meters and click-into detail.
  - The board's view tabs and card actions are icon-only, and Kanban/Cards filters move to a view toolbar, so the
    subnav no longer shifts.
- **Registry as an operational wiki and knowledge graph.** Inline editing, icon-gated inspectors (fields,
  relations, schema, history and governance), a search-and-select relationship linker, readable object chips, and
  in-module back navigation. The overview sweeps larger draft catalogs.
- **Fixed:** chatstream card status and receipt pills, the submit toast's timing, and the Cards window's focus and
  auto-close.
- **Fixed: the workbench tests on macOS.** They wrote key files under `t.TempDir()` (`/var`, a symlink to
  `/private/var`), which the secrets resolver refuses. Six tests failed, and `TestRunsAreLimited` hung until the
  test timeout; it now fails after 30 s instead.
- **Sample data uses generic hosts and paths**, and the repository's history was rewritten to remove deployment
  names. Tags before this one were re-pointed at the rewritten commits; this is the first release cut from the new
  history.

## [0.16.0] - 2026-11-21

- **Ops: "assigned, no live agent".** A workstream's tasks that are assigned or claimed by nobody live are flagged
  on the board, in the drill-in and in Activity.
- **Terminal: links open whole, however they wrap.** A URL a program hard-wraps at the edge (as `claude
  setup-token` prints its login URL) is linked across its lines, as a soft-wrapped one already was; clicking any
  part of it opens the whole URL. Only http and https open, in a new tab, with no opener and no referrer. The
  web-links addon is replaced by the terminal's own provider (`registry/ui/terminal.links.ts`).

## [0.15.0] - 2026-11-21

- **Ops: `ops_brief`**, the Lead Orchestrator's running brief built from Ops data: what needs the owner or the
  orchestrator (holds, unsequenced orchestration requests, blocked tasks), stalled workstreams, what changed, in
  flight, live agents, fleet and budgets, each source with its freshness and the known gaps listed. An MCP read tool
  and `GET /api/m/ops/brief` (JSON or `?format=markdown`).
- **Ops Usage:** cards for the paid media suites (design, audio, video) and Tripo, through per-source adapters.
- **Fixed:** the Agents tab showed no rows after Tether's live-agents feed changed shape.

## [0.14.0] - 2026-11-21

- **The agent's own Tether mailbox.** With `identity.tether` in the agent directory's `host.yaml` and a `mailbox`
  section (`url`, `credential_file`, `poll_seconds`) in the agent config, Nanite reads the agent's own mail: each
  unread message becomes a notice turn, shown in the conversation as *Tether mail* and marked read once handled.
  Mail keeps its own Nanite history. No Surface module polls the agent's address alongside it (docs/agent.md).
- **Nanite v0.16.0** (Substrate BOM unchanged, b123217). Conversation integrity: an interrupted-turn offer
  with `integrity: blocked` shows only the offered discard and reset actions, with the reason. **Reset
  conversation** (owner only, refused while a run is active) moves the chat to a new Nanite conversation
  through `Runner.ResetChat` and keeps the old one on disk. The agent panel now draws every interrupted-turn
  offer (resume, discard, reset), and turns after a reset follow the chat's current Nanite conversation.
- **Ops Usage:** the Tether AI router has its own card with a labelled window (since 00:00 UTC, plus 30 days); the
  LLM gateway card says it is not collected yet; a budget that cannot be evaluated says why.

## [0.13.1] - 2026-11-21

- **Ops: Usage tab, first source.** Cards per paid or rate-limited source with usage against limit, reset time and
  a status colour; details show history and the biggest consumers. The llm-gateway card reads Tether's AI usage,
  budgets and budget alerts. Subscription sources (Claude, Codex, agy, opencode) follow.

## [0.13.0] - 2026-11-21

- **Ops: Agents view.** The live agents, awake or sleeping, with host, state and last activity. It reads Tether's
  unified live-agents feed when deployed; until then it falls back to Tether-launched sessions with a banner
  (kit-booted agents appear once the feed is live).
- **Ops: Fleet view.** The fleet's machines from Cerberus; a machine shows its agents and the last hour of CPU,
  memory, disk and load (5-minute samples). Uses Cerberus's fleet snapshot when it exists, else bounded host reads.
- **Nanite v0.15.0**, Substrate BOM b123217 (libs agentdir v0.6.0). Nanite's per-agent Tether identity and mailbox
  are not wired.

- **Ops: Agents view.** The board shows live Tether sessions and kit seats with agent identity, host,
  awake/sleeping state, last activity and workstream links. A shared bounded refresh also serves the
  read-only `ops_agents` tool; unavailable observations remain explicit.

## [0.12.1] - 2026-11-21

- **Ops: Settle.** A complete workstream can be settled: `settled_at`, `settled_by` and a final summary are recorded,
  and it is hidden from the board by default (never deleted; "show settled" and History still show it).
- **Ops: Send to orchestration.** Planned, parked and queued workstreams have an action that takes an optional note
  and sends the Lead Orchestrator a `sequence_and_launch` request (workstream, Registry identity, links, note); the
  document records `orchestration_request` and the feed shows it.
- **Browser:** a snapshot request without a URL answers "url is required".

## [0.12.0] - 2026-11-21

- **Ops: the owner's stage model.** Workstreams move planned → parked → queued → in flight → landing → complete
  (dropped stays). ops/v1 documents carry `planned | parked | queued | in_flight | landing | complete | dropped`;
  older values are read and mapped (planning → planned, in_review and adoption → landing, active → complete;
  planning or planned with `held` → parked), and an unknown stage is shown as a problem instead of being filed as
  planned. Ops writes only the new keys and keeps fields it does not know. A read-only comparison reports how the
  live documents map.
- **Front desk:** decided plans show as decided after a reload; the conversation's live streams close promptly on
  shutdown.

## [0.11.0] - 2026-11-21

- **Front desk (opt-in).** `"front_desk"` in the agent config turns on Nanite's front desk: a request can become a
  plan the owner accepts or declines in the panel, with background jobs whose results are delivered in order,
  once, across restarts (durable plan and delivery rows, recovery at start). Off by default.
- **Browser: styled snapshots.** A page that cannot be framed can open as a static, styled snapshot: fetched
  through the public-only fetcher, sanitized (no scripts, forms, frames or navigation), CSS and images inlined, shown
  in a fully sandboxed frame under a no-network CSP; links are listed beside it. Unsupported CSS is dropped per
  rule and counted. Logins and script-driven pages do not work there; Reader remains the fallback.
- A Scratch test no longer reads a row before the import wrote it.

## [0.10.2] - 2026-11-21

- **Fixed: Studio crashed on start in 0.10.1.** The agent's worker could ask a module to describe a desk window
  before the app had started that module (Workbench, with a nil library). Windows are described only once their
  module has started, and Workbench guards it too.

## [0.10.1] - 2026-11-21

- **Browser: real search results and a readable view.** A DuckDuckGo search shows a results list (titles, snippets,
  links) parsed server-side; a page that cannot be framed opens in a reader that keeps headings, links and images,
  rendered as structured blocks (no page HTML in the app).
- **Browser: every agent fetch is public-only.** One guarded fetcher for agent fetches and agent-opened windows:
  HTTP(S) on 80/443, resolved once and pinned, every redirect re-checked, loopback, private, link-local, tailnet
  and reserved targets refused, credentials stripped, size/time/concurrency limits. The owner's own browsing keeps
  reaching internal services, with a separate cache and no credentials.
- **Fixed: an approval card could be shown twice, or lose track of its answer, after a reconnect** (the card's
  record was lost when the reconnect cancelled it right after showing).
- **Old agent-folder snapshots are pruned** at start and after a rebind: kept when bound, among the newest 10, or
  used in the last 7 days.

- Prune old module-agent snapshots at boot and after rebinding, retaining current bindings,
  the newest 10 snapshots and seven days of use. Recovery reports a missing snapshot explicitly.

## [0.10.0] - 2026-11-21

- **Choose a module’s agent.** Settings and the agent panel’s Profile picker can bind any module to a
  validated Nanite agent folder. Bindings override built-in profiles and reset live. Turns use isolated
  authored snapshots and record the selected source folder in the run log. Bound agents share the app’s
  runtime and permissions; ignored `host.yaml` runtime keys are shown in the UI and logged. A bound agent's tool
  packages may not ask for environment passthrough or Warden tokens.
- **Workflows (`modules/workflows`, in Studio).** Workflow briefs with user, agent and system stages, a review
  handoff (prepared Markdown) and the designer's slot; the home for the workflow-designer agent.
- **Command palette search providers.** A module contract addition (`Manifest.Palette`): read-only async search
  from any module, with Notes, Files and Browser providing; a Tabs mode over open Browser windows (close, All or
  Focused scope). See docs/writing-a-module.md.
- **The top bar's Inbox shows the unread count**, live from the inbox module's existing check.
- **A Module-conventions pack** for agents that design Surface modules (`agents/packs/surface-module-conventions`).

## [0.9.1] - 2026-11-21

- **Ops finds plans and epics through linked tasks.** When a linked task belongs to a Torque plan (or epic), the
  workstream shows that plan expanded, marked with the task it was found through; its tasks join the roll-up and
  Activity within the same bounds, and "Link this plan" links it for good.

## [0.9.0] - 2026-11-21

- **Ops: Activity across workstreams.** A board-level Activity view: one feed, newest first, over every workstream
  (in-flight first in the refresh order), each row labelled with its workstream; filters for workstream, stage,
  source and time. `GET /api/m/ops/activity`; `ops_activity` without an id reads it; the ops slot shows the latest
  line. Bounded per refresh round (240 calls) with the per-workstream caches reused.
- **Ops: linked Torque plans expand into their phases and tasks** in the drill-in's Tasks tab, with live states; a
  plan's tasks count in the roll-up and feed Activity, within the same bounds.

## [0.8.0] - 2026-11-21

- **Nanite v0.12.1.** Fixed: the internal prompt that asks for a closing reply at the step limit no longer shows up
  in the conversation's history as an owner message, and no longer acts as a turn boundary for compaction (since
  v0.9.0). v0.12.0's tool registry is left off.
- **A daily token cap per agent** (Nanite v0.11.0): host.yaml `runtime.daily_token_budget`, 0 or absent for none
  (UTC day, survives restarts). At the cap a turn is refused with a readable 429 and its reset time; the 80% notice
  shows in the panel.
- **Stop everything** (owner only): `POST /api/agent/stop-all` and the agent panel's menu cancel the agent's running
  turns, subtasks and background work. The agent then refuses new work until the app is restarted.
- **Substrate BOM f70a163**: libs agentdir v0.5.0.

## [0.7.0] - 2026-11-20

- **Nanite v0.10.0 and an opt-in auto mode.** `"auto_approve": true` in the agent config turns on Nanite's
  AutoApprove (off by default; for testing): tool calls that would ask are allowed and logged, explicit denies still
  deny, a missing Warden grant fails the call at once instead of holding the chat. The startup log and
  `/api/agent/setup` show it.
- **A warning at start when the agent's authored context outgrows its Context budget**, and the budget in the
  startup log (`context_tokens`); a regression test with a large seat's context.

## [0.6.0] - 2026-11-20

- **The agent's context files arrive whole.** The Context slot's budget is the per-turn block Surface assembles
  (bounded as before) plus room sized at start for the agent directory's own `context/` files and the largest module
  profile's. Before, a 2000-token cap trimmed or skipped authored context (a seat's carried-over notes, the Agent
  Architect's facts). Changing context files takes effect at the next start.
- **Nanite v0.9.0.** The latest tool step survives step compaction; a turn that reaches its step or turn limit ends
  with a short closing reply; `max_tokens` and `max_steps` from the agent config reach Nanite as its limits.
- **Fixed: a flaky approvals test** now waits on the module's events instead of time.

## [0.5.0] - 2026-11-20

- **Nanite v0.8.0.** Conversations are created atomically by Nanite (`Agent.EnsureConversation`); Surface's own
  initialization lock is gone. Tool-package approvals and runtime state live in the app's data directory
  (`<data dir>/agents/<app>/`), no longer in the agent directory: approvals granted before this release are asked
  again once. Also from Nanite: Warden waits survive restarts, optional `runtime.cache_ttl`.
- **Compaction settings come from the agent's host.yaml** (`runtime.compact_at`, `runtime.compact_keep_tokens`),
  read by Nanite. Surface fills in 110000 and 22000 only for a value the file leaves out; the startup log shows the
  effective values.
- **Substrate BOM 3b89e9c**: libs agentdir v0.4.0, llm/core v0.5.0.

## [0.4.1] - 2026-11-20

- **The npm packages carry the version.** 0.4.0 was tagged without the package version bump, so its npm packages
  were not published; 0.4.1 is 0.4.0 with `@hollis-labs/surface-*` at 0.4.1. No code change.

## [0.4.0] - 2026-11-20

- **The agent runs on Nanite v0.7.0, and libs agent/host is gone.** With no `SURFACE_AGENT_RUNNER` set and a
  complete agent config, every Surface app's agent is a Nanite agent on the conversation wire (`scripted` remains
  for tests; any other value, including `host`, refuses to start with a clear error). Parity with the old engine:
  compaction at `compact_at` 110000 keeping `compact_keep_tokens` 22000 (host.yaml), the Context slot never
  compacted, turns interrupted by a restart recovered at start, Nanite steering in place of the old reflex path,
  per-tool timeouts (5 min, 70 min for tools that wait on the owner). The `agent/host` runner, the legacy Engine
  seam, the engine-served agent-host/1 routes under `/api/agent` and the page's agent-host/1 client
  (`packages/chat` `wire.ts`/`store.ts`) are removed; `/api/agent` keeps setup, context, slots, profiles, reflexes,
  rules and classify. An app with no model configured still starts and says so (setup `ready=false`, a turn 503).
- **Conversations from the old engine carry over.** The transcript is kept as it was; the first Nanite turn starts
  a fresh model history with a note that the earlier messages are in the transcript, and slot notes and standing
  instructions carry the context.
- **Fixed: a first turn could fail with 502 or 409.** Nanite conversation creation is serialized between turns and
  event steering, and startup reconciles the conversation before it accepts turns.
- **Desk shows the life of every message the owner sends** (`modules/assist`): Done for you → Sent has a timeline
  per message (sent, queued, delivered, read, replied) from Tether's event stream, alert cards for a recipient
  offline, a request not acknowledged and a reply that could not be delivered, and the request's Torque task when
  there is one. Request states (acknowledged, in progress, done, declined, blocked) are read from the request.*
  events as they land in Tether/Torque.
- **Workbench on Nanite 0.7.0's public packages**: `agent/providers`, `agent/authored` validators and in-process
  evals with stub tools replace Surface's stopgaps.
- **Nanite v0.7.0** (tagged), Substrate BOM 0d8ca5d, libs agentdir v0.3.0.


- **Module agent profiles (`Manifest.Profile`).** A module may ship an agent profile in `modules/<id>/profile/`: a
  Nanite agent directory without host.yaml or .mcp.json (agent.md; optionally context/, skills/, variants/,
  tools/, steering.json; evals/ is the Workbench's and ignored), embedded with `module.ProfileFS`. On the Nanite
  runner each turn runs as the conversation's pinned profile, else the focused module's, else the app's seat; the
  owner pins one with the Profile picker beside the slot in the agent panel (`POST /api/agent/profiles/pin`), the
  agent with `profile_switch` (act; a profile never changes tools, levels, grants or the Approver). Pins are kept per
  conversation. Nanite opens on a staged directory (`<data dir>/agents/<app>.staged/`, the seat with the profile
  over it, rewritten before each turn); the owner's agent directory is not changed. The run log has a `profile`
  column; `agent.turn` carries it. Profiles are checked at boot with libs agentdir and Nanite's `agent/authored`
  validators; one that fails is left out with the reason in the log. Variants are a profile's internal helpers
  (subtasks), and the `subtasks` and `skills` groups are turned on when a profile needs them; tool packages are
  offered only when the agent config turns `tools` on. Off on the host runner. `steering.json` is checked, not
  applied yet. The Workbench names its profile (the Agent Architect, `modules/workbench/profile/`), so Studio works as the Architect while the Workbench is in front.
- **Nanite main (v0.6.1-0.20261120160952-50b6e5fc121c)** (superseded by v0.7.0 in 0.4.0): authored validators, tool
  packages, steering and resume.
- **Operations (`modules/ops`, in Desk): the owner's workstream board.** A workstream is a concept of any size,
  from one task to a mesh-wide change, and nests: an optional parent, children worked out from the copy (Tether's
  `;parent=ws:` idea as a field). Each is one revisioned Caster document under a new `ops/v1` contract
  (`caster://ops/ws.<id>`; its id is a slug, 1:1 with the Registry workstream key), and the Queued band's launch
  order is one document (`caster://ops/queue`); the module's database is a working copy read again every 15 s, and
  every write publishes `ops.changed`. The board: Planning, Planned, a separate numbered **Queued** band (reordered
  by drag or up/down, among siblings only; advisory, it does not drive dispatch) and In Flight, with Review,
  Adoption and Active behind Later; Kanban and Cards; one level at a time, top concepts first, "N inside" steps in.
  Cards show the owner, held flag, age in stage, the Torque roll-up and the last activity, and a suggested stage
  when the tasks point elsewhere (moves stay explicit). The drill-in: Overview (children, links by role as Cite
  chips, `registry:` chips for Registry ids, the active-when checklist, purpose and notes), Tasks (the Torque
  roll-up, children's included), Activity (Torque task state, `STATUS` comments, status-stream rows where a
  `status_ns` is set, and Ops moves, in one timeline) and History (moves, and Caster revisions with a line diff).
  The agent's `ops_list`, `ops_get`, `ops_activity` (read) and `ops_create`, `ops_update`, `ops_move`,
  `ops_queue_reorder`, `ops_link`, `ops_unlink` (write), the `ops` slot, and the `ops.workstream` Cite kind
  (`ops:<id>`). Its own allowlisted Tether link (`ops.tether_command` or `ops.tether_url`) has no launch,
  dispatch, claim or team tool. A seed maps the owner's tracker (threads and follow-ups) into workstreams as a
  preview the owner edits and applies in the page (`ops_seed` opens one and writes nothing): stages map flight,
  queued, review and landed to In Flight, Queued, In Review and Adoption; tasks become `torque.task` links;
  follow-ups go onto the workstream they name or to the owner's list; proposed concept groupings stay off until
  ticked. A path to the tracker is read only from the owner's page, and a preview applies once. The routes that
  change something are the owner's (an outside client or the agent gets 403 and uses the tools), and a write
  that lands over someone else's is applied again to theirs rather than lost.

- **Registry (`modules/registry`, in Desk): a read-only browser for the Registry.** Kinds with record counts, the
  review queue and the diagnostics; the records of a kind, filtered by lifecycle and owner or searched
  (registry_record_search); a record's fields (the Cite structured view, with its JSON), status and owner, relations
  both ways grouped by type as Cite chips, revisions with a field-by-field diff between two, its review, and the raw
  record; a kind's schema with its fields and allowed relations. The working copy (drafts) by default, Published a
  toggle away. Refreshes on its button and every 30 s while in front. Every call is one of the Registry's read tools
  through the gateway (`registry.tether_command`); changes and review decisions are the agent's, with the Tether
  module's registry_* tools, on the owner's word: the `registry` slot puts them top of mind and tells the agent the
  record in front with its version token and definition hash. Registry records are a Cite kind,
  `registry.record`: `registry:<id>` (`registry:project:surface`) in chat, notes or the terminal is a chip that opens
  the record. The page's sdk exports `RecordBody` (the Cite record view's body) for a module to reuse.

- **Agent Workbench (`modules/workbench`, in Studio): make, edit, evaluate and improve Nanite agents.** An agent is
  an agent directory (libs agent/agentdir) in the Workbench's library, created from a template, duplicated, deleted to
  a trash, imported (a `.tar.gz` or `.zip` archive checked for unsafe paths, links and size, or a directory copied in
  read-only) and exported (the directory, a `workbench.json` manifest and the Workbench's sidecar; never `.nanite/`).
  Tabs for the prompt (agent.md, with Nanite's base prompt shown read-only), skills, tools (.mcp.json,
  permissions.yaml), subtask variants, providers.yaml, reflexes and triggers (libs agent/steer v0.1.0 config in the
  sidecar; Nanite applies it from its next release), context (host.yaml runtime knobs, `compact_at` as a runtime option, context files), history
  (every save a version with a digest and a note; diff and rollback), evals and the assembled prompt. Every save is
  validated as Nanite reads the files, literal credentials refused. Eval suites run through Nanite's eval harness:
  offline by default (each case's script stands in for the model), live only on the owner's confirmation, through the
  app's gateway within a dollar cap and token budgets; graders for text, regex, the ANSWER line, JSON paths, tool
  calls with arguments, steps, tokens, workspace files and a model-graded rubric; runs kept by digest and compared.
  From a run's failures a model proposes edits (file, diff, rationale) that the owner accepts or rejects one by one;
  accepting saves a version. Saves change only authored files, in place (Nanite's `.nanite/` state is never
  touched); eval runs happen in a child process so the server never changes directory; spending, accepting,
  rollback, delete and directory import are the owner's at the page only. `workbench_*` agent tools; none accepts a
  proposal, and the configuration files go through a confirm-level tool. See modules/workbench/README.md.
- **`Host.Agent().Gateway`** gives a module the model gateway the app's agent config names (model, URL, the key
  file's path, never the key and never marshaled), read at the moment of use, for a module that calls a model on the
  owner's action. Only a module whose manifest sets `Gateway: true` gets it.
- **Nanite v0.6.0; Substrate's BOM 768e5f7; libs agent/steer v0.1.0.**

- **(Interim, removed in this release) the agent could run on libs agent/host on the conversation wire** while
  Nanite caught up; superseded by the Nanite runner above.
- **Fixed: an answer to the agent's question is no longer lost when the run that asked has ended.** A `card_ask`
  whose run ends while the card is up (stopped, the app shutting down or restarting) now ends the card
  `abandoned`, not `canceled`, which stays the owner declining; a question left open by a killed app is abandoned at
  the next start. An agent's question that is `abandoned`, or `expired` because `card_ask` stopped waiting, takes one
  late answer (`late: true` on the card); a submitted one is sent to the agent as a turn (errand `late-answer`, out
  of the context trace), queued in the database while the agent is busy and delivered once. The page keeps such a
  card answerable with a note, and keeps what the owner types into an open card as a draft in the browser across a
  reload. See docs/agent.md.
- **Work: Planner, Content and Writer windows.** The Work module's views, ported from Basalisk: a workspace's
  board, outline, calendar, canvas and flow, and an item's detail with its review, sign-off, Torque link and
  revisions. Three windows on one component, one for each preset, with commands to open each on a workspace.
  See modules/work/README.md.
- **Files reaches folders on other hosts over ssh.** The live setting `files.remotes` is a JSON list of
  `{id, label, host, path}`. The host is an ssh destination from the owner's ssh config. Each remote is a place with
  the id `remote:<id>`: it is listed by `files_places`, shown in the Files window's place picker, and taken by
  every files tool's `place` argument. Tool names and schemas are unchanged. The host runs one fixed POSIX script
  per operation over `ssh -o BatchMode=yes -o ConnectTimeout=8`, and every argument is quoted. The denylist is checked
  both on the path given and on the real path the host resolves it to. Reads, listings, downloads (16 MB), searches
  and writes are bounded. The host needs GNU find and coreutils, as any Linux host has. In a remote place a search
  runs the host's grep, so a regular expression there is POSIX extended.
- **The classifier can mint its gateway tokens from Warden** (`classifier.warden` in the agent config, instead of
  `key_file`). Parent credentials are rotate-on-use. Mints hold a lock on the credential file, so apps can share
  one, and save the next parent before the token is used. A mint whose outcome is lost is never replayed: an
  `.inflight` marker stops minting until an operator re-enrolls. Tokens are renewed at two thirds of their life.
  See docs/agent.md.
- **Fixed: message texts are scrubbed before they go to the embedding gateway** (libs `core/util` v0.11.1). The JEV
  path already scrubbed them; the embeddings path (the slot centroid and the emerging report) did not.
- **The centroid judge is on the slot facet.**
  - It is fitted on each slot's description and the newest texts the owner's turns landed in or were moved to,
    and refitted every 25 learned turns.
  - It used to be on `act` and `affect`. A sentence-embedding model groups texts by topic, not by what they ask
    for ("how come the tests broke?" sits nearer "fix the failing test" than any question), so it can tell slots
    and not speech acts.
## [0.3.0] - 2026-11-19

- **The classifier's async tier speaks an LLM gateway.** The `classifier` agent config is now `gateway_url`,
  `dialect`, `key_file`, `decide_model`, `embed_model` and `purpose`. The default dialect, `llm-gateway`, uses JEV's
  System-1 API (`/v1/systemone`) and OpenAI-compatible `/v1/embeddings` with the gateway's model aliases. `tether`
  keeps `/ai/decide` and `/ai/embeddings`. The JEV judge is on only when `decide_model` is set. Changed:
  `decide_url` and `model` (v0.2.0) are now `gateway_url` and `decide_model`. Nothing had configured them yet.
- Docs: the `theme` package is named, and the release workflow's list must have it (#48).

## [0.2.0] - 2026-11-19

- **Modules ported from Basalisk**, in Desk and Studio:
  - **Terminal**: local login shells in PTYs that outlive their windows. Studio also gains Files. (#26)
  - **Viewer**: look at a picture, a video, a recording, a PDF or an HTML mock, and mark what you mean on it.
    (#28)
  - **Diagram**: drawings kept as revisions, with agent tools. (#29)
  - **Inbox**: the owner's Tether mail, read without consuming it, with `module/tetherlink`. (#30)
  - **Reader**: RSS and Atom feeds. (#31)
- **The page side of the desk rules**:
  - the grabbed window's id on a drag, Keep, the attention badge and jump key (#35);
  - the Context window, an inspector on the context trace (#37);
  - unsent input reported, held changes with Apply and Drop, the agent's answer marked seen, and the demo's ghost
    cursor (#40).
- **The chat package binds to the conversation wire**, and the panel follows. The agent is served on the wire with
  the scripted runner.
- **Theme and chrome**:
  - palettes, with every token derived from them and Blade Runner as the default (#23);
  - window borders that do not move on focus, a status bar and a theme picker (#24);
  - no scrollbars drawn in any Surface app (#21).
- **The Design module**:
  - live design review: the gallery on its own page (#9);
  - follow a job from the Designer's replies and its Torque task (#10);
  - approvals bound to the reviewed commit;
  - a draft-only review window and a details dialog (#20);
  - close a job from the window (#22);
  - Approve stays in reach in a narrow window (#25).
- Fixed: closing Attend while events arrive no longer sends on a closed channel (#41). Docs: the ADE framework
  feature list and the ADE audit synthesis (#42).

- **The classifier can pick the slot.** The live setting `surface.agent_selector`: `focus` (the default) or
  `classifier`. With `classifier` set, a sure answer (p ≥ 0.7) for another slot overrules the window in front; a pin
  still wins, and the trace says `classified`.
- **The classifier's centroid judges and emerging labels.**
  - With `classifier.embed_model` set, centroid judges on `act` and `affect` vote at the async tier, through the
    gateway's embeddings.
  - `GET /api/agent/classify/emerging` clusters the turns a facet abstained on, after collapsing near-copies: what
    the owner keeps asking that has no label yet.
- **The classifier's async tier, tone, and hint feedback.**
  - A decision gateway (JEV) named in the agent config file (`classifier.decide_url`) is asked about every
    facet after the turn, off its path. Its result is kept beside the turn (`classified_async`, with the
    `agent.classified` event). With no gateway named it is off.
  - A third facet, `affect`, labels tone (neutral, frustrated, urgent, positive).
  - `POST /api/agent/classify/feedback` keeps and learns the owner's answer to a composer hint. An accepted slot
    hint pins that slot.
  - Start-up training learns only what was kept before start.
- **A classifier labels every message in shadow**: what it asks for (`act`) and which slot it belongs to (`slot`).
  It is built on libs `core/util/classify`, and its result is kept beside the turn in the context trace
  (`classified`). Its slot model learns from the trace, and an owner's re-pin counts as a correction.
  `POST /api/agent/classify` classifies a draft. `GET /api/agent/classify/replay` scores a fresh classifier against
  the focus selector on the trace (`docs/agent.md`). The selector is unchanged.
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
- **Scripts: one walk-through that runs as an onboarding guide, a recorded demo, or a test** (`docs/scripts.md`).
  Each step is a guide step plus `do` (look-only tool calls that drive the desk, run as the agent) and `expect`
  (checks on the desk). It runs in three modes:
  - `guide`: on the owner's desk, moved on by them;
  - `demo`: on its own, with `script.step` events;
  - `test`: headless, a pass/fail report, the desk put back after.

  There are tools (`script_*`), HTTP routes (`/api/scripts`) and `scripts/record-demo.mjs`, which records a demo
  with headless Chrome's screencast (and ffmpeg, when present).

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
