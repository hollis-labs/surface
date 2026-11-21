# Operations

The owner's workstream board (`modules/ops`, in Desk). A workstream is a **concept of any size**: one task,
a feature across two repos, or a mesh-wide change such as auth, identity and secrets with smaller
workstreams under it. Each is one revisioned Caster document under the contract `ops/v1`; the module's
database is only a working copy. Torque CW-20261120-0117; the design is the Operations module design
(Tesseract workspace `project/surface/workspace/operations`, key `operations_module_design_2026_11_20`).

Operations never launches anything. Its Tether link is its own and allowlisted (`ops.go`, `allowed`): no
session, team, dispatch, claim or start tool is on it, and a test fails if one is added.

## Data: `ops/v1`

| Caster | Holds |
|---|---|
| `caster://ops/ws.<id>` | One workstream: YAML front matter and a Markdown body. Every write is a revision, so Caster's log is its history. |
| `caster://ops/queue` | `ops: v1` and `order: [<id>, …]`: the Queued band's launch order. One document, so a reorder is one write. |
| `caster://ops/followups` | Markdown: follow-ups a seed could not place on a workstream, for the owner. |

A workstream:

```yaml
---
ops: v1
id: registry-source-of-truth   # the Ops id: 3-64 of [a-z0-9-], not ending in a dash, never re-keyed; text form ops:<id>
title: Registry as source of truth for workstreams and correlation
owner: owner                   # a name or an agent urn
project: PRJ-20990101-0001     # optional Torque project
status_ns: project/registry/event/status   # optional: where its status entries are read
parent: auth-identity-secrets  # optional: the workstream it sits inside
stage: in_flight               # planning | planned | queued | in_flight | in_review | adoption | active | dropped
stage_since: 2026-11-20T13:37:12Z
held: {reason: "awaiting a review", since: ...}   # optional: a flag in any lane, not a stage
active_when:                   # what "active" means for this one
  - {text: "Torque dispatch ensures workstream ids", done: false, ref: CW-20990101-0002}
links:                         # each kind is a Cite kind, so each link is a chip
  - {kind: work.item, id: garden/ab12cd34, role: origin}
  - {kind: torque.epic, id: EP-20990101-0004, role: scope}
  - {kind: torque.task, id: CW-20990101-0090, role: task, label: "Registry ensure"}
  - {kind: registry.record, id: "workstream:01J…", role: identity}
correlation:
  labels: [registry-source-of-truth]   # free-text unit_plan.workstream_id values it answers to
transitions:                   # the last 50 moves; Caster's log is the whole record
  - {from: queued, to: in_flight, at: ..., by: owner, why: "launched"}
---
## Purpose
## Active means
## Notes
```

Keys and headings Ops does not know are kept as they are when it writes.

**Writes.** Every write reads the document again and applies the change to it. Caster has no
expected-revision put ("write only if still at revision N"), so a write is checked by the revision it answers:
one further on than expected means someone wrote in between and this write now sits over theirs. Their
revision is read at once, the change applied to it again and written once more (up to twice), so the window in
which the latest revision lacks their change is one read and one put long. What a write reports always matches
what Caster holds:

- success: the change is the latest revision, built on everyone else's;
- refused ("id taken", or 409 for an update that replaces what its caller read, or when others kept writing
  through every retry): the change does not stand. The latest foreign revision seen is put back as the latest,
  and that restore is checked the same way: when someone wrote after this change it is left as they wrote it,
  and when a write lands on the restore itself, theirs is put back on top. The 409 names the revision now
  latest.

A race can still lose a write: a writer who builds on this write's revision before it is re-applied builds
without the change it overwrote. Caster's history keeps every revision, so nothing is gone from it, and a
conditional put in Caster would close the window (a follow-up below). A reorder is worked out on the queue as it
is at the write. `ops_update` needs `base_rev` for what replaces something read (notes, purpose, active_means,
active_when, labels, tick). Link kinds kept: `ops.workstream`,
`work.item`, `torque.task|epic|plan|project|sprint`, `registry.record` (`<kind>:<id>`, the chip
`registry:<kind>:<id>`), `caster.doc`, `tesseract.item|key`, `tether.agent|message`, `url`, and
`tether.team|session`, which show as text until they have a resolver.

**Ids.** The Ops id is 1:1 with the Registry workstream key: Torque dispatch's `workstream_ensure(key=<ops id>)`
mints the `workstream:<ULID>`, and Ops keeps it as the `identity` link. Ops never mints a Registry id.

