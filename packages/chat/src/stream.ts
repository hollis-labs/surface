/** Shared conversation stream types and SSE decoding. */

/** What the host says about itself. */
export interface HostState {
  /** The server has an agent at all. */
  available: boolean
  /** A model is configured, so a message can be answered. */
  ready: boolean
  model: string
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

export type RunEnd = "finish" | "error" | "abort" | "lost"

/** One SSE frame's event, or null for a keepalive comment. */
export function parseFrame(frame: string): StreamEvent | null {
  const data = frame
    .split("\n")
    .filter((l) => l.startsWith("data:"))
    .map((l) => l.slice(l[5] === " " ? 6 : 5))
    .join("\n")
  return data ? (JSON.parse(data) as StreamEvent) : null
}
