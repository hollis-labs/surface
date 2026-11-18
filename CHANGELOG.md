# Changelog

One version for the whole of Surface: the registry at the tag, the Go module and the
`@hollis-labs/surface-*` npm packages (`docs/releasing.md`).

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
