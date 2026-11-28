/**
 * Test support: a host that speaks the conversation wire in memory, by the rules of the
 * wire document: one event log per conversation, attachments that a generation bump
 * invalidates, history sent without deltas, a committed-only mode, a reset that closes the
 * old conversation. `run` scripts what the "agent" says for a turn.
 */
import type { StreamEvent } from "../src/stream.ts"

type Ev = Omit<StreamEvent, "seq"> & { seq?: number }
interface Sub {
  attachment: string
  committed: boolean
  push: (ev: StreamEvent) => void
  close: () => void
}
interface Conv {
  id: string
  log: StreamEvent[]
  generation: number
  attachments: Map<string, { role: string; stream: string; generation: number }>
  holder: string | null
  activeRun: string | null
  closedFor: string | null
  subs: Set<Sub>
  finished: Set<string>
}

export interface FakeHost {
  fetch: typeof fetch
  /** What the agent says for the next turns: called with the run id and the text. */
  script: (fn: (run: string, text: string) => Ev[]) => void
  /** Publish events as the agent would, now (a run in progress). */
  publish: (events: Ev[]) => void
  /** Replace the agent's side: every attachment and the lease are void. */
  bump: () => void
  /** Refuse the next submit the way a host hook would. */
  refuseNext: (code: string, message: string) => void
  /** Drop every open event stream, as a network failure would. */
  lapse: () => void
  dropStreams: () => void
  requests: { method: string; path: string; body: unknown; headers: Record<string, string> }[]
  conversation: () => Conv
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })

