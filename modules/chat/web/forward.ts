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
  const res = await fetch(`${SEND}/${encodeURIComponent(seat)}/send`, {
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
