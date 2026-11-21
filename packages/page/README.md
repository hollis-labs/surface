# @hollis-labs/surface-page

The core of a Surface app's page: the desktop (the bar, windows that tile, tab and float, the palette), the
agent panel, cards, generated surfaces and ADE's overlays, plus the sdk a module's views import. It is built
from `web/src` in the [Surface](https://github.com/hollis-labs/surface) repository, where Surface's own apps
compile it from source.

It draws nothing of its own. Every control is one of the app's installed Surface registry components
(`@/components/ui/*`), so the app installs those first and keeps them in step with the registry:

```sh
npx surface-ui init --ref v0.1.0
npx surface-ui add theme --for @hollis-labs/surface-page
```

| Import | What |
|---|---|
| `@hollis-labs/surface-page` | `mountPage({ modules, core? })`, `Page`, `Desktop`, `CardHost`, `CardsWindow`, `coreWeb`, `registerCard` |
| `@hollis-labs/surface-page/sdk` | What a module's views may import: `useEvent`, `useSetting`, `useMarks`, `useAnchor`, `Icon`, `CiteText`, `CiteRef`, `RecordBody`, `ObjectActionsMenu`, `registerObjectAction`, `useObjectActions`, `registerNavGroup`, `registerNavItem`, `useNavTree`, and the SDK types |
| `@hollis-labs/surface-page/gallery` | `galleryViews`: the Components window, which needs every registry item installed |
| `@hollis-labs/surface-page/vite` | `surfacePage()`: the Vite settings the package needs |

The page talks to the app's server over `/api` and `/mcp`. The full setup is in the repository's
`registry/README.md`, under "An app in its own repository". The package's version is the Surface tag the app
is on: its registry `ref` and the other `@hollis-labs/surface-*` packages move with it.
