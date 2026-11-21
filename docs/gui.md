# The interface: how it is built and how to change it

This is the guide for anyone, person or agent, who builds or changes an interface on Surface. Read it before
you style anything.

The rule everything else follows from: **a look is changed in one place, the registry, and reaches every app
from there.** If you find yourself adjusting a button, a card or a colour inside the app you are working on,
stop: that change belongs in the registry or the theme, not in the app.

## The shape

Three layers. Nothing else is needed to build an app's interface.

| Layer | What it is | Where it lives | How an app gets it |
|---|---|---|---|
| Theme | One stylesheet of tokens: colours, radius, fonts. An app's look is its theme. | `registry/theme/surface.css` | Imported once, after Tailwind |
| Components | Stock shadcn components (style base-nova, on Base UI) and our own: cards, chat, the window manager, the app frame, ADE's overlays, generated surfaces | `registry/ui/` | Copied into the app's source by `scripts/ui.mjs`, never edited there |
| Behaviour | Code that decides and draws nothing: the chat stream and store, the card engine and outcome contract, the GenUI contract and controller | `packages/chat`, `packages/cards`, `packages/genui` | Imported as a package |

The sorting rule: **anything that draws goes through the registry; anything that only decides is a package with
no styles.** Registry components take props and callbacks. They do not import the behaviour packages, fetch, or
know about a server. An app joins the two in a small amount of glue code.

### What a page is made of

| Piece | Registry component | Behaviour | Glue in Surface |
|---|---|---|---|
| The frame (bar, rail, side panel, content) | `app-frame` | none | `web/src/desk/Desktop.tsx` |
| Windows that tile, tab and float | `window-manager` (on flexlayout-react, pinned) | the tree is the server's (`core/internal/desk`) | `web/src/desk/` |
| The agent's conversation | `conversation`, `message`, `markdown`, `prompt-input`, `tool` | `packages/chat` | `web/src/agent/` |
| Cards (notice, question, approval, progress) | `card-frame` and the `card-*` set | `packages/cards`; state is the server's (`core/internal/cards`) | `web/src/cards/` |
| Generated surfaces | `genui-surface` | `packages/genui`; the host is `core/internal/genui` | `web/src/genui/` |
| Pointing, spotlight, guides | `attention-layer`, `guide-bar` | `@hollis-labs/ade-core`; state in `core/internal/ade` | `web/src/ade/` |
| A module's view | whatever it composes from the registry | the module's own | `modules/<id>/web/` |

Every installed component, in every variant and state, is drawn from fixtures in the **Components window**
(`surface.gallery`, in the apps menu; source in `web/src/gallery/`). Design work happens against that window.

## Building a view

A view is registry components arranged with Tailwind utilities, in the theme's names. `registry/README.md` has
the table of which component to use for what, and the rules in full. In short:

- No raw `<button>`, `<input>`, `<select>` or `<textarea>`. Lint fails the build.
- Colours only as tokens (`bg-card`, `text-muted-foreground`, `border-border`). No hex, no `rgb()`, no `oklch()`.
- `className` on a component says where it sits (`w-64`, `flex-1`, `ml-auto`), never how it looks.
- A stylesheet of a view's own is only for what utilities cannot express (rendered Markdown, a diff), written
  with the theme's variables. It never styles a control.

## Changing how something looks

First decide which kind of change it is. The kind decides where the change goes.

| You want | It is a | Change it in |
|---|---|---|
| A different colour, radius, font, or dark/light value, everywhere | theme change | `registry/theme/surface.css` |
| One app to look different from the others | theme override | that app's own stylesheet, overriding the theme's variables under `:root` and `.dark` |
| A component to look different everywhere | component change | its file in `registry/ui/` |
| A component to have another look for some uses (denser, quieter, a warning tone) | new variant | its file in `registry/ui/` |
| Something no component does | new component | a new file in `registry/ui/` |
| The same components arranged differently in one view | layout | the view |

Only the last row is done in the app. Everything else is done in the registry and then synced.

### The steps for a component change, a new variant, or a new component

All of this is in the Surface repository, whichever app prompted it.

1. **Edit or add the file under `registry/ui/`.** One component family per file. Props only: no fetching, no
   imports from `packages/`, no app state. A new look is a variant (a `variant` or `size` value), not a
   `className` at the call site. A component that needs a stylesheet has it beside it with the same name
   (`window-manager.css`). A package it needs at one exact version is pinned in `registry/items.json`.
2. **Build the registry:** `node scripts/ui.mjs build` (or `make ui-build`). This regenerates
   `registry/registry.json` and `registry/r/*.json`. Commit them with the change.