## Lifecycle

`planning → planned → queued → in_flight → in_review → adoption → active`, plus `dropped`. Any move is
allowed, explicit and recorded (`transitions`, and a Caster revision). Moving into Queued appends the id to
`ops/queue`; moving out removes it. The stage decides membership and the queue document decides order, so a
queue that lags a move (two writes) still shows every queued workstream, the unnamed ones last by
`stage_since`. The order is advisory: it does not drive dispatch.

Moves are the owner's. When a workstream's tasks point elsewhere (a task in progress while it is Planned or
Queued; nothing in progress and the rest done or in review while it is In Flight), Ops shows a suggested
stage with a button. It never moves anything by itself.

## Nesting

A workstream has an optional `parent`; its children are worked out from the copy. Only a workstream that
cannot sit where its document says goes to the top level, with a problem saying why: one whose own parent is not
in Ops, one on a loop of parents (lifting one breaks the loop), and the topmost of a chain deeper than allowed.
Everything under it stays under it, so nothing drops off the board. This mirrors Tether's
workstreams, which carry a parent as `;parent=ws:<id>` inside `workflow_id` rather than as a field. Rules: the
parent exists, no loops, and nothing deeper than 6 below the top, counting what is inside the workstream
being moved. `parent: ""` makes one top level.

- The board shows one level: the top concepts by default. "N inside" steps into one (a breadcrumb leads
  back), and the window's args say the level, so the agent knows it.
- The Queued band orders siblings: `ops/queue` is one global order, and a reorder moves one workstream before
  or after a queued sibling (same parent), or one place up or down among them.
- Tasks and activity roll up: a workstream's roll-up reads its own links and every descendant's.

## Activity

Read when a drill-in opens, on its refresh button, and every minute for in-flight workstreams while an
Operations window is open (`ops.activity`); cached per workstream in the module database for a minute. One
feed, newest first, each row `{at, source, state, summary, task, workstream, verified}`:

| Source | Read |
|---|---|
| Torque (`torque`) | `torque_task_get` of each linked task and of the children of linked epics (`torque_task_list epic_id`) and plans (`torque_plan_list_children`) |
| Status comments (`comment`) | `torque_comment_list` per task; lines starting `STATUS`, `REVIEW`, `VERDICT`, `INCIDENT` or `Independent review` |
| Status stream (`status`) | `event_list` on the workstream's `status_ns`, filtered to its tasks. Shown as claimed: no writer is trusted yet |
| Ops (`ops`) | its own transitions |

Bounded: 80 tasks and 6 calls at once per roll-up, 20 comments per task, 300 rows kept. A task's read is
kept for a minute and shared by every roll-up (a task under a concept and under its child is read once); a
finished task's comments are not read. A `STATUS` comment that mirrors a status-stream entry for the same task
is shown once, as the status entry. Rows sort by their parsed time.

Linked `torque.plan` entries expand into ordered phases and tasks in the Tasks tab. Plan and phase
states come from `torque_plan_get`; child task states and STATUS comments use the same task cache and feed
as direct and epic links. A task linked several ways counts once. Plans and their children are cached for
a minute, including across parent/child workstream roll-ups. Child pages are followed up to the 80-task
bound; incomplete reads are shown as problems, not as an empty plan. At most 80 distinct plans are expanded
per roll-up. `ops_get` includes plans, and the focused workstream's slot context includes cached plans and task counts.

## Seeding from the tracker

