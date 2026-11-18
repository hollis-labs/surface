/**
 * Anchors: the parts of a window a view names, so the agent can point at them ("the address
 * bar", "the marks"). A view puts `ref={useAnchor(win, "address", "The address bar")}` on an
 * element; the page tells the server what each window declares, and draws the agent's
 * highlight, spotlight or arrow at the element.
 */
import { useCallback } from "react"
import { call } from "./runtime"
import type { Win } from "./types"

interface Entry {
  el: HTMLElement
  label?: string
}

const byWindow = new Map<string, Map<string, Entry>>()
const pending = new Map<string, ReturnType<typeof setTimeout>>()

/** Tell the server what a window declares, once things settle. */
function report(win: string) {
  clearTimeout(pending.get(win))
  pending.set(
    win,
    setTimeout(() => {
      pending.delete(win)
      const list = [...(byWindow.get(win)?.entries() ?? [])].map(([name, e]) => ({ name, label: e.label }))
      void call("PUT", `/api/ade/anchors/${encodeURIComponent(win)}`, list).catch(() => {})
    }, 150),
  )
}

/**
 * Name an element of this window so the agent can point at it. Returns a ref for the
 * element; the anchor goes when the element does.
 */
export function useAnchor(win: Pick<Win, "id"> | string, name: string, label?: string) {
  const id = typeof win === "string" ? win : win.id
  return useCallback(
    (el: HTMLElement | null) => {
      if (!el) return
      let m = byWindow.get(id)
      if (!m) {
        m = new Map()
        byWindow.set(id, m)
      }
      m.set(name, { el, label })
      report(id)
      return () => {
        const now = byWindow.get(id)
        if (now?.get(name)?.el !== el) return
        now.delete(name)
        if (!now.size) byWindow.delete(id)
        report(id)
      }
    },
    [id, name, label],
  )
}

/** The element a window's anchor names, if it is on the page. The page's own, for drawing. */
export const anchorElement = (win: string, name: string): HTMLElement | null => byWindow.get(win)?.get(name)?.el ?? null
