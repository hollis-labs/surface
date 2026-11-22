import assert from "node:assert/strict"
import { readdirSync, readFileSync, statSync } from "node:fs"
import { createRequire } from "node:module"
import { join } from "node:path"
import test from "node:test"

// <Icon name="…"> looks a name up in lucide's icon set and draws a dot for one it lacks, so a
// name lucide renamed (lucide 1.x dropped the old aliases: "fingerprint", "trash-2",
// "alert-triangle"…) fails quietly. This reads every page and module source for the names
// written into <Icon name=…> and checks each one exists.

const root = new URL("../../../", import.meta.url).pathname
const icons = createRequire(import.meta.url)("lucide-react").icons as Record<string, unknown>
const pascal = (n: string) =>
  n
    .split("-")
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join("")

function sources(dir: string, out: string[] = []): string[] {
  for (const e of readdirSync(dir)) {
    if (e === "node_modules" || e === "dist" || e === "generated") continue
    const p = join(dir, e)
    if (statSync(p).isDirectory()) sources(p, out)
    else if (/\.tsx?$/.test(e)) out.push(p)
  }
  return out
}

test("every <Icon name> is a lucide icon", () => {
  const missing: string[] = []
  for (const dir of ["web/src", "modules"]) {
    for (const file of sources(join(root, dir))) {
      const text = readFileSync(file, "utf8")
      for (const m of text.matchAll(/<Icon\s+name=(?:"([a-z0-9-]+)"|\{([^}]*)\})/g)) {
        // In an expression, the names are the branches and fallbacks (after ? : ?? ||), not
        // the strings a condition compares with.
        const names = m[1] ? [m[1]] : [...(m[2] ?? "").matchAll(/(?:\?\??|:|\|\|)\s*"([a-z0-9-]+)"/g)].map((x) => x[1])
        for (const n of names) {
          if (!icons[pascal(n)]) missing.push(`${file.slice(root.length)}: ${n}`)
        }
      }
    }
  }
  assert.deepEqual(missing, [])
})