3. **Sync Surface's own copy:** `node scripts/ui.mjs sync`. A new item is added with
   `node scripts/ui.mjs add <name>`.
4. **Add or update its specimen in the gallery** (`web/src/gallery/`): every variant and every state, from
   fixtures. A component that is not in the gallery cannot be designed against or checked.
5. **Look at it.** Open the Components window in dark and light, or run `make ui-shots` and read the images.
   When someone reviews the change live, serve the gallery from your checkout with `make design-preview`
   (below).
6. **Run the checks:** `make lint-web typecheck ui-verify` and `npm test`.
7. **Replace call-site workarounds.** If views were passing `className` to fake the look you just added, change
   them to the variant.
8. **Commit, with the generated files.** One change to the registry, one commit.

### Getting the change into the other apps

A registry change reaches an app when that app syncs.

- **Apps in this repository** share one installed copy (`web/src/components/ui`), so step 3 above is all of it.
- **An app in another repository** reaches it with a Surface release: the change is tagged (`vX.Y.Z`, one
  version for the registry, the npm packages and the Go module), and the app moves to that tag. In the app:
  set `ref` in `ui.lock.json` to the tag and the `@hollis-labs/surface-*` packages to the same version, run
  `npx surface-ui sync` and `npx surface-ui verify`, then commit. Setting up such an app is in
  `registry/README.md`, "An app in its own repository".
- **To see who is behind:** `node scripts/ui.mjs report <app dir...> --registry <this repo>/registry/r`.
- **A behaviour change** (`packages/*`, or the page core in `web/src`) reaches other apps the same way: in a
  release, as the npm packages at the tag's version.

### If you are working in another app and the registry lacks what you need

Do not edit the installed copy: `ui verify` fails the app's CI, on purpose. Do not restyle at the call site.
Instead:

1. Make the change in the Surface repository by the steps above (or ask Surface's owner to, with a
   description of the variant or component and where it is needed).
2. Sync your app.

There is no local-override path. If the change is urgent, the registry change is the urgent thing.

### Taking a newer upstream shadcn component

Replace the file in `registry/ui/` with upstream's (`npx shadcn add <name> -p nova -b base` into a scratch
directory, then copy), review the diff like any other change, and follow the same steps. Our own additions to a
stock component are kept by re-applying them in that review. One seat decides when to take upstream versions.

## Changing the theme

The theme is `registry/theme/surface.css`. Its variable names are stock shadcn's (`--background`,
`--foreground`, `--card`, `--primary`, `--muted`, `--accent`, `--destructive`, `--border`, `--input`, `--ring`,
`--radius`, the `--sidebar-*` and `--chart-*` sets) plus three of ours: `--success`, `--warning`, `--info`.
Each has a value under `:root` (light) and under `.dark`. Dark is the class `dark` on `<html>`.

- Changing a value there changes every app that uses the theme.
- An app that should look different overrides the same variables in its own stylesheet, after the import.
  It never restyles a component.
- A new token is added here, with both light and dark values and a `--color-*` line in the `@theme` block so
  Tailwind has a utility for it. Add a token only when no existing one means the same thing.
- The window manager's colours are mapped from these tokens in `registry/ui/window-manager.css`; a new token
  it should follow is mapped there.

## Adding behaviour

Logic that more than one app needs goes in a package under `packages/`, with tests that run under
`node --test` and no markup or styles. It is joined to registry components in the app's glue. Keep the
registry components free of it: a card component takes `state` and `onRespond`; it does not know there is an
engine or a server.

## The extension points

| To add | Do |
|---|---|
| A card kind of a module's own | Draw it with a new or existing registry component; register the renderer with `registerCard(kind, version, component)` (`web/src/cards/renderers.tsx`); show it from Go with `Host.Cards().Show`. An unregistered kind is drawn as a plain "no renderer" card, never as nothing |
| An action on referenceable objects (`<ObjectActionsMenu />`) | Register a callback hook with `registerObjectAction(actionDef)` from `@hollis-labs/surface-page/sdk` (optionally scoped by `when: (obj) => boolean`), and render `<ObjectActionsMenu object={{ kind, id, title }} />` on any object header, row, or card |
| A menu navigation group or item (up to 3 levels deep) | Call `registerNavGroup({ id, title, icon, parentId, order })` or `registerNavItem({ id, title, groupId, order, viewId })` from `@hollis-labs/surface-page/sdk`. Users can reorganize groups and items live in **Settings → Navigation & Menu** (`surface.nav_config`) |
| Per-module autotiling vs full-frame window behavior | Set `Tiling: "tile"` on `module.View` (with `Multi: true`) when multiple windows of the module should autotile side-by-side within that module (e.g., `terminal`); leave `Tiling` unset (`"full"`) so the module opens full-frame by default and replaces the active module on switch |
| A custom drawing for one of the agent's tools in the conversation | Add it to `toolCards` in `web/src/agent/items.tsx` |
| A generated-UI pattern | Not in an app. The three patterns are a reviewed contract owned by Substrate (`genui/`); a new one is a contract change there, then a catalog entry in `core/internal/genui` and a branch in `genui-surface` |
| Something the agent can point at inside a view | `useAnchor(win, name, label)` from `@hollis-labs/surface-page/sdk` |
| A region of the page | A prop of `app-frame`; the frame does layout only |

