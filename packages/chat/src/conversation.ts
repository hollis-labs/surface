/**
 * The conversation wire, as a page sees it: one durable conversation per agent, outliving
 * any turn and any client. A client attaches as a viewer or a controller, reads one event
 * stream (chatstream/v1, unchanged) from wherever it says, and submits turns when it holds
 * the right to type. Nothing here knows which agent runs the turns: any server that speaks
 * the wire works, at any base URL.
 */
import { HostError, parseFrame, type StreamEvent } from "./wire.ts"

export type Role = "viewer" | "controller"
/** "deltas" takes text as it is written; "committed" takes each part once, whole. */
export type StreamMode = "deltas" | "committed"

export interface Conversation {
  conversation_id: string
  /** The cursor of the latest event. */
  head: number
  active_run?: string | null
  lease?: { attachment?: string; holder?: string; expires_at?: string } | null
  generation?: number
  capabilities?: { deltas?: boolean }
}

export interface Attachment {
  attachment_id: string
  head: number
}

/** What a person decided about a tool the agent asked to run. */
export interface ApprovalAnswer {
  decision: "allow" | "deny"
  /** "once" is this call; "conversation" the rest of this conversation; "always" a standing grant. */
  scope: "once" | "conversation" | "always"
}

export interface ConversationOptions {
  /** The wire's prefix. Default "/api/agent". */
  base?: string
  fetch?: typeof fetch
  headers?: Record<string, string>
}

/** Where a first connect starts reading: 0 for everything, a cursor, or "head" for new events only. */
export type Start = number | "head"

export type ConversationApi = ReturnType<typeof conversationApi>

/** The attachment is from before the agent's side was replaced: attach again. */
export const isStale = (err: unknown): err is HostError =>
  err instanceof HostError && err.status === 409 && err.code === "stale_attachment"
/** The conversation was reset: `data.conversation_id` is the one that replaced it. */
export const isClosed = (err: unknown): err is HostError =>
  err instanceof HostError && err.status === 410 && err.code === "conversation_closed"

async function refusal(res: Response): Promise<HostError> {
  let message = `The agent host answered ${res.status}.`
  let code = ""
  let data: Record<string, unknown> = {}
  try {
    data = (await res.json()) as Record<string, unknown>
    const error = data.error
    const said =
      typeof error === "string" ? error : ((error as { message?: string } | undefined)?.message ?? data.message)
    if (typeof said === "string" && said) message = said
    if (typeof data.code === "string") code = data.code
  } catch {
    // No body to read: the status says it.
  }
  return new HostError(res.status, message, code, data)
}

export function conversationApi(opts: ConversationOptions = {}) {
  const base = (opts.base ?? "/api/agent").replace(/\/$/, "")
  const doFetch: typeof fetch = opts.fetch ?? ((input, init) => fetch(input, { credentials: "same-origin", ...init }))
  const enc = encodeURIComponent

  async function request<T>(path: string, method = "GET", body?: unknown): Promise<T> {
    const res = await doFetch(`${base}${path}`, {
      method,
      headers: { ...opts.headers, ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    })
    if (!res.ok) throw await refusal(res)
    if (res.status === 204) return undefined as T
    const text = await res.text()
    return (text ? JSON.parse(text) : undefined) as T
  }

  return {
    /** The agent's current conversation, made on first use. */
    open: (agent: string) => request<Conversation>(`/agents/${enc(agent)}/conversation`, "POST", {}),
    /** End the agent's conversation and start a new one. */
    reset: (agent: string) => request<Conversation>(`/agents/${enc(agent)}/conversation/reset`, "POST", {}),
    get: (id: string) => request<Conversation>(`/conversations/${enc(id)}`),
    attach: (id: string, role: Role, stream: StreamMode) =>
      request<Attachment>(`/conversations/${enc(id)}/attachments`, "POST", { role, stream }),
    detach: (id: string, attachment: string) =>
      request<void>(`/conversations/${enc(id)}/attachments/${enc(attachment)}`, "DELETE"),
    /** Take or renew the right to type. 409 when another attachment holds it. */
    lease: (id: string, attachment: string) => request<void>(`/conversations/${enc(id)}/lease`, "POST", { attachment }),
    takeover: (id: string, attachment: string) =>
      request<void>(`/conversations/${enc(id)}/lease/takeover`, "POST", { attachment }),
    /**
     * Submit a turn. The events come on the event stream, not here. 403 without the right to
     * type, 409 while a run is going, 422 when the host refuses the turn (with its reason).
     */
    submit: (
      id: string,
      turn: { attachment: string; text: string; request_id: string; meta?: Record<string, unknown>; context?: unknown },
    ) => request<{ run_id: string; cursor: number }>(`/conversations/${enc(id)}/turns`, "POST", turn),
    cancel: (id: string, run: string, attachment: string) =>
      request<void>(`/conversations/${enc(id)}/runs/${enc(run)}/cancel`, "POST", { attachment }),
    approve: (id: string, approval: string, attachment: string, answer: ApprovalAnswer) =>
      request<void>(`/conversations/${enc(id)}/approvals/${enc(approval)}`, "POST", { attachment, ...answer }),

    /**
     * Follow a conversation's events, calling onEvent for each in order, until the signal
     * aborts. The first connect starts where `start` says; every reconnect resumes after the
     * last event seen, so nothing is shown twice or missed. Rejects when the stream cannot
     * be followed any more: the attachment is stale (isStale), the conversation was reset
     * (isClosed), or the connection keeps failing.
     */
    async follow(
      id: string,
      attachment: string,
      start: Start,
      onEvent: (ev: StreamEvent) => void,
      signal: AbortSignal,
    ): Promise<void> {
      // A cursor to resume after, once any event has been seen or the start was a number.
      let last = typeof start === "number" ? start : -1
      let first = true
      let failures = 0
      for (;;) {
        if (signal.aborted) return
        try {
          const after = first || last < 0 ? `&after=${first ? start : "head"}` : ""
          const res = await doFetch(`${base}/conversations/${enc(id)}/events?attachment=${enc(attachment)}${after}`, {
            headers: { ...opts.headers, ...(!first && last >= 0 ? { "Last-Event-ID": String(last) } : {}) },
            signal,
          })
          if (res.status === 409 || res.status === 410 || res.status === 403 || res.status === 404) {
            throw await refusal(res)
          }
          if (!res.ok || !res.body) throw new Error(`events: ${res.status}`)
          first = false
          const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
          let buffer = ""
          for (;;) {
            const { value, done } = await reader.read()
            if (done) break
            buffer += value
            let cut = buffer.indexOf("\n\n")
            while (cut >= 0) {
              const frame = buffer.slice(0, cut)
              buffer = buffer.slice(cut + 2)
              cut = buffer.indexOf("\n\n")
              const ev = parseFrame(frame)
              if (!ev || ev.seq <= last) continue
              last = ev.seq
              failures = 0
              onEvent(ev)
            }
          }
          // The server closed the stream (a slow client, a generation change): connect again.
        } catch (err) {
          if (signal.aborted) return
          if (err instanceof HostError) throw err
          if (++failures > 8) throw err
        }
        await new Promise((r) => setTimeout(r, Math.min(4000, 150 * 2 ** failures)))
      }
    },
  }
}
