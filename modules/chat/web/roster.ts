/**
 * The roster's order: pinned seats first, in the owner's order; then seats that answered
 * recently, most recent first; then the rest by name. A seat drops back to its place once it
 * has been quiet for the "recently active" window.
 */

export interface RosterSeat {
  seat: string
  name: string
  sessions: { last_event_at?: string; state: string }[]
}

/** When the seat last did anything, as epoch ms (0: never seen). A running seat counts as now. */
export function lastActive(s: RosterSeat, now: number): number {
  let at = 0
  for (const x of s.sessions) {
    if (x.state === "running" || x.state === "waiting") return now
    const t = x.last_event_at ? Date.parse(x.last_event_at) : 0
    if (Number.isFinite(t) && t > at) at = t
  }
  return at
}

export interface Ordered<T> {
  pinned: T[]
  rest: T[]
  /** Seats in `rest` that are there because they were recently active. */
  recent: Set<string>
}

export function orderRoster<T extends RosterSeat>(
  seats: T[],
  pins: string[],
  recentMs: number,
  now: number,
): Ordered<T> {
  const bySeat = new Map(seats.map((s) => [s.seat, s]))
  const pinned = pins.map((p) => bySeat.get(p)).filter((s): s is T => !!s)
  const pinnedIds = new Set(pinned.map((s) => s.seat))
  const others = seats.filter((s) => !pinnedIds.has(s.seat))
  const active = others
    .map((s) => ({ s, at: lastActive(s, now) }))
    .filter((x) => x.at > 0 && now - x.at <= recentMs)
    .sort((a, b) => b.at - a.at)
  const recent = new Set(active.map((x) => x.s.seat))
  const alpha = others.filter((s) => !recent.has(s.seat)).sort((a, b) => a.name.localeCompare(b.name))
  return { pinned, rest: [...active.map((x) => x.s), ...alpha], recent }
}

export const parsePins = (csv: string | undefined): string[] =>
  (csv ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)

/** Moves `seat` to just before `before` (or to the end) within the pins. */
export function movePin(pins: string[], seat: string, before: string | null): string[] {
  const without = pins.filter((p) => p !== seat)
  const at = before ? without.indexOf(before) : -1
  if (at < 0) return [...without, seat]
  return [...without.slice(0, at), seat, ...without.slice(at)]
}