export function fakeHost(): FakeHost {
  let n = 0
  const id = (p: string) => `${p}${++n}`
  const convs = new Map<string, Conv>()
  let current = ""
  let scripted: (run: string, text: string) => Ev[] = () => []
  let refusal: { code: string; message: string } | null = null
  const requests: FakeHost["requests"] = []

  const make = (): Conv => {
    const c: Conv = {
      id: id("conv-"),
      log: [],
      generation: 1,
      attachments: new Map(),
      holder: null,
      activeRun: null,
      closedFor: null,
      subs: new Set(),
      finished: new Set(),
    }
    convs.set(c.id, c)
    current = c.id
    return c
  }
  const publishTo = (c: Conv, events: Ev[]) => {
    for (const e of events) {
      const ev = { v: "chatstream/1", ...e, seq: c.log.length + 1 } as StreamEvent
      c.log.push(ev)
      if (ev.verb === "run.start" && ev.run_id) c.activeRun = ev.run_id
      if (["run.finish", "run.error", "run.abort"].includes(ev.verb) && ev.run_id) {
        c.finished.add(ev.run_id)
        if (c.activeRun === ev.run_id) c.activeRun = null
      }
      for (const s of [...c.subs]) if (!(s.committed && ev.verb === "part.delta")) s.push(ev)
    }
  }
  const stale = (c: Conv) => json(409, { code: "stale_attachment", message: "attach again", generation: c.generation })
  const closed = (c: Conv) =>
    json(410, { code: "conversation_closed", message: "the conversation was reset", conversation_id: c.closedFor })

  const doFetch = async (input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
    const url = new URL(String(input), "http://host")
    const method = init.method ?? "GET"
    const body = init.body ? JSON.parse(String(init.body)) : undefined
    const headers = (init.headers ?? {}) as Record<string, string>
    const path = url.pathname.replace(/^\/api\/agent/, "")
    requests.push({ method, path: path + url.search, body, headers })
    const seg = path.split("/").filter(Boolean)

    if (seg[0] === "agents" && seg[2] === "conversation") {
      if (seg[3] === "reset") {
        const old = convs.get(current)
        const next = make()
        if (old) {
          // The old stream's last event names the successor; later requests get 410.
          publishTo(old, [
            {
              verb: "activity",
              run_id: "",
              kind: "chat.closed",
              value: { code: "conversation_closed", conversation_id: next.id },
            },
          ])
          old.closedFor = next.id
          for (const s of [...old.subs]) s.close()
        }
        return json(200, { conversation_id: next.id, head: 0, generation: 1 })
      }
      const c = convs.get(current) ?? make()
      return json(200, { conversation_id: c.id, head: c.log.length, active_run: c.activeRun, generation: c.generation })
    }
    const c = convs.get(seg[1] ?? "")
    if (seg[0] !== "conversations" || !c) return json(404, { code: "not_found", message: "no such conversation" })
    if (c.closedFor) return closed(c)
    const known = (a: unknown) => {
      const at = c.attachments.get(String(a))
      return at && at.generation === c.generation ? at : null
    }

    if (seg[2] === "attachments" && method === "POST") {
      const a = id("att-")
      c.attachments.set(a, { role: body.role, stream: body.stream, generation: c.generation })
      return json(201, { attachment_id: a, head: c.log.length, generation: c.generation })
    }
    if (seg[2] === "attachments" && method === "DELETE") {
      c.attachments.delete(seg[3] ?? "")
      if (c.holder === seg[3]) c.holder = null
      return new Response(null, { status: 204 })
    }
    if (seg[2] === "history") {
      // A window of whole runs, newest last; finished runs without deltas (the wire's GET …/history).
      const q = url.searchParams
      const runs = Number(q.get("runs") || 13)
      const before = Number(q.get("before") || 0) || c.log.length + 1
      const through = Number(q.get("through") || 0)
      const log = c.log.filter((ev) => ev.seq < before)
      const starts = log.filter((ev) => ev.verb === "run.start").map((ev) => ev.seq)
      // A Go host sends null for an empty window: the client must take it.
      if (!starts.length) return json(200, { events: null, from: 0, head: c.log.length, more: false })
      let first = Math.max(0, starts.length - runs)
      if (through) while (first > 0 && (starts[first] ?? 0) > through) first--
      const from = starts[first] ?? 0
      const events = log.filter(
        (ev) => ev.seq >= from && !(ev.verb === "part.delta" && ev.run_id && c.finished.has(ev.run_id)),
      )
      return json(200, { events, from, head: c.log.length, more: first > 0 })
    }
    if (seg[2] === "search") {
      const needle = (url.searchParams.get("q") ?? "").toLowerCase()
      let runSeq = 0
      const hits: { seq: number; run_id: string; run_seq: number; snippet: string }[] = []
      for (const ev of c.log) {
        if (ev.verb === "run.start") runSeq = ev.seq
        const text = ev.verb === "part.end" ? String((ev.final as { text?: string } | undefined)?.text ?? "") : ""
        if (needle && text.toLowerCase().includes(needle))
          hits.push({ seq: ev.seq, run_id: ev.run_id ?? "", run_seq: runSeq, snippet: text })
      }
      return json(200, { hits: hits.reverse() })
    }
    if (seg[2] === "events") {
      const a = url.searchParams.get("attachment")
      const at = known(a)
      if (!at) return stale(c)
      const resume = headers["Last-Event-ID"]
      const after = url.searchParams.get("after")
      const from = resume !== undefined ? Number(resume) : after === "head" ? c.log.length : Number(after ?? 0)
      const committed = at.stream === "committed"
      const enc = new TextEncoder()
      let sub: Sub
      const stream = new ReadableStream<Uint8Array>({
        start(control) {
          const push = (ev: StreamEvent) =>
            control.enqueue(enc.encode(`id: ${ev.seq}\ndata: ${JSON.stringify(ev)}\n\n`))
          for (const ev of c.log.slice(from)) {
            // History goes out without deltas; a run still going goes out whole.
            const old = ev.run_id ? c.finished.has(ev.run_id) : true
            if (ev.verb === "part.delta" && (committed || old)) continue
            push(ev)
          }
          sub = {
            attachment: String(a),
            committed,
            push,
            close: () => {
              c.subs.delete(sub)
              try {
                control.close()
              } catch {
                // Already closed.
              }
            },
          }
          c.subs.add(sub)
          init.signal?.addEventListener("abort", () => sub.close())
        },
        cancel() {
          c.subs.delete(sub)
        },
      })
      return new Response(stream, { status: 200, headers: { "Content-Type": "text/event-stream" } })
    }
    if (!known(body?.attachment)) return stale(c)
    if (seg[2] === "lease" && seg[3] === "takeover") {
      const previous = c.holder
      c.holder = body.attachment
      publishTo(c, [
        {
          verb: "activity",
          run_id: "",
          kind: "chat.lease",
          value: { state: "acquired", attachment: c.holder, previous },
        },
      ])
      return json(200, { attachment: c.holder })
    }
    if (seg[2] === "lease" && !seg[3]) {
      // Acquire or renew: a free lease, or this attachment's own.
      if (c.holder && c.holder !== body.attachment)
        return json(409, { code: "lease_held", message: "another attachment holds the input" })
      if (!c.holder) {
        c.holder = body.attachment
        publishTo(c, [
          { verb: "activity", run_id: "", kind: "chat.lease", value: { state: "acquired", attachment: c.holder } },
        ])
      }
      return json(200, { attachment: c.holder })
    }
    if (seg[2] === "turns") {
      if (c.holder && c.holder !== body.attachment)
        return json(403, { code: "lease_required", message: "another attachment holds the input" })
      if (c.activeRun) return json(409, { code: "run_active", message: "a run is going" })
      if (refusal) {
        const r = refusal
        refusal = null
        return json(422, r)
      }
      if (!c.holder) {
        c.holder = body.attachment
        publishTo(c, [
          { verb: "activity", run_id: "", kind: "chat.lease", value: { state: "acquired", attachment: c.holder } },
        ])
      }
      const run = id("run-")
      publishTo(c, [
        { verb: "run.start", run_id: run },
        {
          verb: "message.start",
          run_id: run,
          role: "user",
          message_id: "u",
          meta: { request_id: body.request_id, ...(body.meta ?? {}) },
        },
        { verb: "part.start", run_id: run, part_id: "u1", kind: "text" },
        { verb: "part.end", run_id: run, part_id: "u1", final: { text: body.text } },
        { verb: "message.end", run_id: run, message_id: "u" },
      ])
      const cursor = c.log.length
      queueMicrotask(() => publishTo(c, scripted(run, body.text)))
      return json(202, { run_id: run, cursor })
    }
    if (seg[2] === "runs" && seg[4] === "cancel") {
      if (c.holder !== body.attachment)
        return json(403, { code: "lease_required", message: "the attachment does not hold the input lease" })
      publishTo(c, [{ verb: "run.abort", run_id: seg[3] }])
      return new Response(null, { status: 204 })
    }
    if (seg[2] === "approvals") {
      // As the real host: an answer needs the lease at the moment it is applied.
      if (c.holder !== body.attachment)
        return json(403, { code: "lease_required", message: "the attachment does not hold the input lease" })
      publishTo(c, [
        {
          verb: "activity",
          kind: "chat.approval",
          run_id: c.activeRun ?? undefined,
          value: { approval_id: seg[3], ...body },
        },
      ])
      return new Response(null, { status: 204 })
    }
    return json(404, { code: "not_found", message: "no such route" })
  }

  return {
    fetch: doFetch as typeof fetch,
    script: (fn) => {
      scripted = fn
    },
    publish: (events) => publishTo(convs.get(current) as Conv, events),
    bump: () => {
      const c = convs.get(current) as Conv
      c.generation++
      c.holder = null
      publishTo(c, [{ verb: "activity", run_id: "", kind: "chat.generation", value: { generation: c.generation } }])
      for (const s of [...c.subs]) s.close()
    },
    refuseNext: (code, message) => {
      refusal = { code, message }
    },
    /** The lease lapses (90 s with no renewal on the real host). */
    lapse: () => {
      const c = convs.get(current) as Conv
      if (c.holder)
        publishTo(c, [
          { verb: "activity", run_id: "", kind: "chat.lease", value: { state: "released", attachment: c.holder } },
        ])
      c.holder = null
    },
    dropStreams: () => {
      for (const s of [...(convs.get(current) as Conv).subs]) s.close()
    },
    requests,
    conversation: () => convs.get(current) as Conv,
  }
}

/** A whole answer as the agent's events: one text part written in pieces. */
export const says = (run: string, pieces: string[]): Ev[] => [
  { verb: "message.start", run_id: run, role: "assistant", message_id: "a" },
  { verb: "part.start", run_id: run, part_id: "p1", kind: "text" },
  ...pieces.map((text) => ({ verb: "part.delta", run_id: run, part_id: "p1", text })),
  { verb: "part.end", run_id: run, part_id: "p1", final: { text: pieces.join("") } },
  { verb: "message.end", run_id: run, message_id: "a" },
  { verb: "run.finish", run_id: run, reason: "stop" },
]

/** Wait until a condition holds, checking between event-loop turns. */
export async function until(check: () => boolean, ms = 2000): Promise<void> {
  const end = Date.now() + ms
  while (!check()) {
    if (Date.now() > end) throw new Error("timed out waiting")
    await new Promise((r) => setTimeout(r, 5))
  }
}
