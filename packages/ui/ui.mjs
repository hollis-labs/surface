#!/usr/bin/env node
import { execFileSync } from "node:child_process"
// surface ui: keep an app's copied components identical to the @surface registry.
//
// Components are installed into an app's own source, where its build sees them and an agent
// can read them. They are not edited there. This tool installs them, records what it
// installed in ui.lock.json, and fails when a file differs from what the registry holds:
// a change is made in the registry, then synced.
//
// In this repository it is `node scripts/ui.mjs`; in an app, `npx @hollis-labs/surface-ui` (bin surface-ui).
//
//   node scripts/ui.mjs init [--ref <tag>] [--dir <components dir>]
//                                           write ui.lock.json naming the registry on GitHub at a tag
//   node scripts/ui.mjs build              registry maintainers: regenerate registry.json and registry/r
//   node scripts/ui.mjs add <item...>      install items (and what they depend on) into the app
//   node scripts/ui.mjs add --for <package>
//                                           install the items an installed package draws with
//                                           (@hollis-labs/surface-page lists its own)
//   node scripts/ui.mjs sync               reinstall every locked item from the registry
//   node scripts/ui.mjs verify             exit 1 if any installed file differs from the lock or the registry
//   node scripts/ui.mjs status             what is installed, from where
//   node scripts/ui.mjs report <app dir...> [--from <file>] [--json]
//                                           each app's registry, item count and drift: items missing,
//                                           edited in place or behind the registry (--registry to compare
//                                           every app with one registry). --from names a file listing app
//                                           dirs, one per line (# starts a comment).
//
// Options: --app <dir> (default: the current directory), --registry <dir or URL template>.
// The lock names the registry: a directory holding <name>.json files, or a URL with {name}
// (and optionally {ref}), for example
//   https://raw.githubusercontent.com/hollis-labs/surface/{ref}/registry/r/{name}.json
import { createHash } from "node:crypto"
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs"
import { createRequire } from "node:module"
import { basename, dirname, join, relative, resolve } from "node:path"

