/** Shortcuts written like "ctrl+alt+t" or "cmd+k", matched against keyboard events. */

const ALIASES: Record<string, string> = {
  return: "enter",
  esc: "escape",
  space: " ",
  left: "arrowleft",
  right: "arrowright",
  up: "arrowup",
  down: "arrowdown",
}

export interface Chord {
  key: string
  ctrl: boolean
  alt: boolean
  shift: boolean
  meta: boolean
}

export function parseKeys(keys: string): Chord | null {
  const parts = keys
    .toLowerCase()
    .split("+")
    .map((p) => p.trim())
    .filter(Boolean)
  if (!parts.length) return null
  const key = parts[parts.length - 1]
  const mods = new Set(parts.slice(0, -1))
  return {
    key: ALIASES[key] ?? key,
    ctrl: mods.has("ctrl") || mods.has("control"),
    alt: mods.has("alt") || mods.has("option"),
    shift: mods.has("shift"),
    meta: mods.has("cmd") || mods.has("meta") || mods.has("super"),
  }
}

export function matches(c: Chord, e: KeyboardEvent): boolean {
  if (e.ctrlKey !== c.ctrl || e.altKey !== c.alt || e.shiftKey !== c.shift || e.metaKey !== c.meta) return false
  const key = e.key.toLowerCase()
  if (key === c.key) return true
  // With alt held, macOS reports the composed character; fall back on the physical key.
  const code = e.code.toLowerCase()
  if (code === `key${c.key}` || code === `digit${c.key}`) return true
  if (c.key === "`" && code === "backquote") return true
  return false
}

const isMac = typeof navigator !== "undefined" && /mac/i.test(navigator.platform)

/** A shortcut as a person reads it: "⌃⌥T". */
export function showKeys(keys?: string): string {
  if (!keys) return ""
  const c = parseKeys(keys)
  if (!c) return ""
  const out: string[] = []
  if (c.ctrl) out.push(isMac ? "⌃" : "Ctrl+")
  if (c.alt) out.push(isMac ? "⌥" : "Alt+")
  if (c.shift) out.push(isMac ? "⇧" : "Shift+")
  if (c.meta) out.push(isMac ? "⌘" : "Win+")
  const names: Record<string, string> = {
    enter: "↵",
    escape: "Esc",
    arrowleft: "←",
    arrowright: "→",
    arrowup: "↑",
    arrowdown: "↓",
    " ": "Space",
  }
  out.push(names[c.key] ?? c.key.toUpperCase())
  return out.join("")
}
