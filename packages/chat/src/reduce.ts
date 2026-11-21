/**
 * The conversation as data, and how each chatstream/v1 event changes it. Pure functions: no
 * network, no timers, no framework.
 *
 * The shape is what keeps drawing cheap. `ids` is the order of the items and `items` holds
 * each one by id. A change to one item replaces that item's object and the `items` record,
 * and nothing else: `ids` keeps its identity unless an item is added or removed, and every
 * other item keeps its identity. So a view that draws the list from `ids` and each row from
 * its own item redraws only the row that changed: a streamed token redraws the last message.
 */
import type { StreamEvent } from "./stream.ts"

export type ChatItem =
  | { kind: "user"; id: string; text: string; auto?: string }
  | { kind: "text"; id: string; text: string; streaming: boolean }
  | {
      kind: "tool"
      id: string
      name: string
      input?: unknown
      result?: string
      status: "running" | "done" | "error"
    }
  | { kind: "plan"; id: string; planID: string; text: string; disposition: string }
  | { kind: "notice"; id: string; text: string; tone: "error" | "quiet" }

export interface Approval {
  run: string
  id: string
  tool: string
  input: unknown
  description?: string
  /** "This session" and "Always" are on offer (not for a tool that asks every time). */
  standing: boolean
}

/**
 * A turn the agent's previous process left unfinished, offered to the person (Nanite's
 * nanite.interrupted activity). `actions` is what is on offer: resume, discard, reset. With
 * `integrity` "blocked" the conversation failed its integrity checks: resume is never on
 * offer, and reset moves the chat to a new conversation (the old one is kept). `turn` is ""
 * when no turn is left, only a conversation to reset.
 */
export interface InterruptedOffer {
  turn: string
  actions: readonly string[]
  integrity: string
  /** Why, when the host says (beyond `integrity`). */
  reason: string
}

export interface ChatState {
  /** Whether the host has an agent and a model, once asked. */
  host: { available: boolean; ready: boolean; model: string } | null
  /** The thread has been read (or could not be). */
  loaded: boolean
  ids: readonly string[]
  items: Readonly<Record<string, ChatItem>>
  /** The run being followed, when there is one. */
  run: string | null
  /** The followed run's parts: part id → "text", "tool_call" or "result:<item id>". */
  parts: Readonly<Record<string, string>>
  /** A question waiting for the person. */
  approval: Approval | null
  /** An interrupted turn on offer; it outlives the run that announced it. */
  interrupted?: InterruptedOffer | null
  error: string
  /** On the conversation wire: the last event taken, so a reconnect resumes after it. */
  cursor?: number
  /** On the conversation wire: the attachment that may type now, or null when nobody holds it. */
  holder?: string | null
  /** On the conversation wire: rises when the agent's side was replaced. */
  generation?: number
}

export const initialState: ChatState = {
  host: null,
  loaded: false,
  ids: [],
  items: {},
  run: null,
  parts: {},
  approval: null,
  error: "",
}

/** Add an item at the end (or replace one with the same id in place). */
export function append(s: ChatState, item: ChatItem): ChatState {
  const ids = item.id in s.items ? s.ids : [...s.ids, item.id]
  return { ...s, ids, items: { ...s.items, [item.id]: item } }
}

/** Change one item. Nothing else changes identity; an unknown id changes nothing. */
export function patch(s: ChatState, id: string, change: (it: ChatItem) => ChatItem): ChatState {
  const it = s.items[id]
  if (!it) return s
  const next = change(it)
  return next === it ? s : { ...s, items: { ...s.items, [id]: next } }
}

export function remove(s: ChatState, id: string): ChatState {
  if (!(id in s.items)) return s
  const { [id]: _gone, ...items } = s.items
  return { ...s, ids: s.ids.filter((i) => i !== id), items }
}

/** The stored thread as items. The run still going is left out: its stream rebuilds it. */
export function startRun(s: ChatState, run: string): ChatState {
  return { ...s, run, parts: {}, approval: null }
}

/** The followed run is over: nothing in it is still running or streaming. */
export function endRun(s: ChatState): ChatState {
  let next: ChatState = { ...s, run: null, parts: {}, approval: null }
  for (const id of s.ids) {
    const it = s.items[id]
    if (it?.kind === "tool" && it.status === "running") next = patch(next, id, () => ({ ...it, status: "done" }))
    else if (it?.kind === "text" && it.streaming) next = patch(next, id, () => ({ ...it, streaming: false }))
  }
  return next
}

const note = (s: ChatState, id: string, text: string, tone: "error" | "quiet"): ChatState =>
  append(s, { kind: "notice", id, text, tone })

