/**
 * References in terminal output: a task id an agent printed is a link. Hovering shows the
 * reference's card beside the pointer; activating it (a click) opens the record. Only kinds
 * with a record behind them are linked: web addresses are the terminal's own web links, and
 * paths would underline every listing.
 *
 * Ported from Basalisk (frontend/src/cite/terminal.ts). The card is one floating layer for
 * the whole page (CiteFloat, mounted at the root), since a terminal's text is drawn on a
 * canvas and has no element to hang a hover card on.
 */
import type { ILink, ILinkProvider, Terminal } from "@xterm/xterm"
import { useSyncExternalStore } from "react"
import type { RefIdentity } from "./core/types"
import { canResolve, openCite, requestRef, segments } from "./store"
import { RefCard } from "./ui"

interface Float {
  ref: RefIdentity
  x: number
  y: number
}

let current: Float | null = null
let hideTimer = 0
const listeners = new Set<() => void>()
const emit = () => {
  for (const l of listeners) l()
}

function show(ref: RefIdentity, x: number, y: number) {
  window.clearTimeout(hideTimer)
  requestRef(ref)
  current = { ref, x, y }
  emit()
}

/** Hides the card after a moment, so the pointer can move onto it. */
function hideSoon() {
  window.clearTimeout(hideTimer)
  hideTimer = window.setTimeout(() => {
    current = null
    emit()
  }, 250)
}

const unlinked = new Set(["url", "path"])

/** The link provider the terminal takes (registry Terminal, linkProviders). */
export function citeTerminalLinks(term: Terminal): ILinkProvider {
  return {
    provideLinks(y, callback) {
      const line = term.buffer.active.getLine(y - 1)
      if (!line) {
        callback(undefined)
        return
      }
      const links: ILink[] = []
      for (const s of segments(line.translateToString(true))) {
        if (s.type !== "ref" || unlinked.has(s.kind) || !canResolve(s.kind)) continue
        const ref = { kind: s.kind, canonical: s.canonical, meta: s.meta }
        links.push({
          range: { start: { x: s.start + 1, y }, end: { x: s.end, y } },
          text: s.raw,
          activate: () => openCite(ref),
          hover: (e: MouseEvent) => show(ref, e.clientX, e.clientY),
          leave: hideSoon,
        })
      }
      callback(links.length ? links : undefined)
    },
  }
}

/** The floating card for references hovered in a terminal. Mounted once, at the page root. */
export function CiteFloat() {
  const f = useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => current,
  )
  if (!f) return null
  // Below the pointer, kept inside the window.
  const left = Math.max(8, Math.min(f.x - 12, window.innerWidth - 336))
  const below = f.y + 16 + 260 < window.innerHeight
  return (
    <div
      role="dialog"
      aria-label="Reference"
      className="fixed z-50 w-80 rounded-lg border bg-popover p-3 text-popover-foreground shadow-md"
      style={below ? { left, top: f.y + 14 } : { left, bottom: window.innerHeight - f.y + 14 }}
      onMouseEnter={() => window.clearTimeout(hideTimer)}
      onMouseLeave={hideSoon}
    >
      <RefCard refId={f.ref} />
    </div>
  )
}
