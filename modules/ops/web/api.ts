/**
 * The Operations module's routes (/api/m/ops/), as the views call them. Types mirror the Go
 * (model.go, activity.go, seed.go).
 */
import type { ModuleAPI } from "@hollis-labs/surface-page/sdk"
import type { SessionsResult } from "./sessions"

export type Stage = "planned" | "parked" | "queued" | "in_flight" | "landing" | "complete" | "dropped"

export const STAGES: Stage[] = ["planned", "parked", "queued", "in_flight", "landing", "complete", "dropped"]

export const STAGE_LABEL: Record<string, string> = {
  planned: "Planned",
  parked: "Parked",
  queued: "Queued",
  in_flight: "In Flight",
  landing: "Landing",
  complete: "Complete",
  dropped: "Dropped",
  // Transition history retains the vocabulary used when each move was recorded.
  planning: "Planning",
  flight: "In Flight",
  review: "In Review",
  in_review: "In Review",
  adoption: "Adoption",
  active: "Active",
}

export const stageLabel = (s: string) => STAGE_LABEL[s] ?? `Unknown stage: ${s || "(empty)"}`

export interface Held {
  reason: string
  since?: string
}

export interface Check {
  text: string
  done: boolean
  ref?: string
}

export interface Link {
  kind: string
  id: string
  role?: string
  label?: string
}

export interface Transition {
  from: string
  to: string
  at: string
  by?: string
  why?: string
}

export interface Workstream {
  settled_at?: string
  settled_by?: string
  settled_summary?: string
  orchestration_request?: { at: string; note: string; message_id: string; request_id: string }
  id: string
  title: string
  owner?: string
  project?: string
  status_ns?: string
  stage: string
  stage_since?: string
  parent?: string
  held?: Held
  active_when: Check[]
  links: Link[]
  correlation: { labels?: string[]; tether_workstreams?: string[] }
  transitions: Transition[]
  purpose?: string
  active_means?: string
  notes?: string
  rev: number
  updated?: string
  children: string[]
  problem?: string
}

export interface Entry {
  at: string
  source: "torque" | "comment" | "status" | "ops" | "liveness"
  state?: string
  summary: string
  subject_urn?: string
  task?: string
  workstream: string
  verified: boolean
}

export interface Card extends Workstream {
  tasks?: string
  task_counts?: Record<string, number>
  task_list?: Task[]
  last?: Entry
  suggest?: string
  suggest_why?: string
  /** Open tasks assigned or claimed that no live agent holds. */
  no_live_agent?: string[]
}

export interface BoardData {
  workstreams: Card[]
  queue: string[]
  followups: string[]
  error?: string
}

export interface Task {
  id: string
  title: string
  status: string
  updated?: string
  workstream: string
  via?: string
  plan_id?: string
  phase_id?: string
  discovered_via?: string
  /** Who the task is assigned to or claimed by, and whether they are live. */
  holder?: Holder
}

/** A task's assignee or claimant (modules/ops/liveness.go). */
export interface Holder {
  agent?: string
  session?: string
  run?: string
  /** How it was assigned (Torque): owner, doing, claim or dispatch. */
  source: string
  /** live, none (nobody live holds it) or unknown (could not tell). */
  live: "live" | "none" | "unknown"
  why?: string
  /** Found by the fallback source, which cannot see kit-booted agents. */
  unconfirmed?: boolean
}

export interface Plan {
  id: string
  title: string
  status: string
  workstream: string
  via?: string
  phases: { id: string; name: string; order: number; state: string }[]
  truncated?: boolean
}

export interface Rollup {
  workstream: string
  counts: Record<string, number>
  tasks: Task[]
  plans?: Plan[]
  entries: Entry[]
  last?: Entry
  suggest?: string
  suggest_why?: string
  errors?: string[]
  /** Open tasks assigned or claimed that no live agent holds. */
  no_live_agent?: string[]
  fetched_at: string
  unavailable?: boolean
}

export interface GlobalFeed {
  entries: Entry[]
  errors?: string[]
  pending: number
  truncated?: boolean
  calls: number
  fetched_at: string
}

export interface Detail {
  workstream: Workstream
  children: { id: string; title: string; stage: Stage; held?: Held; children: number }[]
  path: { id: string; title: string }[] | null
}

export interface Revision {
  rev: number
  at: string
  title: string
}

export interface History {
  settled_at?: string
  settled_by?: string
  settled_summary?: string
  transitions: Transition[]
  revisions: Revision[]
  ref: string
  error?: string
}