const args = process.argv.slice(2)
const cmd = args.shift()
const opt = (name) => {
  const i = args.indexOf(`--${name}`)
  if (i < 0) return undefined
  const v = args[i + 1]
  args.splice(i, 2)
  return v
}
const appDir = resolve(opt("app") ?? ".")
const forPackage = opt("for")
const initRef = opt("ref")
const initDir = opt("dir")
const GITHUB_REGISTRY = "https://raw.githubusercontent.com/hollis-labs/surface/{ref}/registry/r/{name}.json"
const registryOverride = opt("registry")
const lockPath = join(appDir, "ui.lock.json")
const sha = (text) => createHash("sha256").update(text).digest("hex")
const fail = (msg) => {
  console.error(`surface ui: ${msg}`)
  process.exit(1)
}
const bare = (name) => name.replace(/^@surface\//, "")

function readLock() {
  if (!existsSync(lockPath)) {
    fail(`no ui.lock.json in ${appDir}. Create one with {"registry": "...", "uiDir": "src/components/ui", "items": {}}`)
  }
  const lock = JSON.parse(readFileSync(lockPath, "utf8"))
  if (registryOverride) lock.registry = registryOverride
  if (!lock.registry || !lock.uiDir) fail("ui.lock.json needs registry and uiDir")
  lock.items ??= {}
  return lock
}

function writeLock(lock) {
  const items = Object.fromEntries(Object.entries(lock.items).sort(([a], [b]) => a.localeCompare(b)))
  writeFileSync(lockPath, `${JSON.stringify({ ...lock, items }, null, 2)}\n`)
}

/** One registry item, from a directory or a URL template. */
async function fetchItem(lock, name, { base = appDir, soft = false } = {}) {
  const src = lock.registry
  if (/^https?:\/\//.test(src)) {
    const url = src.replace("{name}", name).replace("{ref}", lock.ref ?? "main")
    const res = await fetch(url)
    if (!res.ok) {
      if (soft && res.status === 404) return null
      fail(`${url}: ${res.status} ${res.statusText}`)
    }
    return res.json()
  }
  const file = join(resolve(base, src), `${name}.json`)
  if (!existsSync(file)) {
    if (soft) return null
    fail(`the registry at ${src} has no item "${name}"`)
  }
  return JSON.parse(readFileSync(file, "utf8"))
}

/** The named items and everything they depend on in the registry, dependencies first. */
async function closure(lock, names) {
  const seen = new Map()
  const visit = async (raw) => {
    const name = bare(raw)
    if (seen.has(name)) return
    const item = await fetchItem(lock, name)
    seen.set(name, null) // reserved: a cycle stops here
    for (const dep of item.registryDependencies ?? []) await visit(dep)
    seen.delete(name)
    seen.set(name, item)
  }
  for (const n of names) await visit(n)
  return seen
}

// Components go in uiDir; the theme (a registry:file) beside it, or in themeDir when the lock names one.
const targetOf = (lock, file) =>
  join(file.type === "registry:file" ? (lock.themeDir ?? dirname(lock.uiDir)) : lock.uiDir, basename(file.path))

async function install(lock, names) {
  const items = await closure(lock, names)
  const npm = new Set()
  for (const [name, item] of items) {
    const files = {}
    for (const f of item.files ?? []) {
      const rel = targetOf(lock, f)
      const abs = join(appDir, rel)
      mkdirSync(dirname(abs), { recursive: true })
      writeFileSync(abs, f.content)
      files[rel] = sha(f.content)
    }
    lock.items[name] = { files }
    for (const d of item.dependencies ?? []) npm.add(d)
  }
  writeLock(lock)
  console.log(`installed ${[...items.keys()].join(", ")}`)
  // The app must have the packages the components import.
  const pkgPath = join(appDir, "package.json")
  if (existsSync(pkgPath)) {
    const pkg = JSON.parse(readFileSync(pkgPath, "utf8"))
    const have = { ...pkg.dependencies, ...pkg.devDependencies }
    const missing = [...npm].filter((d) => !have[d.replace(/(?<=.)@[^/]+$/, "")])
    if (missing.length)
      console.log(`these packages are needed and not in package.json:\n  npm install ${missing.join(" ")}`)
  }
}

async function verify(lock) {
  const problems = []
  const locked = new Set()
  for (const [name, entry] of Object.entries(lock.items)) {
    const item = await fetchItem(lock, name)
    const want = new Map((item.files ?? []).map((f) => [targetOf(lock, f), sha(f.content)]))
    for (const [rel, hash] of Object.entries(entry.files)) {
      locked.add(rel)
      const abs = join(appDir, rel)
      if (!existsSync(abs)) problems.push(`${rel}: missing (item ${name})`)
      else if (sha(readFileSync(abs, "utf8")) !== hash)
        problems.push(`${rel}: edited in place. Change it in the registry, then run "ui sync"`)
      else if (want.get(rel) !== hash) problems.push(`${rel}: behind the registry (item ${name}). Run "ui sync"`)
    }
    for (const rel of want.keys())
      if (!entry.files[rel]) problems.push(`${rel}: the registry's ${name} has a file the app lacks. Run "ui sync"`)
  }
  // Nothing lives beside the copied components that the registry did not put there.
  const dir = join(appDir, lock.uiDir)
  if (existsSync(dir)) {
    for (const f of readdirSync(dir)) {
      const rel = join(lock.uiDir, f)
      if (!locked.has(rel))
        problems.push(`${rel}: not from the registry. Shared components come from @surface; an app's own go elsewhere`)
    }
  }
  if (problems.length) {
    console.error(`surface ui: ${problems.length} problem(s)\n  ${problems.join("\n  ")}`)
    process.exit(1)
  }
  console.log(`surface ui: ${locked.size} file(s) in ${Object.keys(lock.items).length} item(s) match the registry`)
}

/**
 * One app's drift against its registry (or the one given with --registry): for each locked
 * item, whether a file is missing, edited in place, or behind the registry; files the
 * registry has that the app lacks; and files in the components directory nothing put there.
 */
async function inspect(dir) {
  const lockFile = join(dir, "ui.lock.json")
  if (!existsSync(lockFile)) return { dir, error: "no ui.lock.json" }
  const lock = JSON.parse(readFileSync(lockFile, "utf8"))
  lock.items ??= {}
  // A registry given on the command line is read from where the command runs.
  const base = registryOverride ? process.cwd() : dir
  if (registryOverride) lock.registry = registryOverride
  let app = basename(resolve(dir))
  try {
    app = JSON.parse(readFileSync(join(dir, "package.json"), "utf8")).name ?? app
  } catch {
    // No package.json: the directory's name will do.
  }
  if (!/^https?:\/\//.test(lock.registry ?? "") && !existsSync(resolve(base, lock.registry ?? ""))) {
    return { app, dir, error: `its registry ${lock.registry} is not there (give one with --registry)` }
  }
  const drift = []
  const locked = new Set()
  for (const [name, entry] of Object.entries(lock.items)) {
    const item = await fetchItem(lock, name, { base, soft: true })
    if (!item) {
      for (const rel of Object.keys(entry.files ?? {})) locked.add(rel)
      drift.push({ item: name, kind: "unknown", detail: "the registry has no such item" })
      continue
    }
    const want = new Map((item.files ?? []).map((f) => [targetOf(lock, f), sha(f.content)]))
    for (const [rel, hash] of Object.entries(entry.files ?? {})) {
      locked.add(rel)
      const abs = join(dir, rel)
      if (!existsSync(abs)) drift.push({ item: name, file: rel, kind: "missing" })
      else if (sha(readFileSync(abs, "utf8")) !== hash) drift.push({ item: name, file: rel, kind: "edited" })
      else if (want.get(rel) !== hash) drift.push({ item: name, file: rel, kind: "behind" })
    }
    for (const rel of want.keys())
      if (!entry.files?.[rel]) drift.push({ item: name, file: rel, kind: "behind", detail: "a file the app lacks" })
  }
  const ui = join(dir, lock.uiDir ?? "")
  if (lock.uiDir && existsSync(ui)) {
    for (const f of readdirSync(ui)) {
      const rel = join(lock.uiDir, f)
      if (!locked.has(rel)) drift.push({ item: "", file: rel, kind: "stray" })
    }
  }
  return { app, dir, registry: lock.registry, ref: lock.ref ?? null, items: Object.keys(lock.items).length, drift }
}

const WORDS = {
  missing: "missing",
  edited: "edited in place",
  behind: "behind the registry",
  unknown: "not in the registry",
  stray: "not from the registry",
}

async function report(dirs, asJson) {
  const out = []
  for (const d of dirs) out.push(await inspect(resolve(d)))
  if (asJson) {
    console.log(JSON.stringify(out, null, 2))
    return
  }
  for (const r of out) {
    if (r.error) {
      console.log(`${r.app ?? r.dir} (${r.dir}): ${r.error}`)
      continue
    }
    const rel = relative(process.cwd(), r.dir)
    const where = rel.startsWith("..") ? r.dir : rel || "."
    const state = r.drift.length ? `${r.drift.length} drifting` : "in step"
    console.log(`${r.app} (${where}): ${r.registry}${r.ref ? ` @ ${r.ref}` : ""} · ${r.items} items · ${state}`)
    for (const x of r.drift) {
      const what = [x.item, x.file].filter(Boolean).join(": ")
      console.log(`  ${what}: ${WORDS[x.kind] ?? x.kind}${x.detail ? ` (${x.detail})` : ""}`)
    }
  }
}

/** Registry maintainers: derive registry.json from the sources, then let shadcn build the item files. */
function build() {
  const root = resolve(appDir, "registry")
  const uiDir = join(root, "ui")
  const meta = existsSync(join(root, "items.json")) ? JSON.parse(readFileSync(join(root, "items.json"), "utf8")) : {}
  const items = []
  for (const f of readdirSync(uiDir)
    .filter((x) => x.endsWith(".tsx"))
    .sort()) {
    const name = f.replace(/\.tsx$/, "")
    const text = readFileSync(join(uiDir, f), "utf8")
    const npm = new Set()
    const deps = new Set()
    // Both `import x from "spec"` and a bare `import "spec"` (a stylesheet) count.
    for (const m of text.matchAll(/(?:from|^import)\s+"([^"]+)"/gm)) {
      const spec = m[1]
      if (spec.startsWith("@/components/ui/")) deps.add(`@surface/${spec.slice("@/components/ui/".length)}`)
      else if (!spec.startsWith(".") && !spec.startsWith("@/") && spec !== "react" && spec !== "react-dom") {
        const pkg = spec.startsWith("@") ? spec.split("/").slice(0, 2).join("/") : spec.split("/")[0]
        // A package the item needs at one exact version says so in items.json ("pins").
        npm.add(meta[name]?.pins?.[pkg] ? `${pkg}@${meta[name].pins[pkg]}` : pkg)
      }
    }
    // An item is its .tsx and any file beside it of the same name (its stylesheet).
    const beside = readdirSync(uiDir)
      .filter((x) => x.startsWith(`${name}.`) && x !== f)
      .sort()
    items.push({
      name,
      type: "registry:ui",
      title: meta[name]?.title ?? name.replace(/(^|-)(\w)/g, (_, d, c) => (d ? " " : "") + c.toUpperCase()),
      ...(meta[name]?.description ? { description: meta[name].description } : {}),
      ...(npm.size ? { dependencies: [...npm].sort() } : {}),
      ...(deps.size ? { registryDependencies: [...deps].sort() } : {}),
      files: [f, ...beside].map((x) => ({ path: `registry/ui/${x}`, type: "registry:ui" })),
    })
  }
  // The theme is one item too, so an app outside this repository installs it at the same ref.
  const themeDir = join(root, "theme")
  for (const f of existsSync(themeDir) ? readdirSync(themeDir).filter((x) => x.endsWith(".css")) : []) {
    const text = readFileSync(join(themeDir, f), "utf8")
    const npm = new Set()
    for (const m of text.matchAll(/^@import\s+"([^".][^"]*)"/gm)) {
      if (m[1] !== "tailwindcss")
        npm.add(m[1].startsWith("@") ? m[1].split("/").slice(0, 2).join("/") : m[1].split("/")[0])
    }
    const name = f === "surface.css" ? "theme" : `theme-${f.replace(/\.css$/, "")}`
    items.push({
      name,
      type: "registry:file",
      title: meta[name]?.title ?? "Theme",
      ...(meta[name]?.description ? { description: meta[name].description } : {}),
      ...(npm.size ? { dependencies: [...npm].sort() } : {}),
      files: [{ path: `registry/theme/${f}`, type: "registry:file", target: `src/${f}` }],
    })
  }
  const registry = {
    $schema: "https://ui.shadcn.com/schema/registry.json",
    name: "surface",
    homepage: "https://github.com/hollis-labs/surface",
    items,
  }
  writeFileSync(join(root, "registry.json"), `${JSON.stringify(registry, null, 2)}\n`)
  execFileSync("npx", ["shadcn", "build", "registry/registry.json", "-o", "registry/r"], {
    cwd: appDir,
    stdio: "inherit",
  })
  console.log(`built ${items.length} items into ${relative(appDir, join(root, "r"))}`)
}

