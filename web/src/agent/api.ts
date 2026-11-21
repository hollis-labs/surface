/**
 * The agent's own routes beside the conversation wire (core/internal/agent/http.go).
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
  /** The module whose agent profile the next turn runs as; absent: the app's own. */
  profile?: string
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

/** One module agent profile (agent.ProfileInfo). */
export interface ProfileInfo {
  id: string
  title: string
  name: string
  description?: string
  skills: string[]
  variants: string[]
  tools: string[]
  active: boolean
}

export interface BindingInfo {
  id: string
  title: string
  dir?: string
  name?: string
  error?: string
  ignored_runtime?: string[]
}

/** The profiles and the one in play (agent.ProfileState). */
export interface ProfileState {
  /** Turns run as profiles (the Nanite runner); off, every turn is the app's own agent. */
  on: boolean
  runner?: string
  /** id "" is the app's own; why is pinned, focus, default or off. */
  active: { id: string; why: string }
  /** A module id, "default", or absent. */
  pinned?: string
  profiles: ProfileInfo[]
  bindings?: BindingInfo[]
}

export interface Setup {
  stop_all?: boolean
  /** Whether the embedded Nanite runner bypasses approval asks (testing only). */
  auto_approve?: boolean
  ready: boolean
  model: string
  config_path: string
  /** Why the agent cannot answer, in a sentence, when it cannot. */
  reason?: string
  /** "conversation" when a configured agent is available. */
  wire?: string
  /** Same-origin attachment endpoint when the runner lives in Tether. */
  wire_base?: string
  /** The agent's name on the conversation wire. */
  agent?: string
}

/** What the owner can do with an interrupted turn (core/internal/agent/interrupted.go). */
export type RecoverAction = "resume" | "discard" | "reset"

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
  stopAll: () => call<{ restart_required: boolean; message: string }>("POST", "/stop-all", {}),
  context: () => call<Assembly>("GET", "/context"),
  setup: () => call<Setup>("GET", "/setup"),
  slots: () => call<SlotInfo[]>("GET", "/slots"),
  /** Pin a slot; "" lets the window in front choose again. */
  pin: (slot: string) => call<Assembly>("POST", "/slots/pin", { slot }),
  profiles: () => call<ProfileState>("GET", "/profiles"),
  /** Pin a profile: a module id, "default" (the app's own), or "auto" to let the window in front choose. */
  pinProfile: (profile: string) => call<ProfileState>("POST", "/profiles/pin", { profile }),
  /** Answer an interrupted turn; reset answers {conversation}: the agent's new history. */
  recover: (conversation: string, action: RecoverAction) =>
    call<{ status?: string; conversation?: string }>(
      "POST",
      `/conversations/${encodeURIComponent(conversation)}/interrupted/${action}`,
      {},
    ),
}
