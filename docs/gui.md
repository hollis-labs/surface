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
6. **Run the checks:** `make lint-web typecheck ui-verify` and `npm test`.
7. **Replace call-site workarounds.** If views were passing `className` to fake the look you just added, change
   them to the variant.
8. **Commit, with the generated files.** One change to the registry, one commit.

### Getting the change into the other apps

A registry change reaches an app when that app syncs.

- **Apps in this repository** share one installed copy (`web/src/components/ui`), so step 3 above is all of it.
- **An app in another repository** has a `ui.lock.json` naming this registry at a ref. In that app:
  `node scripts/ui.mjs sync`, then `node scripts/ui.mjs verify`, then commit. If the lock pins a tag, move the
  `ref` to the new tag first.
- **To see who is behind:** `node scripts/ui.mjs report <app dir...> --registry <this repo>/registry/r`.

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
| A custom drawing for one of the agent's tools in the conversation | Add it to `toolCards` in `web/src/agent/items.tsx` |
| A generated-UI pattern | Not in an app. The three patterns are a reviewed contract owned by Substrate (`genui/`); a new one is a contract change there, then a catalog entry in `core/internal/genui` and a branch in `genui-surface` |
| Something the agent can point at inside a view | `useAnchor(win, name, label)` from `@hollis-labs/surface-page/sdk` |
| A region of the page | A prop of `app-frame`; the frame does layout only |

## What is checked

| Check | Catches | Run by |
|---|---|---|
| `ui verify` | An installed component edited in place, behind the registry, missing, or a stray file beside them | CI |
| `git diff --exit-code -- registry` after `ui build` | Registry sources changed without the generated files | CI |
| Lint (`noRestrictedElements`) | A raw control in a view | CI |
| `tsc`, `npm test` | Broken props, broken behaviour | CI |
| `make ui-shots` | Nothing yet by itself: it produces the images a comparison will use | by hand |
| `ui.mjs report` | Apps behind or diverged | by hand |

Screenshot comparison in CI, registry versions by tag, and automatic sync pull requests are planned and not
built.

## Two design decisions that are open

Both are the owner's. Nothing in the wiring waits on them; they are what the design pass decides.

### 1. The palette and the baseline theme

**Now:** the theme holds shadcn's neutral defaults (greys, base-nova), in light and dark, and the system font.
It was left that way deliberately so the plumbing could be finished first.

**To decide:** the portfolio's own baseline: the values of every colour token in light and dark (including
`--success`, `--warning`, `--info`), `--radius`, the sans and mono fonts, and how dense the default sizes are.
Whether apps share one baseline and override a few tokens each, or each app carries a full theme of its own.

**Where it lands:** `registry/theme/surface.css`, and nowhere else. No component changes for a palette. Per-app
differences are overrides in the app's stylesheet. Check it in the Components window in both modes; contrast of
`--muted-foreground` on `--card` and `--background`, and of `--ring` as the focus outline, are the two to look at
first.

### 2. Layouts, components, cards and flows

**Now:** every component works and is unstyled beyond shadcn's defaults. Views use them with default
variants. Cards and generated surfaces share one frame (`card-frame`).

**To decide:** how each component and card should look and behave in detail, and the arrangements an app
opens with. Known gaps and rough edges found while wiring, each of which is a registry change:

- A selected state for list rows (`item`); truncation for a row's title and description.
- A dense size for `table`; a small size for field labels.
- A warning banner (views use a styled `div` today).
- The window manager's button tooltips are the library's wording ("Maximize tabset"); a floating window has a
  drag strip above its title bar; the bar's tile/float button does not reflect a mixed arrangement.
- The agent's tool-permission prompt is a plain bar, not a card.
- Where pop-up cards sit, and how a card that was put off ("Later") is found again.
- The look of each card kind and each state (the gallery's Cards section lists them all).

**Where it lands:** variants and markup in `registry/ui/`, by the steps above, each with its gallery specimen.
Arrangements are saved layouts and flows (the Layouts window, the Builder), not code. A view's own
composition is the only part done in the app.
