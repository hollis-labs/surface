/**
 * A script's demo, drawn (docs/scripts.md): on each script.step, a labelled agent cursor glides to
 * what the step points at, and when the step asks for zoom the desk scales toward it for the step's
 * hold. What the step says is the guide's caption (AdeGuide); this draws only the cursor. Nothing is
 * drawn outside a demo; script.done takes it away.
 */
import { useEffect, useState } from "react"
import { Icon } from "../sdk"
import { anchorElement } from "../sdk/anchors"
import { onEvent } from "../sdk/runtime"
import { deskState } from "./store"

interface Point {
  window?: string
  view?: string
  anchor?: string
  label?: string
}
interface Step {
  script?: string
  index?: number
  of?: number
  say?: string
  point?: Point[]
  zoom?: boolean
  hold_ms?: number
}

/** The element a point names: an anchor in a window, else the window; a view means its first window. */
function elementOf(p: Point): HTMLElement | null {
  const win = p.window ?? deskState().wins.find((w) => w.view === p.view)?.id
  if (!win) return null
  if (p.anchor) {
    const a = anchorElement(win, p.anchor)
    if (a) return a as HTMLElement
  }
  return document.querySelector<HTMLElement>(`[data-window="${CSS.escape(win)}"]`)
}

export function GhostCursor() {
  const [at, setAt] = useState<{ x: number; y: number; label: string; say: string } | null>(null)
  useEffect(() => {
    const desk = () => document.querySelector<HTMLElement>("[data-slot=app-frame-content]")
    let unzoom = 0
    const reset = () => {
      const d = desk()
      if (d) {
        d.style.transform = ""
        d.style.transformOrigin = ""
      }
    }
    const offStep = onEvent("script.step", (ev) => {
      const s = (ev.data ?? {}) as Step
      const p = s.point?.[0]
      const el = p ? elementOf(p) : null
      const r = el?.getBoundingClientRect()
      if (!r?.width) {
        setAt((cur) => (cur ? { ...cur, say: s.say ?? "" } : null))
        return
      }
      const x = r.x + r.width / 2
      const y = r.y + Math.min(r.height / 2, 120)
      setAt({ x, y, label: p?.label ?? "agent", say: s.say ?? "" })
      window.clearTimeout(unzoom)
      const d = desk()
      if (d && s.zoom) {
        d.style.transition = "transform 600ms ease"
        d.style.transformOrigin = `${x}px ${y}px`
        d.style.transform = "scale(1.25)"
        unzoom = window.setTimeout(reset, Math.max(1500, (s.hold_ms ?? 4000) - 400))
      } else reset()
    })
    const offDone = onEvent("script.done", () => {
      window.clearTimeout(unzoom)
      reset()
      setAt(null)
    })
    return () => {
      offStep()
      offDone()
      window.clearTimeout(unzoom)
      reset()
    }
  }, [])
  if (!at) return null
  return (
    <div
      data-slot="ghost-cursor"
      aria-hidden
      className="pointer-events-none fixed z-50 transition-[left,top] duration-700 ease-out"
      style={{ left: at.x, top: at.y }}
    >
      <span className="block text-primary drop-shadow-[0_0_6px_var(--p-accent)]">
        <Icon name="mouse-pointer-2" size={22} />
      </span>
      <span className="ml-4 inline-block rounded-md bg-primary px-1.5 py-0.5 font-mono text-[11px] text-primary-foreground">
        {at.label}
      </span>
    </div>
  )
}
