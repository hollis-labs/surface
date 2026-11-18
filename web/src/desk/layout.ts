/**
 * Geometry: a pure function from the desk's state and the room available to rectangles.
 * Tiled windows are placed here (the first is the master on the left, the rest stack on
 * the right); floating windows keep the rectangle the server holds for them.
 */
import type { DeskState, DeskWin } from "./store"

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

export interface Placed extends Rect {
  tiled: boolean
  z: number
}

export const MIN_W = 320
export const MIN_H = 180

const clampRect = (r: Rect, area: Rect): Rect => {
  const w = Math.min(Math.max(r.w, MIN_W), Math.max(MIN_W, area.w))
  const h = Math.min(Math.max(r.h, MIN_H), Math.max(MIN_H, area.h))
  return {
    w,
    h,
    x: Math.min(Math.max(r.x, 0), Math.max(0, area.w - 80)),
    y: Math.min(Math.max(r.y, 0), Math.max(0, area.h - 40)),
  }
}

export function place(s: DeskState, area: Rect, gap: number): Map<string, Placed> {
  const out = new Map<string, Placed>()
  const visible = s.wins.filter((w) => !w.minimized)
  const isTiled = (w: DeskWin) => s.layout === "tile" && !w.floating
  const tiled = visible.filter(isTiled)
  const full: Rect = { x: gap, y: gap, w: area.w - 2 * gap, h: area.h - 2 * gap }

  if (tiled.length === 1) {
    out.set(tiled[0].id, { ...full, tiled: true, z: 1 })
  } else if (tiled.length > 1) {
    const masterW = Math.round((full.w - gap) * s.ratio)
    out.set(tiled[0].id, { x: full.x, y: full.y, w: masterW, h: full.h, tiled: true, z: 1 })
    const rest = tiled.slice(1)
    const h = (full.h - gap * (rest.length - 1)) / rest.length
    rest.forEach((w, i) => {
      out.set(w.id, {
        x: full.x + masterW + gap,
        y: Math.round(full.y + i * (h + gap)),
        w: full.w - masterW - gap,
        h: Math.round(h),
        tiled: true,
        z: 1,
      })
    })
  }
  for (const w of visible) {
    if (isTiled(w)) continue
    out.set(w.id, { ...clampRect(w, area), tiled: false, z: 10 + w.z })
  }
  for (const w of visible) {
    if (w.maximized) out.set(w.id, { ...full, tiled: isTiled(w), z: 5000 })
  }
  return out
}
