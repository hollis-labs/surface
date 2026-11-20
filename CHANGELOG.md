# Changelog

One version for the whole of Surface: the registry at the tag, the Go module and the
`@hollis-labs/surface-*` npm packages (`docs/releasing.md`).

## Unreleased

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

- **The agent can run on libs agent/host on the conversation wire** (`SURFACE_AGENT_RUNNER=host`): Basalisk's
  runtime, with every feature of the wire (late answers, module errands, the input lease, the run log, approvals in
  the chat). The system prompt is fixed and the turn's context block goes in front of the owner's message in the
  same stored message, so the history stays byte-stable and the provider's cache holds. A thread whose last request
  passed `compact_at` tokens (default 110,000) has its older part summarized and its recent turns kept whole; the
  panel is told, with the size before and after. `max_tokens`, `max_steps` and `compact_at` come from the agent
  config. One host thread per conversation, in the core database; a restart resumes it. The run log keeps uncached
  input, cache reads and cache writes apart. A conversation moved over from `nanite` starts a fresh thread whose
  first turn says the earlier history is in the transcript. `nanite` is unchanged. See docs/agent.md.
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
