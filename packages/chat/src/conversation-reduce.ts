/**
 * The conversation stream as items. One stream carries every run of a conversation, the
 * user's own messages included, so the same reducer draws a first paint from history, a
 * live answer, another tab's turn, and a reconnect. It is pure: events in, state out.
 *
 * It builds on the run reducer (reduce.ts) and adds what a conversation needs: a run starts
 * when its run.start arrives, the user's message comes from the stream (and takes the place
 * of the one the sending page already drew, by request id), a finished part carries its
 * whole content in `final` (so a client that takes no deltas draws the same thing), and the
 * cursor, the holder of the input and the generation are kept.
 */
import { append, type ChatItem, type ChatState, endRun, patch, reduceEvent, remove, startRun } from "./reduce.ts"
import type { StreamEvent } from "./stream.ts"

const TERMINAL = new Set(["run.finish", "run.error", "run.abort"])
/** In `parts`: the item the user's message of this run is, while that message is open. */
const USER = "@user"

/** The id of the item for a message the user sent with this request id. */
export const userItemId = (requestId: string) => `user:${requestId}`

const record = (v: unknown): Record<string, unknown> =>
  v !== null && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {}

/** A finished part's text: `final` is `{text}` for text parts and `{text, is_error}` for a tool's result. */
function finalText(final: unknown): string | undefined {
  const text = record(final).text
  return typeof text === "string" ? text : undefined
}

/** The label of a message the app wrote and nobody typed, from the turn's meta. */
function autoLabel(meta: Record<string, unknown> | undefined): string | undefined {
  const own = record(meta)
  const auto = own.auto ?? record(own.meta).auto
  if (typeof auto === "string" && auto) return auto
  return auto === true ? "automatic" : undefined
}

/** One event of the conversation, in order. An event at or before the cursor changes nothing. */
export function reduceConversation(state: ChatState, ev: StreamEvent): ChatState {
  if (typeof ev.seq === "number" && ev.seq > 0) {
    if (ev.seq <= (state.cursor ?? 0)) return state
    state = { ...state, cursor: ev.seq }
  }
  const s = state

  // Conversation-level events carry no run.
  if (ev.verb === "activity" && ev.kind?.startsWith("chat.")) {
    const value = record(ev.value)
    if (ev.kind === "chat.lease") {
      // {state: "acquired" | "released", attachment, previous?}; a renewal is not published.
      const holder = value.state === "released" ? null : value.attachment
      return { ...s, holder: typeof holder === "string" && holder ? holder : null }
    }
    if (ev.kind === "chat.generation" && typeof value.generation === "number") {
      return { ...s, generation: value.generation, holder: null }
    }
    if (ev.kind !== "chat.approval") return s
  }
  if (ev.verb === "gap") {
    // What was being drawn of the run in progress cannot be trusted: drop it and say so.
    let next = s
    for (const id of s.ids) {
      const it = s.items[id]
      if ((it?.kind === "text" && it.streaming) || (it?.kind === "tool" && it.status === "running"))
        next = remove(next, id)
    }
    return append(
      { ...next, parts: {} },
      {
        kind: "notice",
        // A gap carries no cursor of its own: it is named by the last event taken.
        id: `gap:${s.cursor ?? 0}`,
        text: "Part of this conversation could not be replayed.",
        tone: "quiet",
      },
    )
  }
  if (!ev.run_id) return s

  // A run begins with run.start; one joined part-way is picked up at its next event.
  let cur = s
  if (ev.verb === "run.start" || (cur.run !== ev.run_id && !TERMINAL.has(ev.verb))) {
    if (cur.run && cur.run !== ev.run_id) cur = endRun(cur)
    cur = { ...startRun(cur, ev.run_id), error: "" }
    if (ev.verb === "run.start") return cur
  }
  if (cur.run !== ev.run_id) return cur
  const run = ev.run_id
  const key = (part: string) => `${run}:${part}`

  switch (ev.verb) {
    case "message.start": {
      if (ev.role !== "user") {
        if (!(USER in cur.parts)) return cur
        const { [USER]: _open, ...parts } = cur.parts
        return { ...cur, parts }
      }
      const meta = record(ev.meta)
      const request = typeof meta.request_id === "string" ? meta.request_id : ""
      const id = request ? userItemId(request) : key(`user:${ev.message_id ?? ""}`)
      const auto = autoLabel(meta)
      const had = cur.items[id]
      // The page that sent it already shows it: the item stays, in its place.
      const item: ChatItem =
        had?.kind === "user" ? had : auto ? { kind: "user", id, text: "", auto } : { kind: "user", id, text: "" }
      return append({ ...cur, parts: { ...cur.parts, [USER]: id } }, item)
    }
    case "message.end": {
      if (!(USER in cur.parts)) return cur
      const { [USER]: _open, ...parts } = cur.parts
      return { ...cur, parts }
    }
    case "part.start": {
      const user = cur.parts[USER]
      if (user && ev.part_id && ev.kind === "text") {
        return { ...cur, parts: { ...cur.parts, [ev.part_id]: `user:${user}` } }
      }
      return reduceEvent(cur, ev)
    }
    case "part.delta": {
      const kind = ev.part_id ? cur.parts[ev.part_id] : undefined
      if (kind?.startsWith("user:") && ev.text) {
        const add = ev.text
        return patch(cur, kind.slice(5), (it) => (it.kind === "user" && !it.auto ? { ...it, text: it.text + add } : it))
      }
      return reduceEvent(cur, ev)
    }
    case "part.end": {
      const kind = ev.part_id ? cur.parts[ev.part_id] : undefined
      if (!ev.part_id || !kind) return cur
      const text = finalText(ev.final)
      if (kind.startsWith("user:")) {
        if (text === undefined) return cur
        return patch(cur, kind.slice(5), (it) =>
          it.kind === "user" && !it.auto && it.text !== text ? { ...it, text } : it,
        )
      }
      if (kind === "text") {
        return patch(cur, key(ev.part_id), (it) =>
          it.kind === "text" ? { ...it, text: text ?? it.text, streaming: false } : it,
        )
      }
      if (kind.startsWith("result:")) {
        const failed = record(ev.final).is_error === true
        return patch(cur, kind.slice(7), (it) =>
          it.kind === "tool" ? { ...it, result: text ?? it.result ?? "", status: failed ? "error" : it.status } : it,
        )
      }
      return reduceEvent(cur, ev)
    }
    case "run.finish":
    case "run.error":
    case "run.abort":
      return endRun(reduceEvent(cur, ev))
    default:
      return reduceEvent(cur, ev)
  }
}
