#!/usr/bin/env node
// Surface has one version: the tag vX.Y.Z names the registry, the Go module and every npm package.
//   node scripts/version.mjs            check that every package carries the same version
//   node scripts/version.mjs 0.2.0      set it everywhere (and the page's pins on its siblings)
import { readFileSync, writeFileSync } from "node:fs"
import { join, resolve } from "node:path"

const root = resolve(import.meta.dirname, "..")
const names = ["chat", "cards", "genui", "page", "ui"]
const file = (p) => join(root, "packages", p, "package.json")
const pkgs = Object.fromEntries(names.map((p) => [p, JSON.parse(readFileSync(file(p), "utf8"))]))
const want = process.argv[2]

if (want) {
  if (!/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(want)) throw new Error(`not a version: ${want}`)
  for (const [p, pkg] of Object.entries(pkgs)) {
    pkg.version = want
    for (const dep of Object.keys(pkg.dependencies ?? {})) if (dep.startsWith("@hollis-labs/surface-")) pkg.dependencies[dep] = want
    writeFileSync(file(p), `${JSON.stringify(pkg, null, 2)}\n`)
  }
  console.log(`set ${names.map((p) => pkgs[p].name).join(", ")} to ${want}; tag v${want}`)
} else {
  const v = pkgs.page.version
  const off = []
  for (const pkg of Object.values(pkgs)) {
    if (pkg.version !== v) off.push(`${pkg.name} is ${pkg.version}`)
    for (const [dep, r] of Object.entries(pkg.dependencies ?? {})) if (dep.startsWith("@hollis-labs/surface-") && r !== v) off.push(`${pkg.name} pins ${dep}@${r}`)
  }
  if (off.length) {
    console.error(`surface version: not one version (${v}):\n  ${off.join("\n  ")}\nRun: node scripts/version.mjs <version>`)
    process.exit(1)
  }
  console.log(`surface version: every package is ${v}`)
}
