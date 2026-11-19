/**
 * What waits for the owner (docs/attention.md): the server's one list, read once and kept
 * current from attend.changed. The bar draws its count; the jump key brings the first item in
 * front.
 */
import { useSyncExternalStore } from "react"
import { call, onEvent } from "../sdk/runtime"

export interface AttendItem {
  subject: string
  state: "needs_you" | "failed" | "unseen"
  title?: string
  reason?: string
  window?: string
  card?: string
  by?: string
  since: string
}
export interface Attention {
  items: AttendItem[]
  counts: Record<string, number>
}

let state: Attention = { items: [], counts: {} }
const subs = new Set<() => void>()
const set = (a: Attention | undefined) => {
  state = { items: a?.items ?? [], counts: a?.counts ?? {} }
  for (const fn of subs) fn()
}

let started = false
function start() {
  if (started) return
  started = true
  void call<Attention>("GET", "/api/attend")
    .then(set)
    .catch(() => {})
  onEvent("attend.changed", (ev) => set(ev.data as Attention))
}

export function useAttention(): Attention {
  start()
  return useSyncExternalStore(
    (fn) => {
      subs.add(fn)
      return () => subs.delete(fn)
    },
    () => state,
  )
}

/** The first thing that waits: what needs an answer first, then failures, then unseen windows. */
export const firstItem = (a: Attention) => a.items[0]
