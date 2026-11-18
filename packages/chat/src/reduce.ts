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
import type { StoredMessage, StreamEvent } from "./wire.ts"

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
export function fromThread(messages: StoredMessage[], skipRun = ""): Pick<ChatState, "ids" | "items"> {
  const ids: string[] = []
  const items: Record<string, ChatItem> = {}
  const add = (it: ChatItem) => {
    ids.push(it.id)
    items[it.id] = it
  }
  for (const m of messages) {
    if (m.role === "user" && typeof m.meta?.text === "string") {
      add(
        m.meta.auto
          ? { kind: "user", id: m.id, text: "", auto: m.meta.auto }
          : { kind: "user", id: m.id, text: m.meta.text },
      )
      continue
    }
    if (skipRun && m.run_id === skipRun) continue
    m.blocks.forEach((b, i) => {
      if (b.type === "text" && b.text) add({ kind: "text", id: `${m.id}:${i}`, text: b.text, streaming: false })
      else if (b.type === "tool_use" && b.id)
        add({ kind: "tool", id: b.id, name: b.name ?? "", input: b.input, status: "done" })
      else if (b.type === "tool_result" && b.tool_use_id) {
        const call = items[b.tool_use_id]
        if (call?.kind === "tool")
          items[call.id] = { ...call, result: b.content ?? "", status: b.is_error ? "error" : "done" }
      }
    })
  }
  return { ids, items }
}

/** A run begins to be followed. */
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
      if (ev.kind === "text") {
        const next = { ...s, parts: { ...s.parts, [ev.part_id]: "text" } }
        return append(next, { kind: "text", id: key(ev.part_id), text: "", streaming: true })
      }
      if (ev.kind === "tool_call") {
        const next = { ...s, parts: { ...s.parts, [ev.part_id]: "tool_call" } }
        return append(next, { kind: "tool", id: key(ev.part_id), name: String(ev.meta?.name ?? ""), status: "running" })
      }
      if (ev.kind === "tool_result") {
        const call = key(String(ev.meta?.call_id ?? ""))
        const next = { ...s, parts: { ...s.parts, [ev.part_id]: `result:${call}` } }
        const failed = ev.meta?.is_error === true
        return patch(next, call, (it) =>
          it.kind === "tool" ? { ...it, result: "", status: failed ? "error" : "done" } : it,
        )
      }
      return s
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
      const what = (ev.kind ?? "").replace(/^[a-z]+\./, "")
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