export interface SeedItem {
  include: boolean
  id: string
  title: string
  stage: Stage
  from_stage: string
  phase?: string
  owner?: string
  parent?: string
  notes: string
  links: Link[]
  exists?: boolean
  problems?: string[]
}

export interface SeedConcept {
  accept: boolean
  id: string
  title: string
  why: string
  members: string[]
  stage: Stage
  exists?: boolean
}

export interface SeedFollowup {
  index: number
  text: string
  workstream?: string
  why?: string
}

export interface SeedPlan {
  id: string
  source: string
  tracker_updated?: string
  created_at: string
  applied_at?: string
  workstreams: SeedItem[]
  concepts: SeedConcept[]
  followups: SeedFollowup[]
  mapping: string[]
}

export interface SeedResult {
  created: string[]
  skipped: string[]
  failed: string[]
  followups_for_owner: number
  noted: string[]
}

const q = encodeURIComponent

export function opsClient(api: ModuleAPI) {
  return {
    agents: () => api.get<SessionsResult>("agents"),
    board: (force = false, showSettled = false) =>
      api.get<BoardData>(`board?force=${force ? 1 : 0}&show_settled=${showSettled ? 1 : 0}`),
    settle: (id: string, base_rev: number) => api.post<Workstream>(`workstreams/${q(id)}/settle`, { base_rev }),
    unsettle: (id: string, base_rev: number) => api.post<Workstream>(`workstreams/${q(id)}/unsettle`, { base_rev }),
    send: (id: string, base_rev: number, request_id: string, note: string) =>
      api.post<Workstream>(`workstreams/${q(id)}/orchestration`, { base_rev, request_id, note }),
    create: (w: Partial<Workstream> & { id: string; title: string }) => api.post<Workstream>("workstreams", w),
    get: (id: string) => api.get<Detail>(`workstreams/${q(id)}`),
    update: (id: string, patch: Record<string, unknown>) => api.put<Workstream>(`workstreams/${q(id)}`, patch),
    move: (id: string, stage: string, why: string) => api.post<Workstream>(`workstreams/${q(id)}/move`, { stage, why }),
    link: (id: string, l: Link) => api.post<Workstream>(`workstreams/${q(id)}/links`, l),
    unlink: (id: string, kind: string, ref: string) =>
      api.del<Workstream>(`workstreams/${q(id)}/links?kind=${q(kind)}&ref=${q(ref)}`),
    globalActivity: (filters: Record<string, string>, refresh = false) =>
      api.get<GlobalFeed>(`activity?${new URLSearchParams({ ...filters, ...(refresh ? { refresh: "1" } : {}) })}`),
    activity: (id: string, refresh = false) =>
      api.get<Rollup>(`workstreams/${q(id)}/activity${refresh ? "?refresh=1" : ""}`),
    history: (id: string) => api.get<History>(`workstreams/${q(id)}/history`),
    at: (id: string, rev: number) => api.get<{ rev: number; content: string }>(`workstreams/${q(id)}/at/${rev}`),
    reorder: (id: string, where: { before?: string; after?: string; dir?: "up" | "down" }) =>
      api.post<{ queue: string[] }>("queue/reorder", { id, ...where }),
    seedPreview: (src: { path?: string; json?: string }) => api.post<SeedPlan>("seed/preview", src),
    seedGet: (id: string) => api.get<SeedPlan>(`seed/${q(id)}`),
    seedSave: (p: SeedPlan) => api.put<SeedPlan>(`seed/${q(p.id)}`, p),
    /** Applies the preview as it is kept: save the owner's edits first. */
    seedApply: (id: string) => api.post<SeedResult>(`seed/${q(id)}/apply`),
  }
}

export type OpsClient = ReturnType<typeof opsClient>

export const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** "3m", "5h", "2d": how long ago, short. */
export function age(iso?: string): string {
  if (!iso) return ""
  const ms = Date.now() - new Date(iso).getTime()
  if (Number.isNaN(ms)) return ""
  const m = Math.max(0, Math.round(ms / 60000))
  if (m < 60) return `${m}m`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h`
  return `${Math.round(h / 24)}d`
}

export const time = (iso?: string) => (iso ? new Date(iso).toLocaleString() : "")

/** The Cite kinds the page resolves; any other link shows as text. */
export const RESOLVED_KINDS = new Set([
  "ops.workstream",
  "work.item",
  "torque.task",
  "torque.epic",
  "torque.plan",
  "torque.project",
  "torque.sprint",
  "registry.record",
  "caster.doc",
  "tesseract.item",
  "tesseract.key",
  "tether.agent",
  "tether.message",
  "url",
])
