import { isWordChar, matches, trimTrailing } from "../scan-utils.ts"
import type { RefHit, RefKind } from "../types.ts"

// Segments never contain "/", so the repeated group is unambiguous. The
// pattern is anchored and applied once per whitespace-delimited token: an
// unanchored global scan retries from every offset of a long run of path
// characters and goes quadratic. Optional `:line[:col]` suffix, as in `foo.ts:42:7`.
const PATH = /^(?:~|\.{1,2})?\/?[A-Za-z0-9_.@+%-]+(?:\/[A-Za-z0-9_.@+%-]+)+\/?(?::\d{1,6}(?::\d{1,4})?)?/
// A path on a remote Files place: `remote:<id>/<path>`.
const REMOTE = /^remote:[a-z0-9][a-z0-9-]{0,39}(?:\/[A-Za-z0-9_.@+%-]+)+\/?(?::\d{1,6}(?::\d{1,4})?)?/

const TOKEN = /[A-Za-z0-9_.@+%~/:-]+/g

const ABS_ROOTS = new Set([
  "home",
  "srv",
  "tmp",
  "etc",
  "var",
  "usr",
  "opt",
  "mnt",
  "Users",
  "root",
  "bin",
  "dev",
  "proc",
  "run",
  "workspace",
  "workspaces",
  "app",
  "data",
  "Volumes",
])
const REPO_DIRS = new Set([
  "apps",
  "src",
  "lib",
  "libs",
  "cmd",
  "internal",
  "pkg",
  "docs",
  "plugins",
  "packages",
  "scripts",
  "test",
  "tests",
  "components",
  "pages",
  "hooks",
  "public",
  "config",
  "migrations",
  "web",
  "gui",
  "server",
  "api",
  "modules",
  "core",
  "registry",
])
const EXT = /\.[A-Za-z][A-Za-z0-9]{0,7}$/
const DOMAIN_FIRST = /^[A-Za-z0-9-]+\.(com|org|net|io|dev|ai|app|co|me|sh|gg)$/i

export type PathStyle = "home" | "absolute" | "relative" | "repo" | "remote"

/** A path on this machine or a remote place: the Files module resolves it. */
export const PATH_KIND = "path"
/** A path relative to somewhere unknown (a repository): detected and copied, not resolved. */
export const RELATIVE_PATH_KIND = "path.relative"

function classify(body: string): PathStyle | null {
  const trimmedSlash = body.replace(/\/$/, "")
  const dir = body.endsWith("/")
  const segs = trimmedSlash.split("/")
  const hasExt = EXT.test(segs[segs.length - 1])
  if (segs.some((s) => s === "")) {
    // Only a leading "" (absolute) is legitimate.
    if (!(segs[0] === "" && segs.slice(1).every((s) => s !== ""))) return null
  }
  if (body.startsWith("~")) {
    return body.startsWith("~/") && segs.length >= 2 ? "home" : null
  }
  if (body.startsWith("/")) {
    const parts = segs.slice(1)
    if (parts.length < 2) return null
    return ABS_ROOTS.has(parts[0]) || hasExt ? "absolute" : null
  }
  if (body.startsWith("./") || body.startsWith("../")) {
    const parts = segs.filter((s) => s !== "." && s !== "..")
    return parts.length >= 1 && (hasExt || dir || parts.length >= 2) ? "relative" : null
  }
  if (DOMAIN_FIRST.test(segs[0])) return null
  if (hasExt && !/^\d+\.\d+/.test(segs[segs.length - 1])) return "repo"
  if (segs.length >= 2 && REPO_DIRS.has(segs[0]) && !/^\d+$/.test(segs[segs.length - 1])) return "repo"
  return null
}

interface PathHit extends RefHit {
  style: PathStyle
}

function scanPaths(text: string): PathHit[] {
  const hits: PathHit[] = []
  for (const tok of matches(TOKEN, text)) {
    if (!tok[0].includes("/")) continue
    const remote = tok[0].startsWith("remote:")
    const m = (remote ? REMOTE : PATH).exec(tok[0])
    if (!m) continue
    const start = tok.index
    const before = text[start - 1]
    if (
      isWordChar(before) ||
      before === "/" ||
      before === "." ||
      before === ":" ||
      before === "@" ||
      before === "~" ||
      before === "-" ||
      before === "#"
    )
      continue
    const end = trimTrailing(text, start, start + m[0].length)
    const raw = text.slice(start, end)
    const loc = /^(.*?)(?::(\d{1,6})(?::(\d{1,4}))?)?$/.exec(raw.slice(remote ? "remote:".length : 0))
    const body = (remote ? "remote:" : "") + (loc ? loc[1] : raw)
    const style = remote ? "remote" : classify(body)
    if (!style) continue
    const meta: Record<string, string | number> = { style }
    if (loc?.[2]) meta.line = Number(loc[2])
    if (loc?.[3]) meta.col = Number(loc[3])
    hits.push({ start, end, canonical: body.replace(/\/$/, "") || body, meta, style })
  }
  return hits
}

const chipLabel: RefKind["chipLabel"] = (ref, raw) => {
  const base = ref.canonical.split("/").pop() || ref.canonical
  const line = ref.meta?.line
  return line !== undefined ? `${base}:${line}` : base || raw
}

/** Two kinds from one scan: paths Files can look up, and relative ones it cannot. */
export function createPathKinds(): RefKind[] {
  const kind = (id: string, label: string, keep: (s: PathStyle) => boolean, priority: number): RefKind => ({
    id,
    label,
    icon: "file",
    priority,
    scan: (text) =>
      scanPaths(text)
        .filter((h) => keep(h.style))
        .map(({ style: _style, ...h }) => h),
    chipLabel,
    open: (ref) => ({ type: "copy", text: ref.canonical }),
  })
  return [
    kind(PATH_KIND, "File path", (s) => s === "home" || s === "absolute" || s === "remote", 40),
    kind(RELATIVE_PATH_KIND, "Relative path", (s) => s === "relative" || s === "repo", 39),
  ]
}