## Reviewing design work live

Design work is reviewed while it is being done, in the app of the person who asked for it.

- **The designer** serves the gallery from their checkout with hot reload:

  ```sh
  make design-preview HOST=<an address the reviewer can reach> PORT=<port>
  ```

  It prints the URL (`http://<host>:<port>/gallery.html`; `?theme=light|dark`, `&job=<job id>`, `#<section>`). The page is the
  Components gallery on its own, from fixtures, with no app and no server, and it may be framed by another
  origin. `HOST=0.0.0.0` listens on every interface. `make design-gallery OUT=<dir>` builds the same page to
  static files.
- **The draft on its own.** The designer marks the sections that show the job's work with the job's id:
  `<Section title="…" draft="d_1234abcd">`. With `job=<id>` in the URL the gallery adds a **Draft** tab, first
  and open, that shows only those sections, wherever they live. The marks are inert elsewhere, so they can stay
  after the merge.
- **The reviewer** sees it in a Design job's window (`modules/design`): the preview fills the window and opens
  on the Draft tab; the bar has the branch/base switch, dark and light, the section picker, Send notes and
  Approve. **Details** opens the rest in a dialog: the notes for the next round (pinned to a section and
  component), the changed files, the rounds, the Designer's replies and the preview URLs. An edit in the
  designer's checkout shows in the window without a refresh.
- **The rounds** go through the agent: `design_request` hands the job to the Design team (a Tether message and a
  manual Torque task), `design_feedback` sends the open notes as the next round, `design_approve` asks the
  owner with an approval card and, on approve, tells the team to merge. The team merges; Surface does not.
  Progress is read from the branch and its pull request. Where messages go is the app's settings
  (`design.*`), never the repository; without them the text is shown to send by hand.

## What is checked

| Check | Catches | Run by |
|---|---|---|
| `ui verify` | An installed component edited in place, behind the registry, missing, or a stray file beside them | CI |
| `git diff --exit-code -- registry` after `ui build` | Registry sources changed without the generated files | CI |
| Lint (`noRestrictedElements`) | A raw control in a view | CI |
| `tsc`, `npm test` | Broken props, broken behaviour | CI |
| `make ui-shots` | Nothing yet by itself: it produces the images a comparison will use | by hand |
| `ui.mjs report` | Apps behind or diverged | by hand |

Registry versions are Surface tags. Screenshot comparison in CI and automatic sync pull requests are planned and not
built.

## Taste, theme and interaction standards

The baseline visual and interaction standard is documented in **[`docs/taste.md`](taste.md)** (*Surface Taste & Design System*):

1. **Evolved Blade Runner Theme (`registry/theme/surface.css`):**
   - Dark recessed slate surfaces (`--desk: #06090b`, `--background: #0c1215`), warm amber primary (`--primary: #e5a463`), cyan secondary accent (`--chart-2: #7ecfd4`), magenta decision accent (`--chart-3: #c984b2`), and semantic status tokens (`--success`, `--warning`, `--destructive`).
   - Tight, deliberate radii: `6px` (`--radius-lg`) for windows/cards, `5px` (`--radius-md`) for buttons/inputs/composer, and `4px` (`--radius-sm`) for chips, badges, kbd, and icon buttons.
2. **Spatial & Interaction Conventions:**
   - **Full-frame module switching by default**, with autotiling scoped within modules that declare `Tiling: "tile"` (e.g., `terminal`).
   - **Contained in-module navigation** when clicking items inside a module (modifier-click or pop-out icon opens a new window).
   - **Slide-over `<CiteDrawer />`** opened via the `"View"` action on Cite hover cards (auto-closing the hover card, with boot-time `initCite()` prefetching to prevent layout shift).
   - **Reusable `<ObjectActionsMenu />`** on every referenceable object.
   - **Workflow Golden Samples (`surface.workflows`)** demonstrating "Show, Don't Tell" architecture exploration, guided repo/prior-art review sessions, and staged fleet operations pipelines.