The owner's workstream tracker (kept by the Local Orchestrator: threads with stage, phase, owner, summary,
phase note and `[Torque id, title, state]` tasks, plus loose follow-ups) is read as data and mapped, never
copied. Always a preview first: `POST /seed/preview` (or the agent's `ops_seed`, which takes the JSON only)
keeps a plan in the module database and opens it; the owner edits it in the window and applies it there.
Nothing is written before that, and the agent cannot apply one. A path on this machine is read only from the
owner's page: an absolute path to a regular `.json` file (not a link), at most 4 MB, with one answer for every
way it can fail. Apply claims the kept preview atomically (it applies once) and applies the preview as kept,
never a plan sent with the request; an apply that wrote nothing gives the claim back.

| Tracker | Ops |
|---|---|
| `stage: flight` | In Flight |
| `stage: queued` | Queued (end of the band, in tracker order) |
| `stage: review` | In Review |
| `stage: landed` | **Adoption**, not Active: landed means merged or deployed, and Active means the workstream's own active-when list is met, which the tracker does not attest. The owner ticks it and moves it on. |
| `name` | title, and the id (a slug of it, editable in the preview; a name with too few plain letters gets a made-up id, flagged) |
| `tasks` | `torque.task` links (role `task`, the title as label; a run `CW-…-0097..0105` is expanded). The tracker's state is not copied: Torque is read live. |
| `summary`, `phase_note`, `phase_label` | notes |
| `owner` | owner (flagged when missing) |
| `followups` | onto a workstream's notes when they name one of its tasks, or two of its distinctive name words (also a workstream Ops had already); the rest, and any whose workstream was not made, to `caster://ops/followups` |

Proposed concepts (`conceptRules` in `seed.go`), each only when it gathers two threads, all **off** until the
owner ticks them: Auth, identity and secrets (Warden, Gatecrasher, identity and service tokens, the approval
flow); Observability and status; MCP gateway and the Ops MCP; Agent runtime and chat; Fleet and placement. A
ticked concept is made as a workstream and its threads nest under it; an unticked one leaves them at the top.
An id Ops already has is skipped, never overwritten. A preview is applied once.

`testdata/tracker.json` is a synthetic tracker in the same shape (invented threads, people and ids), for the
tests.

## Routes (`/api/m/ops/`)

| Route | |
|---|---|
| `GET /board` (`?force=1`) | Every workstream with its cached roll-up line, the queue, the owner's follow-ups |
| `POST /workstreams` | Create |
| `GET /workstreams/{id}` | One, with its children and the path above it |
| `PUT /workstreams/{id}` | Update (`base_rev`; 409 when it moved on) |
| `POST /workstreams/{id}/move` | `{stage, why}` |
| `POST /workstreams/{id}/links`, `DELETE /workstreams/{id}/links?kind=&ref=` | Link, unlink |
| `GET /workstreams/{id}/activity` (`?refresh=1`) | The roll-up and feed |
| `GET /workstreams/{id}/history`, `GET /workstreams/{id}/at/{rev}` | Transitions and Caster revisions; one revision's text |
| `POST /queue/reorder` | `{id, before | after | dir}` |
| `POST /seed/preview`, `GET|PUT /seed/{plan}`, `POST /seed/{plan}/apply` | The seed (apply takes no body: the kept preview) |

Routes that change something (and a seed preview from a path) are the owner's own actions, from the page,
and ask nothing; anyone else (an outside client, the agent) gets 403 and uses the `ops_*` tools, which ask the
owner. Writes are recorded as the actor the core names. Every write publishes `ops.changed` `{id?, what}`.

## Agent tools and slot

| Tool | Level | |
|---|---|---|
| `ops_list` | read | The board by lane at one level (`parent`, or `all`), the queue, filters `stage`, `owner`, `held` |
| `ops_get` | read | One workstream, its roll-up and last 10 activity rows |
| `ops_activity` | read | The merged feed, filtered by `since`, `source`, `task` |
| `ops_create`, `ops_update`, `ops_move`, `ops_queue_reorder`, `ops_link`, `ops_unlink` | write | Asked of the owner each time (an approval can stand) |
| `ops_seed` | act | A seed preview from the tracker's JSON, opened for the owner; writes nothing |

The `ops` slot (views `ops`, `ops.workstream`, `ops.seed`) gives lane counts, the queue head, what is in
flight with held reasons, task counts and the age of its last activity, and the workstream in front, all from
the working copy. Cite: `ops:<id>` is `ops.workstream`, resolved from the copy only; opening it runs `ops.open`.

Settings: `ops.tether_command`, or `ops.tether_url` with `ops.tether_token_file`.

## Not yet

- **Status stream**: read per `status_ns` only (no cross-project read), and every row is "claimed" until
  trusted writers are configured. It is near-empty today, so Torque is the main source.
- **Registry identity**: the `identity` link is added by hand; there is no lookup by key until
  `workstream_ensure` is deployed, and no correlation records as activity.
- **Teams**: no `tether.team` Cite kind; team links show as text. No grouping of activity by team, no
  StateTimeline.
- `ops_request_agent`, `ops_launch_request`, `ops_drop` (phase 1.5), and the Planner → Task Ops hook.
- **A conditional put in Caster** (`caster_put` with an expected revision, refused when the item moved on).
  Until Caster has one, a race between writers can still lose a write (see Writes); with it, `putOver` becomes
  a plain compare-and-swap loop.