/** The registry items an installed package says it draws with ("surface": {"registryItems": file}). */
function itemsFor(pkg) {
  let manifest
  try {
    manifest = createRequire(join(appDir, "package.json")).resolve(`${pkg}/package.json`)
  } catch {
    fail(`${pkg} is not installed in ${appDir} (npm install it first)`)
  }
  const meta = JSON.parse(readFileSync(manifest, "utf8"))
  const list = meta.surface?.registryItems
  if (!list) fail(`${pkg} does not list the registry items it uses`)
  return JSON.parse(readFileSync(join(dirname(manifest), list), "utf8"))
}

switch (cmd) {
  case "init": {
    if (existsSync(lockPath)) fail(`${lockPath} exists already`)
    const lock = {
      registry: registryOverride ?? GITHUB_REGISTRY,
      ...(registryOverride ? {} : { ref: initRef ?? "main" }),
      uiDir: initDir ?? "src/components/ui",
      items: {},
    }
    writeLock(lock)
    console.log(
      `wrote ${relative(process.cwd(), lockPath) || lockPath}: ${lock.registry}${lock.ref ? ` @ ${lock.ref}` : ""}, components in ${lock.uiDir}`,
    )
    break
  }
  case "build":
    build()
    break
  case "add": {
    const names = [...args, ...(forPackage ? itemsFor(forPackage) : [])]
    if (!names.length) fail("add needs at least one item name (or --for <package>)")
    await install(readLock(), names)
    break
  }
  case "sync": {
    const lock = readLock()
    await install(lock, Object.keys(lock.items))
    break
  }
  case "verify":
    await verify(readLock())
    break
  case "report": {
    const asJson = args.includes("--json")
    const from = opt("from")
    const dirs = args.filter((a) => a !== "--json")
    if (from) {
      for (const line of readFileSync(from, "utf8").split("\n")) {
        const d = line.replace(/#.*/, "").trim()
        if (d) dirs.push(resolve(dirname(resolve(from)), d))
      }
    }
    if (!dirs.length) dirs.push(appDir)
    await report(dirs, asJson)
    break
  }
  case "status": {
    const lock = readLock()
    console.log(`registry: ${lock.registry}${lock.ref ? ` @ ${lock.ref}` : ""}\ninstalled into: ${lock.uiDir}`)
    for (const [name, e] of Object.entries(lock.items)) console.log(`  ${name}  (${Object.keys(e.files).length} file)`)
    break
  }
  default:
    fail(
      "usage: ui.mjs init [--ref <tag>] [--dir <dir>] | build | add <item...> [--for <package>] | sync | verify | status | report <app dir...> [--from <file>] [--json]  [--app <dir>] [--registry <dir or URL>]",
    )
}