/** One event of the followed run. Events of any other run change nothing. */
export function reduceEvent(s: ChatState, ev: StreamEvent): ChatState {
  const run = s.run
  if (!run || (ev.run_id && ev.run_id !== run)) return s
  // An item's id carries the run, so two runs' part ids never collide.
  const key = (part: string) => `${run}:${part}`
  switch (ev.verb) {
    case "part.start": {
      if (!ev.part_id) return s
      // A run that answers (a resumed turn, a new one) means the offer was taken up.
      const base = s.interrupted ? { ...s, interrupted: null } : s
      if (ev.kind === "text") {
        const next = { ...base, parts: { ...base.parts, [ev.part_id]: "text" } }
        return append(next, { kind: "text", id: key(ev.part_id), text: "", streaming: true })
      }
      if (ev.kind === "tool_call") {
        const next = { ...base, parts: { ...base.parts, [ev.part_id]: "tool_call" } }
        return append(next, { kind: "tool", id: key(ev.part_id), name: String(ev.meta?.name ?? ""), status: "running" })
      }
      if (ev.kind === "tool_result") {
        const call = key(String(ev.meta?.call_id ?? ""))
        const next = { ...base, parts: { ...base.parts, [ev.part_id]: `result:${call}` } }
        const failed = ev.meta?.is_error === true
        return patch(next, call, (it) =>
          it.kind === "tool" ? { ...it, result: "", status: failed ? "error" : "done" } : it,
        )
      }
      return base
    }
    case "part.delta": {
      if (!ev.part_id || !ev.text) return s
      const kind = s.parts[ev.part_id] ?? ""
      const add = ev.text
      if (kind === "text")
        return patch(s, key(ev.part_id), (it) => (it.kind === "text" ? { ...it, text: it.text + add } : it))
      if (kind.startsWith("result:"))
        return patch(s, kind.slice(7), (it) => (it.kind === "tool" ? { ...it, result: (it.result ?? "") + add } : it))
      return s
    }
    case "part.end": {
      if (!ev.part_id) return s
      const kind = s.parts[ev.part_id]
      if (kind === "text")
        return patch(s, key(ev.part_id), (it) => (it.kind === "text" ? { ...it, streaming: false } : it))
      if (kind === "tool_call")
        return patch(s, key(ev.part_id), (it) => (it.kind === "tool" ? { ...it, input: ev.final } : it))
      return s
    }
    case "approval.request":
      if (!ev.approval_id || !ev.descriptor) return s
      return {
        ...s,
        approval: {
          run,
          id: ev.approval_id,
          tool: ev.descriptor.tool,
          input: ev.descriptor.input,
          description: ev.descriptor.description,
          standing: ev.descriptor.standing !== false,
        },
      }
    case "activity": {
      if (ev.kind === "nanite.interrupted") return interrupted(s, ev.value ?? {}, key)
      if (ev.kind === "nanite.chat_reset")
        return note(
          { ...s, interrupted: null },
          key("chat_reset"),
          "The agent starts a new history here. The earlier one is kept on disk; this conversation is unchanged.",
          "quiet",
        )
      if (ev.kind === "nanite.desk.plan" && typeof ev.value?.id === "string" && typeof ev.value.card === "string")
        return append(s, {
          kind: "plan",
          id: key(`plan:${ev.value.id}`),
          planID: ev.value.id,
          text: ev.value.card,
          disposition: String(ev.value.disposition ?? ""),
        })
      if (ev.kind === "nanite.notice" && ev.value?.kind === "desk" && typeof ev.value.body === "string")
        return note(s, key(`delivery:${String(ev.value.id ?? ev.seq)}`), ev.value.body, "quiet")
      const what = (ev.kind ?? "").replace(/^[a-z]+\./, "")
      if (ev.kind === "nanite.notice" && ev.value?.kind === "budget" && typeof ev.value.body === "string")
        return note(s, key(`budget:${String(ev.value.id ?? "warning")}`), ev.value.body, "quiet")
      if (what === "approval" && s.approval?.id === ev.value?.approval_id) return { ...s, approval: null }
      if (what === "compact" && typeof ev.value?.state === "string")
        return note(
          s,
          key("compact"),
          ev.value.state === "start"
            ? "Summarising the older part of this conversation for the model…"
            : ev.value.state === "done"
              ? "The older part of this conversation was summarised for the model. Every message is still here."
              : "The conversation could not be shortened this time.",
          "quiet",
        )
      return s
    }
    case "run.error":
      return note(s, key("error"), ev.message || "The agent could not finish.", "error")
    case "run.abort":
      return note(s, key("abort"), "Stopped.", "quiet")
    case "run.finish":
      if (ev.reason === "turn_limit" || ev.reason === "length")
        return note(
          s,
          key("limit"),
          ev.reason === "length"
            ? "The answer was cut off at the length limit."
            : "Stopped after many steps. Say “continue” to go on.",
          "quiet",
        )
      return s
    default:
      return s
  }
}

const DISCARDED: Record<string, string> = {
  "new message": "The interrupted turn was set aside for the new message.",
  timeout: "The interrupted turn was set aside: nobody picked it up in time.",
  requested: "The interrupted turn was discarded.",
}

/** An interrupted-turn activity: an offer, or what became of the turn. */
function interrupted(s: ChatState, value: Record<string, unknown>, key: (part: string) => string): ChatState {
  const str = (v: unknown) => (typeof v === "string" ? v : "")
  if (Array.isArray(value.actions)) {
    const integrity = str(value.integrity)
    // A blocked conversation is never resumed, whatever the offer says.
    const actions = value.actions.filter(
      (a): a is string => typeof a === "string" && !(integrity === "blocked" && a === "resume"),
    )
    if (!actions.length) return s
    return { ...s, interrupted: { turn: str(value.turn), actions, integrity, reason: str(value.reason) } }
  }
  if (value.disposition === "discarded") {
    const turn = str(value.turn)
    const next = !s.interrupted || s.interrupted.turn === turn ? { ...s, interrupted: null } : s
    return note(next, key(`discarded:${turn}`), DISCARDED[str(value.reason)] ?? DISCARDED.requested, "quiet")
  }
  return s
}
