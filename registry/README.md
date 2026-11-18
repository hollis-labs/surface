# The @surface registry

The one source of interface components for apps built on Surface. It is a
[shadcn registry](https://ui.shadcn.com/docs/registry): components are installed into an app's own source,
where its build sees them and an agent can read them.

```
registry/
  ui/*.tsx            the components (stock shadcn, style base-nova on Base UI, as of shadcn 4.23.0)
  theme/surface.css   the theme: every colour, radius and font a component may name
  registry.json       generated from ui/ by `node scripts/ui.mjs build`
  r/*.json            generated: one file per item, what installers read
```

## The rules

1. **Anything that draws comes from here.** A view is composed from these components. It does not contain a
   raw `<button>`, `<input>`, `<select>` or `<textarea>`, and it does not restyle a component beyond layout
   (width, margin, flex). Lint enforces this.
2. **Copied components are not edited in the app.** `ui verify` fails when an installed file differs from the
   registry. To change a component, or add a variant, change it here and sync.
3. **An app's look is its theme.** Override the theme's variables in the app's stylesheet; never a component.

## Building a view

A view is components from `@/components/ui/*` arranged with Tailwind utilities. That is all of it.

| For | Use |
|---|---|
| Any action | `Button`: `variant` default (the one primary action), `outline`, `secondary`, `ghost` (toolbars, icon buttons), `destructive`, `link`; `size` `xs`, `sm`, default, `lg`, and `icon`, `icon-sm`, `icon-xs` for a lone icon |
| Text entry | `Input`, `Textarea`; `InputGroup` for an input with an icon or button inside; `TextEditor` for a document's source |
| A choice | `Select`, `Checkbox`, `Switch`, `ToggleGroup`, `Tabs` |
| A label and its control | `Field` (with `FieldLabel`, `FieldDescription`, `FieldError`) or `Label` |
| Asking or showing over the page | `AlertDialog` (confirm), `Dialog`, `Sheet`, `Popover`, `Tooltip`, `DropdownMenu`, `ContextMenu`, `Command` (a searchable list) |
| A row in a list | `Item` (with `ItemMedia`, `ItemContent`, `ItemTitle`, `ItemDescription`, `ItemActions`); `Table` for columns |
| Status and small things | `Badge`, `Kbd`, `Spinner`, `Skeleton`, `Progress`, `Separator`, `Empty` (nothing here yet) |
| Regions | `Card`, `ScrollArea`, `ResizablePanelGroup`, `Collapsible`, `Breadcrumb` |

Layout and surfaces are Tailwind utilities on plain elements, in the theme's names:

- Colours only as tokens: `bg-background`, `bg-card`, `bg-muted`, `bg-accent`, `text-foreground`,
  `text-muted-foreground`, `text-destructive`, `text-success`, `text-warning`, `border-border`, `ring-ring`.
  No hex, no `rgb()`, no `oklch()`, no `text-[#…]`, no `dark:` pairs of literal colours.
- Sizes from the scale: `text-xs`/`text-sm`/`text-base`, `gap-2`, `p-3`, `rounded-md`. An arbitrary value
  (`w-[230px]`) only for a real layout dimension that the scale lacks.
- `className` on a component is for where it sits (`w-64`, `flex-1`, `ml-auto`, `shrink-0`), never for how it
  looks. If a component needs to look different, that is a variant: add it in `registry/ui` and sync.
- Icons are `lucide-react`.

A stylesheet of a view's own is for what utilities cannot say (rendered Markdown, a diff, a canvas), written
with the theme's variables (`var(--foreground)`, `var(--border)`). It never styles a control.

## Using it in an app

`ui.lock.json` at the app's root names the registry and where components go. In this repository:

```json
{ "registry": "./registry/r", "uiDir": "web/src/components/ui", "items": {} }
```

```sh
node scripts/ui.mjs add button dialog    # install items and what they depend on
node scripts/ui.mjs sync                 # reinstall everything locked, from the registry
node scripts/ui.mjs verify               # CI: fail on an edited, stale, missing or stray file
node scripts/ui.mjs status
```

Surface's page imports the theme from `registry/theme/surface.css` directly (`web/src/index.css`).

### An app in its own repository

Everything comes at one version, the Surface tag (`v0.1.0`, ...): the registry is read from GitHub at that
tag, and the npm packages carry the same version. Moving an app to a new Surface is moving `ref` and the
package versions together.

| What | Package | Is |
|---|---|---|
| The `ui` tool | `@hollis-labs/surface-ui` (bin `surface-ui`) | this repository's `scripts/ui.mjs` |
| The page core | `@hollis-labs/surface-page` (`/sdk` for a module's views, `/gallery`, `/vite`) | `web/src` |
| Behaviour | `@hollis-labs/surface-chat`, `-cards`, `-genui` | `packages/*` |

A Vite + React 19 + Tailwind v4 app:

```sh
npm install @hollis-labs/surface-page react react-dom lucide-react
npm install -D @hollis-labs/surface-ui vite tailwindcss @tailwindcss/vite
npx surface-ui init --ref v0.1.0                    # ui.lock.json: GitHub at the tag, components in src/components/ui
npx surface-ui add theme --for @hollis-labs/surface-page   # the theme, and every item the page draws with
npx surface-ui add card-choice                      # anything else the app's own views use
```

`add` ends by listing the npm packages the installed components import that `package.json` lacks; install
them (`flexlayout-react`, `react-markdown` and `remark-gfm` at the exact versions it gives). The lock it writes:

```json
{
  "registry": "https://raw.githubusercontent.com/hollis-labs/surface/{ref}/registry/r/{name}.json",
  "ref": "v0.1.0",
  "uiDir": "src/components/ui",
  "items": {}
}
```

What the app sets up itself, all of it once:

- **The `@/` alias** to its source directory, in `vite.config.ts` and in `tsconfig.json` (`"paths": {"@/*":
  ["./src/*"]}`). Components import each other as `@/components/ui/<name>`, and so does the page package:
  it draws with the app's own installed copies, not copies of its own.
- **The Vite plugin**, which keeps the page package out of Vite's pre-bundling (so those imports reach the
  app's components) and pre-bundles what it imports instead:

  ```ts
  import { surfacePage } from "@hollis-labs/surface-page/vite"
  export default defineConfig({
    plugins: [tailwindcss(), surfacePage()],
    resolve: { alias: { "@": resolve(import.meta.dirname, "src") } },
    server: { proxy: { "/api": "http://127.0.0.1:<app port>", "/mcp": "http://127.0.0.1:<app port>" } },
  })
  ```

- **The stylesheet**: Tailwind, the installed theme, and a `@source` for the page package's classes
  (Tailwind does not look inside `node_modules`):

  ```css
  @import "tailwindcss";
  @import "./components/surface.css";
  @source "../node_modules/@hollis-labs/surface-page/dist";
  html, body, #root { height: 100%; margin: 0; }
  ```

- **The entry**: `mountPage({ modules })` from `@hollis-labs/surface-page`, with `modules` the views of the
  app's modules by module id. A module's view imports only `@hollis-labs/surface-page/sdk`. The Components
  window is optional: `mountPage({ modules, core: galleryViews })` with `galleryViews` from
  `@hollis-labs/surface-page/gallery`, which needs every registry item installed.

The page talks to the app's server (`/api`, `/mcp`), a Go binary built on `github.com/hollis-labs/surface`
at the same tag. In CI the app runs `npx surface-ui verify`.

The theme item is installed beside the components directory (`src/components/surface.css`), or into the
lock's `themeDir`. The stock `shadcn` CLI and its MCP server work against the same registry
(`components.json` lists it as `@surface`): `npx shadcn add @surface/button --diff` shows how an installed
file differs.

## Seeing it and checking it

- **The gallery.** Surface's Components window (`surface.gallery`, in the apps menu) draws every installed
  component in its variants and states from fixtures: controls, display, overlays, chat, cards, generated surfaces,
  ADE's overlays and the window manager. Design work happens against it. Its source is `web/src/gallery/`; add a specimen there when you
  add a component or a variant.
- **Screenshots.** `make ui-shots` builds studio, opens the gallery and saves one PNG per section in dark and light
  (`OUT=<dir>`; default `$TMPDIR/surface-ui-shots`), with an `index.json`. Nothing compares them yet.
- **Drift across apps.** `node scripts/ui.mjs report <app dir...> [--from <file>] [--registry <dir or URL>] [--json]`
  prints each app's registry, item count, and every item missing, edited in place, behind the registry or unknown
  to it, and any file in the components directory the registry did not put there.

## Changing the registry

Edit or add a file under `ui/`, then `node scripts/ui.mjs build` and commit the generated files with it.
Dependencies are read from each file's imports: a package import becomes an npm dependency, an import of
`@/components/ui/<name>` a dependency on that item. To take a newer upstream shadcn component, replace the
file with upstream's and review the diff like any other change.
