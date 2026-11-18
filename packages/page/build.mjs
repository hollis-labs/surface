// Builds @hollis-labs/surface-page from the page's source in web/src, which stays where it is so
// Surface's own apps keep building from source. JavaScript is bundled by esbuild (one shared
// runtime chunk, so the page and a module's sdk import see the same connection); types come from tsc.
//
// What is left as an import for the app to resolve:
//   @/components/ui/*    the app's installed registry components (its own `@/` alias)
//   react, react-dom, lucide-react, @hollis-labs/*    ordinary packages
import { execFileSync } from "node:child_process"
import { rmSync, writeFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const here = dirname(fileURLToPath(import.meta.url))
const src = resolve(here, "../../web/src")
const out = resolve(here, "dist")
rmSync(out, { recursive: true, force: true })

const external = ["react", "react/*", "react-dom", "react-dom/*", "lucide-react", "@hollis-labs/*", "@/*"]
const result = await build({
  entryPoints: { index: `${src}/index.ts`, sdk: `${src}/sdk/index.ts`, gallery: `${src}/gallery/index.ts` },
  outdir: out,
  bundle: true,
  splitting: true,
  format: "esm",
  platform: "browser",
  target: "es2022",
  jsx: "automatic",
  external,
  chunkNames: "chunks/[name]-[hash]",
  legalComments: "none",
  logLevel: "warning",
  metafile: true,
})

// The registry items the page draws with, for `surface-ui add --for @hollis-labs/surface-page`.
// The gallery is left out: it draws every item, and an app opts into it.
const items = new Set()
for (const [file, o] of Object.entries(result.metafile.outputs)) {
  if (file.endsWith("gallery.js")) continue
  for (const i of o.imports)
    if (i.path.startsWith("@/components/ui/")) items.add(i.path.slice("@/components/ui/".length))
}
writeFileSync(resolve(out, "registry-items.json"), `${JSON.stringify([...items].sort(), null, 2)}\n`)

execFileSync("npx", ["tsc", "-p", resolve(here, "tsconfig.build.json")], { stdio: "inherit" })
// The program includes the installed components and generated wiring to check types; they are the
// app's, not this package's.
for (const d of ["components", "generated"]) rmSync(resolve(out, "types", d), { recursive: true, force: true })
rmSync(resolve(out, "types", "main.d.ts"), { force: true })
console.log("built @hollis-labs/surface-page into dist/")
