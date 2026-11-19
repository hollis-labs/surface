# Work

The Work engine under Planner, Content and Writer: workspaces of typed items, links, stages and saved
views, kept in Caster under the contract `work/v1`. Ported from Basalisk (`internal/work`,
`internal/api/work.go`, `frontend/src/work/agent.ts`). The data in Caster is the same, so Basalisk and
Surface read and write the same workspaces.

## Data

| Caster | Holds |
|---|---|
| `caster://work/<workspace>` | The workspace's index: `work: v1`, `workspace`, `title`, `app` (`content`, `planner`, `writer`), `preset`, `ns`, `owners`, `created`; the body says what it is. Listing `work` lists the workspaces. |
| `caster://w-<workspace>/kind.<kind>` | A kind (YAML): `label`, `icon`, `stages`, `fields`, `sections`, `children`, `color`. Copied from a preset when the workspace is made. |
| `caster://w-<workspace>/i.<id>` | An item: front matter (`kind`, `title`, `stage`, `fields`, `parent`, `children`, `links`, `material`, `published`, `task`, `tags`, `cut`, `signed`) and a Markdown body under the kind's section headings. Unknown keys and headings are kept on write. |
| `caster://w-<workspace>/v.<id>` | A saved view (YAML): `view` (`board`, `calendar`, `outline`, `canvas`, `flow`), `title`, `kinds`, `filter`, `group_by`, `date_field`, `root`, `rel`, `layout`. |
| `caster://w-<workspace>/r.<id>.<n>` | Round `n` of feedback on an item. |

The module's store (`migrations/0001_init.sql`) is a working copy of those items, refreshed from Caster
at most every 15 s per namespace when a workspace is listed or opened (`?refresh=1` forces it), and the
owner's draft notes not yet sent. Every write goes to Caster at once, after reading the item again; an
edit made on an older revision is never written over the newer one: its changed sections become draft
suggestions and the call answers 409.

Caster, Torque and Tether mail are reached through Tether's tools (`module/tetherlink`), with the
settings `work.tether_command`, or `work.tether_url` with `work.tether_token_file`. Long listings the
gateway cuts short are read whole (`tetherlink.CallAll`).

After any write the module publishes `work.changed` with `{workspace, id?, what}` (`what`:
`workspace`, `item`, `view`, `note`, `round`).

## Agent tools

| Tool | Level | |
|---|---|---|
| `work_list` | read | Workspaces (`app` narrows), or one workspace's kinds, views and items (`kind`, `stage`, `include_cut`). |
| `work_get` | read | One item: fields, links, text by section, `rev`, draft notes, last round, sign-off. |
| `work_history` | read | An item's revisions (`name`), or one revision (`rev`). |
| `work_note` | write | A draft comment or suggestion; the owner sends the round. |
| `work_space_create`, `work_space_update` | write | Make a workspace from a preset; rename or archive one. |
| `work_item_create`, `work_item_update`, `work_item_stage`, `work_item_place`, `work_item_restore` | write | Make, edit (with `base_rev`), stage (never `published`), place in the outline, restore. |
| `work_note_update`, `work_view_save` | write | Edit a draft note; make or change a saved view. |
| `work_item_delete`, `work_space_delete`, `work_note_delete` | confirm | Cut an item, take a workspace off the list, delete a draft note. All recoverable in Caster except the draft note. |

The agent never sends a round, signs off or publishes: those are the owner's, through the routes below.

## HTTP

Basalisk's `/api/work/...` routes, under the module's prefix: `/api/m/work/work/...`. A view's client
calls the same relative paths Basalisk's `frontend/src/api/work.ts` did (`api.get("work/<ws>")`), and
gets the same JSON. Errors are `{code, message, error}` with the status shown.

