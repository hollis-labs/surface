#!/usr/bin/env node
// surface ui: keep an app's copied components identical to the @surface registry.
//
// Components are installed into an app's own source, where its build sees them and an agent
// can read them. They are not edited there. This tool installs them, records what it
// installed in ui.lock.json, and fails when a file differs from what the registry holds:
// a change is made in the registry, then synced.
//
//   node scripts/ui.mjs build              registry maintainers: regenerate registry.json and registry/r
//   node scripts/ui.mjs add <item...>      install items (and what they depend on) into the app
//   node scripts/ui.mjs sync               reinstall every locked item from the registry
//   node scripts/ui.mjs verify             exit 1 if any installed file differs from the lock or the registry
//   node scripts/ui.mjs status             what is installed, from where
//
// Options: --app <dir> (default: the current directory), --registry <dir or URL template>.
// The lock names the registry: a directory holding <name>.json files, or a URL with {name}
// (and optionally {ref}), for example
//   https://raw.githubusercontent.com/hollis-labs/surface/{ref}/registry/r/{name}.json
import { createHash } from "node:crypto"
import { execFileSync } from "node:child_process"
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs"
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
async function fetchItem(lock, name) {
  const src = lock.registry
  if (/^https?:\/\//.test(src)) {
    const url = src.replace("{name}", name).replace("{ref}", lock.ref ?? "main")
    const res = await fetch(url)
    if (!res.ok) fail(`${url}: ${res.status} ${res.statusText}`)
    return res.json()
  }
  const file = join(resolve(appDir, src), `${name}.json`)
  if (!existsSync(file)) fail(`the registry at ${src} has no item "${name}"`)
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

const targetOf = (lock, file) => join(lock.uiDir, basename(file.path))

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
    if (missing.length) console.log(`these packages are needed and not in package.json:\n  npm install ${missing.join(" ")}`)
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
      else if (sha(readFileSync(abs, "utf8")) !== hash) problems.push(`${rel}: edited in place. Change it in the registry, then run "ui sync"`)
      else if (want.get(rel) !== hash) problems.push(`${rel}: behind the registry (item ${name}). Run "ui sync"`)
    }
    for (const rel of want.keys()) if (!entry.files[rel]) problems.push(`${rel}: the registry's ${name} has a file the app lacks. Run "ui sync"`)
  }
  // Nothing lives beside the copied components that the registry did not put there.
  const dir = join(appDir, lock.uiDir)
  if (existsSync(dir)) {
    for (const f of readdirSync(dir)) {
      const rel = join(lock.uiDir, f)
      if (!locked.has(rel)) problems.push(`${rel}: not from the registry. Shared components come from @surface; an app's own go elsewhere`)
    }
  }
  if (problems.length) {
    console.error(`surface ui: ${problems.length} problem(s)\n  ${problems.join("\n  ")}`)
    process.exit(1)
  }
  console.log(`surface ui: ${locked.size} file(s) in ${Object.keys(lock.items).length} item(s) match the registry`)
}

/** Registry maintainers: derive registry.json from the sources, then let shadcn build the item files. */
function build() {
  const root = resolve(appDir, "registry")
  const uiDir = join(root, "ui")
  const meta = existsSync(join(root, "items.json")) ? JSON.parse(readFileSync(join(root, "items.json"), "utf8")) : {}
  const items = []
  for (const f of readdirSync(uiDir).filter((x) => x.endsWith(".tsx")).sort()) {
    const name = f.replace(/\.tsx$/, "")
    const text = readFileSync(join(uiDir, f), "utf8")
    const npm = new Set()
    const deps = new Set()
    for (const m of text.matchAll(/from\s+"([^"]+)"/g)) {
      const spec = m[1]
      if (spec.startsWith("@/components/ui/")) deps.add(`@surface/${spec.slice("@/components/ui/".length)}`)
      else if (!spec.startsWith(".") && !spec.startsWith("@/") && spec !== "react" && spec !== "react-dom") {
        npm.add(spec.startsWith("@") ? spec.split("/").slice(0, 2).join("/") : spec.split("/")[0])
      }
    }
    items.push({
      name,
      type: "registry:ui",
      title: meta[name]?.title ?? name.replace(/(^|-)(\w)/g, (_, d, c) => (d ? " " : "") + c.toUpperCase()),
      ...(meta[name]?.description ? { description: meta[name].description } : {}),
      ...(npm.size ? { dependencies: [...npm].sort() } : {}),
      ...(deps.size ? { registryDependencies: [...deps].sort() } : {}),
      files: [{ path: `registry/ui/${f}`, type: "registry:ui" }],
    })
  }
  const registry = { $schema: "https://ui.shadcn.com/schema/registry.json", name: "surface", homepage: "https://github.com/hollis-labs/surface", items }
  writeFileSync(join(root, "registry.json"), `${JSON.stringify(registry, null, 2)}\n`)
  execFileSync("npx", ["shadcn", "build", "registry/registry.json", "-o", "registry/r"], { cwd: appDir, stdio: "inherit" })
  console.log(`built ${items.length} items into ${relative(appDir, join(root, "r"))}`)
}

switch (cmd) {
  case "build":
    build()
    break
  case "add": {
    if (!args.length) fail("add needs at least one item name")
    await install(readLock(), args)
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
  case "status": {
    const lock = readLock()
    console.log(`registry: ${lock.registry}${lock.ref ? ` @ ${lock.ref}` : ""}\ninstalled into: ${lock.uiDir}`)
    for (const [name, e] of Object.entries(lock.items)) console.log(`  ${name}  (${Object.keys(e.files).length} file)`)
    break
  }
  default:
    fail("usage: ui.mjs build | add <item...> | sync | verify | status  [--app <dir>] [--registry <dir or URL>]")
}
