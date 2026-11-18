#!/usr/bin/env node
// Build and pack every published package, install the tarballs into an empty directory, and check
// that what an app would get works there: the behaviour packages import under plain Node, and the
// page bundles with its `@/components/ui/*` imports pointed at installed components (this repo's).
// Nothing is published. Usage: node scripts/pack-check.mjs [--keep]
import { execFileSync } from "node:child_process"
import { mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"

const root = resolve(import.meta.dirname, "..")
// Dependency order: the page needs the three behaviour packages.
const packages = ["chat", "cards", "genui", "page", "ui"]
const run = (cmd, args, cwd) => execFileSync(cmd, args, { cwd, stdio: ["ignore", "pipe", "inherit"] }).toString()

const dir = mkdtempSync(join(tmpdir(), "surface-pack-"))
for (const p of packages) {
  const out = run("npm", ["pack", "--silent", "--pack-destination", dir], join(root, "packages", p)).trim().split("\n").at(-1)
  console.log(`packed ${out}`)
}
writeFileSync(join(dir, "package.json"), JSON.stringify({ name: "pack-check", private: true, type: "module" }))
const tarballs = readdirSync(dir).filter((f) => f.endsWith(".tgz")).map((f) => `./${f}`)
const pin = (name) => `${name}@${JSON.parse(run("node", ["-p", `JSON.stringify(require('./package.json').dependencies)`], root))[name]}`
run("npm", ["install", "--silent", "--no-audit", "--no-fund", ...tarballs, pin("react"), pin("react-dom"), pin("lucide-react")], dir)

for (const p of ["chat", "cards", "genui"]) {
  const n = run("node", ["-e", `import("@hollis-labs/surface-${p}").then((m) => console.log(Object.keys(m).length))`], dir).trim()
  console.log(`@hollis-labs/surface-${p}: imports under Node, ${n} exports`)
}

// The page is for a bundler: point `@/` where an app's installed components would be.
writeFileSync(
  join(dir, "entry.js"),
  'import * as page from "@hollis-labs/surface-page"\nimport * as sdk from "@hollis-labs/surface-page/sdk"\nimport * as gallery from "@hollis-labs/surface-page/gallery"\nconsole.log(Object.keys(page).length, Object.keys(sdk).length, Object.keys(gallery).length)\n',
)
const { build } = await import(join(root, "node_modules/esbuild/lib/main.js"))
await build({
  entryPoints: [join(dir, "entry.js")],
  bundle: true,
  write: false,
  format: "esm",
  jsx: "automatic",
  alias: { "@": join(root, "web/src") },
  nodePaths: [join(dir, "node_modules"), join(root, "node_modules")],
  loader: { ".css": "empty" },
  logLevel: "error",
})
console.log("@hollis-labs/surface-page: bundles (index, sdk, gallery) against installed components")

try {
  run(join(dir, "node_modules/.bin/surface-ui"), ["status"], dir)
} catch (e) {
  // No lock in an empty directory: the bin ran and said so.
  if (!String(e.stderr ?? e.message).includes("ui.lock.json") && e.status !== 1) throw e
}
console.log("@hollis-labs/surface-ui: the surface-ui bin runs")
console.log(`ok: ${dir}`)
if (!process.argv.includes("--keep")) rmSync(dir, { recursive: true, force: true })
