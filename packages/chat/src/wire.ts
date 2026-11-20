/**
 * The legacy agent-host/1 wire, as a page sees it: the host's routes and a
 * follower for a run's chatstream/v1 events that resumes a dropped connection from the last
 * event seen, so nothing is shown twice or missed.
 */

/** What the host says about itself. */
export interface HostState {
  /** The server has an agent at all. */
  available: boolean
  /** A model is configured, so a message can be answered. */
  ready: boolean
  model: string
}

/** A content block as the model sees it. */
export interface Block {
  type: "text" | "tool_use" | "tool_result" | "thinking"
  text?: string
  id?: string
  name?: string
  input?: Record<string, unknown>
  tool_use_id?: string
  content?: string
  is_error?: boolean
}

export interface StoredMessage {
  id: string
  seq: number
  role: "user" | "assistant"
  blocks: Block[]
  meta?: { text?: string; context?: unknown; auto?: string }
  run_id?: string
  created_at: string
}

export interface ThreadState {
  thread_id: string
  messages: StoredMessage[]
  active_run: string
}

export type Decision = "once" | "session" | "always" | "deny"

/** One chatstream/v1 event: the fields a page reads. */
export interface StreamEvent {
  v?: string
  seq: number
  run_id?: string
  verb: string
  reason?: string
  code?: string
  message?: string
  message_id?: string
  role?: string
  part_id?: string
  kind?: string
  meta?: Record<string, unknown>
  text?: string
  final?: unknown
  approval_id?: string
  descriptor?: { tool: string; input?: unknown; description?: string; standing?: boolean }
  value?: Record<string, unknown>
  ext?: Record<string, unknown>
}

/** The host refused or failed a request. `code` is the host's own word for it, when it gave one. */
export class HostError extends Error {
  readonly status: number
  readonly code: string
  /** The rest of what the host said with the refusal (a new conversation id, a generation). */
  readonly data: Record<string, unknown>
  constructor(status: number, message: string, code = "", data: Record<string, unknown> = {}) {
    super(message)
    this.status = status
    this.code = code
    this.data = data
  }
}

export interface HostOptions {
  /** The wire's prefix. Default "/api/agent". */
  base?: string
  fetch?: typeof fetch
  headers?: Record<string, string>
}

const TERMINAL = new Set(["run.finish", "run.error", "run.abort"])

export type HostApi = ReturnType<typeof hostApi>

/** The host's routes. */
export function hostApi(opts: HostOptions = {}) {
  const base = (opts.base ?? "/api/agent").replace(/\/$/, "")
  const doFetch: typeof fetch = opts.fetch ?? ((input, init) => fetch(input, { credentials: "same-origin", ...init }))

  async function request<T>(path: string, method = "GET", body?: unknown): Promise<T> {
    const res = await doFetch(`${base}${path}`, {
      method,
      headers: { ...opts.headers, ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    })
    if (!res.ok) {
      let message = `The agent host answered ${res.status}.`
      let code = ""
      try {
        const data = (await res.json()) as { message?: string; code?: string; error?: string | { message?: string } }
        const said = typeof data.error === "string" ? data.error : (data.error?.message ?? data.message)
        if (said) message = said
        if (typeof data.code === "string") code = data.code
      } catch {
        // No body to read: the status says it.
      }
      throw new HostError(res.status, message, code)
    }
    if (res.status === 204) return undefined as T
    return (await res.json()) as T
  }

  return {
    state: () => request<HostState>(""),
    thread: () => request<ThreadState>("/thread"),
    newThread: () => request<void>("/thread", "DELETE"),
    turn: (body: { text: string; client_id: string; auto?: string }) =>
      request<{ run_id: string }>("/turns", "POST", { ...body, tools: [] }),
    cancel: (run: string) => request<void>(`/runs/${run}/cancel`, "POST", {}),
    approve: (run: string, approval_id: string, decision: Decision) =>
      request<void>(`/runs/${run}/approvals`, "POST", { approval_id, decision }),

    /**
     * Follow a run to its end, calling onEvent for each event in order. A dropped connection
     * is resumed from the last event seen. Resolves when the terminal event has arrived (or the
     * run is long over: the thread has it); rejects when the run cannot be followed.
     */
    async follow(run: string, onEvent: (ev: StreamEvent) => void, signal: AbortSignal): Promise<void> {
      let last = 0
      let failures = 0
      for (;;) {
        if (signal.aborted) return
        try {
          const res = await doFetch(`${base}/runs/${run}/events`, {
            headers: { ...opts.headers, ...(last ? { "Last-Event-ID": String(last) } : {}) },
            signal,
          })
          if (res.status === 410 || res.status === 404) return
          if (!res.ok || !res.body) throw new Error(`events: ${res.status}`)
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
              if (TERMINAL.has(ev.verb)) return
            }
          }
          // The stream closed without a terminal event: reconnect and resume.
        } catch (err) {
          if (signal.aborted) return
          if (++failures > 5) throw err
        }
        await new Promise((r) => setTimeout(r, Math.min(4000, 300 * 2 ** failures)))
      }
    },
  }
}

/** One SSE frame's event, or null for a keepalive comment. */
export function parseFrame(frame: string): StreamEvent | null {
  const data = frame
    .split("\n")
    .filter((l) => l.startsWith("data:"))
    .map((l) => l.slice(l[5] === " " ? 6 : 5))
    .join("\n")
  return data ? (JSON.parse(data) as StreamEvent) : null
}