| Route | Body | Answer |
|---|---|---|
| `GET work?app=&refresh=1` | | `{workspaces: Workspace[], presets: Preset[], problem?}` |
| `POST work` | `{workspace, title, preset, about, template?}` | 201 `{workspace, problem?}` |
| `GET work/{ws}?refresh=1` | | `{space: {workspace, kinds, items: Card[], views, default_view}, relations, problem?}`; 404 |
| `PUT work/{ws}` | `{title?, archived?}` | `{updated: true}` |
| `DELETE work/{ws}` | | `{deleted: true}` (kept in Caster, off the list) |
| `PUT work/{ws}/owners` | `{owners: ["msg://..."]}` | `{ok: true}` |
| `GET work/{ws}/log?name=i.<id>` | | `{revisions: [{rev, at, title}]}` |
| `GET work/{ws}/at?name=&rev=` | | `{name, rev, content, title?, stage?, sections?}` |
| `POST work/{ws}/items` | `{kind, title?, stage?, parent?, fields?, tags?, links?, sections?}` | 201 `{id}` |
| `GET work/{ws}/items/{id}` | | `Detail`: the card plus `ref, sections, order, lead?, notes, rounds, sign_off?, changed_since_round, owners` |
| `PUT work/{ws}/items/{id}` | `{base_rev, title?, fields?, sections?, tags?, stage?, material?, published?, links?}` | `{rev}`; 409 `{conflict: true, rev, suggestions}` |
| `PUT work/{ws}/items/{id}/move` | `{key: "stage" or a field, value}` | `{rev}` |
| `PUT work/{ws}/items/{id}/place` | `{parent, before, view}` | `{ok: true}` |
| `POST work/{ws}/items/{id}/cut`, `…/restore` | | `{ok: true}` |
| `POST work/{ws}/items/{id}/torque` | | `{task, created}` (one Torque task per item) |
| `GET work/{ws}/items/{id}/torque` | | `{task: {id, title, status} or null, problem?}` |
| `POST work/{ws}/items/{id}/notes` | `{kind, section?, quote?, replace?, text?}` | the note |
| `PUT work/{ws}/notes/{id}` | `{text?, replace?}` | `{ok: true}` |
| `DELETE work/{ws}/notes/{id}` | | `{ok: true}` |
| `POST work/{ws}/items/{id}/round` | | `{round, ref, notes, mailed, not_mailed, commented?}` |
| `POST work/{ws}/items/{id}/signoff` | `{anyway?}` | `{at, rev, digest, pinned?, round?, stage?, mailed, not_mailed, problem?}`; 409 `{open, round}` |
| `DELETE work/{ws}/items/{id}/signoff` | | `{ok: true}` |
| `POST work/{ws}/views` | view keys | `{id, rev}` |
| `PUT work/{ws}/views/{id}` | view keys (null takes one out) | `{id, rev}` |
| `PUT work/{ws}/views/{id}/layout` | `{cards?, stacks?, notes?}` by id (null takes one out) | `{rev}` |

The shapes (`Workspace`, `Card`, `Detail`, `Kind`, `View`, `Preset`, the layout) are the Go types in
`store.go`, `contract.go`, `presets.go` and `layout.go`; their JSON is Basalisk's.

## Views

Three windows, one component (`work/main`, `web/index.tsx`), each opened on its preset's app: `work`
(Planner), `work.content` (Content) and `work.writer` (Writer), in the Apps menu. Ported from Basalisk's
`frontend/src/work`.

- The app's workspaces, and New workspace (from a preset, and a template where the preset has them).
- A workspace opened on its saved views, switched in the header, and a view added from it: **board**
  (columns by stage, kind or an enum field; drag a card to another column), **outline** (the children
  tree; drag before, after or into a row), **calendar** (month or week by a date field; drag to a day or
  to No date), **canvas** (cards, stacks bound to a stage or a parent, free notes; pan and zoom) and
  **flow** (links as edges; drag from a node's dot to link, Delete to unlink; lanes; tidy).
- An item's **detail** beside the view: title, stage, fields by type, sections edited in place (a newer
  revision becomes suggestions, answered 409), tags, Send to Torque and its status, links both ways,
  material by reference, the publication log, the review (draft notes, Send round, rounds and answers,
  sign-off) and the revisions with a word diff.

What is in front is kept in the window's args (`workspace`, `view`, `item`) and reported to the desk, so
`Describe` and the `work` slot see `work:<workspace>` or `work:<workspace>/<id>`. The views refresh on
`work.changed`; they do not poll.

| Command | Args | |
|---|---|---|
| `work.open`, `work.content`, `work.writer` | `workspace?`, `item?` | Open Planner, Content or Writer: on its list, or on a workspace (a window of its own, keyed by the slug) and an item in it |

Not ported from Basalisk: picking material from the Fragments Engine library (paste the reference
instead), Cite chips for references (shown as text or a link), and the detail's 30 s look for a team's
answer to a round (it is read again on `work.changed` and when reopened). The flow draws with a small
renderer of its own: a module cannot import the Diagram module's.
