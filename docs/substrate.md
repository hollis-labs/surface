# Surface on Substrate

Surface is built on [Substrate](https://github.com/hollis-labs/substrate) and uses its GUI kit as the widget
layer. This page says what is used, and where Surface does something of its own and why.

## What Substrate supplies

- **The shell of every app:** the manifest and generated wiring, boot order and hooks, the config file and its
  override, logging, health (`/healthz`), the HTTP listener's lifecycle (`httphost.Serve`), the database handle
  (`modules/database`, SQLite), and the CLI skeleton (`modules/cli`).
- **Pins:** `go.mod` follows Substrate's BOM.
- **From the shared frontend packages, behaviour only:** `@hollis-labs/ade-core`, the attention hub behind ADE's
  overlays. Everything that draws is Surface's own registry (`registry/README.md`), not the design kit; the agent
  panel's conversation is `packages/chat` (behaviour) with the registry's chat components.

## Where Surface goes its own way

1. **Its own module contract.** A Substrate module is a boot-time capability (`Name`, `Start`, `Stop`) that
   shares an untyped values bag. An ADE module is views, commands and agent tools with a typed Host. So
   Surface runs as the app on top of Substrate and defines `surface/module`; Notes or Files are not Substrate
   modules. The registration idiom is the same (register from `init`, select by blank import in generated
   wiring).
2. **Live settings.** Substrate config is the manifest's YAML baked into the wiring, plus an override file read
   at boot; there is no reload. Surface's app settings must change with no restart, so they are Surface's own:
   a table in the core database, with watchers and a bus event.
3. **The window manager and the palette.** The kit has widgets and a page shell (`app-shell`: routes, nav,
   docked tabs) but no windows: nothing tiles, floats or resizes, and the frontend manifest has no window
   kind. Surface's core owns the desk and the palette (their design is Basalisk's), drawn with the kit's
   tokens. Substrate's status lists a `gui-shell` profile as planned and unplaced; Surface may be it.
4. **Serving its own routes.** `httphost.Handler` guards `/api/` and rewrites handlers' error bodies to its own
   shape. Surface's event stream and the agent's wire protocol must reach the page as written, and Surface has
   no sign-in for the guard to enforce, so `app` mounts Substrate's handler for `/healthz` and serves the rest
   itself, on the listener Substrate manages. If Surface later gets sign-in, its routes can move behind
   `httphost` with raw prefixes for the streams.

None of these is a conflict with Substrate; each is a place where an interface framework needs something a
service framework does not have. The first and third are candidates to fold back as Substrate's GUI profile
once the shapes settle.

## The agent

Surface embeds Nanite through `nanite/chatrunner` and keeps its durable chat transcript separately.
Substrate supplies the process and persistence mechanisms; Nanite supplies the model runtime.
Tests use the scripted conversation runner; no legacy runtime or HTTP transport is constructed.
