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

## Using it in an app

`ui.lock.json` at the app's root names the registry and where components go:

```json
{ "registry": "./registry/r", "uiDir": "web/src/components/ui", "items": {} }
```

An app outside this repository points at a tag:
`"registry": "https://raw.githubusercontent.com/hollis-labs/surface/{ref}/registry/r/{name}.json", "ref": "v0.1.0"`.

```sh
node scripts/ui.mjs add button dialog    # install items and what they depend on
node scripts/ui.mjs sync                 # reinstall everything locked, from the registry
node scripts/ui.mjs verify               # CI: fail on an edited, stale, missing or stray file
node scripts/ui.mjs status
```

The app imports the theme once, after Tailwind, and needs the `@/` alias to its source directory:

```css
@import "tailwindcss";
@import "<path to>/registry/theme/surface.css";
```

The stock `shadcn` CLI and its MCP server work against the same registry (`components.json` lists it as
`@surface`): `npx shadcn add @surface/button --diff` shows how an installed file differs.

## Changing the registry

Edit or add a file under `ui/`, then `node scripts/ui.mjs build` and commit the generated files with it.
Dependencies are read from each file's imports: a package import becomes an npm dependency, an import of
`@/components/ui/<name>` a dependency on that item. To take a newer upstream shadcn component, replace the
file with upstream's and review the diff like any other change.
