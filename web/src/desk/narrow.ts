/**
 * The desk on a narrow screen (a phone, a slim window): below NARROW_PX it shows one window at a
 * time, the focused one, full width, and the bar offers a switcher instead of tiling every window
 * side by side. The saved tree (tiles, tabs, floats) is left as it is: widen the screen and the
 * arrangement is back.
 */
import { useEffect, useState } from "react"
import type { WindowTree } from "@/components/ui/window-manager"

/** Below this width (CSS pixels) the desk shows one window at a time. */
export const NARROW_PX = 720

const query = `(max-width: ${NARROW_PX - 1}px)`

/** Whether the screen is narrow, following resizes and rotations. */
export function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => typeof window !== "undefined" && window.matchMedia?.(query).matches)
  useEffect(() => {
    const mq = window.matchMedia?.(query)
    if (!mq) return
    const on = () => setNarrow(mq.matches)
    on()
    mq.addEventListener("change", on)
    return () => mq.removeEventListener("change", on)
  }, [])
  return Boolean(narrow)
}

/** The narrow desk's projection: only the focused window (else the newest), as one full stack. */
export function narrowProjection(
  wins: { id: string }[],
  focus: string,
): { projectedTree: WindowTree; visibleIds: Set<string>; backgroundIds: string[] } {
  const shown = wins.find((w) => w.id === focus) ?? wins[wins.length - 1]
  if (!shown)
    return { projectedTree: { root: { type: "row", children: [] } }, visibleIds: new Set(), backgroundIds: [] }
  return {
    projectedTree: {
      root: { type: "row", children: [{ type: "stack", id: "stack-narrow", wins: [shown.id], selected: 0 }] },
    },
    visibleIds: new Set([shown.id]),
    backgroundIds: wins.filter((w) => w.id !== shown.id).map((w) => w.id),
  }
}
