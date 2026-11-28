/**
 * Forward to agent: an object action ("Forward to agent ▸ <seat>") on anything the page can
 * name (a cite, a note, a chat message), which hands it to that seat as a turn through the
 * seat host (POST /seats/{seat}/send: the seat's most recently active session, or a new one).
 *
 * The seat list comes from whoever last read the seat host (the chat window, the approvals
 * overlay): the action is registered again whenever it changes.
 */
import { formatObjectRef, type ObjectRef, registerObjectAction } from "@hollis-labs/surface-page/sdk"

export interface SeatName {
  seat: string
  name: string
}

const SEND = "/api/m/chat/h/seats"
/** The app's own seats (Nanite agents in this app) are listed and opened here; their keys in
 * the roster are "local:<id>". */
export const LOCAL_SEATS = "/api/agent/seats"
export const LOCAL_PREFIX = "local:"

/** The app's own seats, in the seat host's shape, keyed "local:<id>" (none when the app has none). */
export async function localSeats<T extends { seat: string }>(): Promise<(T & { local_id: string; local: true })[]> {
  const res = await fetch(LOCAL_SEATS, { credentials: "same-origin" }).catch(() => null)
  if (!res?.ok) return []
  const body = (await res.json().catch(() => ({}))) as { seats?: T[] }
  return (body.seats ?? []).map((s) => ({ ...s, seat: LOCAL_PREFIX + s.seat, local_id: s.seat, local: true as const }))
}
let known = ""

/** Sets the seats Forward offers. Cheap to call on every poll: it re-registers only on change. */
export function setForwardSeats(seats: SeatName[]) {
  const key = seats.map((s) => `${s.seat}=${s.name}`).join("|")
  if (key === known) return
  known = key
  registerObjectAction({
    id: "chat.forward-seat",
    label: "Forward to agent",
    icon: "send",
    group: "send",
    order: 5,
    children: seats.map((s) => ({
      id: `chat.forward-seat.${s.seat}`,
      label: s.name,
      icon: "bot",
      run: async (obj, ctx) => {
        try {
          await sendToSeat(s.seat, forwardText(obj))
          ctx.notify(`Sent to ${s.name}`)
        } catch (err) {
          ctx.notify(err instanceof Error ? err.message : `Could not send to ${s.name}`)
        }
      },
    })),
  })
}

/** What the seat is told: what the thing is, its reference, and its text when there is one. */
export function forwardText(obj: ObjectRef): string {
  const ref = formatObjectRef(obj)
  const lines = [`[Forwarded by the owner: ${obj.title || obj.kind}]`, ref]
  if (obj.subtitle) lines.push(obj.subtitle)
  if (obj.url) lines.push(obj.url)
  if (obj.body?.trim()) lines.push("", obj.body.trim())
  return lines.join("\n")
}

export async function sendToSeat(seat: string, text: string, session = ""): Promise<{ session: string }> {
  const url = seat.startsWith(LOCAL_PREFIX)
    ? `${LOCAL_SEATS}/${encodeURIComponent(seat.slice(LOCAL_PREFIX.length))}/send`
    : `${SEND}/${encodeURIComponent(seat)}/send`
  const res = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text, ...(session ? { session } : {}) }),
  })
  if (res.status === 409) throw new Error("The agent is busy with a turn; forward again when it finishes.")
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string }
    throw new Error(body.message || `The seat host answered ${res.status}.`)
  }
  return (await res.json()) as { session: string }
}
