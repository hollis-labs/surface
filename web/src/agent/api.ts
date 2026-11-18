/**
 * The agent's own routes beside the agent-host/1 wire (core/internal/agent/http.go).
 */

/** One active slot (slots.SlotView). */
export interface SlotView {
  name: string
  module?: string
  about: string
  instructions?: string
  where?: string
  note?: string
  note_updated?: string
  knowledge?: string
}

/** What the next message carries (slots.Assembly). */
export interface Assembly {
  slot: string
  slots: SlotView[]
  why: string
  top_of_mind: string[] | null
  focused?: { window: string; view: string; module: string; title: string; kind?: string; ref?: string }
  mode?: string
  layout?: string
  reminders?: string[]
  block: string
}

/** One slot in the app (agent.SlotInfo). */
export interface SlotInfo {
  name: string
  module?: string
  about: string
  views?: string[]
  tools?: string[]
  note?: string
  active: boolean
}

export interface Setup {
  ready: boolean
  model: string
  config_path: string
  /** Why the agent cannot answer, in a sentence, when it cannot. */
  reason?: string
  /** "conversation" when the agent is on the conversation wire; absent on the run-at-a-time one. */
  wire?: string
  /** The agent's name on the conversation wire. */
  agent?: string
}

const base = "/api/agent"

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(base + path, {
    method,
    credentials: "same-origin",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let msg = res.statusText
    try {
      msg = ((await res.json()) as { message?: string }).message ?? msg
    } catch {
      /* not JSON */
    }
    throw new Error(msg)
  }
  return (await res.json()) as T
}

export const agentApi = {
  context: () => call<Assembly>("GET", "/context"),
  setup: () => call<Setup>("GET", "/setup"),
  slots: () => call<SlotInfo[]>("GET", "/slots"),
  /** Pin a slot; "" lets the window in front choose again. */
  pin: (slot: string) => call<Assembly>("POST", "/slots/pin", { slot }),
}
