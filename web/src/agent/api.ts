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
  /** Slots/items trimmed by context budget. */
  trimmed?: string[]
  /** Module Around lines carried with the message. */
  around?: { module: string; text: string }[]
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
  runner?: string
  context_limit_bytes?: number
}

export interface RunRecord {
  run_id: string
  started_at: string
  ended_at?: string
  status?: string
  reason?: string
  origin?: string
  slot?: string
  profile?: string
  provider?: string
  model?: string
  uncached_input: number
  cache_read: number
  cache_write: number
  output: number
  tools: string[]
  ms: number
}

export interface StandingState {
  path?: string
  text: string
  in_system_prompt?: boolean
  chars: number
  error?: string
}

export interface StandingGrant {
  agent: string
  tool: string
  reason?: string
  created: string
}

export interface ReflexInfo {
  name: string
  about?: string
  trigger_kind: string
  trigger: Record<string, unknown>
  action_kind: string
  action: Record<string, unknown>
  cooldown_seconds?: number
  enabled: boolean
}

export interface RuleInfo {
  id: string
  priority?: number
  when: Record<string, unknown>[]
  target: string
  params?: Record<string, string>
  disabled?: boolean
}

export interface TraceSignal {
  at: string
  kind: string
  actor?: string
  detail?: Record<string, unknown>
}

export interface TraceTurn {
  id: number
  at: string
  run_id?: string
  text: string
  view?: string
  module?: string
  mode?: string
  layout?: string
  open_views: string[]
  slot: string
  slots: string[]
  why?: string
  top_of_mind: string[]
  routed?: string
  fired: string[]
  halted?: boolean
  block_chars: number
  trimmed: string[]
  signals?: TraceSignal[]
}

export interface ReplayReport {
  turns: number
  agreed?: number
  answered?: number
  corrected?: number
  corrected_hits?: number
  acts?: Record<string, number>
  misses?: { id: number; text: string; chosen: string; top: string; p: number; corrected_to?: string }[]
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
  runs: (limit = 50) => call<RunRecord[]>("GET", `/runs?limit=${limit}`),
  standing: () => call<StandingState>("GET", "/standing"),
  grants: () => call<StandingGrant[]>("GET", "/grants"),
  reflexes: () => call<ReflexInfo[]>("GET", "/reflexes"),
  rules: () => call<{ owner: RuleInfo[]; builtin: RuleInfo[] }>("GET", "/rules"),
  trace: (limit = 100) => call<{ on: boolean; turns: TraceTurn[] }>("GET", `/trace?limit=${limit}`),
  replay: () => call<ReplayReport>("GET", "/classify/replay"),
  classifyFeedback: (body: { text: string; facet: string; label: string; accepted: boolean }) =>
    call<Assembly>("POST", "/classify/feedback", body),
  /** Answer an interrupted turn; reset answers {conversation}: the agent's new history. */
  recover: (conversation: string, action: RecoverAction) =>
    call<{ status?: string; conversation?: string }>(
      "POST",
      `/conversations/${encodeURIComponent(conversation)}/interrupted/${action}`,
      {},
    ),
}
